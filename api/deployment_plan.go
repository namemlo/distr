package api

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

type CreateDeploymentPlanRequest struct {
	ReleaseBundleID  uuid.UUID   `json:"releaseBundleId"`
	EnvironmentID    uuid.UUID   `json:"environmentId"`
	TargetIDs        []uuid.UUID `json:"targetIds"`
	DeploymentUnitID *uuid.UUID  `json:"deploymentUnitId,omitempty"`
}

func (r CreateDeploymentPlanRequest) Validate() error {
	if r.ReleaseBundleID == uuid.Nil {
		return validation.NewValidationFailedError("releaseBundleId is required")
	}
	if r.EnvironmentID == uuid.Nil {
		return validation.NewValidationFailedError("environmentId is required")
	}
	if len(r.TargetIDs) == 0 {
		return validation.NewValidationFailedError("at least one targetId is required")
	}
	if r.DeploymentUnitID != nil && *r.DeploymentUnitID == uuid.Nil {
		return validation.NewValidationFailedError("deploymentUnitId must not be empty")
	}
	seen := map[uuid.UUID]struct{}{}
	for _, targetID := range r.TargetIDs {
		if targetID == uuid.Nil {
			return validation.NewValidationFailedError("targetIds must not contain empty IDs")
		}
		if _, ok := seen[targetID]; ok {
			return validation.NewValidationFailedError("targetIds must be unique")
		}
		seen[targetID] = struct{}{}
	}
	return nil
}

type DeploymentPlan struct {
	ID                         uuid.UUID                         `json:"id"`
	CreatedAt                  time.Time                         `json:"createdAt"`
	SealedAt                   *time.Time                        `json:"sealedAt,omitempty"`
	PublishedByUserAccountID   *uuid.UUID                        `json:"publishedByUserAccountId,omitempty"`
	ApplicationID              uuid.UUID                         `json:"applicationId"`
	ReleaseBundleID            uuid.UUID                         `json:"releaseBundleId"`
	ChannelID                  uuid.UUID                         `json:"channelId"`
	EnvironmentID              uuid.UUID                         `json:"environmentId"`
	ProcessSnapshotID          *uuid.UUID                        `json:"processSnapshotId,omitempty"`
	VariableSnapshotID         *uuid.UUID                        `json:"variableSnapshotId,omitempty"`
	ReleaseContract            *types.ReleaseContract            `json:"releaseContract,omitempty"`
	PlanSchema                 string                            `json:"planSchema"`
	DraftID                    *uuid.UUID                        `json:"draftId,omitempty"`
	DeploymentUnitID           *uuid.UUID                        `json:"deploymentUnitId,omitempty"`
	EffectivePolicy            *types.EffectivePolicy            `json:"effectivePolicy,omitempty"`
	EffectivePolicyChecksum    string                            `json:"effectivePolicyChecksum,omitempty"`
	SubscriberSetChecksum      string                            `json:"subscriberSetChecksum,omitempty"`
	TargetConfigSnapshotID     *uuid.UUID                        `json:"targetConfigSnapshotId,omitempty"`
	ProtocolVersion            string                            `json:"protocolVersion"`
	SupersedesDeploymentPlanID *uuid.UUID                        `json:"supersedesDeploymentPlanId,omitempty"`
	SupersedeReason            string                            `json:"supersedeReason,omitempty"`
	PreviousStateSourcePlanID  *uuid.UUID                        `json:"previousStateSourcePlanId,omitempty"`
	Status                     types.DeploymentPlanStatus        `json:"status"`
	CanonicalChecksum          string                            `json:"canonicalChecksum"`
	Targets                    []DeploymentPlanTarget            `json:"targets"`
	TargetComponents           []DeploymentPlanTargetComponent   `json:"targetComponents"`
	PreflightRuns              []DeploymentPreflightRun          `json:"preflightRuns"`
	Steps                      []DeploymentPlanStep              `json:"steps"`
	Variables                  []DeploymentPlanVariable          `json:"variables"`
	Issues                     []DeploymentPlanIssue             `json:"issues"`
	ResolvedRequirements       []types.RequirementResolution     `json:"resolvedRequirements,omitempty"`
	StepEdges                  []types.DeploymentPlanStepEdge    `json:"stepEdges,omitempty"`
	Baselines                  []types.DeploymentPlanBaseline    `json:"baselines,omitempty"`
	Changes                    []types.DeploymentPlanChangeEntry `json:"changes,omitempty"`
	Risks                      []types.DeploymentPlanRiskEntry   `json:"risks,omitempty"`
	Bootstrap                  bool                              `json:"bootstrap"`
}

type CreatePreviousStateDeploymentPlanRequest struct {
	SuccessfulDeploymentPlanID uuid.UUID `json:"successfulDeploymentPlanId"`
	Reason                     string    `json:"reason"`
}

func (r CreatePreviousStateDeploymentPlanRequest) Validate() error {
	if r.SuccessfulDeploymentPlanID == uuid.Nil {
		return validation.NewValidationFailedError("successfulDeploymentPlanId is required")
	}
	reason := strings.TrimSpace(r.Reason)
	if reason == "" || len(reason) > 2048 || strings.ContainsAny(r.Reason, "\r\n") {
		return validation.NewValidationFailedError("reason is invalid")
	}
	return nil
}

