package observation

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestObservationAdmissionAcceptsTrustedFreshInScopeEnvelope(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 7, 18, 5, 0, 0, 0, time.UTC)
	registration, envelope := validObservation(now)

	decision, err := EvaluateAdmission(registration, nil, envelope, now)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(decision.Disposition).To(Equal(types.ObservationDispositionAccepted))
	g.Expect(decision.AdvanceHead).To(BeTrue())
	g.Expect(decision.Trusted).To(BeTrue())
}

func TestObservationAdmissionRejectsObserverMismatchAndUntrustedCredential(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 7, 18, 5, 0, 0, 0, time.UTC)
	registration, envelope := validObservation(now)
	envelope.ObserverID = uuid.New()

	_, err := EvaluateAdmission(registration, nil, envelope, now)
	g.Expect(errors.Is(err, ErrObserverMismatch)).To(BeTrue())

	envelope.ObserverID = registration.ID
	envelope.CredentialFingerprint = digest("wrong")
	_, err = EvaluateAdmission(registration, nil, envelope, now)
	g.Expect(errors.Is(err, ErrUntrustedObservation)).To(BeTrue())
}

func TestObservationAdmissionRejectsStaleEvidenceByCapturedTime(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 7, 18, 5, 0, 0, 0, time.UTC)
	registration, envelope := validObservation(now)
	envelope.CapturedAt = now.Add(-registration.MaxFreshness - time.Second)

	decision, err := EvaluateAdmission(registration, nil, envelope, now)

	g.Expect(errors.Is(err, ErrStaleObservation)).To(BeTrue())
	g.Expect(decision.Disposition).To(Equal(types.ObservationDispositionRejected))
	g.Expect(decision.RetainEvidence).To(BeTrue())
}

func TestObservationAdmissionMakesReplayIdempotentAndRetainsOutOfOrderEvidence(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 7, 18, 5, 0, 0, 0, time.UTC)
	registration, envelope := validObservation(now)
	head := &types.ComponentObservationHead{
		ObserverID:       envelope.ObserverID,
		SourceSequence:   envelope.SourceSequence,
		EvidenceChecksum: envelope.EvidenceChecksum,
		CapturedAt:       envelope.CapturedAt,
	}

	replay, err := EvaluateAdmission(registration, head, envelope, now)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(replay.Disposition).To(Equal(types.ObservationDispositionReplay))
	g.Expect(replay.AdvanceHead).To(BeFalse())

	envelope.SourceSequence--
	envelope.EvidenceChecksum = digest("older")
	outOfOrder, err := EvaluateAdmission(registration, head, envelope, now)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(outOfOrder.Disposition).To(Equal(types.ObservationDispositionOutOfOrder))
	g.Expect(outOfOrder.RetainEvidence).To(BeTrue())
	g.Expect(outOfOrder.AdvanceHead).To(BeFalse())
}

func TestObservationAdmissionRetainsConflictingReplayForReconciliation(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 7, 18, 5, 0, 0, 0, time.UTC)
	registration, envelope := validObservation(now)
	head := &types.ComponentObservationHead{
		ObserverID:       envelope.ObserverID,
		SourceSequence:   envelope.SourceSequence,
		EvidenceChecksum: digest("different"),
		CapturedAt:       envelope.CapturedAt,
	}

	decision, err := EvaluateAdmission(registration, head, envelope, now)

	g.Expect(errors.Is(err, ErrConflictingReplay)).To(BeTrue())
	g.Expect(decision.Disposition).To(Equal(types.ObservationDispositionConflict))
	g.Expect(decision.RetainEvidence).To(BeTrue())
	g.Expect(decision.Quarantine).To(BeTrue())
}

func validObservation(now time.Time) (types.ObserverRegistration, types.ObservationEnvelope) {
	registration := types.ObserverRegistration{
		ID:                    uuid.New(),
		OrganizationID:        uuid.New(),
		DeploymentUnitID:      uuid.New(),
		ObserverKey:           "runtime-http-json",
		Enabled:               true,
		CredentialFingerprint: digest("credential"),
		MaxFreshness:          2 * time.Minute,
		MaxClockSkew:          15 * time.Second,
		Measurements:          []string{"artifact", "config", "schema", "health"},
	}
	envelope := types.ObservationEnvelope{
		OrganizationID:        registration.OrganizationID,
		ObserverID:            registration.ID,
		DeploymentUnitID:      registration.DeploymentUnitID,
		ComponentInstanceID:   uuid.New(),
		ComponentKey:          "api",
		SourceSequence:        4,
		CapturedAt:            now.Add(-10 * time.Second),
		CredentialFingerprint: registration.CredentialFingerprint,
		EvidenceChecksum:      digest("evidence"),
		ArtifactDigest:        digest("artifact"),
		ConfigChecksum:        digest("config"),
		SchemaVersion:         "2026071801",
		CapabilityChecksum:    digest("capability"),
		Platform:              "linux/amd64",
		TopologyChecksum:      digest("topology"),
		Health:                types.ObservedHealthHealthy,
		Outcome:               types.ObservationOutcomeComplete,
	}
	return registration, envelope
}

func digest(value string) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(value)))
}
