package db

import (
	"os"
	"path/filepath"
	"runtime"
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
	g.Expect(sql).To(ContainSubstring("deploymentcampaign_published_immutable"))
	g.Expect(sql).To(ContainSubstring("UNIQUE (campaign_revision_id, deployment_unit_id)"))
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
			ApprovalSubjectChecksum: "sha256:" + campaignTestHex("1"),
			ApprovalChecksum:        "sha256:" + campaignTestHex("1"),
			Approved:                true,
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

func campaignTestHex(value string) string {
	result := ""
	for range 64 {
		result += value
	}
	return result
}
