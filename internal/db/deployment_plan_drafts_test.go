package db

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestTargetRequirementsFromGraphPreservesEverySymbolicTargetRequirement(t *testing.T) {
	g := NewWithT(t)
	graph := types.ProductReleaseGraph{Edges: []types.GraphEdge{
		{
			Key: "product", From: "component:provider", To: "component:consumer",
			Capability: "cache", ResolutionStage: types.CapabilityResolutionStageProduct,
		},
		{
			Key: "target-b", From: "target:consumer:email", To: "component:consumer",
			Capability: "email", VersionRange: "^2.0.0",
			ResolutionStage: types.CapabilityResolutionStageTarget,
			AllowedModes: []types.RequirementResolutionMode{
				types.RequirementResolutionModeApprovedExternal,
			},
		},
		{
			Key: "target-a", From: "target:consumer:cache", To: "component:consumer",
			Capability: "cache", VersionRange: "^1.0.0",
			ResolutionStage: types.CapabilityResolutionStageTarget,
			AllowedModes: []types.RequirementResolutionMode{
				types.RequirementResolutionModeIncluded,
			},
		},
	}}

	requirements := targetRequirementsFromGraph(graph)

	g.Expect(requirements).To(HaveLen(2))
	g.Expect(requirements[0].Key).To(Equal("target:consumer:cache"))
	g.Expect(requirements[1].Key).To(Equal("target:consumer:email"))
}

func TestProjectTargetPlanStepsFreezesDependencies(t *testing.T) {
	g := NewWithT(t)
	graph := types.TargetPlanGraph{
		Steps: []types.TargetPlanStep{
			{StepKey: "a", Name: "A", InputBindings: []byte(`{}`), V1Compatible: true},
			{StepKey: "b", Name: "B", InputBindings: []byte(`{}`), V1Compatible: true},
		},
		Edges: []types.DeploymentPlanStepEdge{
			{Key: "a->b", FromStepKey: "a", ToStepKey: "b"},
		},
	}

	steps := projectTargetPlanSteps(graph)

	g.Expect(steps).To(HaveLen(2))
	g.Expect(steps[1].Dependencies).To(Equal([]string{"a"}))
	g.Expect(steps[0].ID).NotTo(Equal(uuid.Nil))
}

