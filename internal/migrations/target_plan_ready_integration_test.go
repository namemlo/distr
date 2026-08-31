package migrations

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	. "github.com/onsi/gomega"
)

type targetPlanReadyFixture struct {
	organizationID        uuid.UUID
	userAccountID         uuid.UUID
	applicationID         uuid.UUID
	channelID             uuid.UUID
	environmentID         uuid.UUID
	releaseBundleID       uuid.UUID
	deploymentTargetID    uuid.UUID
	deploymentUnitID      uuid.UUID
	targetConfigID        uuid.UUID
	deploymentPlanDraftID uuid.UUID
}

func TestMigration163AllowsReadySealExecuteAndRefusesUnsafeDown(t *testing.T) {
	g := NewWithT(t)
	database := newMigrationTestDatabase(t)
	database.migrateTo(t, 162)
	fixture := createTargetPlanReadyFixture(t, database)
	database.migrateTo(t, 163)

	_, err := database.pool.Exec(context.Background(), targetPlanInsertSQL, pgx.NamedArgs{
		"planID": uuid.New(), "organizationID": fixture.organizationID,
		"userAccountID": fixture.userAccountID, "releaseBundleID": fixture.releaseBundleID,
		"applicationID": fixture.applicationID, "channelID": fixture.channelID,
		"environmentID": fixture.environmentID, "draftID": fixture.deploymentPlanDraftID,
		"deploymentUnitID": fixture.deploymentUnitID, "targetConfigID": fixture.targetConfigID,
		"status": "READY", "payload": []byte(`{}`), "sealed": true,
	})
	g.Expect(err).To(MatchError(ContainSubstring(
		"target deployment plan must be inserted unsealed",
	)))

	planID := insertAndSealTargetPlan(t, database, fixture, "READY", false)
	var status string
	var sealed bool
	var policyEvidenceMatches bool
	err = database.pool.QueryRow(context.Background(), `
SELECT status,
       sealed_at IS NOT NULL,
       effective_policy->>'checksum' = effective_policy_checksum
         AND effective_policy->>'subscriberSetChecksum' = subscriber_set_checksum
FROM DeploymentPlan
WHERE id = $1 AND organization_id = $2`, planID, fixture.organizationID).Scan(
		&status,
		&sealed,
		&policyEvidenceMatches,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(status).To(Equal("READY"))
	g.Expect(sealed).To(BeTrue())
	g.Expect(policyEvidenceMatches).To(BeTrue())

	_, err = database.pool.Exec(context.Background(), `
UPDATE DeploymentPlan
SET status = 'BLOCKED'
WHERE id = $1 AND organization_id = $2`, planID, fixture.organizationID)
	g.Expect(err).To(MatchError(ContainSubstring("published target deployment plan is immutable")))

	_, err = database.pool.Exec(context.Background(), `
UPDATE DeploymentPlan
SET status = 'EXECUTED'
WHERE id = $1 AND organization_id = $2`, planID, fixture.organizationID)
	g.Expect(err).NotTo(HaveOccurred())

	err = database.runner.Migrate(162)
	g.Expect(err).To(MatchError(ContainSubstring(
		"refusing migration 163 rollback: executable target deployment plans exist",
	)))
}

func TestMigration163RoundTripPreservesBlockedPlanAndRestoresShape(t *testing.T) {
	g := NewWithT(t)
	database := newMigrationTestDatabase(t)
	database.migrateTo(t, 162)
	fixture := createTargetPlanReadyFixture(t, database)
	planID := insertAndSealTargetPlan(t, database, fixture, "BLOCKED", true)

	database.migrateTo(t, 163)
	database.migrateTo(t, 162)

	var status string
	err := database.pool.QueryRow(context.Background(), `
SELECT status
FROM DeploymentPlan
WHERE id = $1 AND organization_id = $2`, planID, fixture.organizationID).Scan(&status)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(status).To(Equal("BLOCKED"))

	_, err = database.pool.Exec(context.Background(), targetPlanInsertSQL, pgx.NamedArgs{
		"planID": uuid.New(), "organizationID": fixture.organizationID,
		"userAccountID": fixture.userAccountID, "releaseBundleID": fixture.releaseBundleID,
		"applicationID": fixture.applicationID, "channelID": fixture.channelID,
		"environmentID": fixture.environmentID, "draftID": uuid.New(),
		"deploymentUnitID": fixture.deploymentUnitID, "targetConfigID": fixture.targetConfigID,
		"status": "READY", "payload": []byte(`{}`), "sealed": false,
	})
	g.Expect(err).To(MatchError(ContainSubstring("deploymentplan_v2_shape_check")))
}

func createTargetPlanReadyFixture(
	t *testing.T,
	database *migrationTestDatabase,
) targetPlanReadyFixture {
	t.Helper()
	g := NewWithT(t)
	fixture := targetPlanReadyFixture{
		organizationID:        uuid.New(),
		userAccountID:         uuid.New(),
		applicationID:         uuid.New(),
		channelID:             uuid.New(),
		environmentID:         uuid.New(),
		releaseBundleID:       uuid.New(),
		deploymentTargetID:    uuid.New(),
		deploymentUnitID:      uuid.New(),
		targetConfigID:        uuid.New(),
		deploymentPlanDraftID: uuid.New(),
	}
	lifecycleID := uuid.New()
	deploymentScopeID := uuid.New()
	environmentAssignmentID := uuid.New()
	configPayload := []byte(`{"schema":"distr.target-config/v1"}`)
	releasePayload := []byte(`{}`)
	_, err := database.pool.Exec(context.Background(), `
INSERT INTO Organization (id, name) VALUES (@organizationID, 'Migration 163 test');
INSERT INTO UserAccount (id, email, name)
VALUES (@userAccountID, @email, 'Migration 163 reviewer');
INSERT INTO Organization_UserAccount (organization_id, user_account_id, user_role)
VALUES (@organizationID, @userAccountID, 'admin');
INSERT INTO Application (id, name, type, organization_id)
VALUES (@applicationID, 'Migration 163 app', 'docker', @organizationID);
INSERT INTO Environment (id, organization_id, name)
VALUES (@environmentID, @organizationID, 'Migration 163 environment');
INSERT INTO Lifecycle (id, organization_id, name)
VALUES (@lifecycleID, @organizationID, 'Migration 163 lifecycle');
INSERT INTO Channel (id, organization_id, application_id, lifecycle_id, name, is_default)
VALUES (@channelID, @organizationID, @applicationID, @lifecycleID, 'Migration 163 channel', true);
INSERT INTO ReleaseBundle (
  id, organization_id, application_id, channel_id, release_number,
  status, canonical_checksum, canonical_payload
) VALUES (
  @releaseBundleID, @organizationID, @applicationID, @channelID, 'migration-163',
  'DRAFT', 'sha256:' || encode(sha256(@releasePayload), 'hex'), @releasePayload
);
INSERT INTO DeploymentTarget (id, name, type, organization_id, agent_version_id)
SELECT @deploymentTargetID, 'Migration 163 target', 'docker', @organizationID, id
FROM AgentVersion
ORDER BY created_at, id
LIMIT 1;
INSERT INTO DeploymentScope (
  id, organization_id, key, name, delivery_model, management_state
) VALUES (
  @deploymentScopeID, @organizationID, 'migration-163', 'Migration 163 scope',
  'external', 'managed'
);
INSERT INTO TargetEnvironmentAssignment (
  id, organization_id, deployment_target_id, environment_id, active_from
) VALUES (
  @environmentAssignmentID, @organizationID, @deploymentTargetID, @environmentID, now()
);
INSERT INTO DeploymentUnit (
  id, organization_id, deployment_scope_id, target_environment_assignment_id,
  deployment_target_id, key, name, physical_identity, management_state,
  subscriber_set_checksum, subscriber_set_sealed_at
) VALUES (
  @deploymentUnitID, @organizationID, @deploymentScopeID, @environmentAssignmentID,
  @deploymentTargetID, 'migration-163', 'Migration 163 unit', 'migration-163-unit',
  'managed', deployment_unit_subscriber_set_checksum(
    @organizationID, @deploymentUnitID
  ), now()
);
INSERT INTO TargetConfigSnapshot (
  id, organization_id, created_by_user_account_id, deployment_unit_id,
  target_environment_assignment_id, environment_id, source_repository,
  source_commit, source_adapter, adapter_version, target_platform, schema,
  canonical_payload, canonical_checksum
) VALUES (
  @targetConfigID, @organizationID, @userAccountID, @deploymentUnitID,
  @environmentAssignmentID, @environmentID, 'https://example.invalid/config.git',
  repeat('b', 40), 'migration-test', '1.0.0', 'linux/amd64',
  'distr.target-config/v1', @configPayload,
  'sha256:' || encode(sha256(@configPayload), 'hex')
);
INSERT INTO DeploymentPlanDraft (
  id, organization_id, created_by_user_account_id, updated_by_user_account_id,
  product_release_id, deployment_unit_id, environment_assignment_id,
  target_config_snapshot_id, protocol_version
) VALUES (
  @draftID, @organizationID, @userAccountID, @userAccountID,
  @releaseBundleID, @deploymentUnitID, @environmentAssignmentID,
  @targetConfigID, 'v2'
)`, pgx.QueryExecModeSimpleProtocol, pgx.NamedArgs{
		"organizationID": fixture.organizationID, "userAccountID": fixture.userAccountID,
		"email":         "migration-163-" + uuid.NewString() + "@example.invalid",
		"applicationID": fixture.applicationID, "environmentID": fixture.environmentID,
		"lifecycleID": lifecycleID, "channelID": fixture.channelID,
		"releaseBundleID": fixture.releaseBundleID, "releasePayload": releasePayload,
		"deploymentTargetID": fixture.deploymentTargetID, "deploymentScopeID": deploymentScopeID,
		"environmentAssignmentID": environmentAssignmentID,
		"deploymentUnitID":        fixture.deploymentUnitID, "targetConfigID": fixture.targetConfigID,
		"configPayload": configPayload, "draftID": fixture.deploymentPlanDraftID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	return fixture
}

const targetPlanInsertSQL = `
INSERT INTO DeploymentPlan (
  id, organization_id, published_by_user_account_id, release_bundle_id,
  application_id, channel_id, environment_id, plan_schema, draft_id,
  deployment_unit_id, target_config_snapshot_id, protocol_version, status,
  canonical_checksum, canonical_payload, effective_policy,
  effective_policy_checksum, subscriber_set_checksum, sealed_at
) VALUES (
  @planID, @organizationID, @userAccountID, @releaseBundleID,
  @applicationID, @channelID, @environmentID, 'distr.target-deployment-plan/v2',
  @draftID, @deploymentUnitID, @targetConfigID, 'v2', @status,
  'sha256:' || encode(sha256(@payload), 'hex'), @payload,
  jsonb_build_object(
    'checksum', 'sha256:' || repeat('c', 64),
    'subscriberSetChecksum', deployment_unit_subscriber_set_checksum(
      @organizationID, @deploymentUnitID
    )
  ),
  'sha256:' || repeat('c', 64),
  deployment_unit_subscriber_set_checksum(@organizationID, @deploymentUnitID),
  CASE WHEN @sealed THEN now() END
)`

func insertAndSealTargetPlan(
	t *testing.T,
	database *migrationTestDatabase,
	fixture targetPlanReadyFixture,
	status string,
	includeExecutionBlocker bool,
) uuid.UUID {
	t.Helper()
	g := NewWithT(t)
	ctx := context.Background()
	planID := uuid.New()
	planTargetID := uuid.New()
	transaction, err := database.pool.Begin(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	defer func() {
		_ = transaction.Rollback(context.Background())
	}()

	_, err = transaction.Exec(ctx, targetPlanInsertSQL, pgx.NamedArgs{
		"planID": planID, "organizationID": fixture.organizationID,
		"userAccountID": fixture.userAccountID, "releaseBundleID": fixture.releaseBundleID,
		"applicationID": fixture.applicationID, "channelID": fixture.channelID,
		"environmentID": fixture.environmentID, "draftID": fixture.deploymentPlanDraftID,
		"deploymentUnitID": fixture.deploymentUnitID, "targetConfigID": fixture.targetConfigID,
		"status": status, "payload": []byte(`{}`), "sealed": false,
	})
	g.Expect(err).NotTo(HaveOccurred())
	_, err = transaction.Exec(ctx, `
INSERT INTO DeploymentPlanTarget (
  id, deployment_plan_id, organization_id, deployment_target_id, name, type, platform
) VALUES (
  @targetID, @planID, @organizationID, @deploymentTargetID,
  'Migration 163 target', 'docker', 'linux/amd64'
);
INSERT INTO DeploymentPlanStep (
  deployment_plan_id, organization_id, step_key, name, action_type,
  action_name, execution_location, sort_order
) VALUES (
  @planID, @organizationID, 'deploy', 'Deploy', 'deploy',
  'compose.deploy', 'hub', 0
);
INSERT INTO DeploymentPlanDraftAuditEvent (
  deployment_plan_draft_id, organization_id, revision, event_type,
  actor_user_account_id, published_deployment_plan_id, event_payload, event_checksum
) VALUES (
  @draftID, @organizationID, 1, 'PUBLISHED', @userAccountID, @planID,
  convert_to('{}', 'UTF8'),
  'sha256:' || encode(sha256(convert_to('{}', 'UTF8')), 'hex')
)`, pgx.QueryExecModeSimpleProtocol, pgx.NamedArgs{
		"targetID": planTargetID, "planID": planID,
		"organizationID":     fixture.organizationID,
		"deploymentTargetID": fixture.deploymentTargetID,
		"draftID":            fixture.deploymentPlanDraftID,
		"userAccountID":      fixture.userAccountID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	if includeExecutionBlocker {
		_, err = transaction.Exec(ctx, `
INSERT INTO DeploymentPlanIssue (
  deployment_plan_id, organization_id, severity, code, message
) VALUES (
  $1, $2, 'blocker', 'target_plan_execution_deferred',
  'Target plan execution is not enabled'
)`, planID, fixture.organizationID)
		g.Expect(err).NotTo(HaveOccurred())
	}
	_, err = transaction.Exec(ctx, `
UPDATE DeploymentPlan
SET sealed_at = now()
WHERE id = $1 AND organization_id = $2`, planID, fixture.organizationID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(transaction.Commit(ctx)).To(Succeed())
	return planID
}
