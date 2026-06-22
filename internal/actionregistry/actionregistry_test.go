package actionregistry

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestDefaultRegistryListsBuiltInActionsInRoadmapOrder(t *testing.T) {
	g := NewWithT(t)

	actions := DefaultRegistry().List()

	g.Expect(actions).To(HaveLen(6))
	g.Expect(actionTypes(actions)).To(Equal([]string{
		"distr.preflight",
		"distr.http.check",
		"distr.wait",
		"distr.compose.deploy",
		"distr.oci.job",
		"distr.file.render",
	}))
	g.Expect(actions[0].Name).To(Equal("Preflight checks"))
	g.Expect(actions[0].InputSchema).To(HaveKeyWithValue("$schema", "https://json-schema.org/draft/2020-12/schema"))
	g.Expect(actions[0].InputSchema).To(HaveKeyWithValue("type", "object"))
	g.Expect(actions[0].OutputSchema).To(HaveKeyWithValue("type", "object"))
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

func jsonObject(t *testing.T, raw string) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatalf("decode json object: %v", err)
	}
	return value
}
