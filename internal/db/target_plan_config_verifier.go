package db

import (
	"context"
	"errors"

	"github.com/distr-sh/distr/internal/types"
)

var ErrTargetConfigObjectVerificationUnavailable = errors.New(
	"target config object verification is unavailable",
)

// TargetConfigObjectVerifier is the narrow PR-058 integration seam used by
// planning. The PR-058 object-store verifier adapter must provide observations
// from the backing object provider; database checksums are never observations.
type TargetConfigObjectVerifier interface {
	VerifyTargetConfigObject(
		context.Context,
		types.TargetPlanConfigObject,
	) (types.TargetPlanConfigObservation, error)
}

type unavailableTargetConfigObjectVerifier struct{}

func NewUnavailableTargetConfigObjectVerifier() TargetConfigObjectVerifier {
	return unavailableTargetConfigObjectVerifier{}
}

func (unavailableTargetConfigObjectVerifier) VerifyTargetConfigObject(
	context.Context,
	types.TargetPlanConfigObject,
) (types.TargetPlanConfigObservation, error) {
	return types.TargetPlanConfigObservation{}, ErrTargetConfigObjectVerificationUnavailable
}

func verifyTargetPlanConfigObject(
	ctx context.Context,
	verifier TargetConfigObjectVerifier,
	expected types.TargetPlanConfigObject,
) types.ConfigVerificationFact {
	fact := types.ConfigVerificationFact{
		ObjectKey: expected.Key, Reference: expected.Reference,
		VersionID: expected.VersionID, MediaType: expected.MediaType,
		SizeBytes: expected.SizeBytes, Checksum: expected.Checksum,
		VerificationCode: "verification_failed",
	}
	if verifier == nil {
		verifier = NewUnavailableTargetConfigObjectVerifier()
	}
	observed, err := verifier.VerifyTargetConfigObject(ctx, expected)
	if err != nil {
		if errors.Is(err, ErrTargetConfigObjectVerificationUnavailable) {
			fact.VerificationCode = "verification_unavailable"
		}
		return fact
	}
	fact.ObservedReference = observed.Reference
	fact.ObservedVersionID = observed.VersionID
	fact.ObservedMediaType = observed.MediaType
	fact.ObservedSizeBytes = observed.SizeBytes
	fact.ObservedChecksum = observed.Checksum
	fact.Verified = observed.Reference == expected.Reference &&
		observed.VersionID == expected.VersionID &&
		observed.MediaType == expected.MediaType &&
		observed.SizeBytes == expected.SizeBytes &&
		observed.Checksum == expected.Checksum
	if fact.Verified {
		fact.VerificationCode = "verified"
	} else {
		fact.VerificationCode = "evidence_mismatch"
	}
	return fact
}
