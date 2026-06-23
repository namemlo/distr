package types

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type DeploymentTimelineQuery struct {
	OrganizationID         uuid.UUID
	ApplicationID          *uuid.UUID
	ReleaseBundleID        *uuid.UUID
	EnvironmentID          *uuid.UUID
	DeploymentTargetID     *uuid.UUID
	CustomerOrganizationID *uuid.UUID
	Limit                  int
	IncludeNonTerminal     bool
	IncludeRedeployInfo    bool
}

type DeploymentTimeline struct {
	Items []DeploymentTimelineItem `json:"items"`
}

type DeploymentTimelineItemSource string

const (
	DeploymentTimelineItemSourceTask             DeploymentTimelineItemSource = "task"
	DeploymentTimelineItemSourceLegacyDeployment DeploymentTimelineItemSource = "legacy_deployment"
)

type DeploymentTimelineItem struct {
	Source                     DeploymentTimelineItemSource        `json:"source"`
	TaskID                     uuid.UUID                           `json:"taskId"`
	LegacyDeploymentID         uuid.UUID                           `json:"legacyDeploymentId,omitempty"`
	LegacyDeploymentRevisionID uuid.UUID                           `json:"legacyDeploymentRevisionId,omitempty"`
	SyntheticReleaseID         uuid.UUID                           `json:"syntheticReleaseId,omitempty"`
	DeploymentPlanID           uuid.UUID                           `json:"deploymentPlanId"`
	DeploymentPlanTargetID     uuid.UUID                           `json:"deploymentPlanTargetId"`
	DeploymentTargetID         uuid.UUID                           `json:"deploymentTargetId"`
	ApplicationID              uuid.UUID                           `json:"applicationId"`
	ApplicationName            string                              `json:"applicationName"`
	ReleaseBundleID            uuid.UUID                           `json:"releaseBundleId"`
	ReleaseNumber              string                              `json:"releaseNumber"`
	ChannelID                  uuid.UUID                           `json:"channelId"`
	ChannelName                string                              `json:"channelName"`
	EnvironmentID              uuid.UUID                           `json:"environmentId"`
	EnvironmentName            string                              `json:"environmentName"`
	CustomerOrganizationID     *uuid.UUID                          `json:"customerOrganizationId,omitempty"`
	DeploymentTargetName       string                              `json:"deploymentTargetName"`
	ActorUserAccountID         *uuid.UUID                          `json:"actorUserAccountId,omitempty"`
	Status                     TaskStatus                          `json:"status"`
	QueuedAt                   time.Time                           `json:"queuedAt"`
	StartedAt                  *time.Time                          `json:"startedAt,omitempty"`
	CompletedAt                *time.Time                          `json:"completedAt,omitempty"`
	ProcessSnapshotID          *uuid.UUID                          `json:"processSnapshotId,omitempty"`
	VariableSnapshotID         *uuid.UUID                          `json:"variableSnapshotId,omitempty"`
	Availability               DeploymentCompatibilityAvailability `json:"availability"`
	Components                 []DeploymentTimelineComponent       `json:"components"`
	LastSuccessful             bool                                `json:"lastSuccessful"`
	RedeployAvailable          bool                                `json:"redeployAvailable"`
}

type DeploymentTimelineComponent struct {
	Key     string                     `json:"key"`
	Name    string                     `json:"name"`
	Type    ReleaseBundleComponentType `json:"type"`
	Version string                     `json:"version"`
}

type DeploymentTimelineCompareRequest struct {
	OrganizationID                    uuid.UUID
	BaseTaskID                        uuid.UUID
	BaseLegacyDeploymentRevisionID    uuid.UUID
	CompareTaskID                     uuid.UUID
	CompareLegacyDeploymentRevisionID uuid.UUID
}

type DeploymentTimelineComparison struct {
	Base       DeploymentTimelineItem              `json:"base"`
	Compare    DeploymentTimelineItem              `json:"compare"`
	Process    DeploymentTimelineProcessChange     `json:"process"`
	Components []DeploymentTimelineComponentChange `json:"components"`
	Steps      []DeploymentTimelineStepChange      `json:"steps"`
	Variables  []DeploymentTimelineVariableChange  `json:"variables"`
}

type DeploymentTimelineChangeKind string

const (
	DeploymentTimelineChangeUnchanged DeploymentTimelineChangeKind = "unchanged"
	DeploymentTimelineChangeAdded     DeploymentTimelineChangeKind = "added"
	DeploymentTimelineChangeRemoved   DeploymentTimelineChangeKind = "removed"
	DeploymentTimelineChangeChanged   DeploymentTimelineChangeKind = "changed"
)

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
	Key            string                       `json:"key"`
	Name           string                       `json:"name"`
	Kind           DeploymentTimelineChangeKind `json:"kind"`
	BaseVersion    string                       `json:"baseVersion,omitempty"`
	CompareVersion string                       `json:"compareVersion,omitempty"`
	BaseType       ReleaseBundleComponentType   `json:"baseType,omitempty"`
	CompareType    ReleaseBundleComponentType   `json:"compareType,omitempty"`
}

type DeploymentTimelineStepChange struct {
	StepKey           string                       `json:"stepKey"`
	Name              string                       `json:"name"`
	Kind              DeploymentTimelineChangeKind `json:"kind"`
	BaseActionType    string                       `json:"baseActionType,omitempty"`
	CompareActionType string                       `json:"compareActionType,omitempty"`
	BaseIncluded      *bool                        `json:"baseIncluded,omitempty"`
	CompareIncluded   *bool                        `json:"compareIncluded,omitempty"`
}

type DeploymentTimelineVariableChange struct {
	Key              string                       `json:"key"`
	Kind             DeploymentTimelineChangeKind `json:"kind"`
	BaseStatus       VariableResolutionStatus     `json:"baseStatus,omitempty"`
	CompareStatus    VariableResolutionStatus     `json:"compareStatus,omitempty"`
	BaseSource       VariableResolutionSource     `json:"baseSource,omitempty"`
	CompareSource    VariableResolutionSource     `json:"compareSource,omitempty"`
	BaseRedacted     bool                         `json:"baseRedacted"`
	CompareRedacted  bool                         `json:"compareRedacted"`
	BaseValue        json.RawMessage              `json:"baseValue,omitempty"`
	CompareValue     json.RawMessage              `json:"compareValue,omitempty"`
	BaseReference    string                       `json:"baseReference,omitempty"`
	CompareReference string                       `json:"compareReference,omitempty"`
}

type CreateDeploymentTimelineRedeployRequest struct {
	OrganizationID     uuid.UUID
	TaskID             uuid.UUID
	ActorUserAccountID uuid.UUID
}

type DeploymentTimelineRedeploy struct {
	Plan    DeploymentPlan `json:"plan"`
	Warning string         `json:"warning"`
}
