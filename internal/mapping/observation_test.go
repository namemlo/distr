package mapping

import (
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestObservationMappingOmitsObserverCredentialFingerprint(t *testing.T) {
	g := NewWithT(t)
	registration := types.ObserverRegistration{
		ID: uuid.New(), OrganizationID: uuid.New(), DeploymentUnitID: uuid.New(),
		ObserverKey: "runtime", CredentialFingerprint: "sha256:secret",
		MaxFreshness: 2 * time.Minute, MaxClockSkew: 15 * time.Second,
	}

	mapped := ObserverRegistrationToAPI(registration)

	g.Expect(mapped.ID).To(Equal(registration.ID))
	g.Expect(mapped.MaxFreshnessSeconds).To(Equal(int64(120)))
	g.Expect(mapped.MaxClockSkewSeconds).To(Equal(int64(15)))
}

func TestObservedStateMappingRetainsTrustSequenceAndDisposition(t *testing.T) {
	g := NewWithT(t)
	state := types.ObservedComponentState{
		ID: uuid.New(), ObserverID: uuid.New(), SourceSequence: 42,
		Trusted: true, Current: false,
		Disposition: types.ObservationDispositionOutOfOrder,
	}

	mapped := ObservedComponentStateToAPI(state)

	g.Expect(mapped.ID).To(Equal(state.ID))
	g.Expect(mapped.SourceSequence).To(Equal(int64(42)))
	g.Expect(mapped.Trusted).To(BeTrue())
	g.Expect(mapped.Current).To(BeFalse())
	g.Expect(mapped.Disposition).To(Equal(types.ObservationDispositionOutOfOrder))
}
