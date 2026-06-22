package correlation

import (
	"encoding/json"
	"net/url"
	pathpkg "path"
	"sort"
	"strings"
)

type CorrelationContext struct {
	TraceID     string `json:"trace_id,omitempty"`
	SpanID      string `json:"span_id,omitempty"`
	Service     string `json:"service,omitempty"`
	Environment string `json:"environment,omitempty"`
}

type TimeRange struct {
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

type UnifiedObservabilityInput struct {
	Correlation  CorrelationContext
	MetricName   string
	LabelFilters map[string]string
	DashboardID  string
	TimeRange    TimeRange
}

type UnifiedObservabilityContext struct {
	Correlation   CorrelationContext `json:"correlation"`
	TraceLink     string             `json:"traceLink,omitempty"`
	MetricsLink   string             `json:"metricsLink,omitempty"`
	DashboardLink string             `json:"dashboardLink,omitempty"`
}

func BuildTraceLink(baseURL string, correlation CorrelationContext) string {
	grafanaURL, ok := grafanaLinkURL(baseURL, "/explore")
	if !ok || correlation.TraceID == "" {
		return ""
	}
	query := grafanaURL.Query()
	query.Set("left", explorePayload("tempo", correlation.TraceID))
	setQueryValue(query, "spanId", correlation.SpanID)
	setQueryValue(query, "var-service", correlation.Service)
	setQueryValue(query, "var-environment", correlation.Environment)
	grafanaURL.RawQuery = query.Encode()
	return grafanaURL.String()
}

func BuildMetricsLink(baseURL, metricName string, labelFilters map[string]string) string {
	grafanaURL, ok := grafanaLinkURL(baseURL, "/explore")
	if !ok || metricName == "" {
		return ""
	}
	query := grafanaURL.Query()
	query.Set("left", explorePayload("prometheus", metricName+labelSelector(labelFilters)))
	grafanaURL.RawQuery = query.Encode()
	return grafanaURL.String()
}

func BuildDashboardLink(baseURL, dashboardID string, timeRange TimeRange, filters map[string]string) string {
	grafanaURL, ok := grafanaLinkURL(baseURL, "/d/"+url.PathEscape(dashboardID))
	if !ok || dashboardID == "" {
		return ""
	}
	query := grafanaURL.Query()
	setQueryValue(query, "from", timeRange.From)
	setQueryValue(query, "to", timeRange.To)
	for _, key := range sortedKeys(filters) {
		setQueryValue(query, "var-"+key, filters[key])
	}
	grafanaURL.RawQuery = query.Encode()
	return grafanaURL.String()
}

func BuildUnifiedObservabilityContext(baseURL string, input UnifiedObservabilityInput) UnifiedObservabilityContext {
	filters := withCorrelationFilters(input.LabelFilters, input.Correlation)
	return UnifiedObservabilityContext{
		Correlation:   input.Correlation,
		TraceLink:     BuildTraceLink(baseURL, input.Correlation),
		MetricsLink:   BuildMetricsLink(baseURL, input.MetricName, filters),
		DashboardLink: BuildDashboardLink(baseURL, input.DashboardID, input.TimeRange, filters),
	}
}

func grafanaLinkURL(baseURL, linkPath string) (*url.URL, bool) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, false
	}
	parsed.Path = pathpkg.Join(parsed.Path, linkPath)
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed, true
}

func explorePayload(datasource, expression string) string {
	payload := map[string]any{
		"datasource": datasource,
		"queries": []map[string]string{
			{
				"expr":  expression,
				"query": expression,
			},
		},
		"range": map[string]string{
			"from": "now-1h",
			"to":   "now",
		},
	}
	encoded, _ := json.Marshal(payload)
	return string(encoded)
}

func labelSelector(filters map[string]string) string {
	keys := sortedKeys(filters)
	if len(keys) == 0 {
		return ""
	}
	labels := make([]string, 0, len(keys))
	for _, key := range keys {
		value := strings.ReplaceAll(filters[key], `\`, `\\`)
		value = strings.ReplaceAll(value, `"`, `\"`)
		labels = append(labels, key+`="`+value+`"`)
	}
	return "{" + strings.Join(labels, ",") + "}"
}

func withCorrelationFilters(filters map[string]string, correlation CorrelationContext) map[string]string {
	copied := map[string]string{}
	for key, value := range filters {
		copied[key] = value
	}
	if correlation.Service != "" {
		copied["service"] = correlation.Service
	}
	if correlation.Environment != "" {
		copied["environment"] = correlation.Environment
	}
	return copied
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key, value := range values {
		if key != "" && value != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func setQueryValue(values url.Values, key, value string) {
	if value != "" {
		values.Set(key, value)
	}
}
