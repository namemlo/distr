package db

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCampaignAuditInputsPreserveTypedDefinitionAndRuntimeLineage(t *testing.T) {
	g := NewWithT(t)
	lineage := campaignAuditLineage{
		OrganizationID: uuid.New(), DraftID: uuid.New(), RevisionID: uuid.New(),
		RunID: uuid.New(), WaveID: uuid.New(), WaveRunID: uuid.New(),
		MemberID: uuid.New(), MemberRunID: uuid.New(), PlanID: uuid.New(),
		AdmissionDecisionID: uuid.New(),
		RevisionChecksum:    "sha256:" + strings.Repeat("a", 64),
		PlanChecksum:        "sha256:" + strings.Repeat("b", 64),
		AdmissionChecksum:   "sha256:" + strings.Repeat("c", 64),
	}

	runInput := campaignRunAuditInput(lineage, "campaign.run.started", nil)
	g.Expect(runInput.CampaignDraftID).To(HaveValue(Equal(lineage.DraftID)))
	g.Expect(runInput.CampaignRevisionID).To(HaveValue(Equal(lineage.RevisionID)))
	g.Expect(runInput.CampaignRunID).To(HaveValue(Equal(lineage.RunID)))
	g.Expect(runInput.CampaignRevisionChecksum).To(Equal(lineage.RevisionChecksum))

	memberInput := campaignMemberAuditInput(lineage, "campaign.member.admitted", nil)
	g.Expect(memberInput.CampaignWaveDefinitionID).To(HaveValue(Equal(lineage.WaveID)))
	g.Expect(memberInput.CampaignWaveRunID).To(HaveValue(Equal(lineage.WaveRunID)))
	g.Expect(memberInput.CampaignMemberID).To(HaveValue(Equal(lineage.MemberID)))
	g.Expect(memberInput.CampaignMemberRunID).To(HaveValue(Equal(lineage.MemberRunID)))
	g.Expect(memberInput.DeploymentPlanID).To(HaveValue(Equal(lineage.PlanID)))
	g.Expect(memberInput.AdmissionDecisionID).To(HaveValue(Equal(lineage.AdmissionDecisionID)))
	g.Expect(validateControlPlaneAuditEventInput(memberInput)).To(Succeed())
}

func TestCampaignAuditHookIsInjectableAndPropagatesFailure(t *testing.T) {
	g := NewWithT(t)
	want := errors.New("append campaign audit")
	ctx := WithCampaignControlPlaneAuditHook(context.Background(), ControlPlaneAuditAppendHookFunc(
		func(context.Context, types.ControlPlaneAuditEventInput) error { return want },
	))
	draftID := uuid.New()
	err := recordCampaignAuditMutation(ctx, types.ControlPlaneAuditEventInput{
		OrganizationID: uuid.New(), EventType: "campaign.draft.created",
		Outcome: "SUCCEEDED", CampaignDraftID: &draftID,
	})
	g.Expect(err).To(MatchError(want))
}

func TestCampaignControlAuditEventDistinguishesRequestedAndAppliedState(t *testing.T) {
	g := NewWithT(t)
	g.Expect(campaignControlAuditEventType(
		types.CampaignControlKindPause, types.CampaignControlStatusPendingSafePoint,
	)).To(Equal("campaign.run.pause_requested"))
	g.Expect(campaignControlAuditEventType(
		types.CampaignControlKindPause, types.CampaignControlStatusApplied,
	)).To(Equal("campaign.run.paused"))
	g.Expect(campaignControlAuditEventType(
		types.CampaignControlKindCancel, types.CampaignControlStatusPendingReconciliation,
	)).To(Equal("campaign.run.cancel_requested"))
	g.Expect(campaignControlAuditEventType(
		types.CampaignControlKindCancel, types.CampaignControlStatusApplied,
	)).To(Equal("campaign.run.canceled"))
}

