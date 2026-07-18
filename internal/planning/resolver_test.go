package planning

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestResolveTargetRequirementsAllModesDeterministically(t *testing.T) {
	g := NewWithT(t)
	draft := resolverFixture()

	resolutions, issues := ResolveTargetRequirements(context.Background(), draft)

	g.Expect(issues).To(BeEmpty())
	g.Expect(resolutions).To(HaveLen(5))
	g.Expect([]types.RequirementResolutionMode{
		resolutions[0].Mode,
		resolutions[1].Mode,
		resolutions[2].Mode,
		resolutions[3].Mode,
		resolutions[4].Mode,
	}).To(Equal([]types.RequirementResolutionMode{
		types.RequirementResolutionModePinnedExisting,
		types.RequirementResolutionModeApprovedExternal,
		types.RequirementResolutionModeSharedProvider,
		types.RequirementResolutionModeIncluded,
		types.RequirementResolutionModeFeatureDisabled,
	}))
	for _, resolution := range resolutions {
		g.Expect(resolution.BindingChecksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
	}

	reversed := draft
	reversed.ResolutionInput.Requirements = reverseRequirements(reversed.ResolutionInput.Requirements)
	reversed.ResolutionInput.Candidates = reverseCandidates(reversed.ResolutionInput.Candidates)
	second, secondIssues := ResolveTargetRequirements(context.Background(), reversed)
	g.Expect(secondIssues).To(BeEmpty())
	g.Expect(second).To(Equal(resolutions))
}

func TestResolveTargetRequirementsBlocksAmbiguousAndUnresolvedBindings(t *testing.T) {
	g := NewWithT(t)
	draft := resolverFixture()
	duplicate := draft.ResolutionInput.Candidates[0]
	duplicate.ObservationID = ptrUUID(uuid.New())
	draft.ResolutionInput.Candidates = append(draft.ResolutionInput.Candidates, duplicate)
	draft.ResolutionInput.Requirements = append(
		draft.ResolutionInput.Requirements,
		types.TargetRequirement{
			Key:          "target:consumer:missing",
			ConsumerKey:  "consumer",
			Capability:   "missing",
			VersionRange: "^1.0.0",
			AllowedModes: []types.RequirementResolutionMode{
				types.RequirementResolutionModePinnedExisting,
			},
		},
	)

	_, issues := ResolveTargetRequirements(context.Background(), draft)

	g.Expect(issueCodes(issues)).To(ConsistOf("ambiguous_requirement", "unresolved_requirement"))
}

func TestValidatePlanDraftRequiresOneExactActivePlacement(t *testing.T) {
	g := NewWithT(t)
	draft := resolverFixture()
	second := draft.ResolutionInput.Assignment
	second.ID = uuid.New()
	draft.ResolutionInput.ActiveAssignments = append(
		draft.ResolutionInput.ActiveAssignments,
		second,
	)

	issues := ValidatePlanDraft(context.Background(), draft)

	g.Expect(issueCodes(issues)).To(ContainElement("ambiguous_environment_assignment"))
}

func TestValidatePlanDraftBlocksForeignConfigPlatformProvenanceAndStaleState(t *testing.T) {
	tests := []struct {
		name string
		edit func(*types.PlanDraft)
		code string
	}{
		{
			name: "foreign config",
			edit: func(draft *types.PlanDraft) {
				draft.ResolutionInput.Config.OrganizationID = uuid.New()
			},
			code: "foreign_target_config",
		},
		{
			name: "config unit mismatch",
			edit: func(draft *types.PlanDraft) {
				draft.ResolutionInput.Config.DeploymentUnitID = uuid.New()
			},
			code: "target_config_unit_mismatch",
		},
		{
			name: "observe-only deployment unit",
			edit: func(draft *types.PlanDraft) {
				draft.ResolutionInput.Unit.ManagementState =
					types.RegistryManagementStateObserveOnly
			},
			code: "deployment_unit_not_plannable",
		},
		{
			name: "platform mismatch",
			edit: func(draft *types.PlanDraft) {
				draft.ResolutionInput.Config.TargetPlatform = "linux/arm64"
			},
			code: "target_platform_mismatch",
		},
		{
			name: "deployment target platform mismatch",
			edit: func(draft *types.PlanDraft) {
				draft.ResolutionInput.TargetPlatform =
					types.DeploymentTargetPlatformLinuxARM64
			},
			code: "deployment_target_platform_mismatch",
		},
		{
			name: "provenance mismatch",
			edit: func(draft *types.PlanDraft) {
				draft.ResolutionInput.ReleasePins[0].ProvenanceVerified = false
			},
			code: "component_provenance_unverified",
		},
		{
			name: "stale expected state",
			edit: func(draft *types.PlanDraft) {
				draft.ResolutionInput.Candidates[0].ObservedStateVersion++
			},
			code: "stale_expected_state",
		},
		{
			name: "unverified config object",
			edit: func(draft *types.PlanDraft) {
				draft.ResolutionInput.Config.VerificationFacts[0].Verified = false
			},
			code: "target_config_unverified",
		},
		{
			name: "ambiguous component instance",
			edit: func(draft *types.PlanDraft) {
				duplicate := draft.ResolutionInput.Config.ComponentBindings[0]
				duplicate.ComponentInstanceID = uuid.New()
				draft.ResolutionInput.Config.ComponentBindings = append(
					draft.ResolutionInput.Config.ComponentBindings,
					duplicate,
				)
			},
			code: "ambiguous_component_instance",
		},
		{
			name: "observe-only product component",
			edit: func(draft *types.PlanDraft) {
				draft.ResolutionInput.ComponentInstances[0].ManagementState =
					types.RegistryManagementStateObserveOnly
			},
			code: "component_instance_not_deployable",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			draft := resolverFixture()
			tc.edit(&draft)

			issues := ValidatePlanDraft(context.Background(), draft)

			g.Expect(issueCodes(issues)).To(ContainElement(tc.code))
		})
	}
}

