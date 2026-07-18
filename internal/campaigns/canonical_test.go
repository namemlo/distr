package campaigns

import (
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCanonicalizeCampaignRevisionIsStableAcrossRecreation(t *testing.T) {
	g := NewWithT(t)
	revision := campaignRevisionFixture()
	reordered := campaignRevisionFixture()
	reordered.ID = uuid.New()
	reordered.PublishedAt = time.Now().UTC()
	reordered.CanonicalPayload = []byte(`{"stale":true}`)
	reordered.CanonicalChecksum = "sha256:" + repeatHex("f")
	reordered.Members[0], reordered.Members[1] = reordered.Members[1], reordered.Members[0]
	reordered.Waves[0], reordered.Waves[1] = reordered.Waves[1], reordered.Waves[0]
	reordered.Prerequisites[0], reordered.Prerequisites[1] =
		reordered.Prerequisites[1], reordered.Prerequisites[0]

	firstPayload, firstChecksum, err := CanonicalizeCampaignRevision(revision)
	g.Expect(err).NotTo(HaveOccurred())
	secondPayload, secondChecksum, err := CanonicalizeCampaignRevision(reordered)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(secondPayload).To(Equal(firstPayload))
	g.Expect(secondChecksum).To(Equal(firstChecksum))
	g.Expect(firstChecksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
	g.Expect(string(firstPayload)).NotTo(ContainSubstring("publishedAt"))
	g.Expect(string(firstPayload)).NotTo(ContainSubstring("canonicalChecksum"))
}

func TestCanonicalizeCampaignRevisionBindsMemberPlanChecksum(t *testing.T) {
	g := NewWithT(t)
	revision := campaignRevisionFixture()
	_, firstChecksum, err := CanonicalizeCampaignRevision(revision)
	g.Expect(err).NotTo(HaveOccurred())

	revision.Members[0].PlanChecksum = "sha256:" + repeatHex("9")
	_, secondChecksum, err := CanonicalizeCampaignRevision(revision)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(secondChecksum).NotTo(Equal(firstChecksum))
}

func TestCanonicalizeCampaignRevisionBindsAllMemberEvidenceChecksums(t *testing.T) {
	g := NewWithT(t)
	revision := campaignRevisionFixture()
	_, firstChecksum, err := CanonicalizeCampaignRevision(revision)
	g.Expect(err).NotTo(HaveOccurred())

	revision.Members[0].EffectivePolicyChecksum = "sha256:" + repeatHex("9")
	revision.Members[0].ApprovalChecksum = "sha256:" + repeatHex("a")
	revision.Members[0].CalendarChecksums[0] = "sha256:" + repeatHex("b")
	revision.Members[0].AdmissionChecksum = "sha256:" + repeatHex("c")
	_, secondChecksum, err := CanonicalizeCampaignRevision(revision)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(secondChecksum).NotTo(Equal(firstChecksum))
}

func TestCanonicalizeCampaignRevisionBindsCanonicalProviderIdentity(t *testing.T) {
	g := NewWithT(t)
	revision := campaignRevisionFixture()
	_, firstChecksum, err := CanonicalizeCampaignRevision(revision)
	g.Expect(err).NotTo(HaveOccurred())

	revision.Prerequisites[0].ProviderComponentInstanceID = uuid.MustParse(
		"90000000-0000-0000-0000-000000000009",
	)
	_, secondChecksum, err := CanonicalizeCampaignRevision(revision)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(secondChecksum).NotTo(Equal(firstChecksum))
}

func campaignRevisionFixture() types.CampaignRevision {
	organizationID := uuid.MustParse("10000000-0000-0000-0000-000000000001")
	draftID := uuid.MustParse("20000000-0000-0000-0000-000000000001")
	firstPlanID := uuid.MustParse("30000000-0000-0000-0000-000000000001")
	secondPlanID := uuid.MustParse("30000000-0000-0000-0000-000000000002")
	return types.CampaignRevision{
		ID:                  uuid.New(),
		OrganizationID:      organizationID,
		CampaignDraftID:     draftID,
		RevisionNumber:      2,
		SourceDraftRevision: 4,
		Name:                "production rollout",
		Description:         "frozen plan membership",
		MembershipTagQuery:  "tier=production",
		RiskPolicy: types.CampaignRiskPolicy{
			MaximumConcurrency:          2,
			FailureToleranceBasisPoints: 500,
			MinimumHealthyBasisPoints:   9500,
		},
		Waves: []types.CampaignWave{
			{Order: 2, Name: "broad", BakeSeconds: 7200, MaximumConcurrency: 2},
			{Order: 1, Name: "canary", BakeSeconds: 3600, MaximumConcurrency: 1},
		},
		Members: []types.CampaignMember{
			{
				PlanID:           secondPlanID,
				DeploymentUnitID: uuid.MustParse("40000000-0000-0000-0000-000000000002"),
				PlanChecksum:     "sha256:" + repeatHex("2"),
				ApprovalRequestID: uuid.MustParse(
					"50000000-0000-0000-0000-000000000002",
				),
				ApprovalChecksum:        "sha256:" + repeatHex("4"),
				ApprovalRequestRevision: 2,
				EffectivePolicyChecksum: "sha256:" + repeatHex("5"),
				CalendarVersionIDs: []uuid.UUID{
					uuid.MustParse("70000000-0000-0000-0000-000000000002"),
				},
				CalendarChecksums: []string{"sha256:" + repeatHex("6")},
				AdmissionEvaluationID: uuid.MustParse(
					"80000000-0000-0000-0000-000000000002",
				),
				AdmissionChecksum: "sha256:" + repeatHex("7"),
				WaveOrder:         2,
				MemberOrder:       2,
			},
			{
				PlanID:           firstPlanID,
				DeploymentUnitID: uuid.MustParse("40000000-0000-0000-0000-000000000001"),
				PlanChecksum:     "sha256:" + repeatHex("1"),
				ApprovalRequestID: uuid.MustParse(
					"50000000-0000-0000-0000-000000000001",
				),
				ApprovalChecksum:        "sha256:" + repeatHex("3"),
				ApprovalRequestRevision: 2,
				EffectivePolicyChecksum: "sha256:" + repeatHex("4"),
				CalendarVersionIDs: []uuid.UUID{
					uuid.MustParse("70000000-0000-0000-0000-000000000001"),
				},
				CalendarChecksums: []string{"sha256:" + repeatHex("5")},
				AdmissionEvaluationID: uuid.MustParse(
					"80000000-0000-0000-0000-000000000001",
				),
				AdmissionChecksum: "sha256:" + repeatHex("6"),
				WaveOrder:         1,
				MemberOrder:       1,
			},
		},
		Prerequisites: []types.CampaignPrerequisite{
			{
				DownstreamPlanID:              secondPlanID,
				UpstreamPlanID:                firstPlanID,
				UpstreamStepKey:               "database.migrate",
				ProviderPlacementID:           uuid.MustParse("60000000-0000-0000-0000-000000000001"),
				ProviderDeploymentUnitID:      uuid.MustParse("40000000-0000-0000-0000-000000000001"),
				ProviderComponentInstanceID:   uuid.MustParse("90000000-0000-0000-0000-000000000001"),
				ExpectedObservedStateChecksum: "sha256:" + repeatHex("5"),
			},
			{
				DownstreamPlanID:              secondPlanID,
				UpstreamPlanID:                firstPlanID,
				UpstreamStepKey:               "service.deploy",
				ProviderPlacementID:           uuid.MustParse("60000000-0000-0000-0000-000000000002"),
				ProviderDeploymentUnitID:      uuid.MustParse("40000000-0000-0000-0000-000000000001"),
				ProviderComponentInstanceID:   uuid.MustParse("90000000-0000-0000-0000-000000000002"),
				ExpectedObservedStateChecksum: "sha256:" + repeatHex("6"),
			},
		},
	}
}

func repeatHex(value string) string {
	result := ""
	for range 64 {
		result += value
	}
	return result
}
