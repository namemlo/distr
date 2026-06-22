package actionregistry

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestDefaultRegistryListsBuiltInActionsInRoadmapOrder(t *testing.T) {
	g := NewWithT(t)

	actions := DefaultRegistry().List()

	g.Expect(actions).To(HaveLen(7))
	g.Expect(actionTypes(actions)).To(Equal([]string{
		"distr.preflight",
		"distr.http.check",
		"distr.wait",
		"distr.compose.deploy",
		"distr.oci.job",
		"distr.file.render",
		"distr.webhook",
	}))
	g.Expect(actions[0].Name).To(Equal("Preflight checks"))
	g.Expect(actions[0].InputSchema).To(HaveKeyWithValue("$schema", "https://json-schema.org/draft/2020-12/schema"))
	g.Expect(actions[0].InputSchema).To(HaveKeyWithValue("type", "object"))
	g.Expect(actions[0].OutputSchema).To(HaveKeyWithValue("type", "object"))
}

func TestRegistryContainsWebhookV1(t *testing.T) {
	g := NewWithT(t)

	action, ok := DefaultRegistry().Get("distr.webhook")

	g.Expect(ok).To(BeTrue())
	g.Expect(action.Type).To(Equal("distr.webhook"))
	g.Expect(actionCapabilityID(action.Type, types.AgentActionVersionV1)).To(Equal("distr.webhook.v1"))
}

func TestDefaultRegistryReturnsActionByType(t *testing.T) {
	g := NewWithT(t)

	action, ok := DefaultRegistry().Get("distr.http.check")

	g.Expect(ok).To(BeTrue())
	g.Expect(action.Type).To(Equal("distr.http.check"))
	g.Expect(action.Name).To(Equal("HTTP check"))
	g.Expect(action.Description).To(ContainSubstring("HTTP endpoint"))
}

func TestDefaultRegistryValidatesKnownActionInputs(t *testing.T) {
	registry := DefaultRegistry()
	g := NewWithT(t)

	g.Expect(registry.ValidateInput("distr.preflight", jsonObject(t, `{}`))).To(Succeed())
	g.Expect(registry.ValidateInput("distr.http.check", jsonObject(t, `{
		"url":"https://example.com/health",
		"method":"GET",
		"expectedStatusCodes":[200,204],
		"maxLatencyMs":1000,
		"retry":{"maxAttempts":3,"intervalSeconds":5}
	}`))).To(Succeed())
	g.Expect(registry.ValidateInput("distr.wait", jsonObject(t, `{"durationSeconds":30}`))).To(Succeed())
	g.Expect(registry.ValidateInput("distr.wait", jsonObject(t, `{"condition":"deployment.healthy"}`))).To(Succeed())
	g.Expect(registry.ValidateInput("distr.compose.deploy", jsonObject(t, `{
		"applicationVersion":{
			"composeFile":"services:\n  web:\n    image: nginx:latest\n",
			"registryAuth":{
				"registry.example.com":{
					"username":"user",
					"passwordSecretRef":"docker_password"
				}
			}
		},
		"projectName":"distr-preview",
		"environmentFile":"PORT=8080\n",
		"pullPolicy":"missing",
		"waitForHealthy":true,
		"timeoutSeconds":120,
		"strategy":"compose"
	}`))).To(Succeed())
	g.Expect(registry.ValidateInput("distr.oci.job", jsonObject(t, `{
		"imageDigest":"registry.example.com/jobs/cleanup@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"command":["/bin/cleanup"],
		"arguments":["--tenant","demo"],
		"environment":{"MODE":"once"},
		"secretEnvironment":{"API_TOKEN":"job_api_token"},
		"network":"none",
		"volumes":[{"source":"/var/lib/distr/jobs","target":"/work","readOnly":true}],
		"timeoutSeconds":300,
		"expectedExitCodes":[0],
		"idempotencyKey":"cleanup-demo",
		"runAsUser":"1000:1000",
		"resources":{"cpus":0.5,"memoryBytes":134217728},
		"security":{"readOnlyRootFilesystem":true,"dropCapabilities":["ALL"]}
	}`))).To(Succeed())
	g.Expect(registry.ValidateInput("distr.file.render", jsonObject(t, `{
		"destinationPath":"app/config/runtime.json",
		"template":"{\"apiUrl\":\"${apiUrl}\",\"token\":\"${secrets.apiToken}\"}\n",
		"variables":{"apiUrl":"https://api.example.com"},
		"secretVariables":{"apiToken":"api_token"},
		"mode":"0640",
		"owner":"1000",
		"group":"1000",
		"backup":true,
		"idempotencyKey":"runtime-config",
		"timeoutSeconds":30
	}`))).To(Succeed())
	g.Expect(registry.ValidateInput("distr.webhook", jsonObject(t, `{
		"url":"https://hooks.example.com/deployments",
		"method":"POST",
		"headers":{"X-Deployment":"demo"},
		"secretHeaders":{"Authorization":"webhook_auth_token"},
		"body":{"deploymentId":"dep-123"},
		"sensitiveBody":true,
		"signingSecret":"webhook_signing_key",
		"timeoutSeconds":30,
		"retry":{"maxAttempts":3,"backoffSeconds":1,"retryableStatusCodes":[429,500,502,503,504]},
		"expectedStatusCodes":[200,202],
		"idempotencyKey":"notify-demo",
		"outputs":[
			{"name":"remoteId","pointer":"/id","type":"string","required":true},
			{"name":"accepted","pointer":"/accepted","type":"boolean","sensitive":true}
		]
	}`))).To(Succeed())
}

