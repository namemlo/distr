package targetconfig

import (
	"context"
	"errors"
	"fmt"

	"github.com/distr-sh/distr/internal/types"
)

const maxObservedMediaTypeBytes = 128

type ObjectVerifier interface {
	Verify(context.Context, types.TargetConfigSnapshotObject) (types.VerifiedTargetConfigObject, error)
}

var ErrObjectVerificationUnavailable = errors.New("target config object verification is unavailable")

type unavailableObjectVerifier struct{}

func NewUnavailableObjectVerifier() ObjectVerifier {
	return unavailableObjectVerifier{}
}

func (unavailableObjectVerifier) Verify(
	context.Context,
	types.TargetConfigSnapshotObject,
) (types.VerifiedTargetConfigObject, error) {
	return types.VerifiedTargetConfigObject{}, ErrObjectVerificationUnavailable
}

func VerifyObjects(
	ctx context.Context,
	snapshot types.TargetConfigSnapshot,
	verifiers ...ObjectVerifier,
) (*types.ObjectVerificationResult, error) {
	if len(verifiers) != 1 || verifiers[0] == nil {
		return nil, errors.New("target config object verifier is required")
	}
	if len(snapshot.Objects) > maxTargetConfigObjects {
		return nil, fmt.Errorf("target config snapshot exceeds object verification limit")
	}
	result := &types.ObjectVerificationResult{
		SnapshotID: snapshot.ID,
		Verified:   true,
		Objects:    make([]types.ObjectVerificationFact, 0, len(snapshot.Objects)),
	}
	for _, object := range snapshot.Objects {
		observed, err := verifiers[0].Verify(ctx, object)
		fact := verifyTargetConfigObjectFact(object, observed, err)
		if !fact.Verified {
			result.Verified = false
		}
		result.Objects = append(result.Objects, fact)
	}
	return result, nil
}

func verifyTargetConfigObjectFact(
	expected types.TargetConfigSnapshotObject,
	observed types.VerifiedTargetConfigObject,
	verifyErr error,
) types.ObjectVerificationFact {
	fact := types.ObjectVerificationFact{
		Key:      expected.Key,
		Verified: false,
		Code:     "verification_failed",
		Message:  "object could not be verified",
	}
	if verifyErr != nil {
		if errors.Is(verifyErr, ErrObjectVerificationUnavailable) {
			fact.Code = "verification_unavailable"
			fact.Message = "object verification is unavailable"
		}
		return fact
	}
	if !validObservedVersionID(observed.VersionID) || !validObservedMediaType(observed.MediaType) {
		return fact
	}
	fact.ObservedVersionID = observed.VersionID
	fact.ObservedMediaType = observed.MediaType
	fact.ObservedSizeBytes = &observed.SizeBytes
	fact.ObservedChecksum = observed.Checksum
	switch {
	case observed.Reference != expected.Reference:
		fact.Code = "reference_mismatch"
		fact.Message = "object reference does not match snapshot"
	case observed.VersionID != expected.VersionID:
		fact.Code = "version_mismatch"
		fact.Message = "object version does not match snapshot"
	case expected.MediaType != "" && observed.MediaType != expected.MediaType:
		fact.Code = "media_type_mismatch"
		fact.Message = "object media type does not match snapshot"
	case observed.SizeBytes != expected.SizeBytes:
		fact.Code = "size_mismatch"
		fact.Message = "object size does not match snapshot"
	case observed.Checksum != expected.Checksum:
		fact.Code = "checksum_mismatch"
		fact.Message = "object checksum does not match snapshot"
	default:
		fact.Verified = true
		fact.Code = "verified"
		fact.Message = "object matches snapshot"
	}
	return fact
}

func validObservedVersionID(value string) bool {
	return validTargetConfigVersionID(value)
}

func validObservedMediaType(value string) bool {
	return value == "" ||
		(len(value) <= maxObservedMediaTypeBytes && targetConfigMediaTypePattern.MatchString(value))
}
