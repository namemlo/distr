# Observability Overview

The observability package is split into small, feature-flagged layers. Each layer can be reviewed and enabled independently:

- [Metrics](metrics.md) expose Prometheus-compatible counters and histograms.
- [Tracing](tracing.md) records HTTP and task lifecycle spans through the existing OpenTelemetry provider stack.
- [Dashboards](dashboards.md) publish static Grafana dashboard templates through a read-only API.
- [Correlation](correlation.md) builds deterministic Grafana links between traces, metrics, and dashboards.
- [Grafana integration](grafana-integration.md) explains static link configuration without provisioning dashboards.
- [Feature flags](feature-flags.md) lists the rollout controls and safe combinations.

## Layering

The layers compose in this order:

1. `observability_metrics` records Prometheus metrics when `METRICS_ENABLED=true`.
2. `observability_tracing` installs tracing providers and HTTP/task span hooks.
3. `observability_dashboards` exposes static dashboard definitions at `GET /api/v1/observability/dashboards`.
4. `observability_correlation` adds deterministic link metadata to callers that opt into the correlation utilities or dashboard API enrichment.

Disabling a higher layer does not disable lower layers. For example, dashboard templates can be available while tracing remains disabled, and correlation metadata can be omitted while the base dashboard catalog remains available.

## Current Boundaries

The current observability suite does not:

- call Grafana APIs,
- provision dashboards,
- add a dashboard UI,
- store correlation history,
- execute metric or trace queries,
- add log correlation,
- change task execution behavior,
- change RBAC.

For the implementation slices, see:

- [PR-047a metrics](../fork/PR-047A_OBSERVABILITY_METRICS.md)
- [PR-047b tracing](../fork/PR-047B_OBSERVABILITY_TRACING.md)
- [PR-047c1 static dashboards](../fork/PR-047C1_OBSERVABILITY_STATIC_DASHBOARDS.md)
- [PR-047c2 correlation links](../fork/PR-047C2_OBSERVABILITY_CORRELATION_LINKS.md)
- [PR-047c3 dashboard API enrichment](../fork/PR-047C3_OBSERVABILITY_DASHBOARD_API_ENRICHMENT.md)
