package api

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCreateUpdateVariableSetRequestValidate(t *testing.T) {
	applicationID := uuid.New()
	secretID := uuid.New()

	tests := []struct {
		name    string
		request CreateUpdateVariableSetRequest
		wantErr bool
	}{
		{
			name: "accepts supported variable types",
			request: CreateUpdateVariableSetRequest{
				Name:           " Shared Defaults ",
				Description:    "Reusable defaults",
				SortOrder:      10,
				ApplicationIDs: []uuid.UUID{applicationID},
				Variables: []VariableRequest{
					{Key: " api_url ", Type: "string", DefaultValue: json.RawMessage(`"https://example.test"`)},
					{Key: "replicas", Type: "number", DefaultValue: json.RawMessage(`3`)},
					{Key: "enabled", Type: "boolean", DefaultValue: json.RawMessage(`true`)},
					{Key: "payload", Type: "json", DefaultValue: json.RawMessage(`{"mode":"safe"}`)},
					{Key: "api_token", Type: "secret_reference", ReferenceID: secretID.String()},
					{Key: "cloud_account", Type: "account_reference", ReferenceID: uuid.NewString(), ReferenceName: "Build account"},
					{Key: "tls_cert", Type: "certificate_reference", ReferenceID: uuid.NewString(), ReferenceName: "Public TLS"},
					{Key: "required_url", Type: "string", IsRequired: true},
				},
			},
		},
		{
			name: "rejects blank variable set names",
			request: CreateUpdateVariableSetRequest{
				Name: " ",
			},
			wantErr: true,
		},
		{
			name: "rejects negative sort order",
			request: CreateUpdateVariableSetRequest{
				Name:      "Shared Defaults",
				SortOrder: -1,
			},
			wantErr: true,
		},
		{
			name: "rejects empty application IDs",
			request: CreateUpdateVariableSetRequest{
				Name:           "Shared Defaults",
				ApplicationIDs: []uuid.UUID{uuid.Nil},
			},
			wantErr: true,
		},
		{
			name: "rejects blank variable keys",
			request: CreateUpdateVariableSetRequest{
				Name:      "Shared Defaults",
				Variables: []VariableRequest{{Key: " ", Type: "string", DefaultValue: json.RawMessage(`"value"`)}},
			},
			wantErr: true,
		},
		{
			name: "rejects duplicate trimmed variable keys",
			request: CreateUpdateVariableSetRequest{
				Name: "Shared Defaults",
				Variables: []VariableRequest{
					{Key: "api_url", Type: "string", DefaultValue: json.RawMessage(`"https://example.test"`)},
					{Key: " api_url ", Type: "string", DefaultValue: json.RawMessage(`"https://other.test"`)},
				},
			},
			wantErr: true,
		},
		{
			name: "rejects unknown variable types",
			request: CreateUpdateVariableSetRequest{
				Name:      "Shared Defaults",
				Variables: []VariableRequest{{Key: "api_url", Type: "unsupported", DefaultValue: json.RawMessage(`"value"`)}},
			},
			wantErr: true,
		},
		{
			name: "rejects string variables with non-string default values",
			request: CreateUpdateVariableSetRequest{
				Name:      "Shared Defaults",
				Variables: []VariableRequest{{Key: "api_url", Type: "string", DefaultValue: json.RawMessage(`42`)}},
			},
			wantErr: true,
		},
		{
			name: "rejects number variables with non-number default values",
			request: CreateUpdateVariableSetRequest{
				Name:      "Shared Defaults",
				Variables: []VariableRequest{{Key: "replicas", Type: "number", DefaultValue: json.RawMessage(`"three"`)}},
			},
			wantErr: true,
		},
		{
			name: "rejects boolean variables with non-boolean default values",
			request: CreateUpdateVariableSetRequest{
				Name:      "Shared Defaults",
				Variables: []VariableRequest{{Key: "enabled", Type: "boolean", DefaultValue: json.RawMessage(`"true"`)}},
			},
			wantErr: true,
		},
		{
			name: "rejects invalid JSON default values",
			request: CreateUpdateVariableSetRequest{
				Name:      "Shared Defaults",
				Variables: []VariableRequest{{Key: "payload", Type: "json", DefaultValue: json.RawMessage(`{`)}},
			},
			wantErr: true,
		},
		{
			name: "rejects secret references with inline default values",
			request: CreateUpdateVariableSetRequest{
				Name: "Shared Defaults",
				Variables: []VariableRequest{
					{
						Key:          "api_token",
						Type:         "secret_reference",
						DefaultValue: json.RawMessage(`"plaintext"`),
						ReferenceID:  secretID.String(),
					},
				},
			},
			wantErr: true,
		},
		{
			name: "rejects non-required secret references without reference IDs",
			request: CreateUpdateVariableSetRequest{
				Name:      "Shared Defaults",
				Variables: []VariableRequest{{Key: "api_token", Type: "secret_reference"}},
			},
			wantErr: true,
		},
		{
			name: "accepts required secret references without default references",
			request: CreateUpdateVariableSetRequest{
				Name:      "Shared Defaults",
				Variables: []VariableRequest{{Key: "api_token", Type: "secret_reference", IsRequired: true}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			err := tt.request.Validate()

			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}
