package db

import (
	"strings"
	"testing"

	. "github.com/onsi/gomega"
)

func TestCampaignRuntimeAuthorizationQueriesAreTenantScopedAndResolveEveryEnvironment(t *testing.T) {
	g := NewWithT(t)

	for _, query := range []string{
		campaignRevisionRuntimeAuthorizationTargetQuery,
		campaignRunRuntimeAuthorizationTargetQuery,
	} {
		normalized := strings.ToLower(strings.Join(strings.Fields(query), " "))
		g.Expect(normalized).To(ContainSubstring("deploymentcampaignrevision"))
		g.Expect(normalized).To(ContainSubstring("deploymentcampaignmember"))
		g.Expect(normalized).To(ContainSubstring("deploymentplan"))
		g.Expect(normalized).To(ContainSubstring("member.organization_id = revision.organization_id"))
		g.Expect(normalized).To(ContainSubstring("plan.organization_id = member.organization_id"))
		g.Expect(normalized).To(ContainSubstring("plan.id = member.deployment_plan_id"))
		g.Expect(normalized).To(ContainSubstring("array_agg(distinct plan.environment_id order by plan.environment_id)"))
		g.Expect(normalized).To(ContainSubstring("organization_id = @organization_id"))
	}

	runQuery := strings.ToLower(strings.Join(strings.Fields(campaignRunRuntimeAuthorizationTargetQuery), " "))
	g.Expect(runQuery).To(ContainSubstring("run.campaign_revision_id = revision.id"))
	g.Expect(runQuery).To(ContainSubstring("run.organization_id = revision.organization_id"))
}
