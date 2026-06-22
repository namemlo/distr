package api

import (
	"fmt"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/actionregistry"
	"github.com/distr-sh/distr/internal/conditions"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

type CreateUpdateRunbookRequest struct {
	ApplicationID uuid.UUID `json:"applicationId"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	SortOrder     int       `json:"sortOrder"`
}

func (r *CreateUpdateRunbookRequest) Validate() error {
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" {
		return validation.NewValidationFailedError("name is required")
	}
	if r.ApplicationID == uuid.Nil {
		return validation.NewValidationFailedError("applicationId is required")
	}
	if r.SortOrder < 0 {
		return validation.NewValidationFailedError("sortOrder must be non-negative")
	}
	return nil
}

type CreateRunbookRevisionRequest struct {
	Description string               `json:"description"`
	Steps       []RunbookStepRequest `json:"steps"`
}

func (r *CreateRunbookRevisionRequest) Validate() error {
	r.Description = strings.TrimSpace(r.Description)
	if len(r.Steps) == 0 {
		return validation.NewValidationFailedError("at least one step is required")
	}

	stepKeys := map[string]struct{}{}
	sortOrders := map[int]struct{}{}
	for i := range r.Steps {
		step := &r.Steps[i]
		step.trim()
		if step.Key == "" {
			return validation.NewValidationFailedError("step key is required")
		}
		if step.Name == "" {
			return validation.NewValidationFailedError("step name is required")
		}
		if step.ActionType == "" {
			return validation.NewValidationFailedError("step actionType is required")
		}
		if step.ExecutionLocation == "" {
			return validation.NewValidationFailedError("step executionLocation is required")
		}
		if step.SortOrder < 0 {
			return validation.NewValidationFailedError("step sortOrder must be non-negative")
		}
		if step.TimeoutSeconds < 0 {
			return validation.NewValidationFailedError("step timeoutSeconds must be non-negative")
		}
		if step.RetryPolicy.MaxAttempts < 0 {
			return validation.NewValidationFailedError("step retryPolicy.maxAttempts must be non-negative")
		}
		if step.RetryPolicy.IntervalSeconds < 0 {
			return validation.NewValidationFailedError("step retryPolicy.intervalSeconds must be non-negative")
		}
		requiredPermissions, err := trimRequiredStringList(step.RequiredPermissions, "step requiredPermissions")
		if err != nil {
			return err
		}
		step.RequiredPermissions = requiredPermissions

		if _, ok := stepKeys[step.Key]; ok {
			return validation.NewValidationFailedError("step keys must be unique")
		}
		stepKeys[step.Key] = struct{}{}
		if _, ok := sortOrders[step.SortOrder]; ok {
			return validation.NewValidationFailedError("step sortOrder values must be unique")
		}
		sortOrders[step.SortOrder] = struct{}{}
		if step.FailureMode == "" {
			step.FailureMode = "fail"
		}
		if step.InputBindings == nil {
			step.InputBindings = map[string]any{}
		}
		if err := actionregistry.DefaultRegistry().ValidateInput(step.ActionType, step.InputBindings); err != nil {
			return validation.NewValidationFailedError(
				fmt.Sprintf("step %q %s", step.Key, strings.TrimPrefix(err.Error(), "bad request: ")),
			)
		}
		if err := conditions.Validate(step.Condition); err != nil {
			return validation.NewValidationFailedError(fmt.Sprintf("step %q condition is invalid: %s", step.Key, err.Error()))
		}
	}

	if err := validateRunbookStepDependencies(r.Steps, stepKeys); err != nil {
		return err
	}
	return nil
}

type RunbookStepRetryPolicyRequest struct {
	MaxAttempts     int `json:"maxAttempts"`
	IntervalSeconds int `json:"intervalSeconds"`
}

type RunbookStepRequest struct {
	Key                   string                        `json:"key"`
	Name                  string                        `json:"name"`
	ActionType            string                        `json:"actionType"`
	StepTemplateVersionID *uuid.UUID                    `json:"stepTemplateVersionId,omitempty"`
	ExecutionLocation     string                        `json:"executionLocation"`
	InputBindings         map[string]any                `json:"inputBindings"`
	Condition             string                        `json:"condition"`
	FailureMode           string                        `json:"failureMode"`
	TimeoutSeconds        int                           `json:"timeoutSeconds"`
	RetryPolicy           RunbookStepRetryPolicyRequest `json:"retryPolicy"`
	RequiredPermissions   []string                      `json:"requiredPermissions"`
	SortOrder             int                           `json:"sortOrder"`
	Dependencies          []string                      `json:"dependencies"`
}

func (r *RunbookStepRequest) trim() {
	r.Key = strings.TrimSpace(r.Key)
	r.Name = strings.TrimSpace(r.Name)
	r.ActionType = strings.TrimSpace(r.ActionType)
	r.ExecutionLocation = strings.TrimSpace(r.ExecutionLocation)
	r.Condition = strings.TrimSpace(r.Condition)
	r.FailureMode = strings.TrimSpace(r.FailureMode)
	for i := range r.Dependencies {
		r.Dependencies[i] = strings.TrimSpace(r.Dependencies[i])
	}
}

type Runbook struct {
	ID            uuid.UUID `json:"id"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
	ApplicationID uuid.UUID `json:"applicationId"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	SortOrder     int       `json:"sortOrder"`
}

type RunbookRevision struct {
	ID             uuid.UUID     `json:"id"`
	CreatedAt      time.Time     `json:"createdAt"`
	UpdatedAt      time.Time     `json:"updatedAt"`
	RunbookID      uuid.UUID     `json:"runbookId"`
	RevisionNumber int           `json:"revisionNumber"`
	Description    string        `json:"description"`
	Steps          []RunbookStep `json:"steps"`
}

type RunbookStepRetryPolicy struct {
	MaxAttempts     int `json:"maxAttempts"`
	IntervalSeconds int `json:"intervalSeconds"`
}

type RunbookStep struct {
	ID                    uuid.UUID              `json:"id"`
	RunbookRevisionID     uuid.UUID              `json:"runbookRevisionId"`
	Key                   string                 `json:"key"`
	Name                  string                 `json:"name"`
	ActionType            string                 `json:"actionType"`
	StepTemplateVersionID *uuid.UUID             `json:"stepTemplateVersionId,omitempty"`
	ExecutionLocation     string                 `json:"executionLocation"`
	InputBindings         map[string]any         `json:"inputBindings"`
	Condition             string                 `json:"condition"`
	FailureMode           string                 `json:"failureMode"`
	TimeoutSeconds        int                    `json:"timeoutSeconds"`
	RetryPolicy           RunbookStepRetryPolicy `json:"retryPolicy"`
	RequiredPermissions   []string               `json:"requiredPermissions"`
	SortOrder             int                    `json:"sortOrder"`
	Dependencies          []string               `json:"dependencies"`
}

type RunbookSnapshot struct {
	ID                       uuid.UUID       `json:"id"`
	CreatedAt                time.Time       `json:"createdAt"`
	PublishedAt              time.Time       `json:"publishedAt"`
	PublishedByUserAccountID *uuid.UUID      `json:"publishedByUserAccountId,omitempty"`
	ApplicationID            uuid.UUID       `json:"applicationId"`
	RunbookID                uuid.UUID       `json:"runbookId"`
	RunbookRevisionID        uuid.UUID       `json:"runbookRevisionId"`
	RevisionNumber           int             `json:"revisionNumber"`
	CanonicalChecksum        string          `json:"canonicalChecksum"`
	Revision                 RunbookRevision `json:"revision"`
}

func validateRunbookStepDependencies(
	steps []RunbookStepRequest,
	stepKeys map[string]struct{},
) error {
	graph := make(map[string][]string, len(steps))
	for _, step := range steps {
		dependencies := make([]string, 0, len(step.Dependencies))
		seenDependencies := map[string]struct{}{}
		for _, dependency := range step.Dependencies {
			if dependency == "" {
				return validation.NewValidationFailedError("step dependency is required")
			}
			if dependency == step.Key {
				return validation.NewValidationFailedError("step cannot depend on itself")
			}
			if _, ok := seenDependencies[dependency]; ok {
				return validation.NewValidationFailedError("step dependencies must be unique")
			}
			seenDependencies[dependency] = struct{}{}
			if _, ok := stepKeys[dependency]; !ok {
				return validation.NewValidationFailedError(fmt.Sprintf("step dependency %q does not exist", dependency))
			}
			dependencies = append(dependencies, dependency)
		}
		refs, err := conditions.OutputReferences(step.Condition)
		if err != nil {
			return validation.NewValidationFailedError(fmt.Sprintf("step %q condition is invalid: %s", step.Key, err.Error()))
		}
		for _, ref := range refs {
			if _, ok := stepKeys[ref.StepKey]; !ok {
				return validation.NewValidationFailedError(
					fmt.Sprintf("step condition output reference %q does not exist", ref.StepKey),
				)
			}
			if _, ok := seenDependencies[ref.StepKey]; ok {
				continue
			}
			seenDependencies[ref.StepKey] = struct{}{}
			dependencies = append(dependencies, ref.StepKey)
		}
		graph[step.Key] = dependencies
	}

	visiting := map[string]struct{}{}
	visited := map[string]struct{}{}
	var visit func(string) bool
	visit = func(key string) bool {
		if _, ok := visited[key]; ok {
			return false
		}
		if _, ok := visiting[key]; ok {
			return true
		}
		visiting[key] = struct{}{}
		for _, dependency := range graph[key] {
			if visit(dependency) {
				return true
			}
		}
		delete(visiting, key)
		visited[key] = struct{}{}
		return false
	}
	for key := range graph {
		if visit(key) {
			return validation.NewValidationFailedError("step dependencies must not contain cycles")
		}
	}
	return nil
}
