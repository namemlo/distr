package migrations

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/protectedhistory"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	. "github.com/onsi/gomega"
)

func TestMigration170UpAndCompatibleDown(t *testing.T) {
	g := NewWithT(t)
	database := newMigrationTestDatabase(t)
	database.migrateTo(t, 170)

	var tableExists, auditColumnExists bool
	g.Expect(database.pool.QueryRow(context.Background(), `
SELECT
  to_regclass('protectedhistoryartifact') IS NOT NULL,
  EXISTS (
    SELECT 1
    FROM information_schema.columns
    WHERE table_schema = current_schema()
      AND table_name = 'controlplaneauditevent'
      AND column_name = 'protected_history_artifact_id'
  )
`).Scan(&tableExists, &auditColumnExists)).To(Succeed())
	g.Expect(tableExists).To(BeTrue())
	g.Expect(auditColumnExists).To(BeTrue())

	database.migrateTo(t, 169)
	g.Expect(database.pool.QueryRow(context.Background(), `
SELECT
  to_regclass('protectedhistoryartifact') IS NOT NULL,
  EXISTS (
    SELECT 1
    FROM information_schema.columns
    WHERE table_schema = current_schema()
      AND table_name = 'controlplaneauditevent'
      AND column_name = 'protected_history_artifact_id'
  )
`).Scan(&tableExists, &auditColumnExists)).To(Succeed())
	g.Expect(tableExists).To(BeFalse())
	g.Expect(auditColumnExists).To(BeFalse())
}

func TestMigration170UpOnPostgreSQL18(t *testing.T) {
	g := NewWithT(t)
	database := newMigrationTestDatabase(t)
	ctx := context.Background()
	var serverVersion int
	g.Expect(database.pool.QueryRow(
		ctx,
		"SELECT current_setting('server_version_num')::INTEGER",
	).Scan(&serverVersion)).To(Succeed())
	if serverVersion < 180000 || serverVersion >= 190000 {
		t.Skipf("requires PostgreSQL 18, found server_version_num=%d", serverVersion)
	}

	database.migrateTo(t, 169)
	var pgcryptoInstalledBefore bool
	g.Expect(database.pool.QueryRow(ctx, `
SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pgcrypto')
`).Scan(&pgcryptoInstalledBefore)).To(Succeed())

	organizationID := uuid.New()
	auditEventID := uuid.New()
	actorID := uuid.New()
	deploymentPlanID := uuid.New()
	createdAt := time.Date(2026, time.September, 1, 1, 2, 3, 456789000, time.UTC)
	payload := `{"deploymentPlanId":"` + deploymentPlanID.String() +
		`","result":"preserved"}`
	_, err := database.pool.Exec(ctx, `
INSERT INTO Organization (id, name) VALUES ($1, $2)
`, organizationID, "Migration 170 preservation "+uuid.NewString())
	g.Expect(err).NotTo(HaveOccurred())
	_, err = database.pool.Exec(ctx, `
INSERT INTO ControlPlaneAuditEvent (
  id, organization_id, sequence, event_type, actor_id, outcome,
  deployment_plan_id, deployment_plan_checksum, artifact_digest,
  payload, payload_redacted, payload_truncated, created_at
) VALUES (
  $2, $1, 7, 'deployment.completed', $3, 'SUCCEEDED',
  $4, 'sha256:' || repeat('1', 64), 'sha256:' || repeat('2', 64),
  $5::JSONB, true, false, $6
)
`,
		organizationID,
		auditEventID,
		actorID,
		deploymentPlanID,
		payload,
		createdAt,
	)
	g.Expect(err).NotTo(HaveOccurred())

	type auditEvidence struct {
		ID                     uuid.UUID
		OrganizationID         uuid.UUID
		Sequence               int64
		EventType              string
		ActorID                uuid.UUID
		Outcome                string
		DeploymentPlanID       uuid.UUID
		DeploymentPlanChecksum string
		ArtifactDigest         string
		Payload                string
		PayloadRedacted        bool
		PayloadTruncated       bool
		CreatedAt              time.Time
	}
	readAuditEvidence := func() auditEvidence {
		var evidence auditEvidence
		g.Expect(database.pool.QueryRow(ctx, `
SELECT id, organization_id, sequence, event_type, actor_id, outcome,
       deployment_plan_id, deployment_plan_checksum, artifact_digest,
       payload::TEXT, payload_redacted, payload_truncated, created_at
FROM ControlPlaneAuditEvent
WHERE id = $1
`, auditEventID).Scan(
			&evidence.ID,
			&evidence.OrganizationID,
			&evidence.Sequence,
			&evidence.EventType,
			&evidence.ActorID,
			&evidence.Outcome,
			&evidence.DeploymentPlanID,
			&evidence.DeploymentPlanChecksum,
			&evidence.ArtifactDigest,
			&evidence.Payload,
			&evidence.PayloadRedacted,
			&evidence.PayloadTruncated,
			&evidence.CreatedAt,
		)).To(Succeed())
		return evidence
	}

	before := readAuditEvidence()
	var beforeCount int
	g.Expect(database.pool.QueryRow(ctx, `
SELECT count(*) FROM ControlPlaneAuditEvent
`).Scan(&beforeCount)).To(Succeed())

	database.migrateTo(t, 170)

	after := readAuditEvidence()
	var afterCount int
	g.Expect(database.pool.QueryRow(ctx, `
SELECT count(*) FROM ControlPlaneAuditEvent
`).Scan(&afterCount)).To(Succeed())
	g.Expect(afterCount).To(Equal(beforeCount))
	g.Expect(after).To(Equal(before))

	var protectedHistoryArtifactID *uuid.UUID
	g.Expect(database.pool.QueryRow(ctx, `
SELECT protected_history_artifact_id
FROM ControlPlaneAuditEvent
WHERE id = $1
`, auditEventID).Scan(&protectedHistoryArtifactID)).To(Succeed())
	g.Expect(protectedHistoryArtifactID).To(BeNil())

	var pgcryptoInstalledAfter, sha256Available bool
	g.Expect(database.pool.QueryRow(ctx, `
SELECT
  EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pgcrypto'),
  to_regprocedure('sha256(bytea)') IS NOT NULL
`).Scan(&pgcryptoInstalledAfter, &sha256Available)).To(Succeed())
	g.Expect(pgcryptoInstalledAfter).To(Equal(pgcryptoInstalledBefore))
	g.Expect(sha256Available).To(BeTrue())
}

