package dashboards

import "encoding/json"

type Definition struct {
	ID          string
	Name        string
	Description string
	Category    string
	Version     string
	Template    json.RawMessage
}

const dashboardVersion = "1.0.0"

var definitions = []Definition{
	{
		ID:          "http-overview",
		Name:        "Distr HTTP Overview",
		Description: "HTTP request volume, status classes, and latency for the Distr hub API.",
		Category:    "http",
		Version:     dashboardVersion,
		Template: json.RawMessage(`{
  "uid": "distr-http-overview",
  "title": "Distr HTTP Overview",
  "schemaVersion": 39,
  "version": 1,
  "tags": ["distr", "observability", "http"],
  "timezone": "browser",
  "panels": [
    {
      "id": 1,
      "type": "timeseries",
      "title": "Request rate by status class",
      "targets": [
        {
          "expr": "sum(rate(distr_http_requests_total[$__rate_interval])) by (status_class)",
          "legendFormat": "{{status_class}}"
        }
      ]
    },
    {
      "id": 2,
      "type": "timeseries",
      "title": "p95 request latency",
      "targets": [
        {
          "expr": "histogram_quantile(0.95, sum(rate(distr_http_request_duration_seconds_bucket[$__rate_interval])) by (le))",
          "legendFormat": "p95"
        }
      ]
    },
    {
      "id": 3,
      "type": "stat",
      "title": "Error rate",
      "targets": [
        {
          "expr": "sum(rate(distr_http_errors_total[$__rate_interval]))",
          "legendFormat": "errors"
        }
      ]
    }
  ]
}`),
	},
	{
		ID:          "task-execution-overview",
		Name:        "Distr Task Execution Overview",
		Description: "Task execution counts and durations from durable task state transitions.",
		Category:    "tasks",
		Version:     dashboardVersion,
		Template: json.RawMessage(`{
  "uid": "distr-task-execution-overview",
  "title": "Distr Task Execution Overview",
  "schemaVersion": 39,
  "version": 1,
  "tags": ["distr", "observability", "tasks"],
  "timezone": "browser",
  "panels": [
    {
      "id": 1,
      "type": "timeseries",
      "title": "Task transitions by status",
      "targets": [
        {
          "expr": "sum(rate(distr_task_executions_total[$__rate_interval])) by (status)",
          "legendFormat": "{{status}}"
        }
      ]
    },
    {
      "id": 2,
      "type": "timeseries",
      "title": "p95 task duration",
      "targets": [
        {
          "expr": "histogram_quantile(0.95, sum(rate(distr_task_duration_seconds_bucket[$__rate_interval])) by (le, status))",
          "legendFormat": "{{status}}"
        }
      ]
    }
  ]
}`),
	},
	{
		ID:          "service-health-overview",
		Name:        "Distr Service Health Overview",
		Description: "Process and Go runtime health signals exposed by the metrics endpoint.",
		Category:    "service-health",
		Version:     dashboardVersion,
		Template: json.RawMessage(`{
  "uid": "distr-service-health-overview",
  "title": "Distr Service Health Overview",
  "schemaVersion": 39,
  "version": 1,
  "tags": ["distr", "observability", "service-health"],
  "timezone": "browser",
  "panels": [
    {
      "id": 1,
      "type": "stat",
      "title": "Scrape health",
      "targets": [
        {
          "expr": "up",
          "legendFormat": "{{job}}"
        }
      ]
    },
    {
      "id": 2,
      "type": "timeseries",
      "title": "Goroutines",
      "targets": [
        {
          "expr": "go_goroutines",
          "legendFormat": "goroutines"
        }
      ]
    },
    {
      "id": 3,
      "type": "timeseries",
      "title": "Resident memory",
      "targets": [
        {
          "expr": "process_resident_memory_bytes",
          "legendFormat": "rss"
        }
      ]
    }
  ]
}`),
	},
}

func Definitions() []Definition {
	copied := make([]Definition, 0, len(definitions))
	for _, definition := range definitions {
		definition.Template = append(json.RawMessage{}, definition.Template...)
		copied = append(copied, definition)
	}
	return copied
}