func TestValidatePlanDraftRejectsV1IncompatibleResolution(t *testing.T) {
	g := NewWithT(t)
	draft := resolverFixture()
	draft.ProtocolVersion = types.DeploymentPlanProtocolV1
	draft.ResolutionInput.Candidates[0].V1Compatible = false

	issues := ValidatePlanDraft(context.Background(), draft)

	g.Expect(issueCodes(issues)).To(ContainElement("protocol_v1_incompatible"))
}

func TestValidatePlanDraftAcceptsSelectedPlatformInMultiPlatformSet(t *testing.T) {
	g := NewWithT(t)
	draft := resolverFixture()
	draft.ResolutionInput.RequiredPlatforms = []string{
		"linux/arm64",
		"linux/amd64",
	}

	issues := ValidatePlanDraft(context.Background(), draft)

	g.Expect(issueCodes(issues)).NotTo(ContainElement("target_platform_mismatch"))
}

func TestValidatePlanDraftRejectsPlanSizeBeforeGraphConstruction(t *testing.T) {
	g := NewWithT(t)
	draft := resolverFixture()
	draft.ResolutionInput.Requirements = make(
		[]types.TargetRequirement,
		MaxTargetPlanRequirements+1,
	)

	issues := ValidatePlanDraft(context.Background(), draft)

	g.Expect(issueCodes(issues)).To(ContainElement("plan_requirements_limit_exceeded"))
}

func TestResolveTargetRequirementsRequiresFrozenDisabledFeatureFlag(t *testing.T) {
	g := NewWithT(t)
	draft := resolverFixture()
	delete(draft.ResolutionInput.Config.FeatureFlags, "promo")

	_, issues := ResolveTargetRequirements(context.Background(), draft)

	g.Expect(issueCodes(issues)).To(ContainElement("invalid_requirement_binding"))
}

