# PR-057 - Deployment Registry Import and Classification

## Generic User Story

As an operator, I want to preview, classify, apply, and measure a normalized deployment inventory so that every
root and placement is visible and auditable before it can become managed registry state.

## Contract

- Routes: `POST /deployment-registry/imports/preview`,
  `POST /deployment-registry/imports/{id}/decisions`,
  `POST /deployment-registry/imports/{id}/apply`,
  `GET /deployment-registry/imports/{id}`, and
  `GET /deployment-registry/coverage?importId={id}`.
- Evidence: raw bytes stay outside PostgreSQL; immutable references use
  `evidence://sha256/<64 lowercase hex>` and must match `evidenceChecksum`.
- Determinism: every string is trimmed/canonicalized, subscriber UUID sets are sorted and deduplicated, parameters
  are key-sorted, and preview checksums, full-placement diffs, diagnostics, counts, and checkpoints are stable.
- Completeness: adapters may supply an optional `sourcePlacements` baseline of at most 1,000 generic
  `rootKey`/`physicalName` identities. Every baseline identity absent from the mapped candidates becomes an exact,
  persisted, checksummed omission, while a mapped candidate absent from a supplied baseline is rejected as
  inconsistent evidence. Omissions block completeness and apply; an empty baseline remains backward compatible,
  and an explicit retirement diff is not treated as an omission.
- Safety: one cross-platform sensitive-text sanitizer covers source, root, and placement metadata. Invalid
  persistence identities are rejected before insert. Existing registry baseline strings pass the same sanitizer
  before a diff can expose them. Missing target/environment/customer/subscriber topology, unaliased renames,
  conflicts, and omissions block apply/completeness.
- Idempotency: import UUID is durable identity; same-checksum replay returns the prior result, stale checksum
  conflicts, an owner UUID/lease prevents concurrent writers, and conditional atomic checkpoints support restart.
- Apply semantics: root create, placement create, placement metadata update, rename, placement retirement, and root
  retirement are separate operations. Placement-only imports never recreate a root, and roots sharing an open
  organization-scoped target/environment assignment reuse it under the same target-scoped transaction lock used
  by the overlap trigger.
- Coverage: managed, observe-only, external, ignored, and unresolved roots remain separate and visible.

## Impact

Migration 140 adds four organization-owned import tables, content-addressed evidence checks, append-only monotonic
decisions, actor attribution, exact count/diff/omission/diagnostic payloads, claim ownership, and checkpoint fields. Composite
child constraints prevent import/root mismatches; batched repository validation and a non-pinning trigger reject
foreign candidate references. Authorized organization-retention cascades remain possible. Its down migration
refuses while evidence exists. The API is additive. Mutations reuse the PR-056 read-write/admin, no-super-admin, and
`operator_control_plane_v2` boundary; reads are organization scoped. Existing deployments and agents are unchanged.

The backend stores no raw report bytes, secret-bearing content, local/client paths, or hostnames. Diagnostics are
bounded. Cross-organization import IDs return the same not-found result as missing IDs.

## Verification

Fast unit tests cover deep normalization, checksum stability, every placement metadata field, source-baseline
omissions, cross-platform sensitive-text rejection for incoming and existing baselines, persistence-compatible
identities, placement-only/metadata diffs, topology completeness, classification mapping, and bounded diagnostics.
Static migration tests cover claim/checkpoint/actor, persisted omissions, assignment reuse ordering, composite
integrity, placement identity, retention, and guarded downgrade. Focused live PostgreSQL cases for diagnostic
and omission round-trip, tenant references, omission-blocked apply, concurrent/idempotent apply, assignment reuse,
and placement-only apply are present but remain deferred to the final integration gate.
