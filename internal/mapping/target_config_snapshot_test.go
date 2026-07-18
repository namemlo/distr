package mapping

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestTargetConfigSnapshotToAPIHidesInternalOrganizationAndPayload(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	creatorID := uuid.New()
	snapshot := types.TargetConfigSnapshot{
		ID:                            uuid.New(),
		CreatedAt:                     time.Now().UTC(),
		CreatedByUserAccountID:        creatorID,
		OrganizationID:                organizationID,
		DeploymentUnitID:              uuid.New(),
		TargetEnvironmentAssignmentID: uuid.New(),
		EnvironmentID:                 uuid.New(),
		SourceRepository:              "https://git.example.invalid/config",
		SourceCommit:                  strings.Repeat("a", 40),
		SourceAdapter:                 "git",
		AdapterVersion:                "1.0.0",
		TargetPlatform:                "linux/amd64",
		RuntimeConstraints:            json.RawMessage(`{"runtime":"compose"}`),
		CanonicalPayload:              json.RawMessage(`{"organizationId":"` + organizationID.String() + `"}`),
		CanonicalChecksum:             "sha256:" + strings.Repeat("b", 64),
		Objects: []types.TargetConfigSnapshotObject{{
			OrganizationID: organizationID, Key: "compose",
			Kind:      types.TargetConfigObjectKindDeploymentDescriptor,
			Reference: "s3://bucket/object", MediaType: "application/yaml",
			Checksum: "sha256:" + strings.Repeat("c", 64),
		}},
		SecretReferences: []types.TargetConfigSnapshotSecretReference{{
			OrganizationID: organizationID, Key: "database", Provider: "vault",
			Reference: "kv:database", VersionFingerprint: "sha256:" + strings.Repeat("d", 64),
		}},
	}

	response := TargetConfigSnapshotToAPI(snapshot)
	payload, err := json.Marshal(response)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(response.SecretReferences[0].OpaqueReference).To(Equal("kv:database"))
	g.Expect(response.CreatedByUserAccountID).To(Equal(creatorID))
	g.Expect(string(payload)).NotTo(ContainSubstring(organizationID.String()))
	g.Expect(string(payload)).NotTo(ContainSubstring("canonicalPayload"))
}

func TestTargetConfigVerificationResultToAPI(t *testing.T) {
	g := NewWithT(t)
	zero := int64(0)
	result := types.ObjectVerificationResult{
		SnapshotID: uuid.New(),
		Verified:   false,
		Objects: []types.ObjectVerificationFact{{
			Key: "compose", Code: "checksum_mismatch",
			Message:           "object checksum does not match snapshot",
			ObservedSizeBytes: &zero,
			ObservedChecksum:  "sha256:" + strings.Repeat("a", 64),
		}},
	}

	response := TargetConfigVerificationResultToAPI(result)

	g.Expect(response.SnapshotID).To(Equal(result.SnapshotID))
	g.Expect(response.Verified).To(BeFalse())
	g.Expect(response.Objects).To(HaveLen(1))
	g.Expect(response.Objects[0].Code).To(Equal("checksum_mismatch"))
	g.Expect(response.Objects[0].ObservedSizeBytes).NotTo(BeNil())
	g.Expect(*response.Objects[0].ObservedSizeBytes).To(BeZero())
}
