# Observability Correlation

Correlation is enabled by `observability_correlation`. The correlation package builds deterministic Grafana URLs from provided inputs. It does not call Grafana, query observability backends, or store correlation history.

## Builders

The correlation layer provides:

- `BuildTraceLink`
- `BuildMetricsLink`
- `BuildDashboardLink`
- `BuildUnifiedObservabilityContext`

These builders use `OBSERVABILITY_GRAFANA_BASE_URL` as the base URL. If the base URL is empty or invalid, link fields are empty.

## Placeholders

Dashboard API enrichment uses placeholders so callers can substitute runtime values later:

| Placeholder      | Meaning                                                   |
| ---------------- | --------------------------------------------------------- |
| `${trace_id}`    | Trace identifier to open in Grafana Explore.              |
| `${span_id}`     | Optional span identifier for narrowed trace review.       |
| `${service}`     | Service label value for metric and dashboard filters.     |
| `${environment}` | Environment label value for metric and dashboard filters. |

The placeholders are metadata. They do not cause the API to execute any trace or metric query.

## Example Links

Trace links point to Grafana Explore using the Tempo datasource name used by the link builder:

```text
https://grafana.example.com/explore?left=<encoded-tempo-query>
```

Metric links point to Grafana Explore using the Prometheus datasource name used by the link builder:

```text
https://grafana.example.com/explore?left=<encoded-prometheus-query>
```

Dashboard links point to static dashboard IDs and optional variable filters:

```text
https://grafana.example.com/d/http-overview?from=now-1h&to=now&var-environment=prod&var-service=hub
```

## Determinism

Label filters are sorted before link construction. Given the same base URL, dashboard ID, metric name, time range, and filters, builders return the same URL.

## Boundaries

Correlation does not add:

- log correlation,
- analytics storage,
- live query execution,
- Grafana API validation,
- runtime task or deployment behavior.
