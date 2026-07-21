package db

import (
	"context"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCampaignTaskCreationRequestUsesImmutableTenantEvidenceAndMemberOccurrence(t *testing.T) {
	g := NewWithT(t)
	authorizer := types.AdmissionAuthorizer(func(
		context.Context,
		types.AdmissionAuthorizationContext,
	) error {
		return nil
	})
	candidate := types.CampaignMemberCandidate{
		OrganizationID:     uuid.New(),
		ActorUserAccountID: uuid.New(),
		MemberRunID:        uuid.New(),
		PlanID:             uuid.New(),
		CampaignEvidence: types.AdmissionCampaignEvidence{
			ID: uuid.New(), Revision: 17, Checksum: "sha256:" + strings.Repeat("a", 64),
		},
	}
	admission := types.CampaignMemberAdmission{
		RunID: candidate.CampaignEvidence.ID, MemberRunID: candidate.MemberRunID,
		PlanID: candidate.PlanID,
	}

	request, err := campaignTaskCreationRequest(candidate, admission, authorizer)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(request.OrganizationID).To(Equal(candidate.OrganizationID))
	g.Expect(request.DeploymentPlanID).To(Equal(candidate.PlanID))
	g.Expect(request.ExecutionOccurrenceID).To(Equal(candidate.MemberRunID))
	g.Expect(request.ActorUserAccountID).To(Equal(candidate.ActorUserAccountID))
	g.Expect(request.SchedulerIdempotencyKey).To(Equal(
		"campaign:" + admission.RunID.String() + ":member:" + candidate.MemberRunID.String(),
	))
	g.Expect(request.Campaign).To(Equal(&candidate.CampaignEvidence))
	g.Expect(request.Authorize).NotTo(BeNil())
}

func TestCampaignTaskCreationRequestFailsClosedWithoutTrustedInputs(t *testing.T) {
	valid := types.CampaignMemberCandidate{
		OrganizationID:     uuid.New(),
		ActorUserAccountID: uuid.New(),
		MemberRunID:        uuid.New(),
		PlanID:             uuid.New(),
		CampaignEvidence: types.AdmissionCampaignEvidence{
			ID: uuid.New(), Revision: 1, Checksum: "sha256:" + strings.Repeat("b", 64),
		},
	}
	admission := types.CampaignMemberAdmission{
		RunID: uuid.New(), MemberRunID: valid.MemberRunID, PlanID: valid.PlanID,
	}
	authorizer := types.AdmissionAuthorizer(func(
		context.Context,
		types.AdmissionAuthorizationContext,
	) error {
		return nil
	})

	for _, mutate := range []func(*types.CampaignMemberCandidate){
		func(value *types.CampaignMemberCandidate) { value.OrganizationID = uuid.Nil },
		func(value *types.CampaignMemberCandidate) { value.ActorUserAccountID = uuid.Nil },
		func(value *types.CampaignMemberCandidate) { value.CampaignEvidence.ID = uuid.Nil },
		func(value *types.CampaignMemberCandidate) { value.CampaignEvidence.Revision = 0 },
		func(value *types.CampaignMemberCandidate) { value.CampaignEvidence.Checksum = "" },
	} {
		candidate := valid
		mutate(&candidate)
		_, err := campaignTaskCreationRequest(candidate, admission, authorizer)
		NewWithT(t).Expect(err).To(HaveOccurred())
	}
	_, err := campaignTaskCreationRequest(valid, admission, nil)
	NewWithT(t).Expect(err).To(HaveOccurred())
}

func TestCampaignTaskBindingsPreserveEveryExactTargetTuple(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	runID := uuid.New()
	memberRunID := uuid.New()
	planID := uuid.New()
	tasks := []types.Task{
		{ID: uuid.New(), OrganizationID: organizationID, DeploymentPlanID: planID, ExecutionOccurrenceID: memberRunID, DeploymentTargetID: uuid.New()},
		{ID: uuid.New(), OrganizationID: organizationID, DeploymentPlanID: planID, ExecutionOccurrenceID: memberRunID, DeploymentTargetID: uuid.New()},
	}

	bindings, err := campaignTaskBindings(types.CampaignMemberCandidate{
		OrganizationID: organizationID, MemberRunID: memberRunID, PlanID: planID,
	}, types.CampaignMemberAdmission{
		RunID: runID, MemberRunID: memberRunID, PlanID: planID,
	}, tasks)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(bindings).To(HaveLen(2))
	for index, binding := range bindings {
		g.Expect(binding.ID).NotTo(Equal(uuid.Nil))
		g.Expect(binding.OrganizationID).To(Equal(organizationID))
		g.Expect(binding.CampaignRunID).To(Equal(runID))
		g.Expect(binding.CampaignMemberRunID).To(Equal(memberRunID))
		g.Expect(binding.DeploymentPlanID).To(Equal(planID))
		g.Expect(binding.TaskID).To(Equal(tasks[index].ID))
		g.Expect(binding.DeploymentTargetID).To(Equal(tasks[index].DeploymentTargetID))
	}
}

func TestPendingCampaignDispatchQueryIsTenantLeaseAndAttemptFenced(t *testing.T) {
	g := NewWithT(t)
	for _, fragment := range []string{
		"run.organization_id = lineage.organization_id",
		"run.fencing_token = @fencing_token",
		"run.lease_expires_at > clock_timestamp()",
		"member_run.status IN ('ADMITTED', 'RUNNING')",
		"t.execution_occurrence_id = member_run.id",
		"NOT EXISTS",
		"FROM ExecutionAttempt",
	} {
		g.Expect(loadPendingCampaignDispatchTasksSQL).To(ContainSubstring(fragment))
	}
}

func TestPendingCampaignDispatchQueryRecoversTasksWithAReadyUnattemptedStep(t *testing.T) {
	g := NewWithT(t)
	for _, fragment := range []string{
		"FROM StepRun AS step_run",
		"step_run.task_id = t.id",
		"step_run.status = 'PENDING'",
		"attempt.step_run_id = step_run.id",
		"FROM unnest(plan_step.dependencies)",
		"dependency_run.status NOT IN ('SUCCEEDED', 'SKIPPED')",
	} {
		g.Expect(loadPendingCampaignDispatchTasksSQL).To(ContainSubstring(fragment))
	}
	g.Expect(loadPendingCampaignDispatchTasksSQL).NotTo(ContainSubstring(
		"attempt.task_id = t.id\n  )",
	))
}

func TestCampaignRunInstantiationPersistsExactStarter(t *testing.T) {
	g := NewWithT(t)
	g.Expect(instantiateCampaignRunSQL).To(ContainSubstring("started_by_useraccount_id"))
	g.Expect(instantiateCampaignRunSQL).To(ContainSubstring("@actor_id"))
}
