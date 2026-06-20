# PR-010 Lifecycle Eligibility Engine

## Scope

PR-010 replaces the PR-003 eligibility skeleton with a read-only lifecycle eligibility engine and explanation API for published Release Bundles.

Included:

- Required and optional lifecycle phase evaluation.
- Deterministic phase ordering by sort order.
- Organization-scoped Release Bundle, Channel, Lifecycle, and Environment reads.
- Structured blocking reasons for release status, lifecycle mismatch, missing target environment, required prior phases, and approval-required placeholders.
- Feature-gated API access that requires the Release Bundle, Channel, Lifecycle, and Environment experimental flags.
- Backend, mapping, handler, repository, and live PostgreSQL tests.

Excluded:

- Promotion execution.
- Deployment planning or deployment creation.
- Deployment history persistence.
- Approval workflows.
- Freeze windows.
- Retention behavior.
- Notifications.
- UI workflow changes.
- Agent behavior or protocol changes.

Those features remain PR-011 or later roadmap work.

## Generic User Story

As a release operator, I can ask whether a published Release Bundle is eligible for a lifecycle environment and receive a deterministic explanation of every blocking reason before any promotion or deployment behavior exists.

## Feature Flags

The endpoint requires all of these experimental feature flags:

- `release_bundles`
- `channels`
- `lifecycles`
- `environments`

When any required flag is disabled, the endpoint returns `403 Forbidden`.

## API Contract

```http
GET /api/v1/release-bundles/{releaseBundleId}/eligibility?environmentId={environmentId}
```

The roadmap names `/api/v1/releases/{id}/eligibility`, but the implemented fork domain uses Release Bundles as the release unit. PR-010 therefore attaches the explanation API to the existing Release Bundle API.

Malformed `releaseBundleId` path values return `404 Not Found`, matching the existing Release Bundle route convention. Missing or malformed `environmentId` query values return `400 Bad Request`.

Missing or cross-organization Release Bundles and Environments return `404 Not Found`.

Successful responses include:

- Release Bundle, application, channel, lifecycle, and environment IDs.
- `engineReady`.
- `eligible`.
- `targetPhase`, when the environment belongs to a lifecycle phase.
- ordered `phases` with optional, automatic-promotion, minimum-successful-deployment, approval, retention, target-match, required-before-target, and blocking flags.
- structured `reasons`.

Example blocking reason:

```json
{
  "code": "required_prior_phase_incomplete",
  "field": "phases.00000000-0000-0000-0000-000000000000",
  "message": "required lifecycle phase \"Development\" has no successful deployment evidence for this release bundle"
}
```

## Eligibility Rules

- Only `PUBLISHED` Release Bundles can be eligible.
- `DRAFT` and `VALIDATING` Release Bundles are blocked with `release_not_published`.
- `BLOCKED` Release Bundles are blocked with `release_blocked`.
- `ARCHIVED` Release Bundles are blocked with `release_archived`.
- The Channel lifecycle must match the evaluated Lifecycle.
- The requested Environment must belong to a Lifecycle phase.
- Optional prior phases do not block eligibility.
- Required prior phases block eligibility until later roadmap work supplies successful deployment evidence.
- A target phase with an approval policy blocks eligibility and reports `approval_required`; approval evaluation is intentionally not implemented in PR-010.

## Database

PR-010 adds no migrations.

The repository composes existing organization-scoped reads:

- Release Bundle by ID and organization.
- Environment by ID and organization.
- Channel by Release Bundle channel ID and organization.
- Lifecycle by Channel lifecycle ID and organization.

## Compatibility

Existing Environment, Lifecycle, Channel, Release Bundle, deployment target, deployment, release-name, and agent behavior is unchanged.

PR-010 is read-only. It does not mutate releases, channels, lifecycles, deployments, deployment targets, or agents.
