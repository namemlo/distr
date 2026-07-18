package reconciliation

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestClassifyDriftSeparatesArtifactConfigSchemaAndRuntimeHealth(t *testing.T) {
	active, observed := matchingState()
	tests := []struct {
		name   string
		mutate func(*types.ObservedComponentState)
		class  types.DriftClass
	}{
		{"artifact", func(o *types.ObservedComponentState) { o.ArtifactDigest = digest("other") }, types.DriftClassArtifact},
		{
			"config",
			func(o *types.ObservedComponentState) { o.ConfigChecksum = digest("other") },
			types.DriftClassConfiguration,
		},
		{"schema", func(o *types.ObservedComponentState) { o.SchemaVersion = "other" }, types.DriftClassSchema},
		{
			"health",
			func(o *types.ObservedComponentState) { o.Health = types.ObservedHealthUnhealthy },
			types.DriftClassHealth,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			actual := observed
			tt.mutate(&actual)
			classification := ClassifyDrift(active, actual)
			g.Expect(classification.Classes).To(ContainElement(tt.class))
			g.Expect(classification.Drifted).To(BeTrue())
		})
	}
}

func TestClassifyDriftFlagsExecutorSuccessRuntimeWrong(t *testing.T) {
	g := NewWithT(t)
	active, observed := matchingState()
	observed.ExecutorOutcome = types.ExecutorOutcomeSucceeded
	observed.ArtifactDigest = digest("wrong")

	classification := ClassifyDrift(active, observed)

	g.Expect(classification.Classes).To(ContainElement(types.DriftClassArtifact))
	g.Expect(classification.Classes).To(ContainElement(types.DriftClassExecutorMismatch))
}

func TestAcceptedDeviationIsTimeBoundAndDoesNotRewriteDesiredState(t *testing.T) {
	g := NewWithT(t)
	active, observed := matchingState()
	originalDigest := active.ArtifactDigest
	now := time.Date(2026, 7, 18, 7, 0, 0, 0, time.UTC)
	observed.ArtifactDigest = digest("temporary")
	decision := types.ReconciliationDecision{
		Action:        types.ReconciliationActionAcceptDeviation,
		Reason:        "temporary vendor incident",
		AcceptedUntil: new(now.Add(time.Hour)),
	}

	exception, err := AcceptDeviation(active, observed, decision, now)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(exception.ExpiresAt).To(Equal(now.Add(time.Hour)))
	g.Expect(exception.DesiredRevisionID).To(Equal(active.ID))
	g.Expect(active.ArtifactDigest).To(Equal(originalDigest))
}

func matchingState() (types.ActiveDesiredRevision, types.ObservedComponentState) {
	active := types.ActiveDesiredRevision{
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
	}
	observed := types.ObservedComponentState{
		ID:                  uuid.New(),
		OrganizationID:      active.OrganizationID,
		DeploymentUnitID:    active.DeploymentUnitID,
		ComponentInstanceID: active.ComponentInstanceID,
		ComponentKey:        active.ComponentKey,
		ArtifactDigest:      active.ArtifactDigest,
		ConfigChecksum:      active.ConfigChecksum,
		SchemaVersion:       active.SchemaVersion,
		CapabilityChecksum:  active.CapabilityChecksum,
		Platform:            active.Platform,
		TopologyChecksum:    active.TopologyChecksum,
		Health:              types.ObservedHealthHealthy,
		Outcome:             types.ObservationOutcomeComplete,
		Trusted:             true,
		Current:             true,
	}
	return active, observed
}

func digest(value string) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(value)))
}
