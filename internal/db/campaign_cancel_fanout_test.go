package db

import (
	"os"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCampaignCancelFanoutUsesExactTenantRunTaskLineage(t *testing.T) {
	g := NewWithT(t)

	for _, required := range []string{
		"lineage.organization_id = @organization_id",
		"lineage.campaign_run_id = @campaign_run_id",
		"attempt.task_id = lineage.task_id",
		"attempt.organization_id = lineage.organization_id",
		"attempt.status IN ('PENDING', 'CLAIMED', 'RUNNING')",
		"ORDER BY lineage.task_id, attempt.attempt_number, attempt.id",
		"FOR UPDATE OF attempt",
	} {
		g.Expect(loadCampaignCancelAttemptsSQL).To(ContainSubstring(required))
	}
	g.Expect(loadCampaignCancelAttemptsSQL).NotTo(ContainSubstring(
		"attempt.deployment_plan_id",
	))
	g.Expect(loadCampaignCancelTasksSQL).To(ContainSubstring("FOR UPDATE OF task"))
	g.Expect(loadCampaignCancelTasksSQL).To(ContainSubstring(
		"lineage.campaign_run_id = @campaign_run_id",
	))
}

func TestCampaignCancelRequestIdentityIsDeterministicAndAttemptScoped(t *testing.T) {
	g := NewWithT(t)
	requestID := uuid.New()
	runID := uuid.New()
	attemptID := uuid.New()

	first := campaignCancelIdempotencyKey(runID, requestID, attemptID)
	second := campaignCancelIdempotencyKey(runID, requestID, attemptID)

	g.Expect(second).To(Equal(first))
	g.Expect(first).To(ContainSubstring(runID.String()))
	g.Expect(first).To(ContainSubstring(requestID.String()))
	g.Expect(first).To(ContainSubstring(attemptID.String()))
	g.Expect(campaignCancelIdempotencyKey(runID, requestID, uuid.New())).NotTo(Equal(first))
}

func TestCampaignCancelFanoutRunsOnlyForAppliedTerminalCancel(t *testing.T) {
	g := NewWithT(t)
	input := types.CampaignControlInput{Kind: types.CampaignControlKindCancel}
	decision := types.CampaignControlDecision{
		Status: types.CampaignControlStatusApplied,
		Run:    types.CampaignRun{State: types.CampaignRunStateCanceled},
	}

	g.Expect(shouldFanoutCampaignCancel(input, decision)).To(BeTrue())
	decision.Status = types.CampaignControlStatusPendingReconciliation
	g.Expect(shouldFanoutCampaignCancel(input, decision)).To(BeFalse())
	decision.Status = types.CampaignControlStatusApplied
	decision.Run.State = types.CampaignRunStateRunning
	g.Expect(shouldFanoutCampaignCancel(input, decision)).To(BeFalse())
	input.Kind = types.CampaignControlKindPause
	decision.Run.State = types.CampaignRunStateCanceled
	g.Expect(shouldFanoutCampaignCancel(input, decision)).To(BeFalse())
}

func TestApplyCampaignControlFansOutBeforePersistingControl(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("deployment_campaigns.go")
	g.Expect(err).NotTo(HaveOccurred())
	applyStart := strings.Index(string(source), "func (CampaignRepository) ApplyCampaignControl(")
	g.Expect(applyStart).To(BeNumerically(">=", 0))
	applySource := string(source[applyStart:])
	applyEnd := strings.Index(applySource, "func (CampaignRepository) ExcludeCampaignMember(")
	g.Expect(applyEnd).To(BeNumerically(">", 0))
	applySource = applySource[:applyEnd]

	fanoutAt := strings.Index(applySource, "applyCampaignCancelFanout(txCtx, input)")
	controlWriteAt := strings.Index(applySource, "insertCampaignControlSQL")
	g.Expect(fanoutAt).To(BeNumerically(">=", 0))
	g.Expect(controlWriteAt).To(BeNumerically(">", fanoutAt))
}

func TestCampaignCancelRequestUsesTrustedDatabaseTimestamp(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("campaign_cancel_fanout.go")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(source)).To(ContainSubstring(
		"@requested_by, @idempotency_key, @reason, clock_timestamp()",
	))
}
