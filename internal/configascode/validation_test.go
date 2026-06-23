package configascode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
)

func TestValidateDocumentsCanonicalizesEquivalentYAMLAndJSON(t *testing.T) {
	g := NewWithT(t)
	yamlInput := []byte(`
apiVersion: distr.sh/v1alpha1
kind: DeploymentProcess
metadata:
  name: smoke-process
  path: processes/smoke.yaml
spec:
  description: Smoke deployment
  steps:
    - key: wait
      actionType: distr.wait
      inputBindings:
        durationSeconds: 30
`)
	jsonInput := []byte(`{
  "kind": "DeploymentProcess",
  "spec": {
    "steps": [
      {
        "inputBindings": {"durationSeconds": 30},
        "actionType": "distr.wait",
        "key": "wait"
      }
    ],
    "description": "Smoke deployment"
  },
  "metadata": {
    "path": "processes/smoke.yaml",
    "name": "smoke-process"
  },
  "apiVersion": "distr.sh/v1alpha1"
}`)

	yamlResult := ValidateDocuments(yamlInput)
	jsonResult := ValidateDocuments(jsonInput)

	g.Expect(yamlResult.Valid).To(BeTrue())
	g.Expect(jsonResult.Valid).To(BeTrue())
	g.Expect(yamlResult.Documents).To(HaveLen(1))
	g.Expect(jsonResult.Documents).To(HaveLen(1))
	g.Expect(yamlResult.Documents[0].Kind).To(Equal("DeploymentProcess"))
	g.Expect(yamlResult.Documents[0].APIVersion).To(Equal("distr.sh/v1alpha1"))
	g.Expect(yamlResult.Documents[0].CanonicalChecksum).To(MatchRegexp(`^[0-9a-f]{64}$`))
	g.Expect(yamlResult.Documents[0].CanonicalChecksum).To(Equal(jsonResult.Documents[0].CanonicalChecksum))
	g.Expect(yamlResult.Errors).To(BeEmpty())
	g.Expect(jsonResult.Errors).To(BeEmpty())
}

func TestValidateDocumentsRejectsUnsupportedEnvelopeValues(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantPath  string
		wantError string
	}{
		{
			name: "unsupported api version",
			input: `
apiVersion: distr.sh/v1beta1
kind: Channel
metadata:
  name: stable
  path: channels/stable.yaml
spec:
  description: Stable channel
`,
			wantPath:  "$[0].apiVersion",
			wantError: "unsupported apiVersion",
		},
		{
			name: "unsupported kind",
			input: `
apiVersion: distr.sh/v1alpha1
kind: Widget
metadata:
  name: demo
  path: widgets/demo.yaml
spec: {}
`,
			wantPath:  "$[0].kind",
			wantError: "unsupported kind",
		},
		{
			name: "unknown envelope field",
			input: `
apiVersion: distr.sh/v1alpha1
kind: Channel
metadata:
  name: stable
  path: channels/stable.yaml
owner: ops
spec:
  description: Stable channel
`,
			wantPath:  "$[0].owner",
			wantError: "unknown field",
		},
		{
			name: "unknown kind-specific spec field",
			input: `
apiVersion: distr.sh/v1alpha1
kind: Lifecycle
metadata:
  name: default
  path: lifecycles/default.yaml
spec:
  description: Default lifecycle
  surprise: true
`,
			wantPath:  "$[0].spec.surprise",
			wantError: "unknown field",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			result := ValidateDocuments([]byte(tt.input))

			g.Expect(result.Valid).To(BeFalse())
			g.Expect(result.Errors).To(ContainElement(MatchFields(IgnoreExtras, Fields{
				"DocumentIndex": Equal(0),
				"Path":          Equal(tt.wantPath),
				"Message":       ContainSubstring(tt.wantError),
			})))
		})
	}
}

func TestValidateDocumentsRejectsUnsafeYAMLAndPaths(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantPath  string
		wantError string
	}{
		{
			name: "duplicate yaml key",
			input: `
apiVersion: distr.sh/v1alpha1
kind: Channel
kind: Lifecycle
metadata:
  name: stable
  path: channels/stable.yaml
spec: {}
`,
			wantPath:  "$[0].kind",
			wantError: "duplicate key",
		},
		{
			name: "yaml alias",
			input: `
apiVersion: distr.sh/v1alpha1
kind: Channel
metadata:
  name: stable
  path: channels/stable.yaml
spec:
  description: &desc Stable channel
  copy: *desc
`,
			wantPath:  "$[0].spec.description",
			wantError: "YAML anchors and aliases are not supported",
		},
		{
			name: "absolute metadata path",
			input: `
apiVersion: distr.sh/v1alpha1
kind: Channel
metadata:
  name: stable
  path: /etc/distr/channel.yaml
spec: {}
`,
			wantPath:  "$[0].metadata.path",
			wantError: "relative repository path",
		},
		{
			name: "traversal metadata path",
			input: `
apiVersion: distr.sh/v1alpha1
kind: Channel
metadata:
  name: stable
  path: ../channels/stable.yaml
spec: {}
`,
			wantPath:  "$[0].metadata.path",
			wantError: "traversal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			result := ValidateDocuments([]byte(tt.input))

			g.Expect(result.Valid).To(BeFalse())
			g.Expect(result.Errors).To(ContainElement(MatchFields(IgnoreExtras, Fields{
				"DocumentIndex": Equal(0),
				"Path":          Equal(tt.wantPath),
				"Message":       ContainSubstring(tt.wantError),
			})))
		})
	}
}

