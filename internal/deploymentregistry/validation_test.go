package deploymentregistry

import (
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestValidateDeploymentRegistryPlacementAcceptsDedicatedTopology(t *testing.T) {
	g := NewWithT(t)
	placement := validDedicatedPlacement()

	g.Expect(ValidateDeploymentRegistryPlacement(placement)).To(BeEmpty())
}

func TestValidateDeploymentRegistryPlacementAcceptsSharedTopologyWithStableSubscriberChecksum(t *testing.T) {
	g := NewWithT(t)
	placement := validDedicatedPlacement()
	placement.Scope.DeliveryModel = types.DeliveryModelShared
	placement.Scope.CustomerOrganizationID = nil
	placement.Subscribers = []types.DeploymentUnitSubscriber{
		{
			ID:                     uuid.New(),
			OrganizationID:         placement.Scope.OrganizationID,
			DeploymentUnitID:       placement.Unit.ID,
			CustomerOrganizationID: uuid.New(),
		},
		{
			ID:                     uuid.New(),
			OrganizationID:         placement.Scope.OrganizationID,
			DeploymentUnitID:       placement.Unit.ID,
			CustomerOrganizationID: uuid.New(),
		},
	}
	placement.Unit.SubscriberSetChecksum = SubscriberSetChecksum(placement.Subscribers)

	reversed := []types.DeploymentUnitSubscriber{
		placement.Subscribers[1],
		placement.Subscribers[0],
	}
	g.Expect(SubscriberSetChecksum(reversed)).To(Equal(placement.Unit.SubscriberSetChecksum))
	g.Expect(placement.Unit.SubscriberSetChecksum).To(HavePrefix("sha256:"))
	g.Expect(placement.Unit.SubscriberSetChecksum).To(HaveLen(len("sha256:") + 64))
	g.Expect(ValidateDeploymentRegistryPlacement(placement)).To(BeEmpty())
}

func TestValidateDeploymentRegistryPlacementRejectsAmbiguousActiveEnvironment(t *testing.T) {
	g := NewWithT(t)
	placement := validDedicatedPlacement()
	second := placement.Assignment
	second.ID = uuid.New()
	second.EnvironmentID = uuid.New()
	second.ActiveFrom = placement.EffectiveAt.Add(-time.Hour)
	placement.Assignments = append(placement.Assignments, second)

	g.Expect(issueCodes(ValidateDeploymentRegistryPlacement(placement))).To(ContainElement(
		"registry.assignment.ambiguous_active",
	))
}

func TestValidateDeploymentRegistryPlacementRejectsDuplicatePhysicalIdentity(t *testing.T) {
	g := NewWithT(t)
	placement := validDedicatedPlacement()
	duplicate := placement.Unit
	duplicate.ID = uuid.New()
	placement.Units = append(placement.Units, duplicate)

	g.Expect(issueCodes(ValidateDeploymentRegistryPlacement(placement))).To(ContainElement(
		"registry.unit.duplicate_physical_identity",
	))
}

func TestValidateDeploymentRegistryPlacementRequiresSharedSubscriber(t *testing.T) {
	g := NewWithT(t)
	placement := validDedicatedPlacement()
	placement.Scope.DeliveryModel = types.DeliveryModelShared
	placement.Scope.CustomerOrganizationID = nil
	placement.Subscribers = nil
	placement.Unit.SubscriberSetChecksum = SubscriberSetChecksum(nil)

	g.Expect(issueCodes(ValidateDeploymentRegistryPlacement(placement))).To(ContainElement(
		"registry.shared.subscriber_required",
	))
}

func TestValidateDeploymentRegistryPlacementRejectsOrphanInstance(t *testing.T) {
	g := NewWithT(t)
	placement := validDedicatedPlacement()
	placement.Instances[0].DeploymentUnitID = uuid.New()
	placement.Instances[0].ComponentDefinitionID = uuid.New()

	g.Expect(issueCodes(ValidateDeploymentRegistryPlacement(placement))).To(ContainElements(
		"registry.instance.unit_not_found",
		"registry.instance.definition_not_found",
	))
}

func TestValidateDeploymentRegistryPlacementRequiresAliasForRename(t *testing.T) {
	g := NewWithT(t)
	placement := validDedicatedPlacement()
	placement.Instances[0].RenamedFrom = "legacy-api"

	g.Expect(issueCodes(ValidateDeploymentRegistryPlacement(placement))).To(ContainElement(
		"registry.instance.rename_alias_required",
	))

	placement.Aliases = []types.ComponentAlias{{
		ID:                    uuid.New(),
		OrganizationID:        placement.Scope.OrganizationID,
		ComponentDefinitionID: placement.Instances[0].ComponentDefinitionID,
		Alias:                 "legacy-api",
	}}
	g.Expect(ValidateDeploymentRegistryPlacement(placement)).To(BeEmpty())
}

func TestValidateDeploymentRegistryPlacementRejectsCrossOrganizationSubstitution(t *testing.T) {
	g := NewWithT(t)
	placement := validDedicatedPlacement()
	placement.Assignment.OrganizationID = uuid.New()
	placement.Assignments[0] = placement.Assignment

	g.Expect(issueCodes(ValidateDeploymentRegistryPlacement(placement))).To(ContainElement(
		"registry.organization.mismatch",
	))
}

func TestValidateDeploymentRegistryPlacementReturnsStableOrderedIssueCodes(t *testing.T) {
	g := NewWithT(t)
	placement := validDedicatedPlacement()
	placement.Scope.DeliveryModel = types.DeliveryModelShared
	placement.Scope.CustomerOrganizationID = nil
	placement.Scope.ManagementState = "invalid"
	placement.Assignment.OrganizationID = uuid.New()
	placement.Assignments[0] = placement.Assignment
	placement.Subscribers = nil
	placement.Unit.SubscriberSetChecksum = "sha256:" + strings.Repeat("0", 64)
	placement.Instances[0].DeploymentUnitID = uuid.New()
	placement.Instances[0].ComponentDefinitionID = uuid.New()
	placement.Instances[0].RenamedFrom = "legacy-api"

	g.Expect(issueCodes(ValidateDeploymentRegistryPlacement(placement))).To(Equal([]string{
		"registry.organization.mismatch",
		"registry.management_state.invalid",
		"registry.shared.subscriber_required",
		"registry.shared.subscriber_checksum_mismatch",
		"registry.instance.unit_not_found",
		"registry.instance.definition_not_found",
		"registry.instance.rename_alias_required",
	}))
}

func TestDeploymentRegistryEnumValuesRemainExact(t *testing.T) {
	g := NewWithT(t)

	g.Expect(types.AllDeliveryModels()).To(Equal([]types.DeliveryModel{
		types.DeliveryModelDedicated,
		types.DeliveryModelShared,
		types.DeliveryModelExternal,
	}))
	g.Expect(types.AllRegistryManagementStates()).To(Equal([]types.RegistryManagementState{
		types.RegistryManagementStateManaged,
		types.RegistryManagementStateObserveOnly,
		types.RegistryManagementStateExternal,
		types.RegistryManagementStateLegacyCutover,
		types.RegistryManagementStateBackup,
		types.RegistryManagementStateRetired,
		types.RegistryManagementStateUnclassified,
	}))
}

func validDedicatedPlacement() types.DeploymentRegistryPlacement {
	now := time.Date(2026, time.July, 18, 8, 0, 0, 0, time.UTC)
	organizationID := uuid.New()
	customerOrganizationID := uuid.New()
	scopeID := uuid.New()
	targetID := uuid.New()
	assignmentID := uuid.New()
	unitID := uuid.New()
	definitionID := uuid.New()
	assignment := types.TargetEnvironmentAssignment{
		ID:                 assignmentID,
		OrganizationID:     organizationID,
		DeploymentTargetID: targetID,
		EnvironmentID:      uuid.New(),
		ActiveFrom:         now.Add(-time.Hour),
	}
	unit := types.DeploymentUnit{
		ID:                            unitID,
		OrganizationID:                organizationID,
		DeploymentScopeID:             scopeID,
		TargetEnvironmentAssignmentID: assignmentID,
		DeploymentTargetID:            targetID,
		Key:                           "primary-runtime",
		PhysicalIdentity:              "compose:primary-runtime",
		ManagementState:               types.RegistryManagementStateManaged,
		SubscriberSetChecksum:         SubscriberSetChecksum(nil),
	}
	return types.DeploymentRegistryPlacement{
		EffectiveAt: now,
		Scope: types.DeploymentScope{
			ID:                     scopeID,
			OrganizationID:         organizationID,
			CustomerOrganizationID: &customerOrganizationID,
			Key:                    "customer-primary",
			Name:                   "Customer primary",
			DeliveryModel:          types.DeliveryModelDedicated,
			ManagementState:        types.RegistryManagementStateManaged,
		},
		Assignment:  assignment,
		Assignments: []types.TargetEnvironmentAssignment{assignment},
		Unit:        unit,
		Units:       []types.DeploymentUnit{unit},
		Definitions: []types.ComponentDefinition{{
			ID:              definitionID,
			OrganizationID:  organizationID,
			Key:             "example-api",
			Name:            "Example API",
			ManagementState: types.RegistryManagementStateManaged,
		}},
		Instances: []types.ComponentInstance{{
			ID:                    uuid.New(),
			OrganizationID:        organizationID,
			DeploymentUnitID:      unitID,
			ComponentDefinitionID: definitionID,
			PhysicalName:          "example-api",
			ManagementState:       types.RegistryManagementStateManaged,
		}},
	}
}

func issueCodes(issues []types.ValidationIssue) []string {
	result := make([]string, len(issues))
	for index, issue := range issues {
		result[index] = issue.Code
	}
	return result
}
