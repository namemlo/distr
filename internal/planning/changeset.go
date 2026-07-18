package planning

import (
	"fmt"
	"slices"
	"strings"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

const maxReleaseNotesPerChangeSet = 128

func BuildTargetChangeSet(
	baseline types.BaselineState,
	planned types.PlannedState,
	releaseNotes []types.ReleaseNote,
) []types.DeploymentPlanChangeEntry {
	componentKey := boundedText(strings.TrimSpace(planned.ComponentKey), 128)
	componentInstanceID := planned.ComponentInstanceID
	changes := make([]types.DeploymentPlanChangeEntry, 0, 8)
	add := func(kind types.DeploymentPlanChangeKind, before, after string, forwardOnly bool) {
		changes = append(changes, types.DeploymentPlanChangeEntry{
			ComponentInstanceID: componentInstanceID,
			ComponentKey:        componentKey,
			Kind:                kind,
			Before:              boundedText(before, 4096),
			After:               boundedText(after, 4096),
			ForwardOnly:         forwardOnly,
		})
	}

	if baseline.Bootstrap {
		add(types.DeploymentPlanChangeBootstrap, "", planned.Version, false)
	}
	if !baseline.Bootstrap &&
		baseline.Projection != "" &&
		baseline.Projection != types.BaselineProjectionVerifiedV2 {
		add(
			types.DeploymentPlanChangeBaselineAuthority,
			string(baseline.Projection),
			string(types.BaselineProjectionVerifiedV2),
			false,
		)
	}
	if baseline.ReleaseBundleID != planned.ReleaseBundleID ||
		baseline.Version != planned.Version ||
		baseline.Image != planned.Image ||
		baseline.Platform != planned.Platform {
		add(
			types.DeploymentPlanChangeImage,
			imageChangeValue(baseline.Version, baseline.Image, baseline.Platform),
			imageChangeValue(planned.Version, planned.Image, planned.Platform),
			false,
		)
	}
	if baseline.ConfigChecksum != planned.ConfigChecksum ||
		!equalUUID(baseline.ConfigSnapshotID, planned.ConfigSnapshotID) {
		add(
			types.DeploymentPlanChangeConfig,
			configChangeValue(baseline.ConfigSnapshotID, baseline.ConfigChecksum),
			configChangeValue(planned.ConfigSnapshotID, planned.ConfigChecksum),
			false,
		)
	}
	if baseline.ProviderBindingChecksum != planned.ProviderBindingChecksum {
		add(
			types.DeploymentPlanChangeProvider,
			baseline.ProviderBindingChecksum,
			planned.ProviderBindingChecksum,
			false,
		)
	}
	if baseline.SchemaState != planned.SchemaState ||
		baseline.SchemaChecksum != planned.SchemaChecksum {
		add(
			types.DeploymentPlanChangeSchema,
			baseline.SchemaState+"|"+baseline.SchemaChecksum,
			planned.SchemaState+"|"+planned.SchemaChecksum,
			planned.ForwardOnly,
		)
	}
	if baseline.TopologyChecksum != planned.TopologyChecksum {
		add(
			types.DeploymentPlanChangeTopology,
			baseline.TopologyChecksum,
			planned.TopologyChecksum,
			false,
		)
	}
	notes, exceeded := accumulatedReleaseNotes(
		baseline.ReleaseBundleID,
		planned.ReleaseBundleID,
		releaseNotes,
	)
	if len(notes) > 0 {
		changes = append(changes, types.DeploymentPlanChangeEntry{
			ComponentInstanceID: componentInstanceID,
			ComponentKey:        componentKey,
			Kind:                types.DeploymentPlanChangeSourceNotes,
			Before:              baseline.ReleaseBundleID.String(),
			After:               planned.ReleaseBundleID.String(),
			ReleaseNotes:        notes,
		})
	}
	if exceeded {
		add(
			types.DeploymentPlanChangeLimitExceeded,
			fmt.Sprintf("%d", len(releaseNotes)),
			fmt.Sprintf("maximum %d", maxReleaseNotesPerChangeSet),
			false,
		)
	}
	finalizeChangeOrder(changes)
	return changes
}

func accumulatedReleaseNotes(
	baselineReleaseID,
	plannedReleaseID uuid.UUID,
	source []types.ReleaseNote,
) ([]types.ReleaseNote, bool) {
	notes := slices.Clone(source)
	slices.SortFunc(notes, func(a, b types.ReleaseNote) int {
		if cmp := a.PublishedAt.Compare(b.PublishedAt); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.ReleaseBundleID.String(), b.ReleaseBundleID.String())
	})
	baselineIndex := -1
	plannedIndex := -1
	for index := range notes {
		if notes[index].ReleaseBundleID.String() == baselineReleaseID.String() {
			baselineIndex = index
		}
		if notes[index].ReleaseBundleID.String() == plannedReleaseID.String() {
			plannedIndex = index
		}
	}
	if plannedIndex < 0 {
		return nil, len(notes) > maxReleaseNotesPerChangeSet
	}
	if plannedIndex <= baselineIndex {
		return nil, false
	}
	start := 0
	if baselineIndex >= 0 {
		start = baselineIndex + 1
	}
	result := slices.Clone(notes[start : plannedIndex+1])
	exceeded := len(result) > maxReleaseNotesPerChangeSet
	if exceeded {
		result = result[len(result)-maxReleaseNotesPerChangeSet:]
	}
	for index := range result {
		result[index].Version = boundedText(strings.TrimSpace(result[index].Version), 128)
		result[index].SourceRevision = boundedText(strings.TrimSpace(result[index].SourceRevision), 512)
		result[index].Summary = boundedText(strings.TrimSpace(result[index].Summary), 8192)
		result[index].PublishedAt = result[index].PublishedAt.UTC()
	}
	return result, exceeded
}

func finalizeChangeOrder(changes []types.DeploymentPlanChangeEntry) {
	for index := range changes {
		changes[index].SortOrder = index
		checksum, err := canonicalChecksum(struct {
			ComponentInstanceID string                         `json:"componentInstanceId"`
			ComponentKey        string                         `json:"componentKey"`
			Kind                types.DeploymentPlanChangeKind `json:"kind"`
			Before              string                         `json:"before"`
			After               string                         `json:"after"`
			ReleaseNotes        []types.ReleaseNote            `json:"releaseNotes,omitempty"`
			ForwardOnly         bool                           `json:"forwardOnly"`
			SortOrder           int                            `json:"sortOrder"`
		}{
			ComponentInstanceID: changes[index].ComponentInstanceID.String(),
			ComponentKey:        changes[index].ComponentKey, Kind: changes[index].Kind,
			Before: changes[index].Before, After: changes[index].After,
			ReleaseNotes: changes[index].ReleaseNotes,
			ForwardOnly:  changes[index].ForwardOnly, SortOrder: index,
		})
		if err == nil {
			changes[index].CanonicalChecksum = checksum
		}
	}
}

func imageChangeValue(version, image, platform string) string {
	return boundedText(strings.TrimSpace(version)+"|"+strings.TrimSpace(platform)+"|"+strings.TrimSpace(image), 4096)
}

func configChangeValue(snapshotID *uuid.UUID, checksum string) string {
	if snapshotID == nil {
		return "|" + checksum
	}
	return snapshotID.String() + "|" + checksum
}

func equalUUID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.String() == b.String()
}

func boundedText(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	return value[:maximum]
}
