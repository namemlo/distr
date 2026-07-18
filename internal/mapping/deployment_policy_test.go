package mapping

import (
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestDeploymentPolicyMappingsOmitOrganizationInternals(t *testing.T) {
	g := NewWithT(t)
	policy := types.DeploymentPolicy{
		ID:             uuid.New(),
		OrganizationID: uuid.New(),
		Key:            "standard-dev",
		Name:           "Standard DEV",
		Description:    "Default policy",
	}
	version := types.DeploymentPolicyVersion{
		ID:                     uuid.New(),
		OrganizationID:         policy.OrganizationID,
		PolicyID:               policy.ID,
		VersionNumber:          2,
		State:                  types.DeploymentPolicyVersionStateDraft,
		CanonicalChecksum:      "sha256:test",
		CreatedByUserAccountID: uuid.New(),
		Document: types.DeploymentPolicyDocument{
			Schema: types.DeploymentPolicySchemaV1,
		},
	}

	mappedPolicy := DeploymentPolicyToAPI(policy)
	mappedVersion := DeploymentPolicyVersionToAPI(version)

	g.Expect(mappedPolicy.ID).To(Equal(policy.ID))
	g.Expect(mappedPolicy.Key).To(Equal(policy.Key))
	g.Expect(mappedVersion.PolicyID).To(Equal(policy.ID))
	g.Expect(mappedVersion.VersionNumber).To(Equal(2))
	g.Expect(mappedVersion.Document.Schema).To(Equal(types.DeploymentPolicySchemaV1))
}

func TestDeploymentPolicyPageMappingsExposeCursorMetadataAndVersionSummaries(t *testing.T) {
	g := NewWithT(t)
	policy := types.DeploymentPolicy{ID: uuid.New(), Key: "standard-dev"}
	version := types.DeploymentPolicyVersionSummary{
		ID:                uuid.New(),
		PolicyID:          policy.ID,
		VersionNumber:     3,
		State:             types.DeploymentPolicyVersionStatePublished,
		CanonicalChecksum: "sha256:test",
	}
	binding := types.DeploymentPolicyBinding{
		ID:              uuid.New(),
		PolicyVersionID: version.ID,
	}

	policies := DeploymentPolicyPageToAPI(types.Page[types.DeploymentPolicy]{
		Items:      []types.DeploymentPolicy{policy},
		NextCursor: "policy-cursor",
	})
	versions := DeploymentPolicyVersionPageToAPI(
		types.Page[types.DeploymentPolicyVersionSummary]{
			Items:      []types.DeploymentPolicyVersionSummary{version},
			NextCursor: "version-cursor",
		},
	)
	bindings := DeploymentPolicyBindingPageToAPI(types.Page[types.DeploymentPolicyBinding]{
		Items:      []types.DeploymentPolicyBinding{binding},
		NextCursor: "binding-cursor",
	})

	g.Expect(policies.Items).To(HaveLen(1))
	g.Expect(policies.NextCursor).To(Equal("policy-cursor"))
	g.Expect(versions.Items).To(HaveLen(1))
	g.Expect(versions.Items[0].VersionNumber).To(Equal(3))
	g.Expect(versions.NextCursor).To(Equal("version-cursor"))
	g.Expect(bindings.Items).To(HaveLen(1))
	g.Expect(bindings.NextCursor).To(Equal("binding-cursor"))
}
