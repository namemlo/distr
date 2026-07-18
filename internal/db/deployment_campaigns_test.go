package db

import (
	"errors"
	"github.com/distr-sh/distr/internal/campaigns"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	. "github.com/onsi/gomega"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCampaignRevisionMigrationFreezesMembershipAndPrerequisites(t *testing.T) {
	g := NewWithT(t)
	_, filename, _, ok := runtime.Caller(0)
	g.Expect(ok).To(BeTrue())
	path := filepath.Join(
		filepath.Dir(filename),
		"..",
		"migrations",
		"sql",
		"153_deployment_campaign_revisions.up.sql",
	)
	content, err := os.ReadFile(path)
	g.Expect(err).NotTo(HaveOccurred())
	sql := string(content)

	g.Expect(sql).To(ContainSubstring("CREATE TABLE DeploymentCampaignDraft"))
	g.Expect(sql).To(ContainSubstring("CREATE TABLE DeploymentCampaignRevision"))
	g.Expect(sql).To(ContainSubstring("CREATE TABLE DeploymentCampaignWave"))
	g.Expect(sql).To(ContainSubstring("CREATE TABLE DeploymentCampaignMember"))
	g.Expect(sql).To(ContainSubstring("CREATE TABLE DeploymentCampaignPrerequisite"))
	g.Expect(sql).To(ContainSubstring("expected_runtime_state_checksum"))
	g.Expect(sql).To(ContainSubstring("effective_policy_checksum"))
	g.Expect(sql).To(ContainSubstring("approval_request_revision"))
	g.Expect(sql).To(ContainSubstring("calendar_version_ids"))
	g.Expect(sql).To(ContainSubstring("calendar_checksums"))
	g.Expect(sql).To(ContainSubstring("admission_evaluation_id"))
	g.Expect(sql).To(ContainSubstring("admission_checksum"))
	g.Expect(sql).To(ContainSubstring("provider_deployment_unit_id"))
	g.Expect(sql).To(ContainSubstring("provider_component_instance_id"))
	g.Expect(sql).To(ContainSubstring("deploymentcampaign_published_immutable"))
	g.Expect(sql).To(ContainSubstring("UNIQUE (campaign_revision_id, deployment_unit_id)"))
	g.Expect(sql).To(ContainSubstring("pg_trigger_depth() > 1"))
	g.Expect(sql).To(ContainSubstring(
		"WHERE id = OLD.organization_id",
	))
	g.Expect(sql).To(ContainSubstring("DeploymentCampaignRevision_no_truncate"))
	g.Expect(sql).To(ContainSubstring(
		"FOREIGN KEY (campaign_revision_id, organization_id)",
	))
	g.Expect(sql).To(MatchRegexp(
		`(?s)FOREIGN KEY \(\s*campaign_revision_id,\s*` +
			`downstream_plan_id,\s*organization_id\s*\)`,
	))
	g.Expect(sql).To(MatchRegexp(
		`(?s)FOREIGN KEY \(\s*campaign_revision_id,\s*` +
			`upstream_plan_id,\s*organization_id\s*\)`,
	))
	g.Expect(sql).To(ContainSubstring(
		"FOREIGN KEY (downstream_plan_id, organization_id)",
	))
	g.Expect(sql).To(ContainSubstring(
		"FOREIGN KEY (upstream_plan_id, organization_id)",
	))
}

func TestCampaignPrerequisiteMigrationFreezesCanonicalObservationCoordinates(
	t *testing.T,
) {
	g := NewWithT(t)
	sql := campaignMigrationSQL(t)

	g.Expect(sql).To(ContainSubstring(
		"FOREIGN KEY (provider_deployment_unit_id, organization_id)",
	))
	g.Expect(sql).To(ContainSubstring(
		"REFERENCES ComponentInstance(id, deployment_unit_id, organization_id)",
	))
}

func TestCampaignPlacementHydrationUsesImmutableSnapshotBridge(t *testing.T) {
	g := NewWithT(t)

	g.Expect(campaignPrerequisiteEvidenceQuery).To(ContainSubstring(
		"TargetConfigSnapshotComponent",
	))
	g.Expect(campaignPrerequisiteEvidenceQuery).To(ContainSubstring(
		"provider_component_instance_id",
	))
	g.Expect(campaignPrerequisiteEvidenceQuery).To(ContainSubstring(
		"provider_deployment_unit_id",
	))
	g.Expect(campaignPrerequisiteEvidenceQuery).NotTo(ContainSubstring(
		"component.expected_state_checksum",
	))
	g.Expect(campaignPrerequisiteEvidenceQuery).To(ContainSubstring(
		"component.config_checksum",
	))
	g.Expect(campaignPrerequisiteEvidenceQuery).To(ContainSubstring(
		"plan.canonical_payload",
	))
	g.Expect(campaignPrerequisiteEvidenceQuery).To(ContainSubstring(
		"platformDigest",
	))
}

func TestCampaignMemberMigrationBindsAllEvidenceToTenantPlanAndUnit(t *testing.T) {
	g := NewWithT(t)
	sql := campaignMigrationSQL(t)

	g.Expect(sql).To(MatchRegexp(
		`(?s)deploymentcampaignmember_admission_fk.*FOREIGN KEY \(\s*` +
			`admission_evaluation_id,\s*deployment_plan_id,\s*organization_id`,
	))
	g.Expect(sql).To(MatchRegexp(
		`(?s)deploymentcampaignmember_approval_fk.*FOREIGN KEY \(\s*` +
			`approval_request_id,\s*deployment_plan_id,\s*organization_id`,
	))
	g.Expect(sql).To(MatchRegexp(
		`(?s)deploymentcampaignmember_plan_fk.*FOREIGN KEY \(\s*` +
			`deployment_plan_id,\s*deployment_unit_id,\s*organization_id`,
	))
}

func TestCampaignMigrationBindsCanonicalChecksumToPayload(t *testing.T) {
	g := NewWithT(t)

	g.Expect(campaignMigrationSQL(t)).To(MatchRegexp(
		`(?s)canonical_checksum\s*=\s*'sha256:'\s*\|\|\s*` +
			`encode\(sha256\(canonical_payload\),\s*'hex'\)`,
	))
}

func TestCampaignPrerequisiteMigrationBindsStepAndPlacementToUpstreamPlan(
	t *testing.T,
) {
	g := NewWithT(t)
	sql := campaignMigrationSQL(t)

	g.Expect(sql).To(MatchRegexp(
		`(?s)deploymentcampaignprerequisite_upstream_step_fk.*` +
			`FOREIGN KEY \(\s*upstream_plan_id,\s*upstream_step_key,\s*organization_id`,
	))
	g.Expect(sql).To(MatchRegexp(
		`(?s)deploymentcampaignprerequisite_provider_placement_fk.*` +
			`FOREIGN KEY \(\s*provider_placement_id,\s*upstream_plan_id,\s*organization_id`,
	))
}

func TestCampaignMigrationCapsDraftAndPublishedConcurrency(t *testing.T) {
	g := NewWithT(t)
	sql := campaignMigrationSQL(t)

	g.Expect(strings.Count(
		sql,
		"(risk_policy ->> 'maximumConcurrency')::integer BETWEEN 1 AND 1000",
	)).To(Equal(2))
}

func TestCampaignPublicationConflictReplaysExistingRevision(t *testing.T) {
	for _, code := range []string{
		pgerrcode.UniqueViolation,
		pgerrcode.SerializationFailure,
	} {
		t.Run(code, func(t *testing.T) {
			g := NewWithT(t)
			expected := &types.CampaignRevision{ID: uuid.New()}
			lookupCalls := 0

			result, err := replayCampaignPublicationConflict(
				&pgconn.PgError{Code: code},
				func() (*types.CampaignRevision, error) {
					lookupCalls++
					return expected, nil
				},
			)

			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(result).To(BeIdenticalTo(expected))
			g.Expect(lookupCalls).To(Equal(1))
		})
	}
}

func TestCampaignPublicationNonConflictPreservesOriginalError(t *testing.T) {
	g := NewWithT(t)
	original := errors.New("write failed")

	result, err := replayCampaignPublicationConflict(
		original,
		func() (*types.CampaignRevision, error) {
			t.Fatal("lookup must not run")
			return nil, nil
		},
	)

	g.Expect(result).To(BeNil())
	g.Expect(err).To(MatchError(original))
}

func TestCampaignRetentionMarkerForgeryCannotDeletePublishedEvidence(t *testing.T) {
	g := NewWithT(t)
	sql := campaignMigrationSQL(t)

	g.Expect(sql).To(ContainSubstring(
		"distr.deployment_campaign_deletion_operation_id",
	))
	g.Expect(sql).To(ContainSubstring("pg_trigger_depth() > 1"))
	g.Expect(sql).To(ContainSubstring("BEFORE TRUNCATE"))
	g.Expect(sql).NotTo(ContainSubstring(
		"distr.deployment_registry_deletion_reason",
	))
}

func TestCampaignPrerequisiteMigrationRejectsCrossOrganizationReferences(
	t *testing.T,
) {
	g := NewWithT(t)
	sql := campaignMigrationSQL(t)

	for _, reference := range []string{
		"DeploymentCampaignRevision(id, organization_id)",
		"DeploymentPlan(id, organization_id)",
	} {
		g.Expect(sql).To(ContainSubstring(reference))
	}
	g.Expect(sql).To(MatchRegexp(
		`(?s)deploymentcampaignprerequisite_downstream_fk.*` +
			`campaign_revision_id,\s*downstream_plan_id,\s*organization_id`,
	))
	g.Expect(sql).To(MatchRegexp(
		`(?s)deploymentcampaignprerequisite_upstream_fk.*` +
			`campaign_revision_id,\s*upstream_plan_id,\s*organization_id`,
	))
}

func TestCampaignCandidateQueryFiltersMembershipBeforeBound(t *testing.T) {
	g := NewWithT(t)
	selection := strings.Index(campaignCandidateQuery, "plan.id = ANY($2::uuid[])")
	bound := strings.Index(campaignCandidateQuery, "LIMIT 1001")

	g.Expect(selection).To(BeNumerically(">=", 0))
	g.Expect(bound).To(BeNumerically(">", selection))
	g.Expect(campaignCandidateQuery).To(ContainSubstring("$3::text[] <@"))
}

func TestCampaignRevisionFromDraftIsDeterministic(t *testing.T) {
	g := NewWithT(t)
	draft := types.CampaignDraft{
		ID:             uuid.MustParse("20000000-0000-0000-0000-000000000001"),
		OrganizationID: uuid.MustParse("10000000-0000-0000-0000-000000000001"),
		Name:           "production rollout",
		Revision:       3,
		Membership: types.CampaignMembership{
			PlanIDs: []uuid.UUID{
				uuid.MustParse("30000000-0000-0000-0000-000000000001"),
			},
		},
		Waves: []types.CampaignWaveDraft{{
			Order: 1,
			Name:  "canary",
			PlanIDs: []uuid.UUID{
				uuid.MustParse("30000000-0000-0000-0000-000000000001"),
			},
			BakeSeconds:        3600,
			MaximumConcurrency: 1,
		}},
		RiskPolicy: types.CampaignRiskPolicy{
			MaximumConcurrency:          1,
			FailureToleranceBasisPoints: 0,
			MinimumHealthyBasisPoints:   10000,
		},
		CandidatePlans: []types.CampaignPlanCandidate{{
			PlanID:                  uuid.MustParse("30000000-0000-0000-0000-000000000001"),
			OrganizationID:          uuid.MustParse("10000000-0000-0000-0000-000000000001"),
			DeploymentUnitID:        uuid.MustParse("40000000-0000-0000-0000-000000000001"),
			PlanChecksum:            "sha256:" + campaignTestHex("1"),
			CurrentPlanChecksum:     "sha256:" + campaignTestHex("1"),
			ApprovalRequestID:       uuid.MustParse("50000000-0000-0000-0000-000000000001"),
			ApprovalChecksum:        "sha256:" + campaignTestHex("1"),
			ApprovalRequestRevision: 2,
			EffectivePolicyChecksum: "sha256:" + campaignTestHex("2"),
			CalendarVersionIDs: []uuid.UUID{
				uuid.MustParse("70000000-0000-0000-0000-000000000001"),
			},
			CalendarChecksums: []string{"sha256:" + campaignTestHex("3")},
			AdmissionEvaluationID: uuid.MustParse(
				"80000000-0000-0000-0000-000000000001",
			),
			AdmissionChecksum: "sha256:" + campaignTestHex("4"),
			Admitted:          true,
			Approved:          true,
		}},
	}
	members, err := campaigns.ResolveCampaignMembership(t.Context(), draft)
	g.Expect(err).NotTo(HaveOccurred())

	first, err := campaignRevisionFromDraft(draft, 1, uuid.New(), members)
	g.Expect(err).NotTo(HaveOccurred())
	second, err := campaignRevisionFromDraft(draft, 1, uuid.New(), members)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(second.CanonicalPayload).To(Equal(first.CanonicalPayload))
	g.Expect(second.CanonicalChecksum).To(Equal(first.CanonicalChecksum))
}

func TestCampaignRevisionFromDraftFreezesCanonicalProviderIdentity(t *testing.T) {
	g := NewWithT(t)
	upstreamPlanID := uuid.New()
	placementID := uuid.New()
	providerUnitID := uuid.New()
	componentInstanceID := uuid.New()
	draft := types.CampaignDraft{
		ID:             uuid.New(),
		OrganizationID: uuid.New(),
		Name:           "provider campaign",
		Revision:       1,
		Prerequisites: []types.CampaignPrerequisiteDraft{{
			DownstreamPlanID:             uuid.New(),
			UpstreamPlanID:               upstreamPlanID,
			UpstreamStepKey:              "database.migrate",
			ProviderPlacementID:          placementID,
			ExpectedRuntimeStateChecksum: "sha256:" + campaignTestHex("1"),
		}},
		CandidatePlans: []types.CampaignPlanCandidate{{
			PlanID: upstreamPlanID,
			ExpectedStepPlacementEvidence: map[types.CampaignStepPlacement]types.CampaignStepPlacementEvidence{
				{
					StepKey:     "database.migrate",
					PlacementID: placementID,
				}: {
					ExpectedRuntimeStateChecksum: "sha256:" + campaignTestHex("1"),
					ProviderDeploymentUnitID:     providerUnitID,
					ProviderComponentInstanceID:  componentInstanceID,
				},
			},
		}},
	}

	revision, err := campaignRevisionFromDraft(draft, 1, uuid.New(), nil)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(revision.Prerequisites).To(HaveLen(1))
	g.Expect(revision.Prerequisites[0].ProviderDeploymentUnitID).
		To(Equal(providerUnitID))
	g.Expect(revision.Prerequisites[0].ProviderComponentInstanceID).
		To(Equal(componentInstanceID))
}

func campaignMigrationSQL(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test file")
	}
	content, err := os.ReadFile(filepath.Join(
		filepath.Dir(filename),
		"..",
		"migrations",
		"sql",
		"153_deployment_campaign_revisions.up.sql",
	))
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func campaignTestHex(value string) string {
	result := ""
	for range 64 {
		result += value
	}
	return result
}

func TestCampaignRunMigrationPersistsFencingAndExactPrerequisiteEvidence(t *testing.T) {
	g := NewWithT(t)
	sql, err := os.ReadFile("../migrations/sql/154_deployment_campaign_runs.up.sql")
	g.Expect(err).NotTo(HaveOccurred())
	contents := string(sql)

	for _, required := range []string{
		"DeploymentCampaignRun",
		"DeploymentCampaignWaveRun",
		"DeploymentCampaignMemberRun",
		"CampaignPrerequisiteEvaluation",
		"CampaignThresholdEvaluation",
		"fencing_token",
		"expected_checksum",
		"actual_observation_id",
		"actual_checksum",
		"UNIQUE (campaign_run_id, wave_order, member_order, deployment_plan_id)",
	} {
		g.Expect(contents).To(ContainSubstring(required))
	}
	g.Expect(strings.Count(contents, "CREATE TABLE")).To(Equal(5))
}

func TestCampaignRepositoryUsesOptimisticTransitionsAndFencedAdmissions(t *testing.T) {
	g := NewWithT(t)
	g.Expect(transitionCampaignSQL).To(ContainSubstring("version = @expected_version"))
	g.Expect(transitionCampaignSQL).To(ContainSubstring("transition_evidence"))
	g.Expect(admitCampaignMemberSQL).To(ContainSubstring("fencing_token = @fencing_token"))
	g.Expect(admitCampaignMemberSQL).To(ContainSubstring("status = 'PENDING'"))
}
