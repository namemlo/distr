package campaigns

import (
	"errors"
	"fmt"
	"time"

	"github.com/distr-sh/distr/internal/types"
)

func ValidateBakeDurations(durations []time.Duration) error {
	var previous time.Duration
	for i, duration := range durations {
		if duration <= 0 {
			return errors.New("campaign bake duration must be positive")
		}
		if i > 0 && duration < previous {
			return fmt.Errorf(
				"campaign bake durations must be non-decreasing: wave %d is shorter than wave %d",
				i+1,
				i,
			)
		}
		previous = duration
	}
	return nil
}

func EvaluateThreshold(
	policy types.CampaignThresholdPolicy,
	snapshot types.CampaignThresholdSnapshot,
) types.CampaignThresholdDecision {
	samples := snapshot.Successful + snapshot.Failed
	decision := types.CampaignThresholdDecision{
		Samples:          samples,
		AdmissionAllowed: true,
	}
	if samples > 0 {
		decision.FailureRate = float64(snapshot.Failed) / float64(samples)
	}
	if policy.MinimumSamples > 0 &&
		samples >= policy.MinimumSamples &&
		decision.FailureRate > policy.MaximumFailureRate {
		decision.Breached = true
		decision.AdmissionAllowed = false
	}
	return decision
}
