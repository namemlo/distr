package governance

import (
	"slices"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
)

func TestValidateDeploymentPolicyVersionRejectsInvalidRestrictedExpression(t *testing.T) {
	g := NewWithT(t)
	version := validDeploymentPolicyVersion(uuid.New(), uuid.New(), uuid.New(), uuid.New())
	version.Document.RiskGates[0].Condition = `system("curl example.invalid")`

	issues := ValidateDeploymentPolicyVersion(version)

	g.Expect(issues).To(ContainElement(MatchFields(IgnoreExtras, Fields{
		"Code": Equal("policy.risk_gate.expression_invalid"),
	})))
}

func TestValidateDeploymentPolicyVersionRequiresConcreteBootstrapApproval(t *testing.T) {
	g := NewWithT(t)
	version := validDeploymentPolicyVersion(uuid.New(), uuid.New(), uuid.New(), uuid.New())
	version.Document.BootstrapRules.ApprovalRuleKeys = []string{"missing-rule"}

	issues := ValidateDeploymentPolicyVersion(version)

	g.Expect(issues).To(ContainElement(MatchFields(IgnoreExtras, Fields{
		"Code": Equal("policy.bootstrap.approval_rule_not_found"),
	})))
}

func TestComposeEffectivePolicyUsesStrictConjunction(t *testing.T) {
	g := NewWithT(t)
	windowA, windowB, windowC := uuid.New(), uuid.New(), uuid.New()
	freezeA, freezeB := uuid.New(), uuid.New()
	ownerVersion := validDeploymentPolicyVersion(uuid.New(), uuid.New(), windowA, freezeA)
	ownerVersion.Document.AdmissionRules.MaintenanceWindowVersionIDs = []uuid.UUID{windowA, windowB}
	ownerVersion.Document.AdmissionRules.AllowedResolutionModes = []types.RequirementResolutionMode{
		types.RequirementResolutionModeIncluded,
		types.RequirementResolutionModeSharedProvider,
		types.RequirementResolutionModeApprovedExternal,
	}
	ownerVersion.Document.AdmissionRules.MinimumBakeSeconds = 60
	ownerVersion.Document.AdmissionRules.MaximumWaitSeconds = 300
	ownerVersion.Document.CampaignRules.MinimumWaveBakeSeconds = 120
	ownerVersion.Document.CampaignRules.MaximumWaveSize = 20
	ownerVersion.Document.CampaignRules.MaximumConcurrency = 4
	ownerVersion.Document.CampaignRules.FailureToleranceBasisPoints = 1000
	ownerVersion.Document.CampaignRules.MinimumHealthyBasisPoints = 8000

	subscriberID := uuid.New()
	subscriberVersion := validDeploymentPolicyVersion(uuid.New(), uuid.New(), windowB, freezeB)
	subscriberVersion.Document.AdmissionRules.MaintenanceWindowVersionIDs = []uuid.UUID{windowB, windowC}
	subscriberVersion.Document.AdmissionRules.AllowedResolutionModes = []types.RequirementResolutionMode{
		types.RequirementResolutionModeIncluded,
		types.RequirementResolutionModeSharedProvider,
	}
	subscriberVersion.Document.AdmissionRules.MinimumBakeSeconds = 180
	subscriberVersion.Document.AdmissionRules.MaximumWaitSeconds = 900
	subscriberVersion.Document.CampaignRules.MinimumWaveBakeSeconds = 300
	subscriberVersion.Document.CampaignRules.MaximumWaveSize = 5
	subscriberVersion.Document.CampaignRules.MaximumConcurrency = 2
	subscriberVersion.Document.CampaignRules.FailureToleranceBasisPoints = 250
	subscriberVersion.Document.CampaignRules.MinimumHealthyBasisPoints = 9500
	subscriberVersion.Document.RequiredEvidence = append(
		subscriberVersion.Document.RequiredEvidence,
		"backup",
	)

	effective, issues := ComposeEffectivePolicy(
		types.PolicySet{
			AuthorityKind: types.PolicyAuthorityOwner,
			AuthorityID:   uuid.New(),
			Versions:      []types.DeploymentPolicyVersion{ownerVersion},
		},
		[]types.PolicySet{{
			AuthorityKind: types.PolicyAuthoritySubscriber,
			AuthorityID:   subscriberID,
			Versions:      []types.DeploymentPolicyVersion{subscriberVersion},
		}},
	)

	g.Expect(issues).To(BeEmpty())
	g.Expect(effective.Checksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
	g.Expect(effective.SubscriberSetChecksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
	g.Expect(effective.ApprovalRules).To(HaveLen(2))
	g.Expect(effective.ApprovalRules[0].AuthorityID).NotTo(Equal(effective.ApprovalRules[1].AuthorityID))
	g.Expect(effective.AdmissionRules.AllowedResolutionModes).To(Equal([]types.RequirementResolutionMode{
		types.RequirementResolutionModeIncluded,
		types.RequirementResolutionModeSharedProvider,
	}))
	g.Expect(effective.AdmissionRules.MinimumBakeSeconds).To(Equal(180))
	g.Expect(effective.AdmissionRules.MaximumWaitSeconds).To(Equal(900))
	g.Expect(effective.AdmissionRules.MaintenanceWindowVersionIDs).To(Equal([]uuid.UUID{windowB}))
	g.Expect(effective.AdmissionRules.FreezeRuleVersionIDs).To(ConsistOf(freezeA, freezeB))
	g.Expect(effective.CampaignRules.MinimumWaveBakeSeconds).To(Equal(300))
	g.Expect(effective.CampaignRules.MaximumWaveSize).To(Equal(5))
	g.Expect(effective.CampaignRules.MaximumConcurrency).To(Equal(2))
	g.Expect(effective.CampaignRules.FailureToleranceBasisPoints).To(Equal(250))
	g.Expect(effective.CampaignRules.MinimumHealthyBasisPoints).To(Equal(9500))
	g.Expect(effective.RequiredEvidence).To(ConsistOf("backup", "provenance", "sbom"))
}

func TestComposeEffectivePolicyBlocksConflictingModesAndWindows(t *testing.T) {
	g := NewWithT(t)
	owner := validDeploymentPolicyVersion(uuid.New(), uuid.New(), uuid.New(), uuid.New())
	subscriber := validDeploymentPolicyVersion(uuid.New(), uuid.New(), uuid.New(), uuid.New())
	owner.Document.AdmissionRules.AllowedResolutionModes = []types.RequirementResolutionMode{
		types.RequirementResolutionModeIncluded,
	}
	subscriber.Document.AdmissionRules.AllowedResolutionModes = []types.RequirementResolutionMode{
		types.RequirementResolutionModeFeatureDisabled,
	}

	_, issues := ComposeEffectivePolicy(
		types.PolicySet{
			AuthorityKind: types.PolicyAuthorityOwner,
			AuthorityID:   uuid.New(),
			Versions:      []types.DeploymentPolicyVersion{owner},
		},
		[]types.PolicySet{{
			AuthorityKind: types.PolicyAuthoritySubscriber,
			AuthorityID:   uuid.New(),
			Versions:      []types.DeploymentPolicyVersion{subscriber},
		}},
	)

	g.Expect(issues).To(ContainElements(
		MatchFields(IgnoreExtras, Fields{"Code": Equal("policy.resolution_mode.no_common")}),
		MatchFields(IgnoreExtras, Fields{"Code": Equal("policy.window.no_common")}),
	))
}

func TestComposeEffectivePolicyIsDeterministicAcrossInputOrdering(t *testing.T) {
	g := NewWithT(t)
	window := uuid.New()
	ownerA := validDeploymentPolicyVersion(uuid.New(), uuid.New(), window, uuid.New())
	ownerB := validDeploymentPolicyVersion(uuid.New(), uuid.New(), window, uuid.New())
	ownerA.Document.RequiredEvidence = []string{"sbom", "provenance"}
	ownerB.Document.RequiredEvidence = []string{"provenance", "sbom"}
	subscriberA := types.PolicySet{
		AuthorityKind: types.PolicyAuthoritySubscriber,
		AuthorityID:   uuid.New(),
		Versions: []types.DeploymentPolicyVersion{
			validDeploymentPolicyVersion(uuid.New(), uuid.New(), window, uuid.New()),
		},
	}
	subscriberB := types.PolicySet{
		AuthorityKind: types.PolicyAuthoritySubscriber,
		AuthorityID:   uuid.New(),
		Versions: []types.DeploymentPolicyVersion{
			validDeploymentPolicyVersion(uuid.New(), uuid.New(), window, uuid.New()),
		},
	}
	ownerID := uuid.New()

	first, firstIssues := ComposeEffectivePolicy(
		types.PolicySet{
			AuthorityKind: types.PolicyAuthorityOwner,
			AuthorityID:   ownerID,
			Versions:      []types.DeploymentPolicyVersion{ownerA, ownerB},
		},
		[]types.PolicySet{subscriberA, subscriberB},
	)
	secondOwnerVersions := []types.DeploymentPolicyVersion{ownerA, ownerB}
	slices.Reverse(secondOwnerVersions)
	secondSubscribers := []types.PolicySet{subscriberA, subscriberB}
	slices.Reverse(secondSubscribers)
	second, secondIssues := ComposeEffectivePolicy(
		types.PolicySet{
			AuthorityKind: types.PolicyAuthorityOwner,
			AuthorityID:   ownerID,
			Versions:      secondOwnerVersions,
		},
		secondSubscribers,
	)

	g.Expect(firstIssues).To(BeEmpty())
	g.Expect(secondIssues).To(BeEmpty())
	g.Expect(second).To(Equal(first))
}

func TestComposeEffectivePolicySubscriberMembershipChangesChecksum(t *testing.T) {
	g := NewWithT(t)
	window := uuid.New()
	version := validDeploymentPolicyVersion(uuid.New(), uuid.New(), window, uuid.New())
	ownerID := uuid.New()
	firstSubscriber := uuid.New()
	secondSubscriber := uuid.New()

	first, firstIssues := ComposeEffectivePolicy(
		types.PolicySet{
			AuthorityKind: types.PolicyAuthorityOwner,
			AuthorityID:   ownerID,
			Versions:      []types.DeploymentPolicyVersion{version},
		},
		[]types.PolicySet{{
			AuthorityKind: types.PolicyAuthoritySubscriber,
			AuthorityID:   firstSubscriber,
			Versions:      []types.DeploymentPolicyVersion{version},
		}},
	)
	second, secondIssues := ComposeEffectivePolicy(
		types.PolicySet{
			AuthorityKind: types.PolicyAuthorityOwner,
			AuthorityID:   ownerID,
			Versions:      []types.DeploymentPolicyVersion{version},
		},
		[]types.PolicySet{{
			AuthorityKind: types.PolicyAuthoritySubscriber,
			AuthorityID:   secondSubscriber,
			Versions:      []types.DeploymentPolicyVersion{version},
		}},
	)

	g.Expect(firstIssues).To(BeEmpty())
	g.Expect(secondIssues).To(BeEmpty())
	g.Expect(second.SubscriberSetChecksum).NotTo(Equal(first.SubscriberSetChecksum))
	g.Expect(second.Checksum).NotTo(Equal(first.Checksum))
}

func validDeploymentPolicyVersion(
	versionID uuid.UUID,
	approverGroupID uuid.UUID,
	windowVersionID uuid.UUID,
	freezeVersionID uuid.UUID,
) types.DeploymentPolicyVersion {
	overrideGroupID := uuid.New()
	return types.DeploymentPolicyVersion{
		ID:             versionID,
		OrganizationID: uuid.New(),
		PolicyID:       uuid.New(),
		VersionNumber:  1,
		State:          types.DeploymentPolicyVersionStatePublished,
		CanonicalChecksum: "sha256:" +
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Document: types.DeploymentPolicyDocument{
			Schema: types.DeploymentPolicySchemaV1,
			ApprovalRules: []types.ApprovalRule{{
				Key:              "four-eyes",
				PrincipalGroupID: approverGroupID,
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
					types.RequirementResolutionModeSharedProvider,
					types.RequirementResolutionModeApprovedExternal,
					types.RequirementResolutionModeFeatureDisabled,
				},
				MinimumBakeSeconds:          60,
				MaximumWaitSeconds:          300,
				MaintenanceWindowVersionIDs: []uuid.UUID{windowVersionID},
				FreezeRuleVersionIDs:        []uuid.UUID{freezeVersionID},
			},
			CampaignRules: types.CampaignRules{
				MinimumWaveBakeSeconds:      120,
				MaximumWaveSize:             10,
				MaximumConcurrency:          3,
				FailureToleranceBasisPoints: 1000,
				MinimumHealthyBasisPoints:   9000,
			},
			OverrideRules: types.OverrideRules{
				Allowed:             true,
				AuthorityGroupID:    &overrideGroupID,
				ShortenableGateKeys: []string{"maintenance-wait"},
				MinimumReasonLength: 20,
			},
			RequiredEvidence: []string{"provenance", "sbom"},
			BootstrapRules: types.BootstrapRules{
				Mode:             types.BootstrapModeRequireApproval,
				ApprovalRuleKeys: []string{"four-eyes"},
				RequiredGateKeys: []string{"artifact-integrity"},
			},
		},
	}
}
