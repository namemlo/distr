package targetconfig

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/distr-sh/distr/internal/releasebundles"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

const V1ExtractorVersion = "release-contract-v1/1"

type V1ExtractionBlockedReasonCode string

const (
	V1ExtractionBlockedReasonReleaseChecksumMismatch    V1ExtractionBlockedReasonCode = "release_checksum_mismatch"
	V1ExtractionBlockedReasonPlanChecksumMismatch       V1ExtractionBlockedReasonCode = "plan_checksum_mismatch"
	V1ExtractionBlockedReasonHistoryContractMismatch    V1ExtractionBlockedReasonCode = "history_contract_mismatch"
	V1ExtractionBlockedReasonUnsupportedSchema          V1ExtractionBlockedReasonCode = "unsupported_schema"
	V1ExtractionBlockedReasonMultiComponent             V1ExtractionBlockedReasonCode = "multi_component_release"
	V1ExtractionBlockedReasonTargetCardinality          V1ExtractionBlockedReasonCode = "multi_target_plan"
	V1ExtractionBlockedReasonTargetComponentCardinality V1ExtractionBlockedReasonCode = "target_component_cardinality"
	V1ExtractionBlockedReasonPlacementInvalid           V1ExtractionBlockedReasonCode = "placement_invalid"
	V1ExtractionBlockedReasonComponentMissing           V1ExtractionBlockedReasonCode = "component_instance_missing"
	V1ExtractionBlockedReasonComponentAmbiguous         V1ExtractionBlockedReasonCode = "component_instance_ambiguous"
	V1ExtractionBlockedReasonComponentMismatch          V1ExtractionBlockedReasonCode = "component_history_mismatch"
	V1ExtractionBlockedReasonConfigObjectMissing        V1ExtractionBlockedReasonCode = "config_object_missing"
	V1ExtractionBlockedReasonConfigObjectAmbiguous      V1ExtractionBlockedReasonCode = "config_object_ambiguous"
	V1ExtractionBlockedReasonMutableConfigObject        V1ExtractionBlockedReasonCode = "mutable_config_object"
	V1ExtractionBlockedReasonPlaintextSecret            V1ExtractionBlockedReasonCode = "plaintext_secret"
	V1ExtractionBlockedReasonSecretReferenceUnresolved  V1ExtractionBlockedReasonCode = "secret_reference_unresolved"
	V1ExtractionBlockedReasonSecretReferenceUnsafe      V1ExtractionBlockedReasonCode = "secret_reference_unsafe"
	V1ExtractionBlockedReasonDerivedSnapshotInvalid     V1ExtractionBlockedReasonCode = "derived_snapshot_invalid"
)

type V1ExtractionInput struct {
	OrganizationID  uuid.UUID
	ReleaseBundleID uuid.UUID

	ReleaseChecksum         string
	ReleaseCanonicalPayload []byte

	PlanID               uuid.UUID
	PlanChecksum         string
	PlanCanonicalPayload []byte

	ReleaseContract      *types.ReleaseContract
	PlanTargets          []types.DeploymentPlanTarget
	PlanTargetComponents []types.DeploymentPlanTargetComponent
	PlanVariables        []types.DeploymentPlanVariable
	ComponentInstances   []types.ComponentInstance

	DeploymentUnitID              uuid.UUID
	TargetEnvironmentAssignmentID uuid.UUID
	EnvironmentID                 uuid.UUID
}

type V1ExtractionResult struct {
	Draft             *types.TargetConfigSnapshotDraft
	CanonicalPayload  []byte
	CanonicalChecksum string
	BlockedReasonCode V1ExtractionBlockedReasonCode
}

