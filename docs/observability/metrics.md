# Observability Metrics

Metrics are enabled by `observability_metrics` and the existing `METRICS_ENABLED=true` metrics server setting. When either control is disabled, the metrics router does not expose `/metrics`.

## Metric Names

The current metrics slice exposes:

| Metric                                | Type      | Purpose                                                              |
| ------------------------------------- | --------- | -------------------------------------------------------------------- |
| `distr_http_requests_total`           | Counter   | HTTP request volume by route, method, status code, and status class. |
| `distr_http_errors_total`             | Counter   | HTTP error volume by route, method, status code, and status class.   |
| `distr_http_request_duration_seconds` | Histogram | HTTP request latency distribution.                                   |
| `distr_task_executions_total`         | Counter   | Task lifecycle transition counts by task status.                     |
| `distr_task_duration_seconds`         | Histogram | Task duration distribution by terminal status.                       |

All metrics include the base labels:

- `service`
- `environment`
- `version`

HTTP metrics add low-cardinality labels for request classification. Task metrics label by status only.

## Example Queries

HTTP request rate:

```promql
sum(rate(distr_http_requests_total[$__rate_interval])) by (status_class)
```

p95 HTTP request latency:

```promql
histogram_quantile(
  0.95,
  sum(rate(distr_http_request_duration_seconds_bucket[$__rate_interval])) by (le)
)
```

Task transitions by status:

```promql
sum(rate(distr_task_executions_total[$__rate_interval])) by (status)
```

p95 task duration:

```promql
histogram_quantile(
  0.95,
  sum(rate(distr_task_duration_seconds_bucket[$__rate_interval])) by (le, status)
)
```

## Interpreting Signals

High `5xx` request volume usually points to server-side failures. High `4xx` volume usually points to request validation, authentication, authorization, or missing-resource flows.

Latency should be reviewed with request volume. A high p95 on very low traffic can be noisy, while a sustained p95 increase with stable traffic is more likely to indicate a service issue.

Task counters are lifecycle signals, not business analytics. They show task state movement and terminal outcomes without adding high-cardinality labels such as arbitrary variable values or secrets.

## Boundaries

Metrics do not add tracing, dashboard provisioning, alerting, log correlation, or business-level metrics. Those concerns are handled by separate slices or remain future work.
