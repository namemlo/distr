# ADR-0008: Release UI

## Status

Accepted for PR-008.

## Context

PR-006 introduced Release Bundle draft CRUD and PR-007 introduced validation, publication, block, and archive endpoints. The roadmap next requires an administration UI for those existing backend capabilities without adding CI APIs, promotion, lifecycle eligibility, deployment planning, approvals, retention, execution, or agent behavior.

## Decision

Add a vendor-admin Angular route at `/release-bundles`.

The route and sidebar entry are gated by the `release_bundles` experimental feature flag and the existing prerequisite environment, lifecycle, and channel flags. The page loads release bundles, applications, and channels through the existing organization-scoped APIs.

The UI provides:

- list, loading, empty, and API-error states;
- draft create and edit forms;
- a component editor for the existing generic component types;
- read-only detail display for non-draft releases;
- validation result display using the PR-007 structured errors and warnings;
- validate-before-publish confirmation flow;
- status-aware delete, block, and archive actions.

Draft editing remains an interface affordance only. The backend remains the source of truth for immutability, organization isolation, validation, and allowed state transitions.

## Consequences

- Vendor admins can manage Release Bundles through the community UI when the roadmap flags are enabled.
- No database migration or backend API shape changes are required.
- Release Bundle behavior remains generic and provider-neutral.
- PR-008 does not add CI release APIs, lifecycle eligibility, promotion, deployment planning, approvals, retention, execution, notifications, or agent changes.
