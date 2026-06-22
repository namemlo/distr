# ADR-0047c1 - Observability Static Dashboards

## Status

Accepted

## Context

PR-047a added metrics and PR-047b added tracing. The next observability item is dashboards and correlation links, but combining dashboard definitions, Grafana link generation, and correlation metadata would make the PR harder to review. The first safe slice is static dashboard definitions plus a read-only API surface.

## Decision

Implement PR-047c1 as static dashboards only:

- add `internal/observability/dashboards` with immutable dashboard definitions,
- include static Grafana JSON templates for HTTP overview, task execution overview, and service health overview,
- add `GET /api/v1/observability/dashboards`,
- gate the endpoint with `observability_dashboards`.

When the feature flag is disabled, the endpoint returns `404`. The endpoint does not call Grafana, generate dashboards dynamically, or compute live observability data.

## Consequences

- Dashboard templates can be reviewed independently before correlation links are added.
- Existing metrics and tracing instrumentation stay unchanged.
- No database schema, task execution, RBAC, or action registry behavior changes are required.
- The static API response gives future UI or integration work a stable catalog shape.

## Alternatives Considered

- Add dashboard definitions and correlation links together: rejected to keep this PR small and static.
- Generate dashboards dynamically from live data: rejected because PR-047c1 is a static template catalog.
- Integrate with the Grafana API: rejected because provisioning and link generation belong to later slices.
