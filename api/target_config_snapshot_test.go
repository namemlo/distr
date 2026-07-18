package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCreateTargetConfigSnapshotRequestValidationAndConversion(t *testing.T) {
	g := NewWithT(t)
	draft := validAPITargetConfigDraft()
	request := CreateTargetConfigSnapshotRequest{
		DeploymentUnitID:              draft.DeploymentUnitID,
		TargetEnvironmentAssignmentID: draft.TargetEnvironmentAssignmentID,
		EnvironmentID:                 draft.EnvironmentID,
		SourceRepository:              draft.SourceRepository,
		SourceCommit:                  draft.SourceCommit,
		SourceAdapter:                 draft.SourceAdapter,
		AdapterVersion:                draft.AdapterVersion,
		TargetPlatform:                draft.TargetPlatform,
		RuntimeConstraints:            draft.RuntimeConstraints,
		Objects:                       draft.Objects,
		Components:                    draft.Components,
		SecretReferences:              draft.SecretReferences,
		FeatureFlags:                  draft.FeatureFlags,
	}

	g.Expect(request.Validate()).To(Succeed())
	organizationID := uuid.New()
	converted := request.ToDraft(organizationID)
	g.Expect(converted.OrganizationID).To(Equal(organizationID))
	g.Expect(converted.DeploymentUnitID).To(Equal(draft.DeploymentUnitID))

	request.Objects[0].Reference = "s3://bucket/current/config.json"
	g.Expect(request.Validate()).To(MatchError(ContainSubstring("immutable")))
}

func TestTargetConfigSnapshotAPIContractDoesNotExposeOrganizationOrCanonicalPayload(t *testing.T) {
	g := NewWithT(t)
	creatorID := uuid.New()
	response := TargetConfigSnapshot{
		ID:                     uuid.New(),
		CreatedByUserAccountID: creatorID,
		CanonicalChecksum:      "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SecretReferences: []TargetConfigSnapshotSecretReference{{
			Key: "database", Provider: "vault", OpaqueReference: "kv:database",
			VersionFingerprint: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		}},
	}

	payload, err := json.Marshal(response)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(payload)).NotTo(ContainSubstring("organization"))
	g.Expect(string(payload)).NotTo(ContainSubstring("canonicalPayload"))
	g.Expect(string(payload)).NotTo(ContainSubstring("secretValue"))
	g.Expect(string(payload)).To(ContainSubstring(creatorID.String()))

	requestPayload, err := json.Marshal(CreateTargetConfigSnapshotRequest{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(requestPayload)).NotTo(ContainSubstring("createdBy"))
}

func TestTargetConfigObjectVerificationFactPreservesZeroObservedSizePresence(t *testing.T) {
	g := NewWithT(t)
	zero := int64(0)

	observed, err := json.Marshal(TargetConfigObjectVerificationFact{
		Key:               "compose",
		ObservedSizeBytes: &zero,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(observed)).To(ContainSubstring(`"observedSizeBytes":0`))

	unavailable, err := json.Marshal(TargetConfigObjectVerificationFact{Key: "compose"})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(unavailable)).NotTo(ContainSubstring("observedSizeBytes"))
}

func TestTargetConfigSnapshotListRequestDefaultsOmittedLimitAndRejectsExplicitZero(t *testing.T) {
	g := NewWithT(t)

	omitted, err := TargetConfigSnapshotListRequestFromQuery("", "", "", "")

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(omitted.Limit).To(Equal(50))

	_, err = TargetConfigSnapshotListRequestFromQuery("", "", "0", "")
	g.Expect(err).To(MatchError("limit must be between 1 and 100"))
}

func validAPITargetConfigDraft() types.TargetConfigSnapshotDraft {
	unitID := uuid.New()
	return types.TargetConfigSnapshotDraft{
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
			Reference: "s3://bucket/_immutable/sha256/" + strings.Repeat("a", 64) + "/compose.yaml",
			Checksum:  "sha256:" + strings.Repeat("a", 64), MediaType: "application/yaml", SizeBytes: 10,
		}},
		Components: []types.TargetConfigSnapshotComponentDraft{{
			PhysicalName: "api", ComponentInstanceID: uuid.New(), DeploymentUnitID: unitID,
		}},
		SecretReferences: []types.TargetConfigSnapshotSecretReferenceDraft{{
			Key: "database", Provider: "vault", Reference: "kv:database",
			VersionFingerprint: "sha256:" + strings.Repeat("b", 64),
		}},
		FeatureFlags: []types.TargetConfigSnapshotFeatureFlagDraft{{Key: "audit", Enabled: true}},
	}
}
