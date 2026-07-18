package mapping

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
)

func RegistryImportPreviewToAPI(value types.RegistryImportPreview) api.RegistryImportPreview {
	return api.RegistryImportPreview{
		ID: value.ID, PreviewChecksum: value.PreviewChecksum, Counts: value.Counts,
		Diff: api.RegistryImportDiff{
			Creates:     List(value.Diff.Creates, registryImportChangeToAPI),
			Updates:     List(value.Diff.Updates, registryImportChangeToAPI),
			Retirements: List(value.Diff.Retirements, registryImportChangeToAPI),
			Conflicts:   List(value.Diff.Conflicts, registryImportChangeToAPI),
		},
		Omissions:   value.Omissions,
		Diagnostics: value.Diagnostics, DiagnosticsTruncated: value.DiagnosticsTruncated,
		Roots: List(value.Roots, registryImportCandidateRootToAPI),
	}
}

func registryImportChangeToAPI(value types.RegistryImportChange) api.RegistryImportChange {
	return api.RegistryImportChange{
		Kind: value.Kind, RootKey: value.RootKey, PlacementKey: value.PlacementKey,
		PhysicalName: value.PhysicalName, Message: value.Message,
	}
}

func registryImportCandidateRootToAPI(
	value types.RegistryImportCandidateRoot,
) api.RegistryImportCandidateRoot {
	return api.RegistryImportCandidateRoot{
		Key: value.Key, Name: value.Name, DeliveryModel: value.DeliveryModel,
		Classification: value.Classification, CustomerOrganizationID: value.CustomerOrganizationID,
		DeploymentTargetID: value.DeploymentTargetID, EnvironmentID: value.EnvironmentID,
		SubscriberCustomerOrganizationIDs: value.SubscriberCustomerOrganizationIDs,
		PhysicalIdentity:                  value.PhysicalIdentity,
		Placements:                        List(value.Placements, registryImportCandidatePlacementToAPI),
	}
}

func registryImportCandidatePlacementToAPI(
	value types.RegistryImportCandidatePlacement,
) api.RegistryImportCandidatePlacement {
	return api.RegistryImportCandidatePlacement{
		ComponentKey: value.ComponentKey, PhysicalName: value.PhysicalName,
		ConfigNamespace: value.ConfigNamespace, DatabaseBoundary: value.DatabaseBoundary,
		HealthAdapter: value.HealthAdapter, RenamedFrom: value.RenamedFrom,
	}
}

func RegistryImportResultToAPI(value types.RegistryImportResult) types.RegistryImportResult {
	return value
}

func RegistryCoverageReportToAPI(value types.RegistryCoverageReport) types.RegistryCoverageReport {
	return value
}

func DeploymentScopeToAPI(scope types.DeploymentScope) api.DeploymentScope {
	return api.DeploymentScope{
		ID:                     scope.ID,
		CreatedAt:              scope.CreatedAt,
		UpdatedAt:              scope.UpdatedAt,
		CustomerOrganizationID: scope.CustomerOrganizationID,
		Key:                    scope.Key,
		Name:                   scope.Name,
		Description:            scope.Description,
		DeliveryModel:          scope.DeliveryModel,
		ManagementState:        scope.ManagementState,
		RetiredAt:              scope.RetiredAt,
	}
}

func TargetEnvironmentAssignmentToAPI(
	assignment types.TargetEnvironmentAssignment,
) api.TargetEnvironmentAssignment {
	return api.TargetEnvironmentAssignment{
		ID:                 assignment.ID,
		CreatedAt:          assignment.CreatedAt,
		UpdatedAt:          assignment.UpdatedAt,
		DeploymentTargetID: assignment.DeploymentTargetID,
		EnvironmentID:      assignment.EnvironmentID,
		ActiveFrom:         assignment.ActiveFrom,
		ActiveUntil:        assignment.ActiveUntil,
		PolicyConstraints:  assignment.PolicyConstraints,
	}
}

func DeploymentUnitToAPI(unit types.DeploymentUnit) api.DeploymentUnit {
	return api.DeploymentUnit{
		ID:                            unit.ID,
		CreatedAt:                     unit.CreatedAt,
		UpdatedAt:                     unit.UpdatedAt,
		DeploymentScopeID:             unit.DeploymentScopeID,
		TargetEnvironmentAssignmentID: unit.TargetEnvironmentAssignmentID,
		DeploymentTargetID:            unit.DeploymentTargetID,
		Key:                           unit.Key,
		Name:                          unit.Name,
		PhysicalIdentity:              unit.PhysicalIdentity,
		ManagementState:               unit.ManagementState,
		SubscriberSetChecksum:         unit.SubscriberSetChecksum,
		RetiredAt:                     unit.RetiredAt,
	}
}

func DeploymentUnitSubscriberToAPI(
	subscriber types.DeploymentUnitSubscriber,
) api.DeploymentUnitSubscriber {
	return api.DeploymentUnitSubscriber{
		ID:                     subscriber.ID,
		CreatedAt:              subscriber.CreatedAt,
		DeploymentUnitID:       subscriber.DeploymentUnitID,
		CustomerOrganizationID: subscriber.CustomerOrganizationID,
		RetiredAt:              subscriber.RetiredAt,
	}
}

