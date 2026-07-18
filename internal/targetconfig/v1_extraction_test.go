package targetconfig

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
				SizeBytes: 128,
				Checksum:  "sha256:" + strings.Repeat("a", 64),
			},
			{
				Key:       "service-config",
				Kind:      types.TargetConfigObjectKindServiceConfig,
				Reference: "s3://config-bucket/config/service.json",
				VersionID: "version-7",
				MediaType: "application/json",
				SizeBytes: 256,
				Checksum:  "sha256:" + strings.Repeat("b", 64),
			},
		},
		Components: []types.TargetConfigSnapshotComponentDraft{{
			PhysicalName:        "choice-api",
			ComponentInstanceID: input.ComponentInstances[0].ID,
			DeploymentUnitID:    input.DeploymentUnitID,
		}},
		SecretReferences: []types.TargetConfigSnapshotSecretReferenceDraft{{
			Key:                "api_token",
			Provider:           "distr",
			Reference:          input.PlanVariables[0].ReferenceID,
			VersionFingerprint: "sha256:" + strings.Repeat("d", 64),
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
	reordered.ConfigObjectEvidence[0], reordered.ConfigObjectEvidence[1] =
		reordered.ConfigObjectEvidence[1], reordered.ConfigObjectEvidence[0]
	setV1HistoryPayloads(t, &reordered)
	reorderedResult, err := ExtractV1TargetConfig(reordered)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(reorderedResult.BlockedReasonCode).To(BeEmpty())
	g.Expect(reorderedResult.CanonicalPayload).To(Equal(result.CanonicalPayload))
	g.Expect(reorderedResult.CanonicalChecksum).To(Equal(result.CanonicalChecksum))
}

func TestExtractV1TargetConfigNeverOmitsUnsupportedV1VariableMaterial(t *testing.T) {
	tests := []struct {
		name     string
		variable types.DeploymentPlanVariable
		reason   V1ExtractionBlockedReasonCode
	}{
		{
			name: "string",
			variable: types.DeploymentPlanVariable{
				Key: "PUBLIC_URL", Type: types.VariableTypeString,
				Status: types.VariableResolutionStatusResolved,
				Value:  json.RawMessage(`"https://api.example.invalid"`),
			},
			reason: V1ExtractionBlockedReasonVariableNotRepresentable,
		},
		{
			name: "number",
			variable: types.DeploymentPlanVariable{
				Key: "WORKERS", Type: types.VariableTypeNumber,
				Status: types.VariableResolutionStatusResolved,
				Value:  json.RawMessage(`4`),
			},
			reason: V1ExtractionBlockedReasonVariableNotRepresentable,
		},
		{
			name: "boolean",
			variable: types.DeploymentPlanVariable{
				Key: "AUDIT_ENABLED", Type: types.VariableTypeBoolean,
				Status: types.VariableResolutionStatusResolved,
				Value:  json.RawMessage(`true`),
			},
			reason: V1ExtractionBlockedReasonVariableNotRepresentable,
		},
		{
			name: "json",
			variable: types.DeploymentPlanVariable{
				Key: "LIMITS", Type: types.VariableTypeJSON,
				Status: types.VariableResolutionStatusResolved,
				Value:  json.RawMessage(`{"burst":10}`),
			},
			reason: V1ExtractionBlockedReasonVariableNotRepresentable,
		},
		{
			name: "account reference",
			variable: types.DeploymentPlanVariable{
				Key: "BANK_ACCOUNT", Type: types.VariableTypeAccountReference,
				Status:      types.VariableResolutionStatusResolved,
				ReferenceID: "account-7",
			},
			reason: V1ExtractionBlockedReasonVariableNotRepresentable,
		},
		{
			name: "certificate reference",
			variable: types.DeploymentPlanVariable{
				Key: "TLS_CERT", Type: types.VariableTypeCertificateReference,
				Status:      types.VariableResolutionStatusResolved,
				ReferenceID: "certificate-9",
			},
			reason: V1ExtractionBlockedReasonVariableNotRepresentable,
		},
		{
			name: "required unresolved",
			variable: types.DeploymentPlanVariable{
				Key: "PUBLIC_URL", Type: types.VariableTypeString, IsRequired: true,
				Status: types.VariableResolutionStatusUnresolved,
				Source: types.VariableResolutionSourceUnresolved,
			},
			reason: V1ExtractionBlockedReasonRequiredVariableUnresolved,
		},
		{
			name: "optional unresolved remains fail closed",
			variable: types.DeploymentPlanVariable{
				Key: "OPTIONAL_URL", Type: types.VariableTypeString,
				Status: types.VariableResolutionStatusUnresolved,
				Source: types.VariableResolutionSourceUnresolved,
			},
			reason: V1ExtractionBlockedReasonVariableUnresolved,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			input := validV1ExtractionInput(t)
			input.PlanVariables = append(input.PlanVariables, test.variable)
			setV1HistoryPayloads(t, &input)

			result, err := ExtractV1TargetConfig(input)

			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(result.Draft).To(BeNil())
			g.Expect(result.BlockedReasonCode).To(Equal(test.reason))
		})
	}
}

