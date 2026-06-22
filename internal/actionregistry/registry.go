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

const webhookBuiltInOutputCount = 7

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
	if actionType == "distr.webhook" {
		if err := validateWebhookRegistryInput(input); err != nil {
			return err
		}
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
										"username":          map[string]any{"type": "string"},
										"passwordSecretRef": map[string]any{"type": "string", "minLength": 1},
									},
									"required":             []any{"username", "passwordSecretRef"},
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
		{
			Type:        "distr.oci.job",
			Name:        "OCI one-shot job",
			Description: "Runs one immutable OCI image as a bounded, policy-constrained one-shot Docker job.",
			InputSchema: objectSchema(
				map[string]any{
					"imageDigest": map[string]any{
						"type":    "string",
						"pattern": `^\S+@sha256:[A-Fa-f0-9]{64}$`,
					},
					"command": map[string]any{
						"type":     "array",
						"items":    map[string]any{"type": "string", "minLength": 1},
						"minItems": 1,
					},
					"arguments": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "string"},
					},
					"environment": map[string]any{
						"type":                 "object",
						"additionalProperties": map[string]any{"type": "string"},
					},
					"secretEnvironment": map[string]any{
						"type":                 "object",
						"additionalProperties": map[string]any{"type": "string", "minLength": 1},
					},
					"network": map[string]any{"type": "string", "minLength": 1},
					"volumes": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"source":   map[string]any{"type": "string", "minLength": 1},
								"target":   map[string]any{"type": "string", "minLength": 1},
								"readOnly": map[string]any{"type": "boolean"},
							},
							"required":             []any{"source", "target", "readOnly"},
							"additionalProperties": false,
						},
					},
					"timeoutSeconds": map[string]any{"type": "integer", "minimum": 1},
					"expectedExitCodes": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "integer", "minimum": 0, "maximum": 255},
						"minItems":    1,
						"uniqueItems": true,
					},
					"idempotencyKey": map[string]any{
						"type":      "string",
						"minLength": 1,
						"maxLength": 128,
						"pattern":   `^[A-Za-z0-9_.:-]+$`,
					},
					"runAsUser": map[string]any{"type": "string", "minLength": 1},
					"resources": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"cpus":        map[string]any{"type": "number", "exclusiveMinimum": 0},
							"memoryBytes": map[string]any{"type": "integer", "minimum": 1},
						},
						"additionalProperties": false,
					},
					"security": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"privileged":              map[string]any{"const": false},
							"readOnlyRootFilesystem":  map[string]any{"const": true},
							"dropCapabilities":        map[string]any{"type": "array", "items": map[string]any{"type": "string", "minLength": 1}},
							"noNewPrivilegesDisabled": map[string]any{"const": false},
						},
						"additionalProperties": false,
					},
				},
				[]any{"imageDigest", "command"},
			),
			OutputSchema: objectSchema(
				map[string]any{
					"containerName": map[string]any{"type": "string"},
					"exitCode":      map[string]any{"type": "integer"},
					"status":        map[string]any{"type": "string"},
				},
				[]any{"containerName", "exitCode", "status"},
			),
		},
		{
			Type:        "distr.file.render",
			Name:        "File render",
			Description: "Renders a text file from scoped variables using the target agent's allowlisted destination roots.",
			InputSchema: objectSchema(
				map[string]any{
					"destinationPath": map[string]any{
						"type":      "string",
						"minLength": 1,
						"pattern":   `^[^/\\].*`,
					},
					"template": map[string]any{"type": "string"},
					"variables": map[string]any{
						"type":                 "object",
						"additionalProperties": map[string]any{"type": "string"},
					},
					"secretVariables": map[string]any{
						"type":                 "object",
						"additionalProperties": map[string]any{"type": "string", "minLength": 1},
					},
					"mode": map[string]any{
						"type":    "string",
						"pattern": `^0?[0-7]{3}$`,
					},
					"owner": map[string]any{
						"type":    "string",
						"pattern": `^[0-9]+$`,
					},
					"group": map[string]any{
						"type":    "string",
						"pattern": `^[0-9]+$`,
					},
					"backup": map[string]any{"type": "boolean"},
					"idempotencyKey": map[string]any{
						"type":      "string",
						"minLength": 1,
						"maxLength": 128,
						"pattern":   `^[A-Za-z0-9_.:-]+$`,
					},
					"timeoutSeconds": map[string]any{"type": "integer", "minimum": 1},
				},
				[]any{"destinationPath", "template"},
			),
			OutputSchema: objectSchema(
				map[string]any{
					"destinationPath": map[string]any{"type": "string"},
					"changed":         map[string]any{"type": "boolean"},
					"backupPath":      map[string]any{"type": "string"},
				},
				[]any{"destinationPath", "changed"},
			),
		},
		{
			Type:        "distr.webhook",
			Name:        "Webhook",
			Description: "Calls a trusted outbound HTTPS webhook with signed JSON, secret headers, retry policy, and declared response outputs.",
			InputSchema: objectSchema(
				map[string]any{
					"url": map[string]any{
						"type":    "string",
						"format":  "uri",
						"pattern": `^https://`,
					},
					"method": map[string]any{
						"type": "string",
						"enum": []any{"GET", "POST", "PUT", "PATCH", "DELETE"},
					},
					"headers": map[string]any{
						"type":                 "object",
						"propertyNames":        webhookHeaderNameSchema(),
						"additionalProperties": map[string]any{"type": "string"},
					},
					"secretHeaders": map[string]any{
						"type":                 "object",
						"propertyNames":        webhookHeaderNameSchema(),
						"additionalProperties": webhookSecretReferenceSchema(),
					},
					"body":          map[string]any{},
					"sensitiveBody": map[string]any{"type": "boolean"},
					"signingSecret": webhookSecretReferenceSchema(),
					"signingSecrets": map[string]any{
						"type":     "array",
						"items":    webhookSecretReferenceSchema(),
						"minItems": 1,
						"maxItems": 8,
					},
					"timeoutSeconds": map[string]any{
						"type":    "integer",
						"minimum": 1,
						"maximum": 300,
					},
					"retry": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"maxAttempts": map[string]any{
								"type":    "integer",
								"minimum": 1,
								"maximum": 5,
							},
							"backoffSeconds": map[string]any{
								"type":    "integer",
								"minimum": 0,
								"maximum": 60,
							},
							"retryableStatusCodes": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "integer", "minimum": 100, "maximum": 599},
								"minItems":    1,
								"uniqueItems": true,
							},
						},
						"additionalProperties": false,
					},
					"expectedStatusCodes": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "integer", "minimum": 100, "maximum": 599},
						"minItems":    1,
						"uniqueItems": true,
					},
					"idempotencyKey": map[string]any{
						"type":      "string",
						"minLength": 1,
						"maxLength": 128,
						"pattern":   `^[A-Za-z0-9_.:-]+$`,
					},
					"outputs": map[string]any{
						"type":     "array",
						"maxItems": types.MaxStepRunEventOutputItemCount - webhookBuiltInOutputCount,
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"name": map[string]any{
									"type":      "string",
									"minLength": 1,
									"maxLength": 128,
									"pattern":   `^[A-Za-z_][A-Za-z0-9_.-]*$`,
								},
								"pointer": map[string]any{
									"type":      "string",
									"minLength": 1,
									"pattern":   `^/`,
								},
								"type": map[string]any{
									"type": "string",
									"enum": []any{"string", "number", "boolean", "object", "array"},
								},
								"required":  map[string]any{"type": "boolean"},
								"sensitive": map[string]any{"type": "boolean"},
							},
							"required":             []any{"name", "pointer", "type"},
							"additionalProperties": false,
						},
					},
				},
				[]any{"url"},
			),
			OutputSchema: objectSchema(
				map[string]any{
					"statusCode":         map[string]any{"type": "integer"},
					"attempts":           map[string]any{"type": "integer", "minimum": 1},
					"signingKeyVersion":  map[string]any{"type": "integer", "minimum": 1},
					"keyRotationApplied": map[string]any{"type": "boolean"},
					"auditChainRoot":     map[string]any{"type": "string", "pattern": "^sha256:[0-9a-f]{64}$"},
					"auditEventHash":     map[string]any{"type": "string", "pattern": "^sha256:[0-9a-f]{64}$"},
					"auditTrail": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"events": map[string]any{
								"type": "array",
								"items": map[string]any{
									"type": "object",
								},
							},
						},
						"required":             []any{"events"},
						"additionalProperties": false,
					},
				},
				[]any{"statusCode", "attempts", "signingKeyVersion", "keyRotationApplied", "auditChainRoot", "auditEventHash", "auditTrail"},
			),
		},
	}
}

