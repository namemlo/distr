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
		DownstreamPlanID:              uuid.New(),
		UpstreamPlanID:                uuid.New(),
		UpstreamStepKey:               "database.migrate",
		ProviderPlacementID:           uuid.New(),
		ExpectedObservedStateChecksum: "sha256:" + mappingCampaignHex("a"),
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
	g.Expect(result.Prerequisites[0].ExpectedObservedStateChecksum).
		To(Equal(prerequisite.ExpectedObservedStateChecksum))
}

func mappingCampaignHex(value string) string {
	result := ""
	for range 64 {
		result += value
	}
	return result
}