func ExtractV1TargetConfig(input V1ExtractionInput) (V1ExtractionResult, error) {
	if !matchesV1HistoryChecksum(input.ReleaseCanonicalPayload, input.ReleaseChecksum) {
		return blockedV1Extraction(V1ExtractionBlockedReasonReleaseChecksumMismatch), nil
	}
	if !matchesV1HistoryChecksum(input.PlanCanonicalPayload, input.PlanChecksum) {
		return blockedV1Extraction(V1ExtractionBlockedReasonPlanChecksumMismatch), nil
	}
	contract, planEnvelope, ok := verifiedV1HistoryContract(input)
	if !ok {
		return blockedV1Extraction(V1ExtractionBlockedReasonHistoryContractMismatch), nil
	}
	if contract.Schema != types.ReleaseContractSchemaV1 {
		return blockedV1Extraction(V1ExtractionBlockedReasonUnsupportedSchema), nil
	}
	if len(contract.Components) != 1 {
		return blockedV1Extraction(V1ExtractionBlockedReasonMultiComponent), nil
	}
	if len(input.PlanTargets) != 1 {
		return blockedV1Extraction(V1ExtractionBlockedReasonTargetCardinality), nil
	}
	if len(input.PlanTargetComponents) != 1 {
		return blockedV1Extraction(V1ExtractionBlockedReasonTargetComponentCardinality), nil
	}
	if !sameV1PlanHistory(planEnvelope, input) {
		return blockedV1Extraction(V1ExtractionBlockedReasonHistoryContractMismatch), nil
	}
	if !validV1PlacementIdentity(input, planEnvelope) {
		return blockedV1Extraction(V1ExtractionBlockedReasonPlacementInvalid), nil
	}

	target := input.PlanTargets[0]
	targetComponent := input.PlanTargetComponents[0]
	releaseComponent := contract.Components[0]
	if !matchingV1ComponentHistory(
		target,
		targetComponent,
		releaseComponent,
		contract.Config.ServiceConfigChecksum,
	) {
		return blockedV1Extraction(V1ExtractionBlockedReasonComponentMismatch), nil
	}
	instance, reason := matchV1ComponentInstance(input, targetComponent)
	if reason != "" {
		return blockedV1Extraction(reason), nil
	}

	composeObject, reason := matchV1ConfigObject(
		contract.Config,
		contract.Config.ComposePath,
		contract.Config.ComposeChecksum,
	)
	if reason != "" {
		return blockedV1Extraction(reason), nil
	}
	serviceObject, reason := matchV1ConfigObject(
		contract.Config,
		contract.Config.ServiceConfigPath,
		contract.Config.ServiceConfigChecksum,
	)
	if reason != "" {
		return blockedV1Extraction(reason), nil
	}
	if *composeObject == *serviceObject {
		return blockedV1Extraction(V1ExtractionBlockedReasonConfigObjectAmbiguous), nil
	}

	objects := []types.TargetConfigSnapshotObjectDraft{
		v1ConfigObjectDraft(
			"compose",
			types.TargetConfigObjectKindDeploymentDescriptor,
			contract.Config.ComposePath,
			*composeObject,
		),
		v1ConfigObjectDraft(
			"service-config",
			types.TargetConfigObjectKindServiceConfig,
			contract.Config.ServiceConfigPath,
			*serviceObject,
		),
	}
	for _, object := range objects {
		if !isImmutableTargetConfigObject(object) {
			return blockedV1Extraction(V1ExtractionBlockedReasonMutableConfigObject), nil
		}
	}

	secretReferences, reason := extractV1SecretReferences(input.PlanVariables)
	if reason != "" {
		return blockedV1Extraction(reason), nil
	}
	draft := types.TargetConfigSnapshotDraft{
		OrganizationID:                input.OrganizationID,
		DeploymentUnitID:              input.DeploymentUnitID,
		TargetEnvironmentAssignmentID: input.TargetEnvironmentAssignmentID,
		EnvironmentID:                 input.EnvironmentID,
		SourceRepository:              contract.Source.Repository,
		SourceCommit:                  contract.Config.RepositoryCommit,
		SourceAdapter:                 "release-contract-v1",
		AdapterVersion:                V1ExtractorVersion,
		TargetPlatform:                string(target.Platform),
		RuntimeConstraints:            map[string]string{},
		Objects:                       objects,
		Components: []types.TargetConfigSnapshotComponentDraft{{
			PhysicalName:        instance.PhysicalName,
			ComponentInstanceID: instance.ID,
			DeploymentUnitID:    input.DeploymentUnitID,
		}},
		SecretReferences: secretReferences,
		FeatureFlags:     []types.TargetConfigSnapshotFeatureFlagDraft{},
	}
	if len(ValidateDraft(draft)) > 0 {
		return blockedV1Extraction(V1ExtractionBlockedReasonDerivedSnapshotInvalid), nil
	}
	payload, checksum, err := Canonicalize(draft)
	if err != nil {
		return blockedV1Extraction(V1ExtractionBlockedReasonDerivedSnapshotInvalid), nil
	}
	return V1ExtractionResult{
		Draft:             &draft,
		CanonicalPayload:  payload,
		CanonicalChecksum: checksum,
	}, nil
}

