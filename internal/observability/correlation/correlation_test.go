package correlation

import (
	"net/url"
	"testing"

	. "github.com/onsi/gomega"
)

func TestBuildTraceLink(t *testing.T) {
	g := NewWithT(t)

	link := BuildTraceLink("https://grafana.example/", CorrelationContext{
		TraceID:     "trace-123",
		SpanID:      "span-456",
		Service:     "hub",
		Environment: "prod",
	})

	parsed, err := url.Parse(link)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(parsed.Scheme).To(Equal("https"))
	g.Expect(parsed.Host).To(Equal("grafana.example"))
	g.Expect(parsed.Path).To(Equal("/explore"))
	g.Expect(parsed.Query().Get("left")).To(ContainSubstring("trace-123"))
	g.Expect(parsed.Query().Get("spanId")).To(Equal("span-456"))
	g.Expect(parsed.Query().Get("var-service")).To(Equal("hub"))
	g.Expect(parsed.Query().Get("var-environment")).To(Equal("prod"))
}

func TestBuildMetricsLinkSortsLabelFilters(t *testing.T) {
	g := NewWithT(t)

	link := BuildMetricsLink("https://grafana.example", "distr_http_requests_total", map[string]string{
		"service":     "hub",
		"environment": "prod",
	})

	parsed, err := url.Parse(link)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(parsed.Path).To(Equal("/explore"))
	g.Expect(parsed.Query().Get("left")).To(ContainSubstring(`distr_http_requests_total{environment=\"prod\",service=\"hub\"}`))
}

func TestBuildDashboardLink(t *testing.T) {
	g := NewWithT(t)

	link := BuildDashboardLink("https://grafana.example", "http-overview", TimeRange{
		From: "now-1h",
		To:   "now",
	}, map[string]string{
		"service":     "hub",
		"environment": "prod",
	})

	parsed, err := url.Parse(link)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(parsed.Path).To(Equal("/d/http-overview"))
	g.Expect(parsed.Query().Get("from")).To(Equal("now-1h"))
	g.Expect(parsed.Query().Get("to")).To(Equal("now"))
	g.Expect(parsed.Query().Get("var-environment")).To(Equal("prod"))
	g.Expect(parsed.Query().Get("var-service")).To(Equal("hub"))
}

func TestBuildUnifiedObservabilityContext(t *testing.T) {
	g := NewWithT(t)

	context := BuildUnifiedObservabilityContext("https://grafana.example", UnifiedObservabilityInput{
		Correlation: CorrelationContext{
			TraceID:     "trace-123",
			SpanID:      "span-456",
			Service:     "hub",
			Environment: "prod",
		},
		MetricName:  "distr_task_executions_total",
		DashboardID: "task-execution-overview",
		TimeRange:   TimeRange{From: "now-6h", To: "now"},
	})

	g.Expect(context.Correlation.TraceID).To(Equal("trace-123"))
	g.Expect(context.TraceLink).To(ContainSubstring("trace-123"))
	g.Expect(context.MetricsLink).To(ContainSubstring("distr_task_executions_total"))
	g.Expect(context.DashboardLink).To(ContainSubstring("/d/task-execution-overview"))
}

func TestLinkBuildersReturnEmptyWithoutBaseURL(t *testing.T) {
	g := NewWithT(t)

	g.Expect(BuildTraceLink("", CorrelationContext{TraceID: "trace-123"})).To(BeEmpty())
	g.Expect(BuildMetricsLink("", "distr_http_requests_total", nil)).To(BeEmpty())
	g.Expect(BuildDashboardLink("", "http-overview", TimeRange{}, nil)).To(BeEmpty())
}
