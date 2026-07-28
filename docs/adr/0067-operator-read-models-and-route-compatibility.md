# ADR-0067: Operator Read Models and Route Compatibility

- Status: Accepted
- Date: 2026-07-22
- Owners: Distr control-plane maintainers

## Context

The control-plane source records are distributed across the deployment registry,
releases, plans, campaigns, execution evidence, independent observations,
reconciliation, and audit domains. Operators need bounded list and detail APIs
that correlate those records without creating a second ownership model or a
second write authority. Every read must retain organization isolation, and list
results must remain stable while new records are appended.

The existing `/deployments` and `/deployments/:deploymentTargetId` routes are
public navigation contracts. The operator control room is additive; adopting it
must not reinterpret an existing deployment URL or require a flag-disabled
installation to change behavior.

## Decision

Expose organization-scoped read models below `/api/v1/control-plane` for fleet,
releases, plans, campaigns, executions, reconciliation, and audit. Queries read
the canonical tables introduced by the owning domain migrations. They may join
and summarize those records for transport, but they do not persist a copied
ownership identity or accept writes through a read model.

All list endpoints use descending tuple keysets. The cursor carries the complete
ordering tuple and effective filter state, so it cannot be replayed under a
different organization or query. Page size defaults to 50 and is capped at 100.
Detail and evidence endpoints remain available for approval, preflight,
checksum, uncertainty, and reconciliation facts; a compact list row is not the
only way to retrieve a blocking fact.

Migration 161 is indexes-only. Each new index begins with `organization_id` and
then follows either the endpoint's selective filter or its descending keyset.
Partial indexes cover active registry memberships and unresolved drift. No view,
materialized view, trigger-maintained projection, or projection table is added.
The migration can therefore be rolled back by dropping only its owned indexes,
without deleting control-plane evidence.

The operator HTTP routes are gated by `operator_control_plane_v2` and existing
scope checks. Static control-plane child routes are registered before any
parameterized compatibility route. The existing `/deployments` list and
`/deployments/:deploymentTargetId` detail semantics remain unchanged; PR-080 may
add UI redirects only after static-route ordering and legacy-link tests pass.

## Consequences

- Canonical domain tables remain the sole source of truth and write authority.
- Every index is tenant-leading, matching the mandatory organization predicate.
- Stable keyset pagination avoids offset drift and unbounded responses.
- Filter-specific indexes increase write amplification and storage. The scale
  fixture is an in-memory contract check; PostgreSQL `EXPLAIN (ANALYZE, BUFFERS)`
  and database latency thresholds remain unmeasured and deferred before rollout.
- Cross-domain detail assembly remains query-layer work and must avoid N+1
  access.
- Legacy deployment bookmarks keep their existing meaning.

## Alternatives considered

- Trigger-maintained projection tables were rejected because they would add
  replay, repair, and write-authority ambiguity before measured query plans show
  that indexes and bounded joins are insufficient.
- Materialized views were rejected because refresh freshness and tenant-safe
  concurrent refresh would become another operational contract.
- Offset pagination was rejected because concurrent inserts can duplicate or
  skip rows and large offsets produce unbounded scan cost.
- Replacing the legacy deployment routes was rejected because it would break
  established links and flag-disabled deployments.

## Validation

Migration contract tests require migration 161 to contain only tenant-leading
indexes, cover each read-model filter/keyset path, and drop exactly those indexes
on downgrade. The complete ordered migration lint must accept versions 0 through
161. Query/API tests cover filter-bound cursors, default and maximum page sizes,
empty/partial/unknown state, and cross-organization refusal. The deterministic
scale fixture checks bounded in-memory workload shape. The optional HTTP
benchmark measures endpoint timing only; it neither seeds PostgreSQL nor proves
query plans or database p95/p99 gates, which remain deferred.
Route tests preserve both legacy deployment URL forms and match static operator
children before parameterized routes.
