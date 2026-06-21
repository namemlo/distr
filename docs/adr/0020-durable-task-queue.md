# ADR-0020 - Durable task queue

## Status

Accepted

## Context

PR-018 introduced durable Deployment Plans, and PR-019 exposed those plans in the Angular administration UI. The roadmap next needs durable queue records that can survive Hub restarts and later support leases, locks, agent work, and execution.

PR-020 must stop at the queue foundation. It must not implement PR-021 locks/concurrency, PR-022 agent capabilities, PR-023 leases/heartbeats, execution adapters, approvals, cancellation, guided failure, logs, timelines, or agent protocol changes.

## Decision

Add a feature-flagged durable Task Queue model:

- `Task` rows are created from READY Deployment Plans.
- Each Deployment Plan target receives one Task.
- Each Deployment Plan step receives one StepRun per Task.
- Included plan steps start as `PENDING`.
- Excluded plan steps start as `SKIPPED` with the plan excluded reason.
- Task creation is idempotent per Deployment Plan target.
- Queue ordering is stored in a sequence-backed `queue_order` column.
- Reads return tasks in queue order.
- Task state transitions are validated by a small state machine.
- StepRun state transitions are repository-only for later agent work.

The public API surface is:

```http
POST /api/v1/deployment-plans/{deploymentPlanId}/tasks
GET  /api/v1/tasks
GET  /api/v1/tasks/{taskId}
POST /api/v1/tasks/{taskId}/state
```

The API requires:

```text
task_queue
deployment_plans
release_bundles
deployment_processes
scoped_variables_v2
channels
lifecycles
environments
```

## Consequences

- The Hub can now store durable task and step-run records without executing deployment actions.
- Queue ordering is deterministic and survives process restarts.
- Repeated enqueue requests for the same plan are safe and return existing records.
- Organization isolation is enforced by repository lookups and composite database foreign keys.
- Later PRs can build locks, leases, heartbeats, agent endpoints, events, logs, and adapters on top of this model.
- Existing Deployment Plan preview behavior remains unchanged.

## Alternatives Considered

Using only `created_at` ordering was rejected because timestamp ties can make queue ordering ambiguous under fast inserts.

Creating tasks for BLOCKED plans was rejected because task records represent executable queue candidates and blockers should remain inspectable on the Deployment Plan.

Adding leases, heartbeats, lock resources, or agent endpoints now was rejected because those behaviors are assigned to PR-021 through PR-023.
