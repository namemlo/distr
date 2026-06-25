package subscription

import (
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/onsi/gomega"
)

func TestCommunityDeploymentTargetLimitSupportsMultiTargetCustomers(t *testing.T) {
	g := gomega.NewWithT(t)

	limit := GetDeploymentTargetsPerCustomerOrganizationLimit(types.SubscriptionTypeCommunity)

	g.Expect(limit.Value()).To(gomega.Equal(int64(100)))
	g.Expect(limit.IsReached(1)).To(gomega.BeFalse())
	g.Expect(limit.IsReached(99)).To(gomega.BeFalse())
	g.Expect(limit.IsReached(100)).To(gomega.BeTrue())
}

func TestTrialDeploymentTargetLimitRemainsUnlimited(t *testing.T) {
	g := gomega.NewWithT(t)

	limit := GetDeploymentTargetsPerCustomerOrganizationLimit(types.SubscriptionTypeTrial)

	g.Expect(limit.IsUnlimited()).To(gomega.BeTrue())
}
