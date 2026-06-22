package dashboards

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/gomega"
)

func TestDefinitionsReturnStaticGrafanaDashboards(t *testing.T) {
	g := NewWithT(t)

	definitions := Definitions()

	g.Expect(definitions).To(HaveLen(3))
	g.Expect(definitions[0].ID).To(Equal("http-overview"))
	g.Expect(definitions[0].Version).NotTo(BeEmpty())
	g.Expect(json.Valid(definitions[0].Template)).To(BeTrue())
	g.Expect(definitions[0].Correlation.MetricName).To(Equal("distr_http_requests_total"))
	g.Expect(definitions[0].Correlation.MetricsQueryTemplate).To(Equal("sum(rate(distr_http_requests_total[$__rate_interval])) by (status_class)"))
	g.Expect(definitions[0].Correlation.DashboardVariables).To(Equal([]string{"environment", "service"}))
	g.Expect(definitions[1].ID).To(Equal("task-execution-overview"))
	g.Expect(json.Valid(definitions[1].Template)).To(BeTrue())
	g.Expect(definitions[1].Correlation.MetricName).To(Equal("distr_task_executions_total"))
	g.Expect(definitions[2].ID).To(Equal("service-health-overview"))
	g.Expect(json.Valid(definitions[2].Template)).To(BeTrue())
	g.Expect(definitions[2].Correlation.MetricName).To(Equal("up"))
}

func TestDefinitionsReturnImmutableCopies(t *testing.T) {
	g := NewWithT(t)

	definitions := Definitions()
	definitions[0].Template[0] = 'x'
	definitions[0].Correlation.DashboardVariables[0] = "mutated"

	freshDefinitions := Definitions()
	g.Expect(json.Valid(freshDefinitions[0].Template)).To(BeTrue())
	g.Expect(freshDefinitions[0].ID).To(Equal("http-overview"))
	g.Expect(freshDefinitions[0].Correlation.DashboardVariables).To(Equal([]string{"environment", "service"}))
}
