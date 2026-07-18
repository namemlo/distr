package observation

import (
	"context"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

type campaignObservationStoreStub struct {
	organizationID uuid.UUID
	observationID  uuid.UUID
	placementID    uuid.UUID
	checksum       string
	actualChecksum string
	err            error
}

func (s *campaignObservationStoreStub) VerifyTrustedObservation(
	_ context.Context,
	organizationID uuid.UUID,
	observationID uuid.UUID,
	checksum string,
) error {
	s.organizationID = organizationID
	s.observationID = observationID
	s.checksum = checksum
	return s.err
}

func (s *campaignObservationStoreStub) ResolveTrustedObservation(
	_ context.Context,
	organizationID uuid.UUID,
	componentInstanceID uuid.UUID,
	expectedChecksum string,
) (uuid.UUID, string, error) {
	s.organizationID = organizationID
	s.placementID = componentInstanceID
	s.checksum = expectedChecksum
	return s.observationID, s.actualChecksum, s.err
}

func TestObservationGateRequiresIndependentExactMatch(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 7, 18, 6, 0, 0, 0, time.UTC)
	pending := gatePending(now)
	observed := matchingObserved(pending, now)

	result := EvaluateGate(pending, []types.ObservedComponentState{observed}, now)

	g.Expect(result.Status).To(Equal(types.ObservationGateStatusVerified))
	g.Expect(result.ObservationID).To(Equal(observed.ID))
	g.Expect(result.ObservationChecksum).To(Equal(observed.StateChecksum))
}

func TestObservationGateDoesNotInventSuccessForPartialUnknownCancelOrFailure(t *testing.T) {
	now := time.Date(2026, 7, 18, 6, 0, 0, 0, time.UTC)
	for _, outcome := range []types.ObservationOutcome{
		types.ObservationOutcomePartial,
		types.ObservationOutcomeUnknown,
		types.ObservationOutcomeCancelled,
		types.ObservationOutcomeFailed,
	} {
		t.Run(string(outcome), func(t *testing.T) {
			g := NewWithT(t)
			pending := gatePending(now)
			observed := matchingObserved(pending, now)
			observed.Outcome = outcome

			result := EvaluateGate(pending, []types.ObservedComponentState{observed}, now)

			g.Expect(result.Status).NotTo(Equal(types.ObservationGateStatusVerified))
		})
	}
}

func TestObservationGateQuarantinesTimeoutAndConflictingObservers(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 7, 18, 6, 0, 0, 0, time.UTC)
	pending := gatePending(now.Add(-10 * time.Minute))

	timeout := EvaluateGate(pending, nil, now)
	g.Expect(timeout.Status).To(Equal(types.ObservationGateStatusTimedOut))
	g.Expect(timeout.Quarantine).To(BeTrue())
	g.Expect(timeout.ReleaseMutationLock).To(BeTrue())

	first := matchingObserved(pending, now)
	first.CapturedAt = pending.ObservationDeadline.Add(-time.Second)
	second := first
	second.ID = uuid.New()
	second.ObserverID = uuid.New()
	second.ArtifactDigest = digest("wrong")
	second.StateChecksum = digest("other-state")
	conflict := EvaluateGate(pending, []types.ObservedComponentState{first, second}, now)
	g.Expect(conflict.Status).To(Equal(types.ObservationGateStatusConflict))
	g.Expect(conflict.Quarantine).To(BeTrue())
	g.Expect(conflict.ReleaseMutationLock).To(BeTrue())
}

func TestObservationGateAcceptsIndependentObserversThatAgree(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 7, 18, 6, 0, 0, 0, time.UTC)
	pending := gatePending(now)
	first := matchingObserved(pending, now)
	second := first
	second.ID = uuid.New()
	second.ObserverID = uuid.New()
	second.StateChecksum = digest("independent-envelope")

	result := EvaluateGate(
		pending,
		[]types.ObservedComponentState{first, second},
		now,
	)

	g.Expect(result.Status).To(Equal(types.ObservationGateStatusVerified))
	g.Expect(result.Quarantine).To(BeFalse())
}

func TestObservationGateDoesNotVerifyEvidenceCapturedAfterDeadline(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 7, 18, 6, 0, 0, 0, time.UTC)
	pending := gatePending(now)
	observed := matchingObserved(pending, now)
	observed.CapturedAt = pending.ObservationDeadline.Add(time.Second)

	result := EvaluateGate(
		pending,
		[]types.ObservedComponentState{observed},
		pending.ObservationDeadline.Add(2*time.Second),
	)

	g.Expect(result.Status).To(Equal(types.ObservationGateStatusTimedOut))
	g.Expect(result.Quarantine).To(BeTrue())
}

