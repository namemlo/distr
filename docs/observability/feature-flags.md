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

| Flag                        | Effective when                                               | Boundary                                                                                             |
| --------------------------- | ------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------- |
| `operator_control_plane_v2` | Its key is configured.                                       | Umbrella for new control-plane v2 writes; historical reads and v1 behavior stay available.           |
| `executor_protocol_v2`      | Both its key and `operator_control_plane_v2` are configured. | Admission kill switch for the fenced executor protocol v2; configuring it alone remains ineffective. |

Both flags are process-wide until PR-066 adds organization/environment enrollment. They must remain disabled in
shared and production environments until PR-083 completes hardening. In an isolated developer environment only,
the layered state can be evaluated with:

```text
DISTR_EXPERIMENTAL_FEATURE_FLAGS=operator_control_plane_v2,executor_protocol_v2
```

Do not use `all` in a shared or production environment during this program because it includes both registered
control-plane flags. Removing the umbrella key immediately makes executor protocol v2 ineffective without
removing its configured key.

## Boundaries

The observability flags do not enable Grafana provisioning, dashboard UI, alerting, log correlation, analytics storage, RBAC changes, or task execution changes. PR-055 only establishes the control-plane isolation boundary; it does not add v2 routes, persistence, execution, retry, or migration behavior.
