# ADR-0061: Scoped Authorization and Control-Plane Enrollment

## Status

Accepted

## Context

The existing `Organization_UserAccount.user_role` value is intentionally coarse. It protects the current v1
application, release, and deployment routes, but it cannot express that a principal may approve one environment,
operate one deployment unit, inspect audit evidence, or manage one campaign without receiving organization-wide
authority. A process-wide experimental feature flag is also a kill switch, not tenant authorization or rollout
policy.

The operator control plane needs action-specific authorization across organization, customer, environment,
deployment-unit, component, and campaign boundaries. It must preserve all v1 role behavior while the new model is
adopted, avoid revealing foreign identifiers, support user and group principals, and let an operator disable one
organization or environment without restarting the Hub.

## Decision

Migration 148 adds the following organization-owned records:

- `RoleDefinition` and `RolePermission` define an immutable role and its action set.
- `RoleBinding` assigns one role to one user or principal group at exactly one supported scope for a half-open
  effective interval; `RoleBindingRevision` appends its active and revoked states.
- `PrincipalGroup` and `PrincipalGroupMember` provide organization-scoped group membership with an independent
  half-open effective interval; `PrincipalGroupMemberRevision` appends its active and revoked states.
- `ControlPlaneEnrollment` appends organization or environment enable/disable revisions with actor, reason, and
  effective interval.
- `AuthorizationBackfillCheckpoint` records completion of the restart-safe built-in role migration.

Actions use stable lowercase names such as `release.publish`, `registry.manage`, `plan.execute`,
`approval.decide`, `campaign.control`, `audit.export`, and `authorization.manage`. Permission checks are
deny-by-default. A binding grants an action only when:

1. the user is the direct principal or is an effective member of the bound group;
2. the binding is effective at the decision instant;
3. the role contains the requested action; and
4. the binding scope exactly matches the resource or one of its resolved ancestors.

The authenticated credential's effective `CurrentUserRole` caps every decision before either scoped grants or
legacy fallback are considered. A read-only PAT held by an organization administrator can therefore use only the
viewer-compatible actions. A missing or malformed regular role denies access. Super administrators deliberately
have no implicit organization role: an organization-scoped super administrator may read authorization
administration collections, but mutation middleware blocks the principal before action evaluation.

Middleware captures one UTC `decisionAt` instant and carries it through the access request, group-membership
query, binding interval evaluation, and organization/environment enrollment evaluation. Repository SQL uses that
parameter instead of database `now()`, preserving exact half-open interval behavior.

Every new binding or membership writes active revision 1 in the same transaction. Revocation takes a per-subject
transaction advisory lock and appends the next monotonic revoked revision effective at the requested instant.
Revisions are never updated or deleted. User bindings and group memberships also snapshot the existing
`Organization_UserAccount.created_at` membership identity; if that membership is deleted and later recreated, its
old authorization facts stay inert without changing the legacy membership schema.

Organization is always the root scope. A deployment unit resolves to its organization, environment, exact unit,
and dedicated or active shared subscribers. Customer, environment, and component references are validated inside
the authenticated organization. Campaign storage arrives in PR-071; until then a campaign authorization key is
the authenticated organization plus campaign UUID, and the campaign repository remains responsible for the
tenant-scoped existence check.

`Organization_UserAccount.user_role` remains a dual-read compatibility source. Migration 148 and the idempotent
repository backfill create built-in viewer, developer, and administrator role definitions and permissions without
changing or duplicating the legacy membership row. When no scoped binding grants the requested action, the
authorization service evaluates the legacy role's deliberately bounded v2 action set. Viewer can inspect/export
audit evidence. Developer can
create and publish releases, manage registry/configuration, plan and execute, control campaigns, and inspect
audit evidence. Administrator receives every registered control-plane action. Developer does not implicitly gain
approval, policy, emergency, reconciliation, or authorization-administration authority.

New v2 routes use action middleware. The middleware resolves the resource inside the current organization, returns
not found for a disabled process flag or foreign resource, and returns one generic forbidden response for a
non-matching grant. Existing v1 middleware and routes are unchanged.

Control-plane execution eligibility is separate from action authority:

