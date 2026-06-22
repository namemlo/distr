# PR-047c1 - Observability Static Dashboards

## Summary

PR-047c1 adds the first dashboard slice for observability. It publishes static, versioned Grafana dashboard templates through a read-only API endpoint. Correlation links, Grafana provisioning, and runtime dashboard generation remain deferred.

## Feature Flag

- `observability_dashboards`
- When disabled, `/api/v1/observability/dashboards` returns `404`.

## Dashboards

Static templates are available for:

- HTTP overview
- Task execution overview
- Service health overview

Each dashboard definition includes:

- `id`
- `name`
- `description`
- `category`
- `version`
- `template`

## API

- `GET /api/v1/observability/dashboards`

The response contains a static dashboard catalog. The endpoint does not call Grafana, query metrics, query traces, or mutate dashboard definitions at runtime.

## Compatibility

- No database migration is required.
- Existing metrics behavior from PR-047a is unchanged.
- Existing tracing behavior from PR-047b is unchanged.
- Existing RBAC, authentication, action registry, deployment process logic, task transition semantics, and agent protocol behavior are unchanged.
- No UI page is added. The frontend feature flag model recognizes `observability_dashboards` for future UI gating.

## Deferred Work

- Correlation link generation.
- Grafana base URL configuration.
- Grafana provisioning API integration.
- Dashboard UI.
- Alerting rules.
- Log correlation.

## Verification

- Dashboard package tests cover static definition count, JSON validity, and immutable copies.
- Handler tests cover the dashboard response shape and disabled feature flag behavior.
- Feature flag tests cover backend and frontend flag plumbing.
