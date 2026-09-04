package migrations

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	. "github.com/onsi/gomega"
)

func TestMigration171RejectsIncompleteProtectedHistoryExceptionEvidence(t *testing.T) {
	database := newMigrationTestDatabase(t)
	database.migrateTo(t, 170)
	legacy := insertMigration170ProtectedHistoryFixture(t, database)
	database.migrateTo(t, 171)

	for _, testCase := range []struct {
		name      string
		key       any
		reference any
	}{
		{name: "missing evidence", key: nil, reference: nil},
		{
			name:      "missing reference",
			key:       "scoped-single-reviewer-pilot",
			reference: nil,
		},
		{
			name:      "missing key",
			key:       nil,
			reference: "approval:pilot-001",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			tx, err := database.pool.Begin(context.Background())
			NewWithT(t).Expect(err).NotTo(HaveOccurred())
			defer func() { _ = tx.Rollback(context.Background()) }()

			err = insertMigration171ProtectedHistoryCandidate(
				context.Background(),
				tx,
				legacy.ID,
				uuid.New(),
				uuid.New(),
				nil,
				[]uuid.UUID{uuid.New()},
				testCase.key,
				testCase.reference,
				"migration-171-incomplete-"+uuid.NewString(),
			)
			expectMigration171CheckViolation(
				t, err, "protectedhistoryartifact_review_governance_check",
			)
		})
	}
}

func TestMigration171RejectsIncompleteApprovalDecisionExceptionEvidence(t *testing.T) {
	g := NewWithT(t)
	database := newMigrationTestDatabase(t)
	database.migrateTo(t, 171)
	organizationID := uuid.New()
	actorID := uuid.New()
	_, err := database.pool.Exec(context.Background(), `
INSERT INTO Organization (id, name) VALUES ($1, $2)
`, organizationID, "Migration 171 approval "+uuid.NewString())
	g.Expect(err).NotTo(HaveOccurred())
	_, err = database.pool.Exec(context.Background(), `
INSERT INTO UserAccount (id, email) VALUES ($1, $2)
`, actorID, "migration-171-"+uuid.NewString()+"@example.invalid")
	g.Expect(err).NotTo(HaveOccurred())
	_, err = database.pool.Exec(context.Background(), `
ALTER TABLE ApprovalDecision
  DROP CONSTRAINT approvaldecision_request_fk,
	  DROP CONSTRAINT approvaldecision_requirement_fk
`)
	g.Expect(err).NotTo(HaveOccurred())

	for _, testCase := range []struct {
		name      string
		key       any
		reference any
	}{
		{
			name:      "missing reference",
			key:       "scoped-single-reviewer-pilot",
			reference: nil,
		},
		{
			name:      "missing key",
			key:       nil,
			reference: "approval:pilot-001",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			tx, beginErr := database.pool.Begin(context.Background())
			NewWithT(t).Expect(beginErr).NotTo(HaveOccurred())
			defer func() { _ = tx.Rollback(context.Background()) }()

			_, insertErr := tx.Exec(context.Background(), `
INSERT INTO ApprovalDecision (
  id, organization_id, approval_request_id, approval_requirement_id,
  actor_useraccount_id, decision, comment, request_revision, idempotency_key,
  governance_exception_key, governance_exception_reference
) VALUES (
  $1, $2, $3, $4,
  $5, 'APPROVE', 'Migration 171 incomplete exception test', 1, $6,
  $7, $8
)
`,
				uuid.New(), organizationID, uuid.New(), uuid.New(), actorID,
				"migration-171-decision-"+uuid.NewString(),
				testCase.key, testCase.reference,
			)
			expectMigration171CheckViolation(
				t, insertErr, "approvaldecision_governance_exception_check",
			)
		})
	}
}

