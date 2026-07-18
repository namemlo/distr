package observation

import (
	"context"
	"errors"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

func EvaluateGate(
	pending types.PendingDesiredRevision,
	observations []types.ObservedComponentState,
	now time.Time,
) types.ObservationGateResult {
	trusted := make([]types.ObservedComponentState, 0, len(observations))
	for _, observed := range observations {
		if observed.Trusted && observed.Current &&
			observed.OrganizationID == pending.OrganizationID &&
			observed.DeploymentUnitID == pending.DeploymentUnitID &&
			observed.ComponentInstanceID == pending.ComponentInstanceID &&
			!observed.CapturedAt.After(pending.ObservationDeadline) {
			trusted = append(trusted, observed)
		}
	}
	if hasObserverConflict(trusted) {
		return terminalGate(types.ObservationGateStatusConflict,
			"trusted observers report conflicting runtime state")
	}
	if len(trusted) == 0 {
		if !now.Before(pending.ObservationDeadline) {
			return terminalGate(types.ObservationGateStatusTimedOut,
				"independent observation deadline elapsed")
		}
		return types.ObservationGateResult{Status: types.ObservationGateStatusPending}
	}
	observed := newestObservation(trusted)
	switch observed.ExecutorOutcome {
	case types.ExecutorOutcomeFailed:
		return terminalGate(
			types.ObservationGateStatusFailed,
			"executor reported terminal failure before independent verification",
		)
	case types.ExecutorOutcomeCancelled:
		return terminalGate(
			types.ObservationGateStatusCancelled,
			"executor was cancelled before independent verification",
		)
	case types.ExecutorOutcomeUnknown:
		return terminalGate(
			types.ObservationGateStatusUnknown,
			"executor outcome remains unknown",
		)
	case "":
		if !now.Before(pending.ObservationDeadline) {
			return terminalGate(
				types.ObservationGateStatusTimedOut,
				"executor did not report success before the observation deadline",
			)
		}
		return types.ObservationGateResult{
			Status: types.ObservationGateStatusPending,
			Reason: "executor success remains provisional or unavailable",
		}
	}
	switch observed.Outcome {
	case types.ObservationOutcomePartial:
		return terminalGate(types.ObservationGateStatusPartial,
			"observer returned partial state")
	case types.ObservationOutcomeUnknown:
		return terminalGate(types.ObservationGateStatusUnknown,
			"observer could not determine runtime state")
	case types.ObservationOutcomeCancelled:
		return terminalGate(types.ObservationGateStatusCancelled,
			"observation was cancelled")
	case types.ObservationOutcomeFailed:
		return terminalGate(types.ObservationGateStatusFailed,
			"observer reported failure")
	}
	if !matchesPending(pending, observed) || observed.Health != types.ObservedHealthHealthy {
		if observed.ExecutorOutcome == types.ExecutorOutcomeSucceeded {
			return terminalGate(
				types.ObservationGateStatusFailed,
				"executor reported success but independent runtime observation does not match",
			)
		}
		return terminalGate(types.ObservationGateStatusFailed,
			"independent runtime observation does not match pending desired state")
	}
	return types.ObservationGateResult{
		Status:        types.ObservationGateStatusVerified,
		ObservationID: observed.ID, ObservationChecksum: observed.StateChecksum,
		ReleaseMutationLock: true,
	}
}

func hasObserverConflict(observations []types.ObservedComponentState) bool {
	if len(observations) < 2 {
		return false
	}
	first := observations[0]
	for _, observed := range observations[1:] {
		if observed.ObserverID != first.ObserverID &&
			!sameObservedRuntimeState(first, observed) {
			return true
		}
	}
	return false
}

func sameObservedRuntimeState(
	first types.ObservedComponentState,
	second types.ObservedComponentState,
) bool {
	return first.ArtifactDigest == second.ArtifactDigest &&
		first.ConfigChecksum == second.ConfigChecksum &&
		first.SchemaVersion == second.SchemaVersion &&
		first.CapabilityChecksum == second.CapabilityChecksum &&
		first.Platform == second.Platform &&
		first.TopologyChecksum == second.TopologyChecksum &&
		first.Health == second.Health &&
		first.Outcome == second.Outcome
}

func newestObservation(observations []types.ObservedComponentState) types.ObservedComponentState {
	newest := observations[0]
	for _, observed := range observations[1:] {
		if observed.CapturedAt.After(newest.CapturedAt) {
			newest = observed
		}
	}
	return newest
}

func matchesPending(pending types.PendingDesiredRevision, observed types.ObservedComponentState) bool {
	return observed.ArtifactDigest == pending.ArtifactDigest &&
		observed.ConfigChecksum == pending.ConfigChecksum &&
		observed.SchemaVersion == pending.SchemaVersion &&
		observed.CapabilityChecksum == pending.CapabilityChecksum &&
		observed.Platform == pending.Platform &&
		observed.TopologyChecksum == pending.TopologyChecksum
}

func terminalGate(
	status types.ObservationGateStatus,
	reason string,
) types.ObservationGateResult {
	return types.ObservationGateResult{
		Status: status, Reason: reason, Quarantine: true, ReleaseMutationLock: true,
	}
}

type CampaignObservationStore interface {
	VerifyTrustedObservation(
		context.Context,
		uuid.UUID,
		uuid.UUID,
		string,
	) error
}

type CampaignObservationResolverStore interface {
	ResolveTrustedObservation(
		context.Context,
		uuid.UUID,
		uuid.UUID,
		string,
	) (uuid.UUID, string, error)
}

type CampaignVerifier struct {
	Store CampaignObservationStore
}

// CampaignResolver resolves a frozen canonical component placement to its
// current trusted observation. The placement UUID is ComponentInstance.id,
// not the plan-local DeploymentPlanTargetComponent.id.
type CampaignResolver struct {
	Store CampaignObservationResolverStore
}

func (r CampaignResolver) ResolveCampaignObservation(
	ctx context.Context,
	organizationID uuid.UUID,
	componentInstanceID uuid.UUID,
	expectedChecksum string,
) (uuid.UUID, string, error) {
	if r.Store == nil {
		return uuid.Nil, "", errors.New("campaign observation store is required")
	}
	if organizationID == uuid.Nil || componentInstanceID == uuid.Nil ||
		!observationChecksumPattern.MatchString(expectedChecksum) {
		return uuid.Nil, "", errors.New(
			"campaign organization, canonical component placement, and lowercase sha256 checksum are required",
		)
	}
	return r.Store.ResolveTrustedObservation(
		ctx,
		organizationID,
		componentInstanceID,
		expectedChecksum,
	)
}

func (v CampaignVerifier) VerifyCampaignObservation(
	ctx context.Context,
	organizationID uuid.UUID,
	observationID uuid.UUID,
	checksum string,
) error {
	if v.Store == nil {
		return errors.New("campaign observation store is required")
	}
	if organizationID == uuid.Nil || observationID == uuid.Nil ||
		!observationChecksumPattern.MatchString(checksum) {
		return errors.New("campaign observation identity and lowercase sha256 checksum are required")
	}
	return v.Store.VerifyTrustedObservation(ctx, organizationID, observationID, checksum)
}
