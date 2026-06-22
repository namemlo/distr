# PR-047c3 - Observability Dashboard API Enrichment

## Summary

PR-047c3 enriches the read-only observability dashboard catalog with optional correlation metadata. It exposes static link templates and metric query templates for callers that enable observability correlation, without calling Grafana or changing runtime telemetry behavior.

## Feature Flags

- `observability_dashboards`
- `observability_correlation`

The dashboard endpoint still requires `observability_dashboards`. Correlation metadata is included only when `observability_correlation` is enabled.

## Configuration

- `OBSERVABILITY_GRAFANA_BASE_URL`

The configured Grafana base URL is used only to build deterministic link templates in the dashboard API response. Empty or invalid values result in empty link-template fields from the pure correlation builders.

## API

- `GET /api/v1/observability/dashboards`

When correlation is disabled, the response shape remains the PR-047c1 base dashboard catalog:

- `id`
- `name`
- `description`
- `category`
- `version`
- `template`

When correlation is enabled, each dashboard can also include:

- `traceLinkTemplate`
- `metricsQueryTemplate`
- `correlationHints`

`correlationHints` contains placeholder names, dashboard variables, a metrics link template, and a dashboard link template. These values are static or config-derived only.

## Compatibility

- No database migration is required.
- No dashboard UI is added.
- No Grafana API integration or provisioning is added.
- No metric or trace instrumentation changes are made.
- Existing RBAC, authentication, task execution, action registry, deployment process logic, and agent protocol behavior are unchanged.

## Deferred Work

- Dashboard UI.
- Grafana provisioning API integration.
- Alerting rules.
- Log correlation.
- Runtime analytics or correlation history storage.

## Verification

- Dashboard tests cover static correlation metadata and immutable copies.
- Handler tests cover disabled base responses, enabled correlation metadata, and deterministic enriched output.
- Correlation package tests continue to cover link-builder behavior.
