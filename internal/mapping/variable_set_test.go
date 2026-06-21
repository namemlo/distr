package mapping

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestVariableSetToAPI(t *testing.T) {
	g := NewWithT(t)
	id := uuid.New()
	orgID := uuid.New()
	applicationID := uuid.New()
	variableID := uuid.New()
	scopedSecretID := uuid.New()
	scopedPayloadID := uuid.New()
	secretReferenceID := uuid.New()
	createdAt := time.Date(2026, 6, 21, 9, 30, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 6, 21, 10, 45, 0, 0, time.UTC)

	res := VariableSetToAPI(types.VariableSet{
		ID:             id,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
		OrganizationID: orgID,
		Name:           "Shared Defaults",
		Description:    "Reusable defaults",
		SortOrder:      10,
		ApplicationIDs: []uuid.UUID{applicationID},
		Variables: []types.Variable{
			{
				ID:             variableID,
				CreatedAt:      createdAt,
				UpdatedAt:      updatedAt,
				OrganizationID: orgID,
				VariableSetID:  id,
				Key:            "api_token",
				Type:           types.VariableTypeSecretReference,
				ReferenceID:    secretReferenceID.String(),
				ReferenceName:  "api_token",
				ScopedValues: []types.VariableScopedValue{
					{
						ID:            scopedSecretID,
						CreatedAt:     createdAt,
						UpdatedAt:     updatedAt,
						VariableSetID: id,
						VariableID:    variableID,
						Scope:         types.VariableScope{ApplicationID: &applicationID},
						ReferenceID:   secretReferenceID.String(),
						ReferenceName: "application_api_token",
					},
				},
			},
			{
				ID:            uuid.New(),
				CreatedAt:     createdAt,
				UpdatedAt:     updatedAt,
				VariableSetID: id,
				Key:           "payload",
				Type:          types.VariableTypeJSON,
				DefaultValue:  json.RawMessage(`{"mode":"safe"}`),
				ScopedValues: []types.VariableScopedValue{
					{
						ID:            scopedPayloadID,
						CreatedAt:     createdAt,
						UpdatedAt:     updatedAt,
						VariableSetID: id,
						VariableID:    variableID,
						Scope:         types.VariableScope{ApplicationID: &applicationID},
						Value:         json.RawMessage(`{"mode":"application"}`),
					},
				},
			},
		},
	})

	g.Expect(res).To(Equal(api.VariableSet{
		ID:             id,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
		Name:           "Shared Defaults",
		Description:    "Reusable defaults",
		SortOrder:      10,
		ApplicationIDs: []uuid.UUID{applicationID},
		Variables: []api.Variable{
			{
				ID:            variableID,
				CreatedAt:     createdAt,
				UpdatedAt:     updatedAt,
				Key:           "api_token",
				Type:          api.VariableTypeSecretReference,
				ReferenceID:   res.Variables[0].ReferenceID,
				ReferenceName: "api_token",
				ScopedValues: []api.VariableScopedValue{
					{
						ID:            scopedSecretID,
						CreatedAt:     createdAt,
						UpdatedAt:     updatedAt,
						Scope:         api.VariableScope{ApplicationID: &applicationID},
						ReferenceID:   secretReferenceID.String(),
						ReferenceName: "application_api_token",
					},
				},
			},
			{
				ID:           res.Variables[1].ID,
				CreatedAt:    createdAt,
				UpdatedAt:    updatedAt,
				Key:          "payload",
				Type:         api.VariableTypeJSON,
				DefaultValue: json.RawMessage(`{"mode":"safe"}`),
				ScopedValues: []api.VariableScopedValue{
					{
						ID:        scopedPayloadID,
						CreatedAt: createdAt,
						UpdatedAt: updatedAt,
						Scope:     api.VariableScope{ApplicationID: &applicationID},
						Value:     json.RawMessage(`{"mode":"application"}`),
					},
				},
			},
		},
	}))
}
