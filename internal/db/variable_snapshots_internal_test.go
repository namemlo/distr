package db

import (
	"encoding/json"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/variableresolution"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
)

func TestVariableSnapshotCanonicalPayloadPreservesTargetScopedCandidates(t *testing.T) {
	g := NewWithT(t)
	applicationID := uuid.New()
	channelID := uuid.New()
	environmentID := uuid.New()
	referenceCustomerID := uuid.New()
	referenceTargetID := uuid.New()
	secondaryCustomerID := uuid.New()
	secondaryTargetID := uuid.New()
	variable := types.Variable{
		ID:            uuid.New(),
		VariableSetID: uuid.New(),
		Key:           "api_url",
		Type:          types.VariableTypeString,
		IsRequired:    true,
		ScopedValues: []types.VariableScopedValue{
			{
				Scope: types.VariableScope{
					CustomerOrganizationID: &referenceCustomerID,
					EnvironmentID:          &environmentID,
					DeploymentTargetID:     &referenceTargetID,
				},
				Value: json.RawMessage(`"https://reference.example"`),
			},
			{
				Scope: types.VariableScope{
					CustomerOrganizationID: &secondaryCustomerID,
					EnvironmentID:          &environmentID,
					DeploymentTargetID:     &secondaryTargetID,
				},
				Value: json.RawMessage(`"https://secondary.example"`),
			},
		},
	}
	bundle := types.ReleaseBundle{
		ID: uuid.New(), ApplicationID: applicationID, ChannelID: channelID,
	}
	payload, _, err := canonicalizeVariableSnapshot(bundle, nil, []types.Variable{variable})
	g.Expect(err).NotTo(HaveOccurred())
	snapshot := types.VariableSnapshot{CanonicalPayload: payload}
	g.Expect(hydrateVariableSnapshotResolution(&snapshot)).To(Succeed())
	g.Expect(snapshot.ResolutionMode).To(Equal(types.VariableSnapshotResolutionModeTarget))
	g.Expect(snapshot.Variables).To(HaveLen(1))
	g.Expect(snapshot.Variables[0].ScopedValues).To(HaveLen(2))

	resolve := func(customerID, targetID uuid.UUID) types.ResolvedVariable {
		resolved, resolveErr := variableresolution.Resolve(variableresolution.Request{
			Variables: snapshot.Variables,
			Scope: types.VariableResolutionScope{
				CustomerOrganizationID: &customerID,
				EnvironmentID:          &environmentID,
				ChannelID:              &channelID,
				DeploymentTargetID:     &targetID,
				ApplicationID:          &applicationID,
			},
		})
		g.Expect(resolveErr).NotTo(HaveOccurred())
		g.Expect(resolved).To(HaveLen(1))
		return resolved[0]
	}
	g.Expect(resolve(referenceCustomerID, referenceTargetID).Value).To(MatchJSON(`"https://reference.example"`))
	g.Expect(resolve(secondaryCustomerID, secondaryTargetID).Value).To(MatchJSON(`"https://secondary.example"`))
	g.Expect(resolve(uuid.New(), uuid.New()).Status).To(Equal(types.VariableResolutionStatusUnresolved))
}

func TestVariableSnapshotCanonicalPayloadWithoutCandidatesRemainsLegacy(t *testing.T) {
	g := NewWithT(t)
	payload, err := json.Marshal(canonicalVariableSnapshot{
		ReleaseBundleID: uuid.NewString(),
		ApplicationID:   uuid.NewString(),
		ChannelID:       uuid.NewString(),
		Values:          []canonicalVariableSnapshotValue{},
	})
	g.Expect(err).NotTo(HaveOccurred())
	snapshot := types.VariableSnapshot{CanonicalPayload: payload}

	g.Expect(hydrateVariableSnapshotResolution(&snapshot)).To(Succeed())
	g.Expect(snapshot.ResolutionMode).To(Equal(types.VariableSnapshotResolutionModeLegacy))
	g.Expect(snapshot.Variables).To(BeEmpty())
}

func TestDeploymentPlanTargetResolutionFreezesOneTargetAndBlocksMixedTargetValues(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	applicationID := uuid.New()
	channelID := uuid.New()
	environmentID := uuid.New()
	referenceCustomerID := uuid.New()
	referenceTargetID := uuid.New()
	secondaryCustomerID := uuid.New()
	secondaryTargetID := uuid.New()
	variable := types.Variable{
		ID: uuid.New(), VariableSetID: uuid.New(), Key: "api_url",
		Type: types.VariableTypeString, IsRequired: true,
		ScopedValues: []types.VariableScopedValue{
			{
				Scope: types.VariableScope{
					CustomerOrganizationID: &referenceCustomerID,
					EnvironmentID:          &environmentID,
					DeploymentTargetID:     &referenceTargetID,
				},
				Value: json.RawMessage(`"https://reference.example"`),
			},
			{
				Scope: types.VariableScope{
					CustomerOrganizationID: &secondaryCustomerID,
					EnvironmentID:          &environmentID,
					DeploymentTargetID:     &secondaryTargetID,
				},
				Value: json.RawMessage(`"https://secondary.example"`),
			},
		},
	}
	referenceTarget := types.DeploymentPlanTarget{
		OrganizationID: organizationID, DeploymentTargetID: referenceTargetID,
		CustomerOrganizationID: &referenceCustomerID,
	}
	secondaryTarget := types.DeploymentPlanTarget{
		OrganizationID: organizationID, DeploymentTargetID: secondaryTargetID,
		CustomerOrganizationID: &secondaryCustomerID,
	}
	newPlan := func(targets ...types.DeploymentPlanTarget) *types.DeploymentPlan {
		return &types.DeploymentPlan{
			OrganizationID: organizationID,
			ApplicationID:  applicationID,
			ChannelID:      channelID,
			EnvironmentID:  environmentID,
			Targets:        targets,
		}
	}

	referencePlan := newPlan(referenceTarget)
	addDeploymentPlanTargetResolvedVariables(referencePlan, []types.Variable{variable})
	g.Expect(referencePlan.Issues).To(BeEmpty())
	g.Expect(referencePlan.Variables).To(HaveLen(1))
	g.Expect(referencePlan.Variables[0].Value).To(MatchJSON(`"https://reference.example"`))

	missingPlan := newPlan(types.DeploymentPlanTarget{
		OrganizationID: organizationID, DeploymentTargetID: uuid.New(),
	})
	addDeploymentPlanTargetResolvedVariables(missingPlan, []types.Variable{variable})
	g.Expect(missingPlan.Variables).To(HaveLen(1))
	g.Expect(missingPlan.Variables[0].Status).To(Equal(types.VariableResolutionStatusUnresolved))
	g.Expect(missingPlan.Issues).To(ContainElement(MatchFields(IgnoreExtras, Fields{
		"Code": Equal("required_variable_unresolved"),
	})))

	mixedPlan := newPlan(referenceTarget, secondaryTarget)
	addDeploymentPlanTargetResolvedVariables(mixedPlan, []types.Variable{variable})
	g.Expect(mixedPlan.Variables).To(BeEmpty())
	g.Expect(mixedPlan.Issues).To(ContainElement(MatchFields(IgnoreExtras, Fields{
		"Code": Equal("target_scoped_variables_require_separate_plans"),
	})))
}
