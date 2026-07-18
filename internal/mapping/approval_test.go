package mapping

import (
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestApprovalMappingRetainsPinnedEvidenceAndOmitsOrganizationID(t *testing.T) {
	g := NewWithT(t)
	request := types.ApprovalRequest{
		ID:                      uuid.New(),
		OrganizationID:          uuid.New(),
		SubjectType:             types.ApprovalSubjectDeploymentPlan,
		SubjectID:               uuid.New(),
		SubjectRevision:         3,
		SubjectChecksum:         "sha256:plan",
		EffectivePolicyChecksum: "sha256:policy",
		SubscriberSetChecksum:   "sha256:subscribers",
		State:                   types.ApprovalRequestStatePending,
		Revision:                4,
		Requirements:            []types.ApprovalRequirement{},
		Decisions:               []types.ApprovalDecision{},
	}

	mapped := ApprovalRequestToAPI(request)

	g.Expect(mapped.ID).To(Equal(request.ID))
	g.Expect(mapped.SubjectChecksum).To(Equal(request.SubjectChecksum))
	g.Expect(mapped.EffectivePolicyChecksum).To(Equal(request.EffectivePolicyChecksum))
	g.Expect(mapped.SubscriberSetChecksum).To(Equal(request.SubscriberSetChecksum))
	g.Expect(mapped.Revision).To(Equal(int64(4)))
	g.Expect(mapped.Requirements).To(BeEmpty())
	g.Expect(mapped.Decisions).To(BeEmpty())
}