type v1HistoryEnvelope struct {
	ReleaseBundleID  string          `json:"releaseBundleId"`
	EnvironmentID    string          `json:"environmentId"`
	ReleaseContract  json.RawMessage `json:"releaseContract"`
	Targets          json.RawMessage `json:"targets"`
	TargetComponents json.RawMessage `json:"targetComponents"`
	Variables        json.RawMessage `json:"variables"`
}

func verifiedV1HistoryContract(
	input V1ExtractionInput,
) (*types.ReleaseContract, v1HistoryEnvelope, bool) {
	if input.ReleaseContract == nil {
		return nil, v1HistoryEnvelope{}, false
	}
	var releaseEnvelope v1HistoryEnvelope
	if err := json.Unmarshal(input.ReleaseCanonicalPayload, &releaseEnvelope); err != nil {
		return nil, v1HistoryEnvelope{}, false
	}
	var planEnvelope v1HistoryEnvelope
	if err := json.Unmarshal(input.PlanCanonicalPayload, &planEnvelope); err != nil {
		return nil, v1HistoryEnvelope{}, false
	}
	contract := releasebundles.NormalizedReleaseContract(input.ReleaseContract)
	if contract == nil ||
		!sameNormalizedV1Contract(releaseEnvelope.ReleaseContract, contract) ||
		!sameNormalizedV1Contract(planEnvelope.ReleaseContract, contract) {
		return nil, v1HistoryEnvelope{}, false
	}
	return contract, planEnvelope, true
}

func sameNormalizedV1Contract(raw json.RawMessage, expected *types.ReleaseContract) bool {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return false
	}
	var actual types.ReleaseContract
	if err := json.Unmarshal(raw, &actual); err != nil {
		return false
	}
	actualNormalized := releasebundles.NormalizedReleaseContract(&actual)
	actualBytes, err := json.Marshal(actualNormalized)
	if err != nil {
		return false
	}
	expectedBytes, err := json.Marshal(expected)
	return err == nil && bytes.Equal(actualBytes, expectedBytes)
}

func matchesV1HistoryChecksum(payload []byte, checksum string) bool {
	if len(payload) == 0 || !targetConfigChecksumPattern.MatchString(checksum) {
		return false
	}
	sum := sha256.Sum256(payload)
	return checksum == "sha256:"+hex.EncodeToString(sum[:])
}

type canonicalV1PlanTarget struct {
	DeploymentTargetID     string `json:"deploymentTargetId"`
	Name                   string `json:"name"`
	Type                   string `json:"type"`
	Platform               string `json:"platform"`
	CustomerOrganizationID string `json:"customerOrganizationId,omitempty"`
	SortOrder              int    `json:"sortOrder"`
}