func TestExtractV1TargetConfigBindsExactProviderObjectEvidenceAndVerifies(t *testing.T) {
	g := NewWithT(t)
	input := validV1ExtractionInput(t)

	result, err := ExtractV1TargetConfig(input)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.BlockedReasonCode).To(BeEmpty())
	g.Expect(result.Draft).NotTo(BeNil())
	snapshot := types.TargetConfigSnapshot{ID: uuid.New()}
	for _, object := range result.Draft.Objects {
		snapshot.Objects = append(snapshot.Objects, types.TargetConfigSnapshotObject{
			Key:       object.Key,
			Kind:      object.Kind,
			Reference: object.Reference,
			VersionID: object.VersionID,
			MediaType: object.MediaType,
			SizeBytes: object.SizeBytes,
			Checksum:  object.Checksum,
		})
	}
	verification, err := VerifyObjects(
		t.Context(),
		snapshot,
		v1EvidenceObjectVerifier{evidence: input.ConfigObjectEvidence},
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(verification.Verified).To(BeTrue())
	g.Expect(verification.Objects).To(HaveLen(2))
	g.Expect(verification.Objects[0].Verified).To(BeTrue())
	g.Expect(verification.Objects[1].Verified).To(BeTrue())
}

func TestExtractV1TargetConfigBlocksUnavailableOrMismatchedObjectEvidence(t *testing.T) {
	tests := []struct {
		name   string
		change func(*V1ExtractionInput)
		reason V1ExtractionBlockedReasonCode
	}{
		{
			name: "missing evidence",
			change: func(input *V1ExtractionInput) {
				input.ConfigObjectEvidence = input.ConfigObjectEvidence[:1]
			},
			reason: V1ExtractionBlockedReasonConfigObjectEvidenceUnavailable,
		},
		{
			name: "duplicate evidence",
			change: func(input *V1ExtractionInput) {
				input.ConfigObjectEvidence = append(
					input.ConfigObjectEvidence,
					input.ConfigObjectEvidence[0],
				)
			},
			reason: V1ExtractionBlockedReasonConfigObjectEvidenceAmbiguous,
		},
		{
			name: "wrong version",
			change: func(input *V1ExtractionInput) {
				input.ConfigObjectEvidence[1].VersionID = "version-8"
			},
			reason: V1ExtractionBlockedReasonConfigObjectEvidenceMismatch,
		},
		{
			name: "wrong digest",
			change: func(input *V1ExtractionInput) {
				input.ConfigObjectEvidence[0].Checksum = "sha256:" + strings.Repeat("e", 64)
			},
			reason: V1ExtractionBlockedReasonConfigObjectEvidenceMismatch,
		},
		{
			name: "missing media type",
			change: func(input *V1ExtractionInput) {
				input.ConfigObjectEvidence[0].MediaType = ""
			},
			reason: V1ExtractionBlockedReasonConfigObjectEvidenceMismatch,
		},
		{
			name: "negative size",
			change: func(input *V1ExtractionInput) {
				input.ConfigObjectEvidence[0].SizeBytes = -1
			},
			reason: V1ExtractionBlockedReasonConfigObjectEvidenceMismatch,
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

func TestExtractV1TargetConfigRequiresScopedImmutableSecretEvidence(t *testing.T) {
	customerID := uuid.MustParse("10000000-0000-4000-8000-000000000099")
	tests := []struct {
		name   string
		change func(*V1ExtractionInput)
		reason V1ExtractionBlockedReasonCode
	}{
		{
			name: "reference does not exist in scoped evidence",
			change: func(input *V1ExtractionInput) {
				input.SecretReferenceEvidence = nil
			},
			reason: V1ExtractionBlockedReasonSecretReferenceUnavailable,
		},
		{
			name: "foreign organization is tenant safe unavailable",
			change: func(input *V1ExtractionInput) {
				input.SecretReferenceEvidence[0].OrganizationID = uuid.New()
			},
			reason: V1ExtractionBlockedReasonSecretReferenceUnavailable,
		},
		{
			name: "customer scope mismatch is tenant safe unavailable",
			change: func(input *V1ExtractionInput) {
				input.PlanTargets[0].CustomerOrganizationID = &customerID
				setV1HistoryPayloads(t, input)
			},
			reason: V1ExtractionBlockedReasonSecretReferenceUnavailable,
		},
		{
			name: "mutable secret has no immutable provider version",
			change: func(input *V1ExtractionInput) {
				input.SecretReferenceEvidence[0].VersionFingerprint = ""
			},
			reason: V1ExtractionBlockedReasonSecretVersionUnavailable,
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

func TestExtractV1TargetConfigSecretRotationChangesSnapshotChecksum(t *testing.T) {
	g := NewWithT(t)
	first := validV1ExtractionInput(t)
	second := validV1ExtractionInput(t)
	second.SecretReferenceEvidence[0].VersionFingerprint =
		"sha256:" + strings.Repeat("e", 64)

	firstResult, err := ExtractV1TargetConfig(first)
	g.Expect(err).NotTo(HaveOccurred())
	secondResult, err := ExtractV1TargetConfig(second)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(firstResult.BlockedReasonCode).To(BeEmpty())
	g.Expect(secondResult.BlockedReasonCode).To(BeEmpty())
	g.Expect(secondResult.CanonicalChecksum).NotTo(Equal(firstResult.CanonicalChecksum))
}

func TestExtractV1TargetConfigResolvesLogicalComponentIdentityAndActiveAliases(t *testing.T) {
	tests := []struct {
		name        string
		component   string
		change      func(*V1ExtractionInput)
		wantBlocked V1ExtractionBlockedReasonCode
	}{
		{
			name:      "canonical definition key",
			component: "api",
		},
		{
			name:      "canonical definition name",
			component: "Payments API",
		},
		{
			name:      "active alias",
			component: "legacy-api",
		},
		{
			name:      "retired alias",
			component: "legacy-api",
			change: func(input *V1ExtractionInput) {
				retiredAt := input.ComponentAliases[0].CreatedAt.Add(1)
				input.ComponentAliases[0].RetiredAt = &retiredAt
			},
			wantBlocked: V1ExtractionBlockedReasonComponentMissing,
		},
		{
			name:      "retired definition",
			component: "api",
			change: func(input *V1ExtractionInput) {
				retiredAt := input.ComponentDefinitions[0].CreatedAt.Add(1)
				input.ComponentDefinitions[0].RetiredAt = &retiredAt
				input.ComponentDefinitions[0].ManagementState = types.RegistryManagementStateRetired
			},
			wantBlocked: V1ExtractionBlockedReasonComponentMissing,
		},
		{
			name:      "multiple logical matches",
			component: "legacy-api",
			change: func(input *V1ExtractionInput) {
				secondDefinition := input.ComponentDefinitions[0]
				secondDefinition.ID = uuid.New()
				secondDefinition.Key = "worker"
				secondDefinition.Name = "Worker"
				input.ComponentDefinitions = append(input.ComponentDefinitions, secondDefinition)
				secondAlias := input.ComponentAliases[0]
				secondAlias.ID = uuid.New()
				secondAlias.ComponentDefinitionID = secondDefinition.ID
				input.ComponentAliases = append(input.ComponentAliases, secondAlias)
				secondInstance := input.ComponentInstances[0]
				secondInstance.ID = uuid.New()
				secondInstance.ComponentDefinitionID = secondDefinition.ID
				secondInstance.PhysicalName = "choice-worker"
				input.ComponentInstances = append(input.ComponentInstances, secondInstance)
			},
			wantBlocked: V1ExtractionBlockedReasonComponentAmbiguous,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			input := validV1ExtractionInput(t)
			input.PlanTargetComponents[0].Component = test.component
			input.ReleaseContract.Components[0].Name = test.component
			if test.change != nil {
				test.change(&input)
			}
			setV1HistoryPayloads(t, &input)

			result, err := ExtractV1TargetConfig(input)

			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(result.BlockedReasonCode).To(Equal(test.wantBlocked))
			if test.wantBlocked == "" {
				g.Expect(result.Draft).NotTo(BeNil())
				g.Expect(result.Draft.Components[0].PhysicalName).To(Equal("choice-api"))
			} else {
				g.Expect(result.Draft).To(BeNil())
			}
		})
	}
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
				input.PlanVariables[0].ReferenceName = "changed-secret"
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
				input.PlanVariables = append(input.PlanVariables, types.DeploymentPlanVariable{
					Key:    "DATABASE_PASSWORD",
					Type:   types.VariableTypeString,
					Status: types.VariableResolutionStatusResolved,
					Value:  json.RawMessage(`"plaintext-value"`),
				})
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
	componentDefinitionID := uuid.MustParse("10000000-0000-4000-8000-000000000011")
	componentAliasID := uuid.MustParse("10000000-0000-4000-8000-000000000012")
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
		},
		ComponentDefinitions: []types.ComponentDefinition{{
			ID:              componentDefinitionID,
			OrganizationID:  organizationID,
			Key:             "api",
			Name:            "Payments API",
			ManagementState: types.RegistryManagementStateManaged,
		}},
		ComponentAliases: []types.ComponentAlias{{
			ID:                    componentAliasID,
			OrganizationID:        organizationID,
			ComponentDefinitionID: componentDefinitionID,
			Alias:                 "legacy-api",
		}},
		ComponentInstances: []types.ComponentInstance{{
			ID:                    componentInstanceID,
			OrganizationID:        organizationID,
			DeploymentUnitID:      deploymentUnitID,
			ComponentDefinitionID: componentDefinitionID,
			PhysicalName:          "choice-api",
			ManagementState:       types.RegistryManagementStateManaged,
		}},
		ConfigObjectEvidence: []V1ConfigObjectEvidence{
			{
				Reference: "s3://config-bucket/_immutable/sha256/" +
					strings.Repeat("a", 64) + "/config/docker-compose.yaml",
				MediaType: "application/yaml",
				SizeBytes: 128,
				Checksum:  "sha256:" + strings.Repeat("a", 64),
			},
			{
				Reference: "s3://config-bucket/config/service.json",
				VersionID: "version-7",
				MediaType: "application/json",
				SizeBytes: 256,
				Checksum:  "sha256:" + strings.Repeat("b", 64),
			},
		},
		SecretReferenceEvidence: []V1SecretReferenceEvidence{{
			Provider:           "distr",
			ReferenceID:        secretID,
			OrganizationID:     organizationID,
			VersionFingerprint: "sha256:" + strings.Repeat("d", 64),
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

type v1EvidenceObjectVerifier struct {
	evidence []V1ConfigObjectEvidence
}

func (verifier v1EvidenceObjectVerifier) Verify(
	_ context.Context,
	object types.TargetConfigSnapshotObject,
) (types.VerifiedTargetConfigObject, error) {
	for _, evidence := range verifier.evidence {
		if evidence.Reference == object.Reference {
			return types.VerifiedTargetConfigObject{
				Reference: evidence.Reference,
				VersionID: evidence.VersionID,
				MediaType: evidence.MediaType,
				SizeBytes: evidence.SizeBytes,
				Checksum:  evidence.Checksum,
			}, nil
		}
	}
	return types.VerifiedTargetConfigObject{}, errors.New("object evidence not found")
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
