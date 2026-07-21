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
  checksum, and idempotent replay for the same draft revision even after the draft advances.
- Calendar semantics: unique rule names, sorted weekdays, start-inclusive/end-exclusive intervals, and
  previous-weekday handling for overnight windows.
- Freeze semantics: exact UTC start-inclusive/end-exclusive intervals; overlap selection uses descending priority
  then immutable revision UUID.
- Time evidence: UTC instant, local time, exact offset, IANA zone, timezone-rule version, selected immutable IDs,
  reason code, and deterministic evaluation identity. Production uses the embedded IANA 2026a dataset from the
  checksum-pinned `gotz` v0.1.2 module, rejects `Local`, and exposes the exact build dataset identity.
- Tenant behavior: every repository operation includes the authenticated organization; foreign and missing
  identities are indistinguishable.

## Impact

Migration 151 adds `MaintenanceCalendar`, `MaintenanceCalendarVersion`, `MaintenanceWindowRule`,
`DeploymentFreeze`, and `DeploymentFreezeRevision`. Draft roots remain editable. Published versions, revisions, and
window rules are immutable except during the existing authorized organization-retention cascade. The guarded down
migration refuses while any new rows exist.

Draft rule UUIDs are stable logical identities. Each publication derives a separate version-scoped immutable row
UUID, so publishing an edited draft never reuses a child primary key and does not change canonical checksum
semantics. Version pages hydrate all child rules with one bounded batch query.

The API and routing changes are additive and default-off. There is no UI, scheduler admission write, override,
deployment execution, agent-protocol, or client-database change in this slice. Migration 153 activates campaign
scope through tenant-owned deployment campaign draft identities. Existing v1 behavior and historical checksums
are unchanged.

## Verification

Focused tests cover ordinary allow/deny, overnight windows, DST gaps, repeated hours without evaluation-identity
collision, IANA/rule-version drift, canonical checksum stability and material changes, exact freeze instants,
freeze overlap/priority, cursor bounds, API validation, response non-leakage, strict JSON, hidden feature routes,
admin/super-admin policy, scoped-action handoff, tenant-safe error mapping, and the static migration contract.
Additional focused tests cover host-zoneinfo independence, declared/runtime rule-data mismatch, fail-closed PR-066
integration, current-before-destination freeze authorization, historical publish replay, version-scoped child IDs,
and batch rule grouping/query shape.

Live PostgreSQL migration/repository/retention tests, the complete repository suite, containers, browser/UI, and
production deployment remain final integration gates. Migration 151 must be rebased after migrations 141 through
150 before those gates run.

## Operations

1. Keep `operator_control_plane_v2` disabled while applying the sequential migration set.
2. Rebase PR-066 and replace the fail-closed calendar authorization factory with its shared
   `authorization.Authorize` adapter before enabling the flag. No permissive production fallback exists.
3. Submit rule version `2026a`; reject a deployment if its reported
   `zonerules.ProductionRuleDataIdentity` differs from the approved build identity.
4. Pin the returned immutable IDs and checksums in the policy/plan slice.
5. At start-time admission, pass one UTC instant and the pinned zone/rule binding to the pure evaluator.
6. Persist its exact evidence in the admission slice; an ordinary closed window or active freeze waits and does not
   mutate an approved plan.
7. Treat a changed timezone, rule version, published revision, or future override as material policy input requiring
   the later invalidation behavior.
