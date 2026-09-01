package db

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	. "github.com/onsi/gomega"
)

func TestOperatorExecutionListSQLIsTenantScopedStableAndAttemptGranular(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	combined := operatorExecutionListSQL + operatorExecutionDetailSQL
	for _, required := range []string{
		"FROM ExecutionAttempt AS attempt",
		"JOIN Task AS task",
		"task.id = attempt.task_id",
		"task.organization_id = attempt.organization_id",
		"attempt.organization_id = @organizationID",
		"attempt.status = @status",
		"task.deployment_plan_id = @deploymentPlanID",
		"attempt.deployment_target_id = @deploymentTargetID",
		"attempt.created_at >= @fromTime",
		"attempt.created_at < @toTime",
		"(attempt.created_at, attempt.id) < (@cursorCreatedAt, @cursorID)",
		"ORDER BY attempt.created_at DESC, attempt.id DESC",
		"LIMIT @limitPlusOne",
	} {
		g.Expect(combined).To(ContainSubstring(required))
	}

	// A retry is a separate attempt/evidence chain even when execution_id and
	// step_key are unchanged. The operator list must never collapse attempts.
	g.Expect(operatorExecutionListSQL).NotTo(ContainSubstring("DISTINCT ON (attempt.execution_id"))
	g.Expect(operatorExecutionListSQL).NotTo(ContainSubstring("GROUP BY attempt.execution_id"))
}

func TestOperatorExecutionListSQLAppliesAuthorizationScopeBeforePagination(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	scopeAt := strings.Index(operatorExecutionListSQL, "@organizationWide")
	orderAt := strings.Index(operatorExecutionListSQL, "ORDER BY attempt.created_at DESC")
	limitAt := strings.Index(operatorExecutionListSQL, "LIMIT @limitPlusOne")

	g.Expect(scopeAt).To(BeNumerically(">", 0))
	g.Expect(orderAt).To(BeNumerically(">", scopeAt))
	g.Expect(limitAt).To(BeNumerically(">", orderAt))
	for _, required := range []string{
		"task.environment_id = ANY(@environmentIDs::uuid[])",
		"scoped_desired.organization_id = attempt.organization_id",
		"scoped_desired.execution_attempt_id = attempt.id",
		"scoped_unit.id = ANY(@deploymentUnitIDs::uuid[])",
		"scoped_component.id = ANY(@componentIDs::uuid[])",
		"campaign.campaign_id = ANY(@campaignIDs::uuid[])",
		"scoped_scope.customer_organization_id = ANY(@customerIDs::uuid[])",
		"scoped_subscriber.customer_organization_id = ANY(@customerIDs::uuid[])",
	} {
		g.Expect(operatorExecutionListSQL).To(ContainSubstring(required))
	}
}

func TestOperatorExecutionListSQLIncludesControlObservationAndPreviousStateEvidence(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	combined := operatorExecutionListSQL + operatorExecutionDetailSQL
	for _, required := range []string{
		"ExecutionIntent",
		"ExecutionRuntimeEvidence",
		"DeploymentPlanStepAdapter",
		"ExecutionCancelRequest",
		"ExecutionStatusQuery",
		"ExecutionReconciliationEvent",
		"ObservedComponentState",
		"PendingDesiredRevision",
		"previous_state_source_plan_id",
		"evidence_checksum",
		"evidence_reference",
	} {
		g.Expect(combined).To(ContainSubstring(required))
	}

	// Operator reads expose immutable evidence identities and checksums, never
	// signed payload bytes, signatures, or secret-provider references.
	for _, forbidden := range []string{
		"intent.payload",
		"intent.signature",
		"reconciliation.evidence_payload",
		"reconciliation.evidence_signature",
		"signing_key_reference",
	} {
		g.Expect(combined).NotTo(ContainSubstring(forbidden))
	}
}

