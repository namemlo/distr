package api

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/deploymentregistry"
	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

type RegistryImportPreviewRequest struct {
	SourceKind        string                          `json:"sourceKind"`
	ToolName          string                          `json:"toolName"`
	ToolVersion       string                          `json:"toolVersion"`
	SourceCommit      string                          `json:"sourceCommit,omitempty"`
	Parameters        map[string]string               `json:"parameters"`
	EvidenceReference string                          `json:"evidenceReference"`
	EvidenceChecksum  string                          `json:"evidenceChecksum"`
	SourcePlacements  []RegistryImportSourcePlacement `json:"sourcePlacements,omitempty"`
	Roots             []RegistryImportCandidateRoot   `json:"roots"`
}

type RegistryImportSourcePlacement struct {
	RootKey      string `json:"rootKey"`
	PhysicalName string `json:"physicalName"`
}

type RegistryImportCandidatePlacement struct {
	ComponentKey     string `json:"componentKey"`
	PhysicalName     string `json:"physicalName"`
	ConfigNamespace  string `json:"configNamespace,omitempty"`
	DatabaseBoundary string `json:"databaseBoundary,omitempty"`
	HealthAdapter    string `json:"healthAdapter,omitempty"`
	RenamedFrom      string `json:"renamedFrom,omitempty"`
}

type RegistryImportCandidateRoot struct {
	Key                               string                             `json:"key"`
	Name                              string                             `json:"name"`
	DeliveryModel                     types.DeliveryModel                `json:"deliveryModel"`
	Classification                    types.ImportClassification         `json:"classification"`
	CustomerOrganizationID            *uuid.UUID                         `json:"customerOrganizationId,omitempty"`
	DeploymentTargetID                uuid.UUID                          `json:"deploymentTargetId"`
	EnvironmentID                     uuid.UUID                          `json:"environmentId"`
	SubscriberCustomerOrganizationIDs []uuid.UUID                        `json:"subscriberCustomerOrganizationIds,omitempty"`
	PhysicalIdentity                  string                             `json:"physicalIdentity"`
	Placements                        []RegistryImportCandidatePlacement `json:"placements"`
}

type RegistryImportChange struct {
	Kind         string `json:"kind"`
	RootKey      string `json:"rootKey"`
	PlacementKey string `json:"placementKey,omitempty"`
	PhysicalName string `json:"physicalName,omitempty"`
	Message      string `json:"message"`
}

type RegistryImportDiff struct {
	Creates     []RegistryImportChange `json:"creates"`
	Updates     []RegistryImportChange `json:"updates"`
	Retirements []RegistryImportChange `json:"retirements"`
	Conflicts   []RegistryImportChange `json:"conflicts"`
}

type RegistryImportPreview struct {
	ID                   uuid.UUID                     `json:"id"`
	PreviewChecksum      string                        `json:"previewChecksum"`
	Counts               types.RegistryImportCounts    `json:"counts"`
	Diff                 RegistryImportDiff            `json:"diff"`
	Omissions            []string                      `json:"omissions"`
	Diagnostics          []types.ValidationIssue       `json:"diagnostics"`
	DiagnosticsTruncated bool                          `json:"diagnosticsTruncated"`
	Roots                []RegistryImportCandidateRoot `json:"roots"`
}

func (r RegistryImportPreviewRequest) ToDomain(
	organizationID, actorID uuid.UUID,
) types.RegistryImportRequest {
	return deploymentregistry.NormalizeImportRequest(r.toDomain(organizationID, actorID))
}

