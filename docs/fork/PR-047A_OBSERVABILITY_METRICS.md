# PR-047a - Observability Metrics

## Summary

PR-047a adds the first observability slice: a metrics-only instrumentation foundation. It introduces a small recorder abstraction, a Prometheus-backed implementation, HTTP request middleware, and task execution transition hooks. Tracing, dashboards, external links, and external vendor integrations remain deferred.

## Feature Flag

- `observability_metrics`
- The metrics server still requires `METRICS_ENABLED=true`.
- When `observability_metrics` is disabled, the metrics router does not expose `/metrics` and the HTTP metrics middleware is not installed.

## Metrics

- `distr_http_requests_total`
- `distr_http_errors_total`
- `distr_http_request_duration_seconds`
- `distr_task_executions_total`
- `distr_task_duration_seconds`

All PR-047a metrics include base labels:

- `service`
- `environment`
- `version`

HTTP metrics also use low-cardinality request labels for method, route pattern, status code, and status class. Task metrics label only by task status.

## Compatibility

- No database migration is required.
- No public API endpoints are added.
- Existing agent deployment-target metrics are not changed.
- Existing tracing, logging, RBAC, authentication, and action registry behavior is unchanged.

## Deferred Work

- PR-047b: distributed tracing spans.
- PR-047c: dashboard link templates and examples.
- Business-level metrics are intentionally excluded.

## Verification

- Metrics package tests cover Prometheus output, base labels, HTTP counters/errors/latency, and task counters/duration.
- Service tests cover `/metrics` router gating.
- Task queue repository tests cover task transition metric hooks.
- Feature flag tests cover the backend registry and frontend feature flag type/service.