func TestOperatorExecutionDetailSQLScopesEveryEvidenceBranchToTenantAndExecution(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	for _, required := range []string{
		"candidate.organization_id = @organizationID",
		"candidate.id = @executionID",
		"control.organization_id = attempt.organization_id",
		"control.execution_id = attempt.execution_id",
		"runtime.organization_id = attempt.organization_id",
		"runtime.execution_id = attempt.execution_id",
		"runtime.execution_attempt_id = attempt.id",
		"observed.organization_id = desired.organization_id",
		"desired.execution_id = attempt.execution_id",
		"ORDER BY retry.attempt_number, retry.created_at, retry.id",
		"ORDER BY event.event_sequence, event.id",
		"JOIN ExecutionFence AS fence",
		"'fenceGeneration', fence.generation",
		"'fenceResourceKey', fence.resource_key",
		"'idempotencyKey', external.idempotency_key",
		"'keyId', intent.key_id",
		"'planChecksum', retry.plan_checksum",
		"'artifactDigest', retry.artifact_digest",
		"'configChecksum', retry.config_checksum",
		"FROM TaskResourceLock AS lockrow",
		"FROM TaskLease AS lease",
		"'currentConflict'",
		"'releaseReason'",
		"'fenceLeaseExpiresAt'",
		"'fenceReleasedAt'",
		"'zeroLockClosure'",
	} {
		g.Expect(operatorExecutionDetailSQL).To(ContainSubstring(required))
	}
}

func TestOperatorExecutionQueriesAreConstantCount(t *testing.T) {
	t.Parallel()

	// The adapter executes one bounded list statement and one bounded detail
	// statement. Nested collections are aggregated in SQL, never queried per
	// execution, task, step, attempt, or evidence row.
	g := NewWithT(t)
	g.Expect(operatorExecutionListQueryCount).To(Equal(1))
	g.Expect(operatorExecutionDetailQueryCount).To(Equal(1))
}

func TestOperatorExecutionRepositoryScopeValidationFailsClosed(t *testing.T) {
	t.Parallel()

	base := types.OperatorScopeFilter{
		OrganizationID: uuid.New(), DecisionAt: time.Now().UTC(), OrganizationWide: true,
		CustomerIDs: []uuid.UUID{}, EnvironmentIDs: []uuid.UUID{},
		DeploymentUnitIDs: []uuid.UUID{}, ComponentIDs: []uuid.UUID{}, CampaignIDs: []uuid.UUID{},
	}
	g := NewWithT(t)
	g.Expect(validateOperatorExecutionRepositoryInput(base)).To(Succeed())

	withNarrowScope := base
	withNarrowScope.CustomerIDs = []uuid.UUID{uuid.New()}
	g.Expect(errors.Is(
		validateOperatorExecutionRepositoryInput(withNarrowScope),
		apierrors.ErrForbidden,
	)).To(BeTrue())

	invalidID := base
	invalidID.OrganizationWide = false
	invalidID.EnvironmentIDs = []uuid.UUID{uuid.Nil}
	g.Expect(errors.Is(
		validateOperatorExecutionRepositoryInput(invalidID),
		apierrors.ErrForbidden,
	)).To(BeTrue())

	nilSlice := base
	nilSlice.ComponentIDs = nil
	g.Expect(errors.Is(
		validateOperatorExecutionRepositoryInput(nilSlice),
		apierrors.ErrForbidden,
	)).To(BeTrue())

	firstID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	secondID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	unsorted := base
	unsorted.OrganizationWide = false
	unsorted.EnvironmentIDs = []uuid.UUID{secondID, firstID}
	g.Expect(errors.Is(
		validateOperatorExecutionRepositoryInput(unsorted),
		apierrors.ErrForbidden,
	)).To(BeTrue())
}

