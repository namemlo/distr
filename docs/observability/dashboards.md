# Observability Dashboards

Dashboards are enabled by `observability_dashboards`. When disabled, `GET /api/v1/observability/dashboards` returns `404`.

## Dashboard Catalog

The static catalog currently includes:

| ID                        | Name                          | Category         |
| ------------------------- | ----------------------------- | ---------------- |
| `http-overview`           | Distr HTTP Overview           | `http`           |
| `task-execution-overview` | Distr Task Execution Overview | `tasks`          |
| `service-health-overview` | Distr Service Health Overview | `service-health` |

Each dashboard response includes:

- `id`
- `name`
- `description`
- `category`
- `version`
- `template`

The `template` value is static Grafana dashboard JSON. The API does not mutate templates at request time.

## Correlation Enrichment

When `observability_correlation` is also enabled, the dashboard API can include:

- `traceLinkTemplate`
- `metricsQueryTemplate`
- `correlationHints`

The enriched fields are metadata only. They are generated from static dashboard definitions and `OBSERVABILITY_GRAFANA_BASE_URL`.

Example shape:

```json
{
  "id": "http-overview",
  "name": "Distr HTTP Overview",
  "metricsQueryTemplate": "sum(rate(distr_http_requests_total[$__rate_interval])) by (status_class)",
  "correlationHints": {
    "traceIdPlaceholder": "${trace_id}",
    "spanIdPlaceholder": "${span_id}",
    "serviceLabel": "service",
    "environmentLabel": "environment",
    "dashboardVariables": ["environment", "service"]
  }
}
```

## Boundaries

The dashboard catalog does not:

- call Grafana,
- provision dashboards,
- add a dashboard UI,
- query metrics or traces,
- store user-specific dashboard state,
- change metrics or tracing instrumentation.
