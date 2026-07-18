package targetconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestExtractV1TargetConfigDerivesDeterministicSnapshotWithoutMutatingHistory(t *testing.T) {
	g := NewWithT(t)
	input := validV1ExtractionInput(t)
	before, err := json.Marshal(input)
	g.Expect(err).NotTo(HaveOccurred())

	result, err := ExtractV1TargetConfig(input)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.BlockedReasonCode).To(BeEmpty())
	g.Expect(result.Draft).NotTo(BeNil())
	g.Expect(*result.Draft).To(Equal(types.TargetConfigSnapshotDraft{
		OrganizationID:                input.OrganizationID,
		DeploymentUnitID:              input.DeploymentUnitID,
		TargetEnvironmentAssignmentID: input.TargetEnvironmentAssignmentID,
		EnvironmentID:                 input.EnvironmentID,
		SourceRepository:              "https://git.example.invalid/acme/config.git",
		SourceCommit:                  strings.Repeat("3", 40),
		SourceAdapter:                 "release-contract-v1",
		AdapterVersion:                V1ExtractorVersion,
		TargetPlatform:                "linux/amd64",
		RuntimeConstraints:            map[string]string{},
		Objects: []types.TargetConfigSnapshotObjectDraft{
			{
				Key:       "compose",
				Kind:      types.TargetConfigObjectKindDeploymentDescriptor,
				Reference: "s3://config-bucket/_immutable/sha256/" + strings.Repeat("a", 64) + "/config/docker-compose.yaml",
				MediaType: "application/yaml",
				Checksum:  "sha256:" + strings.Repeat("a", 64),
			},
			{
				Key:       "service-config",
				Kind:      types.TargetConfigObjectKindServiceConfig,
				Reference: "s3://config-bucket/config/service.json",
				VersionID: "version-7",
				MediaType: "application/json",
				Checksum:  "sha256:" + strings.Repeat("b", 64),
			},
		},
		Components: []types.TargetConfigSnapshotComponentDraft{{
			PhysicalName:        "api",
			ComponentInstanceID: input.ComponentInstances[0].ID,
			DeploymentUnitID:    input.DeploymentUnitID,
		}},
		SecretReferences: []types.TargetConfigSnapshotSecretReferenceDraft{{
			Key:                "api_token",
			Provider:           "distr",
			Reference:          input.PlanVariables[0].ReferenceID,
			VersionFingerprint: fingerprintV1SecretReference(input.PlanVariables[0].ReferenceID),
		}},
		FeatureFlags: []types.TargetConfigSnapshotFeatureFlagDraft{},
	}))
	expectedPayload, expectedChecksum, err := Canonicalize(*result.Draft)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.CanonicalPayload).To(Equal(expectedPayload))
	g.Expect(result.CanonicalChecksum).To(Equal(expectedChecksum))

	after, err := json.Marshal(input)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(after).To(Equal(before))

	reordered := validV1ExtractionInput(t)
	reordered.ReleaseContract.Config.ImmutableObjects[0], reordered.ReleaseContract.Config.ImmutableObjects[1] =
		reordered.ReleaseContract.Config.ImmutableObjects[1], reordered.ReleaseContract.Config.ImmutableObjects[0]
	reordered.PlanVariables[0], reordered.PlanVariables[1] = reordered.PlanVariables[1], reordered.PlanVariables[0]
	setV1HistoryPayloads(t, &reordered)
	reorderedResult, err := ExtractV1TargetConfig(reordered)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(reorderedResult.BlockedReasonCode).To(BeEmpty())
	g.Expect(reorderedResult.CanonicalPayload).To(Equal(result.CanonicalPayload))
	g.Expect(reorderedResult.CanonicalChecksum).To(Equal(result.CanonicalChecksum))
}

