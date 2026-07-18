# ADR-0057: Deployment Registry Import Evidence

## Status

Accepted

## Context

Configuration adapters discover deployment roots and placements, but discovery must not silently create executable
registry state. Operators need a deterministic preview, explicit classification, restartable apply, exact coverage,
and immutable source evidence without putting raw reports, paths, hostnames, or secrets in PostgreSQL.

## Decision

Migration 140 adds organization-scoped `RegistryImport`, `RegistryImportRoot`, `RegistryImportPlacement`, and
append-only `RegistryImportDecision` records. PostgreSQL stores normalized candidates and immutable metadata only.
Raw report bytes stay in the evidence store and are referenced as `evidence://sha256/<64 lowercase hex>`; that
digest must equal the separately supplied checksum.

Preview deep-normalizes every source, parameter, root, subscriber, and placement field before computing a SHA-256
checksum or returning the public DTO. Subscriber UUIDs are sorted and deduplicated. One OS-independent sanitizer
rejects absolute Windows/Unix paths, URLs, hostnames/IPs, credential-looking input, and unbounded values at both the
service and repository boundary, including canonical registry baseline values before diff generation. Invalid keys,
enums, and destination-incompatible duplicate placement identities are rejected before persistence; accepted
topology diagnostics remain stable, bounded to 100 entries, and persistable.

An adapter may declare an optional source-placement baseline containing at most 1,000 canonical
`rootKey`/`physicalName` identities. The preview compares that physical inventory with mapped candidates and stores
the exact missing identities as checksummed omission evidence; mapped candidates outside a supplied baseline are
rejected as inconsistent evidence. The empty default preserves existing adapters. Adapter omissions block
completeness and apply; explicit retirement diffs remain distinct and applicable.

Classification is import evidence, not a second registry management enum. `standard` maps to dedicated/managed,
`shared` to shared/managed, `external` to external/external, and `observe_only` retains the validated delivery model
with observe-only management. `ignored` records an explicit exclusion. `needs_decision`, conflicts, or omissions
block apply and completeness. Renames require an active alias or explicit retire/new-identity decision.

The import UUID is the durable idempotency identity. Apply atomically claims an import with an owner UUID and a
renewable database lease, requires the exact preview checksum, rejects an active concurrent owner, safely reclaims
an expired owner after interruption, and returns the prior result for an already-applied matching import. Each root
or retirement mutation and its expected checkpoint commit in one transaction; claim ownership and affected-row
counts are checked on every transition. Classification locks the same import row before appending a monotonic
per-root decision ordinal.

Apply separates whole-root creation, placement creation, metadata update, rename, placement retirement, and root
retirement. A new placement resolves the existing root/unit instead of recreating it. Open organization-scoped
target/environment assignments are selected and reused before insert while holding the same target-scoped advisory
transaction lock as the overlap trigger. Before any topology mutation, apply revalidates all organization-owned
references, actionable topology, existing targets, and active rename aliases.

Every repository operation includes the authenticated organization ID. Preview batches candidate reference checks
inside its transaction, while migration 140 adds composite import/root/organization constraints and a non-pinning
organization-validation trigger for historical candidate references. Preview, decision, and apply mutations require
the PR-056 read-write/admin mutation boundary and `operator_control_plane_v2`; reads retain PR-056 behavior.

Coverage reports discovered, classified, actionable-managed, observe-only, external, ignored, and unresolved roots,
plus source placements, mapped distinct services, exact omissions, and completeness separately. External and
observe-only are covered but non-executable; ignored remains visible; unresolved or omitted candidates block
completeness.

## Consequences

Imports are reproducible and auditable without making PostgreSQL a raw-report or secret store. Classification and
apply history remain attributable to a user account and organization. Migration 140 downgrade refuses while any
import evidence exists.

Existing registry identities, v1 deployment behavior, agents, and historical checksums are unchanged. Focused live
PostgreSQL tests cover diagnostic persistence, foreign-reference rejection, idempotent/concurrent apply, assignment
reuse, and placement-only apply, but execution remains deferred to the final integration gate.

## Alternatives Considered

- Store raw report JSON in PostgreSQL. Rejected because reports can contain paths, hostnames, and secrets.
- Treat discovery as activation. Rejected because unresolved topology and renames require operator decisions.
- Add import values to `RegistryManagementState`. Rejected because classifications are review evidence mapped to
  the existing PR-056 identity states.
- Use source checksum as idempotency identity. Rejected because one durable import can receive decisions while
  retaining its immutable preview checksum.
