# PR-069 - Versioned Maintenance Calendars and Deployment Freezes

## Generic User Story

As a fleet operator, I want editable future scheduling policy and immutable published calendar/freeze revisions so
that deployment admission is deterministic across clients, services, timezones, and daylight-saving transitions.

## Contract

- Calendar routes: list/create/get/update draft, publish, list versions, and get a version below
  `/api/v1/maintenance-calendars`.
- Freeze routes: list/create/get/update draft, publish, list revisions, and get a revision below
  `/api/v1/deployment-freezes`.
- Access: routes are absent unless `operator_control_plane_v2` is enabled; enabled routes require vendor,
  organization, admin, and non-super-admin context. Mutations expose `calendar.manage` and `freeze.manage` scoped
  authorization seams for the action-grant slice.
- Pagination: lists use an opaque versioned keyset cursor, default 50, maximum 100, and deterministic
  timestamp/UUID ordering.
- Publication: optimistic draft revision, immutable source-revision publication, canonical JSON payload, SHA-256
  checksum, and idempotent replay for the same draft revision.
- Calendar semantics: unique rule names, sorted weekdays, start-inclusive/end-exclusive intervals, and
  previous-weekday handling for overnight windows.
- Freeze semantics: exact UTC start-inclusive/end-exclusive intervals; overlap selection uses descending priority
  then immutable revision UUID.
- Time evidence: UTC instant, local time, exact offset, IANA zone, timezone-rule version, selected immutable IDs,
  reason code, and deterministic evaluation identity.
- Tenant behavior: every repository operation includes the authenticated organization; foreign and missing
  identities are indistinguishable.

## Impact

Migration 151 adds `MaintenanceCalendar`, `MaintenanceCalendarVersion`, `MaintenanceWindowRule`,
`DeploymentFreeze`, and `DeploymentFreezeRevision`. Draft roots remain editable. Published versions, revisions, and
window rules are immutable except during the existing authorized organization-retention cascade. The guarded down
migration refuses while any new rows exist.

The API and routing changes are additive and default-off. There is no UI, scheduler admission write, override,
campaign, deployment execution, agent-protocol, or client-database change in this slice. Campaign scope remains
unwritable until immutable campaign revisions land. Existing v1 behavior and historical checksums are unchanged.

## Verification

Focused tests cover ordinary allow/deny, overnight windows, DST gaps, repeated hours without evaluation-identity
collision, IANA/rule-version drift, canonical checksum stability and material changes, exact freeze instants,
freeze overlap/priority, cursor bounds, API validation, response non-leakage, strict JSON, hidden feature routes,
admin/super-admin policy, scoped-action handoff, tenant-safe error mapping, and the static migration contract.

Live PostgreSQL migration/repository/retention tests, the complete repository suite, containers, browser/UI, and
production deployment remain final integration gates. Migration 151 must be rebased after migrations 141 through
150 before those gates run.

## Operations

1. Keep `operator_control_plane_v2` disabled while applying the sequential migration set.
2. Publish a calendar version and freeze revision only after the IANA zone and deployed timezone-rule version are
   recorded.
3. Pin the returned immutable IDs and checksums in the policy/plan slice.
4. At start-time admission, pass one UTC instant and the pinned zone/rule binding to the pure evaluator.
5. Persist its exact evidence in the admission slice; an ordinary closed window or active freeze waits and does not
   mutate an approved plan.
6. Treat a changed timezone, rule version, published revision, or future override as material policy input requiring
   the later invalidation behavior.