type canonicalV1PlanTargetComponent struct {
	DeploymentPlanTargetID  string   `json:"deploymentPlanTargetId"`
	DeploymentTargetID      string   `json:"deploymentTargetId"`
	Component               string   `json:"component"`
	Version                 string   `json:"version"`
	Image                   string   `json:"image"`
	Platform                string   `json:"platform"`
	Contracts               []string `json:"contracts"`
	ConfigChecksum          string   `json:"configChecksum"`
	ExpectedStateVersion    int64    `json:"expectedStateVersion"`
	ExpectedStateChecksum   string   `json:"expectedStateChecksum"`
	ExpectedReleaseBundleID string   `json:"expectedReleaseBundleId,omitempty"`
	SortOrder               int      `json:"sortOrder"`
}

type canonicalV1PlanVariable struct {
	VariableSetID string                               `json:"variableSetId"`
	VariableID    string                               `json:"variableId"`
	Key           string                               `json:"key"`
	Type          string                               `json:"type"`
	IsRequired    bool                                 `json:"isRequired"`
	Status        string                               `json:"status"`
	Source        string                               `json:"source"`
	Value         json.RawMessage                      `json:"value,omitempty"`
	ReferenceID   string                               `json:"referenceId,omitempty"`
	ReferenceName string                               `json:"referenceName,omitempty"`
	Redacted      bool                                 `json:"redacted"`
	Trace         []types.VariableResolutionTraceEntry `json:"trace"`
}

func sameV1PlanHistory(envelope v1HistoryEnvelope, input V1ExtractionInput) bool {
	targets := make([]canonicalV1PlanTarget, 0, len(input.PlanTargets))
	for _, target := range input.PlanTargets {
		customerOrganizationID := ""
		if target.CustomerOrganizationID != nil {
			customerOrganizationID = target.CustomerOrganizationID.String()
		}
		targets = append(targets, canonicalV1PlanTarget{
			DeploymentTargetID:     target.DeploymentTargetID.String(),
			Name:                   target.Name,
			Type:                   string(target.Type),
			Platform:               string(target.Platform),
			CustomerOrganizationID: customerOrganizationID,
			SortOrder:              target.SortOrder,
		})
	}
	components := make([]canonicalV1PlanTargetComponent, 0, len(input.PlanTargetComponents))
	for _, component := range input.PlanTargetComponents {
		expectedReleaseBundleID := ""
		if component.ExpectedReleaseBundleID != nil {
			expectedReleaseBundleID = component.ExpectedReleaseBundleID.String()
		}
		components = append(components, canonicalV1PlanTargetComponent{
			DeploymentPlanTargetID:  component.DeploymentPlanTargetID.String(),
			DeploymentTargetID:      component.DeploymentTargetID.String(),
			Component:               component.Component,
			Version:                 component.Version,
			Image:                   component.Image,
			Platform:                string(component.Platform),
			Contracts:               slices.Clone(component.Contracts),
			ConfigChecksum:          component.ConfigChecksum,
			ExpectedStateVersion:    component.ExpectedStateVersion,
			ExpectedStateChecksum:   component.ExpectedStateChecksum,
			ExpectedReleaseBundleID: expectedReleaseBundleID,
			SortOrder:               component.SortOrder,
		})
	}
	variables := make([]canonicalV1PlanVariable, 0, len(input.PlanVariables))
	for _, variable := range input.PlanVariables {
		variables = append(variables, canonicalV1PlanVariable{
			VariableSetID: variable.VariableSetID.String(),
			VariableID:    variable.VariableID.String(),
			Key:           variable.Key,
			Type:          string(variable.Type),
			IsRequired:    variable.IsRequired,
			Status:        string(variable.Status),
			Source:        string(variable.Source),
			Value:         slices.Clone(variable.Value),
			ReferenceID:   variable.ReferenceID,
			ReferenceName: variable.ReferenceName,
			Redacted:      variable.Redacted,
			Trace:         slices.Clone(variable.Trace),
		})
	}
	return sameV1CanonicalField(envelope.Targets, targets) &&
		sameV1CanonicalField(envelope.TargetComponents, components) &&
		sameV1CanonicalField(envelope.Variables, variables)
}

