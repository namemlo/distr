# PR-017 Built-in Action Registry

## Scope

PR-017 adds a small built-in action registry for Deployment Process steps.

Included:

- Static action metadata for `distr.preflight`, `distr.http.check`, and `distr.wait`.
- JSON input and output schemas for those actions.
- Read-only API for listing action definitions.
- API-level and repository-level validation of Deployment Process step `actionType` and `inputBindings`.
- Angular service support for reading action definitions.
- Backend, handler, mapping, repository, action registry, and Angular service tests.

Excluded:

- Deployment Plan tables, planning service, preview, or export.
- Task queue, execution, step runs, or agent protocol behavior.
- Compose, Helm, OCI job, webhook, file render, notification, child release, approval, or Step Template actions.
- UI route or sidebar changes.
- Database migrations.

Those features remain PR-018 or later roadmap work.

## Feature Flag

The action definition API uses the existing `deployment_processes` experimental feature flag.

When the flag is disabled:

```text
GET /api/v1/action-definitions
```

returns `403 Forbidden`.

Deployment Process create-revision endpoints already require `deployment_processes`; PR-017 adds registry validation inside that existing feature-gated flow.

## Built-in Actions

### `distr.preflight`

Runs built-in agent preflight checks and returns structured results.

Input schema:

- object
- no required properties
- no additional properties

### `distr.http.check`

Calls an HTTP endpoint and validates status, headers, body match, latency, and retry policy.

Input schema requires:

- `url`

Optional inputs include:

- `method`
- `expectedStatusCodes`
- `expectedHeaders`
- `bodyContains`
- `bodyRegex`
- `maxLatencyMs`
- `retry.maxAttempts`
- `retry.intervalSeconds`

### `distr.wait`

Waits for either:

- a positive `durationSeconds` value, or
- a non-empty `condition` string

Exactly one of those input shapes must be provided.

## API

Read-only endpoint:

```http
GET /api/v1/action-definitions
```

Response shape:

```json
[
  {
    "type": "distr.preflight",
    "name": "Preflight checks",
    "description": "Runs built-in agent preflight checks and returns structured results.",
    "inputSchema": {},
    "outputSchema": {}
  }
]
```

The schema objects are JSON Schema Draft 2020-12 documents.

## Validation

Deployment Process revision creation now rejects:

- unknown `actionType` values
- `inputBindings` that do not match the registered action input schema

Validation runs in both:

- `CreateDeploymentProcessRevisionRequest.Validate()`
- `db.CreateDeploymentProcessRevision()`

This keeps HTTP callers and direct repository callers aligned.

## Database

No database migration is added in PR-017.

The registry is static code and does not persist action definitions.

## Compatibility

Existing Environment, Lifecycle, Channel, Release Bundle, Process Snapshot, Variable Set, deployment target, deployment, release-name, and agent behavior is unchanged.

Existing Deployment Process revision APIs remain the same shape. They now enforce that steps use the PR-017 built-in action types and schema-valid input bindings.

PR-017 adds no deployment planning, promotion execution, approval, retention, notification, runbook persistence, task queue, or agent behavior.
