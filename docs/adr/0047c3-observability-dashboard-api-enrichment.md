# ADR-0047c3 - Observability Dashboard API Enrichment

## Status

Accepted

## Context

PR-047c1 added a static dashboard catalog and PR-047c2 added pure Grafana link builders. The next safe slice is to expose optional correlation metadata from the dashboard API without adding UI, Grafana API calls, storage, or new telemetry instrumentation.

## Decision

Implement PR-047c3 as read-only dashboard API enrichment:

- extend dashboard definitions with static metric/query metadata,
- extend `api.ObservabilityDashboard` with optional correlation fields,
- enrich `GET /api/v1/observability/dashboards` only when `observability_correlation` is enabled,
- use `internal/observability/correlation` builders with `OBSERVABILITY_GRAFANA_BASE_URL`,
- keep all enriched values deterministic from static dashboard metadata and configuration.

The dashboard endpoint remains gated by `observability_dashboards`. Correlation enrichment is optional and does not execute queries or call Grafana.

## Consequences

- Existing dashboard API consumers continue to receive the base catalog unless the correlation flag is enabled.
- Future UI or integration work can consume stable link/query metadata.
- The observability package still has no storage, external API dependency, runtime analytics, alerting, or log correlation engine.
- Metrics and tracing instrumentation from PR-047a and PR-047b remain unchanged.

## Alternatives Considered

- Add dashboard UI in this PR: rejected because this slice is API metadata only.
- Call Grafana to validate dashboards or links: rejected because the catalog remains read-only and config-derived.
- Compute per-request trace or task context links: rejected because PR-047c3 exposes static templates, not runtime correlation.
