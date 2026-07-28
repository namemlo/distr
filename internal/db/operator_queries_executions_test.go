package db

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
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
		"observed.organization_id = desired.organization_id",
		"desired.execution_id = attempt.execution_id",
		"ORDER BY retry.attempt_number, retry.created_at, retry.id",
		"ORDER BY event.event_sequence, event.id",
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
