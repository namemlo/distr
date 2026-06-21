# ADR-0021 - Task locks and concurrency

## Status

Accepted

## Context

PR-020 introduced durable Task and StepRun rows but intentionally did not prevent two Tasks from mutating the same resource at the same time. PR-021 adds the first concurrency boundary while still avoiding agent leases, heartbeats, execution adapters, approvals, and protocol changes.

## Decision

Add `TaskResourceLock` as a durable lock request attached to a Task.

Each Task receives a default `deployment_target` lock. API callers may also provide generic lock resources for tenant/environment, application/environment, or custom named resources.

Supported policies are:

- `QUEUE`
- `REJECT_NEW`
- `CANCEL_OLDER`
- `ALLOW_PARALLEL`

Lock enforcement happens in two places:

- Task creation applies `REJECT_NEW` and `CANCEL_OLDER` against queued or running Tasks.
- `QUEUED -> RUNNING` acquisition locks all rows for each resource key before checking active running holders.

Task terminal transitions release acquired locks. `CANCELED` is a terminal status used by concurrency policy cancellation.

## Consequences

- Two Tasks cannot both enter `RUNNING` for the same exclusive resource.
- Existing queued work can be rejected or canceled by policy without adding a general cancellation endpoint.
- Existing Tasks receive default target locks during migration.
- Later agent lease and execution PRs can reuse these durable locks without changing their basic shape.

## Alternatives Considered

Using only a partial unique index for active locks was rejected because `ALLOW_PARALLEL` and `QUEUE` need policy-aware behavior.

Using only PostgreSQL advisory locks was rejected because the lock state must survive Hub restarts and be inspectable through the API.

Implementing leases, heartbeats, or agent task claims now was rejected because those behaviors belong to PR-023 and later roadmap work.
