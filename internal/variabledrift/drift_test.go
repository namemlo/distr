package variabledrift_test

import (
	"encoding/json"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/variabledrift"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCompareClassifiesSchemaAndConfigurationDrift(t *testing.T) {
	g := NewWithT(t)
	secretID := uuid.NewString()

	drift, err := variabledrift.Compare(
		[]types.ResolvedVariable{
			{
				Key:        "API_URL",
				Type:       types.VariableTypeString,
				Status:     types.VariableResolutionStatusResolved,
				Source:     types.VariableResolutionSourceDefault,
				Value:      json.RawMessage(`"https://new.example"`),
				IsRequired: true,
			},
			{
				Key:        "REPLICAS",
				Type:       types.VariableTypeNumber,
				Status:     types.VariableResolutionStatusUnresolved,
				Source:     types.VariableResolutionSourceUnresolved,
				IsRequired: true,
			},
			{
				Key:    "DEBUG",
				Type:   types.VariableTypeBoolean,
				Status: types.VariableResolutionStatusResolved,
				Source: types.VariableResolutionSourceDefault,
				Value:  json.RawMessage(`true`),
			},
			{
				Key:           "API_TOKEN",
				Type:          types.VariableTypeSecretReference,
				Status:        types.VariableResolutionStatusResolved,
				Source:        types.VariableResolutionSourceDefault,
				ReferenceID:   secretID,
				ReferenceName: "api_token",
				Redacted:      true,
			},
			{
				Key:    "JSON_PAYLOAD",
				Type:   types.VariableTypeJSON,
				Status: types.VariableResolutionStatusResolved,
				Source: types.VariableResolutionSourceDefault,
				Value:  json.RawMessage(`{"mode":"safe"}`),
			},
		},
		variabledrift.DeployedConfiguration{
			EnvFileData: []byte("API_URL=https://old.example\nAPI_TOKEN=runtime-secret\nOLD_SETTING=legacy\n"),
			ValuesYAML:  []byte("JSON_PAYLOAD:\n  mode: safe\nDEBUG: not-a-bool\n"),
		},
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(drift.HasDrift).To(BeTrue())
	g.Expect(drift.NewRequiredVariables).To(ConsistOf(types.ConfigurationDriftVariable{
		Key:        "REPLICAS",
		Type:       types.VariableTypeNumber,
		IsRequired: true,
		Source:     types.VariableResolutionSourceUnresolved,
	}))
	g.Expect(drift.MissingVariables).To(BeEmpty())
	g.Expect(drift.RemovedVariables).To(ConsistOf(types.ConfigurationDriftRemovedValue{Key: "OLD_SETTING"}))
	g.Expect(drift.TypeChanges).To(ConsistOf(types.ConfigurationDriftTypeChange{
		Key:          "DEBUG",
		ExpectedType: types.VariableTypeBoolean,
		DeployedType: "string",
	}))
	g.Expect(drift.DefaultChanges).To(ConsistOf(types.ConfigurationDriftDefaultChange{
		Key:           "API_URL",
		Type:          types.VariableTypeString,
		CurrentValue:  json.RawMessage(`"https://new.example"`),
		DeployedValue: json.RawMessage(`"https://old.example"`),
	}))
	g.Expect(drift.SecretReferenceChanges).To(ConsistOf(types.ConfigurationDriftReferenceChange{
		Key:           "API_TOKEN",
		Type:          types.VariableTypeSecretReference,
		ReferenceID:   secretID,
		ReferenceName: "api_token",
		Redacted:      true,
	}))
}

func TestCompareTreatsMatchingCurrentValuesAsNoDrift(t *testing.T) {
	g := NewWithT(t)

	drift, err := variabledrift.Compare(
		[]types.ResolvedVariable{
			{
				Key:    "API_URL",
				Type:   types.VariableTypeString,
				Status: types.VariableResolutionStatusResolved,
				Source: types.VariableResolutionSourceDefault,
				Value:  json.RawMessage(`"https://current.example"`),
			},
			{
				Key:    "REPLICAS",
				Type:   types.VariableTypeNumber,
				Status: types.VariableResolutionStatusResolved,
				Source: types.VariableResolutionSourceDefault,
				Value:  json.RawMessage(`3`),
			},
		},
		variabledrift.DeployedConfiguration{
			EnvFileData: []byte("API_URL=https://current.example\nREPLICAS=3\n"),
		},
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(drift.HasDrift).To(BeFalse())
	g.Expect(drift.NewRequiredVariables).To(BeEmpty())
	g.Expect(drift.MissingVariables).To(BeEmpty())
	g.Expect(drift.RemovedVariables).To(BeEmpty())
	g.Expect(drift.TypeChanges).To(BeEmpty())
	g.Expect(drift.DefaultChanges).To(BeEmpty())
	g.Expect(drift.SecretReferenceChanges).To(BeEmpty())
}