func TestDefaultRegistryValidatesWebhookSigningSecretRotation(t *testing.T) {
	registry := DefaultRegistry()
	g := NewWithT(t)

	g.Expect(registry.ValidateInput("distr.webhook", jsonObject(t, `{
		"url":"https://hooks.example.com/deployments",
		"method":"POST",
		"secretHeaders":{"Authorization":"webhook_auth_token"},
		"signingSecrets":["webhook_signing_key_v1","webhook_signing_key_v2"],
		"expectedStatusCodes":[200,202],
		"idempotencyKey":"notify-demo"
	}`))).To(Succeed())
}

func TestDefaultRegistryRejectsUnknownActionAndInvalidInputs(t *testing.T) {
	registry := DefaultRegistry()

	tests := []struct {
		name       string
		actionType string
		input      map[string]any
		want       string
	}{
		{
			name:       "unknown action",
			actionType: "script",
			input:      jsonObject(t, `{}`),
			want:       `unknown actionType "script"`,
		},
		{
			name:       "preflight rejects undeclared inputs",
			actionType: "distr.preflight",
			input:      jsonObject(t, `{"unexpected":true}`),
			want:       "additional properties",
		},
		{
			name:       "http check requires url",
			actionType: "distr.http.check",
			input:      jsonObject(t, `{"expectedStatusCodes":[200]}`),
			want:       "url",
		},
		{
			name:       "wait requires positive duration",
			actionType: "distr.wait",
			input:      jsonObject(t, `{"durationSeconds":0}`),
			want:       "durationSeconds",
		},
		{
			name:       "wait requires duration or condition",
			actionType: "distr.wait",
			input:      jsonObject(t, `{}`),
			want:       "oneOf",
		},
		{
			name:       "compose deploy requires compose file",
			actionType: "distr.compose.deploy",
			input:      jsonObject(t, `{"applicationVersion":{},"projectName":"distr-preview"}`),
			want:       "composeFile",
		},
		{
			name:       "compose deploy rejects unknown strategy",
			actionType: "distr.compose.deploy",
			input: jsonObject(t, `{
				"applicationVersion":{"composeFile":"services:\n  web:\n    image: nginx:latest\n"},
				"projectName":"distr-preview",
				"strategy":"blue-green"
			}`),
			want: "strategy",
		},
		{
			name:       "compose deploy rejects plaintext registry password",
			actionType: "distr.compose.deploy",
			input: jsonObject(t, `{
				"applicationVersion":{
					"composeFile":"services:\n  web:\n    image: nginx:latest\n",
					"registryAuth":{
						"registry.example.com":{
							"username":"user",
							"password":"plain-secret"
						}
					}
				},
				"projectName":"distr-preview"
			}`),
			want: "password",
		},
		{
			name:       "oci job rejects mutable image tag",
			actionType: "distr.oci.job",
			input: jsonObject(t, `{
				"imageDigest":"registry.example.com/jobs/cleanup:latest",
				"command":["/bin/cleanup"]
			}`),
			want: "imageDigest",
		},
		{
			name:       "oci job requires command",
			actionType: "distr.oci.job",
			input: jsonObject(t, `{
				"imageDigest":"registry.example.com/jobs/cleanup@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			}`),
			want: "command",
		},
		{
			name:       "oci job rejects privileged execution",
			actionType: "distr.oci.job",
			input: jsonObject(t, `{
				"imageDigest":"registry.example.com/jobs/cleanup@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"command":["/bin/cleanup"],
				"security":{"privileged":true}
			}`),
			want: "privileged",
		},
		{
			name:       "oci job rejects writable root filesystem",
			actionType: "distr.oci.job",
			input: jsonObject(t, `{
				"imageDigest":"registry.example.com/jobs/cleanup@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"command":["/bin/cleanup"],
				"security":{"readOnlyRootFilesystem":false}
			}`),
			want: "readOnlyRootFilesystem",
		},
		{
			name:       "file render requires destination path",
			actionType: "distr.file.render",
			input: jsonObject(t, `{
				"template":"PORT=${port}\n",
				"variables":{"port":"8080"}
			}`),
			want: "destinationPath",
		},
		{
			name:       "file render rejects absolute destination path",
			actionType: "distr.file.render",
			input: jsonObject(t, `{
				"destinationPath":"/etc/passwd",
				"template":"PORT=${port}\n",
				"variables":{"port":"8080"}
			}`),
			want: "destinationPath",
		},
		{
			name:       "file render rejects invalid mode",
			actionType: "distr.file.render",
			input: jsonObject(t, `{
				"destinationPath":"app/config/runtime.env",
				"template":"PORT=${port}\n",
				"variables":{"port":"8080"},
				"mode":"9999"
			}`),
			want: "mode",
		},
		{
			name:       "webhook rejects non-https url",
			actionType: "distr.webhook",
			input: jsonObject(t, `{
				"url":"http://hooks.example.com/deployments",
				"signingSecret":"webhook_signing_key"
			}`),
			want: "url",
		},
		{
			name:       "webhook rejects plaintext authorization header",
			actionType: "distr.webhook",
			input: jsonObject(t, `{
				"url":"https://hooks.example.com/deployments",
				"headers":{"Authorization":"Bearer plain-secret"},
				"signingSecret":"webhook_signing_key"
			}`),
			want: "Authorization",
		},
		{
			name:       "webhook rejects malformed output declaration",
			actionType: "distr.webhook",
			input: jsonObject(t, `{
				"url":"https://hooks.example.com/deployments",
				"signingSecret":"webhook_signing_key",
				"outputs":[{"name":"remoteId","pointer":"id","type":"string"}]
			}`),
			want: "pointer",
		},
		{
			name:       "webhook rejects reserved output name",
			actionType: "distr.webhook",
			input: jsonObject(t, `{
				"url":"https://hooks.example.com/deployments",
				"signingSecret":"webhook_signing_key",
				"outputs":[{"name":"attempts","pointer":"/attempts","type":"number"}]
			}`),
			want: "reserved",
		},
		{
			name:       "webhook rejects invalid retry policy",
			actionType: "distr.webhook",
			input: jsonObject(t, `{
				"url":"https://hooks.example.com/deployments",
				"signingSecret":"webhook_signing_key",
				"retry":{"maxAttempts":0}
			}`),
			want: "maxAttempts",
		},
		{
			name:       "webhook rejects missing signing secret config",
			actionType: "distr.webhook",
			input: jsonObject(t, `{
				"url":"https://hooks.example.com/deployments"
			}`),
			want: "signing",
		},
		{
			name:       "webhook rejects duplicate signing secrets",
			actionType: "distr.webhook",
			input: jsonObject(t, `{
				"url":"https://hooks.example.com/deployments",
				"signingSecrets":["webhook_signing_key","webhook_signing_key"]
			}`),
			want: "duplicate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			err := registry.ValidateInput(tt.actionType, tt.input)

			g.Expect(err).To(HaveOccurred())
			g.Expect(strings.ToLower(err.Error())).To(ContainSubstring(strings.ToLower(tt.want)))
		})
	}
}

