package db

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/campaigns"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
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
	g.Expect(sql).To(ContainSubstring("expected_observed_state_checksum"))
	g.Expect(sql).To(ContainSubstring("effective_policy_checksum"))
	g.Expect(sql).To(ContainSubstring("approval_request_revision"))
	g.Expect(sql).To(ContainSubstring("calendar_version_ids"))
	g.Expect(sql).To(ContainSubstring("calendar_checksums"))
	g.Expect(sql).To(ContainSubstring("admission_evaluation_id"))
	g.Expect(sql).To(ContainSubstring("admission_checksum"))
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
