package db

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestMigration146CreatesTenantFencedAppendOnlyPlanEvidence(t *testing.T) {
	g := NewWithT(t)
	root := filepath.Join("..", "migrations", "sql")
	up, err := os.ReadFile(filepath.Join(root, "146_deployment_plan_baseline_changes.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	down, err := os.ReadFile(filepath.Join(root, "146_deployment_plan_baseline_changes.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	upText := string(up)
	downText := string(down)

	for _, table := range []string{
		"CREATE TABLE DeploymentPlanBaseline",
		"CREATE TABLE DeploymentPlanChangeEntry",
		"CREATE TABLE DeploymentPlanRiskEntry",
	} {
		g.Expect(upText).To(ContainSubstring(table))
	}
	g.Expect(strings.Count(upText, "organization_id UUID NOT NULL")).To(BeNumerically(">=", 3))
	g.Expect(strings.Count(upText, "actor_user_account_id UUID NOT NULL")).To(BeNumerically(">=", 3))
	g.Expect(upText).To(ContainSubstring("deployment_plan_change_evidence_append_only_guard"))
	g.Expect(upText).To(ContainSubstring("legacy_projection"))
	g.Expect(upText).To(ContainSubstring("authorizes_v2_execution"))
	g.Expect(upText).To(ContainSubstring("ADD COLUMN component_instance_id UUID"))
	g.Expect(upText).To(ContainSubstring("targetcomponentobservation_instance_fk"))
	g.Expect(downText).To(ContainSubstring("refusing migration 146 rollback"))
}

func TestDeploymentPlanChangeRepositoryUsesTenantScopeAndSerializableCAS(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("deployment_plan_changes.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)

	g.Expect(text).To(ContainSubstring("RunTxIso(ctx, pgx.Serializable"))
	g.Expect(text).To(ContainSubstring("FOR UPDATE"))
	g.Expect(text).To(ContainSubstring("expectedDesiredRevision"))
	g.Expect(text).To(ContainSubstring("expectedDesiredChecksum"))
	g.Expect(strings.Count(text, "organization_id = @organizationID")).To(BeNumerically(">=", 8))
	g.Expect(text).To(ContainSubstring("supersedes_deployment_plan_id"))
	g.Expect(text).To(ContainSubstring("successfulPlanID"))
}

func TestReleaseNoteQueryIsApplicationLineageScopedAndKeepsBounds(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("deployment_plan_changes.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)

	g.Expect(text).To(ContainSubstring("bundle.application_id = bounds.application_id"))
	g.Expect(text).To(ContainSubstring("FROM ProductReleaseComponent lineage"))
	g.Expect(text).To(ContainSubstring("lineage.component_release_bundle_id = bundle.id"))
	g.Expect(text).To(ContainSubstring("lineage_product.application_id = bounds.application_id"))
	g.Expect(text).To(ContainSubstring("lineage_product.status = 'PUBLISHED'"))
	g.Expect(text).To(ContainSubstring("recent_rank <= 129"))
	g.Expect(text).To(ContainSubstring("id = baseline_release_id"))
	g.Expect(text).To(ContainSubstring("LIMIT 130"))
}

func TestSuccessfulObservationCoverageRequiresEachComponentInstance(t *testing.T) {
	g := NewWithT(t)
	releaseID := uuid.New()
	firstInstanceID := uuid.New()
	secondInstanceID := uuid.New()
	digest := testDBChecksum("a")
	configChecksum := testDBChecksum("b")
	planned := []types.PlannedState{
		{
			ComponentInstanceID: firstInstanceID,
			ReleaseBundleID:     releaseID,
			Image:               digest,
			Platform:            "linux/amd64",
			ConfigChecksum:      configChecksum,
		},
		{
			ComponentInstanceID: secondInstanceID,
			ReleaseBundleID:     releaseID,
			Image:               digest,
			Platform:            "linux/amd64",
			ConfigChecksum:      configChecksum,
		},
	}
	observed := []successfulComponentObservation{{
		ComponentInstanceID: firstInstanceID,
		ReleaseBundleID:     releaseID,
		Image:               "registry.example/api@" + digest,
		Platform:            "linux/amd64",
		ConfigChecksum:      configChecksum,
	}}

	missing := missingSuccessfulObservationCoverage(planned, observed)

	g.Expect(missing).To(Equal([]componentObservationKey{{
		ComponentInstanceID: secondInstanceID,
		ReleaseBundleID:     releaseID,
	}}))
}

func TestObservationWritePersistsPlanComponentInstanceIdentity(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("external_executions.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)

	g.Expect(text).To(ContainSubstring("component_instance_id"))
	g.Expect(text).To(ContainSubstring("FROM DeploymentPlanBaseline baseline"))
	g.Expect(text).To(ContainSubstring("baseline.deployment_plan_id = @deploymentPlanId"))
	g.Expect(text).To(ContainSubstring("baseline.component_key = @component"))
	g.Expect(text).To(ContainSubstring("instance.physical_name = @component"))
}

func TestValidatePreviousStatePlanPairRejectsCrossTenant(t *testing.T) {
	g := NewWithT(t)
	deploymentUnitID := uuid.New()
	configID := uuid.New()
	draftID := uuid.New()
	current := &types.DeploymentPlan{
		OrganizationID:   uuid.New(),
		PlanSchema:       types.TargetDeploymentPlanSchemaV2,
		ApplicationID:    uuid.New(),
		EnvironmentID:    uuid.New(),
		DeploymentUnitID: &deploymentUnitID,
	}
	successful := &types.DeploymentPlan{
		OrganizationID:         uuid.New(),
		PlanSchema:             types.TargetDeploymentPlanSchemaV2,
		ApplicationID:          current.ApplicationID,
		EnvironmentID:          current.EnvironmentID,
		DeploymentUnitID:       &deploymentUnitID,
		TargetConfigSnapshotID: &configID,
		DraftID:                &draftID,
		Status:                 types.DeploymentPlanStatusExecuted,
	}

	err := validatePreviousStatePlanPair(current, successful)

	g.Expect(err).To(MatchError(ContainSubstring("same organization")))
}

func TestAddPreviousStateEvidenceCreatesNewBToALineage(t *testing.T) {
	g := NewWithT(t)
	currentPlanID := uuid.New()
	successfulPlanID := uuid.New()
	input := &types.PlanResolutionInput{}
	draft := &types.PlanDraft{
		ID:              uuid.New(),
		OrganizationID:  uuid.New(),
		ResolutionInput: input,
	}
	validation := &types.PlanDraftValidation{
		Draft: *draft,
		StepAdapters: []types.ResolvedPlanStepAdapter{{
			StepKey: "component.deploy",
		}},
	}

	err := addPreviousStateEvidence(
		draft,
		validation,
		currentPlanID,
		successfulPlanID,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(draft.PreviousStateSourcePlanID).To(Equal(&successfulPlanID))
	g.Expect(validation.Draft.PreviousStateSourcePlanID).To(Equal(&successfulPlanID))
	g.Expect(validation.Changes).To(ContainElement(And(
		HaveField("Kind", types.DeploymentPlanChangePreviousState),
		HaveField("Before", currentPlanID.String()),
		HaveField("After", successfulPlanID.String()),
	)))
	g.Expect(validation.PreviewChecksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
	var canonical types.TargetDeploymentPlanCanonical
	g.Expect(json.Unmarshal(validation.Draft.PreviewPayload, &canonical)).To(Succeed())
	g.Expect(canonical.StepAdapters).To(Equal(validation.StepAdapters))
}

func TestPreviousStateGuardsRejectStaleAndForwardOnlyPlans(t *testing.T) {
	g := NewWithT(t)

	g.Expect(rejectStaleCurrentPlan(false)).To(Succeed())
	g.Expect(rejectStaleCurrentPlan(true)).To(MatchError(ContainSubstring("stale")))
	g.Expect(rejectForwardOnlyPreviousState(false)).To(Succeed())
	g.Expect(rejectForwardOnlyPreviousState(true)).
		To(MatchError(ContainSubstring("forward-only")))
}

func TestExistingPreviousStatePlanShortCircuitsIdempotentRetry(t *testing.T) {
	g := NewWithT(t)
	existing := &types.DeploymentPlan{ID: uuid.New()}

	plan, found, err := resolveExistingPreviousStatePlan(existing, nil)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(plan).To(BeIdenticalTo(existing))

	plan, found, err = resolveExistingPreviousStatePlan(nil, apierrors.ErrNotFound)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeFalse())
	g.Expect(plan).To(BeNil())
}

func testDBChecksum(char string) string {
	return "sha256:" + strings.Repeat(char, 64)
}
