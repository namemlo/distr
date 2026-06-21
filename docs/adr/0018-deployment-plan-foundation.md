# ADR-0018: Deployment Plan Foundation

## Status

Accepted for PR-018.

## Context

PR-013 introduced immutable Process Snapshots linked to Release Bundles. PR-016 introduced immutable Variable Snapshots. PR-017 introduced the first built-in action registry and JSON-schema validation for Deployment Process steps.

The roadmap next requires a Deployment Plan foundation that can resolve a release, process, variables, actions, and selected targets before execution. It must remain backend-only and must not add the PR-019 plan UI/export work or later task queue, lock, approval, lease, execution, or agent protocol behavior.

## Decision

Add feature-flagged Deployment Plan tables and APIs:

```http
GET  /api/v1/deployment-plans
POST /api/v1/deployment-plans
GET  /api/v1/deployment-plans/{deploymentPlanId}
```

Plan creation accepts a Release Bundle ID, Environment ID, and explicit Deployment Target IDs. The planner resolves and persists:

- immutable Release Bundle scope
- immutable Process Snapshot reference and steps
- immutable Variable Snapshot reference and values
- built-in action metadata for each process step
- selected Deployment Target identity
- structured blocker and warning issues
- canonical payload and checksum

Plans are persisted even when blocked. This lets API callers inspect why a plan cannot proceed without starting any execution workflow. A plan with blocker issues is stored as `BLOCKED`; a plan without blocker issues is stored as `READY`.

Target selection is explicit in PR-018. Automatic target selection by tags, rollout groups, waves, tenant expressions, or environment assignments is deferred to later roadmap PRs.

The API requires all dependent experimental flags:

```text
deployment_plans
release_bundles
deployment_processes
scoped_variables_v2
channels
lifecycles
environments
```

## Consequences

- Deployment Plans now have durable backend state that later UI, export, approvals, and task queue work can reference.
- Plans preserve organization isolation through both repository validation and composite database foreign keys.
- Secret values remain redacted because the planner copies Variable Snapshot metadata and redacted values only.
- Lifecycle eligibility, missing snapshots, unresolved required variables, invalid actions, and no applicable steps are represented as structured blockers.
- PR-018 does not mutate targets, create deployments, enqueue tasks, acquire locks, evaluate approvals, contact agents, or execute dry runs.

## Alternatives Considered

Creating plans entirely in memory was rejected because later approvals, exports, and task creation need a durable reference.

Automatically selecting targets was rejected because target tags, rollout groups, waves, and target-environment assignment semantics belong to later roadmap PRs.

Treating blocked plans as failed API calls was rejected because the planner's purpose is to explain blockers in a stable, inspectable form.

Adding UI or export support now was rejected because PR-019 owns plan UI, JSON/Markdown export, and checksum display.
