# ADR-0059: Product Release Capability Graph

## Status

Accepted

## Context

A target-neutral Component Release proves one component build, but an operator must deploy a coherent product made
from many independently published components. A list of child releases is insufficient: it cannot prove capability
compatibility, expose a dependency cycle, order providers before consumers, or distinguish requirements that must
resolve at product publication from requirements intentionally deferred to target planning.

The existing `ReleaseBundle` identity, organization boundary, status, checksum, publication actor, and audit events
must remain authoritative. Historical v1 releases and Component Release v2 contracts must not be rewritten.

## Decision

A Product Release remains `ReleaseBundle(kind=product)` with schema `distr.product-release/v1`. Migration 144 adds
organization-scoped `ProductReleaseComponent` and `ProductReleaseCapabilityEdge` projections.

Each component row pins:

- one already-published Component Release ID from the same organization;
- its exact immutable canonical checksum;
- its component key and semantic version; and
- a frozen Component Release v2 contract snapshot used to reconstruct the graph without reading mutable target
  state.

Product-stage requirements must resolve to exactly one included provider whose declared semantic version satisfies
the required range. Missing, ambiguous, incompatible, cyclic, duplicate, unpublished, foreign, platform-incomplete,
or migration-inconsistent graphs block publication. Edges run provider to consumer, so deterministic topological
order places provider deployment and health proof before its consumer.

Target-stage requirements remain explicit unresolved symbolic nodes. They are valid only with at least one exact
wire mode: `included`, `pinned_existing`, `shared_provider`, `approved_external`, or `feature_disabled`. There is no
implicit or generic ignore result. A later target-plan slice must resolve every symbolic node before plan
publication.

Canonical Product Release bytes contain sorted child pins, product requirements, graph nodes, edges, topological
order, and graph checksum. Draft publication recomputes and validates this material under a row lock. The first
successful publish freezes the child snapshots, graph, checksum, actor, and time; an exact retry is idempotent and a
different retry is a stable conflict.

Product publication exposes narrow, fail-closed integration hooks. PR-061 must register the provenance verifier for
each exact organization/component pair, and PR-067 must register the resolver that proves the exact dependency policy
version is organization-scoped, immutable, and published. Both run in the publication transaction after every pinned
Component Release row has been batch-loaded and locked in deterministic UUID order. Publication is unavailable while
either verifier is not registered; this slice does not duplicate Sigstore, CI, or policy persistence logic.

The product routes are:

- `POST /api/v1/product-releases`;
- `GET /api/v1/product-releases/{id}`;
- `POST /api/v1/product-releases/{id}/validate`;
- `POST /api/v1/product-releases/{id}/publish`; and
- `GET /api/v1/product-releases/{id}/graph`.

They require the existing Release Bundles feature plus `operator_control_plane_v2`. Mutations additionally require
vendor read-write/admin authority and reject super-admin impersonation. All lookups are organization-scoped and
expected failures use tenant-safe messages.

## Consequences

Release managers can inspect and publish a deterministic, reusable product composition before selecting any
customer, environment, deployment unit, host, configuration, or secret. A neutral provider/consumer fixture proves
that provider deployment and health precede consumer deployment.

Normalized child and edge rows duplicate immutable canonical facts for bounded queries. A Product Release is limited
to 256 Component Releases, 256 product-level requirements, and 4,096 total graph requirements. Product and component
versions and every indexed graph key are byte-bounded in both API validation and migration constraints. Their foreign
keys and checksums preserve tenant and lineage safety, while the canonical Product Release remains the audit source.
Future resolver changes must introduce another schema version instead of changing v1 graph meaning.

## Alternatives Considered

- Store only child IDs in `ReleaseBundleComponent`. Rejected because it cannot freeze requirement edges, symbolic
  target nodes, or the exact contract facts used for resolution.
- Resolve all dependencies at target planning. Rejected because product-stage cycles and compatibility gaps would
  escape release management and be repeated per target.
- Silently satisfy target-stage requirements from an included provider. Rejected because target resolution mode,
  observed health, shared blast radius, external approval, or disabling policy must be frozen explicitly.
- Reimplement provenance validation. Rejected because PR-061 owns that policy and evidence lifecycle.

## Validation

- Pure graph tests cover exact cycle paths, missing and ambiguous providers, incompatible ranges, duplicate,
  unpublished and foreign children, stable ordering, product-stage gaps, platform coverage, and valid symbolic
  target nodes.
- Canonicalization tests prove stable bytes and checksums across input order.
- API, mapping, handler, migration, and repository-focused tests cover strict child pins, tenant-safe errors,
  bounded collections, deterministic batch locking, feature/mutation guards, immutable projections, and the
  fail-closed provenance/policy hook boundaries.
- Live PostgreSQL 16/18, full-repository Go, container, and browser gates are intentionally deferred to the final
  integrated release gate.
