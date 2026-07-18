package targetconfig

import (
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestTargetConfigCanonicalizeIsStableAcrossInputOrder(t *testing.T) {
	g := NewWithT(t)
	draft := validTargetConfigDraft()
	draft.Objects = []types.TargetConfigSnapshotObjectDraft{
		{
			Key: "service", Kind: types.TargetConfigObjectKindServiceConfig,
			Reference: immutableTargetConfigURI("b"), Checksum: targetConfigDigest("b"),
			MediaType: "application/json", SizeBytes: 12,
		},
		{
			Key: "compose", Kind: types.TargetConfigObjectKindDeploymentDescriptor,
			Reference: immutableTargetConfigURI("a"), Checksum: targetConfigDigest("a"),
			MediaType: "application/yaml", SizeBytes: 18,
		},
	}
	draft.Components = []types.TargetConfigSnapshotComponentDraft{
		{PhysicalName: "web", ComponentInstanceID: uuid.New(), DeploymentUnitID: draft.DeploymentUnitID},
		{PhysicalName: "api", ComponentInstanceID: uuid.New(), DeploymentUnitID: draft.DeploymentUnitID},
	}
	draft.SecretReferences = []types.TargetConfigSnapshotSecretReferenceDraft{
		{Key: "db", Provider: "vault", Reference: "kv:database", VersionFingerprint: targetConfigDigest("c")},
		{Key: "api", Provider: "vault", Reference: "kv:api", VersionFingerprint: targetConfigDigest("d")},
	}
	draft.FeatureFlags = []types.TargetConfigSnapshotFeatureFlagDraft{
		{Key: "new-ui", Enabled: true},
		{Key: "audit", Enabled: false},
	}

	firstBytes, firstChecksum, err := Canonicalize(draft)
	g.Expect(err).NotTo(HaveOccurred())

	draft.Objects[0], draft.Objects[1] = draft.Objects[1], draft.Objects[0]
	draft.Components[0], draft.Components[1] = draft.Components[1], draft.Components[0]
	draft.SecretReferences[0], draft.SecretReferences[1] = draft.SecretReferences[1], draft.SecretReferences[0]
	draft.FeatureFlags[0], draft.FeatureFlags[1] = draft.FeatureFlags[1], draft.FeatureFlags[0]
	secondBytes, secondChecksum, err := Canonicalize(draft)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(secondBytes).To(Equal(firstBytes))
	g.Expect(secondChecksum).To(Equal(firstChecksum))
	g.Expect(string(firstBytes)).To(ContainSubstring(`"schema":"distr.target-config/v1"`))
}

func TestTargetConfigCanonicalizeChangesChecksumForMaterialChange(t *testing.T) {
	g := NewWithT(t)
	draft := validTargetConfigDraft()
	_, firstChecksum, err := Canonicalize(draft)
	g.Expect(err).NotTo(HaveOccurred())

	draft.FeatureFlags[0].Enabled = !draft.FeatureFlags[0].Enabled
	_, secondChecksum, err := Canonicalize(draft)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(secondChecksum).NotTo(Equal(firstChecksum))
}

func TestTargetConfigCanonicalizeTreatsNilAndEmptyCollectionsEqually(t *testing.T) {
	g := NewWithT(t)
	nilCollections := validTargetConfigDraft()
	nilCollections.Objects = nil
	nilCollections.Components = nil
	nilCollections.SecretReferences = nil
	nilCollections.FeatureFlags = nil
	nilCollections.RuntimeConstraints = nil

	emptyCollections := nilCollections
	emptyCollections.Objects = []types.TargetConfigSnapshotObjectDraft{}
	emptyCollections.Components = []types.TargetConfigSnapshotComponentDraft{}
	emptyCollections.SecretReferences = []types.TargetConfigSnapshotSecretReferenceDraft{}
	emptyCollections.FeatureFlags = []types.TargetConfigSnapshotFeatureFlagDraft{}
	emptyCollections.RuntimeConstraints = map[string]string{}

	nilPayload, nilChecksum, err := Canonicalize(nilCollections)
	g.Expect(err).NotTo(HaveOccurred())
	emptyPayload, emptyChecksum, err := Canonicalize(emptyCollections)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(nilPayload).To(Equal(emptyPayload))
	g.Expect(nilChecksum).To(Equal(emptyChecksum))
	g.Expect(string(nilPayload)).To(ContainSubstring(`"objects":[]`))
	g.Expect(string(nilPayload)).To(ContainSubstring(`"components":[]`))
	g.Expect(string(nilPayload)).To(ContainSubstring(`"secretReferences":[]`))
	g.Expect(string(nilPayload)).To(ContainSubstring(`"featureFlags":[]`))
	g.Expect(string(nilPayload)).NotTo(ContainSubstring(`:null`))
}

func TestTargetConfigCanonicalizeRejectsDuplicateStableKeys(t *testing.T) {
	g := NewWithT(t)
	draft := validTargetConfigDraft()
	draft.Objects = append(draft.Objects, draft.Objects[0])

	_, _, err := Canonicalize(draft)

	g.Expect(err).To(MatchError(ContainSubstring("duplicate object key")))
}

func validTargetConfigDraft() types.TargetConfigSnapshotDraft {
	unitID := uuid.New()
	return types.TargetConfigSnapshotDraft{
		OrganizationID:                uuid.New(),
		DeploymentUnitID:              unitID,
		TargetEnvironmentAssignmentID: uuid.New(),
		EnvironmentID:                 uuid.New(),
		SourceRepository:              "https://git.example.invalid/platform/config",
		SourceCommit:                  strings.Repeat("a", 40),
		SourceAdapter:                 "git",
		AdapterVersion:                "1.0.0",
		TargetPlatform:                "linux/amd64",
		RuntimeConstraints:            map[string]string{"runtime": "compose"},
		Objects: []types.TargetConfigSnapshotObjectDraft{{
			Key: "compose", Kind: types.TargetConfigObjectKindDeploymentDescriptor,
			Reference: immutableTargetConfigURI("a"), Checksum: targetConfigDigest("a"),
			MediaType: "application/yaml", SizeBytes: 18,
		}},
		Components: []types.TargetConfigSnapshotComponentDraft{{
			PhysicalName: "api", ComponentInstanceID: uuid.New(), DeploymentUnitID: unitID,
		}},
		SecretReferences: []types.TargetConfigSnapshotSecretReferenceDraft{{
			Key: "database", Provider: "vault", Reference: "kv:database",
			VersionFingerprint: targetConfigDigest("b"),
		}},
		FeatureFlags: []types.TargetConfigSnapshotFeatureFlagDraft{{
			Key: "audit", Enabled: true,
		}},
	}
}

func immutableTargetConfigURI(hexCharacter string) string {
	return "s3://config-bucket/_immutable/sha256/" + strings.Repeat(hexCharacter, 64) + "/config.json"
}

func targetConfigDigest(hexCharacter string) string {
	return "sha256:" + strings.Repeat(hexCharacter, 64)
}
