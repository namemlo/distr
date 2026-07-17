# ADR-0055: External Execution Timestamp Instants

## Status

Accepted

## Context

Migration 136 stored the five `ExternalExecution` lifecycle timestamps and
`ExternalExecutionEvent.created_at` as `TIMESTAMP WITHOUT TIME ZONE`. Those values represent civil wall clocks,
not instants, and the original UTC offset cannot be recovered from SQL data alone. Reinterpreting every historical
value as UTC would create false audit evidence. Changing the columns in place would also combine an irreversible
data decision, a locking type rewrite, and an application compatibility change in one release.

Distr must preserve existing execution history and public API behavior, support databases with and without
historical executions, and remain compatible with PostgreSQL 16 and 18.

## Decision

Use an expand-migrate-contract transition.

Migration 138 adds nullable `TIMESTAMPTZ` shadow columns, paired future-row defaults, future canonical indexes, a
manifest, per-cell provenance, an immutable expand-transition state row, and a short-lived contract gate. It never backfills a historical shadow merely
because a raw value exists. Historical conversion requires a complete manifest with an explicit offset and a
`PROVEN` or `ATTESTED` decision for that cell; unresolved cells remain null. Provenance is append-only and manifest
content is immutable after insertion.

The expand-compatible Hub keeps reading the legacy columns and dual-writes each instant to the shadow plus a UTC
naive legacy representation. Manifest generation uses one repeatable-read schema-137 snapshot and canonical
SHA-256 framing. A superseding manifest is a complete decision revision over that same original fenced snapshot;
it never substitutes later live lifecycle values into manifest cells. The operator command is dry-run by default
and validates database identity, row counts, checksums, evidence, and idempotency before applying a manifest.

Normal organization retention records one immutable tombstone for every deleted execution/event timestamp cell.
The delete is authorized with a transaction-local operation identity and fixed retention reason; direct or
unexplained deletes fail closed. Readiness accepts either the retained source cell or its exact authorized
tombstone, never both, and continues to validate the original provenance and permitted lifecycle evolution.
Tombstones have no organization foreign key, so one tenant's completed retention cannot invalidate another tenant
or prevent all Hub replicas from becoming ready.

A later complete manifest may promote an unresolved cell whose source was already removed by authorized retention.
That promotion is provenance-only: the immutable tombstone remains unchanged, its null instant is never backfilled,
and no live shadow row is required or created. The tombstone deletion time must precede the promoting manifest's
applied time; the deletion trigger records one statement-time `clock_timestamp()` for the complete tombstone set,
preventing a long-running retention transaction from disguising a later deletion as older provenance-only evidence.
Verification and readiness report resolved/unresolved live shadows separately from resolved/unresolved deleted
evidence and still reject any manifest/tombstone raw-value mismatch.

The transaction-local retention context is an application-integrity and accidental-delete guard for deployments
where the Hub and migrator use trusted database-owner credentials. It is not a PostgreSQL privilege boundary:
the trusted owner can set custom GUCs or bypass object guards. Separate least-privilege runtime/migrator roles and
direct ledger-insert denial remain deferred hardening and are not claimed by this expand release.

Post-start verification keeps creation/deadline/event history immutable while allowing only defined paired lifecycle
evolution: `updated_at` may advance as one UTC-naive/instant pair, and an originally null start/completion may be
filled once as a pair. An originally populated unresolved lifecycle value is never inferred or overwritten.

Migration 138 atomically records whether both execution tables were empty at the transition. A later no-manifest
startup is permitted only with that immutable `ZERO_HISTORY` marker and complete paired values for every row created
after expand. A missing marker, a `MANIFEST_REQUIRED` marker, or paired values alone cannot substitute for historical
provenance.

For a non-empty database, the Hub and callback writers are fenced before backup and schema-137 inspection. The
complete offline raw-cell manifest is generated before migration 138, then persisted and verified immediately after
138 and before the dual-write Hub starts. This preserves mutable unresolved lifecycle values before any callback can
overwrite them. Expand deployment uses an explicit migration command and starts Hub with automatic migration
disabled.

Contract is a separate logical change and receives the next contiguous migration number only when it is
shippable. It requires a latest complete verified decision revision over the original fenced snapshot for every
non-empty database, separate verification of permitted live lifecycle evolution, a verified backup and isolated
restore, a writer fence, and an expiring one-use contract gate. A zero-history database may contract after proving
all execution and provenance tables empty. The contract-compatible Hub reads canonical instant columns and
continues dual-writing retained `*_legacy_raw` columns throughout the rollback window.

Contract deployment runs an explicit migration command under a bounded maintenance job and then starts Hub with
automatic migration disabled. The contract migration swaps prebuilt indexes and column names in one
runner-managed transaction. Targeted `ANALYZE` runs after commit and before Hub startup. Rollback reverses the
column and index swaps before restarting the expand-compatible Hub.

This decision changes no public field name, null behavior, execution state machine, agent protocol, or adopter
application database.

## Consequences

