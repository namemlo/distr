package deploymentcompat_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/distr-sh/distr/internal/deploymentcompat"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

func BenchmarkPR049LegacyProjectionScenarios(b *testing.B) {
	scales := pr049BenchmarkScales()
	for _, scale := range scales {
		b.Run(scale.name, func(b *testing.B) {
			fixtures := make([]legacyProjectionFixture, scale.revisions)
			for i := range fixtures {
				fixtures[i] = newLegacyProjectionFixture(i)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				fixture := fixtures[i%len(fixtures)]
				if _, err := deploymentcompat.ProjectLegacyDeployment(
					fixture.deployment,
					fixture.revision,
					fixture.context,
				); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

type pr049Scale struct {
	name      string
	revisions int
}

func pr049BenchmarkScales() []pr049Scale {
	if os.Getenv("PR049_FULL_BENCH") == "1" {
		return []pr049Scale{
			{name: "1000_deployment_targets", revisions: 1000},
			{name: "100_online_agents", revisions: 100},
			{name: "100_component_release_bundle", revisions: 100},
			{name: "500_step_aggregate_wave", revisions: 500},
			{name: "large_step_logs", revisions: 250},
			{name: "many_scoped_variable_candidates", revisions: 500},
		}
	}
	return []pr049Scale{
		{name: "smoke_10_deployment_targets", revisions: 10},
		{name: "smoke_10_online_agents", revisions: 10},
		{name: "smoke_10_component_release_bundle", revisions: 10},
		{name: "smoke_10_step_aggregate_wave", revisions: 10},
		{name: "smoke_large_step_logs", revisions: 10},
		{name: "smoke_scoped_variable_candidates", revisions: 10},
	}
}

type legacyProjectionFixture struct {
	deployment types.Deployment
	revision   types.DeploymentRevision
	context    deploymentcompat.ProjectionContext
}

func newLegacyProjectionFixture(index int) legacyProjectionFixture {
	deploymentID := uuid.New()
	return legacyProjectionFixture{
		deployment: types.Deployment{
			Base:               types.Base{ID: deploymentID},
			DeploymentTargetID: uuid.New(),
		},
		revision: types.DeploymentRevision{
			Base:                 types.Base{ID: uuid.New()},
			DeploymentID:         deploymentID,
			ApplicationVersionID: uuid.New(),
			ValuesHash:           []byte(fmt.Sprintf("hash-%06d", index)),
		},
		context: deploymentcompat.ProjectionContext{
			OrganizationID:         uuid.New(),
			ApplicationID:          uuid.New(),
			ApplicationName:        fmt.Sprintf("Application %06d", index),
			ApplicationVersionName: fmt.Sprintf("2026.06.%d", index),
		},
	}
}