func ComponentDefinitionToAPI(definition types.ComponentDefinition) api.ComponentDefinition {
	return api.ComponentDefinition{
		ID:              definition.ID,
		CreatedAt:       definition.CreatedAt,
		UpdatedAt:       definition.UpdatedAt,
		Key:             definition.Key,
		Name:            definition.Name,
		Description:     definition.Description,
		CapabilityScope: definition.CapabilityScope,
		ManagementState: definition.ManagementState,
		RetiredAt:       definition.RetiredAt,
	}
}

func ComponentAliasToAPI(alias types.ComponentAlias) api.ComponentAlias {
	return api.ComponentAlias{
		ID:                    alias.ID,
		CreatedAt:             alias.CreatedAt,
		ComponentDefinitionID: alias.ComponentDefinitionID,
		Alias:                 alias.Alias,
		RetiredAt:             alias.RetiredAt,
	}
}

func ComponentInstanceToAPI(instance types.ComponentInstance) api.ComponentInstance {
	return api.ComponentInstance{
		ID:                    instance.ID,
		CreatedAt:             instance.CreatedAt,
		UpdatedAt:             instance.UpdatedAt,
		DeploymentUnitID:      instance.DeploymentUnitID,
		ComponentDefinitionID: instance.ComponentDefinitionID,
		PhysicalName:          instance.PhysicalName,
		ConfigNamespace:       instance.ConfigNamespace,
		DatabaseBoundary:      instance.DatabaseBoundary,
		HealthAdapter:         instance.HealthAdapter,
		ManagementState:       instance.ManagementState,
		RetiredAt:             instance.RetiredAt,
	}
}

func DeploymentRegistryPlacementToAPI(
	placement types.DeploymentRegistryPlacement,
) api.DeploymentRegistryPlacement {
	return api.DeploymentRegistryPlacement{
		Scope:       DeploymentScopeToAPI(placement.Scope),
		Assignment:  TargetEnvironmentAssignmentToAPI(placement.Assignment),
		Unit:        DeploymentUnitToAPI(placement.Unit),
		Subscribers: List(placement.Subscribers, DeploymentUnitSubscriberToAPI),
		Definitions: List(placement.Definitions, ComponentDefinitionToAPI),
		Aliases:     List(placement.Aliases, ComponentAliasToAPI),
		Instances:   List(placement.Instances, ComponentInstanceToAPI),
	}
}

func DeploymentScopePageToAPI(page types.Page[types.DeploymentScope]) api.DeploymentScopePage {
	return api.DeploymentScopePage{
		Items:      List(page.Items, DeploymentScopeToAPI),
		NextCursor: page.NextCursor,
	}
}

func TargetEnvironmentAssignmentPageToAPI(
	page types.Page[types.TargetEnvironmentAssignment],
) api.TargetEnvironmentAssignmentPage {
	return api.TargetEnvironmentAssignmentPage{
		Items:      List(page.Items, TargetEnvironmentAssignmentToAPI),
		NextCursor: page.NextCursor,
	}
}

func DeploymentUnitPageToAPI(page types.Page[types.DeploymentUnit]) api.DeploymentUnitPage {
	return api.DeploymentUnitPage{
		Items:      List(page.Items, DeploymentUnitToAPI),
		NextCursor: page.NextCursor,
	}
}

func DeploymentUnitSubscriberPageToAPI(
	page types.Page[types.DeploymentUnitSubscriber],
) api.DeploymentUnitSubscriberPage {
	return api.DeploymentUnitSubscriberPage{
		Items:      List(page.Items, DeploymentUnitSubscriberToAPI),
		NextCursor: page.NextCursor,
	}
}

func ComponentDefinitionPageToAPI(
	page types.Page[types.ComponentDefinition],
) api.ComponentDefinitionPage {
	return api.ComponentDefinitionPage{
		Items:      List(page.Items, ComponentDefinitionToAPI),
		NextCursor: page.NextCursor,
	}
}

func ComponentAliasPageToAPI(page types.Page[types.ComponentAlias]) api.ComponentAliasPage {
	return api.ComponentAliasPage{
		Items:      List(page.Items, ComponentAliasToAPI),
		NextCursor: page.NextCursor,
	}
}

func ComponentInstancePageToAPI(
	page types.Page[types.ComponentInstance],
) api.ComponentInstancePage {
	return api.ComponentInstancePage{
		Items:      List(page.Items, ComponentInstanceToAPI),
		NextCursor: page.NextCursor,
	}
}

func DeploymentRegistryPlacementPageToAPI(
	page types.Page[types.DeploymentRegistryPlacement],
) api.DeploymentRegistryPlacementPage {
	return api.DeploymentRegistryPlacementPage{
		Items:      List(page.Items, DeploymentRegistryPlacementToAPI),
		NextCursor: page.NextCursor,
	}
}