func TestOperatorExecutionDetailReadsPersistedFenceIntentIdempotencyAndRetries(t *testing.T) {
	ctx, _ := deploymentRegistryIsolatedPool(t, 159)
	g := NewWithT(t)
	organizationID, applicationID, channelID := createOperatorReleaseDependencies(t, ctx)
	environmentID := createDeploymentRegistryEnvironment(t, ctx, organizationID)
	targetID := createDeploymentRegistryTarget(t, ctx, organizationID)
	releaseID := insertOperatorReleaseFixture(
		t, ctx, organizationID, applicationID, channelID,
		types.ReleaseBundleKindComponent, "3.0.0", time.Now().UTC(),
	)
	planID, planTargetID, planStepID := uuid.New(), uuid.New(), uuid.New()
	taskID, stepRunID, externalID := uuid.New(), uuid.New(), uuid.New()
	firstAttemptID, secondAttemptID, intentID := uuid.New(), uuid.New(), uuid.New()
	planChecksum := "sha256:" + strings.Repeat("1", 64)
	artifactDigest := "sha256:" + strings.Repeat("2", 64)
	configChecksum := "sha256:" + strings.Repeat("3", 64)
	intentKeyID := "sha256:" + strings.Repeat("4", 64)
	payload := []byte(`{}`)
	now := time.Now().UTC()
	_, err := internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO DeploymentPlan (
		  id, organization_id, release_bundle_id, application_id, channel_id,
		  environment_id, status, canonical_checksum, canonical_payload
		) VALUES (
		  @planID, @organizationID, @releaseID, @applicationID, @channelID,
		  @environmentID, 'READY', @planChecksum, @payload
		);
		INSERT INTO DeploymentPlanTarget (
		  id, deployment_plan_id, organization_id, deployment_target_id, name, type
		) VALUES (@planTargetID, @planID, @organizationID, @targetID, 'execution target', 'docker');
		INSERT INTO DeploymentPlanStep (
		  id, deployment_plan_id, organization_id, step_key, name, action_type,
		  action_name, execution_location, sort_order
		) VALUES (
		  @planStepID, @planID, @organizationID, 'deploy', 'Deploy', 'deploy',
		  'deploy', 'target', 0
		);
		INSERT INTO Task (
		  id, organization_id, deployment_plan_id, deployment_plan_target_id,
		  deployment_target_id, application_id, release_bundle_id, channel_id,
		  environment_id, status
		) VALUES (
		  @taskID, @organizationID, @planID, @planTargetID,
		  @targetID, @applicationID, @releaseID, @channelID, @environmentID, 'RUNNING'
		);
		INSERT INTO StepRun (
		  id, organization_id, task_id, deployment_plan_id, deployment_plan_step_id,
		  step_key, name, action_type, status, sort_order
		) VALUES (
		  @stepRunID, @organizationID, @taskID, @planID, @planStepID,
		  'deploy', 'Deploy', 'deploy', 'RUNNING', 0
		);
		INSERT INTO ExternalExecution (
		  id, callback_deadline_at, organization_id, step_run_id, task_id,
		  deployment_plan_id, deployment_plan_target_id, deployment_target_id,
		  application_id, release_bundle_id, component, plan_checksum,
		  idempotency_key, expected_state_version, expected_version, expected_image,
		  expected_platform, expected_config_reference, expected_config_checksum, status
		) VALUES (
		  @externalID, @deadline, @organizationID, @stepRunID, @taskID,
		  @planID, @planTargetID, @targetID, @applicationID, @releaseID,
		  'worker', @planChecksum, 'external:worker:deploy', 1, '3.0.0',
		  'registry.example/worker@' || @artifactDigest, 'linux/amd64',
		  'config://worker', @configChecksum, 'RUNNING'
		);
		INSERT INTO ExecutionAttempt (
		  id, created_at, organization_id, deployment_target_id, task_id, step_run_id,
		  execution_id, attempt_number, step_key, status, plan_checksum,
		  artifact_digest, config_checksum, adapter_revision, intent_issued_at,
		  intent_expires_at, completed_at, cancellable, retry_safe, failure_reason
		) VALUES
		  (@firstAttemptID, @firstCreatedAt, @organizationID, @targetID, @taskID, @stepRunID,
		   @externalID, 1, 'deploy', 'FAILED', @planChecksum, @artifactDigest,
		   @configChecksum, 'adapter.compose@2', @issuedAt, @expiresAt, @firstCompletedAt, false, true,
		   'retryable provider timeout'),
		  (@secondAttemptID, @secondCreatedAt, @organizationID, @targetID, @taskID, @stepRunID,
		   @externalID, 2, 'deploy', 'RUNNING', @planChecksum, @artifactDigest,
		   @configChecksum, 'adapter.compose@2', @issuedAt, @expiresAt, NULL, true, true, '');
		INSERT INTO TaskResourceLock (
		  organization_id, task_id, resource_type, resource_key, concurrency_policy,
		  acquired_at, released_at
		) VALUES (
		  @organizationID, @taskID, 'deployment_target', @targetID::text, 'QUEUE',
		  @firstCreatedAt, NULL
		);
		INSERT INTO TaskLease (
		  organization_id, task_id, agent_id, executor_type, lease_token_hash,
		  leased_at, expires_at, heartbeat_at, attempt, released_at
		) VALUES
		  (@organizationID, @taskID, @targetID, 'AGENT', repeat('a', 64),
		   @firstCreatedAt, @firstCompletedAt, @firstCreatedAt, 1, @secondCreatedAt),
		  (@organizationID, @taskID, @targetID, 'AGENT', repeat('b', 64),
		   @secondCreatedAt, @deadline, @secondCreatedAt, 2, NULL);
		INSERT INTO ExecutionFence (
		  execution_attempt_id, organization_id, resource_key, generation, lease_expires_at, released_at
		) VALUES
		  (@firstAttemptID, @organizationID, 'target:' || @targetID::text, 41, @firstCompletedAt, @secondCreatedAt),
		  (@secondAttemptID, @organizationID, 'target:' || @targetID::text, 42, @deadline, NULL);
		INSERT INTO ExecutionIntent (
		  id, organization_id, execution_attempt_id, payload, checksum, key_id, signature
		) VALUES (
		  @intentID, @organizationID, @secondAttemptID, @payload,
		  'sha256:' || encode(sha256(@payload), 'hex'), @intentKeyID, repeat('s', 80)
		)`, pgx.NamedArgs{
		"organizationID": organizationID, "applicationID": applicationID,
		"channelID": channelID, "environmentID": environmentID, "targetID": targetID,
		"releaseID": releaseID, "planID": planID, "planTargetID": planTargetID,
		"planStepID": planStepID, "taskID": taskID, "stepRunID": stepRunID,
		"externalID": externalID, "firstAttemptID": firstAttemptID,
		"secondAttemptID": secondAttemptID, "intentID": intentID,
		"planChecksum": planChecksum, "artifactDigest": artifactDigest,
		"configChecksum": configChecksum, "intentKeyID": intentKeyID, "payload": payload,
		"deadline": now.Add(time.Hour), "issuedAt": now.Add(-time.Minute),
		"expiresAt": now.Add(time.Hour), "firstCreatedAt": now.Add(-time.Minute),
		"firstCompletedAt": now.Add(-30 * time.Second), "secondCreatedAt": now,
	})
	g.Expect(err).NotTo(HaveOccurred())

	detail, err := (OperatorExecutionRepository{}).GetOperatorExecution(
		ctx,
		types.OperatorScopeFilter{
			OrganizationID: organizationID, DecisionAt: now, OrganizationWide: true,
			CustomerIDs: []uuid.UUID{}, EnvironmentIDs: []uuid.UUID{},
			DeploymentUnitIDs: []uuid.UUID{}, ComponentIDs: []uuid.UUID{},
			CampaignIDs: []uuid.UUID{},
		},
		secondAttemptID,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(detail.Execution.FenceGeneration).To(Equal(int64(42)))
	g.Expect(detail.Execution.IdempotencyKey).To(Equal("external:worker:deploy"))
	g.Expect(detail.Intent).NotTo(BeNil())
	g.Expect(detail.Intent.KeyID).To(Equal(intentKeyID))
	g.Expect(detail.Attempts).To(HaveLen(2))
	g.Expect(detail.Attempts[0].FenceGeneration).To(Equal(int64(41)))
	g.Expect(detail.Attempts[1].FenceGeneration).To(Equal(int64(42)))
	g.Expect(detail.Attempts[1].IdempotencyKey).To(Equal("external:worker:deploy"))
	g.Expect(detail.Locks).To(HaveLen(1))
	g.Expect(detail.Locks[0].Status).To(Equal("ACQUIRED"))
	g.Expect(detail.Locks[0].CurrentConflict).To(BeFalse())
	g.Expect(detail.Leases).To(HaveLen(2))
	g.Expect(detail.Leases[0].Status).To(Equal("RELEASED"))
	g.Expect(detail.Leases[1].Status).To(Equal("ACTIVE"))
	g.Expect(detail.Coordination.InFlight).To(BeTrue())
	g.Expect(detail.Coordination.ActiveLockCount).To(Equal(1))
	g.Expect(detail.Coordination.ActiveLeaseCount).To(Equal(1))
	g.Expect(detail.Coordination.FenceStatus).To(Equal("ACTIVE"))
	g.Expect(detail.Coordination.ZeroLockClosure).To(BeFalse())

	closedAt := now.Add(30 * time.Second)
	_, err = internalctx.GetDb(ctx).Exec(ctx, `
		UPDATE ExecutionAttempt
		SET status = 'SUCCEEDED', completed_at = @closedAt, cancellable = false
		WHERE id = @attemptID AND organization_id = @organizationID;
		UPDATE Task
		SET status = 'SUCCEEDED', completed_at = @closedAt
		WHERE id = @taskID AND organization_id = @organizationID;
		UPDATE TaskResourceLock
		SET released_at = @closedAt
		WHERE task_id = @taskID AND organization_id = @organizationID;
		UPDATE TaskLease
		SET released_at = @closedAt
		WHERE task_id = @taskID AND organization_id = @organizationID AND released_at IS NULL;
		UPDATE ExecutionFence
		SET released_at = @closedAt
		WHERE execution_attempt_id = @attemptID AND organization_id = @organizationID`, pgx.NamedArgs{
		"closedAt": closedAt, "attemptID": secondAttemptID,
		"taskID": taskID, "organizationID": organizationID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	closed, err := (OperatorExecutionRepository{}).GetOperatorExecution(
		ctx,
		types.OperatorScopeFilter{
			OrganizationID: organizationID, DecisionAt: closedAt, OrganizationWide: true,
			CustomerIDs: []uuid.UUID{}, EnvironmentIDs: []uuid.UUID{},
			DeploymentUnitIDs: []uuid.UUID{}, ComponentIDs: []uuid.UUID{},
			CampaignIDs: []uuid.UUID{},
		},
		secondAttemptID,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(closed.Locks[0].Status).To(Equal("RELEASED"))
	g.Expect(closed.Locks[0].ReleaseReason).To(Equal("derived from terminal execution attempt: SUCCEEDED"))
	g.Expect(closed.Leases[1].Status).To(Equal("RELEASED"))
	g.Expect(closed.Coordination.FenceStatus).To(Equal("RELEASED"))
	g.Expect(closed.Coordination.UnreleasedLockCount).To(Equal(0))
	g.Expect(closed.Coordination.UnreleasedLeaseCount).To(Equal(0))
	g.Expect(closed.Coordination.ZeroLockClosure).To(BeTrue())
}
