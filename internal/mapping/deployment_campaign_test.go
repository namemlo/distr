package mapping

import (
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCampaignRevisionToAPIPreservesFrozenPrerequisite(t *testing.T) {
	g := NewWithT(t)
	prerequisite := types.CampaignPrerequisite{
		DownstreamPlanID:             uuid.New(),
		UpstreamPlanID:               uuid.New(),
		UpstreamStepKey:              "database.migrate",
		ProviderPlacementID:          uuid.New(),
		ProviderDeploymentUnitID:     uuid.New(),
		ProviderComponentInstanceID:  uuid.New(),
		ExpectedRuntimeStateChecksum: "sha256:" + mappingCampaignHex("a"),
	}
	revision := types.CampaignRevision{
		ID:                  uuid.New(),
		OrganizationID:      uuid.New(),
		CampaignDraftID:     uuid.New(),
		RevisionNumber:      1,
		SourceDraftRevision: 2,
		CanonicalChecksum:   "sha256:" + mappingCampaignHex("b"),
		Prerequisites:       []types.CampaignPrerequisite{prerequisite},
		Members:             []types.CampaignMember{},
		Waves:               []types.CampaignWave{},
	}

	result := CampaignRevisionToAPI(revision)

	g.Expect(result.Prerequisites).To(HaveLen(1))
	g.Expect(result.Prerequisites[0].UpstreamStepKey).To(Equal("database.migrate"))
	g.Expect(result.Prerequisites[0].ExpectedRuntimeStateChecksum).
		To(Equal(prerequisite.ExpectedRuntimeStateChecksum))
	g.Expect(result.Prerequisites[0].ProviderDeploymentUnitID).
		To(Equal(prerequisite.ProviderDeploymentUnitID))
	g.Expect(result.Prerequisites[0].ProviderComponentInstanceID).
		To(Equal(prerequisite.ProviderComponentInstanceID))
}

func TestCampaignRevisionToAPIPreservesExactMemberEvidence(t *testing.T) {
	g := NewWithT(t)
	member := types.CampaignMember{
		PlanID:                  uuid.New(),
		DeploymentUnitID:        uuid.New(),
		PlanChecksum:            "sha256:" + mappingCampaignHex("1"),
		EffectivePolicyChecksum: "sha256:" + mappingCampaignHex("2"),
		ApprovalRequestID:       uuid.New(),
		ApprovalRequestRevision: 3,
		ApprovalChecksum:        "sha256:" + mappingCampaignHex("3"),
		CalendarVersionIDs:      []uuid.UUID{uuid.New()},
		CalendarChecksums:       []string{"sha256:" + mappingCampaignHex("4")},
		AdmissionEvaluationID:   uuid.New(),
		AdmissionChecksum:       "sha256:" + mappingCampaignHex("5"),
		WaveOrder:               1,
		MemberOrder:             1,
	}

	result := CampaignRevisionToAPI(types.CampaignRevision{
		Members: []types.CampaignMember{member},
	})

	g.Expect(result.Members).To(HaveLen(1))
	g.Expect(result.Members[0].EffectivePolicyChecksum).To(Equal(
		member.EffectivePolicyChecksum,
	))
	g.Expect(result.Members[0].ApprovalRequestRevision).To(Equal(
		member.ApprovalRequestRevision,
	))
	g.Expect(result.Members[0].CalendarChecksums).To(Equal(
		member.CalendarChecksums,
	))
	g.Expect(result.Members[0].AdmissionChecksum).To(Equal(
		member.AdmissionChecksum,
	))
}

func mappingCampaignHex(value string) string {
	result := ""
	for range 64 {
		result += value
	}
	return result
}
