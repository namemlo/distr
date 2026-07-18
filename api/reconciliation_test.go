package api

import (
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestReconciliationDecisionValidationRequiresTimeBoundDeviation(t *testing.T) {
	g := NewWithT(t)
	now := time.Now().UTC()
	request := ReconciliationDecisionRequest{
		Action:        types.ReconciliationActionAcceptDeviation,
		Reason:        "temporary vendor incident",
		AcceptedUntil: new(now.Add(time.Hour)),
	}
	g.Expect(request.Validate(now)).To(Succeed())

	request.AcceptedUntil = nil
	g.Expect(request.Validate(now)).To(HaveOccurred())
	request.AcceptedUntil = new(now.Add(-time.Second))
	g.Expect(request.Validate(now)).To(HaveOccurred())
}

func TestReconciliationPlanActionRequiresPlanIdentity(t *testing.T) {
	g := NewWithT(t)
	request := ReconciliationDecisionRequest{
		Action: types.ReconciliationActionCreatePlan,
		Reason: "restore exact active desired state",
	}
	g.Expect(request.Validate(time.Now().UTC())).To(HaveOccurred())
	request.DeploymentPlanID = new(uuid.New())
	g.Expect(request.Validate(time.Now().UTC())).To(Succeed())
}

func TestReconciliationResolutionRequiresOutcomeObservation(t *testing.T) {
	g := NewWithT(t)
	request := ReconciliationDecisionRequest{
		Action: types.ReconciliationActionCloseWithEvidence,
		Reason: "verified restored runtime",
	}
	g.Expect(request.Validate(time.Now().UTC())).To(HaveOccurred())
	request.OutcomeObservationID = new(uuid.New())
	g.Expect(request.Validate(time.Now().UTC())).To(Succeed())
}
