package api

import (
	"encoding/json"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

type DeploymentTimelineQueryRequest struct {
	ApplicationID          *uuid.UUID `query:"applicationId"`
	ReleaseBundleID        *uuid.UUID `query:"releaseBundleId"`
	EnvironmentID          *uuid.UUID `query:"environmentId"`
	DeploymentTargetID     *uuid.UUID `query:"deploymentTargetId"`
	CustomerOrganizationID *uuid.UUID `query:"customerOrganizationId"`
	Limit                  int        `query:"limit"`
	IncludeNonTerminal     bool       `query:"includeNonTerminal"`
}

type DeploymentTimelineCompareQueryRequest struct {
	BaseTaskID                        *uuid.UUID `query:"baseTaskId"`
	BaseLegacyDeploymentRevisionID    *uuid.UUID `query:"baseLegacyDeploymentRevisionId"`
	CompareTaskID                     *uuid.UUID `query:"compareTaskId"`
	CompareLegacyDeploymentRevisionID *uuid.UUID `query:"compareLegacyDeploymentRevisionId"`
}

type DeploymentTimeline struct {
	Items []DeploymentTimelineItem `json:"items"`
}

type DeploymentTimelineItem struct {
	Source                     types.DeploymentTimelineItemSource        `json:"source"`
	TaskID                     uuid.UUID                                 `json:"taskId"`
	LegacyDeploymentID         uuid.UUID                                 `json:"legacyDeploymentId,omitempty"`
	LegacyDeploymentRevisionID uuid.UUID                                 `json:"legacyDeploymentRevisionId,omitempty"`
	SyntheticReleaseID         uuid.UUID                                 `json:"syntheticReleaseId,omitempty"`
	DeploymentPlanID           uuid.UUID                                 `json:"deploymentPlanId"`
	DeploymentPlanTargetID     uuid.UUID                                 `json:"deploymentPlanTargetId"`
	DeploymentTargetID         uuid.UUID                                 `json:"deploymentTargetId"`
	ApplicationID              uuid.UUID                                 `json:"applicationId"`
	ApplicationName            string                                    `json:"applicationName"`
	ReleaseBundleID            uuid.UUID                                 `json:"releaseBundleId"`
	ReleaseNumber              string                                    `json:"releaseNumber"`
	ChannelID                  uuid.UUID                                 `json:"channelId"`
	ChannelName                string                                    `json:"channelName"`
	EnvironmentID              uuid.UUID                                 `json:"environmentId"`
	EnvironmentName            string                                    `json:"environmentName"`
	CustomerOrganizationID     *uuid.UUID                                `json:"customerOrganizationId,omitempty"`
	DeploymentTargetName       string                                    `json:"deploymentTargetName"`
	ActorUserAccountID         *uuid.UUID                                `json:"actorUserAccountId,omitempty"`
	Status                     types.TaskStatus                          `json:"status"`
	QueuedAt                   time.Time                                 `json:"queuedAt"`
	StartedAt                  *time.Time                                `json:"startedAt,omitempty"`
	CompletedAt                *time.Time                                `json:"completedAt,omitempty"`
	ProcessSnapshotID          *uuid.UUID                                `json:"processSnapshotId,omitempty"`
	VariableSnapshotID         *uuid.UUID                                `json:"variableSnapshotId,omitempty"`
	Availability               types.DeploymentCompatibilityAvailability `json:"availability"`
	Components                 []DeploymentTimelineComponent             `json:"components"`
	LastSuccessful             bool                                      `json:"lastSuccessful"`
	RedeployAvailable          bool                                      `json:"redeployAvailable"`
}

type DeploymentTimelineComponent struct {
	Key     string                           `json:"key"`
	Name    string                           `json:"name"`
	Type    types.ReleaseBundleComponentType `json:"type"`
	Version string                           `json:"version"`
}

type DeploymentTimelineComparison struct {
	Base       DeploymentTimelineItem              `json:"base"`
	Compare    DeploymentTimelineItem              `json:"compare"`
	Process    DeploymentTimelineProcessChange     `json:"process"`
	Components []DeploymentTimelineComponentChange `json:"components"`
	Steps      []DeploymentTimelineStepChange      `json:"steps"`
	Variables  []DeploymentTimelineVariableChange  `json:"variables"`
}

type DeploymentTimelineProcessChange struct {
	BaseProcessSnapshotID    *uuid.UUID `json:"baseProcessSnapshotId,omitempty"`
	CompareProcessSnapshotID *uuid.UUID `json:"compareProcessSnapshotId,omitempty"`
	BaseRevisionNumber       int        `json:"baseRevisionNumber,omitempty"`
	CompareRevisionNumber    int        `json:"compareRevisionNumber,omitempty"`
	BaseCanonicalChecksum    string     `json:"baseCanonicalChecksum,omitempty"`
	CompareCanonicalChecksum string     `json:"compareCanonicalChecksum,omitempty"`
	Changed                  bool       `json:"changed"`
}

type DeploymentTimelineComponentChange struct {
	Key            string                             `json:"key"`
	Name           string                             `json:"name"`
	Kind           types.DeploymentTimelineChangeKind `json:"kind"`
	BaseVersion    string                             `json:"baseVersion,omitempty"`
	CompareVersion string                             `json:"compareVersion,omitempty"`
	BaseType       types.ReleaseBundleComponentType   `json:"baseType,omitempty"`
	CompareType    types.ReleaseBundleComponentType   `json:"compareType,omitempty"`
}

type DeploymentTimelineStepChange struct {
	StepKey           string                             `json:"stepKey"`
	Name              string                             `json:"name"`
	Kind              types.DeploymentTimelineChangeKind `json:"kind"`
	BaseActionType    string                             `json:"baseActionType,omitempty"`
	CompareActionType string                             `json:"compareActionType,omitempty"`
	BaseIncluded      *bool                              `json:"baseIncluded,omitempty"`
	CompareIncluded   *bool                              `json:"compareIncluded,omitempty"`
}

type DeploymentTimelineVariableChange struct {
	Key              string                             `json:"key"`
	Kind             types.DeploymentTimelineChangeKind `json:"kind"`
	BaseStatus       types.VariableResolutionStatus     `json:"baseStatus,omitempty"`
	CompareStatus    types.VariableResolutionStatus     `json:"compareStatus,omitempty"`
	BaseSource       types.VariableResolutionSource     `json:"baseSource,omitempty"`
	CompareSource    types.VariableResolutionSource     `json:"compareSource,omitempty"`
	BaseRedacted     bool                               `json:"baseRedacted"`
	CompareRedacted  bool                               `json:"compareRedacted"`
	BaseValue        json.RawMessage                    `json:"baseValue,omitempty"`
	CompareValue     json.RawMessage                    `json:"compareValue,omitempty"`
	BaseReference    string                             `json:"baseReference,omitempty"`
	CompareReference string                             `json:"compareReference,omitempty"`
}

type DeploymentTimelineRedeploy struct {
	Plan    DeploymentPlan `json:"plan"`
	Warning string         `json:"warning"`
}
