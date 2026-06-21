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

func TestVariableSnapshotToAPI(t *testing.T) {
	g := NewWithT(t)
	snapshotID := uuid.New()
	releaseBundleID := uuid.New()
	applicationID := uuid.New()
	channelID := uuid.New()
	variableSetID := uuid.New()
	variableID := uuid.New()
	valueID := uuid.New()
	createdAt := time.Date(2026, 6, 21, 9, 30, 0, 0, time.UTC)

	response := VariableSnapshotToAPI(types.VariableSnapshot{
		ID:                snapshotID,
		CreatedAt:         createdAt,
		ReleaseBundleID:   releaseBundleID,
		ApplicationID:     applicationID,
		ChannelID:         channelID,
		CanonicalChecksum: "sha256:abc",
		Values: []types.VariableSnapshotValue{
			{
				ID:                 valueID,
				VariableSnapshotID: snapshotID,
				VariableSetID:      variableSetID,
				VariableID:         variableID,
				Key:                "API_TOKEN",
				Type:               types.VariableTypeSecretReference,
				Status:             types.VariableResolutionStatusResolved,
				Source:             types.VariableResolutionSourceDefault,
				ReferenceID:        "secret-1",
				ReferenceName:      "api_token",
				Redacted:           true,
				Trace: []types.VariableResolutionTraceEntry{
					{
						Source:   types.VariableResolutionSourceDefault,
						Selected: true,
						Reason:   "default reference selected",
					},
				},
			},
		},
	})

	g.Expect(response).To(Equal(api.VariableSnapshot{
		ID:                snapshotID,
		CreatedAt:         createdAt,
		ReleaseBundleID:   releaseBundleID,
		ApplicationID:     applicationID,
		ChannelID:         channelID,
		CanonicalChecksum: "sha256:abc",
		Values: []api.VariableSnapshotValue{
			{
				ID:                 valueID,
				VariableSnapshotID: snapshotID,
				VariableSetID:      variableSetID,
				VariableID:         variableID,
				Key:                "API_TOKEN",
				Type:               api.VariableTypeSecretReference,
				Status:             string(types.VariableResolutionStatusResolved),
				Source:             string(types.VariableResolutionSourceDefault),
				ReferenceID:        "secret-1",
				ReferenceName:      "api_token",
				Redacted:           true,
				Trace: []api.VariableResolutionTraceEntry{
					{
						Source:   string(types.VariableResolutionSourceDefault),
						Selected: true,
						Reason:   "default reference selected",
					},
				},
			},
		},
	}))
}

func TestConfigurationDriftToAPI(t *testing.T) {
	g := NewWithT(t)
	deploymentID := uuid.New()
	applicationID := uuid.New()

	response := ConfigurationDriftToAPI(types.ConfigurationDrift{
		DeploymentID:  deploymentID,
		ApplicationID: applicationID,
		HasDrift:      true,
		NewRequiredVariables: []types.ConfigurationDriftVariable{
			{
				Key:        "REPLICAS",
				Type:       types.VariableTypeNumber,
				IsRequired: true,
				Source:     types.VariableResolutionSourceUnresolved,
			},
		},
		MissingVariables: []types.ConfigurationDriftVariable{
			{
				Key:    "DEBUG",
				Type:   types.VariableTypeBoolean,
				Source: types.VariableResolutionSourceDefault,
				Value:  json.RawMessage(`true`),
			},
		},
		RemovedVariables: []types.ConfigurationDriftRemovedValue{
			{Key: "OLD_SETTING"},
		},
		TypeChanges: []types.ConfigurationDriftTypeChange{
			{Key: "PORT", ExpectedType: types.VariableTypeNumber, DeployedType: "string"},
		},
		DefaultChanges: []types.ConfigurationDriftDefaultChange{
			{
				Key:           "API_URL",
				Type:          types.VariableTypeString,
				CurrentValue:  json.RawMessage(`"https://new.example"`),
				DeployedValue: json.RawMessage(`"https://old.example"`),
			},
		},
		SecretReferenceChanges: []types.ConfigurationDriftReferenceChange{
			{
				Key:           "API_TOKEN",
				Type:          types.VariableTypeSecretReference,
				ReferenceID:   "secret-1",
				ReferenceName: "api_token",
				Redacted:      true,
			},
		},
	})

	g.Expect(response).To(Equal(api.ConfigurationDrift{
		DeploymentID:  deploymentID,
		ApplicationID: applicationID,
		HasDrift:      true,
		NewRequiredVariables: []api.ConfigurationDriftVariable{
			{
				Key:        "REPLICAS",
				Type:       api.VariableTypeNumber,
				IsRequired: true,
				Source:     string(types.VariableResolutionSourceUnresolved),
			},
		},
		MissingVariables: []api.ConfigurationDriftVariable{
			{
				Key:    "DEBUG",
				Type:   api.VariableTypeBoolean,
				Source: string(types.VariableResolutionSourceDefault),
				Value:  json.RawMessage(`true`),
			},
		},
		RemovedVariables: []api.ConfigurationDriftRemovedValue{
			{Key: "OLD_SETTING"},
		},
		TypeChanges: []api.ConfigurationDriftTypeChange{
			{Key: "PORT", ExpectedType: api.VariableTypeNumber, DeployedType: "string"},
		},
		DefaultChanges: []api.ConfigurationDriftDefaultChange{
			{
				Key:           "API_URL",
				Type:          api.VariableTypeString,
				CurrentValue:  json.RawMessage(`"https://new.example"`),
				DeployedValue: json.RawMessage(`"https://old.example"`),
			},
		},
		SecretReferenceChanges: []api.ConfigurationDriftReferenceChange{
			{
				Key:           "API_TOKEN",
				Type:          api.VariableTypeSecretReference,
				ReferenceID:   "secret-1",
				ReferenceName: "api_token",
				Redacted:      true,
			},
		},
	}))
}