type DeploymentPreflightRun struct {
	ID                 uuid.UUID                       `json:"id"`
	CreatedAt          time.Time                       `json:"createdAt"`
	DeploymentPlanID   uuid.UUID                       `json:"deploymentPlanId"`
	PlanChecksum       string                          `json:"planChecksum"`
	ActorUserAccountID *uuid.UUID                      `json:"actorUserAccountId,omitempty"`
	Status             types.DeploymentPreflightStatus `json:"status"`
	Checks             []DeploymentPreflightCheck      `json:"checks"`
}

type DeploymentPreflightCheck struct {
	ID                       uuid.UUID                            `json:"id"`
	CreatedAt                time.Time                            `json:"createdAt"`
	DeploymentPreflightRunID uuid.UUID                            `json:"deploymentPreflightRunId"`
	DeploymentPlanID         uuid.UUID                            `json:"deploymentPlanId"`
	DeploymentPlanTargetID   *uuid.UUID                           `json:"deploymentPlanTargetId,omitempty"`
	DeploymentTargetID       *uuid.UUID                           `json:"deploymentTargetId,omitempty"`
	TaskID                   *uuid.UUID                           `json:"taskId,omitempty"`
	Component                string                               `json:"component,omitempty"`
	CheckKey                 string                               `json:"checkKey"`
	Status                   types.DeploymentPreflightCheckStatus `json:"status"`
	Expected                 map[string]any                       `json:"expected"`
	Actual                   map[string]any                       `json:"actual"`
	Message                  string                               `json:"message"`
	SortOrder                int                                  `json:"sortOrder"`
}

type DeploymentPlanTarget struct {
	ID                     uuid.UUID                      `json:"id"`
	DeploymentTargetID     uuid.UUID                      `json:"deploymentTargetId"`
	Name                   string                         `json:"name"`
	Type                   types.DeploymentType           `json:"type"`
	Platform               types.DeploymentTargetPlatform `json:"platform"`
	CustomerOrganizationID *uuid.UUID                     `json:"customerOrganizationId,omitempty"`
	SortOrder              int                            `json:"sortOrder"`
}

type DeploymentPlanTargetComponent struct {
	ID                      uuid.UUID                      `json:"id"`
	DeploymentPlanTargetID  uuid.UUID                      `json:"deploymentPlanTargetId"`
	DeploymentTargetID      uuid.UUID                      `json:"deploymentTargetId"`
	Component               string                         `json:"component"`
	Version                 string                         `json:"version"`
	Image                   string                         `json:"image"`
	Platform                types.DeploymentTargetPlatform `json:"platform"`
	Contracts               []string                       `json:"contracts"`
	ConfigChecksum          string                         `json:"configChecksum"`
	ExpectedStateVersion    int64                          `json:"expectedStateVersion"`
	ExpectedStateChecksum   string                         `json:"expectedStateChecksum"`
	ExpectedReleaseBundleID *uuid.UUID                     `json:"expectedReleaseBundleId,omitempty"`
	SortOrder               int                            `json:"sortOrder"`
}

type DeploymentPlanStep struct {
	ID                   uuid.UUID      `json:"id"`
	StepKey              string         `json:"stepKey"`
	Name                 string         `json:"name"`
	ActionType           string         `json:"actionType"`
	ActionName           string         `json:"actionName"`
	ExecutionLocation    string         `json:"executionLocation"`
	InputBindings        map[string]any `json:"inputBindings"`
	Condition            string         `json:"condition"`
	TargetTags           []string       `json:"targetTags"`
	FailureMode          string         `json:"failureMode"`
	TimeoutSeconds       int            `json:"timeoutSeconds"`
	RetryMaxAttempts     int            `json:"retryMaxAttempts"`
	RetryIntervalSeconds int            `json:"retryIntervalSeconds"`
	RequiredPermissions  []string       `json:"requiredPermissions"`
	SortOrder            int            `json:"sortOrder"`
	Dependencies         []string       `json:"dependencies"`
	Included             bool           `json:"included"`
	ExcludedReason       string         `json:"excludedReason,omitempty"`
}

type DeploymentPlanVariable struct {
	ID            uuid.UUID                            `json:"id"`
	VariableSetID uuid.UUID                            `json:"variableSetId"`
	VariableID    uuid.UUID                            `json:"variableId"`
	Key           string                               `json:"key"`
	Type          types.VariableType                   `json:"type"`
	IsRequired    bool                                 `json:"isRequired"`
	Status        types.VariableResolutionStatus       `json:"status"`
	Source        types.VariableResolutionSource       `json:"source"`
	Value         json.RawMessage                      `json:"value,omitempty"`
	ReferenceID   string                               `json:"referenceId,omitempty"`
	ReferenceName string                               `json:"referenceName,omitempty"`
	Redacted      bool                                 `json:"redacted"`
	Trace         []types.VariableResolutionTraceEntry `json:"trace"`
}

type DeploymentPlanIssue struct {
	ID        uuid.UUID                         `json:"id"`
	Severity  types.DeploymentPlanIssueSeverity `json:"severity"`
	Code      string                            `json:"code"`
	Field     string                            `json:"field"`
	Message   string                            `json:"message"`
	SortOrder int                               `json:"sortOrder"`
}