func TestExtractV1TargetConfigBlocksChangedHistoricalBytesOrChecksums(t *testing.T) {
	tests := []struct {
		name   string
		change func(*V1ExtractionInput)
		reason V1ExtractionBlockedReasonCode
	}{
		{
			name: "release checksum",
			change: func(input *V1ExtractionInput) {
				input.ReleaseChecksum = "sha256:" + strings.Repeat("f", 64)
			},
			reason: V1ExtractionBlockedReasonReleaseChecksumMismatch,
		},
		{
			name: "plan checksum",
			change: func(input *V1ExtractionInput) {
				input.PlanChecksum = "sha256:" + strings.Repeat("f", 64)
			},
			reason: V1ExtractionBlockedReasonPlanChecksumMismatch,
		},
		{
			name: "release contract differs from preserved payload",
			change: func(input *V1ExtractionInput) {
				input.ReleaseContract.Config.ServiceConfigPath = "config/other.json"
			},
			reason: V1ExtractionBlockedReasonHistoryContractMismatch,
		},
		{
			name: "typed plan input differs from preserved payload",
			change: func(input *V1ExtractionInput) {
				input.PlanVariables[1].Value = json.RawMessage(`"https://changed.example.invalid"`)
			},
			reason: V1ExtractionBlockedReasonHistoryContractMismatch,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			input := validV1ExtractionInput(t)
			test.change(&input)

			result, err := ExtractV1TargetConfig(input)

			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(result.Draft).To(BeNil())
			g.Expect(result.BlockedReasonCode).To(Equal(test.reason))
		})
	}
}

func TestExtractV1TargetConfigBlocksUnsupportedOrAmbiguousTopology(t *testing.T) {
	tests := []struct {
		name   string
		change func(*V1ExtractionInput)
		reason V1ExtractionBlockedReasonCode
	}{
		{
			name: "unsupported schema",
			change: func(input *V1ExtractionInput) {
				input.ReleaseContract.Schema = "distr.component-release/v2"
				setV1HistoryPayloads(t, input)
			},
			reason: V1ExtractionBlockedReasonUnsupportedSchema,
		},
		{
			name: "multiple release components",
			change: func(input *V1ExtractionInput) {
				input.ReleaseContract.Components = append(
					input.ReleaseContract.Components,
					types.ReleaseContractComponent{Name: "worker", Platform: "linux/amd64"},
				)
				setV1HistoryPayloads(t, input)
			},
			reason: V1ExtractionBlockedReasonMultiComponent,
		},
		{
			name: "multiple targets",
			change: func(input *V1ExtractionInput) {
				input.PlanTargets = append(input.PlanTargets, input.PlanTargets[0])
			},
			reason: V1ExtractionBlockedReasonTargetCardinality,
		},
		{
			name: "multiple target components",
			change: func(input *V1ExtractionInput) {
				input.PlanTargetComponents = append(input.PlanTargetComponents, input.PlanTargetComponents[0])
			},
			reason: V1ExtractionBlockedReasonTargetComponentCardinality,
		},
		{
			name: "ambiguous component instance",
			change: func(input *V1ExtractionInput) {
				duplicate := input.ComponentInstances[0]
				duplicate.ID = uuid.New()
				input.ComponentInstances = append(input.ComponentInstances, duplicate)
			},
			reason: V1ExtractionBlockedReasonComponentAmbiguous,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			input := validV1ExtractionInput(t)
			test.change(&input)
			setV1HistoryPayloads(t, &input)

			result, err := ExtractV1TargetConfig(input)

			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(result.Draft).To(BeNil())
			g.Expect(result.BlockedReasonCode).To(Equal(test.reason))
		})
	}
}

