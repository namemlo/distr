package db

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/targetconfig"
	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

type targetPlanObjectVerifierStub struct {
	received types.TargetConfigSnapshotObject
	observed types.VerifiedTargetConfigObject
	err      error
}

func (stub *targetPlanObjectVerifierStub) Verify(
	_ context.Context,
	object types.TargetConfigSnapshotObject,
) (types.VerifiedTargetConfigObject, error) {
	stub.received = object
	return stub.observed, stub.err
}

func TestTargetPlanConfigVerifierAdaptsExactSnapshotIdentity(t *testing.T) {
	g := NewWithT(t)
	expected := types.TargetPlanConfigObject{
		Key:       "service-config",
		Kind:      types.TargetConfigObjectKindServiceConfig,
		Reference: "s3://config/service.json",
		VersionID: "version-7",
		MediaType: "application/json",
		SizeBytes: 42,
		Checksum:  "sha256:" + strings.Repeat("a", 64),
	}
	stub := &targetPlanObjectVerifierStub{
		observed: types.VerifiedTargetConfigObject{
			Reference: expected.Reference,
			VersionID: expected.VersionID,
			MediaType: expected.MediaType,
			SizeBytes: expected.SizeBytes,
			Checksum:  expected.Checksum,
		},
	}

	observed, err := NewTargetPlanConfigObjectVerifier(stub).
		VerifyTargetConfigObject(t.Context(), expected)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(stub.received.Key).To(Equal(expected.Key))
	g.Expect(stub.received.Kind).To(Equal(expected.Kind))
	g.Expect(stub.received.Reference).To(Equal(expected.Reference))
	g.Expect(stub.received.VersionID).To(Equal(expected.VersionID))
	g.Expect(stub.received.MediaType).To(Equal(expected.MediaType))
	g.Expect(stub.received.SizeBytes).To(Equal(expected.SizeBytes))
	g.Expect(stub.received.Checksum).To(Equal(expected.Checksum))
	g.Expect(observed.Checksum).To(Equal(expected.Checksum))
}

func TestTargetPlanConfigVerifierMapsUnavailableEvidence(t *testing.T) {
	g := NewWithT(t)
	stub := &targetPlanObjectVerifierStub{err: targetconfig.ErrObjectVerificationUnavailable}

	_, err := NewTargetPlanConfigObjectVerifier(stub).VerifyTargetConfigObject(
		t.Context(),
		types.TargetPlanConfigObject{},
	)

	g.Expect(errors.Is(err, ErrTargetConfigObjectVerificationUnavailable)).To(BeTrue())
}