func TestMigration171AcceptsValidExceptionAndRefusesDown(t *testing.T) {
	g := NewWithT(t)
	database := newMigrationTestDatabase(t)
	database.migrateTo(t, 170)
	legacy := insertMigration170ProtectedHistoryFixture(t, database)
	database.migrateTo(t, 171)

	ctx := context.Background()
	artifactID := uuid.New()
	auditEventID := uuid.New()
	tx, err := database.pool.Begin(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	defer func() { _ = tx.Rollback(context.Background()) }()
	g.Expect(insertMigration171ProtectedHistoryCandidate(
		ctx,
		tx,
		legacy.ID,
		artifactID,
		auditEventID,
		nil,
		[]uuid.UUID{uuid.New()},
		"scoped-single-reviewer-pilot",
		"approval:pilot-001",
		"migration-171-valid-"+uuid.NewString(),
	)).To(Succeed())
	_, err = tx.Exec(ctx, `
INSERT INTO ControlPlaneAuditEvent (
  id, organization_id, sequence, event_type, actor_id, outcome,
  protected_history_artifact_id, artifact_digest, payload
)
SELECT
  audit_event_id,
  organization_id,
  audit_event_sequence,
  'protected_history.retained',
  issuer_useraccount_id,
  'SUCCEEDED',
  id,
  content_checksum,
  jsonb_build_object(
    'retentionChecksum', retention_checksum,
    'requestChecksum', request_checksum,
    'artifactId', artifact_id,
    'recordsRoot', records_root,
    'objectReference', object_reference,
    'mediaType', media_type,
    'byteLength', byte_length,
    'contentChecksum', content_checksum,
    'capturedAt', protected_history_rfc3339_microseconds(captured_at),
    'issuerUserAccountId', issuer_useraccount_id::TEXT,
    'reviewerUserAccountId', reviewer_useraccount_id::TEXT,
    'governanceExceptionKey', governance_exception_key,
    'governanceExceptionReference', governance_exception_reference
  )
FROM ProtectedHistoryArtifact
WHERE id = $1
`, artifactID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(tx.Commit(ctx)).To(Succeed())

	err = database.runner.Migrate(170)
	g.Expect(err).To(MatchError(ContainSubstring(
		"refusing migration 171 rollback while pilot governance exception evidence exists",
	)))
	var retainedCount int
	g.Expect(database.pool.QueryRow(ctx, `
SELECT count(*) FROM ProtectedHistoryArtifact WHERE id = $1
`, artifactID).Scan(&retainedCount)).To(Succeed())
	g.Expect(retainedCount).To(Equal(1))
}

func TestMigration171RejectsSingleReviewerOutsideOneTargetScope(t *testing.T) {
	database := newMigrationTestDatabase(t)
	database.migrateTo(t, 170)
	legacy := insertMigration170ProtectedHistoryFixture(t, database)
	database.migrateTo(t, 171)

	for _, testCase := range []struct {
		name        string
		customerIDs []uuid.UUID
		targetIDs   []uuid.UUID
	}{
		{name: "customer only", customerIDs: []uuid.UUID{uuid.New()}},
		{
			name:        "customer and target",
			customerIDs: []uuid.UUID{uuid.New()},
			targetIDs:   []uuid.UUID{uuid.New()},
		},
		{name: "multiple targets", targetIDs: []uuid.UUID{uuid.New(), uuid.New()}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			tx, err := database.pool.Begin(context.Background())
			NewWithT(t).Expect(err).NotTo(HaveOccurred())
			defer func() { _ = tx.Rollback(context.Background()) }()

			err = insertMigration171ProtectedHistoryCandidate(
				context.Background(), tx, legacy.ID, uuid.New(), uuid.New(),
				testCase.customerIDs, testCase.targetIDs,
				"scoped-single-reviewer-pilot", "approval:pilot-001",
				"migration-171-scope-"+uuid.NewString(),
			)
			expectMigration171CheckViolation(
				t, err, "protectedhistoryartifact_review_governance_check",
			)
		})
	}
}

func TestRunnerRestoresCleanStatusAfterMigration171GuardRefusal(t *testing.T) {
	g := NewWithT(t)
	database := newRunnerTestDatabase(t)
	database.migrateTo(t, 171)
	ctx := context.Background()
	organizationID := uuid.New()
	actorID := uuid.New()
	_, err := database.pool.Exec(
		ctx,
		`INSERT INTO Organization (id, name) VALUES ($1, $2)`,
		organizationID,
		"Migration 171 runner "+uuid.NewString(),
	)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = database.pool.Exec(
		ctx,
		`INSERT INTO UserAccount (id, email) VALUES ($1, $2)`,
		actorID,
		"migration-171-runner-"+uuid.NewString()+"@example.invalid",
	)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = database.pool.Exec(ctx, `ALTER TABLE ApprovalDecision
  DROP CONSTRAINT approvaldecision_request_fk,
	  DROP CONSTRAINT approvaldecision_requirement_fk`)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = database.pool.Exec(ctx, `INSERT INTO ApprovalDecision (
  id, organization_id, approval_request_id, approval_requirement_id,
  actor_useraccount_id, decision, comment, request_revision, idempotency_key,
  governance_exception_key, governance_exception_reference
) VALUES (
  $1, $2, $3, $4, $5, 'APPROVE', 'Migration 171 runner guard', 1, $6,
  'scoped-single-reviewer-pilot', 'approval:pilot-001'
)`,
		uuid.New(), organizationID, uuid.New(), uuid.New(), actorID,
		"migration-171-runner-"+uuid.NewString(),
	)
	g.Expect(err).NotTo(HaveOccurred())

	err = database.runner.Run(ctx, RunOptions{Target: uintPointer(170)})
	g.Expect(err).To(MatchError(ContainSubstring(
		"refusing migration 171 rollback while pilot governance exception evidence exists",
	)))
	status, statusErr := database.runner.Status(ctx)
	g.Expect(statusErr).NotTo(HaveOccurred())
	g.Expect(status).To(Equal(SchemaStatus{Version: 171, Dirty: false}))
}

func TestScopedSingleReviewerDowngradeGuardRejected(t *testing.T) {
	g := NewWithT(t)
	for _, message := range []string{
		"refusing migration 171 rollback while pilot governance exception evidence exists",
		"refusing migration 171 rollback while schema-171 protected-history evidence exists",
	} {
		g.Expect(scopedSingleReviewerDowngradeGuardRejected(errors.New("wrapped: " + message))).To(BeTrue())
	}
	g.Expect(scopedSingleReviewerDowngradeGuardRejected(errors.New(
		"downgrade crossing 138 is forbidden after timestamp retention",
	))).To(BeFalse())
}

func TestMigration171LegacyProtectedHistoryChecksumRoundTrip(t *testing.T) {
	g := NewWithT(t)
	database := newMigrationTestDatabase(t)
	database.migrateTo(t, 170)
	legacy := insertMigration170ProtectedHistoryFixture(t, database)
	before := readMigration171Checksums(t, database, legacy.ID)

	database.migrateTo(t, 171)
	g.Expect(readMigration171Checksums(t, database, legacy.ID)).To(Equal(before))

	database.migrateTo(t, 170)
	g.Expect(readMigration171Checksums(t, database, legacy.ID)).To(Equal(before))
}

func insertMigration171ProtectedHistoryCandidate(
	ctx context.Context,
	tx pgx.Tx,
	sourceID uuid.UUID,
	artifactID uuid.UUID,
	auditEventID uuid.UUID,
	customerOrganizationIDs []uuid.UUID,
	deploymentTargetIDs []uuid.UUID,
	governanceExceptionKey any,
	governanceExceptionReference any,
	idempotencyKey string,
) error {
	if customerOrganizationIDs == nil {
		customerOrganizationIDs = []uuid.UUID{}
	}
	if deploymentTargetIDs == nil {
		deploymentTargetIDs = []uuid.UUID{}
	}
	if len(customerOrganizationIDs) > 0 {
		_, err := tx.Exec(ctx, `
INSERT INTO CustomerOrganization (id, organization_id, name)
SELECT customer_id, source.organization_id, 'Migration 171 customer ' || customer_id::TEXT
FROM ProtectedHistoryArtifact source
CROSS JOIN unnest($2::UUID[]) customer_id
WHERE source.id = $1
ON CONFLICT (id) DO NOTHING
`, sourceID, customerOrganizationIDs)
		if err != nil {
			return err
		}
	}
	if len(deploymentTargetIDs) > 0 {
		_, err := tx.Exec(ctx, `
INSERT INTO DeploymentTarget (id, name, type, organization_id, agent_version_id)
SELECT
  target_id,
  'Migration 171 target ' || target_id::TEXT,
  'docker',
  source.organization_id,
  agent.id
FROM ProtectedHistoryArtifact source
CROSS JOIN LATERAL (
  SELECT id FROM AgentVersion ORDER BY created_at, id LIMIT 1
) agent
CROSS JOIN unnest($2::UUID[]) target_id
WHERE source.id = $1
ON CONFLICT (id) DO NOTHING
`, sourceID, deploymentTargetIDs)
		if err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `
WITH source AS (
  SELECT * FROM ProtectedHistoryArtifact WHERE id = $1
), candidate AS (
  SELECT
    $2::UUID AS id,
    source.created_at,
    source.organization_id,
    source.schema,
    source.source_schema_version,
    $7::UUID[] AS customer_organization_ids,
    $8::UUID[] AS deployment_target_ids,
    source.artifact_id,
    source.records_root,
    source.record_count,
    source.object_reference,
    source.media_type,
    source.byte_length,
    source.content_checksum,
    source.captured_at,
    source.issuer_useraccount_id,
    source.issuer_useraccount_id AS reviewer_useraccount_id,
    $4::TEXT AS governance_exception_key,
    $5::TEXT AS governance_exception_reference,
    COALESCE(
      protected_history_retention_checksum(
        $2,
        source.organization_id,
        source.schema,
        source.source_schema_version,
        $7,
        $8,
        source.artifact_id,
        source.records_root,
        source.record_count,
        source.object_reference,
        source.media_type,
        source.byte_length,
        source.content_checksum,
        source.captured_at,
        source.issuer_useraccount_id,
        source.issuer_useraccount_id,
        $4,
        $5
      ),
      'sha256:' || repeat('f', 64)
    ) AS retention_checksum,
    $3::UUID AS audit_event_id,
    (
      SELECT COALESCE(MAX(event.sequence), 0) + 1
      FROM ControlPlaneAuditEvent event
      WHERE event.organization_id = source.organization_id
    ) AS audit_event_sequence,
    $6::TEXT AS idempotency_key,
    COALESCE(
      protected_history_request_checksum(
        source.organization_id,
        $7,
        $8,
        source.issuer_useraccount_id,
        source.issuer_useraccount_id,
        $4,
        $5,
        $6
      ),
      'sha256:' || repeat('e', 64)
    ) AS request_checksum
  FROM source
)
INSERT INTO ProtectedHistoryArtifact (
  id, created_at, organization_id, schema, source_schema_version,
  customer_organization_ids, deployment_target_ids,
  artifact_id, records_root, record_count,
  object_reference, media_type, byte_length, content_checksum, captured_at,
  issuer_useraccount_id, reviewer_useraccount_id,
  governance_exception_key, governance_exception_reference, retention_checksum,
  audit_event_id, audit_event_sequence, audit_binding_checksum,
  idempotency_key, request_checksum
)
SELECT
  id, created_at, organization_id, schema, source_schema_version,
  customer_organization_ids, deployment_target_ids,
  artifact_id, records_root, record_count,
  object_reference, media_type, byte_length, content_checksum, captured_at,
  issuer_useraccount_id, reviewer_useraccount_id,
  governance_exception_key, governance_exception_reference, retention_checksum,
  audit_event_id, audit_event_sequence,
  protected_history_audit_binding_checksum(
    id, retention_checksum, audit_event_id, audit_event_sequence
  ),
  idempotency_key, request_checksum
FROM candidate
`,
		sourceID,
		artifactID,
		auditEventID,
		governanceExceptionKey,
		governanceExceptionReference,
		idempotencyKey,
		customerOrganizationIDs,
		deploymentTargetIDs,
	)
	return err
}

func expectMigration171CheckViolation(
	t *testing.T,
	err error,
	constraintName string,
) {
	t.Helper()
	g := NewWithT(t)
	g.Expect(err).To(HaveOccurred())
	var pgErr *pgconn.PgError
	g.Expect(errors.As(err, &pgErr)).To(BeTrue())
	g.Expect(pgErr.Code).To(Equal(pgerrcode.CheckViolation))
	g.Expect(pgErr.ConstraintName).To(Equal(constraintName))
}

type migration171Checksums struct {
	requestChecksum      string
	retentionChecksum    string
	auditBindingChecksum string
}

func readMigration171Checksums(
	t *testing.T,
	database *migrationTestDatabase,
	artifactID uuid.UUID,
) migration171Checksums {
	t.Helper()
	var checksums migration171Checksums
	err := database.pool.QueryRow(context.Background(), `
SELECT request_checksum, retention_checksum, audit_binding_checksum
FROM ProtectedHistoryArtifact
WHERE id = $1
`, artifactID).Scan(
		&checksums.requestChecksum,
		&checksums.retentionChecksum,
		&checksums.auditBindingChecksum,
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return checksums
}
