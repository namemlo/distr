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
	for _, observed := range observations {
		if observed.Trusted &&
			observed.Disposition == types.ObservationDispositionConflict &&
			observed.OrganizationID == pending.OrganizationID &&
			observed.DeploymentUnitID == pending.DeploymentUnitID &&
			observed.ComponentInstanceID == pending.ComponentInstanceID &&
			!observed.CapturedAt.Before(pending.CreatedAt) &&
			!observed.CapturedAt.After(pending.ObservationDeadline) {
			return terminalObservationGate(
				types.ObservationGateStatusConflict,
				"trusted observer sequence conflicts with retained evidence",
				observed,
			)
		}
	}
	trusted := make([]types.ObservedComponentState, 0, len(observations))
	for _, observed := range observations {
		if observed.Trusted &&
			observed.Disposition == types.ObservationDispositionAccepted &&
			observed.OrganizationID == pending.OrganizationID &&
			observed.DeploymentUnitID == pending.DeploymentUnitID &&
			observed.ComponentInstanceID == pending.ComponentInstanceID &&
			!observed.CapturedAt.Before(pending.CreatedAt) &&
			!observed.CapturedAt.After(pending.ObservationDeadline) &&
			!observed.FreshUntil.IsZero() &&
			!now.After(observed.FreshUntil) {
			trusted = append(trusted, observed)
		}
	}
	if hasObserverConflict(trusted) {
		return terminalObservationGate(
			types.ObservationGateStatusConflict,
			"trusted observers report conflicting runtime state",
			newestObservation(trusted),
		)
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
		return terminalObservationGate(
			types.ObservationGateStatusFailed,
			"executor reported terminal failure before independent verification",
			observed,
		)
	case types.ExecutorOutcomeCancelled:
		return terminalObservationGate(
			types.ObservationGateStatusCancelled,
			"executor was cancelled before independent verification",
			observed,
		)
	case types.ExecutorOutcomeUnknown:
		return terminalObservationGate(
			types.ObservationGateStatusUnknown,
			"executor outcome remains unknown",
			observed,
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
		return terminalObservationGate(
			types.ObservationGateStatusPartial,
			"observer returned partial state",
			observed,
		)
	case types.ObservationOutcomeUnknown:
		return terminalObservationGate(
			types.ObservationGateStatusUnknown,
			"observer could not determine runtime state",
			observed,
		)
	case types.ObservationOutcomeCancelled:
		return terminalObservationGate(
			types.ObservationGateStatusCancelled,
			"observation was cancelled",
			observed,
		)
	case types.ObservationOutcomeFailed:
		return terminalObservationGate(
			types.ObservationGateStatusFailed,
			"observer reported failure",
			observed,
		)
	}
	if !matchesPending(pending, observed) || observed.Health != types.ObservedHealthHealthy {
		if observed.ExecutorOutcome == types.ExecutorOutcomeSucceeded {
			return terminalObservationGate(
				types.ObservationGateStatusFailed,
				"executor reported success but independent runtime observation does not match",
				observed,
			)
		}
		return terminalObservationGate(
			types.ObservationGateStatusFailed,
			"independent runtime observation does not match pending desired state",
			observed,
		)
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

func terminalObservationGate(
	status types.ObservationGateStatus,
	reason string,
	observed types.ObservedComponentState,
) types.ObservationGateResult {
	result := terminalGate(status, reason)
	result.ObservationID = observed.ID
	result.ObservationChecksum = observed.StateChecksum
	return result
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
