# ADR-0073: New-Volume Restore and Protected-History Continuity

## Status

Accepted

## Context

Application-only rollback is unsafe after a schema change that an earlier Hub
binary cannot write. An in-place database or object-store restore also destroys
the failed state needed for diagnosis and makes a partial restore difficult to
distinguish from the accepted runtime. The operator additionally needs a
canonical, client-scoped proof that release and execution history survived an
upgrade, restore, or separately approved sample-domain retirement.

## Decision

The server Compose deployment helper exposes two separate operations:

- `restore-plan` restores checksum-bound PostgreSQL and RustFS backups into new,
  labeled Docker volumes. It verifies the prior immutable image handoff and OCI
  revision, checks the restored schema with the prior image, exports protected
  history with the current operator image, performs an exact comparison, and
  seals a read-only plan. It does not stop or switch the active runtime.
- `restore-apply` revalidates every sealed input, volume label, object aggregate,
  image identity, and protected-history fingerprint before stopping the active
  runtime. It then replaces the image identity and both Compose volume names in
  one atomic `.env` rename, starts the restored runtime, and repeats health,
  schema, object, and protected-history checks.

Candidate and previous active volumes are never deleted automatically. A failed
candidate remains labeled and an immutable failure record identifies it. If an
apply fails after the switch, the helper atomically restores the previous image
and volume names and starts the untouched previous runtime.

Protected-history artifacts remain canonical JSON with record and artifact
SHA-256 identities. Schema 138 through 165 export the stable customer,
deployment-target, external-execution, and external-event projection. Schema
166 and later export the expanded control-plane projection. Exact comparison
rejects additions, modifications, deletions, scope changes, and schema changes.

A missing record can be accepted only when a separately supplied
`distr.protected-history-retirement-allowlist/v1` artifact:

- is bound to the exact baseline artifact and scope;
- identifies every missing kind, UUID, and baseline record hash;
- is bound to an immutable preview checksum;
- carries a nonzero approval UUID, approval checksum, and `APPROVED` state; and
- contains no unused allowance.

This exception is restricted to the existing sample-domain retirement purpose.
It cannot authorize patterns, additions, modifications, cross-scope changes, or
ordinary retention cleanup.

## Consequences

Schema rollback requires additional disk capacity for parallel PostgreSQL and
RustFS volumes and incurs a short outage only during `restore-apply`. Operators
must retain the exact prior handoff, both backups and sidecars, the protected
history baseline, and any separate retirement approval. Diagnosis is safer
because no failed or previous volume is erased automatically; retirement of
retained volumes is a later, separately reviewed operation.

The protected projection intentionally differs before and after schema 166, so
exact comparisons are made only between artifacts from the same schema. This is
appropriate for backup/restore equality and avoids fabricating unavailable v2
history on schema 138.

## Alternatives Considered

- **In-place restore:** rejected because failure can destroy the only diagnostic
  copy and expose a partially restored runtime.
- **Automated down migrations:** rejected because existing migrations include
  destructive rollback behavior and do not restore object storage.
- **Binary-only rollback after every schema:** rejected because compatibility is
  not implied once the schema is newer than the earlier binary contract.
- **Count-only or selected-row checks:** rejected because they do not prove exact
  record identity or payload continuity.
- **Pattern-based cleanup exceptions:** rejected because they cannot prove the
  reviewed deletion boundary.

## Validation

- Focused Go tests cover canonical allowlist sealing, exact equality, additions,
  modifications, missing records, stale/unused allowances, schema mismatch, and
  CLI fingerprint/compare behavior.
- Database tests prove the schema-138 projection uses only the stable audit
  tables and excludes sensitive payload fields.
- `hack/test-server-compose-restore.sh` proves validation-before-switch order,
  atomic image/volume identity replacement, failed-volume retention, apply
  recovery, and CLI arity.
- `bash -n`, focused Go tests, and diff checks are required before integration.
