package deploymentregistry

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"sort"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

var validationIssuePriority = map[string]int{
	"registry.organization.mismatch":                  10,
	"registry.delivery_model.invalid":                 20,
	"registry.management_state.invalid":               30,
	"registry.scope.customer_required":                40,
	"registry.scope.customer_forbidden":               50,
	"registry.assignment.interval_invalid":            60,
	"registry.assignment.inactive":                    70,
	"registry.assignment.ambiguous_active":            80,
	"registry.unit.assignment_mismatch":               90,
	"registry.unit.duplicate_physical_identity":       100,
	"registry.shared.subscriber_required":             110,
	"registry.shared.subscriber_checksum_mismatch":    120,
	"registry.non_shared.subscriber_forbidden":        130,
	"registry.instance.unit_not_found":                140,
	"registry.instance.definition_not_found":          150,
	"registry.instance.rename_alias_required":         160,
	"registry.component_definition.key_invalid":       170,
	"registry.component_instance.physical_name_empty": 180,
}

func SubscriberSetChecksum(subscribers []types.DeploymentUnitSubscriber) string {
	customerIDs := make([]uuid.UUID, 0, len(subscribers))
	for _, subscriber := range subscribers {
		if subscriber.RetiredAt == nil {
			customerIDs = append(customerIDs, subscriber.CustomerOrganizationID)
		}
	}
	sort.Slice(customerIDs, func(i, j int) bool {
		return bytes.Compare(customerIDs[i][:], customerIDs[j][:]) < 0
	})

	hash := sha256.New()
	_, _ = hash.Write([]byte("distr.deployment-unit-subscriber-set/v1"))
	for _, id := range customerIDs {
		customerID := id.String()
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(customerID)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(customerID))
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func ValidateDeploymentRegistryPlacement(
	placement types.DeploymentRegistryPlacement,
) []types.ValidationIssue {
	issues := make([]types.ValidationIssue, 0)
	organizationID := placement.Scope.OrganizationID
	if hasOrganizationMismatch(placement, organizationID) {
		issues = append(issues, issue(
			"registry.organization.mismatch",
			"organizationId",
			"all registry identities must belong to the same organization",
		))
	}
	issues = append(issues, validateScopeTopology(placement.Scope)...)
	if invalidManagementState(placement) {
		issues = append(issues, issue(
			"registry.management_state.invalid",
			"managementState",
			"management state is invalid",
		))
	}

	effectiveAt := placement.EffectiveAt
	if effectiveAt.IsZero() {
		effectiveAt = time.Now().UTC()
	}
	issues = append(issues, validateAssignmentTopology(placement, effectiveAt)...)
	issues = append(issues, validateUnitTopology(placement)...)
	issues = append(issues, validateSubscriberTopology(placement)...)
	issues = append(issues, validateComponentTopology(placement)...)

	sort.SliceStable(issues, func(i, j int) bool {
		leftPriority := validationIssuePriority[issues[i].Code]
		rightPriority := validationIssuePriority[issues[j].Code]
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		if issues[i].Code != issues[j].Code {
			return issues[i].Code < issues[j].Code
		}
		return issues[i].Field < issues[j].Field
	})
	return issues
}

func validateScopeTopology(scope types.DeploymentScope) []types.ValidationIssue {
	issues := make([]types.ValidationIssue, 0, 2)
	if !scope.DeliveryModel.IsValid() {
		issues = append(issues, issue(
			"registry.delivery_model.invalid",
			"scope.deliveryModel",
			"delivery model must be dedicated, shared, or external",
		))
	}
	switch scope.DeliveryModel {
	case types.DeliveryModelDedicated:
		if scope.CustomerOrganizationID == nil {
			issues = append(issues, issue(
				"registry.scope.customer_required",
				"scope.customerOrganizationId",
				"a dedicated deployment scope requires a customer organization",
			))
		}
	case types.DeliveryModelShared, types.DeliveryModelExternal:
		if scope.CustomerOrganizationID != nil {
			issues = append(issues, issue(
				"registry.scope.customer_forbidden",
				"scope.customerOrganizationId",
				"shared and external deployment scopes are organization owned",
			))
		}
	}
	return issues
}

func validateAssignmentTopology(
	placement types.DeploymentRegistryPlacement,
	effectiveAt time.Time,
) []types.ValidationIssue {
	issues := make([]types.ValidationIssue, 0, 3)
	assignments := placement.Assignments
	if len(assignments) == 0 {
		assignments = []types.TargetEnvironmentAssignment{placement.Assignment}
	}
	activeAssignments := 0
	for _, assignment := range assignments {
		if assignment.ActiveFrom.IsZero() ||
			(assignment.ActiveUntil != nil && !assignment.ActiveUntil.After(assignment.ActiveFrom)) {
			issues = append(issues, issue(
				"registry.assignment.interval_invalid",
				"assignments.activeUntil",
				"an active environment interval must have a start before its optional end",
			))
			continue
		}
		if assignment.DeploymentTargetID == placement.Unit.DeploymentTargetID &&
			assignmentIsActive(assignment, effectiveAt) {
			activeAssignments++
		}
	}
	if !assignmentIsActive(placement.Assignment, effectiveAt) {
		issues = append(issues, issue(
			"registry.assignment.inactive",
			"assignment",
			"the selected target environment assignment is not active",
		))
	}
	if activeAssignments > 1 {
		issues = append(issues, issue(
			"registry.assignment.ambiguous_active",
			"assignments",
			"the target has more than one active environment assignment",
		))
	}
	return issues
}

func validateUnitTopology(
	placement types.DeploymentRegistryPlacement,
) []types.ValidationIssue {
	issues := make([]types.ValidationIssue, 0, 2)
	if placement.Unit.TargetEnvironmentAssignmentID != placement.Assignment.ID ||
		placement.Unit.DeploymentTargetID != placement.Assignment.DeploymentTargetID ||
		placement.Unit.DeploymentScopeID != placement.Scope.ID {
		issues = append(issues, issue(
			"registry.unit.assignment_mismatch",
			"unit",
			"deployment unit identity does not match its scope and assignment",
		))
	}
	units := placement.Units
	if len(units) == 0 {
		units = []types.DeploymentUnit{placement.Unit}
	}
	if duplicatePhysicalIdentityCount(units, placement.Unit) > 1 {
		issues = append(issues, issue(
			"registry.unit.duplicate_physical_identity",
			"units.physicalIdentity",
			"only one active deployment unit may use a physical target and scope identity",
		))
	}
	return issues
}

func validateSubscriberTopology(
	placement types.DeploymentRegistryPlacement,
) []types.ValidationIssue {
	issues := make([]types.ValidationIssue, 0, 2)
	activeSubscribers := activeSubscribersForUnit(placement.Subscribers, placement.Unit.ID)
	if placement.Scope.DeliveryModel == types.DeliveryModelShared {
		if len(activeSubscribers) == 0 {
			issues = append(issues, issue(
				"registry.shared.subscriber_required",
				"subscribers",
				"a shared deployment unit requires at least one subscriber",
			))
		}
		if placement.Unit.SubscriberSetChecksum != SubscriberSetChecksum(activeSubscribers) {
			issues = append(issues, issue(
				"registry.shared.subscriber_checksum_mismatch",
				"unit.subscriberSetChecksum",
				"subscriber set checksum does not match the active subscriber set",
			))
		}
	} else if len(activeSubscribers) > 0 {
		issues = append(issues, issue(
			"registry.non_shared.subscriber_forbidden",
			"subscribers",
			"only shared deployment units may have subscribers",
		))
	}
	return issues
}

func validateComponentTopology(
	placement types.DeploymentRegistryPlacement,
) []types.ValidationIssue {
	issues := make([]types.ValidationIssue, 0)
	definitions := make(map[uuid.UUID]types.ComponentDefinition, len(placement.Definitions))
	for _, definition := range placement.Definitions {
		definitions[definition.ID] = definition
		if !isCanonicalComponentKey(definition.Key) {
			issues = append(issues, issue(
				"registry.component_definition.key_invalid",
				"definitions."+definition.ID.String()+".key",
				"component key must be lowercase canonical text",
			))
		}
	}
	aliases := make(map[string]uuid.UUID, len(placement.Aliases))
	for _, alias := range placement.Aliases {
		if alias.RetiredAt == nil {
			aliases[strings.ToLower(strings.TrimSpace(alias.Alias))] = alias.ComponentDefinitionID
		}
	}
	instances := append([]types.ComponentInstance(nil), placement.Instances...)
	sort.Slice(instances, func(i, j int) bool {
		return instances[i].ID.String() < instances[j].ID.String()
	})
	for _, instance := range instances {
		prefix := "instances." + instance.ID.String()
		if instance.DeploymentUnitID != placement.Unit.ID {
			issues = append(issues, issue(
				"registry.instance.unit_not_found",
				prefix+".deploymentUnitId",
				"component instance does not belong to the selected deployment unit",
			))
		}
		if _, exists := definitions[instance.ComponentDefinitionID]; !exists {
			issues = append(issues, issue(
				"registry.instance.definition_not_found",
				prefix+".componentDefinitionId",
				"component instance references an unknown component definition",
			))
		}
		if strings.TrimSpace(instance.PhysicalName) == "" {
			issues = append(issues, issue(
				"registry.component_instance.physical_name_empty",
				prefix+".physicalName",
				"component instance physical name is required",
			))
		}
		if renamedFrom := strings.ToLower(strings.TrimSpace(instance.RenamedFrom)); renamedFrom != "" &&
			aliases[renamedFrom] != instance.ComponentDefinitionID {
			issues = append(issues, issue(
				"registry.instance.rename_alias_required",
				prefix+".renamedFrom",
				"a renamed component requires an active alias or explicit retirement and new instance",
			))
		}
	}
	return issues
}

func issue(code, field, message string) types.ValidationIssue {
	return types.ValidationIssue{Code: code, Field: field, Message: message}
}

func hasOrganizationMismatch(
	placement types.DeploymentRegistryPlacement,
	organizationID uuid.UUID,
) bool {
	if organizationID == uuid.Nil ||
		placement.Assignment.OrganizationID != organizationID ||
		placement.Unit.OrganizationID != organizationID {
		return true
	}
	for _, assignment := range placement.Assignments {
		if assignment.OrganizationID != organizationID {
			return true
		}
	}
	for _, unit := range placement.Units {
		if unit.OrganizationID != organizationID {
			return true
		}
	}
	for _, subscriber := range placement.Subscribers {
		if subscriber.OrganizationID != organizationID {
			return true
		}
	}
	for _, definition := range placement.Definitions {
		if definition.OrganizationID != organizationID {
			return true
		}
	}
	for _, alias := range placement.Aliases {
		if alias.OrganizationID != organizationID {
			return true
		}
	}
	for _, instance := range placement.Instances {
		if instance.OrganizationID != organizationID {
			return true
		}
	}
	return false
}

func invalidManagementState(placement types.DeploymentRegistryPlacement) bool {
	if !placement.Scope.ManagementState.IsValid() ||
		!placement.Unit.ManagementState.IsValid() {
		return true
	}
	for _, unit := range placement.Units {
		if !unit.ManagementState.IsValid() {
			return true
		}
	}
	for _, definition := range placement.Definitions {
		if !definition.ManagementState.IsValid() {
			return true
		}
	}
	for _, instance := range placement.Instances {
		if !instance.ManagementState.IsValid() {
			return true
		}
	}
	return false
}

func assignmentIsActive(assignment types.TargetEnvironmentAssignment, at time.Time) bool {
	if assignment.ActiveFrom.IsZero() || at.Before(assignment.ActiveFrom) {
		return false
	}
	return assignment.ActiveUntil == nil || at.Before(*assignment.ActiveUntil)
}

func duplicatePhysicalIdentityCount(units []types.DeploymentUnit, selected types.DeploymentUnit) int {
	count := 0
	for _, unit := range units {
		if unit.RetiredAt == nil &&
			unit.OrganizationID == selected.OrganizationID &&
			unit.DeploymentScopeID == selected.DeploymentScopeID &&
			unit.DeploymentTargetID == selected.DeploymentTargetID &&
			strings.EqualFold(
				strings.TrimSpace(unit.PhysicalIdentity),
				strings.TrimSpace(selected.PhysicalIdentity),
			) {
			count++
		}
	}
	return count
}

func activeSubscribersForUnit(
	subscribers []types.DeploymentUnitSubscriber,
	unitID uuid.UUID,
) []types.DeploymentUnitSubscriber {
	result := make([]types.DeploymentUnitSubscriber, 0, len(subscribers))
	for _, subscriber := range subscribers {
		if subscriber.DeploymentUnitID == unitID && subscriber.RetiredAt == nil {
			result = append(result, subscriber)
		}
	}
	return result
}

func isCanonicalComponentKey(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" || key != strings.ToLower(key) {
		return false
	}
	separator := false
	for index, character := range key {
		isAlphaNumeric := character >= 'a' && character <= 'z' ||
			character >= '0' && character <= '9'
		if isAlphaNumeric {
			separator = false
			continue
		}
		if (character != '-' && character != '_' && character != '.') ||
			index == 0 || separator {
			return false
		}
		separator = true
	}
	return !separator
}
