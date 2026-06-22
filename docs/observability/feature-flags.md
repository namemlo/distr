# Observability Feature Flags

Observability features are enabled with `DISTR_EXPERIMENTAL_FEATURE_FLAGS`. Multiple flags can be separated by commas, spaces, semicolons, tabs, or newlines.

## Matrix

| Flag                        | Enables                                                                                  | Still inactive when disabled                                                                 |
| --------------------------- | ---------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `observability_metrics`     | Prometheus metrics instrumentation and metrics route wiring when `METRICS_ENABLED=true`. | `/metrics` route exposure and PR-047a HTTP/task metrics hooks.                               |
| `observability_tracing`     | OpenTelemetry-backed HTTP and task lifecycle tracing.                                    | HTTP tracing middleware, task lifecycle span recording, and OTEL exporter construction.      |
| `observability_dashboards`  | Read-only dashboard catalog at `GET /api/v1/observability/dashboards`.                   | Dashboard API route, static dashboard template response.                                     |
| `observability_correlation` | Pure correlation link generation and optional dashboard API enrichment.                  | Optional link/query metadata in dashboard responses and any caller-gated correlation output. |

## Example Configurations

Metrics only:

```text
METRICS_ENABLED=true
DISTR_EXPERIMENTAL_FEATURE_FLAGS=observability_metrics
```

Metrics and tracing:

```text
METRICS_ENABLED=true
DISTR_EXPERIMENTAL_FEATURE_FLAGS=observability_metrics,observability_tracing
```

Dashboard catalog with correlation metadata:

```text
OBSERVABILITY_GRAFANA_BASE_URL=https://grafana.example.com
DISTR_EXPERIMENTAL_FEATURE_FLAGS=observability_dashboards,observability_correlation
```

Full current observability suite:

```text
METRICS_ENABLED=true
OBSERVABILITY_GRAFANA_BASE_URL=https://grafana.example.com
DISTR_EXPERIMENTAL_FEATURE_FLAGS=observability_metrics,observability_tracing,observability_dashboards,observability_correlation
```

## Safe Production Pattern

Start with metrics first, then tracing, then dashboards, then correlation metadata:

1. Enable `observability_metrics` and confirm `/metrics` is scraped.
2. Enable `observability_tracing` and confirm exporters receive spans.
3. Enable `observability_dashboards` and review the static catalog.
4. Set `OBSERVABILITY_GRAFANA_BASE_URL` and enable `observability_correlation`.

This order keeps each layer observable on its own before adding links between layers.

## Boundaries

These flags do not enable Grafana provisioning, dashboard UI, alerting, log correlation, analytics storage, RBAC changes, or task execution changes.
