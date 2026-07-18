package desiredstate

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestDesiredStateAdmissionDoesNotReplaceActiveRevision(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 7, 18, 4, 0, 0, 0, time.UTC)
	active := types.ActiveDesiredRevision{ID: uuid.New(), Revision: 4, ArtifactDigest: digest("a")}
	input := validPendingInput()

	pending, nextActive, err := Admit(input, &active, now)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(pending.Revision).To(Equal(int64(5)))
	g.Expect(pending.Status).To(Equal(types.PendingDesiredStatusPending))
	g.Expect(nextActive).To(Equal(&active))
}

func TestDesiredStateAdvancesOnlyAfterIndependentVerification(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 7, 18, 4, 0, 0, 0, time.UTC)
	previous := types.ActiveDesiredRevision{ID: uuid.New(), Revision: 2, ArtifactDigest: digest("old")}
	pending, _, err := Admit(validPendingInput(), &previous, now.Add(-time.Minute))
	g.Expect(err).NotTo(HaveOccurred())

	active, updatedPending, err := Advance(&previous, *pending, types.ObservationGateResult{
		Status:              types.ObservationGateStatusVerified,
		ObservationID:       uuid.New(),
		ObservationChecksum: digest("observation"),
	}, now)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(active.Revision).To(Equal(int64(3)))
	g.Expect(active.ArtifactDigest).To(Equal(pending.ArtifactDigest))
	g.Expect(active.VerifiedObservationID).To(Equal(updatedPending.VerifiedObservationID))
	g.Expect(updatedPending.Status).To(Equal(types.PendingDesiredStatusVerified))
}

func TestDesiredStateTerminalFailurePreservesPriorActive(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 7, 18, 4, 0, 0, 0, time.UTC)
	previous := types.ActiveDesiredRevision{ID: uuid.New(), Revision: 7, ArtifactDigest: digest("old")}
	pending, _, err := Admit(validPendingInput(), &previous, now.Add(-time.Minute))
	g.Expect(err).NotTo(HaveOccurred())

	active, updatedPending, err := Advance(&previous, *pending, types.ObservationGateResult{
		Status: types.ObservationGateStatusFailed,
		Reason: "executor failed before independent verification",
	}, now)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(active).To(Equal(&previous))
	g.Expect(updatedPending.Status).To(Equal(types.PendingDesiredStatusFailed))
	g.Expect(updatedPending.TerminalReason).To(Equal("executor failed before independent verification"))
}

func TestDesiredStateCancellationPreservesPriorActive(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 7, 18, 4, 0, 0, 0, time.UTC)
	previous := types.ActiveDesiredRevision{
		ID: uuid.New(), Revision: 7, ArtifactDigest: digest("old"),
	}
	pending, _, err := Admit(validPendingInput(), &previous, now.Add(-time.Minute))
	g.Expect(err).NotTo(HaveOccurred())

	active, updatedPending, err := Advance(
		&previous,
		*pending,
		types.ObservationGateResult{
			Status: types.ObservationGateStatusCancelled,
			Reason: "executor cancellation acknowledged",
		},
		now,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(active).To(Equal(&previous))
	g.Expect(updatedPending.Status).To(Equal(types.PendingDesiredStatusCancelled))
}

func validPendingInput() types.PendingDesiredRevisionInput {
	return types.PendingDesiredRevisionInput{
		OrganizationID:      uuid.New(),
		DeploymentPlanID:    uuid.New(),
		ExecutionID:         uuid.New(),
		DeploymentUnitID:    uuid.New(),
		ComponentInstanceID: uuid.New(),
		ComponentKey:        "api",
		ArtifactDigest:      digest("artifact"),
		ConfigChecksum:      digest("config"),
		SchemaVersion:       "2026071801",
		CapabilityChecksum:  digest("capability"),
		Platform:            "linux/amd64",
		TopologyChecksum:    digest("topology"),
		ObservationDeadline: time.Date(2026, 7, 18, 4, 10, 0, 0, time.UTC),
	}
}

func digest(value string) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(value)))
}
