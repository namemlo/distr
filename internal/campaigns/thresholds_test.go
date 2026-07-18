package campaigns

import (
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/onsi/gomega"
)

func TestBakeDurationsMustBeNonDecreasing(t *testing.T) {
	g := gomega.NewWithT(t)
	g.Expect(ValidateBakeDurations([]time.Duration{time.Minute, 5 * time.Minute, 5 * time.Minute})).
		To(gomega.Succeed())
	g.Expect(ValidateBakeDurations([]time.Duration{5 * time.Minute, time.Minute})).
		To(gomega.MatchError(gomega.ContainSubstring("non-decreasing")))
	g.Expect(ValidateBakeDurations([]time.Duration{0})).
		To(gomega.MatchError(gomega.ContainSubstring("positive")))
}

func TestThresholdBreachStopsAdmissionAtomically(t *testing.T) {
	g := gomega.NewWithT(t)
	evaluation := EvaluateThreshold(
		types.CampaignThresholdPolicy{MinimumSamples: 4, MaximumFailureRate: 0.25},
		types.CampaignThresholdSnapshot{Successful: 2, Failed: 2},
	)
	g.Expect(evaluation.Breached).To(gomega.BeTrue())
	g.Expect(evaluation.AdmissionAllowed).To(gomega.BeFalse())

	belowMinimum := EvaluateThreshold(
		types.CampaignThresholdPolicy{MinimumSamples: 5, MaximumFailureRate: 0.25},
		types.CampaignThresholdSnapshot{Successful: 2, Failed: 2},
	)
	g.Expect(belowMinimum.Breached).To(gomega.BeFalse())
	g.Expect(belowMinimum.AdmissionAllowed).To(gomega.BeTrue())
}
