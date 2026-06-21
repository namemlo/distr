# PR-018 Deployment Plan Foundation

## Scope

PR-018 adds the first backend-only Deployment Plan foundation.

Included:

- Organization-scoped Deployment Plan persistence.
- Explicit selected-target resolution from Deployment Target IDs.
- Immutable Release Bundle, Process Snapshot, and Variable Snapshot references.
- Resolved process steps with built-in action metadata from the action registry.
- Resolved variable snapshot values with secret values redacted.
- Structured blocker and warning issues.
- Read-only plan list/get API plus create API for resolved previews.
- Backend, API, handler, mapping, live PostgreSQL repository, and migration tests.

Excluded:

- Plan UI.
- JSON or Markdown export.
- User-facing checksum display.
- Task queue, execution, locks, leases, approvals, step events, notifications, runbooks, rollout waves, or agent protocol changes.
- Target mutation or remote dry-run execution.

Those features remain PR-019 or later roadmap work.

## Feature Flags

The Deployment Plan API is gated by all required foundation flags:

```text
DISTR_EXPERIMENTAL_FEATURE_FLAGS=deployment_plans,release_bundles,deployment_processes,scoped_variables_v2,channels,lifecycles,environments
```

If any required flag is disabled, `/api/v1/deployment-plans` returns `403 Forbidden`.

## Database

Migration `120_deployment_plans` adds:

- `DeploymentPlan`
- `DeploymentPlanTarget`
- `DeploymentPlanStep`
- `DeploymentPlanVariable`
- `DeploymentPlanIssue`

`DeploymentPlan` stores:

- organization, application, release bundle, channel, and environment scope
- optional immutable process snapshot and variable snapshot references
- plan status
- canonical payload and checksum

Composite foreign keys enforce:

- plans reference Release Bundles in the same organization/application/channel
- plans reference Environments in the same organization
- plans reference Process Snapshots in the same organization/application
- plans reference Variable Snapshots in the same organization/application/channel
- plan targets reference Deployment Targets in the same organization

The down migration removes the PR-018 tables only. It does not alter existing Release Bundle, Process Snapshot, Variable Snapshot, Environment, Lifecycle, Channel, Deployment Process, Deployment Target, deployment, release-name, or agent tables.

## API

Endpoints:

```http
GET  /api/v1/deployment-plans
POST /api/v1/deployment-plans
GET  /api/v1/deployment-plans/{deploymentPlanId}
```

Create request:

```json
{
  "releaseBundleId": "00000000-0000-0000-0000-000000000000",
  "environmentId": "00000000-0000-0000-0000-000000000000",
  "targetIds": ["00000000-0000-0000-0000-000000000000"]
}
```

Validation:

- `releaseBundleId` is required.
- `environmentId` is required.
- at least one `targetId` is required.
- empty target IDs are rejected.
- duplicate target IDs are rejected.
- malformed path UUIDs return `404 Not Found`.
- missing or cross-organization resources return `404 Not Found`.

## Planning Semantics

Plan creation is transactional. The planner:

1. Loads the Release Bundle inside the caller organization.
2. Validates the selected Environment.
3. Resolves selected Deployment Targets by organization.
4. Evaluates existing lifecycle eligibility.
5. Copies Process Snapshot steps and action registry metadata.
6. Copies Variable Snapshot values without exposing plaintext secrets.
7. Adds blocker issues for missing snapshots, lifecycle ineligibility, unknown actions, invalid action inputs, no applicable steps, and unresolved required variables.
8. Adds a warning that remote dry-run checks are not performed in PR-018.
9. Stores the immutable resolved plan rows and canonical checksum.

Blocked plans are persisted with `BLOCKED` status so callers can inspect the exact blocker list. Plans without blocker issues are persisted with `READY` status.

Target selection is explicit in PR-018. Automatic target selection by tags, rollout groups, waves, or tenant expressions belongs to later roadmap PRs.

## Compatibility

Existing Environment, Lifecycle, Channel, Release Bundle, Deployment Process, Process Snapshot, Variable Snapshot, deployment target, deployment, release-name, and agent behavior is unchanged.

PR-018 is read-only with respect to deployments and targets. It does not create tasks or mutate remote systems.
