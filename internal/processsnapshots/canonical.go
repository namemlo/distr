package processsnapshots

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

type canonicalSnapshot struct {
	ApplicationID               string          `json:"applicationId"`
	DeploymentProcessID         string          `json:"deploymentProcessId"`
	DeploymentProcessRevisionID string          `json:"deploymentProcessRevisionId"`
	RevisionNumber              int             `json:"revisionNumber"`
	Description                 string          `json:"description"`
	Steps                       []canonicalStep `json:"steps"`
}

type canonicalStep struct {
	Key                   string         `json:"key"`
	Name                  string         `json:"name"`
	ActionType            string         `json:"actionType"`
	StepTemplateVersionID string         `json:"stepTemplateVersionId,omitempty"`
	ExecutionLocation     string         `json:"executionLocation"`
	InputBindings         map[string]any `json:"inputBindings"`
	Condition             string         `json:"condition,omitempty"`
	ChannelIDs            []string       `json:"channelIds,omitempty"`
	EnvironmentIDs        []string       `json:"environmentIds,omitempty"`
	TargetTags            []string       `json:"targetTags,omitempty"`
	FailureMode           string         `json:"failureMode"`
	TimeoutSeconds        int            `json:"timeoutSeconds,omitempty"`
	RetryMaxAttempts      int            `json:"retryMaxAttempts,omitempty"`
	RetryIntervalSeconds  int            `json:"retryIntervalSeconds,omitempty"`
	RequiredPermissions   []string       `json:"requiredPermissions,omitempty"`
	SortOrder             int            `json:"sortOrder"`
	Dependencies          []string       `json:"dependencies,omitempty"`
}

func Canonicalize(
	process types.DeploymentProcess,
	revision types.DeploymentProcessRevision,
) ([]byte, string, error) {
	steps := slices.Clone(revision.Steps)
	slices.SortFunc(steps, func(a, b types.DeploymentProcessStep) int {
		if a.SortOrder < b.SortOrder {
			return -1
		}
		if a.SortOrder > b.SortOrder {
			return 1
		}
		if a.Key < b.Key {
			return -1
		}
		if a.Key > b.Key {
			return 1
		}
		return 0
	})

	canonical := canonicalSnapshot{
		ApplicationID:               process.ApplicationID.String(),
		DeploymentProcessID:         process.ID.String(),
		DeploymentProcessRevisionID: revision.ID.String(),
		RevisionNumber:              revision.RevisionNumber,
		Description:                 revision.Description,
		Steps:                       make([]canonicalStep, 0, len(steps)),
	}
	for _, step := range steps {
		canonical.Steps = append(canonical.Steps, canonicalizeStep(step))
	}

	payload, err := json.Marshal(canonical)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(payload)
	return payload, "sha256:" + hex.EncodeToString(sum[:]), nil
}

func DecodeRevision(payload []byte) (types.DeploymentProcessRevision, error) {
	var snapshot canonicalSnapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return types.DeploymentProcessRevision{}, err
	}
	revision := types.DeploymentProcessRevision{
		ID:                  mustUUID(snapshot.DeploymentProcessRevisionID),
		DeploymentProcessID: mustUUID(snapshot.DeploymentProcessID),
		RevisionNumber:      snapshot.RevisionNumber,
		Description:         snapshot.Description,
		Steps:               make([]types.DeploymentProcessStep, 0, len(snapshot.Steps)),
	}
	for _, step := range snapshot.Steps {
		revision.Steps = append(revision.Steps, step.toDeploymentProcessStep())
	}
	return revision, nil
}

func canonicalizeStep(step types.DeploymentProcessStep) canonicalStep {
	result := canonicalStep{
		Key:                  step.Key,
		Name:                 step.Name,
		ActionType:           step.ActionType,
		ExecutionLocation:    step.ExecutionLocation,
		InputBindings:        step.InputBindings,
		Condition:            step.Condition,
		ChannelIDs:           uuidStrings(step.ChannelIDs),
		EnvironmentIDs:       uuidStrings(step.EnvironmentIDs),
		TargetTags:           slices.Clone(step.TargetTags),
		FailureMode:          step.FailureMode,
		TimeoutSeconds:       step.TimeoutSeconds,
		RetryMaxAttempts:     step.RetryMaxAttempts,
		RetryIntervalSeconds: step.RetryIntervalSeconds,
		RequiredPermissions:  slices.Clone(step.RequiredPermissions),
		SortOrder:            step.SortOrder,
		Dependencies:         slices.Clone(step.Dependencies),
	}
	if step.StepTemplateVersionID != nil {
		result.StepTemplateVersionID = step.StepTemplateVersionID.String()
	}
	if result.InputBindings == nil {
		result.InputBindings = map[string]any{}
	}
	return result
}

func (s canonicalStep) toDeploymentProcessStep() types.DeploymentProcessStep {
	return types.DeploymentProcessStep{
		Key:                   s.Key,
		Name:                  s.Name,
		ActionType:            s.ActionType,
		StepTemplateVersionID: uuidPtrFromString(s.StepTemplateVersionID),
		ExecutionLocation:     s.ExecutionLocation,
		InputBindings:         s.InputBindings,
		Condition:             s.Condition,
		ChannelIDs:            uuidsFromStrings(s.ChannelIDs),
		EnvironmentIDs:        uuidsFromStrings(s.EnvironmentIDs),
		TargetTags:            slices.Clone(s.TargetTags),
		FailureMode:           s.FailureMode,
		TimeoutSeconds:        s.TimeoutSeconds,
		RetryMaxAttempts:      s.RetryMaxAttempts,
		RetryIntervalSeconds:  s.RetryIntervalSeconds,
		RequiredPermissions:   slices.Clone(s.RequiredPermissions),
		SortOrder:             s.SortOrder,
		Dependencies:          slices.Clone(s.Dependencies),
	}
}

func uuidStrings(values []uuid.UUID) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.String())
	}
	return result
}

func uuidsFromStrings(values []string) []uuid.UUID {
	if len(values) == 0 {
		return nil
	}
	result := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		result = append(result, mustUUID(value))
	}
	return result
}

func uuidPtrFromString(value string) *uuid.UUID {
	if value == "" {
		return nil
	}
	id := mustUUID(value)
	return &id
}

func mustUUID(value string) uuid.UUID {
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil
	}
	return id
}
