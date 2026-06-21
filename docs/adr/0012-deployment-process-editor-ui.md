# ADR-0012: Deployment Process Editor UI

## Status

Accepted for PR-012.

## Context

PR-011 introduced the Deployment Process schema and feature-gated API. The roadmap next requires an administration UI for process metadata, revision history, and structured step revision creation without adding snapshots, action registry behavior, release links, planning, execution, approvals, retention, notifications, or agent changes.

## Decision

Add a vendor-admin Angular route at `/deployment-processes`.

The route and sidebar entry are gated by the `deployment_processes` experimental feature flag and the existing prerequisite environment, lifecycle, and channel flags. The page loads deployment processes, applications, channels, and environments through existing organization-scoped APIs.

The UI provides:

- list, loading, empty, and API-error states;
- create, update, and delete process actions;
- revision history and revision detail views;
- structured revision creation with ordered steps;
- client-side JSON-object validation for step input bindings;
- scoped Channel selectors filtered to the selected process application;
- Environment selectors using organization-scoped environment data.

The backend remains the source of truth for organization isolation, duplicate-name validation, step dependency validation, scoped Channel and Environment references, and append-only revision behavior.

## Consequences

- Vendor admins can manage Deployment Processes through the community UI when the roadmap flags are enabled.
- No database migration or backend API shape changes are required.
- Deployment Process behavior remains generic and provider-neutral.
- PR-012 does not add process snapshots, Release Bundle links, variables, deployment planning, approvals, retention, execution, notifications, or agent changes.
