# Feature Flags

Experimental features are enabled with `DISTR_EXPERIMENTAL_FEATURE_FLAGS`. Multiple flags can be separated by commas, spaces, semicolons, tabs, or newlines. Registered flags are off by default, and unknown keys fail Hub startup validation.

## Observability Matrix

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

## Operator Control-Plane Isolation

| Flag                        | Process state                                                | Resource boundary                                                                                                                     |
| --------------------------- | ------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------- |
| `operator_control_plane_v2` | Its key is configured.                                       | New v2 operation also requires action authority plus active organization and selected-environment enrollment.                         |
| `executor_protocol_v2`      | Both its key and `operator_control_plane_v2` are configured. | Fenced executor admission additionally requires the same PR-066 tenant enrollment; configuring either process flag grants no access. |

PR-066 adds append-only organization and environment enrollment revisions under `/api/v1/authorization`.
The process flag remains the emergency kill switch; both tenant enrollment levels must be effective for the
selected environment. These controls must remain disabled in shared and production environments until PR-083
completes hardening. In an isolated developer environment only, the process-layer state can be evaluated with:

```text
DISTR_EXPERIMENTAL_FEATURE_FLAGS=operator_control_plane_v2,executor_protocol_v2
```

Do not use `all` in a shared or production environment during this program because it includes both registered
control-plane flags. Removing the umbrella key and restarting the Hub makes executor protocol v2 ineffective
without removing its configured key.

## Boundaries

The observability flags do not enable Grafana provisioning, dashboard UI, alerting, log correlation, analytics
storage, authorization changes, or task execution changes. Control-plane process flags never grant tenant
authority. PR-066 scoped roles and enrollment add the resource boundary but do not add executor-v2 dispatch,
retry, or migration behavior.
