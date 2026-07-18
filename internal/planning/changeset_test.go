package planning

import (
	"slices"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestBuildTargetChangeSetReturnsExactStableChangesAndSkippedNotes(t *testing.T) {
	g := NewWithT(t)
	baseReleaseID := uuid.New()
	skippedReleaseID := uuid.New()
	desiredReleaseID := uuid.New()
	componentInstanceID := uuid.New()
	baseline := types.BaselineState{
		ComponentInstanceID: componentInstanceID, ComponentKey: "loyalty-api",
		ReleaseBundleID: baseReleaseID, Version: "1.0.0",
		Image:          "registry.example/loyalty@" + testChecksum("1"),
		ConfigChecksum: testChecksum("2"), ProviderBindingChecksum: testChecksum("3"),
		SchemaState: "schema-10", SchemaChecksum: testChecksum("4"),
		TopologyChecksum: testChecksum("5"),
	}
	planned := types.PlannedState{
		ComponentInstanceID: componentInstanceID, ComponentKey: "loyalty-api",
		ReleaseBundleID: desiredReleaseID, Version: "3.0.0",
		Image:          "registry.example/loyalty@" + testChecksum("6"),
		ConfigChecksum: testChecksum("7"), ProviderBindingChecksum: testChecksum("8"),
		SchemaState: "schema-12", SchemaChecksum: testChecksum("9"),
		TopologyChecksum: testChecksum("a"), ForwardOnly: true,
	}
	now := time.Now().UTC()
	notes := []types.ReleaseNote{
		{ReleaseBundleID: desiredReleaseID, PublishedAt: now.Add(2 * time.Hour), Summary: "3.0"},
		{ReleaseBundleID: baseReleaseID, PublishedAt: now, Summary: "1.0"},
		{ReleaseBundleID: skippedReleaseID, PublishedAt: now.Add(time.Hour), Summary: "2.0"},
	}

	changes := BuildTargetChangeSet(baseline, planned, notes)
	reversed := slices.Clone(notes)
	slices.Reverse(reversed)
	again := BuildTargetChangeSet(baseline, planned, reversed)

	g.Expect(changes).To(Equal(again))
	g.Expect(changeKinds(changes)).To(Equal([]types.DeploymentPlanChangeKind{
		types.DeploymentPlanChangeImage,
		types.DeploymentPlanChangeConfig,
		types.DeploymentPlanChangeProvider,
		types.DeploymentPlanChangeSchema,
		types.DeploymentPlanChangeTopology,
		types.DeploymentPlanChangeSourceNotes,
	}))
	g.Expect(changes[3].ForwardOnly).To(BeTrue())
	g.Expect(changes[5].ReleaseNotes).To(HaveLen(2))
	g.Expect(changes[5].ReleaseNotes[0].ReleaseBundleID).To(Equal(skippedReleaseID))
	g.Expect(changes[5].ReleaseNotes[1].ReleaseBundleID).To(Equal(desiredReleaseID))
	for i := range changes {
		g.Expect(changes[i].SortOrder).To(Equal(i))
	}
}

func TestBuildTargetChangeSetLabelsBootstrap(t *testing.T) {
	g := NewWithT(t)
	planned := types.PlannedState{
		ComponentInstanceID: uuid.New(), ComponentKey: "new-worker",
		ReleaseBundleID: uuid.New(), Version: "1.0.0",
		Image:          "registry.example/worker@" + testChecksum("b"),
		ConfigChecksum: testChecksum("c"),
	}

	changes := BuildTargetChangeSet(
		types.BaselineState{
			Bootstrap:           true,
			ComponentInstanceID: planned.ComponentInstanceID,
			ComponentKey:        planned.ComponentKey,
		},
		planned,
		nil,
	)

	g.Expect(changes).To(HaveLen(1))
	g.Expect(changes[0].Kind).To(Equal(types.DeploymentPlanChangeBootstrap))
	g.Expect(changes[0].After).To(Equal("1.0.0"))
}

func changeKinds(changes []types.DeploymentPlanChangeEntry) []types.DeploymentPlanChangeKind {
	result := make([]types.DeploymentPlanChangeKind, len(changes))
	for i := range changes {
		result[i] = changes[i].Kind
	}
	return result
}