func (r RegistryImportPreviewRequest) toDomain(
	organizationID, actorID uuid.UUID,
) types.RegistryImportRequest {
	sourcePlacements := make([]types.RegistryImportSourcePlacement, len(r.SourcePlacements))
	for index, placement := range r.SourcePlacements {
		sourcePlacements[index] = types.RegistryImportSourcePlacement{
			RootKey: placement.RootKey, PhysicalName: placement.PhysicalName,
		}
	}
	roots := make([]types.RegistryImportCandidateRoot, len(r.Roots))
	for index, root := range r.Roots {
		placements := make([]types.RegistryImportCandidatePlacement, len(root.Placements))
		for placementIndex, placement := range root.Placements {
			placements[placementIndex] = types.RegistryImportCandidatePlacement{
				ComponentKey: placement.ComponentKey, PhysicalName: placement.PhysicalName,
				ConfigNamespace: placement.ConfigNamespace, DatabaseBoundary: placement.DatabaseBoundary,
				HealthAdapter: placement.HealthAdapter, RenamedFrom: placement.RenamedFrom,
			}
		}
		roots[index] = types.RegistryImportCandidateRoot{
			Key: root.Key, Name: root.Name, DeliveryModel: root.DeliveryModel,
			Classification: root.Classification, CustomerOrganizationID: root.CustomerOrganizationID,
			DeploymentTargetID: root.DeploymentTargetID, EnvironmentID: root.EnvironmentID,
			SubscriberCustomerOrganizationIDs: root.SubscriberCustomerOrganizationIDs,
			PhysicalIdentity:                  root.PhysicalIdentity, Placements: placements,
		}
	}
	return types.RegistryImportRequest{
		OrganizationID: organizationID, SourceKind: r.SourceKind, ToolName: r.ToolName,
		ToolVersion: r.ToolVersion, SourceCommit: r.SourceCommit, Parameters: r.Parameters,
		EvidenceReference: r.EvidenceReference, EvidenceChecksum: r.EvidenceChecksum,
		ActorID: actorID, SourcePlacements: sourcePlacements, Roots: roots,
	}
}

func (r RegistryImportPreviewRequest) Validate() error {
	request := r.toDomain(uuid.New(), uuid.New())
	_, err := deploymentregistry.PreviewImport(context.Background(), request)
	if err != nil {
		return validation.NewValidationFailedError(err.Error())
	}
	return nil
}

type RegistryImportDecisionRequest struct {
	RootKey        string                     `json:"rootKey"`
	Classification types.ImportClassification `json:"classification"`
}

func (r RegistryImportDecisionRequest) Validate() error {
	if strings.TrimSpace(r.RootKey) == "" {
		return validation.NewValidationFailedError("rootKey is required")
	}
	if !r.Classification.IsValid() {
		return validation.NewValidationFailedError("classification is invalid")
	}
	return nil
}

type RegistryImportApplyRequest struct {
	PreviewChecksum string `json:"previewChecksum"`
}

func (r RegistryImportApplyRequest) Validate() error {
	if !deploymentRegistryChecksumPattern.MatchString(r.PreviewChecksum) {
		return validation.NewValidationFailedError("previewChecksum must be a sha256 checksum")
	}
	return nil
}

const (
	deploymentRegistryMaximumPageLimit  = 100
	deploymentRegistryMaximumCursorSize = 2048
)

