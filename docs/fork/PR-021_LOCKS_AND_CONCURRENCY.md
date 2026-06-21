# PR-021 - Locks and concurrency

## Scope

PR-021 adds resource locks and task concurrency policies on top of the durable Task Queue.

It adds:

- `TaskResourceLock` rows attached to Tasks.
- Default deployment-target locks for every Task.
- Optional additional generic lock resources during Task creation.
- Concurrency policies: `QUEUE`, `REJECT_NEW`, `CANCEL_OLDER`, and `ALLOW_PARALLEL`.
- Lock acquisition during `QUEUED -> RUNNING` Task transitions.
- Lock release when Tasks enter terminal states.
- Transaction-scoped PostgreSQL advisory guards around resource policy checks.
- Race-condition tests for concurrent Task creation and lock acquisition.

## Feature flag

The API remains visible only when the full planning stack plus the task queue flag is enabled:

```text
DISTR_EXPERIMENTAL_FEATURE_FLAGS=task_queue,deployment_plans,release_bundles,deployment_processes,scoped_variables_v2,channels,lifecycles,environments
```

If any required flag is disabled, task queue endpoints return `403 Forbidden`.

## Database

Migration `122_task_locks` adds:

- `TaskResourceLock`
- `CANCELED` as a terminal Task status

`TaskResourceLock` stores:

- organization scope
- Task reference
- resource type
- resource key
- concurrency policy
- acquired and released timestamps

Supported resource types are:

```text
deployment_target
tenant_environment
application_environment
custom
```

The migration backfills `QUEUE` deployment-target locks for existing Tasks.

## API

`POST /api/v1/deployment-plans/{deploymentPlanId}/tasks` now accepts an optional body:

```json
{
  "concurrencyPolicy": "QUEUE",
  "lockResources": [
    {
      "resourceType": "custom",
      "resourceKey": "shared-db",
      "concurrencyPolicy": "REJECT_NEW"
    }
  ]
}
```

If omitted, `concurrencyPolicy` defaults to `QUEUE`.

Each Task automatically receives a `deployment_target` lock keyed by its Deployment Target ID. Additional lock resources are applied to each Task created for the plan.

Task responses now include `locks`.

## Policy semantics

`QUEUE`:
Creates the Task. A `QUEUED -> RUNNING` transition fails with `409 Conflict` while another running Task holds the same resource.

`REJECT_NEW`:
Rejects Task creation with `409 Conflict` when an existing queued or running Task already references the same resource. Concurrent creates for the same resource are serialized before insertion.

`CANCEL_OLDER`:
Cancels older queued Tasks that reference the same resource, releases their locks, and creates the new Task. Running Tasks are not canceled. Concurrent creates for the same resource are serialized before insertion.

`ALLOW_PARALLEL`:
Allows concurrent running Tasks for the same resource.

## Non-goals

PR-021 does not add:

- agent capability protocol
- leases or heartbeats
- agent task endpoints
- execution adapters
- approvals
- guided failure
- step events, logs, or timeline APIs
- Angular task queue UI
- deployment target mutation

Those features remain PR-022 or later roadmap work.

## Compatibility notes

Existing Environment, Lifecycle, Channel, Release Bundle, Deployment Process, Process Snapshot, Variable Snapshot, Deployment Plan preview/UI, deployment target, deployment, release-name, frontend planning UI, and agent behavior is unchanged.
