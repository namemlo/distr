# PR-062 - Product Release Capability Graph

## Generic User Story

As a release manager, I want to publish one immutable product composition with validated component capabilities so
that every later target plan uses the same compatible child releases and dependency order.

## Scope

- Keep Product Release identity on `ReleaseBundle(kind=product)`.
- Pin published Component Release IDs, exact checksums, and frozen contract snapshots.
- Build and persist a deterministic provider-to-consumer capability DAG.
- Block product-stage gaps and preserve valid target-stage requirements as unresolved symbolic nodes.
- Publish with immutable checksums, idempotent exact retry, stable conflict, audit actor/time, and tenant-safe errors.
- Leave provenance and dependency policy behind narrow, fail-closed PR-061/PR-067 integration hooks.

## Resolution Contract

| Stage     | Publication behavior                                                                                 |
| --------- | ---------------------------------------------------------------------------------------------------- |
| `product` | Exactly one compatible included provider is required; its edge is provider to consumer.              |
| `target`  | The requirement remains an explicit unresolved node with one or more approved target resolution modes. |

Target mode wire values are exactly `included`, `pinned_existing`, `shared_provider`, `approved_external`, and
`feature_disabled`. No dependency is implicitly ignored or silently satisfied.

## Required Impact Report

### Database/schema impact

Migration 144 adds `ProductReleaseComponent` and `ProductReleaseCapabilityEdge`. Every row carries the owning
organization. Composite foreign keys constrain both product and component release IDs to that organization. Child
checksums are lowercase SHA-256, contract snapshots must be Component Release v2 JSON objects, and edge constraints
enforce the product/target provider and mode invariants. Product/component versions and indexed edge values are
byte-bounded. Downgrade refuses while Product Release facts exist.

### Public API impact

Adds `/api/v1/product-releases` create, get, validate, publish, and graph routes. Requests pin child IDs and expected
checksums; responses expose the canonical and graph checksums plus the frozen manifest. Cross-organization or
wrong-kind reads return the same 404 and internal database details are never returned.

Requests are bounded to 256 Component Releases, 256 product-level requirements, 4,096 total graph requirements, and
byte-bounded version/range values. Creation and validation batch-load children instead of issuing per-component
queries. Publication locks the complete child set in UUID order and repeats child identity, checksum,
dependency-policy, and provenance eligibility checks in the same transaction. Missing PR-061 or PR-067 wiring blocks
publication.

### Frontend/UI impact

None in PR-062. The API and graph response are ready for the later Releases operator view.

### Agent/protocol impact

None. Product capability edges are target-neutral planning input; agents and external executor v1 receive no new
message.

### Feature-flag and authorization impact

The route family requires `release_bundles` and `operator_control_plane_v2`. Create and publish additionally require
vendor read-write/admin permission and block super-admin mutation. Existing Release Bundle v1 routes and bytes remain
unchanged.

### Security impact

Product records contain no customer, target, environment, hostname, path, variable, credential, or secret.
Organization-scoped queries and composite foreign keys prevent foreign child pins. Expected validation and conflict
responses are stable and tenant-safe.

### Backward-compatibility impact

Historical Release Contract v1 and Component Release v2 JSON, IDs, checksums, and routes are not rewritten. The
generic Release Bundle mutation API refuses Product Release creation, update, delete, validation, and publication so
the immutable product workflow cannot be bypassed.

## Neutral Fixture

The focused fixture contains a `provider` Component Release providing `transactions@1.4.0` and a `consumer`
requiring `transactions >=1.0.0 <2.0.0`. Its topological order is:

1. `component:provider`;
2. provider deployment and health/capability verification; then
3. `component:consumer`.

Changing the requirement to target-stage retains `target:consumer:transactions` as an unresolved graph node rather
than claiming it is already satisfied.

## Validation

Focused Go tests cover graph resolution, cycle paths, migration equality, stable canonical bytes, bounded API
collections/indexed values, repository migration contracts, deterministic child locking, fail-closed external
eligibility, tenant-safe handler errors, and compile integration. Live PostgreSQL, full Go, container, production
frontend, and browser checks are deferred until the numbered branches are integrated.
