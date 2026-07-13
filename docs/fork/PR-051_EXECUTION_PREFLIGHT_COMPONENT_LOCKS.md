# PR-051 - Execution Preflight And Target-Component Locks

## Generic User Story

As a deployment operator, I want the control plane to revalidate an immutable plan immediately before execution and
retain the result, so that stale target, release, contract, dependency, or observed-state changes block execution
with an auditable explanation.

## Scope

- Add persisted execution preflight runs and ordered checks.
- Re-evaluate canonical plan state, lifecycle eligibility, release contract, target binding/platform, dependencies,
  and expected component state under task-creation locks.
- Commit failed evidence without creating tasks.
- Link passed checks to tasks and transition the plan to `EXECUTED` without checksum drift.
- Add target-component task locks while retaining existing deployment-target locks.
- Expose preflight evidence through the Deployment Plan API, UI, JSON export, and Markdown export.

## Required Impact Report

### Database/schema impact

Migration 135 adds `DeploymentPreflightRun` and `DeploymentPreflightCheck`, and extends the task lock resource-type
constraint with `target_component`. Foreign keys are organization-scoped. Optional task and target links use
column-specific `SET NULL` behavior so historical evidence survives retention or target removal.

### Public API impact

Deployment Plan responses add `preflightRuns`, containing run status, plan checksum, actor, ordered checks, optional
task/target/component links, expected and actual non-secret metadata, and messages. Existing request payloads and
routes are unchanged.

### Frontend/UI impact

Deployment Plan detail shows the latest execution preflight checklist and its checksum. JSON and Markdown exports
include the same evidence.

### Agent/protocol impact

None. Preflight and component lock creation occur in the Hub before task execution.

### Feature-flag impact

No new flag. The behavior is reachable only through the existing deployment plan and task queue feature surfaces.

### Security impact

Positive. Execution is blocked on stale state and binding drift. Evidence is organization-scoped and excludes secret
values, request credentials, and configuration content.

### Backward-compatibility impact

Existing plans with legacy canonical payloads remain executable when their stored checksum is valid. Existing task
locks remain present; component locks add stricter service-level identity without weakening target serialization.

## Validation

- `go test ./internal/deploymentpreflight ./internal/db ./internal/mapping`
- Focused Angular Deployment Plan and Deployment Timeline tests
- Community frontend and Hub builds
- Full Go suite and live PostgreSQL migration/repository tests before deployment