func TestObservationGateRejectsExecutorSuccessWhenRuntimeIsWrong(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 7, 18, 6, 0, 0, 0, time.UTC)
	pending := gatePending(now)
	observed := matchingObserved(pending, now)
	observed.ExecutorOutcome = types.ExecutorOutcomeSucceeded
	observed.ArtifactDigest = digest("wrong-runtime")

	result := EvaluateGate(
		pending,
		[]types.ObservedComponentState{observed},
		now,
	)

	g.Expect(result.Status).To(Equal(types.ObservationGateStatusFailed))
	g.Expect(result.Reason).To(ContainSubstring("executor reported success"))
	g.Expect(result.Quarantine).To(BeTrue())
}

func TestObservationGateDoesNotAdvanceTerminalExecutorFailure(t *testing.T) {
	now := time.Date(2026, 7, 18, 6, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		outcome types.ExecutorOutcome
		status  types.ObservationGateStatus
	}{
		{types.ExecutorOutcomeFailed, types.ObservationGateStatusFailed},
		{types.ExecutorOutcomeCancelled, types.ObservationGateStatusCancelled},
		{types.ExecutorOutcomeUnknown, types.ObservationGateStatusUnknown},
	} {
		t.Run(string(test.outcome), func(t *testing.T) {
			g := NewWithT(t)
			pending := gatePending(now)
			observed := matchingObserved(pending, now)
			observed.ExecutorOutcome = test.outcome

			result := EvaluateGate(
				pending,
				[]types.ObservedComponentState{observed},
				now,
			)

			g.Expect(result.Status).To(Equal(test.status))
			g.Expect(result.ReleaseMutationLock).To(BeTrue())
		})
	}
}

func TestCampaignObservationVerifierBindsTrustedIdentityAndChecksum(t *testing.T) {
	g := NewWithT(t)
	store := &campaignObservationStoreStub{}
	organizationID := uuid.New()
	observationID := uuid.New()
	checksum := digest("campaign-observation")
	verifier := CampaignVerifier{Store: store}

	err := verifier.VerifyCampaignObservation(
		context.Background(),
		organizationID,
		observationID,
		checksum,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(store.organizationID).To(Equal(organizationID))
	g.Expect(store.observationID).To(Equal(observationID))
	g.Expect(store.checksum).To(Equal(checksum))
}

func TestCampaignObservationResolverUsesCanonicalComponentPlacement(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	componentInstanceID := uuid.New()
	observationID := uuid.New()
	checksum := digest("frozen-campaign-observation")
	store := &campaignObservationStoreStub{
		observationID: observationID, actualChecksum: checksum,
	}

	resolvedID, actualChecksum, err := (CampaignResolver{Store: store}).
		ResolveCampaignObservation(
			context.Background(),
			organizationID,
			componentInstanceID,
			checksum,
		)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(resolvedID).To(Equal(observationID))
	g.Expect(actualChecksum).To(Equal(checksum))
	g.Expect(store.organizationID).To(Equal(organizationID))
	g.Expect(store.placementID).To(Equal(componentInstanceID))
	g.Expect(store.checksum).To(Equal(checksum))
}

func gatePending(now time.Time) types.PendingDesiredRevision {
	return types.PendingDesiredRevision{
		ID:                  uuid.New(),
		OrganizationID:      uuid.New(),
		DeploymentUnitID:    uuid.New(),
		ComponentInstanceID: uuid.New(),
		ComponentKey:        "api",
		ArtifactDigest:      digest("artifact"),
		ConfigChecksum:      digest("config"),
		SchemaVersion:       "2026071801",
		CapabilityChecksum:  digest("capability"),
		Platform:            "linux/amd64",
		TopologyChecksum:    digest("topology"),
		ObservationDeadline: now.Add(2 * time.Minute),
		Status:              types.PendingDesiredStatusPending,
	}
}

func matchingObserved(pending types.PendingDesiredRevision, now time.Time) types.ObservedComponentState {
	return types.ObservedComponentState{
		ID:                  uuid.New(),
		OrganizationID:      pending.OrganizationID,
		ObserverID:          uuid.New(),
		DeploymentUnitID:    pending.DeploymentUnitID,
		ComponentInstanceID: pending.ComponentInstanceID,
		ComponentKey:        pending.ComponentKey,
		ArtifactDigest:      pending.ArtifactDigest,
		ConfigChecksum:      pending.ConfigChecksum,
		SchemaVersion:       pending.SchemaVersion,
		CapabilityChecksum:  pending.CapabilityChecksum,
		Platform:            pending.Platform,
		TopologyChecksum:    pending.TopologyChecksum,
		Health:              types.ObservedHealthHealthy,
		Outcome:             types.ObservationOutcomeComplete,
		Trusted:             true,
		Current:             true,
		CapturedAt:          now.Add(-time.Second),
		StateChecksum:       digest("state"),
		ExecutorOutcome:     types.ExecutorOutcomeSucceeded,
	}
}