func sameV1CanonicalField(raw json.RawMessage, value any) bool {
	expected, err := json.Marshal(value)
	return err == nil && bytes.Equal(raw, expected)
}

func validV1PlacementIdentity(input V1ExtractionInput, plan v1HistoryEnvelope) bool {
	if input.OrganizationID == uuid.Nil ||
		input.ReleaseBundleID == uuid.Nil ||
		input.PlanID == uuid.Nil ||
		input.DeploymentUnitID == uuid.Nil ||
		input.TargetEnvironmentAssignmentID == uuid.Nil ||
		input.EnvironmentID == uuid.Nil ||
		plan.ReleaseBundleID != input.ReleaseBundleID.String() ||
		plan.EnvironmentID != input.EnvironmentID.String() {
		return false
	}
	target := input.PlanTargets[0]
	component := input.PlanTargetComponents[0]
	return target.ID != uuid.Nil &&
		target.OrganizationID == input.OrganizationID &&
		component.DeploymentPlanID == input.PlanID &&
		component.DeploymentPlanTargetID == target.ID &&
		component.OrganizationID == input.OrganizationID &&
		component.DeploymentTargetID == target.DeploymentTargetID
}

func matchingV1ComponentHistory(
	target types.DeploymentPlanTarget,
	planned types.DeploymentPlanTargetComponent,
	released types.ReleaseContractComponent,
	serviceConfigChecksum string,
) bool {
	return strings.TrimSpace(planned.Component) == strings.TrimSpace(released.Name) &&
		strings.TrimSpace(planned.Version) == strings.TrimSpace(released.Version) &&
		strings.TrimSpace(planned.Image) == strings.TrimSpace(released.Image) &&
		planned.Platform == target.Platform &&
		strings.TrimSpace(string(planned.Platform)) == strings.TrimSpace(released.Platform) &&
		strings.TrimSpace(planned.ConfigChecksum) ==
			strings.TrimSpace(serviceConfigChecksum)
}

func matchV1ComponentInstance(
	input V1ExtractionInput,
	component types.DeploymentPlanTargetComponent,
) (*types.ComponentInstance, V1ExtractionBlockedReasonCode) {
	matches := make([]types.ComponentInstance, 0, 1)
	for _, instance := range input.ComponentInstances {
		if instance.OrganizationID == input.OrganizationID &&
			instance.DeploymentUnitID == input.DeploymentUnitID &&
			instance.RetiredAt == nil &&
			strings.TrimSpace(instance.PhysicalName) == strings.TrimSpace(component.Component) {
			matches = append(matches, instance)
		}
	}
	switch len(matches) {
	case 0:
		return nil, V1ExtractionBlockedReasonComponentMissing
	case 1:
		return &matches[0], ""
	default:
		return nil, V1ExtractionBlockedReasonComponentAmbiguous
	}
}

func matchV1ConfigObject(
	config types.ReleaseContractConfig,
	configPath string,
	checksum string,
) (*types.ReleaseContractConfigObject, V1ExtractionBlockedReasonCode) {
	matches := make([]types.ReleaseContractConfigObject, 0, 1)
	for _, object := range config.ImmutableObjects {
		if object.Checksum == checksum && v1ObjectPathMatches(object.URI, configPath) {
			matches = append(matches, object)
		}
	}
	switch len(matches) {
	case 0:
		return nil, V1ExtractionBlockedReasonConfigObjectMissing
	case 1:
		return &matches[0], ""
	default:
		return nil, V1ExtractionBlockedReasonConfigObjectAmbiguous
	}
}

func v1ObjectPathMatches(reference, configPath string) bool {
	parsed, err := url.Parse(reference)
	if err != nil || parsed.Scheme != "s3" || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		parsed.Opaque != "" || strings.Contains(parsed.Path, "\\") ||
		path.Clean(parsed.Path) != parsed.Path {
		return false
	}
	objectPath := strings.TrimPrefix(parsed.Path, "/")
	configPath = strings.TrimPrefix(configPath, "/")
	return objectPath == configPath || strings.HasSuffix(objectPath, "/"+configPath)
}