func TestDefaultRegistryRejectsWebhookTooManyDeclaredOutputs(t *testing.T) {
	registry := DefaultRegistry()
	g := NewWithT(t)
	outputs := make([]any, 0, types.MaxStepRunEventOutputItemCount-1)
	for i := 0; i < types.MaxStepRunEventOutputItemCount-1; i++ {
		outputs = append(outputs, map[string]any{
			"name":    fmt.Sprintf("remoteId%d", i),
			"pointer": fmt.Sprintf("/items/%d/id", i),
			"type":    "string",
		})
	}

	err := registry.ValidateInput("distr.webhook", map[string]any{
		"url":           "https://hooks.example.com/deployments",
		"signingSecret": "webhook_signing_key",
		"outputs":       outputs,
	})

	g.Expect(err).To(HaveOccurred())
	g.Expect(strings.ToLower(err.Error())).To(ContainSubstring("maxitems"))
}

func TestDefaultRegistryValidatesDeploymentProcessSteps(t *testing.T) {
	registry := DefaultRegistry()
	g := NewWithT(t)
	steps := []types.DeploymentProcessStep{
		{
			Key:           "check",
			ActionType:    "distr.http.check",
			InputBindings: jsonObject(t, `{"url":"https://example.com/health"}`),
		},
		{
			Key:           "pause",
			ActionType:    "distr.wait",
			InputBindings: jsonObject(t, `{"durationSeconds":10}`),
		},
	}

	g.Expect(registry.ValidateSteps(steps)).To(Succeed())

	steps[1].InputBindings = jsonObject(t, `{"durationSeconds":0}`)
	err := registry.ValidateSteps(steps)

	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring(`step "pause"`))
	g.Expect(err.Error()).To(ContainSubstring("durationSeconds"))
}

func actionTypes(actions []types.ActionDefinition) []string {
	values := make([]string, 0, len(actions))
	for _, action := range actions {
		values = append(values, action.Type)
	}
	return values
}

func actionCapabilityID(actionType, version string) string {
	return actionType + ".v" + strings.TrimPrefix(version, "v")
}

func jsonObject(t *testing.T, raw string) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatalf("decode json object: %v", err)
	}
	return value
}