func resolverFixture() types.PlanDraft {
	organizationID := uuid.MustParse("10000000-0000-0000-0000-000000000001")
	productReleaseID := uuid.MustParse("10000000-0000-0000-0000-000000000002")
	unitID := uuid.MustParse("10000000-0000-0000-0000-000000000003")
	assignmentID := uuid.MustParse("10000000-0000-0000-0000-000000000004")
	environmentID := uuid.MustParse("10000000-0000-0000-0000-000000000005")
	configID := uuid.MustParse("10000000-0000-0000-0000-000000000006")
	componentInstanceID := uuid.MustParse("10000000-0000-0000-0000-000000000007")
	componentReleaseID := uuid.MustParse("10000000-0000-0000-0000-000000000008")
	sharedUnitID := uuid.MustParse("10000000-0000-0000-0000-000000000009")
	now := time.Date(2026, 7, 18, 3, 0, 0, 0, time.UTC)
	provenanceFacts := []types.ComponentProvenanceFact{{
		VerificationID: uuid.MustParse("10000000-0000-0000-0000-000000000010"),
		ArtifactKey:    "consumer-image",
		Platform:       "linux/amd64",
		ArtifactDigest: checksum("f"),
		EvidenceDigest: checksum("a"),
		PolicyChecksum: checksum("b"),
		TrustRootID:    "10000000-0000-0000-0000-000000000011",
	}}
	provenanceBindingChecksum, err := canonicalChecksum(provenanceFacts)
	if err != nil {
		panic(err)
	}
	requirements := []types.TargetRequirement{
		targetRequirement("target:consumer:cache", "cache", types.RequirementResolutionModePinnedExisting),
		targetRequirement("target:consumer:email", "email", types.RequirementResolutionModeApprovedExternal),
		targetRequirement("target:consumer:fraud", "fraud", types.RequirementResolutionModeSharedProvider),
		targetRequirement("target:consumer:metrics", "metrics", types.RequirementResolutionModeIncluded),
		targetRequirement("target:consumer:promo", "promo", types.RequirementResolutionModeFeatureDisabled),
	}
	candidates := []types.RequirementProviderCandidate{
		{
			RequirementKey:            "target:consumer:cache",
			Mode:                      types.RequirementResolutionModePinnedExisting,
			ProviderReleaseID:         ptrUUID(componentReleaseID),
			ObservationID:             ptrUUID(uuid.MustParse("20000000-0000-0000-0000-000000000001")),
			ProviderVersion:           "1.4.0",
			ProviderPlatform:          "linux/amd64",
			DeploymentUnitID:          unitID,
			ComponentInstanceID:       ptrUUID(componentInstanceID),
			ObservedStateVersion:      7,
			ExpectedStateVersion:      7,
			ObservedStateChecksum:     checksum("1"),
			ExpectedStateChecksum:     checksum("1"),
			ProviderReleaseChecksum:   checksum("7"),
			ProvenanceBindingChecksum: checksum("8"),
			ProvenanceVerified:        true,
			V1Compatible:              true,
		},
		{
			RequirementKey:        "target:consumer:email",
			Mode:                  types.RequirementResolutionModeApprovedExternal,
			ObservationID:         ptrUUID(uuid.MustParse("20000000-0000-0000-0000-000000000002")),
			ProviderVersion:       "1.2.0",
			ProviderPlatform:      "linux/amd64",
			ExpectedStateChecksum: checksum("2"),
			ObservedStateChecksum: checksum("2"),
			ExpectedStateVersion:  1,
			ObservedStateVersion:  1,
			ProvenanceVerified:    true,
			V1Compatible:          true,
		},
		{
			RequirementKey:            "target:consumer:fraud",
			Mode:                      types.RequirementResolutionModeSharedProvider,
			ProviderReleaseID:         ptrUUID(uuid.MustParse("20000000-0000-0000-0000-000000000003")),
			ObservationID:             ptrUUID(uuid.MustParse("20000000-0000-0000-0000-000000000004")),
			ProviderVersion:           "1.5.0",
			ProviderPlatform:          "linux/amd64",
			DeploymentUnitID:          sharedUnitID,
			SubscriberSetChecksum:     checksum("3"),
			ExpectedStateChecksum:     checksum("4"),
			ObservedStateChecksum:     checksum("4"),
			ExpectedStateVersion:      3,
			ObservedStateVersion:      3,
			ProviderReleaseChecksum:   checksum("7"),
			ProvenanceBindingChecksum: checksum("8"),
			ProvenanceVerified:        true,
			V1Compatible:              true,
		},
		{
			RequirementKey:            "target:consumer:metrics",
			Mode:                      types.RequirementResolutionModeIncluded,
			ProviderReleaseID:         ptrUUID(componentReleaseID),
			ProviderVersion:           "1.3.0",
			ProviderPlatform:          "linux/amd64",
			DeploymentUnitID:          unitID,
			ComponentInstanceID:       ptrUUID(componentInstanceID),
			ExpectedStateChecksum:     checksum("5"),
			ObservedStateChecksum:     checksum("5"),
			ExpectedStateVersion:      0,
			ObservedStateVersion:      0,
			ProviderReleaseChecksum:   checksum("e"),
			ProvenanceBindingChecksum: provenanceBindingChecksum,
			ProvenanceVerified:        true,
			V1Compatible:              true,
		},
		{
			RequirementKey:        "target:consumer:promo",
			Mode:                  types.RequirementResolutionModeFeatureDisabled,
			ProviderVersion:       "1.0.0",
			ProviderPlatform:      "linux/amd64",
			ExpectedStateChecksum: checksum("6"),
			ObservedStateChecksum: checksum("6"),
			ExpectedStateVersion:  0,
			ObservedStateVersion:  0,
			FeatureFlagKey:        "promo",
			FeatureFlagEnabled:    false,
			ProvenanceVerified:    true,
			V1Compatible:          true,
		},
	}
	assignment := types.TargetEnvironmentAssignment{
		ID:                 assignmentID,
		OrganizationID:     organizationID,
		DeploymentTargetID: uuid.MustParse("30000000-0000-0000-0000-000000000001"),
		EnvironmentID:      environmentID,
		ActiveFrom:         now.Add(-time.Hour),
	}
	unit := types.DeploymentUnit{
		ID:                            unitID,
		OrganizationID:                organizationID,
		TargetEnvironmentAssignmentID: assignmentID,
		DeploymentTargetID:            assignment.DeploymentTargetID,
		SubscriberSetChecksum:         checksum("a"),
		ManagementState:               types.RegistryManagementStateManaged,
	}
	return types.PlanDraft{
		ID:                      uuid.MustParse("40000000-0000-0000-0000-000000000001"),
		OrganizationID:          organizationID,
		Revision:                1,
		ProductReleaseID:        productReleaseID,
		DeploymentUnitID:        unitID,
		EnvironmentAssignmentID: assignmentID,
		TargetConfigSnapshotID:  configID,
		ProtocolVersion:         types.DeploymentPlanProtocolV2,
		ExpectedPreviewChecksum: "",
		ResolutionInput: &types.PlanResolutionInput{
			EffectiveAt:       now,
			Assignment:        assignment,
			ActiveAssignments: []types.TargetEnvironmentAssignment{assignment},
			Unit:              unit,
			ActiveUnits:       []types.DeploymentUnit{unit},
			TargetPlatform:    types.DeploymentTargetPlatformLinuxAMD64,
			ProductReleaseID:  productReleaseID,
			ProductChecksum:   checksum("b"),
			ProductPublished:  true,
			RequiredPlatforms: []string{"linux/amd64"},
			Config: types.TargetConfigBinding{
				ID:                      configID,
				OrganizationID:          organizationID,
				DeploymentUnitID:        unitID,
				EnvironmentAssignmentID: assignmentID,
				EnvironmentID:           environmentID,
				CanonicalChecksum:       checksum("c"),
				TargetPlatform:          "linux/amd64",
				VerificationFacts: []types.ConfigVerificationFact{{
					ObjectKey: "compose", Checksum: checksum("d"), Verified: true,
				}},
				FeatureFlags: map[string]bool{"promo": false},
				ComponentBindings: []types.ConfigComponentBinding{{
					ComponentKey: "consumer", ComponentInstanceID: componentInstanceID,
					PhysicalName: "consumer",
				}},
			},
			Requirements: requirements,
			Candidates:   candidates,
			ReleasePins: []types.ComponentReleasePin{{
				ComponentKey:       "consumer",
				ComponentReleaseID: componentReleaseID,
				ReleaseChecksum:    checksum("e"),
				Platforms:          []string{"linux/amd64"},
				Artifacts: []types.PinnedReleaseArtifact{{
					Key: "consumer-image", Type: "oci-image",
					MediaType:      "application/vnd.oci.image.manifest.v1+json",
					ManifestDigest: checksum("d"), Platform: "linux/amd64",
					PlatformDigest: checksum("f"),
				}},
				ProvenanceVerified:        true,
				ProvenanceBindingChecksum: provenanceBindingChecksum,
				ProvenanceFacts:           provenanceFacts,
			}},
			ComponentInstances: []types.ComponentInstance{{
				ID: componentInstanceID, OrganizationID: organizationID,
				DeploymentUnitID: unitID, PhysicalName: "consumer",
				ManagementState: types.RegistryManagementStateManaged,
			}},
		},
	}
}

func targetRequirement(
	key string,
	capability string,
	mode types.RequirementResolutionMode,
) types.TargetRequirement {
	return types.TargetRequirement{
		Key:          key,
		ConsumerKey:  "consumer",
		Capability:   capability,
		VersionRange: "^1.0.0",
		AllowedModes: []types.RequirementResolutionMode{mode},
	}
}

func checksum(seed string) string {
	return "sha256:" + strings.Repeat(seed, 64)
}

func ptrUUID(value uuid.UUID) *uuid.UUID {
	return &value
}

func issueCodes(issues []types.ValidationIssue) []string {
	result := make([]string, 0, len(issues))
	for _, issue := range issues {
		result = append(result, issue.Code)
	}
	return result
}

func reverseRequirements(values []types.TargetRequirement) []types.TargetRequirement {
	result := append([]types.TargetRequirement(nil), values...)
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}
	return result
}

func reverseCandidates(
	values []types.RequirementProviderCandidate,
) []types.RequirementProviderCandidate {
	result := append([]types.RequirementProviderCandidate(nil), values...)
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}
	return result
}
