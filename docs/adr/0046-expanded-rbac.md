# ADR-0046 - Expanded RBAC

## Status

Accepted

## Context

The fork roadmap calls for scoped permissions, built-in roles, authorization tests, and later audit coverage. The current application already stores organization-level roles as `read_only`, `read_write`, and `admin`, and many mutation endpoints depend on shared middleware aliases. Replacing persisted roles in one PR would require database migrations, frontend role-management changes, token-claim compatibility work, and a migration path for existing access tokens.

## Decision

Introduce a named permission model while preserving the existing persisted role values. PR-046 defines the RBAC permission families from the roadmap, records known scope names, and supports runtime enforcement for organization-scoped permissions only.

The built-in role mapping is:

- `Viewer` for `read_only`, with read and audit-view permissions.
- `Developer` for `read_write`, with the existing non-admin mutation capability set.
- `Administrator` for `admin`, with all permissions.

The existing `RequireReadWriteOrAdmin` middleware alias is rewired through mutation permissions. This gives current mutation routes a permission-backed server-side check without changing route behavior or token claims.

Exact role middleware remains available for admin-only or legacy checks. New resource authorization should use scoped permission middleware so future PRs can replace generic mutation gates with route-specific permissions incrementally.

## Consequences

- The authorization model can move from coarse role names to named permissions incrementally.
- Existing users, personal access tokens, JWT claims, and route behavior remain compatible.
- Future PRs can replace generic mutation gates with route-specific permissions without another migration.
- Known but unsupported scopes are rejected until there is a resource binding model for application, environment, tenant/customer, and tag-set scopes.

## Alternatives Considered

- Add all suggested roles immediately: rejected because role assignment storage and UI would be larger than the roadmap slice.
- Add a policy language: rejected because the roadmap explicitly asks for a manageable subset that remains auditable.
- Keep role-only middleware: rejected because it would not create a concrete server-side permission layer for later RBAC expansion.
