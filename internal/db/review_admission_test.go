package db

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/reviewadmission"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestReviewAdmissionMigrationAndTaskGateAreFailClosed(t *testing.T) {
	g := NewWithT(t)
	migration, err := os.ReadFile("../migrations/sql/165_review_admission_decisions.up.sql")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(migration)
	for _, required := range []string{
		"CREATE TABLE ReviewAdmissionDecision", "decision IN ('GO', 'NO_GO')",
		"review_material_checksum", "observed_state_checksum",
		"supersedes_decision_id", "revokes_decision_id",
		"ReviewAdmissionDecision_append_only", "ReviewAdmissionDecision_no_truncate",
	} {
		g.Expect(text).To(ContainSubstring(required))
	}

	repositorySource, err := os.ReadFile("review_admission.go")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(repositorySource)).To(ContainSubstring("lockReviewAdmissionPlan"))

	taskSource, err := os.ReadFile("task_queue.go")
	g.Expect(err).NotTo(HaveOccurred())
	approvalGate := strings.Index(
		string(taskSource),
		"requireCurrentDeploymentPlanApprovalForExecution",
	)
	gate := strings.Index(string(taskSource), "requireCurrentReviewAdmissionGo")
	preflight := strings.Index(string(taskSource), "evaluateAndPersistDeploymentPreflight")
	insert := strings.Index(string(taskSource), "insertTasksForDeploymentPlan")
	g.Expect(approvalGate).To(BeNumerically(">", 0))
	g.Expect(approvalGate).To(BeNumerically("<", gate))
	g.Expect(approvalGate).To(BeNumerically("<", preflight))
	g.Expect(approvalGate).To(BeNumerically("<", insert))
	g.Expect(gate).To(BeNumerically(">", 0))
	g.Expect(gate).To(BeNumerically("<", preflight))
	g.Expect(gate).To(BeNumerically("<", insert))
}

func TestReviewDecisionReplayMatchesOnlyExactMaterial(t *testing.T) {
	g := NewWithT(t)
	expiresAt := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	request := types.CreateReviewAdmissionDecisionRequest{
		OrganizationID: uuid.New(), DeploymentPlanID: uuid.New(), ActorUserAccountID: uuid.New(),
		Decision: types.ReviewAdmissionDecisionGo, Reason: "reviewed", ExpiresAt: expiresAt,
		IdempotencyKey: "review-1",
	}
	snapshot := types.AdmissionPlanSnapshot{PlanRevision: 3, Plan: types.DeploymentPlan{
		ID: request.DeploymentPlanID, OrganizationID: request.OrganizationID,
		CanonicalChecksum: "sha256:" + strings.Repeat("a", 64),
	}}
	observed := "sha256:" + strings.Repeat("b", 64)
	material := reviewadmission.ReviewMaterialChecksum(snapshot.Plan.CanonicalChecksum, observed)
	record := types.ReviewAdmissionDecisionRecord{
		ID: uuid.New(), OrganizationID: request.OrganizationID, DeploymentPlanID: request.DeploymentPlanID,
		PlanRevision: snapshot.PlanRevision, PlanChecksum: snapshot.Plan.CanonicalChecksum,
		ReviewMaterialChecksum: material, ObservedStateChecksum: observed, Decision: request.Decision,
		Reason: request.Reason, ActorUserAccountID: request.ActorUserAccountID, ExpiresAt: expiresAt,
		AuthorizationEvidence: "sha256:" + strings.Repeat("c", 64), IdempotencyKey: request.IdempotencyKey,
	}
	record.CanonicalChecksum = reviewadmission.CanonicalChecksum(record)
	g.Expect(reviewDecisionMatchesRequest(record, request, snapshot, material, observed)).To(BeTrue())
	request.Decision = types.ReviewAdmissionDecisionNoGo
	g.Expect(reviewDecisionMatchesRequest(record, request, snapshot, material, observed)).To(BeFalse())
}

func TestReviewMaterialRequiresCurrentAdmitAndCurrentApproval(t *testing.T) {
	g := NewWithT(t)
	evaluatedAt := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	plan := types.DeploymentPlan{
		CanonicalChecksum:       "sha256:" + strings.Repeat("a", 64),
		EffectivePolicyChecksum: "sha256:" + strings.Repeat("b", 64),
	}
	evaluation := reviewAdmissionEvaluationMaterial{
		ID: uuid.New(), PlanRevision: 1, PlanChecksum: plan.CanonicalChecksum,
		EffectivePolicyChecksum: plan.EffectivePolicyChecksum,
		Decision:                types.AdmissionDecisionAdmit,
		EvaluatedAt:             evaluatedAt,
		MaterialChecksum:        "sha256:" + strings.Repeat("c", 64),
		DecisionChecksum:        "sha256:" + strings.Repeat("d", 64),
		ActorUserAccountID:      uuid.New(),
	}

	g.Expect(currentReviewAdmissionEvaluation(
		evaluation,
		plan,
		evaluatedAt,
		true,
	)).To(BeTrue())
	g.Expect(currentReviewAdmissionEvaluation(
		evaluation,
		plan,
		evaluatedAt.Add(time.Second),
		true,
	)).To(BeFalse())
	g.Expect(currentReviewAdmissionEvaluation(
		evaluation,
		plan,
		evaluatedAt,
		false,
	)).To(BeFalse())
	evaluation.Decision = types.AdmissionDecisionBlock
	g.Expect(currentReviewAdmissionEvaluation(
		evaluation,
		plan,
		evaluatedAt,
		true,
	)).To(BeFalse())
}
