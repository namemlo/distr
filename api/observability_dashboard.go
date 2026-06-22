package api

import "encoding/json"

type ObservabilityDashboardListResponse struct {
	Dashboards []ObservabilityDashboard `json:"dashboards"`
}

type ObservabilityDashboard struct {
	ID                   string                                  `json:"id"`
	Name                 string                                  `json:"name"`
	Description          string                                  `json:"description"`
	Category             string                                  `json:"category"`
	Version              string                                  `json:"version"`
	Template             json.RawMessage                         `json:"template"`
	TraceLinkTemplate    string                                  `json:"traceLinkTemplate,omitempty"`
	MetricsQueryTemplate string                                  `json:"metricsQueryTemplate,omitempty"`
	CorrelationHints     *ObservabilityDashboardCorrelationHints `json:"correlationHints,omitempty"`
}

type ObservabilityDashboardCorrelationHints struct {
	TraceIDPlaceholder    string   `json:"traceIdPlaceholder,omitempty"`
	SpanIDPlaceholder     string   `json:"spanIdPlaceholder,omitempty"`
	ServiceLabel          string   `json:"serviceLabel,omitempty"`
	EnvironmentLabel      string   `json:"environmentLabel,omitempty"`
	DashboardVariables    []string `json:"dashboardVariables,omitempty"`
	MetricsLinkTemplate   string   `json:"metricsLinkTemplate,omitempty"`
	DashboardLinkTemplate string   `json:"dashboardLinkTemplate,omitempty"`
}
