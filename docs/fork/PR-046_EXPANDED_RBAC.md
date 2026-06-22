# PR-046 - Expanded RBAC

## Summary

PR-046 adds a permission-based authorization foundation on top of the existing organization user roles. The first slice keeps the persisted roles compatible while introducing named permission families, organization-scoped permission checks, and tests that prove read-only users cannot cross into mutation permissions.

## Scope

- Adds permission constants for the RBAC families in the fork roadmap.
- Adds built-in role definitions for `Viewer`, `Developer`, and `Administrator`.
- Maps existing persisted roles to those built-in definitions:
  - `read_only` maps to `Viewer`.
  - `read_write` maps to `Developer`.
  - `admin` maps to `Administrator`.
- Adds organization-scoped permission middleware.
- Rewires the existing `RequireReadWriteOrAdmin` middleware alias through mutation permissions.
- Keeps exact role middleware available for admin-only or legacy role checks.

## Compatibility

- No database migration is required.
- Existing JWT and access-token role claims remain unchanged.
- Existing API route wiring remains compatible; mutation endpoints that already used `RequireReadWriteOrAdmin` now pass through the permission layer.
- Super admin behavior is unchanged: super admins may pass role checks and are still blocked by existing `BlockSuperAdmin` middleware where mutation routes require that.

## Deferred Work

- Application, environment, tenant/customer, and tag-set scopes are known to the type model but not supported by runtime enforcement until those resources have a policy binding model.
- Additional named roles such as Release Manager, Deployment Operator, Approver, Runbook Operator, and Environment Manager can be added after role assignment storage and UI are designed.
- Audit event storage and before/after metadata are handled by a later audit-specific PR.

## Verification

- Type tests cover built-in role permissions, permission parsing, and isolated role-definition copies.
- Middleware tests cover organization-scoped permission allow/deny behavior, super-admin compatibility, and unsupported scope rejection.
