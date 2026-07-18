package planning

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

const maxBaselineCandidates = 256

func SelectVerifiedBaseline(
	_ context.Context,
	query types.BaselineQuery,
) (*types.DeploymentPlanBaseline, error) {
	if query.OrganizationID == uuid.Nil || query.DeploymentUnitID == uuid.Nil ||
		query.ComponentInstanceID == uuid.Nil {
		return nil, fmt.Errorf("organization, deployment unit, and component instance are required")
	}
	componentKey := strings.TrimSpace(query.ComponentKey)
	if componentKey == "" || len(componentKey) > 128 {
		return nil, fmt.Errorf("component key is invalid")
	}
	if query.ExpectedDesiredRevision < 1 ||
		!planChecksumPattern.MatchString(query.ExpectedDesiredChecksum) {
		return nil, fmt.Errorf("active desired revision and checksum are invalid")
	}
	if len(query.Candidates) > maxBaselineCandidates {
		return nil, fmt.Errorf("baseline candidate limit exceeded")
	}

	candidates := slices.Clone(query.Candidates)
	slices.SortFunc(candidates, func(a, b types.BaselineCandidate) int {
		if cmp := b.ObservedAt.Compare(a.ObservedAt); cmp != 0 {
			return cmp
		}
		if a.ObservedRevision != b.ObservedRevision {
			if a.ObservedRevision > b.ObservedRevision {
				return -1
			}
			return 1
		}
		return strings.Compare(b.ObservationID.String(), a.ObservationID.String())
	})
	for _, candidate := range candidates {
		if !baselineCandidateMatches(query, candidate) {
			continue
		}
		projection := types.BaselineProjectionLegacy
		authoritative := candidate.PlanSchema == types.TargetDeploymentPlanSchemaV2 &&
			candidate.ProtocolVersion == types.DeploymentPlanProtocolV2 &&
			candidate.PlanFactsMatch &&
			candidate.SourceDeploymentPlanID != nil &&
			candidate.ExternalExecutionID != nil
		if authoritative {
			projection = types.BaselineProjectionVerifiedV2
		}
		observationChecksum, err := canonicalChecksum(struct {
			ID              uuid.UUID `json:"id"`
			ObservedAt      string    `json:"observedAt"`
			DesiredRevision int64     `json:"desiredRevision"`
			DesiredChecksum string    `json:"desiredChecksum"`
			ReleaseBundleID uuid.UUID `json:"releaseBundleId"`
			Version         string    `json:"version"`
			Image           string    `json:"image"`
			Platform        string    `json:"platform"`
			ConfigChecksum  string    `json:"configChecksum"`
		}{
			ID: candidate.ObservationID, ObservedAt: candidate.ObservedAt.UTC().Format("2006-01-02T15:04:05.999999999Z"),
			DesiredRevision: candidate.DesiredRevision, DesiredChecksum: candidate.DesiredChecksum,
			ReleaseBundleID: candidate.ReleaseBundleID, Version: strings.TrimSpace(candidate.Version),
			Image: strings.TrimSpace(candidate.Image), Platform: candidate.Platform,
			ConfigChecksum: candidate.ConfigChecksum,
		})
		if err != nil {
			return nil, fmt.Errorf("checksum baseline observation: %w", err)
		}
		observationID := candidate.ObservationID
		observedAt := candidate.ObservedAt.UTC()
		releaseID := candidate.ReleaseBundleID
		return &types.DeploymentPlanBaseline{
			ComponentInstanceID: query.ComponentInstanceID, ComponentKey: componentKey,
			SourceDeploymentPlanID: cloneUUID(candidate.SourceDeploymentPlanID),
			ExternalExecutionID:    cloneUUID(candidate.ExternalExecutionID),
			ObservationID:          &observationID, ObservedAt: &observedAt,
			DesiredRevision: candidate.DesiredRevision, DesiredChecksum: candidate.DesiredChecksum,
			ObservationChecksum: observationChecksum, ReleaseBundleID: &releaseID,
			Version: strings.TrimSpace(candidate.Version), Image: strings.TrimSpace(candidate.Image),
			Platform:                strings.TrimSpace(candidate.Platform),
			ConfigSnapshotID:        cloneUUID(candidate.ConfigSnapshotID),
			ConfigChecksum:          candidate.ConfigChecksum,
			ProviderBindingChecksum: candidate.ProviderBindingChecksum,
			SchemaState:             strings.TrimSpace(candidate.SchemaState),
			SchemaChecksum:          candidate.SchemaChecksum,
			TopologyChecksum:        candidate.TopologyChecksum,
			Projection:              projection, AuthorizesV2Execution: authoritative,
		}, nil
	}

	return &types.DeploymentPlanBaseline{
		ComponentInstanceID: query.ComponentInstanceID,
		ComponentKey:        componentKey,
		DesiredRevision:     query.ExpectedDesiredRevision,
		DesiredChecksum:     query.ExpectedDesiredChecksum,
		Projection:          types.BaselineProjectionBootstrap,
		Bootstrap:           true,
	}, nil
}

func baselineCandidateMatches(query types.BaselineQuery, candidate types.BaselineCandidate) bool {
	return candidate.ObservationID != uuid.Nil &&
		!candidate.ObservedAt.IsZero() &&
		candidate.Health == types.TargetComponentHealthHealthy &&
		candidate.DesiredRevision == query.ExpectedDesiredRevision &&
		candidate.DesiredChecksum == query.ExpectedDesiredChecksum &&
		candidate.ObservedRevision == candidate.DesiredRevision &&
		candidate.ObservedChecksum == candidate.DesiredChecksum &&
		candidate.ReleaseBundleID != uuid.Nil &&
		boundedNonEmpty(candidate.Version, 128) &&
		boundedNonEmpty(candidate.Image, 4096) &&
		(candidate.Platform == "linux/amd64" || candidate.Platform == "linux/arm64") &&
		planChecksumPattern.MatchString(candidate.ConfigChecksum) &&
		optionalChecksum(candidate.ProviderBindingChecksum) &&
		len(candidate.SchemaState) <= 4096 &&
		optionalChecksum(candidate.SchemaChecksum) &&
		optionalChecksum(candidate.TopologyChecksum)
}

func boundedNonEmpty(value string, maximum int) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= maximum
}

func optionalChecksum(value string) bool {
	return value == "" || planChecksumPattern.MatchString(value)
}
