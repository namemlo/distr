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
  seals a read-only plan. It also creates
  `distr.release-restore-snapshot/v1`, whose checksum binds the plan ID, prior
  handoff, PostgreSQL backup, RustFS backup, protected-history baseline, target
  digest reference, and target release commit. It does not stop or switch the
  active runtime.
- `restore-apply` revalidates every sealed input, volume label, object aggregate,
  snapshot binding, image identity, and protected-history fingerprint. It also
  proves that the running Hub digest and the PostgreSQL/RustFS container mounts
  match the configured source identity. It writes a durable `PREPARED` switch
  journal before stopping the active runtime, records
  `SOURCE_STOPPED`, mounts the stopped source PostgreSQL volume in an isolated
  container, and compares its live protected history with the sealed baseline.
  Only an exact result permits the image and both Compose volume names to be
  replaced in one atomic `.env` rename. The restored runtime must then pass
  health, image, schema, object, and protected-history checks before the journal
  advances through `TARGET_VERIFIED` to `COMMITTED`.
- `restore-recover` validates the self-checksummed journal and its sealed source
  and target identities. `PREPARED` is closed as `RECOVERED` after proving the
  source identity; `SOURCE_STOPPED`, `IDENTITY_SWITCHED`, and
  `RECOVERY_REQUIRED` restore and verify the source runtime before becoming
  `RECOVERED`; and `TARGET_VERIFIED` becomes `COMMITTED` only when the matching
  checksummed applied-state record already exists. `COMMITTED` and `RECOVERED`
  are idempotent terminal results. A checksum or identity mismatch is refused.

Candidate and previous active volumes are never deleted automatically. A failed
candidate remains labeled and an immutable failure record identifies it. A
failed apply attempts to restore the previous image and volume names and start
the untouched previous runtime; if that cannot be proven, the journal remains
`RECOVERY_REQUIRED` and a later `restore-recover` must resolve it. A new apply is
refused while any switch journal is unresolved.

Protected-history artifacts remain canonical JSON with record and artifact
SHA-256 identities. Schemas 138 through 165 use the complete schema-138 table
set and whole-row payloads across release, deployment, task, execution, log,
observation, and external-execution timestamp-audit families. Schema 166 has an
explicit expanded control-plane projection; schema 167 adds
`ExecutionRuntimeEvidence`; schema 168 adds
`DeploymentPlanResolvedRequirement`; and schema 169 adds
`BaselineAdoptionComponent`. Any schema newer than the highest registered
projection is refused rather than silently omitted. Exact comparison rejects
additions, modifications, deletions, scope changes, and schema changes.

A missing record can be accepted only when all three separately sealed artifacts
are supplied together:

- `distr.protected-history-retirement-allowlist/v1` names every exact missing
  kind, UUID, and baseline record hash and binds the other two artifact IDs;
- `distr.protected-history-retirement-approval/v1` is backed by the exact
  baseline `ApprovalRequest`, approving `ApprovalDecision` records, and applied
  or verified `SampleRetirementJob`; and
- `distr.protected-history-sample-membership/v1` is backed by exact baseline
  `SampleRetirementOwnershipEvidence` and applied `SampleRetirementItem` records
  and proves that each protected record is directly bound to the named
  application, deployment target, or environment.

All three artifacts bind the same baseline artifact, canonical scope, immutable
preview checksum, and exact item set. The allowlist cannot authorize itself, and
any missing artifact, stale or unused allowance, or indirect membership fails.

This exception is restricted to the existing sample-domain retirement purpose.
It cannot authorize patterns, additions, modifications, cross-scope changes, or
ordinary retention cleanup.

## Consequences

Schema rollback requires additional disk capacity for parallel PostgreSQL and
RustFS volumes and incurs a short outage only during `restore-apply`. Operators
must retain the exact prior handoff, both backups and sidecars, the protected
history baseline, aggregate restore snapshot, switch journal, and any three-part
retirement authorization. Diagnosis is safer because no failed or previous
volume is erased automatically; retirement of retained volumes is a later,
separately reviewed operation.

Each registered schema projection reflects only tables available at that schema,
so exact comparisons are made only between artifacts from the same schema. This
is appropriate for backup/restore equality, avoids fabricating unavailable
history, and forces a deliberate projection review whenever a later migration
introduces protected records.

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

- Focused Go tests cover canonical three-artifact retirement authorization,
  exact equality, additions, modifications, missing records, stale/unused
  allowances, direct membership, schema mismatch, and CLI behavior.
- Database tests prove the complete schema-138 whole-row projection, explicit
  schema 166-169 registration, and refusal of an unknown future schema.
- `hack/test-server-compose-restore.sh` proves aggregate snapshot binding,
  validation-before-switch order, the stopped-source history fence, durable
  journal transitions, crash recovery, volume retention, and CLI arity.
- `bash -n`, focused Go tests, and diff checks are required before integration.
