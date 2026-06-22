# PR-047c2 - Observability Correlation Links

## Summary

PR-047c2 adds pure observability correlation link utilities. It builds deterministic Grafana URLs for traces, metrics, and dashboards without calling Grafana or changing telemetry instrumentation.

## Feature Flag

- `observability_correlation`
- This PR adds the flag for future activation points. The pure builders are only called by code that opts into them.

## Configuration

- `OBSERVABILITY_GRAFANA_BASE_URL`
- Used only as a base URL for future callers constructing Grafana links.
- No Grafana API integration is added.

## Utilities

Added package:

- `internal/observability/correlation`

Helpers:

- `BuildTraceLink`
- `BuildMetricsLink`
- `BuildDashboardLink`
- `BuildUnifiedObservabilityContext`

Types:

- `CorrelationContext`
- `TimeRange`
- `UnifiedObservabilityInput`
- `UnifiedObservabilityContext`

## Compatibility

- No database migration is required.
- No API endpoints are added or changed.
- Existing dashboard catalog behavior from PR-047c1 is unchanged.
- Existing metrics and tracing instrumentation from PR-047a and PR-047b is unchanged.
- Existing RBAC, authentication, action registry, deployment process logic, task transition semantics, and agent protocol behavior are unchanged.
- No UI page is added. The frontend feature flag model recognizes `observability_correlation` for future UI gating.

## Deferred Work

- PR-047c3: optional dashboard API enrichment with correlation metadata.
- Dashboard UI.
- Grafana provisioning API integration.
- Alerting.
- Log correlation.
- Runtime analytics or correlation history storage.

## Verification

- Correlation tests cover trace, metrics, dashboard, unified context, deterministic label ordering, and empty-base behavior.
- Feature flag tests cover backend and frontend flag plumbing.
