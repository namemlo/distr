package migrations

import (
	"context"
	"errors"
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
