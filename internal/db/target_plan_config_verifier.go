package db

import (
	"context"
	"errors"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/targetconfig"
	"github.com/distr-sh/distr/internal/types"
)

const maxTargetPlanConfigObjects = 100

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

type targetPlanConfigObjectVerifier struct {
	delegate targetconfig.ObjectVerifier
}

func NewUnavailableTargetConfigObjectVerifier() TargetConfigObjectVerifier {
	return unavailableTargetConfigObjectVerifier{}
}

// NewTargetPlanConfigObjectVerifier adapts the PR-058 object verifier to the
// immutable PR-063 target-plan validation seam.
func NewTargetPlanConfigObjectVerifier(
	delegate targetconfig.ObjectVerifier,
) TargetConfigObjectVerifier {
	if delegate == nil {
		return NewUnavailableTargetConfigObjectVerifier()
	}
	return targetPlanConfigObjectVerifier{delegate: delegate}
}

func (unavailableTargetConfigObjectVerifier) VerifyTargetConfigObject(
	context.Context,
	types.TargetPlanConfigObject,
) (types.TargetPlanConfigObservation, error) {
	return types.TargetPlanConfigObservation{}, ErrTargetConfigObjectVerificationUnavailable
}

func (verifier targetPlanConfigObjectVerifier) VerifyTargetConfigObject(
	ctx context.Context,
	expected types.TargetPlanConfigObject,
) (types.TargetPlanConfigObservation, error) {
	observed, err := verifier.delegate.Verify(ctx, types.TargetConfigSnapshotObject{
		Key:       expected.Key,
		Kind:      expected.Kind,
		Reference: expected.Reference,
		VersionID: expected.VersionID,
		MediaType: expected.MediaType,
		SizeBytes: expected.SizeBytes,
		Checksum:  expected.Checksum,
	})
	if err != nil {
		if errors.Is(err, targetconfig.ErrObjectVerificationUnavailable) {
			return types.TargetPlanConfigObservation{}, ErrTargetConfigObjectVerificationUnavailable
		}
		return types.TargetPlanConfigObservation{}, err
	}
	return types.TargetPlanConfigObservation{
		Reference: observed.Reference,
		VersionID: observed.VersionID,
		MediaType: observed.MediaType,
		SizeBytes: observed.SizeBytes,
		Checksum:  observed.Checksum,
	}, nil
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

func verifyTargetPlanConfigObjects(
	ctx context.Context,
	verifier TargetConfigObjectVerifier,
	objects []types.TargetPlanConfigObject,
) ([]types.ConfigVerificationFact, error) {
	if len(objects) == 0 {
		return nil, apierrors.NewBadRequest(
			"target config snapshot must contain at least one object",
		)
	}
	if len(objects) > maxTargetPlanConfigObjects {
		return nil, apierrors.NewBadRequest(
			"target config snapshot exceeds the object limit",
		)
	}
	facts := make([]types.ConfigVerificationFact, 0, len(objects))
	for _, object := range objects {
		facts = append(facts, verifyTargetPlanConfigObject(ctx, verifier, object))
	}
	return facts, nil
}