```text
operator_control_plane_v2 process flag
AND active organization enrollment
AND active selected-environment enrollment
```

The latest effective revision at each scope wins, so an appended disabled revision rolls a tenant or environment
back without deleting evidence. Authorization administration itself is process-gated but does not require an
existing enrollment; otherwise an administrator could not bootstrap or disable enrollment.

The admin API is additive under `/api/v1/authorization`:

- `GET|POST /roles`
- `GET|POST /bindings`
- `GET|POST /groups`
- `GET|POST /groups/{groupId}/members`
- `GET|POST /control-plane-enrollments`
- `POST /bindings/{bindingId}/revocations`
- `POST /groups/{groupId}/members/{memberId}/revocations`

The API creates append-only role, binding, membership, revocation, and enrollment records. It exposes no update or
delete operation. Every collection GET is keyset paginated with a stable `created_at DESC, id DESC` order and a
versioned opaque cursor bound to organization, collection, and parent group where applicable. Every repository
query includes `organization_id`; a foreign role, principal, group, or scope is reported as not found without
confirming its existence. Group-member listing first resolves the same-tenant parent, so missing and foreign groups
both return not found while an existing empty group returns an empty page.

The three compatibility role keys are reserved by API, repository, and database constraint. Custom-role creation
takes the organization built-in-role advisory lock and transactionally initializes exactly viewer, developer, and
administrator plus their permissions/checkpoint before inserting the custom role. A POST-before-GET request
therefore cannot squat a compatibility key.

Migration 148 down takes `ACCESS EXCLUSIVE` locks on all authorization evidence tables before checking for custom
roles, bindings, principal groups, or enrollments. It refuses when evidence exists and can remove an unused
compatibility-only backfill. A concurrent writer either finishes before the locked guard and is observed, or
blocks and fails after rollback; it cannot race between guard and drop. No v1 role row, release payload,
deployment, execution, callback, or audit history is rewritten.

## Consequences

Operators can grant narrow duties and roll out the control plane per organization and environment while retaining
the process-wide emergency kill switch. Group membership and binding expiry are evaluated independently, and
effective decisions retain the matching immutable binding IDs.

The compatibility fallback is intentionally temporary and additive. It prevents an upgrade from removing access,
but adopters must create narrower bindings before a separately approved future removal of legacy dual-read. A
custom role cannot relax organization ownership or make a disabled enrollment effective.

Campaign scope existence remains delegated to the future campaign repository until migration 153 introduces the
campaign table. All other supported scope kinds are validated immediately by migration-139 identities.

## Alternatives Considered

- Treat the process flag as authorization. Rejected because a kill switch cannot express tenant, environment, or
  principal authority.
- Replace `user_role` immediately. Rejected because it would break v1 access and make rollback unsafe.
- Store one mutable role or binding row. Rejected because in-place changes erase the authority used for an
  approval or execution decision.
- Infer scope from names and tags. Rejected because mutable labels are not stable tenant boundaries.
- Return forbidden for foreign resources. Rejected because different responses would reveal that a foreign UUID
  exists.

## Validation

- Pure authorization tests cover exact and organization-ancestor matches, wrong customer/environment/unit,
  effective group membership, expired binding, view-versus-mutation separation, legacy fallback, deterministic
  matched binding IDs, and generic denial.
- Enrollment tests cover process-off, organization-off, environment-off, effective intervals, both gates on, and
  a later disabled revision.
- Repository tests cover normalization, revision, cursor, and validation contracts; migration 148 statically
  verifies all nine tables, compatibility backfill, and locked guarded downgrade.
- Handler, middleware, and OpenAPI tests cover request validation, action-specific dispatch, hidden disabled or
  foreign resources, generic denial, tenant-safe write errors, and every admin collection route.
- Compile-valid PostgreSQL tests cover concurrent revocation ordering, membership removal/re-entry, half-open
  revocation, and downgrade/write serialization, and skip without `DISTR_TEST_DATABASE_URL`. Live PostgreSQL
  16/18 execution and repeated backfill remain mandatory at the integrated PR-066 gate after prerequisite
  migrations 141 through 147 are present.
