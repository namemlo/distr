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

Ordinary migration startup cannot perform the non-empty contract accidentally. Expected precondition failures
occur before golang-migrate is invoked. An audited recovery command is still required for an unexpected dirty
version; it may force only a catalog shape proven to match either the exact predecessor or exact contracted schema.

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

- PostgreSQL 16 and 18 jobs cover clean install/latest/full-down, 137-to-138 upgrade, atomic zero-history/manifest-required
  transition markers, expand rollback before apply, non-UTC and daylight-saving session zones, lock/statement timeouts,
  and dirty recovery.
- Repository tests prove paired dual-writes for create, callback, timeout, failure, and event paths.
- A neutral five-execution fixture proves partial backfill, unresolved contract refusal, superseding manifest,
  contract, and exact rollback.
- API tests prove unchanged keys/null behavior and deterministic UTC `Z` output after contract.
- A real backup is restored and verified in isolation before any non-empty contract deployment.
