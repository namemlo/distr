# PR-079: Operator Control-Plane Read Models

## Scope

PR-079 adds bounded, tenant-scoped operator reads over the canonical
control-plane records. It covers fleet, release, plan, campaign, execution,
reconciliation, and audit list/detail surfaces below:

```text
/api/v1/control-plane
```

The collection routes are `/fleet`, `/releases`, `/plans`, `/campaigns`,
`/executions`, `/reconciliation`, and `/audit`; domain-specific detail, compare,
and evidence routes remain under the same root. This PR does not add the Angular
control room, adopter inventory, executor/observer fixtures, or sample cleanup.

## Read-model contract

Read models compose the source records owned by migrations 139 through 160. They
do not copy deployment ownership, mutate source state, or become an input to
admission/execution decisions. The mandatory organization ID is applied at every
query root and tenant-sensitive join.

Lists use descending keyset order such as `(created_at, id)`, `(updated_at, id)`,
or `(evaluated_at, id)`. Cursors bind the ordering tuple to the effective filter
and organization. The default page size is 50 and the maximum is 100. Compact
rows may summarize incomplete, stale, or unknown evidence, while detail routes
retain the underlying checksums, approval/preflight blockers, execution
uncertainty, drift, and evidence links.

## Database changes

Migration 161 adds indexes only:

- active fleet placement, customer subscription, environment assignment,
  component definition, and unresolved-drift paths;
- release and plan keysets plus their supported status/application/kind,
  environment/unit/release filters;
- campaign run, member, member-run, and latest evaluation paths;
- execution keysets and task plan/target joins;
- reconciliation keysets and audit event-type/actor filters.

Every index begins with `organization_id`. Partial predicates are limited to
active registry rows or unresolved drift. Migration 161 creates no tables,
views, materialized views, triggers, or copied source records. Its downgrade
drops only its owned indexes, so no business or evidence record is destroyed.

## Compatibility and security

The operator API is additive and requires the authenticated organization,
`operator_control_plane_v2`, and the route's existing authorization scope.
Cross-organization rows are refused rather than filtered after mapping.

Existing `/deployments` and `/deployments/:deploymentTargetId` behavior remains
unchanged. Static operator children must precede any parameterized compatibility
route. UI navigation and legacy redirects belong to PR-080.

## Verification

Focused migration tests verify that migration 161 is indexes-only,
tenant-leading, keyset/filter complete, and exactly reversible. Ordered
migration lint validates the 0 through 161 chain. Query and handler suites cover
every filter, stable/filter-bound cursors, default and maximum limits,
empty/partial/stale/unknown state, shared-unit blast radius, and tenant
isolation. The scale fixture and benchmark remain the production acceptance gate
for retaining filter-specific indexes and proving page-size-100 warm p95/p99
thresholds without N+1 reads.

## Upstream contribution notes

The contract and index set use community-neutral deployment concepts. They do
not contain adopter names, provider assumptions, credentials, or environment-
specific routing.