func TestCampaignMutationsAppendAuditBeforeOwningTransactionCommit(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("deployment_campaigns.go")
	g.Expect(err).NotTo(HaveOccurred())
	code := string(source)

	for _, boundary := range []struct {
		start string
		end   string
		event string
	}{
		{"func CreateDeploymentCampaignDraft(", "func GetDeploymentCampaignDraft(", "campaign.draft.created"},
		{"func UpdateDeploymentCampaignDraft(", "func ValidateStoredDeploymentCampaignDraft(", "campaign.draft.updated"},
		{"func PublishCampaignRevision(", "func replayCampaignPublicationConflict(", "campaign.revision.published"},
		{"func (CampaignRepository) StartCampaignRun(", "func (CampaignRepository) TransitionCampaignRun(", "campaign.run.started"},
		{"func (CampaignRepository) ApplyCampaignControl(", "func (CampaignRepository) ExcludeCampaignMember(", "recordCampaignAuditMutation"},
		{"func (CampaignRepository) ExcludeCampaignMember(", "func TransitionCampaign(", "campaign.member.excluded"},
	} {
		start := strings.Index(code, boundary.start)
		end := strings.Index(code[start:], boundary.end)
		g.Expect(start).To(BeNumerically(">=", 0), boundary.start)
		g.Expect(end).To(BeNumerically(">", 0), boundary.end)
		body := code[start : start+end]
		g.Expect(body).To(ContainSubstring(boundary.event), boundary.start)
		if commit := strings.LastIndex(body, "Commit(ctx)"); commit >= 0 {
			g.Expect(strings.Index(body, boundary.event)).To(BeNumerically("<", commit), boundary.start)
		}
	}
}

func TestCampaignControlReplayReturnsBeforeAuditAppend(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("deployment_campaigns.go")
	g.Expect(err).NotTo(HaveOccurred())
	code := string(source)
	start := strings.Index(code, "func (CampaignRepository) ApplyCampaignControl(")
	end := strings.Index(code[start:], "func (CampaignRepository) ExcludeCampaignMember(")
	g.Expect(start).To(BeNumerically(">=", 0))
	g.Expect(end).To(BeNumerically(">", 0))
	body := code[start : start+end]
	replayReturn := strings.Index(body, "return existing, nil")
	auditAppend := strings.LastIndex(body, "recordCampaignAuditMutation")
	g.Expect(replayReturn).To(BeNumerically(">=", 0))
	g.Expect(auditAppend).To(BeNumerically(">", replayReturn))
}

func TestCampaignExecutionProjectionAuditUsesImmutableAttemptCorrelation(t *testing.T) {
	g := NewWithT(t)
	lineage := campaignAuditLineage{
		OrganizationID: uuid.New(), DraftID: uuid.New(), RevisionID: uuid.New(),
		RunID: uuid.New(), WaveID: uuid.New(), WaveRunID: uuid.New(),
		MemberID: uuid.New(), MemberRunID: uuid.New(), PlanID: uuid.New(),
		AdmissionDecisionID: uuid.New(),
	}
	executionID := uuid.New()
	attempt := types.ExecutionAttempt{
		ID: uuid.New(), OrganizationID: lineage.OrganizationID, TaskID: uuid.New(),
		StepRunID: uuid.New(), DeploymentTargetID: uuid.New(),
		Identity: types.ExecutionIdentity{ExecutionID: executionID, AttemptNumber: 1, StepKey: "deploy"},
		Status:   types.ExecutionAttemptStatusFailed,
	}

	first := campaignExecutionProjectionAuditInput(lineage, attempt, "campaign.execution.terminal", "FAILED")
	replay := campaignExecutionProjectionAuditInput(lineage, attempt, "campaign.execution.terminal", "FAILED")
	g.Expect(first.ExecutionAttemptID).To(HaveValue(Equal(attempt.ID)))
	g.Expect(first.ExecutionID).To(HaveValue(Equal(executionID)))
	g.Expect(replay.ExecutionAttemptID).To(Equal(first.ExecutionAttemptID))

	retry := attempt
	retry.ID = uuid.New()
	retry.Identity.AttemptNumber = 2
	second := campaignExecutionProjectionAuditInput(lineage, retry, "campaign.execution.terminal", "FAILED")
	g.Expect(second.ExecutionAttemptID).To(HaveValue(Equal(retry.ID)))
	g.Expect(second.ExecutionAttemptID).NotTo(Equal(first.ExecutionAttemptID))
}
