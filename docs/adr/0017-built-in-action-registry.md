# ADR-0017: Built-in Action Registry

## Status

Accepted for PR-017.

## Context

PR-011 introduced generic Deployment Process steps with `actionType` and JSON `inputBindings`. PR-012 added an editor for those fields, and PR-013 added immutable process snapshots. The roadmap next requires the first built-in action registry before Deployment Plans, queues, execution, or agent behavior are introduced.

The first registry slice must remain small and generic. It must not add Compose, Helm, OCI job, webhook, file render, notification, approval, child release, Step Template, deployment planning, task queue, or execution behavior.

## Decision

Add a static in-memory action registry containing:

- `distr.preflight`
- `distr.http.check`
- `distr.wait`

Each action definition includes:

- stable action type
- display name
- description
- JSON Schema Draft 2020-12 input schema
- JSON Schema Draft 2020-12 output schema

Expose the registry through:

```http
GET /api/v1/action-definitions
```

The endpoint is read-only, vendor-scoped, and gated by the existing `deployment_processes` experimental feature flag.

Validate Deployment Process revision steps against the registry in both request validation and repository normalization. Unknown action types and schema-invalid `inputBindings` return bad request. No database table is added because PR-017 owns built-in metadata only.

## Consequences

- Deployment Process revisions now have a stable, generic action vocabulary for later planning work.
- The Hub can reject invalid action inputs before they become immutable revisions or snapshots.
- The action registry can be consumed by UI or API clients without exposing execution capabilities.
- No agent protocol, task queue, or execution contract is introduced.
- Adding later actions will require code and tests, keeping the public action vocabulary deliberate.

## Alternatives Considered

Adding an `ActionDefinition` database table was rejected because PR-017 defines built-in actions only. User-defined actions and Step Templates belong to later roadmap work.

Keeping Deployment Process steps fully free-form was rejected because Deployment Plans need schema-valid inputs to produce deterministic previews.

Adding Compose, Helm, OCI job, webhook, or file actions now was rejected because the roadmap reserves those actions for later implementation slices.

## Validation

PR-017 adds action registry tests, API validation tests, handler tests, mapping tests, live PostgreSQL Deployment Process repository tests, Angular service tests, and changed-file Unicode scans.