func TestMigration170RetainedRowsAreAppendOnlyAndRefuseDown(t *testing.T) {
	g := NewWithT(t)
	database := newMigrationTestDatabase(t)
	database.migrateTo(t, 170)
	retained := insertMigration170ProtectedHistoryFixture(t, database)

	for name, statement := range map[string]string{
		"update":   `UPDATE ProtectedHistoryArtifact SET media_type = media_type WHERE id = $1`,
		"delete":   `DELETE FROM ProtectedHistoryArtifact WHERE id = $1`,
		"truncate": `TRUNCATE ProtectedHistoryArtifact CASCADE`,
	} {
		t.Run(name, func(t *testing.T) {
			args := []any{retained.ID}
			if name == "truncate" {
				args = nil
			}
			_, err := database.pool.Exec(context.Background(), statement, args...)
			NewWithT(t).Expect(err).To(MatchError(ContainSubstring(
				"protected-history artifact records are append-only",
			)))
		})
	}

	err := database.runner.Migrate(169)
	g.Expect(err).To(MatchError(ContainSubstring(
		"refusing migration 170 rollback while protected-history artifacts exist",
	)))
	var count int
	g.Expect(database.pool.QueryRow(
		context.Background(),
		`SELECT count(*) FROM ProtectedHistoryArtifact WHERE id = $1`,
		retained.ID,
	).Scan(&count)).To(Succeed())
	g.Expect(count).To(Equal(1))
}

