# ADR-0007: Release Validation and Publication

## Status

Accepted for PR-007.

## Context

PR-006 introduced draft Release Bundle persistence, component storage, and canonical checksum calculation. The roadmap next requires a generic validation and publication layer before any UI, CI, promotion, deployment planning, approval, retention, execution, or agent behavior is added.

PR-007 must keep Release Bundles organization-scoped and reusable while enforcing that published bundles are immutable release snapshots.

## Decision

Add a Release Bundle validation service in `internal/releasebundles` and repository-level validation orchestration in `internal/db`.

Validation returns structured errors and warnings. The initial checks cover:

- canonical checksum consistency;
- component presence and key uniqueness;
- component type requirements;
- organization-scoped application-version references;
- Channel/Application/Organization consistency already enforced by PR-006 database constraints;
- PR-005 Channel version and source rules for application-version components;
- child Release Bundle references in the same organization;
- published-only child Release Bundle references.

Add publication metadata to `ReleaseBundle`:

- `published_by_user_account_id`
- `published_at`

Add an append-only `ReleaseBundleAuditEvent` table for publish, block, archive, and rejected transition events.

Expose feature-flagged endpoints:

- `POST /api/v1/release-bundles/{releaseBundleId}/validate`
- `POST /api/v1/release-bundles/{releaseBundleId}/publish`
- `POST /api/v1/release-bundles/{releaseBundleId}/block`
- `POST /api/v1/release-bundles/{releaseBundleId}/archive`

Publish is allowed only from `DRAFT` and only when validation succeeds. Publish sets status to `PUBLISHED`, records actor/time, and writes a `published` audit event.

Block is allowed only from `PUBLISHED`. Archive is allowed from `PUBLISHED` or `BLOCKED`.

Invalid transitions write `state_transition_rejected` audit events. Publish validation failures also write `state_transition_rejected` audit events and return the validation response with HTTP 400.

Published, blocked, and archived bundles remain immutable because existing update/delete operations reject non-draft rows.

## Consequences

- Release Bundle callers can validate drafts before publication.
- Published bundles have actor/time metadata.
- Published, blocked, and archived bundles cannot be edited or deleted through existing mutation APIs.
- State changes and rejected state changes leave audit evidence.
- Cross-organization reads and mutations continue to return not found.
- Channel source branch/tag rules can make publication fail until later PRs add source metadata capture to release creation.
- PR-007 does not add Release UI, CI APIs, lifecycle eligibility, promotion, deployment planning, approvals, retention, execution, notifications, or agent changes.
