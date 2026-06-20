# PR-003 - Lifecycle Domain Model

## User Story

As a Hub administrator, I want to define reusable promotion lifecycles with ordered phases so future releases can move through a consistent environment model.

## Scope

PR-003 adds only the Lifecycle domain model, phase editor, and eligibility service skeleton. It does not add channel links, release bundles, deployment plans, approvals, retention behavior, automatic deployments, or promotion execution.

## Feature Flag

The API is guarded by the `lifecycles` experimental feature flag from PR-001.

The UI route requires both `environments` and `lifecycles` because phase editing selects environments from the PR-002 Environment model.

Example:

```shell
DISTR_EXPERIMENTAL_FEATURE_FLAGS=environments,lifecycles
```

## Data Model

Adds `Lifecycle` with:

- `id`
- `created_at`
- `updated_at`
- `organization_id`
- `name`
- `description`
- `sort_order`

Adds `LifecyclePhase` with:

- `id`
- `lifecycle_id`
- `name`
- `description`
- `sort_order`
- `optional`
- `automatic_promotion`
- `minimum_successful_deployments`
- `approval_policy_id`
- `retention_policy_id`

Adds `LifecyclePhaseEnvironment` to assign one or more existing environments to a phase.

Lifecycle names are unique per organization. Phase names and phase sort orders are unique per lifecycle. Sort orders and minimum successful deployments must be non-negative.

## API

Admin/vendor scoped endpoints:

```http
GET    /api/v1/lifecycles
POST   /api/v1/lifecycles
GET    /api/v1/lifecycles/{lifecycleId}
PUT    /api/v1/lifecycles/{lifecycleId}
DELETE /api/v1/lifecycles/{lifecycleId}
GET    /api/v1/lifecycles/{lifecycleId}/phases
PUT    /api/v1/lifecycles/{lifecycleId}/phases
```

Writes require read-write or admin role and are blocked for super admins.

## UI

Adds an admin-only Lifecycles page with:

- loading state;
- error/retry state;
- empty state;
- list/filter table;
- create dialog;
- update dialog;
- dynamic phase editor;
- delete confirmation.

## Eligibility Skeleton

Adds an internal eligibility service skeleton that orders phases and identifies which phases reference the requested environment. It intentionally reports that release, channel, and deployment history models are not available yet.

## Compatibility

Existing deployment target, deployment, release, and agent behavior is preserved. PR-003 does not alter deployment execution, target selection, release publication, promotion checks, or agent protocol.

## Tests

Focused checks added for:

- API request validation;
- internal-to-API mapping;
- handler conversion helpers;
- lifecycle eligibility skeleton ordering and explanation;
- Angular lifecycle service endpoint behavior.
