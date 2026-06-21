package actionregistry

import (
	"fmt"
	"strings"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/types"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

type Registry struct {
	actions    []types.ActionDefinition
	validators map[string]*jsonschema.Schema
}

var defaultRegistry = mustBuildRegistry(defaultActions())

func DefaultRegistry() Registry {
	return defaultRegistry
}

func (r Registry) List() []types.ActionDefinition {
	actions := make([]types.ActionDefinition, 0, len(r.actions))
	for _, action := range r.actions {
		actions = append(actions, cloneAction(action))
	}
	return actions
}

func (r Registry) Get(actionType string) (types.ActionDefinition, bool) {
	actionType = strings.TrimSpace(actionType)
	for _, action := range r.actions {
		if action.Type == actionType {
			return cloneAction(action), true
		}
	}
	return types.ActionDefinition{}, false
}

func (r Registry) ValidateInput(actionType string, input map[string]any) error {
	actionType = strings.TrimSpace(actionType)
	validator, ok := r.validators[actionType]
	if !ok {
		return apierrors.NewBadRequest(fmt.Sprintf("unknown actionType %q", actionType))
	}
	if input == nil {
		input = map[string]any{}
	}
	if err := validator.Validate(input); err != nil {
		return apierrors.NewBadRequest(
			fmt.Sprintf("actionType %q inputBindings do not match schema: %s", actionType, err.Error()),
		)
	}
	return nil
}

func (r Registry) ValidateSteps(steps []types.DeploymentProcessStep) error {
	for _, step := range steps {
		if err := r.ValidateInput(step.ActionType, step.InputBindings); err != nil {
			return apierrors.NewBadRequest(fmt.Sprintf("step %q %s", step.Key, badRequestMessage(err)))
		}
	}
	return nil
}

func mustBuildRegistry(actions []types.ActionDefinition) Registry {
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	validators := make(map[string]*jsonschema.Schema, len(actions))
	for _, action := range actions {
		if err := compiler.AddResource(action.Type+".input.schema.json", action.InputSchema); err != nil {
			panic(fmt.Sprintf("actionregistry: add input schema for %s: %v", action.Type, err))
		}
		schema, err := compiler.Compile(action.Type + ".input.schema.json")
		if err != nil {
			panic(fmt.Sprintf("actionregistry: compile input schema for %s: %v", action.Type, err))
		}
		validators[action.Type] = schema
	}
	return Registry{actions: actions, validators: validators}
}

func defaultActions() []types.ActionDefinition {
	return []types.ActionDefinition{
		{
			Type:        "distr.preflight",
			Name:        "Preflight checks",
			Description: "Runs built-in agent preflight checks and returns structured results.",
			InputSchema: objectSchema(
				map[string]any{},
				nil,
			),
			OutputSchema: objectSchema(
				map[string]any{
					"passed": map[string]any{"type": "boolean"},
					"checks": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"name":    map[string]any{"type": "string"},
								"status":  map[string]any{"type": "string", "enum": []any{"passed", "failed", "skipped"}},
								"message": map[string]any{"type": "string"},
							},
							"required":             []any{"name", "status"},
							"additionalProperties": false,
						},
					},
				},
				[]any{"passed", "checks"},
			),
		},
		{
			Type:        "distr.http.check",
			Name:        "HTTP check",
			Description: "Calls an HTTP endpoint and validates status, headers, body matching, latency, and retry policy.",
			InputSchema: objectSchema(
				map[string]any{
					"url":    map[string]any{"type": "string", "format": "uri"},
					"method": map[string]any{"type": "string", "enum": []any{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"}},
					"expectedStatusCodes": map[string]any{
						"type":     "array",
						"items":    map[string]any{"type": "integer", "minimum": 100, "maximum": 599},
						"minItems": 1,
					},
					"expectedHeaders": map[string]any{
						"type":                 "object",
						"additionalProperties": map[string]any{"type": "string"},
					},
					"bodyContains": map[string]any{"type": "string"},
					"bodyRegex":    map[string]any{"type": "string"},
					"maxLatencyMs": map[string]any{"type": "integer", "minimum": 0},
					"retry": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"maxAttempts":     map[string]any{"type": "integer", "minimum": 0},
							"intervalSeconds": map[string]any{"type": "integer", "minimum": 0},
						},
						"additionalProperties": false,
					},
				},
				[]any{"url"},
			),
			OutputSchema: objectSchema(
				map[string]any{
					"passed":      map[string]any{"type": "boolean"},
					"statusCode":  map[string]any{"type": "integer"},
					"latencyMs":   map[string]any{"type": "integer", "minimum": 0},
					"bodyMatched": map[string]any{"type": "boolean"},
				},
				[]any{"passed", "statusCode", "latencyMs"},
			),
		},
		{
			Type:        "distr.wait",
			Name:        "Wait",
			Description: "Waits for a duration or until a declared condition is met.",
			InputSchema: map[string]any{
				"$schema":              "https://json-schema.org/draft/2020-12/schema",
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"durationSeconds": map[string]any{"type": "integer", "minimum": 1},
					"condition":       map[string]any{"type": "string", "minLength": 1},
				},
				"oneOf": []any{
					map[string]any{"required": []any{"durationSeconds"}},
					map[string]any{"required": []any{"condition"}},
				},
			},
			OutputSchema: objectSchema(
				map[string]any{
					"completed":      map[string]any{"type": "boolean"},
					"elapsedSeconds": map[string]any{"type": "integer", "minimum": 0},
				},
				[]any{"completed", "elapsedSeconds"},
			),
		},
		{
			Type:        "distr.compose.deploy",
			Name:        "Compose deploy",
			Description: "Runs the Docker Compose deployment adapter through the typed action protocol.",
			InputSchema: objectSchema(
				map[string]any{
					"applicationVersion": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"composeFile": map[string]any{"type": "string", "minLength": 1},
							"registryAuth": map[string]any{
								"type": "object",
								"additionalProperties": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"username": map[string]any{"type": "string"},
										"password": map[string]any{"type": "string"},
									},
									"required":             []any{"username", "password"},
									"additionalProperties": false,
								},
							},
						},
						"required":             []any{"composeFile"},
						"additionalProperties": false,
					},
					"projectName":     map[string]any{"type": "string", "minLength": 1, "pattern": "^[a-z0-9][a-z0-9_-]*$"},
					"environmentFile": map[string]any{"type": "string"},
					"pullPolicy": map[string]any{
						"type": "string",
						"enum": []any{"always", "missing", "if_not_present", "never"},
					},
					"waitForHealthy": map[string]any{"type": "boolean"},
					"timeoutSeconds": map[string]any{"type": "integer", "minimum": 1},
					"strategy": map[string]any{
						"type": "string",
						"enum": []any{"compose", "swarm"},
					},
				},
				[]any{"applicationVersion", "projectName"},
			),
			OutputSchema: objectSchema(
				map[string]any{
					"projectName": map[string]any{"type": "string"},
					"strategy":    map[string]any{"type": "string"},
					"status":      map[string]any{"type": "string"},
					"state":       map[string]any{"type": "string"},
				},
				[]any{"projectName", "strategy", "status"},
			),
		},
	}
}

func objectSchema(properties map[string]any, required []any) map[string]any {
	schema := map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func cloneAction(action types.ActionDefinition) types.ActionDefinition {
	return types.ActionDefinition{
		Type:         action.Type,
		Name:         action.Name,
		Description:  action.Description,
		InputSchema:  cloneMap(action.InputSchema),
		OutputSchema: cloneMap(action.OutputSchema),
	}
}

func cloneMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	clone := make(map[string]any, len(value))
	for key, item := range value {
		clone[key] = cloneValue(item)
	}
	return clone
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		clone := make([]any, 0, len(typed))
		for _, item := range typed {
			clone = append(clone, cloneValue(item))
		}
		return clone
	default:
		return typed
	}
}

func badRequestMessage(err error) string {
	return strings.TrimPrefix(err.Error(), apierrors.ErrBadRequest.Error()+": ")
}
