package mapping

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
)

func DeploymentPolicyToAPI(policy types.DeploymentPolicy) api.DeploymentPolicy {
	return api.DeploymentPolicy{
		ID:          policy.ID,
		CreatedAt:   policy.CreatedAt,
		UpdatedAt:   policy.UpdatedAt,
		Key:         policy.Key,
		Name:        policy.Name,
		Description: policy.Description,
	}
}

func DeploymentPolicyVersionToAPI(version types.DeploymentPolicyVersion) api.DeploymentPolicyVersion {
	return api.DeploymentPolicyVersion{
		ID:                       version.ID,
		CreatedAt:                version.CreatedAt,
		UpdatedAt:                version.UpdatedAt,
		PolicyID:                 version.PolicyID,
		VersionNumber:            version.VersionNumber,
		State:                    version.State,
		Document:                 version.Document,
		CanonicalChecksum:        version.CanonicalChecksum,
		CreatedByUserAccountID:   version.CreatedByUserAccountID,
		PublishedByUserAccountID: version.PublishedByUserAccountID,
		PublishedAt:              version.PublishedAt,
	}
}

func DeploymentPolicyBindingToAPI(binding types.DeploymentPolicyBinding) api.DeploymentPolicyBinding {
	return api.DeploymentPolicyBinding{
		ID:                     binding.ID,
		CreatedAt:              binding.CreatedAt,
		PolicyVersionID:        binding.PolicyVersionID,
		ScopeKind:              binding.ScopeKind,
		ScopeID:                binding.ScopeID,
		Role:                   binding.Role,
		CreatedByUserAccountID: binding.CreatedByUserAccountID,
		RetiredAt:              binding.RetiredAt,
	}
}
