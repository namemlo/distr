# PR-066 - Scoped Authorization and Feature Enrollment

## Generic User Story

As an operator administrator, I want action-specific roles, scoped user or group bindings, and organization plus
environment enrollment so that a team can operate only its assigned control-plane resources and a process flag
alone can never grant tenant authority.

## Scope

- Add organization-owned role definitions, action permissions, user/group bindings, group membership, and
  enrollment revisions.
- Resolve organization, customer, environment, deployment-unit, component, and campaign authorization scopes.
- Keep the legacy `Organization_UserAccount.user_role` path unchanged and use it as a dual-read fallback.
- Backfill the three existing roles idempotently and retain an explicit completion checkpoint.
- Add action-specific v2 middleware and additive authorization administration APIs.
- Require the process flag plus active organization and selected-environment enrollment before effective v2
  operations.
- Return tenant-safe not-found or generic denied responses without exposing a foreign identifier.

## Built-In Compatibility Roles

| Legacy role  | v2 compatibility authority |
| ------------ | -------------------------- |
| `read_only`  | Audit view and export |
| `read_write` | Release create/publish, registry/config manage, plan create/publish/execute, campaign control, audit view/export |
| `admin`      | Every registered control-plane action, including authorization administration |

Developer and administrator are deliberately distinct. Custom immutable roles can narrow authority further.

## Effective Authorization

An action is allowed only when an effective direct or group binding contains the action and its exact scope
matches the resource or a resolved ancestor. If no scoped binding matches, the legacy compatibility role is read
without changing its row. Every decision is deny-by-default and returns deterministically ordered matching binding
IDs.

An execution-capable v2 route additionally requires:

```text
operator_control_plane_v2 enabled
AND latest effective organization enrollment enabled
AND latest effective selected-environment enrollment enabled
```

Enrollment changes append a revision. A later disabled revision provides rollback without deleting the earlier
reason, actor, or interval.

## Required Impact Report

### Database/schema impact

Migration 148 adds `RoleDefinition`, `RolePermission`, `RoleBinding`, `PrincipalGroup`,
`PrincipalGroupMember`, `ControlPlaneEnrollment`, and `AuthorizationBackfillCheckpoint`. It inserts checkpointed
built-in role definitions and their action permissions. It does not duplicate or alter current
`Organization_UserAccount` memberships.

The down migration refuses after any custom authorization or enrollment evidence exists. Compatibility-only
backfill data can be removed when unused.

### Public API impact

Adds admin collections under `/api/v1/authorization` for roles, bindings, groups, group members, and
control-plane enrollments. Collections support GET and append-only POST; no update or delete API is exposed.

### Frontend/UI impact

None in PR-066. PR-080 consumes these APIs for role-aware operator screens.

### Agent/protocol impact

None. Existing agents and ADR-0052 protocol v1 are unchanged.

### Feature-flag impact

`operator_control_plane_v2` remains the process kill switch. PR-066 adds organization/environment enrollment as
an independent tenant rollout gate. Authorization administration is process-gated but can bootstrap enrollment.

### Security impact

Positive authorization boundary. New actions are deny-by-default, group and binding intervals are checked, every
resource lookup remains organization scoped, and foreign identifiers return not found. API responses omit
organization IDs and database details. No secret value is stored.

### Backward-compatibility impact

All v1 role checks, APIs, deployment execution, callbacks, and history remain unchanged. The legacy role is
dual-read until a separately approved removal. Migration backfill is idempotent and does not mutate the legacy
membership row.

## Validation

Fast focused tests cover actions/scopes, exact and ancestor grants, wrong resource denial, group membership,
expiry, legacy fallback, enrollment gates and rollback, request/repository validation, handler error mapping,
middleware behavior, and OpenAPI publication. The migration pair is present at reserved version 148.

Live PostgreSQL 16/18, repeated concurrent backfill, full Go regression, Hub/container builds, Angular/browser
tests, and integrated migration validation are deferred to the final gate because this speculative branch
intentionally does not contain prerequisite migrations 141 through 147.