func TestValidateDocumentsRejectsOversizedAndExcessivelyNestedDocuments(t *testing.T) {
	g := NewWithT(t)
	oversized := []byte(strings.Repeat("a", 1048577))
	nested := []byte(`
apiVersion: distr.sh/v1alpha1
kind: Channel
metadata:
  name: too-nested
  path: channels/too-nested.yaml
spec:
` + nestedRulesYAML(80))

	oversizedResult := ValidateDocuments(oversized)
	nestedResult := ValidateDocuments(nested)

	g.Expect(oversizedResult.Valid).To(BeFalse())
	g.Expect(oversizedResult.Errors).To(ContainElement(MatchFields(IgnoreExtras, Fields{
		"Path":    Equal("$"),
		"Message": ContainSubstring("document is too large"),
	})))
	g.Expect(nestedResult.Valid).To(BeFalse())
	g.Expect(nestedResult.Errors).To(ContainElement(MatchFields(IgnoreExtras, Fields{
		"DocumentIndex": Equal(0),
		"Message":       ContainSubstring("too deeply nested"),
	})))
}

func TestValidateDocumentsRejectsPlaintextSecretsWithoutEchoingValues(t *testing.T) {
	g := NewWithT(t)
	input := []byte(`
apiVersion: distr.sh/v1alpha1
kind: VariableSetDefinition
metadata:
  name: prod-vars
  path: variable-sets/prod.yaml
spec:
  variables:
    - name: DATABASE_PASSWORD
      type: string
      default: plaintext-fixture-value
`)

	result := ValidateDocuments(input)

	g.Expect(result.Valid).To(BeFalse())
	g.Expect(result.Errors).To(ContainElement(MatchFields(IgnoreExtras, Fields{
		"DocumentIndex": Equal(0),
		"Path":          Equal("$[0].spec.variables[0].default"),
		"Message":       ContainSubstring("plaintext secret values are not allowed"),
	})))
	for _, issue := range result.Errors {
		g.Expect(issue.Message).NotTo(ContainSubstring("plaintext-fixture-value"))
	}
}

func TestValidateDocumentsAcceptsSecretReferencesWhereSchemaPermits(t *testing.T) {
	g := NewWithT(t)
	input := []byte(`
apiVersion: distr.sh/v1alpha1
kind: VariableSetDefinition
metadata:
  name: prod-vars
  path: variable-sets/prod.yaml
spec:
  variables:
    - name: DATABASE_PASSWORD
      type: secret
      secretRef: database-password
    - name: TLS_CERTIFICATE
      type: certificate
      certificateRef: prod-tls
    - name: CLOUD_ACCOUNT
      type: account
      accountRef: aws-prod
`)

	result := ValidateDocuments(input)

	g.Expect(result.Valid).To(BeTrue())
	g.Expect(result.Errors).To(BeEmpty())
	g.Expect(result.Documents).To(HaveLen(1))
	g.Expect(result.Documents[0].Kind).To(Equal("VariableSetDefinition"))
}

func TestValidateDocumentsAcceptsRepositoryExamples(t *testing.T) {
	g := NewWithT(t)
	for _, file := range []string{
		filepath.Join("..", "..", "examples", "config-as-code", "channel.yaml"),
		filepath.Join("..", "..", "examples", "config-as-code", "variable-set.json"),
	} {
		content, err := os.ReadFile(file)
		g.Expect(err).NotTo(HaveOccurred())

		result := ValidateDocuments(content)

		g.Expect(result.Valid).To(BeTrue(), file)
		g.Expect(result.Errors).To(BeEmpty(), file)
	}
}

func nestedRulesYAML(depth int) string {
	var b strings.Builder
	for i := 0; i < depth; i++ {
		b.WriteString(strings.Repeat("  ", i+1))
		b.WriteString("rules:\n")
	}
	b.WriteString(strings.Repeat("  ", depth+1))
	b.WriteString("- name: final\n")
	return b.String()
}
