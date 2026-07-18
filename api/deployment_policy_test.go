package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCreateDeploymentPolicyRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		request CreateDeploymentPolicyRequest
		wantErr string
	}{
		{
			name: "valid",
			request: CreateDeploymentPolicyRequest{
				Key:  "standard-dev",
				Name: "Standard DEV",
			},
		},
		{
			name: "invalid key",
			request: CreateDeploymentPolicyRequest{
				Key:  "Standard DEV",
				Name: "Standard DEV",
			},
			wantErr: "key must be canonical lowercase text",
		},
		{
			name: "missing name",
			request: CreateDeploymentPolicyRequest{
				Key: "standard-dev",
			},
			wantErr: "name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			err := tt.request.Validate()
			if tt.wantErr == "" {
				g.Expect(err).NotTo(HaveOccurred())
			} else {
				g.Expect(err).To(MatchError(ContainSubstring(tt.wantErr)))
			}
		})
	}
}

func TestDeploymentPolicyListRequestValidate(t *testing.T) {
	g := NewWithT(t)
	g.Expect((DeploymentPolicyListRequest{}).Validate()).To(Succeed())
	g.Expect((DeploymentPolicyListRequest{
		Limit:  100,
		Cursor: "eyJ2IjoxfQ",
	}).Validate()).To(Succeed())
	g.Expect((DeploymentPolicyListRequest{Limit: 101}).Validate()).To(
		MatchError(ContainSubstring("limit must be between 1 and 100")),
	)
	g.Expect((DeploymentPolicyListRequest{Cursor: "not a cursor!"}).Validate()).To(
		MatchError(ContainSubstring("opaque URL-safe token")),
	)
	g.Expect((DeploymentPolicyListRequest{
		Cursor: strings.Repeat("a", 2049),
	}).Validate()).To(MatchError(ContainSubstring("cursor is too large")))
}

func TestDeploymentPolicyVersionPageOmitsDocumentPayload(t *testing.T) {
	g := NewWithT(t)
	payload, err := json.Marshal(DeploymentPolicyVersionPage{
		Items: []DeploymentPolicyVersionSummary{{
			ID:                uuid.New(),
			PolicyID:          uuid.New(),
			VersionNumber:     1,
			State:             types.DeploymentPolicyVersionStateDraft,
			CanonicalChecksum: "sha256:test",
		}},
		NextCursor: "opaque",
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(payload)).To(ContainSubstring(`"nextCursor":"opaque"`))
	g.Expect(string(payload)).NotTo(ContainSubstring(`"document"`))
	g.Expect(string(payload)).NotTo(ContainSubstring(`"canonicalPayload"`))
}

func TestCreateDeploymentPolicyVersionRequestValidateUsesExactPolicySchema(t *testing.T) {
	g := NewWithT(t)
	request := CreateDeploymentPolicyVersionRequest{
		Document: validDeploymentPolicyDocumentForAPI(),
	}
	g.Expect(request.Validate()).To(Succeed())

	request.Document.RiskGates[0].Condition = `system("not allowed")`
	g.Expect(request.Validate()).To(MatchError(ContainSubstring(
		"risk gate condition is not in the restricted expression language",
	)))

	request.Document = validDeploymentPolicyDocumentForAPI()
	request.Document.ApprovalRules[0].AuthorityKind = types.PolicyAuthorityOwner
	g.Expect(request.Validate()).To(MatchError(ContainSubstring(
		"document approval rules must not contain derived authority fields",
	)))
}

func TestCreateDeploymentPolicyBindingRequestValidate(t *testing.T) {
	g := NewWithT(t)
	request := CreateDeploymentPolicyBindingRequest{
		PolicyVersionID: uuid.New(),
		ScopeKind:       types.DeploymentPolicyBindingScopeCustomer,
		ScopeID:         uuid.New(),
		Role:            types.DeploymentPolicyBindingRoleSubscriber,
	}
	g.Expect(request.Validate()).To(Succeed())

	request.ScopeKind = types.DeploymentPolicyBindingScopeEnvironment
	g.Expect(request.Validate()).To(MatchError(ContainSubstring(
		"subscriber bindings require customer scope",
	)))
}

func validDeploymentPolicyDocumentForAPI() types.DeploymentPolicyDocument {
	overrideGroupID := uuid.New()
	return types.DeploymentPolicyDocument{
		Schema: types.DeploymentPolicySchemaV1,
		ApprovalRules: []types.ApprovalRule{{
			Key:              "four-eyes",
			PrincipalGroupID: uuid.New(),
			Quorum:           2,
			SeparationConstraints: []types.SeparationConstraint{
				types.SeparationConstraintRequesterCannotApprove,
				types.SeparationConstraintDistinctApprovers,
			},
		}},
		RiskGates: []types.PolicyRiskGate{{
			Key:       "artifact-integrity",
			Condition: "always()",
		}},
		AdmissionRules: types.AdmissionRules{
			AllowedResolutionModes: []types.RequirementResolutionMode{
				types.RequirementResolutionModeIncluded,
				types.RequirementResolutionModePinnedExisting,
			},
			MinimumBakeSeconds:          60,
			MaximumWaitSeconds:          300,
			MaintenanceWindowVersionIDs: []uuid.UUID{},
			FreezeRuleVersionIDs:        []uuid.UUID{},
		},
		CampaignRules: types.CampaignRules{
			MinimumWaveBakeSeconds:      120,
			MaximumWaveSize:             10,
			MaximumConcurrency:          2,
			FailureToleranceBasisPoints: 1000,
			MinimumHealthyBasisPoints:   9000,
		},
		OverrideRules: types.OverrideRules{
			Allowed:             true,
			AuthorityGroupID:    &overrideGroupID,
			ShortenableGateKeys: []string{"maintenance-wait"},
			MinimumReasonLength: 20,
		},
		RequiredEvidence: []string{"provenance"},
		BootstrapRules: types.BootstrapRules{
			Mode:             types.BootstrapModeRequireApproval,
			ApprovalRuleKeys: []string{"four-eyes"},
			RequiredGateKeys: []string{"artifact-integrity"},
		},
	}
}