func TestMigration145HasTenantFencesImmutabilityAndRollbackRefusal(t *testing.T) {
	g := NewWithT(t)
	root := filepath.Join("..", "migrations", "sql")
	up, err := os.ReadFile(filepath.Join(root, "145_target_deployment_plan_v2.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	down, err := os.ReadFile(filepath.Join(root, "145_target_deployment_plan_v2.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	upText := string(up)
	downText := string(down)

	for _, table := range []string{
		"CREATE TABLE DeploymentPlanDraft",
		"CREATE TABLE DeploymentPlanDraftAuditEvent",
		"CREATE TABLE DeploymentPlanResolvedRequirement",
		"CREATE TABLE DeploymentPlanStepEdge",
	} {
		g.Expect(upText).To(ContainSubstring(table))
	}
	for _, column := range []string{
		"plan_schema", "draft_id", "deployment_unit_id",
		"target_config_snapshot_id", "protocol_version",
		"supersedes_deployment_plan_id", "supersede_reason",
		"created_by_user_account_id", "updated_by_user_account_id",
		"published_by_user_account_id", "sealed_at",
	} {
		g.Expect(upText).To(ContainSubstring(column))
	}
	g.Expect(upText).To(ContainSubstring("FOREIGN KEY (draft_id, organization_id)"))
	g.Expect(upText).To(ContainSubstring("DeploymentPlanDraft_publication_guard"))
	g.Expect(upText).To(ContainSubstring("DeploymentPlan_v2_immutable_guard"))
	g.Expect(upText).To(ContainSubstring("DeploymentPlanResolvedRequirement_append_only"))
	g.Expect(upText).To(ContainSubstring("DeploymentPlanStepEdge_append_only"))
	g.Expect(upText).To(ContainSubstring("DeploymentPlanTarget_v2_seal_guard"))
	g.Expect(upText).To(ContainSubstring("DeploymentPlanStep_v2_seal_guard"))
	g.Expect(upText).To(ContainSubstring("DeploymentPlanIssue_v2_seal_guard"))
	g.Expect(upText).To(ContainSubstring("DeploymentPlanDraftAuditEvent_append_only"))
	g.Expect(upText).To(ContainSubstring("status = 'BLOCKED'"))
	g.Expect(upText).To(ContainSubstring("DeploymentPlan_v2_supersedes_unique"))
	g.Expect(upText).To(ContainSubstring("OLD.deployment_plan_id"))
	g.Expect(upText).To(ContainSubstring("NEW.deployment_plan_id"))
	g.Expect(upText).To(ContainSubstring(
		"NEW.deployment_plan_id IS DISTINCT FROM OLD.deployment_plan_id",
	))
	g.Expect(downText).To(ContainSubstring("LOCK TABLE"))
	g.Expect(downText).To(ContainSubstring("ACCESS EXCLUSIVE MODE"))
	g.Expect(
		strings.Index(downText, "LOCK TABLE"),
	).To(BeNumerically("<", strings.Index(downText, "refusing migration 145 rollback")))
	g.Expect(downText).To(ContainSubstring("refusing migration 145 rollback"))
}

func TestDraftPublicationUsesRowLockAndExactOptimisticChecksum(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("deployment_plan_drafts.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)
	g.Expect(text).To(ContainSubstring("RunTxIso(ctx, pgx.Serializable"))
	g.Expect(text).To(ContainSubstring("FOR UPDATE"))
	g.Expect(text).To(ContainSubstring("draft.Revision != expectedRevision"))
	g.Expect(text).To(ContainSubstring("validation.PreviewChecksum != expectedPreviewChecksum"))
	g.Expect(text).To(ContainSubstring("sealPublishedTargetPlan"))
	g.Expect(text).To(ContainSubstring("lockAndValidateTargetPlanSupersession"))
	g.Expect(strings.Count(text, "organization_id = @organizationID")).To(
		BeNumerically(">=", 8),
	)
}

func TestTargetConfigVerificationUsesObservedObjectEvidence(t *testing.T) {
	g := NewWithT(t)
	expected := types.TargetPlanConfigObject{
		Key: "compose", Reference: "s3://config/_immutable/sha256/" +
			strings.Repeat("a", 64) + "/compose.yaml",
		VersionID: "version-1", MediaType: "application/yaml",
		SizeBytes: 42, Checksum: checksumForDraftTest("a"),
	}
	verifier := targetPlanConfigVerifierFunc(func(
		_ context.Context,
		object types.TargetPlanConfigObject,
	) (types.TargetPlanConfigObservation, error) {
		g.Expect(object).To(Equal(expected))
		return types.TargetPlanConfigObservation{
			Reference: object.Reference, VersionID: object.VersionID,
			MediaType: object.MediaType, SizeBytes: object.SizeBytes,
			Checksum: object.Checksum,
		}, nil
	})

	fact := verifyTargetPlanConfigObject(t.Context(), verifier, expected)

	g.Expect(fact.Verified).To(BeTrue())
	g.Expect(fact.ObservedReference).To(Equal(expected.Reference))
	g.Expect(fact.ObservedVersionID).To(Equal(expected.VersionID))
	g.Expect(fact.ObservedMediaType).To(Equal(expected.MediaType))
	g.Expect(fact.ObservedSizeBytes).To(Equal(expected.SizeBytes))
	g.Expect(fact.ObservedChecksum).To(Equal(expected.Checksum))
}

func TestTargetConfigVerificationFailsClosedWhenVerifierUnavailable(t *testing.T) {
	g := NewWithT(t)
	fact := verifyTargetPlanConfigObject(
		t.Context(),
		NewUnavailableTargetConfigObjectVerifier(),
		types.TargetPlanConfigObject{Key: "compose", Checksum: checksumForDraftTest("a")},
	)

	g.Expect(fact.Verified).To(BeFalse())
	g.Expect(fact.ObservedChecksum).To(BeEmpty())
	g.Expect(fact.VerificationCode).To(Equal("verification_unavailable"))
}

func TestTargetConfigVerificationRejectsEmptyObjectSet(t *testing.T) {
	g := NewWithT(t)

	facts, err := verifyTargetPlanConfigObjects(
		t.Context(),
		NewUnavailableTargetConfigObjectVerifier(),
		nil,
	)

	g.Expect(err).To(MatchError(ContainSubstring("at least one object")))
	g.Expect(facts).To(BeNil())
}

func TestTargetConfigVerificationRejectsOversizedSetBeforeVerifierCalls(t *testing.T) {
	g := NewWithT(t)
	calls := 0
	verifier := targetPlanConfigVerifierFunc(func(
		_ context.Context,
		_ types.TargetPlanConfigObject,
	) (types.TargetPlanConfigObservation, error) {
		calls++
		return types.TargetPlanConfigObservation{}, nil
	})
	objects := make(
		[]types.TargetPlanConfigObject,
		maxTargetPlanConfigObjects+1,
	)

	facts, err := verifyTargetPlanConfigObjects(t.Context(), verifier, objects)

	g.Expect(err).To(MatchError(ContainSubstring("object limit")))
	g.Expect(facts).To(BeNil())
	g.Expect(calls).To(Equal(0))
}

func TestTargetPlanProviderBoundsRejectRowsAndCandidateCrossProduct(t *testing.T) {
	g := NewWithT(t)

	g.Expect(validateTargetPlanProviderRowCount(maxTargetPlanProviderRows + 1)).
		To(MatchError(ContainSubstring("provider row limit")))
	candidates := make(
		[]types.RequirementProviderCandidate,
		maxTargetPlanCandidates,
	)
	result, err := appendTargetPlanCandidate(
		candidates,
		types.RequirementProviderCandidate{},
	)
	g.Expect(err).To(MatchError(ContainSubstring("candidate limit")))
	g.Expect(result).To(BeNil())
}

func TestObservedProviderQueryAppliesDatabaseRowLimit(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("deployment_plan_drafts.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)

	g.Expect(text).To(ContainSubstring("LIMIT @providerRowLimit"))
	g.Expect(text).To(MatchRegexp(
		`"providerRowLimit"\s*:\s*maxTargetPlanProviderRows \+ 1`,
	))
}

type targetPlanConfigVerifierFunc func(
	context.Context,
	types.TargetPlanConfigObject,
) (types.TargetPlanConfigObservation, error)

func (fn targetPlanConfigVerifierFunc) VerifyTargetConfigObject(
	ctx context.Context,
	object types.TargetPlanConfigObject,
) (types.TargetPlanConfigObservation, error) {
	return fn(ctx, object)
}

func checksumForDraftTest(seed string) string {
	return "sha256:" + strings.Repeat(seed, 64)
}
