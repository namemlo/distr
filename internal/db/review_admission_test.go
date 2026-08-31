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

	reviewText := string(repositorySource)
	g.Expect(reviewText).NotTo(ContainSubstring("LIMIT 100"))
	for _, required := range []string{
		"baselineCount > 0", "b.authorizes_v2_execution", "head.active_revision_id = b.active_desired_revision_id",
		"head.pending_revision_id IS NULL", "NOT head.quarantined", "o.id = active.verified_observation_id",
		"o.is_current", "o.trusted", "o.disposition = 'ACCEPTED'", "o.health = 'HEALTHY'",
		"o.outcome = 'COMPLETE'", "o.fresh_until >= @now", "o.capability_checksum = active.capability_checksum",
	} {
		g.Expect(reviewText).To(ContainSubstring(required))
	}
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

func TestExactAdmissionBindingRejectsStaleOrMismatchedMaterial(t *testing.T) {
	g := NewWithT(t)
	approvalID := uuid.New()
	approvalRevision := int64(7)
	evaluation := reviewAdmissionEvaluationMaterial{
		ID: uuid.New(), PlanRevision: 1,
		PlanChecksum:            "sha256:" + strings.Repeat("a", 64),
		EffectivePolicyChecksum: "sha256:" + strings.Repeat("b", 64),
		Decision:                types.AdmissionDecisionAdmit,
		MaterialChecksum:        "sha256:" + strings.Repeat("c", 64),
		DecisionChecksum:        "sha256:" + strings.Repeat("d", 64),
		ApprovalRequestID:       &approvalID, ApprovalRequestRevision: &approvalRevision,
	}

	g.Expect(validateExactReviewAdmissionBinding(
		evaluation, evaluation.ID, evaluation.DecisionChecksum, approvalID, approvalRevision,
	)).To(Succeed())
	g.Expect(validateExactReviewAdmissionBinding(
		evaluation, uuid.New(), evaluation.DecisionChecksum, approvalID, approvalRevision,
	)).To(MatchError(ContainSubstring("exact admission")))
	g.Expect(validateExactReviewAdmissionBinding(
		evaluation, evaluation.ID, "sha256:"+strings.Repeat("e", 64), approvalID, approvalRevision,
	)).To(MatchError(ContainSubstring("exact admission")))
	g.Expect(validateExactReviewAdmissionBinding(
		evaluation, evaluation.ID, evaluation.DecisionChecksum, uuid.New(), approvalRevision,
	)).To(MatchError(ContainSubstring("current approval")))
}

func TestReviewAuthorizationEvidenceBindsAdmissionAndApproval(t *testing.T) {
	g := NewWithT(t)
	decisionAt := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	organizationID, actorID, planID := uuid.New(), uuid.New(), uuid.New()
	admissionID, approvalID := uuid.New(), uuid.New()

	first := reviewAuthorizationEvidence(
		organizationID, actorID, planID, decisionAt,
		admissionID, "sha256:"+strings.Repeat("a", 64), approvalID, 3,
	)
	second := reviewAuthorizationEvidence(
		organizationID, actorID, planID, decisionAt,
		uuid.New(), "sha256:"+strings.Repeat("a", 64), approvalID, 3,
	)
	third := reviewAuthorizationEvidence(
		organizationID, actorID, planID, decisionAt,
		admissionID, "sha256:"+strings.Repeat("a", 64), approvalID, 4,
	)

	g.Expect(first).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
	g.Expect(second).NotTo(Equal(first))
	g.Expect(third).NotTo(Equal(first))
}
