# ADR-0051: Execution Preflight And Target-Component Locks

## Status

Accepted

## Context

An immutable deployment plan can become stale between creation and execution. A deployment target can change
platform or customer ownership, a lifecycle can make a release ineligible, and another deployment can change the
observed state of the same application component. The durable task queue previously created tasks directly from a
ready plan and serialized the entire deployment target, without retaining execution-time validation evidence.

External control-plane executors also need one stable concurrency identity per target and component. A target-only
lock prevents unsafe overlap but is too coarse to represent the service that owns observed state.

## Decision

Before task creation, execute a generic preflight under the same database transaction and advisory lock scope used
by the task queue. Persist one `DeploymentPreflightRun` and ordered `DeploymentPreflightCheck` records bound to the
plan checksum.

The preflight verifies:

- the stored canonical payload and current immutable plan state;
- current release lifecycle eligibility;
- the frozen release contract against published components and config checksums;
- target type, customer ownership, and platform;
- expected target-component state version and checksum;
- declared dependency versions and contracts;
- recorded migration and configuration-change flags.

A failed preflight commits its evidence and creates no tasks. A passed preflight is linked to the created tasks,
and the plan transitions from `READY` to `EXECUTED` without changing its canonical payload or checksum. Repeating
task creation for an executed plan returns the existing tasks.

Add `target_component` as a task lock resource using `<deployment-target-id>:<component>` as the stable key. Keep
the existing deployment-target lock for compatibility and conservative serialization. Both locks remain held for
the task's full running lifetime.

The API and Deployment Plan UI expose non-secret preflight metadata. Expected and actual values are limited to
identifiers, versions, platforms, checksums, booleans, and contract names; configuration values and credentials are
never persisted in this model.

## Consequences

Operators receive durable evidence explaining why execution passed or was blocked. Stale plans cannot silently
overwrite newer observed state, and task history now carries the component-level concurrency identity needed by
asynchronous external executors.

Migration 135 is additive. Deleting a task or deployment target clears only the optional link from historical
preflight checks, preserving the evidence itself. Downgrade removes preflight records and component locks before
restoring the previous task-lock constraint.

Legacy canonical plan payloads that included runtime status remain checksum-valid. New plans exclude runtime status
so the transition to `EXECUTED` does not mutate their immutable identity.

## Alternatives Considered

- Validate only when the plan is created. Rejected because target and observed state can change before execution.
- Store preflight only in task logs. Rejected because failed preflight creates no task and logs are retention-prone.
- Replace the target lock with component locks immediately. Rejected to preserve existing conservative concurrency
  behavior while the external-execution path is introduced.
- Store configuration documents in the checks. Rejected because checks need immutable identity, not secret-bearing
  content.

## Validation

- Pure evaluator tests cover pass, platform drift, state drift, target binding drift, lifecycle/contract failure,
  dependency version/contract failure, and canonical checksum mismatch.
- Repository tests cover persisted pass/fail evidence, zero tasks on failure, idempotent execution, plan status, and
  target-component locks. Live PostgreSQL execution requires `DISTR_TEST_DATABASE_URL`.
- API mapping and Angular tests cover public evidence and plan export/UI behavior.
- Migration tests cover reversible tables, lock constraints, and evidence-preserving foreign keys.