func insertMigration170ProtectedHistoryFixture(
	t *testing.T,
	database *migrationTestDatabase,
) protectedhistory.RetainedArtifact {
	t.Helper()
	g := NewWithT(t)
	ctx := context.Background()
	organizationID := uuid.New()
	issuerID := uuid.New()
	reviewerID := uuid.New()
	customerID := uuid.New()
	_, err := database.pool.Exec(ctx, `
INSERT INTO Organization (id, name) VALUES ($1, $2)
`, organizationID, "Protected History "+uuid.NewString())
	g.Expect(err).NotTo(HaveOccurred())
	_, err = database.pool.Exec(ctx, `
INSERT INTO UserAccount (id, email) VALUES ($1, $2), ($3, $4)
`,
		issuerID,
		"issuer-"+uuid.NewString()+"@example.com",
		reviewerID,
		"reviewer-"+uuid.NewString()+"@example.com",
	)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = database.pool.Exec(ctx, `
INSERT INTO Organization_UserAccount (
  organization_id, user_account_id, user_role
) VALUES ($1, $2, 'admin'), ($1, $3, 'admin')
`, organizationID, issuerID, reviewerID)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = database.pool.Exec(ctx, `
INSERT INTO CustomerOrganization (id, organization_id, name)
VALUES ($1, $2, $3)
`,
		customerID,
		organizationID,
		"Customer "+uuid.NewString(),
	)
	g.Expect(err).NotTo(HaveOccurred())

	scope := protectedhistory.Scope{
		OrganizationID:          organizationID.String(),
		CustomerOrganizationIDs: []string{customerID.String()},
	}
	artifact, err := protectedhistory.Build(scope, 170, []protectedhistory.RawRecord{{
		Kind: "customerorganization",
		ID:   customerID.String(),
		Payload: json.RawMessage(`{"organizationId":"` + organizationID.String() +
			`","id":"` + customerID.String() + `"}`),
	}})
	g.Expect(err).NotTo(HaveOccurred())
	payload, err := protectedhistory.Marshal(*artifact)
	g.Expect(err).NotTo(HaveOccurred())
	identity := protectedhistory.ObjectIdentityForPayload("protected-history", payload)
	capturedAt := time.Date(2026, time.September, 1, 7, 8, 9, 123456000, time.UTC)
	retained, err := protectedhistory.BuildRetention(protectedhistory.RetentionInput{
		ID:                    uuid.New(),
		Artifact:              *artifact,
		ObjectReference:       identity.Reference,
		MediaType:             identity.MediaType,
		ByteLength:            identity.ByteLength,
		ContentChecksum:       identity.Checksum,
		CapturedAt:            capturedAt,
		IssuerUserAccountID:   issuerID,
		ReviewerUserAccountID: reviewerID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	retained.IdempotencyKey = "migration-170-fixture"
	retained.RequestChecksum, err = protectedhistory.RetentionRequestChecksum(
		protectedhistory.CreateRetentionRequest{
			OrganizationID:        organizationID,
			Scope:                 scope,
			IssuerUserAccountID:   issuerID,
			ReviewerUserAccountID: reviewerID,
			IdempotencyKey:        retained.IdempotencyKey,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	auditEventID := uuid.New()
	g.Expect(protectedhistory.BindRetentionAudit(retained, auditEventID, 1)).To(Succeed())
	auditPayload, err := json.Marshal(map[string]any{
		"retentionChecksum":     retained.RetentionChecksum,
		"requestChecksum":       retained.RequestChecksum,
		"artifactId":            retained.ArtifactID,
		"recordsRoot":           retained.RecordsRoot,
		"objectReference":       retained.ObjectReference,
		"mediaType":             retained.MediaType,
		"byteLength":            retained.ByteLength,
		"contentChecksum":       retained.ContentChecksum,
		"capturedAt":            retained.CapturedAt.Format(time.RFC3339Nano),
		"issuerUserAccountId":   retained.IssuerUserAccountID.String(),
		"reviewerUserAccountId": retained.ReviewerUserAccountID.String(),
	})
	g.Expect(err).NotTo(HaveOccurred())

	tx, err := database.pool.BeginTx(ctx, pgx.TxOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `
INSERT INTO ProtectedHistoryArtifact (
  id, created_at, organization_id, schema, source_schema_version,
  customer_organization_ids, deployment_target_ids,
  artifact_id, records_root, record_count,
  object_reference, media_type, byte_length, content_checksum, captured_at,
  issuer_useraccount_id, reviewer_useraccount_id, retention_checksum,
  audit_event_id, audit_event_sequence, audit_binding_checksum,
  idempotency_key, request_checksum
) VALUES (
  $1, $2, $3, $4, $5,
  $6, $7,
  $8, $9, $10,
  $11, $12, $13, $14, $15,
  $16, $17, $18,
  $19, $20, $21,
  $22, $23
)`,
		retained.ID,
		capturedAt.Add(time.Second),
		retained.OrganizationID,
		retained.Schema,
		retained.SourceSchemaVersion,
		[]uuid.UUID{customerID},
		[]uuid.UUID{},
		retained.ArtifactID,
		retained.RecordsRoot,
		retained.RecordCount,
		retained.ObjectReference,
		retained.MediaType,
		retained.ByteLength,
		retained.ContentChecksum,
		retained.CapturedAt,
		retained.IssuerUserAccountID,
		retained.ReviewerUserAccountID,
		retained.RetentionChecksum,
		retained.AuditEventID,
		retained.AuditEventSequence,
		retained.AuditBindingChecksum,
		retained.IdempotencyKey,
		retained.RequestChecksum,
	)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = tx.Exec(ctx, `
INSERT INTO ControlPlaneAuditEvent (
  id, organization_id, sequence, event_type, actor_id, outcome,
  protected_history_artifact_id, artifact_digest, payload
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
`,
		retained.AuditEventID,
		retained.OrganizationID,
		retained.AuditEventSequence,
		"protected_history.retained",
		retained.IssuerUserAccountID,
		"SUCCEEDED",
		retained.ID,
		retained.ContentChecksum,
		auditPayload,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(tx.Commit(ctx)).To(Succeed())

	var checksum string
	g.Expect(database.pool.QueryRow(ctx, `
SELECT audit_binding_checksum
FROM ProtectedHistoryArtifact
WHERE id = $1
`, retained.ID).Scan(&checksum)).To(Succeed())
	g.Expect(checksum).To(Equal(retained.AuditBindingChecksum))
	return *retained
}
