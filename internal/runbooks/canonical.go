package runbooks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

type canonicalSnapshot struct {
	ApplicationID     string          `json:"applicationId"`
	RunbookID         string          `json:"runbookId"`
	RunbookRevisionID string          `json:"runbookRevisionId"`
	RevisionNumber    int             `json:"revisionNumber"`
	Description       string          `json:"description"`
	Steps             []canonicalStep `json:"steps"`
}

type canonicalStep struct {
	Key                   string         `json:"key"`
	Name                  string         `json:"name"`
	ActionType            string         `json:"actionType"`
	StepTemplateVersionID string         `json:"stepTemplateVersionId,omitempty"`
	ExecutionLocation     string         `json:"executionLocation"`
	InputBindings         map[string]any `json:"inputBindings"`
	Condition             string         `json:"condition,omitempty"`
	FailureMode           string         `json:"failureMode"`
	TimeoutSeconds        int            `json:"timeoutSeconds,omitempty"`
	RetryMaxAttempts      int            `json:"retryMaxAttempts,omitempty"`
	RetryIntervalSeconds  int            `json:"retryIntervalSeconds,omitempty"`
	RequiredPermissions   []string       `json:"requiredPermissions,omitempty"`
	SortOrder             int            `json:"sortOrder"`
	Dependencies          []string       `json:"dependencies,omitempty"`
}

func Canonicalize(runbook types.Runbook, revision types.RunbookRevision) ([]byte, string, error) {
	steps := slices.Clone(revision.Steps)
	slices.SortFunc(steps, func(a, b types.RunbookStep) int {
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
		ApplicationID:     runbook.ApplicationID.String(),
		RunbookID:         runbook.ID.String(),
		RunbookRevisionID: revision.ID.String(),
		RevisionNumber:    revision.RevisionNumber,
		Description:       revision.Description,
		Steps:             make([]canonicalStep, 0, len(steps)),
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

func DecodeRevision(payload []byte) (types.RunbookRevision, error) {
	var snapshot canonicalSnapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return types.RunbookRevision{}, err
	}
	revision := types.RunbookRevision{
		ID:             mustUUID(snapshot.RunbookRevisionID),
		RunbookID:      mustUUID(snapshot.RunbookID),
		RevisionNumber: snapshot.RevisionNumber,
		Description:    snapshot.Description,
		Steps:          make([]types.RunbookStep, 0, len(snapshot.Steps)),
	}
	for _, step := range snapshot.Steps {
		revision.Steps = append(revision.Steps, step.toRunbookStep())
	}
	return revision, nil
}

func canonicalizeStep(step types.RunbookStep) canonicalStep {
	result := canonicalStep{
		Key:                  step.Key,
		Name:                 step.Name,
		ActionType:           step.ActionType,
		ExecutionLocation:    step.ExecutionLocation,
		InputBindings:        step.InputBindings,
		Condition:            step.Condition,
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

func (s canonicalStep) toRunbookStep() types.RunbookStep {
	return types.RunbookStep{
		Key:                   s.Key,
		Name:                  s.Name,
		ActionType:            s.ActionType,
		StepTemplateVersionID: uuidPtrFromString(s.StepTemplateVersionID),
		ExecutionLocation:     s.ExecutionLocation,
		InputBindings:         s.InputBindings,
		Condition:             s.Condition,
		FailureMode:           s.FailureMode,
		TimeoutSeconds:        s.TimeoutSeconds,
		RetryMaxAttempts:      s.RetryMaxAttempts,
		RetryIntervalSeconds:  s.RetryIntervalSeconds,
		RequiredPermissions:   slices.Clone(s.RequiredPermissions),
		SortOrder:             s.SortOrder,
		Dependencies:          slices.Clone(s.Dependencies),
	}
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
