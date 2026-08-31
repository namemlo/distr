package migrations

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	. "github.com/onsi/gomega"
)

func TestMigration166SerializesTaskReassignmentWithBaselineAdoption(t *testing.T) {
	g := NewWithT(t)
	database := newMigrationTestDatabase(t)
	database.migrateTo(t, 162)
	fixture := createTargetPlanReadyFixture(t, database)
	database.migrateTo(t, 166)

	targetPlanID := insertAndSealTargetPlan(t, database, fixture, "READY", false)
	var targetPlanTargetID uuid.UUID
	g.Expect(database.pool.QueryRow(context.Background(), `
SELECT id
FROM DeploymentPlanTarget
WHERE deployment_plan_id = $1 AND organization_id = $2`,
		targetPlanID,
		fixture.organizationID,
	).Scan(&targetPlanTargetID)).To(Succeed())

	sourcePlanID := uuid.New()
	sourcePlanTargetID := uuid.New()
	taskID := uuid.New()
	_, err := database.pool.Exec(
		context.Background(),
		`
INSERT INTO DeploymentPlan (
  id, organization_id, release_bundle_id, application_id, channel_id,
  environment_id, status, canonical_checksum, canonical_payload
) VALUES (
  @sourcePlanID, @organizationID, @releaseBundleID, @applicationID, @channelID,
  @environmentID, 'EXECUTED', 'sha256:' || repeat('d', 64), convert_to('{}', 'UTF8')
);
INSERT INTO DeploymentPlanTarget (
  id, deployment_plan_id, organization_id, deployment_target_id, name, type, platform
) VALUES (
  @sourcePlanTargetID, @sourcePlanID, @organizationID, @deploymentTargetID,
  'Migration 166 source target', 'docker', 'linux/amd64'
);
INSERT INTO Task (
  id, organization_id, task_type, deployment_plan_id, execution_occurrence_id,
  deployment_plan_target_id, deployment_target_id, application_id,
  release_bundle_id, channel_id, environment_id, status, protocol_version
) VALUES (
  @taskID, @organizationID, 'deployment', @sourcePlanID, @sourcePlanID,
  @sourcePlanTargetID, @deploymentTargetID, @applicationID,
  @releaseBundleID, @channelID, @environmentID, 'QUEUED', 'v1'
)`,
		pgx.QueryExecModeSimpleProtocol,
		pgx.NamedArgs{
			"sourcePlanID": sourcePlanID, "organizationID": fixture.organizationID,
			"releaseBundleID": fixture.releaseBundleID, "applicationID": fixture.applicationID,
			"channelID": fixture.channelID, "environmentID": fixture.environmentID,
			"sourcePlanTargetID": sourcePlanTargetID,
			"deploymentTargetID": fixture.deploymentTargetID, "taskID": taskID,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())

	_, err = database.pool.Exec(
		context.Background(),
		`ALTER TABLE BaselineAdoption DISABLE TRIGGER BaselineAdoption_commit_guard`,
	)
	g.Expect(err).NotTo(HaveOccurred())

	adoptionTx, err := database.pool.Begin(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	defer func() { _ = adoptionTx.Rollback(context.Background()) }()
	_, err = adoptionTx.Exec(context.Background(), `
INSERT INTO BaselineAdoption (
  id, organization_id, deployment_plan_id, product_release_id,
  target_config_snapshot_id, deployment_unit_id, environment_id,
  deployment_target_id, actor_user_account_id, authorization_action,
  idempotency_key, reason, plan_checksum, product_release_checksum,
  target_config_checksum, request_payload, request_checksum,
  outcome_checksum, status
) VALUES (
  @id, @organizationID, @deploymentPlanID, @productReleaseID,
  @targetConfigID, @deploymentUnitID, @environmentID,
  @deploymentTargetID, @actorID, 'plan.execute',
  'migration-166-race', 'Concurrent reassignment regression',
  'sha256:' || repeat('a', 64), 'sha256:' || repeat('b', 64),
  'sha256:' || repeat('c', 64), convert_to('{}', 'UTF8'),
  'sha256:' || encode(sha256(convert_to('{}', 'UTF8')), 'hex'),
  'sha256:' || repeat('e', 64), 'ADOPTED'
)`, pgx.NamedArgs{
		"id": uuid.New(), "organizationID": fixture.organizationID,
		"deploymentPlanID": targetPlanID, "productReleaseID": fixture.releaseBundleID,
		"targetConfigID": fixture.targetConfigID, "deploymentUnitID": fixture.deploymentUnitID,
		"environmentID": fixture.environmentID, "deploymentTargetID": fixture.deploymentTargetID,
		"actorID": fixture.userAccountID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	reassignmentConnection, err := database.pool.Acquire(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	defer reassignmentConnection.Release()
	var reassignmentBackendPID int32
	g.Expect(reassignmentConnection.QueryRow(
		context.Background(),
		`SELECT pg_backend_pid()`,
	).Scan(&reassignmentBackendPID)).To(Succeed())

	reassignmentResult := make(chan error, 1)
	go func() {
		_, execErr := reassignmentConnection.Exec(context.Background(), `
UPDATE Task
SET deployment_plan_id = @targetPlanID,
    execution_occurrence_id = @targetPlanID,
    deployment_plan_target_id = @targetPlanTargetID,
    protocol_version = 'v2'
WHERE id = @taskID AND organization_id = @organizationID`, pgx.NamedArgs{
			"targetPlanID": targetPlanID, "targetPlanTargetID": targetPlanTargetID,
			"taskID": taskID, "organizationID": fixture.organizationID,
		})
		reassignmentResult <- execErr
	}()

	waitForMigrationBackendLock(t, database, reassignmentBackendPID)
	g.Expect(adoptionTx.Commit(context.Background())).To(Succeed())

	var reassignmentErr error
	select {
	case reassignmentErr = <-reassignmentResult:
	case <-time.After(5 * time.Second):
		t.Fatal("task reassignment did not finish after baseline adoption committed")
	}
	var pgErr *pgconn.PgError
	g.Expect(errors.As(reassignmentErr, &pgErr)).To(BeTrue())
	g.Expect(pgErr.Code).To(Equal(pgerrcode.CheckViolation))
	g.Expect(pgErr.Message).To(ContainSubstring(
		"baseline adoption cannot coexist with deployment tasks or executions",
	))

	var sourceTasks, targetTasks int
	g.Expect(database.pool.QueryRow(context.Background(), `
SELECT
  count(*) FILTER (WHERE deployment_plan_id = $1),
  count(*) FILTER (WHERE deployment_plan_id = $2)
FROM Task
WHERE id = $3 AND organization_id = $4`,
		sourcePlanID,
		targetPlanID,
		taskID,
		fixture.organizationID,
	).Scan(&sourceTasks, &targetTasks)).To(Succeed())
	g.Expect(sourceTasks).To(Equal(1))
	g.Expect(targetTasks).To(Equal(0))
}

func TestMigration169RefusesDownAfterSeparatedBaselineFacts(t *testing.T) {
	g := NewWithT(t)
	database := newMigrationTestDatabase(t)
	database.migrateTo(t, 169)

	adoptionID, auditEventID := insertMigration169BaselineFixture(
		t,
		database,
		"1.2.3",
		"customer-schema-42",
		"sha256:"+strings.Repeat("a", 64),
		"sha256:"+strings.Repeat("b", 64),
	)
	before := readMigration169ChecksumEvidence(
		t, database, adoptionID, auditEventID,
	)

	err := database.runner.Migrate(168)
	g.Expect(err).To(MatchError(ContainSubstring(
		"refusing migration 169 rollback: separated baseline adoption facts exist",
	)))
	g.Expect(readMigration169ChecksumEvidence(
		t, database, adoptionID, auditEventID,
	)).To(Equal(before))

	var applicationVersionColumn, v2Guard bool
	g.Expect(database.pool.QueryRow(context.Background(), `
SELECT
  EXISTS (
    SELECT 1
    FROM information_schema.columns
    WHERE table_schema = current_schema()
      AND table_name = 'baselineadoptioncomponent'
      AND column_name = 'application_version'
  ),
  to_regprocedure('baseline_adoption_commit_guard_v2()') IS NOT NULL
`).Scan(&applicationVersionColumn, &v2Guard)).To(Succeed())
	g.Expect(applicationVersionColumn).To(BeTrue())
	g.Expect(v2Guard).To(BeTrue())
}

func TestMigration169CompatibleDownPreservesChecksumEvidence(t *testing.T) {
	g := NewWithT(t)
	database := newMigrationTestDatabase(t)
	database.migrateTo(t, 169)

	legacyVersion := "1.2.3"
	legacyChecksum := "sha256:" + strings.Repeat("a", 64)
	adoptionID, auditEventID := insertMigration169BaselineFixture(
		t,
		database,
		legacyVersion,
		legacyVersion,
		legacyChecksum,
		legacyChecksum,
	)
	before := readMigration169ChecksumEvidence(
		t, database, adoptionID, auditEventID,
	)

	database.migrateTo(t, 168)
	g.Expect(readMigration169ChecksumEvidence(
		t, database, adoptionID, auditEventID,
	)).To(Equal(before))

	var applicationVersionColumn bool
	g.Expect(database.pool.QueryRow(context.Background(), `
SELECT EXISTS (
  SELECT 1
  FROM information_schema.columns
  WHERE table_schema = current_schema()
    AND table_name = 'baselineadoptioncomponent'
    AND column_name = 'application_version'
)`).Scan(&applicationVersionColumn)).To(Succeed())
	g.Expect(applicationVersionColumn).To(BeFalse())
}

type migration169ChecksumEvidence struct {
	requestChecksum      string
	outcomeChecksum      string
	auditPlanChecksum    string
	auditProductChecksum string
	auditConfigChecksum  string
	auditOutcomeChecksum string
}

func insertMigration169BaselineFixture(
	t *testing.T,
	database *migrationTestDatabase,
	applicationVersion string,
	schemaVersion string,
	releaseChecksum string,
	capabilityChecksum string,
) (uuid.UUID, uuid.UUID) {
	t.Helper()
	g := NewWithT(t)
	organizationID := uuid.New()
	adoptionID := uuid.New()
	auditEventID := uuid.New()
	deploymentPlanID := uuid.New()
	productReleaseID := uuid.New()
	targetConfigID := uuid.New()
	requestPayload := []byte(`{"schema":"distr.baseline-adoption-request/v1"}`)
	outcomeChecksum := "sha256:" + strings.Repeat("e", 64)

	_, err := database.pool.Exec(context.Background(), `
INSERT INTO Organization (id, name)
VALUES (@organizationID, 'Migration 169 checksum fixture');

DO $$
DECLARE item RECORD;
BEGIN
  FOR item IN
    SELECT relation.relname, constraint_row.conname
    FROM pg_constraint constraint_row
    JOIN pg_class relation ON relation.oid = constraint_row.conrelid
    WHERE relation.relnamespace = to_regnamespace(current_schema())
      AND lower(relation.relname) IN (
        'baselineadoption', 'baselineadoptioncomponent'
      )
      AND constraint_row.contype = 'f'
  LOOP
    EXECUTE format(
      'ALTER TABLE %I DROP CONSTRAINT %I',
      item.relname,
      item.conname
    );
  END LOOP;
END
$$;

ALTER TABLE BaselineAdoption
  DISABLE TRIGGER BaselineAdoption_insert_guard;
ALTER TABLE BaselineAdoption
  DISABLE TRIGGER BaselineAdoption_commit_guard;

INSERT INTO BaselineAdoption (
  id, organization_id, deployment_plan_id, product_release_id,
  target_config_snapshot_id, deployment_unit_id, environment_id,
  deployment_target_id, actor_user_account_id, authorization_action,
  idempotency_key, reason, plan_checksum, product_release_checksum,
  target_config_checksum, request_payload, request_checksum,
  outcome_checksum, status
) VALUES (
  @adoptionID, @organizationID, @deploymentPlanID, @productReleaseID,
  @targetConfigID, @deploymentUnitID, @environmentID,
  @deploymentTargetID, @actorID, 'plan.execute',
  'migration-169-checksum', 'Migration 169 downgrade fixture',
  'sha256:' || repeat('1', 64), 'sha256:' || repeat('2', 64),
  'sha256:' || repeat('3', 64), @requestPayload,
  'sha256:' || encode(sha256(@requestPayload), 'hex'),
  @outcomeChecksum, 'ADOPTED'
);

INSERT INTO BaselineAdoptionComponent (
  id, organization_id, baseline_adoption_id, deployment_plan_id,
  deployment_unit_id, component_instance_id, component_key,
  component_release_id, component_release_checksum, application_version,
  source_commit, build_id, provenance_verification_id,
  provenance_evidence_digest, provenance_policy_checksum, artifact_digest,
  platform, target_config_snapshot_id, config_checksum, schema_version,
  capability_checksum, topology_checksum, observation_id, observer_id,
  observation_evidence_checksum, observation_evidence_reference,
  observation_state_checksum, observation_runtime_state_checksum,
  health_evidence_kind, health_evidence_use, health_policy_checksum,
  observation_captured_at, observation_fresh_until,
  active_desired_revision_id, desired_revision
) VALUES (
  @componentID, @organizationID, @adoptionID, @deploymentPlanID,
  @deploymentUnitID, @componentInstanceID, 'customer-api',
  @componentReleaseID, @releaseChecksum, @applicationVersion,
  repeat('4', 40), 'migration-169-build', @provenanceVerificationID,
  'sha256:' || repeat('5', 64), 'sha256:' || repeat('6', 64),
  'sha256:' || repeat('7', 64), 'linux/amd64', @targetConfigID,
  'sha256:' || repeat('3', 64), @schemaVersion, @capabilityChecksum,
  'sha256:' || repeat('8', 64), @observationID, @observerID,
  'sha256:' || repeat('9', 64), 'evidence://sha256/' || repeat('9', 64),
  'sha256:' || repeat('a', 64), 'sha256:' || repeat('b', 64),
  'STANDARD_READINESS', 'STANDARD_PROMOTION_ELIGIBLE',
  'sha256:' || repeat('c', 64), now(), now() + interval '10 minutes',
  @activeDesiredRevisionID, 1
);

INSERT INTO ControlPlaneAuditEvent (
  id, organization_id, sequence, event_type, outcome,
  deployment_plan_id, deployment_plan_checksum,
  product_release_checksum, target_config_checksum, payload
) VALUES (
  @auditEventID, @organizationID, 1, 'baseline_adoption.adopted', 'ADOPTED',
  @deploymentPlanID, 'sha256:' || repeat('1', 64),
  'sha256:' || repeat('2', 64), 'sha256:' || repeat('3', 64),
  jsonb_build_object(
    'baselineAdoptionId', @adoptionID::TEXT,
    'outcomeChecksum', @outcomeChecksum,
    'outcome', 'ADOPTED'
  )
);

ALTER TABLE BaselineAdoption
  ENABLE TRIGGER BaselineAdoption_insert_guard;
ALTER TABLE BaselineAdoption
  ENABLE TRIGGER BaselineAdoption_commit_guard;
`, pgx.QueryExecModeSimpleProtocol, pgx.NamedArgs{
		"organizationID": organizationID, "adoptionID": adoptionID,
		"auditEventID": auditEventID, "deploymentPlanID": deploymentPlanID,
		"productReleaseID": productReleaseID, "targetConfigID": targetConfigID,
		"deploymentUnitID": uuid.New(), "environmentID": uuid.New(),
		"deploymentTargetID": uuid.New(), "actorID": uuid.New(),
		"requestPayload": requestPayload, "outcomeChecksum": outcomeChecksum,
		"componentID": uuid.New(), "componentInstanceID": uuid.New(),
		"componentReleaseID": uuid.New(), "releaseChecksum": releaseChecksum,
		"applicationVersion":       applicationVersion,
		"provenanceVerificationID": uuid.New(), "schemaVersion": schemaVersion,
		"capabilityChecksum": capabilityChecksum,
		"observationID":      uuid.New(), "observerID": uuid.New(),
		"activeDesiredRevisionID": uuid.New(),
	})
	g.Expect(err).NotTo(HaveOccurred())
	return adoptionID, auditEventID
}

func readMigration169ChecksumEvidence(
	t *testing.T,
	database *migrationTestDatabase,
	adoptionID uuid.UUID,
	auditEventID uuid.UUID,
) migration169ChecksumEvidence {
	t.Helper()
	var evidence migration169ChecksumEvidence
	err := database.pool.QueryRow(context.Background(), `
SELECT adoption.request_checksum,
       adoption.outcome_checksum,
       event.deployment_plan_checksum,
       event.product_release_checksum,
       event.target_config_checksum,
       event.payload ->> 'outcomeChecksum'
FROM BaselineAdoption adoption
JOIN ControlPlaneAuditEvent event ON event.id = $2
WHERE adoption.id = $1`, adoptionID, auditEventID).Scan(
		&evidence.requestChecksum,
		&evidence.outcomeChecksum,
		&evidence.auditPlanChecksum,
		&evidence.auditProductChecksum,
		&evidence.auditConfigChecksum,
		&evidence.auditOutcomeChecksum,
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return evidence
}

func waitForMigrationBackendLock(
	t *testing.T,
	database *migrationTestDatabase,
	backendPID int32,
) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		err := database.pool.QueryRow(context.Background(), `
SELECT COALESCE(
  (SELECT wait_event_type = 'Lock' FROM pg_stat_activity WHERE pid = $1),
  false
)`, backendPID).Scan(&waiting)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		if waiting {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("concurrent task reassignment did not wait on the adoption plan lock")
}