func TestExtractV1TargetConfigBlocksMutableOrAmbiguousConfigObjects(t *testing.T) {
	tests := []struct {
		name   string
		change func(*V1ExtractionInput)
		reason V1ExtractionBlockedReasonCode
	}{
		{
			name: "mutable service config",
			change: func(input *V1ExtractionInput) {
				input.ReleaseContract.Config.ImmutableObjects[1].VersionID = ""
				setV1HistoryPayloads(t, input)
			},
			reason: V1ExtractionBlockedReasonMutableConfigObject,
		},
		{
			name: "missing compose object",
			change: func(input *V1ExtractionInput) {
				input.ReleaseContract.Config.ImmutableObjects =
					input.ReleaseContract.Config.ImmutableObjects[1:]
				setV1HistoryPayloads(t, input)
			},
			reason: V1ExtractionBlockedReasonConfigObjectMissing,
		},
		{
			name: "ambiguous service object",
			change: func(input *V1ExtractionInput) {
				input.ReleaseContract.Config.ImmutableObjects = append(
					input.ReleaseContract.Config.ImmutableObjects,
					input.ReleaseContract.Config.ImmutableObjects[1],
				)
				setV1HistoryPayloads(t, input)
			},
			reason: V1ExtractionBlockedReasonConfigObjectAmbiguous,
		},
		{
			name: "same object mapped to compose and service config",
			change: func(input *V1ExtractionInput) {
				input.ReleaseContract.Config.ServiceConfigPath =
					input.ReleaseContract.Config.ComposePath
				input.ReleaseContract.Config.ServiceConfigChecksum =
					input.ReleaseContract.Config.ComposeChecksum
				input.ReleaseContract.Config.ImmutableObjects =
					input.ReleaseContract.Config.ImmutableObjects[:1]
				input.PlanTargetComponents[0].ConfigChecksum =
					input.ReleaseContract.Config.ServiceConfigChecksum
				setV1HistoryPayloads(t, input)
			},
			reason: V1ExtractionBlockedReasonConfigObjectAmbiguous,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			input := validV1ExtractionInput(t)
			test.change(&input)

			result, err := ExtractV1TargetConfig(input)

			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(result.Draft).To(BeNil())
			g.Expect(result.BlockedReasonCode).To(Equal(test.reason))
		})
	}
}

func TestExtractV1TargetConfigBlocksPlaintextSecretsAndUnsafeReferences(t *testing.T) {
	tests := []struct {
		name   string
		change func(*V1ExtractionInput)
		reason V1ExtractionBlockedReasonCode
	}{
		{
			name: "plaintext non-secret variable",
			change: func(input *V1ExtractionInput) {
				input.PlanVariables[1].Key = "DATABASE_PASSWORD"
				input.PlanVariables[1].Value = json.RawMessage(`"plaintext-value"`)
			},
			reason: V1ExtractionBlockedReasonPlaintextSecret,
		},
		{
			name: "unresolved secret reference",
			change: func(input *V1ExtractionInput) {
				input.PlanVariables[0].Status = types.VariableResolutionStatusUnresolved
			},
			reason: V1ExtractionBlockedReasonSecretReferenceUnresolved,
		},
		{
			name: "unsafe secret reference",
			change: func(input *V1ExtractionInput) {
				input.PlanVariables[0].ReferenceID = "clients/example/config.json"
			},
			reason: V1ExtractionBlockedReasonSecretReferenceUnsafe,
		},
		{
			name: "inline value on secret reference",
			change: func(input *V1ExtractionInput) {
				input.PlanVariables[0].Value = json.RawMessage(`"plaintext-value"`)
			},
			reason: V1ExtractionBlockedReasonPlaintextSecret,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			input := validV1ExtractionInput(t)
			test.change(&input)
			setV1HistoryPayloads(t, &input)

			result, err := ExtractV1TargetConfig(input)

			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(result.Draft).To(BeNil())
			g.Expect(result.BlockedReasonCode).To(Equal(test.reason))
		})
	}
}