var (
	deploymentRegistryKeyPattern      = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*$`)
	deploymentRegistryChecksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	deploymentRegistryCursorPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

type DeploymentRegistryListRequest struct {
	Cursor string `query:"cursor"`
	Limit  int    `query:"limit"`
}

func (r DeploymentRegistryListRequest) Validate() error {
	if r.Limit < 0 || r.Limit > deploymentRegistryMaximumPageLimit {
		return validation.NewValidationFailedError("limit must be between 1 and 100 when provided")
	}
	if len(r.Cursor) > deploymentRegistryMaximumCursorSize {
		return validation.NewValidationFailedError("cursor is too large")
	}
	if r.Cursor != "" && !deploymentRegistryCursorPattern.MatchString(r.Cursor) {
		return validation.NewValidationFailedError("cursor must be an opaque URL-safe token")
	}
	return nil
}

type CreateDeploymentScopeRequest struct {
	CustomerOrganizationID *uuid.UUID                    `json:"customerOrganizationId,omitempty"`
	Key                    string                        `json:"key"`
	Name                   string                        `json:"name"`
	Description            string                        `json:"description"`
	DeliveryModel          types.DeliveryModel           `json:"deliveryModel"`
	ManagementState        types.RegistryManagementState `json:"managementState"`
	RetiredAt              *time.Time                    `json:"retiredAt,omitempty"`
}

func (r CreateDeploymentScopeRequest) Validate() error {
	if err := validateRegistryKey(r.Key); err != nil {
		return err
	}
	if strings.TrimSpace(r.Name) == "" {
		return validation.NewValidationFailedError("name is required")
	}
	if !r.DeliveryModel.IsValid() {
		return validation.NewValidationFailedError("deliveryModel is invalid")
	}
	if r.DeliveryModel == types.DeliveryModelDedicated && r.CustomerOrganizationID == nil {
		return validation.NewValidationFailedError("customerOrganizationId is required for dedicated scopes")
	}
	if r.DeliveryModel != types.DeliveryModelDedicated && r.CustomerOrganizationID != nil {
		return validation.NewValidationFailedError(
			"customerOrganizationId is allowed only for dedicated scopes",
		)
	}
	return validateRegistryRetirement(r.ManagementState, r.RetiredAt)
}

type UpdateDeploymentScopeRequest struct {
	Name            string                        `json:"name"`
	Description     string                        `json:"description"`
	ManagementState types.RegistryManagementState `json:"managementState"`
	RetiredAt       *time.Time                    `json:"retiredAt,omitempty"`
}

func (r UpdateDeploymentScopeRequest) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return validation.NewValidationFailedError("name is required")
	}
	return validateRegistryRetirement(r.ManagementState, r.RetiredAt)
}

type CreateTargetEnvironmentAssignmentRequest struct {
	DeploymentTargetID uuid.UUID       `json:"deploymentTargetId"`
	EnvironmentID      uuid.UUID       `json:"environmentId"`
	ActiveFrom         time.Time       `json:"activeFrom"`
	ActiveUntil        *time.Time      `json:"activeUntil,omitempty"`
	PolicyConstraints  json.RawMessage `json:"policyConstraints"`
}

func (r CreateTargetEnvironmentAssignmentRequest) Validate() error {
	if r.DeploymentTargetID == uuid.Nil {
		return validation.NewValidationFailedError("deploymentTargetId is required")
	}
	if r.EnvironmentID == uuid.Nil {
		return validation.NewValidationFailedError("environmentId is required")
	}
	if r.ActiveFrom.IsZero() {
		return validation.NewValidationFailedError("activeFrom is required")
	}
	if r.ActiveUntil != nil && !r.ActiveUntil.After(r.ActiveFrom) {
		return validation.NewValidationFailedError("activeUntil must be after activeFrom")
	}
	return validateRegistryJSON("policyConstraints", r.PolicyConstraints)
}

type UpdateTargetEnvironmentAssignmentRequest struct {
	ActiveUntil       *time.Time      `json:"activeUntil,omitempty"`
	PolicyConstraints json.RawMessage `json:"policyConstraints"`
}

func (r UpdateTargetEnvironmentAssignmentRequest) Validate() error {
	return validateRegistryJSON("policyConstraints", r.PolicyConstraints)
}

type CreateDeploymentUnitRequest struct {
	DeploymentScopeID                 uuid.UUID                     `json:"deploymentScopeId"`
	TargetEnvironmentAssignmentID     uuid.UUID                     `json:"targetEnvironmentAssignmentId"`
	DeploymentTargetID                uuid.UUID                     `json:"deploymentTargetId"`
	Key                               string                        `json:"key"`
	Name                              string                        `json:"name"`
	PhysicalIdentity                  string                        `json:"physicalIdentity"`
	ManagementState                   types.RegistryManagementState `json:"managementState"`
	SubscriberSetChecksum             string                        `json:"subscriberSetChecksum"`
	SubscriberCustomerOrganizationIDs []uuid.UUID                   `json:"subscriberCustomerOrganizationIds,omitempty"`
	RetiredAt                         *time.Time                    `json:"retiredAt,omitempty"`
}

func (r CreateDeploymentUnitRequest) Validate() error {
	if r.DeploymentScopeID == uuid.Nil {
		return validation.NewValidationFailedError("deploymentScopeId is required")
	}
	if r.TargetEnvironmentAssignmentID == uuid.Nil {
		return validation.NewValidationFailedError("targetEnvironmentAssignmentId is required")
	}
	if r.DeploymentTargetID == uuid.Nil {
		return validation.NewValidationFailedError("deploymentTargetId is required")
	}
	if err := validateRegistryKey(r.Key); err != nil {
		return err
	}
	if strings.TrimSpace(r.Name) == "" {
		return validation.NewValidationFailedError("name is required")
	}
	if strings.TrimSpace(r.PhysicalIdentity) == "" {
		return validation.NewValidationFailedError("physicalIdentity is required")
	}
	if !deploymentRegistryChecksumPattern.MatchString(r.SubscriberSetChecksum) {
		return validation.NewValidationFailedError("subscriberSetChecksum must be a sha256 checksum")
	}
	seenSubscriberIDs := make(map[uuid.UUID]struct{}, len(r.SubscriberCustomerOrganizationIDs))
	for _, customerOrganizationID := range r.SubscriberCustomerOrganizationIDs {
		if customerOrganizationID == uuid.Nil {
			return validation.NewValidationFailedError(
				"subscriberCustomerOrganizationIds must not contain empty IDs",
			)
		}
		if _, exists := seenSubscriberIDs[customerOrganizationID]; exists {
			return validation.NewValidationFailedError(
				"subscriberCustomerOrganizationIds must be unique",
			)
		}
		seenSubscriberIDs[customerOrganizationID] = struct{}{}
	}
	return validateRegistryRetirement(r.ManagementState, r.RetiredAt)
}

type UpdateDeploymentUnitRequest struct {
	Name            string                        `json:"name"`
	ManagementState types.RegistryManagementState `json:"managementState"`
	RetiredAt       *time.Time                    `json:"retiredAt,omitempty"`
}

func (r UpdateDeploymentUnitRequest) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return validation.NewValidationFailedError("name is required")
	}
	return validateRegistryRetirement(r.ManagementState, r.RetiredAt)
}

type CreateDeploymentUnitSubscriberRequest struct {
	DeploymentUnitID       uuid.UUID  `json:"deploymentUnitId"`
	CustomerOrganizationID uuid.UUID  `json:"customerOrganizationId"`
	RetiredAt              *time.Time `json:"retiredAt,omitempty"`
}

func (r CreateDeploymentUnitSubscriberRequest) Validate() error {
	if r.DeploymentUnitID == uuid.Nil {
		return validation.NewValidationFailedError("deploymentUnitId is required")
	}
	if r.CustomerOrganizationID == uuid.Nil {
		return validation.NewValidationFailedError("customerOrganizationId is required")
	}
	return nil
}

type UpdateDeploymentUnitSubscriberRequest struct {
	RetiredAt *time.Time `json:"retiredAt,omitempty"`
}

func (UpdateDeploymentUnitSubscriberRequest) Validate() error {
	return nil
}

type CreateComponentDefinitionRequest struct {
	Key             string                        `json:"key"`
	Name            string                        `json:"name"`
	Description     string                        `json:"description"`
	CapabilityScope string                        `json:"capabilityScope"`
	ManagementState types.RegistryManagementState `json:"managementState"`
	RetiredAt       *time.Time                    `json:"retiredAt,omitempty"`
}

func (r CreateComponentDefinitionRequest) Validate() error {
	if err := validateRegistryKey(r.Key); err != nil {
		return err
	}
	if strings.TrimSpace(r.Name) == "" {
		return validation.NewValidationFailedError("name is required")
	}
	return validateRegistryRetirement(r.ManagementState, r.RetiredAt)
}

type UpdateComponentDefinitionRequest struct {
	Name            string                        `json:"name"`
	Description     string                        `json:"description"`
	CapabilityScope string                        `json:"capabilityScope"`
	ManagementState types.RegistryManagementState `json:"managementState"`
	RetiredAt       *time.Time                    `json:"retiredAt,omitempty"`
}

func (r UpdateComponentDefinitionRequest) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return validation.NewValidationFailedError("name is required")
	}
	return validateRegistryRetirement(r.ManagementState, r.RetiredAt)
}

type CreateComponentAliasRequest struct {
	ComponentDefinitionID uuid.UUID  `json:"componentDefinitionId"`
	Alias                 string     `json:"alias"`
	RetiredAt             *time.Time `json:"retiredAt,omitempty"`
}

func (r CreateComponentAliasRequest) Validate() error {
	if r.ComponentDefinitionID == uuid.Nil {
		return validation.NewValidationFailedError("componentDefinitionId is required")
	}
	if strings.TrimSpace(r.Alias) == "" {
		return validation.NewValidationFailedError("alias is required")
	}
	return nil
}

type UpdateComponentAliasRequest struct {
	RetiredAt *time.Time `json:"retiredAt,omitempty"`
}

func (UpdateComponentAliasRequest) Validate() error {
	return nil
}

type CreateComponentInstanceRequest struct {
	DeploymentUnitID      uuid.UUID                     `json:"deploymentUnitId"`
	ComponentDefinitionID uuid.UUID                     `json:"componentDefinitionId"`
	PhysicalName          string                        `json:"physicalName"`
	ConfigNamespace       string                        `json:"configNamespace"`
	DatabaseBoundary      string                        `json:"databaseBoundary"`
	HealthAdapter         string                        `json:"healthAdapter"`
	ManagementState       types.RegistryManagementState `json:"managementState"`
	RenamedFrom           string                        `json:"renamedFrom,omitempty"`
	RetiredAt             *time.Time                    `json:"retiredAt,omitempty"`
}

func (r CreateComponentInstanceRequest) Validate() error {
	if r.DeploymentUnitID == uuid.Nil {
		return validation.NewValidationFailedError("deploymentUnitId is required")
	}
	if r.ComponentDefinitionID == uuid.Nil {
		return validation.NewValidationFailedError("componentDefinitionId is required")
	}
	if strings.TrimSpace(r.PhysicalName) == "" {
		return validation.NewValidationFailedError("physicalName is required")
	}
	return validateRegistryRetirement(r.ManagementState, r.RetiredAt)
}

type UpdateComponentInstanceRequest struct {
	PhysicalName     string                        `json:"physicalName"`
	ConfigNamespace  string                        `json:"configNamespace"`
	DatabaseBoundary string                        `json:"databaseBoundary"`
	HealthAdapter    string                        `json:"healthAdapter"`
	ManagementState  types.RegistryManagementState `json:"managementState"`
	RenamedFrom      string                        `json:"renamedFrom,omitempty"`
	RetiredAt        *time.Time                    `json:"retiredAt,omitempty"`
}

func (r UpdateComponentInstanceRequest) Validate() error {
	if strings.TrimSpace(r.PhysicalName) == "" {
		return validation.NewValidationFailedError("physicalName is required")
	}
	return validateRegistryRetirement(r.ManagementState, r.RetiredAt)
}

type DeploymentScope struct {
	ID                     uuid.UUID                     `json:"id"`
	CreatedAt              time.Time                     `json:"createdAt"`
	UpdatedAt              time.Time                     `json:"updatedAt"`
	CustomerOrganizationID *uuid.UUID                    `json:"customerOrganizationId,omitempty"`
	Key                    string                        `json:"key"`
	Name                   string                        `json:"name"`
	Description            string                        `json:"description"`
	DeliveryModel          types.DeliveryModel           `json:"deliveryModel"`
	ManagementState        types.RegistryManagementState `json:"managementState"`
	RetiredAt              *time.Time                    `json:"retiredAt,omitempty"`
}

type TargetEnvironmentAssignment struct {
	ID                 uuid.UUID       `json:"id"`
	CreatedAt          time.Time       `json:"createdAt"`
	UpdatedAt          time.Time       `json:"updatedAt"`
	DeploymentTargetID uuid.UUID       `json:"deploymentTargetId"`
	EnvironmentID      uuid.UUID       `json:"environmentId"`
	ActiveFrom         time.Time       `json:"activeFrom"`
	ActiveUntil        *time.Time      `json:"activeUntil,omitempty"`
	PolicyConstraints  json.RawMessage `json:"policyConstraints"`
}

type DeploymentUnit struct {
	ID                            uuid.UUID                     `json:"id"`
	CreatedAt                     time.Time                     `json:"createdAt"`
	UpdatedAt                     time.Time                     `json:"updatedAt"`
	DeploymentScopeID             uuid.UUID                     `json:"deploymentScopeId"`
	TargetEnvironmentAssignmentID uuid.UUID                     `json:"targetEnvironmentAssignmentId"`
	DeploymentTargetID            uuid.UUID                     `json:"deploymentTargetId"`
	Key                           string                        `json:"key"`
	Name                          string                        `json:"name"`
	PhysicalIdentity              string                        `json:"physicalIdentity"`
	ManagementState               types.RegistryManagementState `json:"managementState"`
	SubscriberSetChecksum         string                        `json:"subscriberSetChecksum"`
	RetiredAt                     *time.Time                    `json:"retiredAt,omitempty"`
}

type DeploymentUnitSubscriber struct {
	ID                     uuid.UUID  `json:"id"`
	CreatedAt              time.Time  `json:"createdAt"`
	DeploymentUnitID       uuid.UUID  `json:"deploymentUnitId"`
	CustomerOrganizationID uuid.UUID  `json:"customerOrganizationId"`
	RetiredAt              *time.Time `json:"retiredAt,omitempty"`
}

type ComponentDefinition struct {
	ID              uuid.UUID                     `json:"id"`
	CreatedAt       time.Time                     `json:"createdAt"`
	UpdatedAt       time.Time                     `json:"updatedAt"`
	Key             string                        `json:"key"`
	Name            string                        `json:"name"`
	Description     string                        `json:"description"`
	CapabilityScope string                        `json:"capabilityScope"`
	ManagementState types.RegistryManagementState `json:"managementState"`
	RetiredAt       *time.Time                    `json:"retiredAt,omitempty"`
}

type ComponentAlias struct {
	ID                    uuid.UUID  `json:"id"`
	CreatedAt             time.Time  `json:"createdAt"`
	ComponentDefinitionID uuid.UUID  `json:"componentDefinitionId"`
	Alias                 string     `json:"alias"`
	RetiredAt             *time.Time `json:"retiredAt,omitempty"`
}

type ComponentInstance struct {
	ID                    uuid.UUID                     `json:"id"`
	CreatedAt             time.Time                     `json:"createdAt"`
	UpdatedAt             time.Time                     `json:"updatedAt"`
	DeploymentUnitID      uuid.UUID                     `json:"deploymentUnitId"`
	ComponentDefinitionID uuid.UUID                     `json:"componentDefinitionId"`
	PhysicalName          string                        `json:"physicalName"`
	ConfigNamespace       string                        `json:"configNamespace"`
	DatabaseBoundary      string                        `json:"databaseBoundary"`
	HealthAdapter         string                        `json:"healthAdapter"`
	ManagementState       types.RegistryManagementState `json:"managementState"`
	RetiredAt             *time.Time                    `json:"retiredAt,omitempty"`
}

type DeploymentRegistryPlacement struct {
	Scope       DeploymentScope             `json:"scope"`
	Assignment  TargetEnvironmentAssignment `json:"assignment"`
	Unit        DeploymentUnit              `json:"unit"`
	Subscribers []DeploymentUnitSubscriber  `json:"subscribers"`
	Definitions []ComponentDefinition       `json:"definitions"`
	Aliases     []ComponentAlias            `json:"aliases"`
	Instances   []ComponentInstance         `json:"instances"`
}

type DeploymentScopePage struct {
	Items      []DeploymentScope `json:"items"`
	NextCursor string            `json:"nextCursor,omitempty"`
}

type TargetEnvironmentAssignmentPage struct {
	Items      []TargetEnvironmentAssignment `json:"items"`
	NextCursor string                        `json:"nextCursor,omitempty"`
}

type DeploymentUnitPage struct {
	Items      []DeploymentUnit `json:"items"`
	NextCursor string           `json:"nextCursor,omitempty"`
}

type DeploymentUnitSubscriberPage struct {
	Items      []DeploymentUnitSubscriber `json:"items"`
	NextCursor string                     `json:"nextCursor,omitempty"`
}

type ComponentDefinitionPage struct {
	Items      []ComponentDefinition `json:"items"`
	NextCursor string                `json:"nextCursor,omitempty"`
}

type ComponentAliasPage struct {
	Items      []ComponentAlias `json:"items"`
	NextCursor string           `json:"nextCursor,omitempty"`
}

type ComponentInstancePage struct {
	Items      []ComponentInstance `json:"items"`
	NextCursor string              `json:"nextCursor,omitempty"`
}

type DeploymentRegistryPlacementPage struct {
	Items      []DeploymentRegistryPlacement `json:"items"`
	NextCursor string                        `json:"nextCursor,omitempty"`
}

func validateRegistryKey(value string) error {
	if !deploymentRegistryKeyPattern.MatchString(strings.TrimSpace(value)) {
		return validation.NewValidationFailedError(
			"key must use lowercase letters, digits, dots, underscores, or hyphens",
		)
	}
	return nil
}

func validateRegistryRetirement(
	state types.RegistryManagementState,
	retiredAt *time.Time,
) error {
	if !state.IsValid() {
		return validation.NewValidationFailedError("managementState is invalid")
	}
	if (state == types.RegistryManagementStateRetired) != (retiredAt != nil) {
		return validation.NewValidationFailedError(
			"retiredAt must be set exactly when managementState is retired",
		)
	}
	return nil
}

func validateRegistryJSON(field string, value json.RawMessage) error {
	if len(value) == 0 {
		return nil
	}
	if !json.Valid(value) {
		return validation.NewValidationFailedError(field + " must be valid JSON")
	}
	return nil
}
