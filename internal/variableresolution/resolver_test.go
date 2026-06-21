package variableresolution_test

import (
	"encoding/json"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/variableresolution"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestResolveSelectsPromptedValueBeforeScopedAndDefaultValues(t *testing.T) {
	g := NewWithT(t)
	fixture := newResolverPrecedenceFixture()

	resolved, err := variableresolution.Resolve(variableresolution.Request{
		Variables: []types.Variable{fixture.variable()},
		Scope:     fixture.scope(),
		PromptedValues: []types.VariablePromptedValue{
			{
				Key:   "api_url",
				Value: json.RawMessage(`"https://prompted.example"`),
			},
		},
	})

	g.Expect(err).NotTo(HaveOccurred())
	apiURL := resolvedVariableByKey(resolved, "api_url")
	g.Expect(apiURL.Status).To(Equal(types.VariableResolutionStatusResolved))
	g.Expect(apiURL.Source).To(Equal(types.VariableResolutionSourcePrompted))
	g.Expect(apiURL.Value).To(MatchJSON(`"https://prompted.example"`))
	g.Expect(apiURL.Trace).NotTo(BeEmpty())
	g.Expect(apiURL.Trace[0].Source).To(Equal(types.VariableResolutionSourcePrompted))
	g.Expect(apiURL.Trace[0].Selected).To(BeTrue())
}

func TestResolveUsesDeterministicPrecedenceWhenPromptedValueIsAbsent(t *testing.T) {
	g := NewWithT(t)
	fixture := newResolverPrecedenceFixture()

	resolved, err := variableresolution.Resolve(variableresolution.Request{
		Variables: []types.Variable{fixture.variable()},
		Scope:     fixture.scope(),
	})

	g.Expect(err).NotTo(HaveOccurred())
	apiURL := resolvedVariableByKey(resolved, "api_url")
	g.Expect(apiURL.Status).To(Equal(types.VariableResolutionStatusResolved))
	g.Expect(apiURL.Source).To(Equal(types.VariableResolutionSourceExactTenantEnvironmentTargetChannelStep))
	g.Expect(apiURL.Value).To(MatchJSON(`"https://exact.example"`))
	g.Expect(apiURL.Trace[0].Selected).To(BeTrue())
	g.Expect(apiURL.Trace[0].Source).To(Equal(types.VariableResolutionSourceExactTenantEnvironmentTargetChannelStep))
}

func TestResolveReportsRequiredVariablesAsUnresolvedAndRedactsSecretReferences(t *testing.T) {
	g := NewWithT(t)
	secretID := uuid.NewString()

	resolved, err := variableresolution.Resolve(variableresolution.Request{
		Variables: []types.Variable{
			{
				Key:        "api_url",
				Type:       types.VariableTypeString,
				IsRequired: true,
			},
			{
				Key:           "api_token",
				Type:          types.VariableTypeSecretReference,
				ReferenceID:   secretID,
				ReferenceName: "api_token",
			},
		},
	})

	g.Expect(err).NotTo(HaveOccurred())
	apiURL := resolvedVariableByKey(resolved, "api_url")
	g.Expect(apiURL.Status).To(Equal(types.VariableResolutionStatusUnresolved))
	g.Expect(apiURL.Value).To(BeNil())
	g.Expect(apiURL.Trace[0].Source).To(Equal(types.VariableResolutionSourceUnresolved))

	apiToken := resolvedVariableByKey(resolved, "api_token")
	g.Expect(apiToken.Status).To(Equal(types.VariableResolutionStatusResolved))
	g.Expect(apiToken.Source).To(Equal(types.VariableResolutionSourceDefault))
	g.Expect(apiToken.Value).To(BeNil())
	g.Expect(apiToken.ReferenceID).To(Equal(secretID))
	g.Expect(apiToken.ReferenceName).To(Equal("api_token"))
	g.Expect(apiToken.Redacted).To(BeTrue())
}

type resolverPrecedenceFixture struct {
	applicationID          uuid.UUID
	channelID              uuid.UUID
	customerOrganizationID uuid.UUID
	environmentID          uuid.UUID
	deploymentTargetID     uuid.UUID
}

func newResolverPrecedenceFixture() resolverPrecedenceFixture {
	return resolverPrecedenceFixture{
		applicationID:          uuid.New(),
		channelID:              uuid.New(),
		customerOrganizationID: uuid.New(),
		environmentID:          uuid.New(),
		deploymentTargetID:     uuid.New(),
	}
}

func (f resolverPrecedenceFixture) variable() types.Variable {
	return types.Variable{
		Key:          "api_url",
		Type:         types.VariableTypeString,
		DefaultValue: json.RawMessage(`"https://default.example"`),
		ScopedValues: []types.VariableScopedValue{
			{
				Scope: types.VariableScope{ApplicationID: &f.applicationID},
				Value: json.RawMessage(`"https://application.example"`),
			},
			{
				Scope: types.VariableScope{ChannelID: &f.channelID},
				Value: json.RawMessage(`"https://channel.example"`),
			},
			{
				Scope: types.VariableScope{EnvironmentID: &f.environmentID},
				Value: json.RawMessage(`"https://environment.example"`),
			},
			{
				Scope: types.VariableScope{EnvironmentID: &f.environmentID, TargetTag: "linux"},
				Value: json.RawMessage(`"https://tag.example"`),
			},
			{
				Scope: types.VariableScope{
					CustomerOrganizationID: &f.customerOrganizationID,
					EnvironmentID:          &f.environmentID,
				},
				Value: json.RawMessage(`"https://tenant-environment.example"`),
			},
			{
				Scope: types.VariableScope{
					CustomerOrganizationID: &f.customerOrganizationID,
					EnvironmentID:          &f.environmentID,
					ChannelID:              &f.channelID,
				},
				Value: json.RawMessage(`"https://tenant-environment-channel.example"`),
			},
			{
				Scope: types.VariableScope{
					CustomerOrganizationID: &f.customerOrganizationID,
					EnvironmentID:          &f.environmentID,
					DeploymentTargetID:     &f.deploymentTargetID,
				},
				Value: json.RawMessage(`"https://tenant-environment-target.example"`),
			},
			{
				Scope: types.VariableScope{
					CustomerOrganizationID: &f.customerOrganizationID,
					EnvironmentID:          &f.environmentID,
					DeploymentTargetID:     &f.deploymentTargetID,
					ChannelID:              &f.channelID,
					ProcessStepKey:         "deploy",
				},
				Value: json.RawMessage(`"https://exact.example"`),
			},
		},
	}
}

func (f resolverPrecedenceFixture) scope() types.VariableResolutionScope {
	return types.VariableResolutionScope{
		ApplicationID:          &f.applicationID,
		ChannelID:              &f.channelID,
		CustomerOrganizationID: &f.customerOrganizationID,
		EnvironmentID:          &f.environmentID,
		DeploymentTargetID:     &f.deploymentTargetID,
		TargetTags:             []string{"linux", "gpu"},
		ProcessStepKey:         "deploy",
	}
}

func resolvedVariableByKey(variables []types.ResolvedVariable, key string) types.ResolvedVariable {
	for _, variable := range variables {
		if variable.Key == key {
			return variable
		}
	}
	return types.ResolvedVariable{}
}