Historical ambiguity is visible and fail-closed rather than silently normalized. The additive release is safe to
run while evidence is incomplete, and unrelated later migrations are not blocked by a reserved version. The cost
is two application/schema phases, additional provenance storage, an operator command, and a maintenance window for
the final metadata swap.

Retention no longer requires retaining source rows forever, but the timestamp evidence itself remains append-only.
Disabling the deletion/tombstone guards or losing any member of a tombstone set is treated as corruption.

Migration 138 cannot be downgraded while any retention tombstone exists. An untouched `ZERO_HISTORY` transition may
return to 137, but the downgrade is refused once any post-expand execution or event exists, even if no manifest was
needed. The existing refusal after an `APPLIED` or `VERIFIED` manifest remains unchanged. The runner performs the
read-only preflight, then migration 138 down repeats the refusal checks while holding the timestamp source and
evidence tables under `ACCESS EXCLUSIVE` locks. A guard rejection restores the migration marker to clean schema 138.
That marker repair uses a bounded cleanup context independent of caller cancellation, so cancellation while the down
migration waits for a writer cannot leave an intact schema 138 falsely marked dirty 137.

Ordinary migration startup cannot perform the non-empty contract accidentally. Expected precondition failures
occur before golang-migrate is invoked. An unexpected dirty marker at 137 or 138 may be repaired only by the audited
PR-054A recovery path. It accepts only the exact `PREDECESSOR_137` or `EXPAND_138` catalog shape:
`PREDECESSOR_137` selects marker 137 and `EXPAND_138` selects marker 138. The observed dirty marker is recorded
evidence of interruption and does not select the force target.

The supported Compose wrapper revalidates the active fence, exact target image, checksummed capture evidence, and
required approved manifest before invoking that recovery. For already verified evidence, the supplied JSON remains
the exact original `APPROVED` document whose content matches the stored `VERIFIED` tip. It retains checksummed
plan/result records and interrupted result archives, performs no DDL or external-execution data mutation, and never
starts Hub, persists compatibility, or clears the fence. Direct `schema_migrations` edits, raw/manual
`migrate force`, and `.Force(` calls outside `internal/migrations/timestamp_dirty_recovery_runner.go` remain
prohibited.

No-manifest recovery additionally requires the complete capture bundle and active fence to predate migration. It
cannot retrofit an interrupted ordinary zero-history release; without those records, recovery is verified restore or
escalation. After empty-history clean predecessor repair, guarded normal `timestamp-expand-cancel` exits the staged
fence and persists the source image identity. Stop there for rollback. Before a forward ordinary zero-history
release, restore the exact target image reference, commit, and digest together from the original immutable handoff. A
non-empty clean predecessor result returns to normal `timestamp-expand-apply` with the same approved root evidence. A
clean expand result with manifest evidence returns to normal `timestamp-expand-apply` with identical approved content
and evidence. A clean `EXPAND_138` result with durable `ZERO_HISTORY` remains stopped and fenced for escalation
because this release has no no-manifest finalizer for that post-repair state.

PR-054A proves timestamp-evidence compatibility for the direct Organization -> ExternalExecution ->
ExternalExecutionEvent retention cascade. It does not claim that every pre-existing modern organization graph is
purgeable: a valid plan/target/task graph currently reproduces a separate
`deploymentplantarget_target_fk` `ON DELETE RESTRICT` failure during organization cleanup. That purge-order issue is
a functional blocker tracked outside this timestamp migration.

## Alternatives Considered

- Convert every raw value with `AT TIME ZONE 'UTC'`. Rejected because a naive value contains no source offset.
- Alter the six columns in place. Rejected because it combines uncertain conversion, stronger locks, and an
  incompatible application change.
- Reserve the next physical migration number until evidence is complete. Rejected because the repository requires
  contiguous migrations and unrelated roadmap work could be blocked indefinitely.
- Keep naive timestamps permanently. Rejected because new execution audit data requires unambiguous instants and
  deterministic cross-time-zone behavior.
- Replace the global pgx `TIMESTAMPTZ` codec. Rejected because it could change unrelated API timestamps; the
  contract release normalizes only the six external-execution fields.

## Validation

- PostgreSQL 16 and 18 jobs cover clean install/latest/full-down, 137-to-138 upgrade, atomic
  zero-history/manifest-required transition markers, untouched zero-history rollback, active-row/tombstone downgrade
  refusal including a writer between preflight and migration execution, non-UTC and daylight-saving session zones,
  lock/statement timeouts, and dirty recovery.
- Repository tests prove paired dual-writes for create, callback, timeout, failure, and event paths.
- A neutral five-execution fixture proves partial backfill, unresolved contract refusal, live and retained
  `PROVEN`/`ATTESTED` superseding-manifest promotion, deletion-before-promotion ordering, contract, post-apply
  raw-mismatch refusal, and exact rollback.
- API tests prove unchanged keys/null behavior and deterministic UTC `Z` output after contract.
- A real backup is restored and verified in isolation before any non-empty contract deployment.
