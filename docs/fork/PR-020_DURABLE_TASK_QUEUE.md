# PR-020 - Durable task queue

## Scope

PR-020 adds the first durable task queue foundation for Deployment Plans.

It adds:

- `Task` records created from READY Deployment Plans.
- `StepRun` records created from Deployment Plan steps.
- Deterministic queue ordering through durable `queueOrder` values.
- Idempotent task creation per Deployment Plan target.
- Read APIs for queued tasks.
- A guarded task state transition API.
- Repository-only StepRun state transition support for later agent work.

## Feature flag

The API is visible only when the full planning stack plus the task queue flag is enabled:

```text
DISTR_EXPERIMENTAL_FEATURE_FLAGS=task_queue,deployment_plans,release_bundles,deployment_processes,scoped_variables_v2,channels,lifecycles,environments
```

If any required flag is disabled, task queue endpoints return `403 Forbidden`.

## Database

Migration `121_task_queue` adds:

- `Task_queue_order_seq`
- `Task`
- `StepRun`

`Task` stores:

- organization scope
- Deployment Plan reference
- Deployment Plan target reference
- Deployment Target reference
- Application, Release Bundle, Channel, and Environment scope copied from the plan
- task status
- durable queue order
- queued, started, and completed timestamps

`StepRun` stores:

- organization scope
- Task reference
- Deployment Plan step reference
- step key, name, action type, sort order, skipped reason
- step-run status
- started and completed timestamps

Composite foreign keys preserve organization isolation and ensure Task and StepRun rows reference the matching Deployment Plan graph. The down migration removes only PR-020 tables, constraints, indexes, and sequence.

## API

Endpoints:

```http
POST /api/v1/deployment-plans/{deploymentPlanId}/tasks
GET  /api/v1/tasks
GET  /api/v1/tasks/{taskId}
POST /api/v1/tasks/{taskId}/state
```

`POST /api/v1/deployment-plans/{deploymentPlanId}/tasks` creates one Task for each Deployment Plan target and one StepRun for each Deployment Plan step. Repeating the request returns the existing Tasks without creating duplicates.

Before creating new Tasks, the repository rechecks the referenced Release Bundle inside the same database transaction with a row lock. If the bundle is no longer `PUBLISHED`, creation returns `409 Conflict`.

`POST /api/v1/tasks/{taskId}/state` accepts:

```json
{
  "status": "RUNNING"
}
```

Allowed Task transitions:

```text
QUEUED -> RUNNING
RUNNING -> SUCCEEDED
RUNNING -> FAILED
```

Allowed StepRun transitions in the repository:

```text
PENDING -> RUNNING
PENDING -> SKIPPED
RUNNING -> SUCCEEDED
RUNNING -> FAILED
```

Invalid transitions return `409 Conflict`. Missing or cross-organization resources return `404 Not Found`. Malformed UUIDs return `404 Not Found`. Invalid payloads return `400 Bad Request`.

## Non-goals

PR-020 does not add:

- locks or concurrency policies
- queue/reject/cancel policy handling
- agent capability protocol
- leases or heartbeats
- agent task endpoints
- execution adapters
- approvals
- task cancellation or guided failure
- step events, logs, or timeline APIs
- deployment target mutation
- Angular task queue UI

Those features remain PR-021 or later roadmap work.

## Compatibility notes

Existing Environment, Lifecycle, Channel, Release Bundle, Deployment Process, Process Snapshot, Variable Snapshot, Deployment Plan preview, deployment target, deployment, release-name, frontend planning UI, and agent behavior is unchanged.
