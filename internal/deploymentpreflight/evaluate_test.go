package deploymentpreflight

import (
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestEvaluatePassesMatchingFrozenTargetStateAndDependency(t *testing.T) {
	g := NewWithT(t)
	input := preflightInputFixture()

	checks := Evaluate(input)

	g.Expect(failedCheckKeys(checks)).To(BeEmpty())
	g.Expect(checkKeys(checks)).To(ContainElements(
		"plan_checksum", "target_binding", "release_eligibility", "release_contract", "release_operations",
		"target_platform:loyalty-api", "target_state:loyalty-api",
		"dependency_version:mc-api", "dependency_contract:mc-api",
	))
}

func TestEvaluateRejectsStateCreatedAfterPlanAndPlatformDrift(t *testing.T) {
	g := NewWithT(t)
	input := preflightInputFixture()
	input.Plan.Targets[0].Platform = types.DeploymentTargetPlatformLinuxAMD64
	input.CurrentTargets[input.Plan.Targets[0].DeploymentTargetID] = types.DeploymentTarget{
		ID: input.Plan.Targets[0].DeploymentTargetID, Platform: types.DeploymentTargetPlatformLinuxARM64,
	}
	input.Plan.TargetComponents[0].ExpectedStateVersion = 0
	input.Plan.TargetComponents[0].ExpectedStateChecksum = ""
	input.CurrentStates[StateKey{
		DeploymentTargetID: input.Plan.Targets[0].DeploymentTargetID,
		ApplicationID:      input.Plan.ApplicationID,
		Component:          "loyalty-api",
	}] = targetState("loyalty-api", "1.2.2", "loyalty-api/v1")

	checks := Evaluate(input)

	g.Expect(failedCheckKeys(checks)).To(ContainElements(
		"target_platform:loyalty-api", "target_state:loyalty-api",
	))
}

func TestEvaluateRejectsMissingDependencyVersionAndContract(t *testing.T) {
	g := NewWithT(t)
	input := preflightInputFixture()
	dependencyKey := StateKey{
		DeploymentTargetID: input.Plan.Targets[0].DeploymentTargetID,
		ApplicationID:      input.Plan.ApplicationID,
		Component:          "mc-api",
	}
	input.CurrentStates[dependencyKey] = targetState("mc-api", "0.0.4", "mc-api.http@4")

	checks := Evaluate(input)

	g.Expect(failedCheckKeys(checks)).To(ContainElements(
		"dependency_version:mc-api", "dependency_contract:mc-api",
	))
}

func TestEvaluateRejectsPlanChecksumMismatch(t *testing.T) {
	g := NewWithT(t)
	input := preflightInputFixture()
	input.PlanPayloadChecksumValid = false

	checks := Evaluate(input)

	g.Expect(failedCheckKeys(checks)).To(ContainElement("plan_checksum"))
}

func TestEvaluateRejectsTargetOwnershipAndTypeDrift(t *testing.T) {
	g := NewWithT(t)
	input := preflightInputFixture()
	otherCustomerID := uuid.New()
	input.CurrentTargets[input.Plan.Targets[0].DeploymentTargetID] = types.DeploymentTarget{
		ID:                     input.Plan.Targets[0].DeploymentTargetID,
		Type:                   types.DeploymentTypeKubernetes,
		Platform:               types.DeploymentTargetPlatformLinuxAMD64,
		CustomerOrganizationID: &otherCustomerID,
	}

	checks := Evaluate(input)

	g.Expect(failedCheckKeys(checks)).To(ContainElement("target_binding"))
}

func TestEvaluateRejectsCurrentEligibilityAndReleaseContractFailures(t *testing.T) {
	g := NewWithT(t)
	input := preflightInputFixture()
	input.ReleaseEligible = false
	input.ReleaseEligibilityMessage = "release is no longer eligible for the environment"
	input.ReleaseContractValid = false
	input.ReleaseContractMessage = "release contract no longer matches the published components"

	checks := Evaluate(input)

	g.Expect(failedCheckKeys(checks)).To(ContainElements("release_eligibility", "release_contract"))
}

func preflightInputFixture() Input {
	orgID := uuid.New()
	targetID := uuid.New()
	applicationID := uuid.New()
	planTargetID := uuid.New()
	customerID := uuid.New()
	checksum := "sha256:" + strings.Repeat("a", 64)
	stateChecksum := "sha256:" + strings.Repeat("b", 64)
	plan := types.DeploymentPlan{
		ID: uuid.New(), OrganizationID: orgID, ApplicationID: applicationID,
		CanonicalChecksum: checksum,
		ReleaseContract: &types.ReleaseContract{
			Compatibility: types.ReleaseContractCompatibility{Requires: []types.ReleaseContractRequirement{{
				Component: "mc-api", MinimumVersion: "0.0.5", Contract: "mc-api.http@5",
			}}},
		},
		Targets: []types.DeploymentPlanTarget{{
			ID: planTargetID, DeploymentTargetID: targetID, Name: "choice-tp-dev",
			Type: types.DeploymentTypeDocker, Platform: types.DeploymentTargetPlatformLinuxAMD64,
			CustomerOrganizationID: &customerID,
		}},
		TargetComponents: []types.DeploymentPlanTargetComponent{{
			ID: uuid.New(), DeploymentPlanTargetID: planTargetID, DeploymentTargetID: targetID,
			Component: "loyalty-api", Version: "1.2.3", Platform: types.DeploymentTargetPlatformLinuxAMD64,
			ExpectedStateVersion: 4, ExpectedStateChecksum: stateChecksum,
		}},
	}
	return Input{
		Plan:                     plan,
		PlanPayloadChecksumValid: true,
		ReleaseEligible:          true,
		ReleaseContractValid:     true,
		CurrentTargets: map[uuid.UUID]types.DeploymentTarget{
			targetID: {
				ID: targetID, Type: types.DeploymentTypeDocker, Platform: types.DeploymentTargetPlatformLinuxAMD64,
				CustomerOrganizationID: &customerID,
			},
		},
		CurrentStates: map[StateKey]types.TargetComponentState{
			{DeploymentTargetID: targetID, ApplicationID: applicationID, Component: "loyalty-api"}: {
				DeploymentTargetID: targetID, ApplicationID: applicationID, Component: "loyalty-api",
				StateVersion: 4, StateChecksum: stateChecksum, Version: "1.2.2",
			},
			{DeploymentTargetID: targetID, ApplicationID: applicationID, Component: "mc-api"}: targetState("mc-api", "0.0.5", "mc-api.http@5"),
		},
	}
}

func targetState(component, version string, contracts ...string) types.TargetComponentState {
	return types.TargetComponentState{Component: component, Version: version, Contracts: contracts, StateVersion: 1}
}

func failedCheckKeys(checks []types.DeploymentPreflightCheck) []string {
	result := []string{}
	for _, check := range checks {
		if check.Status == types.DeploymentPreflightCheckStatusFailed {
			result = append(result, check.CheckKey)
		}
	}
	return result
}

func checkKeys(checks []types.DeploymentPreflightCheck) []string {
	result := make([]string, 0, len(checks))
	for _, check := range checks {
		result = append(result, check.CheckKey)
	}
	return result
}
