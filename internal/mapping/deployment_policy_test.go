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
