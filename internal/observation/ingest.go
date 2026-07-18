package observation

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
)

var (
	ErrObserverMismatch        = errors.New("observer identity or scope mismatch")
	ErrUntrustedObservation    = errors.New("observation authentication failed")
	ErrStaleObservation        = errors.New("observation is stale")
	ErrConflictingReplay       = errors.New("observation sequence conflicts with retained evidence")
	ErrInvalidObservation      = errors.New("observation envelope is invalid")
	observationChecksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

func EvaluateAdmission(
	registration types.ObserverRegistration,
	head *types.ComponentObservationHead,
	envelope types.ObservationEnvelope,
	receivedAt time.Time,
) (types.ObservationAdmissionDecision, error) {
	reject := func(reason string, retain bool) types.ObservationAdmissionDecision {
		return types.ObservationAdmissionDecision{
			Disposition:    types.ObservationDispositionRejected,
			RetainEvidence: retain, Reason: reason,
		}
	}
	if !registration.Enabled || envelope.ObserverID != registration.ID ||
		envelope.OrganizationID != registration.OrganizationID ||
		envelope.DeploymentUnitID != registration.DeploymentUnitID ||
		(registration.ComponentInstanceID != nil &&
			envelope.ComponentInstanceID != *registration.ComponentInstanceID) {
		return reject(ErrObserverMismatch.Error(), false), ErrObserverMismatch
	}
	if registration.CredentialFingerprint == "" ||
		envelope.CredentialFingerprint != registration.CredentialFingerprint {
		return reject(ErrUntrustedObservation.Error(), false), ErrUntrustedObservation
	}
	if envelope.SourceSequence < 1 || envelope.ComponentInstanceID == [16]byte{} ||
		strings.TrimSpace(envelope.ComponentKey) == "" ||
		!observationChecksumPattern.MatchString(envelope.EvidenceChecksum) {
		return reject(ErrInvalidObservation.Error(), false), ErrInvalidObservation
	}
	if envelope.CapturedAt.After(receivedAt.Add(registration.MaxClockSkew)) {
		decision := reject("observation captured time exceeds clock-skew policy", true)
		decision.Trusted = true
		return decision, ErrInvalidObservation
	}
	if receivedAt.Sub(envelope.CapturedAt) > registration.MaxFreshness {
		decision := reject(ErrStaleObservation.Error(), true)
		decision.Trusted = true
		return decision, ErrStaleObservation
	}
	if head != nil && head.ObserverID == envelope.ObserverID {
		switch {
		case envelope.SourceSequence == head.SourceSequence &&
			envelope.EvidenceChecksum == head.EvidenceChecksum:
			return types.ObservationAdmissionDecision{
				Disposition: types.ObservationDispositionReplay,
				Trusted:     true, RetainEvidence: true,
			}, nil
		case envelope.SourceSequence == head.SourceSequence:
			return types.ObservationAdmissionDecision{
				Disposition: types.ObservationDispositionConflict,
				Trusted:     true, RetainEvidence: true, Quarantine: true,
				Reason: ErrConflictingReplay.Error(),
			}, ErrConflictingReplay
		case envelope.SourceSequence < head.SourceSequence:
			return types.ObservationAdmissionDecision{
				Disposition: types.ObservationDispositionOutOfOrder,
				Trusted:     true, RetainEvidence: true,
				Reason: "older source sequence retained without replacing current evidence",
			}, nil
		}
	}
	return types.ObservationAdmissionDecision{
		Disposition: types.ObservationDispositionAccepted,
		Trusted:     true, AdvanceHead: true, RetainEvidence: true,
	}, nil
}
