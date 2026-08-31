package reviewadmission_test

import (
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/reviewadmission"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCurrentGoRequiresExactUnexpiredObservedState(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	decision := types.ReviewAdmissionDecisionRecord{
		ID: uuid.New(), OrganizationID: uuid.New(), DeploymentPlanID: uuid.New(),
		PlanRevision: 1, PlanChecksum: testChecksum('a'),
		ObservedStateChecksum: testChecksum('b'), Decision: types.ReviewAdmissionDecisionGo,
		Reason: "reviewed", ActorUserAccountID: uuid.New(), ExpiresAt: now.Add(time.Hour),
		AuthorizationEvidence: testChecksum('c'), IdempotencyKey: "review-1",
	}
	decision.ReviewMaterialChecksum = reviewadmission.ReviewMaterialChecksum(
		decision.PlanChecksum, decision.ObservedStateChecksum,
	)
	decision.CanonicalChecksum = reviewadmission.CanonicalChecksum(decision)

	g.Expect(reviewadmission.ValidateCurrentGo(
		decision, decision.PlanChecksum, decision.ObservedStateChecksum, now,
	)).To(Succeed())
	g.Expect(reviewadmission.ValidateCurrentGo(
		decision, decision.PlanChecksum, testChecksum('d'), now,
	)).To(MatchError("review admission GO decision is stale"))
	decision.Decision = types.ReviewAdmissionDecisionNoGo
	decision.CanonicalChecksum = reviewadmission.CanonicalChecksum(decision)
	g.Expect(reviewadmission.ValidateCurrentGo(
		decision, decision.PlanChecksum, decision.ObservedStateChecksum, now,
	)).To(MatchError("latest review admission decision is NO_GO"))
}

func TestReviewMaterialStateDistinguishesMissingCurrentNoGoAndStale(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	planChecksum := testChecksum('a')
	observedChecksum := testChecksum('b')

	g.Expect(reviewadmission.MaterialState(
		nil,
		planChecksum,
		observedChecksum,
		now,
	)).To(Equal(types.ReviewAdmissionMaterialStateMissing))

	decision := types.ReviewAdmissionDecisionRecord{
		ID: uuid.New(), OrganizationID: uuid.New(), DeploymentPlanID: uuid.New(),
		PlanRevision: 1, PlanChecksum: planChecksum,
		ObservedStateChecksum: observedChecksum, Decision: types.ReviewAdmissionDecisionGo,
		Reason: "reviewed", ActorUserAccountID: uuid.New(), ExpiresAt: now.Add(time.Hour),
		AuthorizationEvidence: testChecksum('c'), IdempotencyKey: "review-state-1",
	}
	decision.ReviewMaterialChecksum = reviewadmission.ReviewMaterialChecksum(
		planChecksum,
		observedChecksum,
	)
	decision.CanonicalChecksum = reviewadmission.CanonicalChecksum(decision)
	g.Expect(reviewadmission.MaterialState(
		&decision,
		planChecksum,
		observedChecksum,
		now,
	)).To(Equal(types.ReviewAdmissionMaterialStateGo))

	decision.Decision = types.ReviewAdmissionDecisionNoGo
	decision.CanonicalChecksum = reviewadmission.CanonicalChecksum(decision)
	g.Expect(reviewadmission.MaterialState(
		&decision,
		planChecksum,
		observedChecksum,
		now,
	)).To(Equal(types.ReviewAdmissionMaterialStateNoGo))

	g.Expect(reviewadmission.MaterialState(
		&decision,
		planChecksum,
		testChecksum('d'),
		now,
	)).To(Equal(types.ReviewAdmissionMaterialStateStale))
}

func testChecksum(character byte) string {
	return "sha256:" + strings.Repeat(string(character), 64)
}