func validV1ExtractionInput(t *testing.T) V1ExtractionInput {
	t.Helper()
	organizationID := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	releaseBundleID := uuid.MustParse("10000000-0000-4000-8000-000000000002")
	planID := uuid.MustParse("10000000-0000-4000-8000-000000000003")
	planTargetID := uuid.MustParse("10000000-0000-4000-8000-000000000004")
	deploymentTargetID := uuid.MustParse("10000000-0000-4000-8000-000000000005")
	deploymentUnitID := uuid.MustParse("10000000-0000-4000-8000-000000000006")
	assignmentID := uuid.MustParse("10000000-0000-4000-8000-000000000007")
	environmentID := uuid.MustParse("10000000-0000-4000-8000-000000000008")
	componentInstanceID := uuid.MustParse("10000000-0000-4000-8000-000000000009")
	secretID := uuid.MustParse("10000000-0000-4000-8000-000000000010")
	componentDigest := "sha256:" + strings.Repeat("c", 64)
	input := V1ExtractionInput{
		OrganizationID:  organizationID,
		ReleaseBundleID: releaseBundleID,
		PlanID:          planID,
		ReleaseContract: &types.ReleaseContract{
			Schema: types.ReleaseContractSchemaV1,
			Source: types.ReleaseContractSource{
				Repository:   "https://git.example.invalid/acme/config.git",
				Branch:       "main",
				SourceCommit: strings.Repeat("2", 40),
				BuiltCommit:  strings.Repeat("2", 40),
			},
			Components: []types.ReleaseContractComponent{{
				Name:     "api",
				Version:  "1.2.3",
				Image:    "registry.example.invalid/acme/api@" + componentDigest,
				Platform: "linux/amd64",
			}},
			Config: types.ReleaseContractConfig{
				RepositoryCommit:      strings.Repeat("3", 40),
				ComposePath:           "config/docker-compose.yaml",
				ServiceConfigPath:     "config/service.json",
				ComposeChecksum:       "sha256:" + strings.Repeat("a", 64),
				ServiceConfigChecksum: "sha256:" + strings.Repeat("b", 64),
				ImmutableObjects: []types.ReleaseContractConfigObject{
					{
						URI: "s3://config-bucket/_immutable/sha256/" +
							strings.Repeat("a", 64) + "/config/docker-compose.yaml",
						Checksum: "sha256:" + strings.Repeat("a", 64),
					},
					{
						URI:       "s3://config-bucket/config/service.json",
						VersionID: "version-7",
						Checksum:  "sha256:" + strings.Repeat("b", 64),
					},
				},
			},
		},
		PlanTargets: []types.DeploymentPlanTarget{{
			ID:                 planTargetID,
			OrganizationID:     organizationID,
			DeploymentTargetID: deploymentTargetID,
			Name:               "example-target",
			Type:               types.DeploymentTypeDocker,
			Platform:           types.DeploymentTargetPlatformLinuxAMD64,
		}},
		PlanTargetComponents: []types.DeploymentPlanTargetComponent{{
			ID:                     uuid.New(),
			DeploymentPlanID:       planID,
			DeploymentPlanTargetID: planTargetID,
			OrganizationID:         organizationID,
			DeploymentTargetID:     deploymentTargetID,
			Component:              "api",
			Version:                "1.2.3",
			Image:                  "registry.example.invalid/acme/api@" + componentDigest,
			Platform:               types.DeploymentTargetPlatformLinuxAMD64,
			ConfigChecksum:         "sha256:" + strings.Repeat("b", 64),
		}},
		PlanVariables: []types.DeploymentPlanVariable{
			{
				OrganizationID: organizationID,
				Key:            "API_TOKEN",
				Type:           types.VariableTypeSecretReference,
				Status:         types.VariableResolutionStatusResolved,
				ReferenceID:    secretID.String(),
				Redacted:       true,
			},
			{
				OrganizationID: organizationID,
				Key:            "PUBLIC_URL",
				Type:           types.VariableTypeString,
				Status:         types.VariableResolutionStatusResolved,
				Value:          json.RawMessage(`"https://api.example.invalid"`),
			},
		},
		ComponentInstances: []types.ComponentInstance{{
			ID:               componentInstanceID,
			OrganizationID:   organizationID,
			DeploymentUnitID: deploymentUnitID,
			PhysicalName:     "api",
			ManagementState:  types.RegistryManagementStateManaged,
		}},
		DeploymentUnitID:              deploymentUnitID,
		TargetEnvironmentAssignmentID: assignmentID,
		EnvironmentID:                 environmentID,
	}
	setV1HistoryPayloads(t, &input)
	return input
}

