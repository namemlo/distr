# PR-023 - Agent task leases

## Scope

PR-023 adds durable agent task leases on top of the Task Queue, locks, and agent capability protocol.

It adds:

- `TaskLease` rows scoped to an organization, Task, and deployment target agent.
- Hidden agent-authenticated lease and heartbeat endpoints.
- Short-lived opaque lease tokens stored only as hashes.
- Expired-lease reclaim with monotonically increasing attempts.
- Agent client helpers and generated manifest endpoint configuration.
- Repository and handler tests for claim, heartbeat, expiry, reclaim, and organization isolation.

## Feature flag

Agent lease endpoints are visible only when both flags are enabled:

```text
DISTR_EXPERIMENTAL_FEATURE_FLAGS=task_queue,agent_task_leases
```

If either flag is disabled, the hidden agent lease endpoints return `403 Forbidden`.

Generated Docker and Kubernetes manifests include:

```text
DISTR_LEASE_ENDPOINT
DISTR_TASK_HEARTBEAT_ENDPOINT_TEMPLATE
```

The agent client treats missing, disabled, or absent lease endpoints as no-op compatibility behavior. Existing agent loops do not execute leased tasks in PR-023.

## Database

Migration `124_task_leases` adds:

- `TaskLease`
- `task_id_target_organization_unique` on `Task`
- a partial unique index allowing at most one active lease per Task

`TaskLease` stores:

- organization ID
- task ID
- agent deployment target ID
- hashed lease token
- lease, expiry, heartbeat, attempt, and release timestamps

Composite foreign keys preserve organization isolation and ensure the leased agent matches the Task deployment target.

## API

Hidden agent-authenticated endpoints:

```http
POST /api/v1/agents/{id}/lease
POST /api/v1/agents/{id}/tasks/{taskId}/heartbeat
```

`POST /api/v1/agents/{id}/lease` claims the next queued Task for the authenticated deployment target when that Task has at least one included target-executed step. The claim runs in one database transaction, acquires existing task resource locks, marks the Task `RUNNING`, inserts a short-lived lease, and returns a payload containing the lease token and target-executed step metadata.

If no claimable work exists, the endpoint returns `204 No Content`.

`POST /api/v1/agents/{id}/tasks/{taskId}/heartbeat` accepts:

```json
{
  "leaseToken": "opaque-token"
}
```

An active heartbeat extends `heartbeatAt` and `expiresAt`. Missing or cross-organization leases return `404 Not Found`; expired leases return `409 Conflict`; invalid payloads return `400 Bad Request`.

## Reclaim behavior

If a Task is already `RUNNING` but its active lease is expired, a later lease claim releases the expired lease and creates a new lease for the same Task with the next attempt number. The Task remains `RUNNING`; resource locks remain held by the Task across attempts.

Hub-executed-only Tasks are not claimed by target agents.

## Non-goals

PR-023 does not add:

- step events or logs
- output storage
- task completion endpoints
- execution adapters
- release promotion execution
- approvals
- guided failure
- cancellation APIs
- Angular task queue UI
- PR-024 or later behavior

Those features remain later roadmap work.

## Compatibility notes

Existing Environment, Lifecycle, Channel, Release Bundle, Deployment Process, Process Snapshot, Variable Snapshot, Deployment Plan preview/UI, Task Queue read/create APIs, locks/concurrency semantics, deployment target, deployment, release-name, frontend planning UI, and current agent deployment behavior are unchanged.
