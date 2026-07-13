package api

import (
	"encoding/json"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

type CreateDeploymentPlanRequest struct {
	ReleaseBundleID uuid.UUID   `json:"releaseBundleId"`
	EnvironmentID   uuid.UUID   `json:"environmentId"`
	TargetIDs       []uuid.UUID `json:"targetIds"`
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
	ID                 uuid.UUID                  `json:"id"`
	CreatedAt          time.Time                  `json:"createdAt"`
	ApplicationID      uuid.UUID                  `json:"applicationId"`
	ReleaseBundleID    uuid.UUID                  `json:"releaseBundleId"`
	ChannelID          uuid.UUID                  `json:"channelId"`
	EnvironmentID      uuid.UUID                  `json:"environmentId"`
	ProcessSnapshotID  *uuid.UUID                 `json:"processSnapshotId,omitempty"`
	VariableSnapshotID *uuid.UUID                 `json:"variableSnapshotId,omitempty"`
	ReleaseContract    *types.ReleaseContract     `json:"releaseContract,omitempty"`
	Status             types.DeploymentPlanStatus `json:"status"`
	CanonicalChecksum  string                     `json:"canonicalChecksum"`
	Targets            []DeploymentPlanTarget     `json:"targets"`
	Steps              []DeploymentPlanStep       `json:"steps"`
	Variables          []DeploymentPlanVariable   `json:"variables"`
	Issues             []DeploymentPlanIssue      `json:"issues"`
}

type DeploymentPlanTarget struct {
	ID                     uuid.UUID            `json:"id"`
	DeploymentTargetID     uuid.UUID            `json:"deploymentTargetId"`
	Name                   string               `json:"name"`
	Type                   types.DeploymentType `json:"type"`
	CustomerOrganizationID *uuid.UUID           `json:"customerOrganizationId,omitempty"`
	SortOrder              int                  `json:"sortOrder"`
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