func v1ConfigObjectDraft(
	key string,
	kind types.TargetConfigObjectKind,
	configPath string,
	object types.ReleaseContractConfigObject,
) types.TargetConfigSnapshotObjectDraft {
	return types.TargetConfigSnapshotObjectDraft{
		Key:       key,
		Kind:      kind,
		Reference: object.URI,
		VersionID: object.VersionID,
		MediaType: v1ConfigMediaType(configPath),
		Checksum:  object.Checksum,
	}
}

func v1ConfigMediaType(configPath string) string {
	lower := strings.ToLower(configPath)
	switch {
	case strings.HasSuffix(lower, ".json"):
		return "application/json"
	case strings.HasSuffix(lower, ".yaml"), strings.HasSuffix(lower, ".yml"):
		return "application/yaml"
	case strings.HasSuffix(lower, ".toml"):
		return "application/toml"
	case strings.HasSuffix(lower, ".xml"):
		return "application/xml"
	default:
		return "application/octet-stream"
	}
}

func extractV1SecretReferences(
	variables []types.DeploymentPlanVariable,
) ([]types.TargetConfigSnapshotSecretReferenceDraft, V1ExtractionBlockedReasonCode) {
	references := make([]types.TargetConfigSnapshotSecretReferenceDraft, 0)
	for _, variable := range variables {
		if variable.Type != types.VariableTypeSecretReference {
			if v1PlaintextLooksSecret(variable) {
				return nil, V1ExtractionBlockedReasonPlaintextSecret
			}
			continue
		}
		if v1RawValuePresent(variable.Value) {
			return nil, V1ExtractionBlockedReasonPlaintextSecret
		}
		if variable.Status != types.VariableResolutionStatusResolved {
			return nil, V1ExtractionBlockedReasonSecretReferenceUnresolved
		}
		referenceID := strings.TrimSpace(variable.ReferenceID)
		parsedReferenceID, err := uuid.Parse(referenceID)
		if err != nil {
			return nil, V1ExtractionBlockedReasonSecretReferenceUnsafe
		}
		key := strings.ToLower(strings.TrimSpace(variable.Key))
		if !targetConfigKeyPattern.MatchString(key) || len(key) > 128 {
			return nil, V1ExtractionBlockedReasonSecretReferenceUnsafe
		}
		canonicalReferenceID := parsedReferenceID.String()
		references = append(references, types.TargetConfigSnapshotSecretReferenceDraft{
			Key:                key,
			Provider:           "distr",
			Reference:          canonicalReferenceID,
			VersionFingerprint: fingerprintV1SecretReference(canonicalReferenceID),
		})
	}
	slices.SortFunc(references, func(a, b types.TargetConfigSnapshotSecretReferenceDraft) int {
		return strings.Compare(a.Key, b.Key)
	})
	return references, ""
}

func v1PlaintextLooksSecret(variable types.DeploymentPlanVariable) bool {
	if !v1RawValuePresent(variable.Value) {
		return false
	}
	raw := strings.ToLower(string(variable.Value))
	return secretLookingPattern.MatchString(variable.Key) ||
		secretLookingPattern.MatchString(raw) ||
		inlineSecretPattern.MatchString(raw) ||
		strings.Contains(raw, "bearer ") ||
		strings.Contains(raw, "-----begin private key-----")
}

func v1RawValuePresent(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

func fingerprintV1SecretReference(referenceID string) string {
	sum := sha256.Sum256([]byte("distr.secret-reference/v1\x00" + referenceID))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func blockedV1Extraction(reason V1ExtractionBlockedReasonCode) V1ExtractionResult {
	return V1ExtractionResult{BlockedReasonCode: reason}
}
