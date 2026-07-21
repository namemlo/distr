package reconciliation

import (
	"errors"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

func ClassifyDrift(
	desired types.ActiveDesiredRevision,
	observed types.ObservedComponentState,
) types.DriftClassification {
	return ClassifyDriftAt(desired, observed, time.Now().UTC())
}

func ClassifyDriftAt(
	desired types.ActiveDesiredRevision,
	observed types.ObservedComponentState,
	now time.Time,
) types.DriftClassification {
	classes := make([]types.DriftClass, 0, 8)
	if observed.ID == [16]byte{} {
		classes = append(classes, types.DriftClassMissing)
	}
	if !observed.Trusted || !observed.Current || observed.FreshUntil.IsZero() ||
		now.After(observed.FreshUntil) {
		classes = append(classes, types.DriftClassStale)
	}
	if observed.ArtifactDigest != desired.ArtifactDigest {
		classes = append(classes, types.DriftClassArtifact)
	}
	if observed.ConfigChecksum != desired.ConfigChecksum {
		classes = append(classes, types.DriftClassConfiguration)
	}
	if observed.SchemaVersion != desired.SchemaVersion {
		classes = append(classes, types.DriftClassSchema)
	}
	if observed.CapabilityChecksum != desired.CapabilityChecksum {
		classes = append(classes, types.DriftClassCapability)
	}
	if observed.Platform != desired.Platform {
		classes = append(classes, types.DriftClassPlatform)
	}
	if observed.TopologyChecksum != desired.TopologyChecksum {
		classes = append(classes, types.DriftClassTopology)
	}
	if observed.Health != types.ObservedHealthHealthy {
		classes = append(classes, types.DriftClassHealth)
	}
	if observed.ExecutorOutcome == types.ExecutorOutcomeSucceeded && len(classes) > 0 {
		classes = append(classes, types.DriftClassExecutorMismatch)
	}
	summary := "observed state matches active desired state"
	if len(classes) > 0 {
		summary = "observed state differs from active desired state"
	}
	return types.DriftClassification{Drifted: len(classes) > 0, Classes: classes, Summary: summary}
}

func AcceptDeviation(
	desired types.ActiveDesiredRevision,
	observed types.ObservedComponentState,
	decision types.ReconciliationDecision,
	now time.Time,
) (*types.AcceptedDeviation, error) {
	if decision.Action != types.ReconciliationActionAcceptDeviation {
		return nil, errors.New("reconciliation decision is not an accepted deviation")
	}
	if strings.TrimSpace(decision.Reason) == "" {
		return nil, errors.New("accepted deviation requires a reason")
	}
	if decision.AcceptedUntil == nil || !decision.AcceptedUntil.After(now) {
		return nil, errors.New("accepted deviation must expire in the future")
	}
	if !ClassifyDriftAt(desired, observed, now).Drifted {
		return nil, errors.New("matching state does not require a deviation")
	}
	return &types.AcceptedDeviation{
		DesiredRevisionID: desired.ID,
		ObservationID:     observed.ID,
		Reason:            strings.TrimSpace(decision.Reason),
		ExpiresAt:         *decision.AcceptedUntil,
	}, nil
}

func DecisionTargetStatus(
	decision types.ReconciliationDecision,
	now time.Time,
) (types.DriftCaseStatus, error) {
	if strings.TrimSpace(decision.Reason) == "" || decision.ActorID == uuid.Nil {
		return "", errors.New("reconciliation decision requires actor and reason")
	}
	switch decision.Action {
	case types.ReconciliationActionCreatePlan:
		if decision.DeploymentPlanID == nil ||
			*decision.DeploymentPlanID == uuid.Nil {
			return "", errors.New("create-plan reconciliation requires a deployment plan")
		}
		return types.DriftCaseStatusAssigned, nil
	case types.ReconciliationActionAcceptDeviation:
		if decision.AcceptedUntil == nil || !decision.AcceptedUntil.After(now) {
			return "", errors.New("accepted deviation must expire in the future")
		}
		return types.DriftCaseStatusException, nil
	case types.ReconciliationActionRestoreDesired,
		types.ReconciliationActionCloseWithEvidence:
		if decision.OutcomeObservationID == nil ||
			*decision.OutcomeObservationID == uuid.Nil {
			return "", errors.New(
				"resolved reconciliation requires an outcome observation",
			)
		}
		return types.DriftCaseStatusResolved, nil
	default:
		return "", errors.New("reconciliation action is invalid")
	}
}