func webhookHeaderNameSchema() map[string]any {
	return map[string]any{
		"type":    "string",
		"pattern": "^[!#$%&'*+.^_`|~0-9A-Za-z-]+$",
	}
}

func webhookSecretReferenceSchema() map[string]any {
	return map[string]any{
		"type":      "string",
		"minLength": 1,
		"maxLength": 128,
		"pattern":   `^[A-Za-z0-9_.:-]+$`,
	}
}

func validateWebhookRegistryInput(input map[string]any) error {
	signingSecret, _ := input["signingSecret"].(string)
	signingSecrets, _ := input["signingSecrets"].([]any)
	if strings.TrimSpace(signingSecret) == "" && len(signingSecrets) == 0 {
		return apierrors.NewBadRequest("webhook signingSecret or signingSecrets is required")
	}
	if strings.TrimSpace(signingSecret) != "" && len(signingSecrets) > 0 {
		return apierrors.NewBadRequest("webhook signingSecret and signingSecrets cannot both be set")
	}
	seenSigningSecrets := map[string]struct{}{}
	for _, rawSecret := range signingSecrets {
		secret, _ := rawSecret.(string)
		if strings.TrimSpace(secret) == "" {
			return apierrors.NewBadRequest("webhook signingSecrets contains empty secret")
		}
		if _, ok := seenSigningSecrets[secret]; ok {
			return apierrors.NewBadRequest("webhook signingSecrets contains duplicate secret")
		}
		seenSigningSecrets[secret] = struct{}{}
	}
	headers, _ := input["headers"].(map[string]any)
	for name := range headers {
		if isWebhookSensitiveHeaderName(name) {
			return apierrors.NewBadRequest(fmt.Sprintf("webhook headers cannot include %s; use secretHeaders", name))
		}
		if isWebhookReservedHeaderName(name) {
			return apierrors.NewBadRequest(fmt.Sprintf("webhook headers cannot include reserved header %s", name))
		}
	}
	secretHeaders, _ := input["secretHeaders"].(map[string]any)
	for name := range secretHeaders {
		if isWebhookReservedHeaderName(name) {
			return apierrors.NewBadRequest(fmt.Sprintf("webhook secretHeaders cannot include reserved header %s", name))
		}
	}
	outputs, _ := input["outputs"].([]any)
	if len(outputs) > types.MaxStepRunEventOutputItemCount-webhookBuiltInOutputCount {
		return apierrors.NewBadRequest("webhook outputs contains too many entries")
	}
	seen := map[string]struct{}{}
	for _, rawOutput := range outputs {
		output, _ := rawOutput.(map[string]any)
		name, _ := output["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if isWebhookReservedOutputName(name) {
			return apierrors.NewBadRequest(fmt.Sprintf("webhook outputs name %s is reserved", name))
		}
		if _, ok := seen[name]; ok {
			return apierrors.NewBadRequest("webhook outputs contains duplicate name")
		}
		seen[name] = struct{}{}
	}
	return nil
}

func isWebhookReservedOutputName(name string) bool {
	switch name {
	case "statusCode", "attempts", "signingKeyVersion", "keyRotationApplied", "auditChainRoot", "auditEventHash", "auditTrail":
		return true
	default:
		return false
	}
}

func isWebhookSensitiveHeaderName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "authorization", "proxy-authorization", "x-api-key", "cookie":
		return true
	default:
		return false
	}
}

func isWebhookReservedHeaderName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "idempotency-key", "x-distr-timestamp", "x-distr-body-digest", "x-distr-signature", "x-distr-key-version", "x-distr-tenant-id":
		return true
	default:
		return false
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