func setV1HistoryPayloads(t *testing.T, input *V1ExtractionInput) {
	t.Helper()
	releasePayload, err := json.Marshal(struct {
		ReleaseContract *types.ReleaseContract `json:"releaseContract"`
	}{ReleaseContract: input.ReleaseContract})
	if err != nil {
		t.Fatal(err)
	}
	planPayload, err := json.Marshal(struct {
		ReleaseBundleID  string                      `json:"releaseBundleId"`
		EnvironmentID    string                      `json:"environmentId"`
		ReleaseContract  *types.ReleaseContract      `json:"releaseContract"`
		Targets          []v1TestPlanTarget          `json:"targets"`
		TargetComponents []v1TestPlanTargetComponent `json:"targetComponents"`
		Variables        []v1TestPlanVariable        `json:"variables"`
	}{
		ReleaseBundleID:  input.ReleaseBundleID.String(),
		EnvironmentID:    input.EnvironmentID.String(),
		ReleaseContract:  input.ReleaseContract,
		Targets:          v1TestPlanTargets(input.PlanTargets),
		TargetComponents: v1TestPlanTargetComponents(input.PlanTargetComponents),
		Variables:        v1TestPlanVariables(input.PlanVariables),
	})
	if err != nil {
		t.Fatal(err)
	}
	input.ReleaseCanonicalPayload = releasePayload
	input.ReleaseChecksum = v1TestChecksum(releasePayload)
	input.PlanCanonicalPayload = planPayload
	input.PlanChecksum = v1TestChecksum(planPayload)
}

func v1TestChecksum(payload []byte) string {
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

type v1TestPlanTarget struct {
	DeploymentTargetID     string `json:"deploymentTargetId"`
	Name                   string `json:"name"`
	Type                   string `json:"type"`
	Platform               string `json:"platform"`
	CustomerOrganizationID string `json:"customerOrganizationId,omitempty"`
	SortOrder              int    `json:"sortOrder"`
}

type v1TestPlanTargetComponent struct {
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

type v1TestPlanVariable struct {
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

func v1TestPlanTargets(values []types.DeploymentPlanTarget) []v1TestPlanTarget {
	result := make([]v1TestPlanTarget, 0, len(values))
	for _, value := range values {
		customerOrganizationID := ""
		if value.CustomerOrganizationID != nil {
			customerOrganizationID = value.CustomerOrganizationID.String()
		}
		result = append(result, v1TestPlanTarget{
			DeploymentTargetID:     value.DeploymentTargetID.String(),
			Name:                   value.Name,
			Type:                   string(value.Type),
			Platform:               string(value.Platform),
			CustomerOrganizationID: customerOrganizationID,
			SortOrder:              value.SortOrder,
		})
	}
	return result
}

func v1TestPlanTargetComponents(
	values []types.DeploymentPlanTargetComponent,
) []v1TestPlanTargetComponent {
	result := make([]v1TestPlanTargetComponent, 0, len(values))
	for _, value := range values {
		expectedReleaseBundleID := ""
		if value.ExpectedReleaseBundleID != nil {
			expectedReleaseBundleID = value.ExpectedReleaseBundleID.String()
		}
		result = append(result, v1TestPlanTargetComponent{
			DeploymentPlanTargetID:  value.DeploymentPlanTargetID.String(),
			DeploymentTargetID:      value.DeploymentTargetID.String(),
			Component:               value.Component,
			Version:                 value.Version,
			Image:                   value.Image,
			Platform:                string(value.Platform),
			Contracts:               value.Contracts,
			ConfigChecksum:          value.ConfigChecksum,
			ExpectedStateVersion:    value.ExpectedStateVersion,
			ExpectedStateChecksum:   value.ExpectedStateChecksum,
			ExpectedReleaseBundleID: expectedReleaseBundleID,
			SortOrder:               value.SortOrder,
		})
	}
	return result
}

func v1TestPlanVariables(values []types.DeploymentPlanVariable) []v1TestPlanVariable {
	result := make([]v1TestPlanVariable, 0, len(values))
	for _, value := range values {
		result = append(result, v1TestPlanVariable{
			VariableSetID: value.VariableSetID.String(),
			VariableID:    value.VariableID.String(),
			Key:           value.Key,
			Type:          string(value.Type),
			IsRequired:    value.IsRequired,
			Status:        string(value.Status),
			Source:        string(value.Source),
			Value:         value.Value,
			ReferenceID:   value.ReferenceID,
			ReferenceName: value.ReferenceName,
			Redacted:      value.Redacted,
			Trace:         value.Trace,
		})
	}
	return result
}
