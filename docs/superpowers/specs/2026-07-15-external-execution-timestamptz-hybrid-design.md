# External-execution TIMESTAMPTZ hybrid design

Status: approved for Distr-only design and implementation on 2026-07-15.

Base: `35318e6d40e66c15c1fd95091b16605a9a618d20` (`codex/ci-production-audit-baseline`).

Architecture decision: `docs/adr/0055-external-execution-timestamp-instants.md`.

## 1. Scope boundary

This design changes only the Distr Hub repository and its control-plane PostgreSQL schema, migration tooling, and operational metadata. It does not change an adopter's workload source, agent protocol, application database, or business data. Pilot-specific authorization, credentials, evidence locations, and cleanup decisions remain in the external operator runbook and never enter this community-neutral repository.

Control-plane authorization does not turn an unresolved timestamp into a UTC attestation; evidence decisions remain fail-closed.

## 2. Decision

Use the conventional **expand–migrate–contract** pattern:

1. **Expand (migration 138):** add nullable `TIMESTAMPTZ` shadows, append-only provenance, and an immutable expand-transition state while retaining every original naive timestamp and existing API behavior.
2. **Migrate:** dual-write new timestamps, inspect historical cells, record evidence decisions, and backfill only cells classified `PROVEN` or explicitly `ATTESTED`.
3. **Contract (logical `external-execution-timestamp-contract`, separate later release):** refuse to run until every populated historical cell is resolved, then atomically make the instant columns canonical while retaining exact raw history and rollback evidence.

The contract does not reserve a physical migration number. It receives the next contiguous version only when it is shippable. The existing migration runner applies every embedded version, so reserving an unshippable number would either block unrelated roadmap work or tempt a gap that the repository validator rejects. The expand and contract can never ship in the same release.

This follows established enterprise practice rather than introducing a custom migration model:

- PostgreSQL documents that most `ALTER TABLE` forms acquire strong locks, that `USING` controls type conversion, and that type changes remove statistics and should be followed by `ANALYZE`.
- GitLab's published zero-downtime migration guidance recommends adding a new-format column, copying data, deploying compatible application code, migrating remaining data, and contracting only in a later deployment.

Primary references:

- https://www.postgresql.org/docs/16/sql-altertable.html
- https://www.postgresql.org/docs/16/explicit-locking.html
- https://www.postgresql.org/docs/16/runtime-config-client.html
- https://docs.gitlab.com/development/database/avoiding_downtime_in_migrations/

## 3. Problem and current evidence

Migration 136 created six operational values as `TIMESTAMP WITHOUT TIME ZONE`:

- `ExternalExecution.created_at`
- `ExternalExecution.updated_at`
- `ExternalExecution.started_at`
- `ExternalExecution.completed_at`
- `ExternalExecution.callback_deadline_at`
- `ExternalExecutionEvent.created_at`

A naive timestamp is a civil wall-clock value, not an instant. SQL cannot recover its original UTC offset from the value alone.

A read-only Distr evidence snapshot collected on 2026-07-15 had five external executions and five events, with 30 populated timestamp cells:

| Cell group                                      |  Count | Evidence state                              |
| ----------------------------------------------- | -----: | ------------------------------------------- |
| execution `created_at`                          |      5 | `PROVEN`, UTC-equivalent                    |
| execution `started_at`                          |      5 | `PROVEN`, UTC-equivalent                    |
| execution `callback_deadline_at`                |      5 | `PROVEN`, written from Go UTC               |
| timed-out execution `updated_at`                |      1 | `PROVEN`, directly anchored to UTC deadline |
| timed-out execution `completed_at`              |      1 | `PROVEN`, equal to anchored terminal time   |
| timed-out event `created_at`                    |      1 | `PROVEN`, equal to anchored terminal time   |
| four successful execution `updated_at` values   |      4 | `UNRESOLVED`                                |
| four successful execution `completed_at` values |      4 | `UNRESOLVED`                                |
| four successful event `created_at` values       |      4 | `UNRESOLVED`                                |
| **Total**                                       | **30** | **18 proven, 12 unresolved**                |

The current and earlier cached Hub images have identical relevant writer code, and the database/session evidence strongly supports UTC continuity. It does not prove the exact writer identity for every successful terminal cell. The 12 cells therefore remain unresolved unless direct evidence or a named human attestation supplies an explicit source offset. These counts are a dated baseline and test-fixture shape, not deployment acceptance; the first apply uses the exact fenced schema-137 snapshot and its actual counts, while any later decision revision preserves that snapshot and separately verifies current lifecycle evolution.

## 4. Non-negotiable invariants

1. No unresolved wall-clock value is converted into an instant.
2. Every converted historical cell retains its exact original value, explicit evaluated UTC offset, decision class, evidence checksum, manifest checksum, and approving identity.
3. Existing execution/event IDs, statuses, messages, hashes, checksums, sequences, references, and relationships remain unchanged.
4. No execution, event, failed attempt, or audit record is deleted, nulled, or quarantined to make a migration pass.
5. Partial shadow backfill is never exposed as canonical API data.
6. Contract is all-or-nothing across both tables and all six timestamp fields.
7. Proof is scoped to one database snapshot and writer interval; no client inherits another environment's UTC decision.
8. Current public JSON field names, null behavior, ordering, and RFC3339 shape remain unchanged. Returned instants use deterministic UTC `Z` serialization.
9. The migration is compatible with PostgreSQL 16 and PostgreSQL 18.
10. Adopter workload repositories, services, agent protocols, and application databases are not accessed or changed.

## 5. Migration 138 — expand

Migration 138 is expand-first and reversible before contract. Its only changes to existing definitions are the paired UTC-safe defaults described below; it does not rewrite historical values or replace a column type.

### 5.1 Shadow columns

Add these nullable `TIMESTAMPTZ` columns:

`ExternalExecution`:

- `created_at_instant`
- `updated_at_instant`
- `started_at_instant`
- `completed_at_instant`
- `callback_deadline_at_instant`

`ExternalExecutionEvent`:

- `created_at_instant`

The six shadows are added without defaults so PostgreSQL cannot populate historical rows with the migration time. In later statements in the same migration, future-row defaults are paired as follows:

- `created_at_instant`, `updated_at_instant`, and event `created_at_instant`: `CURRENT_TIMESTAMP`;
- legacy execution `created_at` and `updated_at`, plus legacy event `created_at`: `CURRENT_TIMESTAMP AT TIME ZONE 'UTC'`.

The paired defaults describe the same instant while keeping the retained legacy representation as a UTC-naive wall value regardless of the database session timezone. Migration 138 down restores the original `now()` defaults when down is still permitted. Historical values, existing indexes/constraints, and reads remain unchanged during expand.

Migration 138 also builds the future canonical indexes under temporary names while the Hub is stopped:

- `ExternalExecution_organization_status_instant_next` on `(organization_id, status, updated_at_instant DESC, id)`; and
- `ExternalExecution_task_instant_next` on `(task_id, created_at_instant, id)`.

The current indexes remain attached to the legacy columns. Backfill maintains the future indexes so contract needs only a metadata/name swap, not a full index build while holding exclusive table locks.

### 5.2 Manifest table

Create `ExternalExecutionTimestampManifest` with:

- UUID primary key;
- immutable database identity checksum;
- source schema version, fixed to 137 for the first manifest;
- source snapshot start/end instants;
- execution/event counts and canonical raw-cell checksum;
- evidence bundle reference and SHA-256 checksum;
- tool/version and conversion-expression version;
- author, reviewer/approver identities, and approval instant;
- target release commit/image digest fields;
- state: `DRAFT`, `APPROVED`, `APPLIED`, `VERIFIED`, or `REVOKED_BEFORE_APPLY`;
- created/applied/verified timestamps as `TIMESTAMPTZ`.

No secret, DSN, token, password, private path, payload, message, or customer data is stored in the manifest.

The database identity is a logical-dataset fingerprint over the schema version, stable execution/event identifiers, row counts, and canonical raw-cell checksum. It deliberately excludes host names and database names so the same backup can be verified after an isolated restore. Canonical cells use lowercase table/column identifiers, lowercase UUID text, an explicit null marker, and a fixed six-digit microsecond wall-time representation, sorted by table, row UUID, and column ordinal before length-prefixed SHA-256 framing. Domain-separated checksum inputs and their exact ordered fields are fixed in the expand implementation plan; implementations may not substitute JSON serialization or map iteration. A manifest's decision-content checksum excludes lifecycle state/timestamps and is immutable after insertion. Its state may advance only through the ordered lifecycle above, with the corresponding lifecycle timestamp recorded; an invalid or backward transition is rejected. Any content correction creates a new manifest.

Inspection and manifest generation run in one `REPEATABLE READ, READ ONLY` transaction. Counts, stable identifiers, raw values, and checksums come from that one snapshot; the command records the transaction's snapshot start/end instants. First-run inspection accepts exact clean schema 137 and emits an offline manifest/evidence bundle before expand. Later inspection accepts schema 138 or newer while the expand contract still exists. Apply requires migration 138. Every mode rejects a dirty or unrecognized schema.

### 5.3 Per-cell provenance table

Create `ExternalExecutionTimestampCellProvenance` keyed by manifest, source table, source row UUID, and source column. It stores:

- exact original `TIMESTAMP WITHOUT TIME ZONE` value or explicit null marker;
- decision class: `PROVEN`, `ATTESTED`, `UNRESOLVED`, or `NULL_VALUE`;
- source IANA zone when asserted and an explicit evaluated offset in seconds;
- converted `TIMESTAMPTZ` value, nullable for unresolved/null cells;
- evidence reference/checksum and approving identity;
- canonical raw-cell and parent-manifest checksums;
- conversion-expression version; and
- immutable creation timestamp.

Rows are append-only. A correction uses a new, complete manifest and new decision rows; applied rows are never updated or deleted. A database trigger rejects every provenance `UPDATE` or `DELETE`. A separate manifest trigger permits only the documented forward state transition and its matching lifecycle timestamp while rejecting changes to decision-content fields after insertion. `ATTESTED` is not a general deployment approval: it requires a named human assertion for the specific cells and the explicit source offset used for conversion.

Offset sign follows ISO-8601: `local wall clock = UTC + offset`. Conversion version 1 constructs the wall-clock calendar fields in a fixed zone with that offset, so `converted instant = wall clock - offset`; it never uses the database session timezone. Independent verification repeats the formula and includes positive, negative, zero, and DST-fold test vectors.

Database constraints enforce:

- an allowlist of exactly `ExternalExecution` and `ExternalExecutionEvent` and the six named columns;
- at most one row per expected cell per manifest;
- null consistency;
- converted values only for `PROVEN` or `ATTESTED` decisions;
- required offset/evidence/approver fields for converted values; and
- no converted instant for `UNRESOLVED` cells.

The operator verifier, inside the same transaction used to apply a manifest, enforces completeness: exactly one decision row for every expected cell in that manifest and no decision for a missing source row or column. SQL `CHECK` constraints alone cannot prove cross-row completeness.

### 5.4 Expand-transition state

Migration 138 creates `ExternalExecutionTimestampExpandState` and inserts exactly one immutable row in the same migration transaction. It records source version 137, execution/event/raw-cell counts, transition instant, and either:

- `ZERO_HISTORY`, only when both execution tables are empty at the exact 138 transition; or
- `MANIFEST_REQUIRED`, whenever either table contains a row.

An append-only trigger rejects update/delete. Startup may use the no-manifest branch only with the durable `ZERO_HISTORY` row and complete matching legacy/shadow pairs for every row created after expand. A missing marker, a `MANIFEST_REQUIRED` marker, manually paired data, or an unsupported direct schema repair never proves zero history.

### 5.5 Contract gate

Create `ExternalExecutionTimestampContractGate` as a short-lived, one-use operational gate keyed to a verified complete manifest. It records the expected current schema version, allocated contract version, target release commit/image digest, backup checksum/reference, isolated-restore verification checksum/reference, writer-fence identifier, preparer, preparation/expiry instants, and consumed instant. References are opaque identifiers, never credentials or private paths.

Only the Distr operator command can prepare a gate, while holding the shared timestamp-migration advisory lock. It first reproduces the manifest and shadow checks. The supported migration wrapper checks an unexpired matching gate before invoking golang-migrate and consumes it only after the contract succeeds. This makes an accidental ordinary startup or migration attempt fail cleanly before golang-migrate can mark the schema dirty. The only exception is a verified zero-history bootstrap: both execution tables are empty and no manifest exists, so there is no civil timestamp to interpret. The deployment orchestrator remains responsible for actually stopping the Hub and preserving the fence; the durable gate makes that assertion explicit and auditable rather than pretending SQL can observe an external process directly.

### 5.5 Down behavior

The supported `hub migrate` path obtains the timestamp-migration advisory lock and checks manifest state before invoking golang-migrate. A downgrade crossing 138 returns a clean preflight error, before the schema version changes, when any manifest is `APPLIED` or `VERIFIED`. Before application, migration 138 down removes only the additive schema and restores the original defaults. This avoids using an expected SQL exception as flow control, which would leave golang-migrate dirty. Direct execution of embedded down SQL outside the Distr migration command is unsupported. Operational rollback after data application uses the retained old columns and a verified backup rather than deleting evidence.

## 6. Application/schema transition and dual-write

The Hub is stopped during both schema transitions; rolling mixed-writer compatibility is not promised. Two application releases make the column-name contract explicit.

### 6.1 Expand release

The expand-compatible Hub writes both representations from one authoritative instant:

- instant shadow: authoritative `TIMESTAMPTZ` value;
- legacy column: the same instant rendered as a UTC naive wall value for compatibility.

Implementation rules:

- `created_at_instant`, `updated_at_instant`, and event `created_at_instant` receive `CURRENT_TIMESTAMP` defaults only after the empty shadows exist;
- repository inserts explicitly write `callback_deadline_at_instant` from the existing Go UTC deadline;
- state-transition statements capture one PostgreSQL `now()` value, store it in the instant shadow, and store `instant AT TIME ZONE 'UTC'` in the legacy lifecycle field in the same SQL statement/transaction;
- legacy reads remain unchanged before contract;
- shadow fields remain internal and are not exposed by pre-contract repository reads or public APIs; and
- no generic trigger interprets a naive deadline using the session timezone; column-specific writer semantics remain explicit.

Expand deployment order is fixed because a later callback can overwrite mutable raw lifecycle values:

1. stop/fence the pre-expand Hub and all external callback writers;
2. create and checksum the PostgreSQL/object-store backup, restore it in isolation, and verify it;
3. inspect exact schema 137 and generate the complete offline manifest/ledger for every cell, including exact `UNRESOLVED` and `NULL_VALUE` raw evidence;
4. run `migrate --to 138` as a separate bounded job;
5. validate and apply that manifest, then verify the ledger, shadows, counts, and raw checksum while writers remain fenced;
6. start only the expand-compatible Hub with `serve --migrate=false`; and
7. verify readiness and API/audit invariants before releasing the fence.

A zero-history database may skip the manifest only when migration 138 atomically recorded both execution tables empty in the durable expand-transition row.

The expand release leaves legacy timestamp decoding and public mapping unchanged. The contract release normalizes only these six now-known `TIMESTAMPTZ` fields with `.UTC()` in the external-execution repository/mapping path. It does not replace pgx's global `TIMESTAMPTZ` codec, which could change unrelated API timestamps. It must never attach UTC to or reinterpret a retained `TIMESTAMP WITHOUT TIME ZONE` value. Tests must vary both the application host location and PostgreSQL session `TimeZone` across UTC, Asia/Bangkok, and a DST fold/gap zone, and must prove deterministic JSON `Z` output for known instants.

### 6.2 Contract release

The later contract-compatible Hub is built only after the complete manifest and contract migration are independently reviewed. Its repository SQL reads the canonical `TIMESTAMPTZ` columns and continues dual-writing the retained `*_legacy_raw` columns as UTC-naive values for the entire rollback-retention window. It refuses startup when the database schema is still pre-contract or when the expected contract migration/version does not match the build.

Deployment order is fixed:

1. stop/fence the expand Hub and external callback writers;
2. run the contract operator preflight and persist its short-lived gate;
3. run the contract binary's explicit `migrate --to <contract-version>` command under a hard orchestration/job timeout;
4. run post-commit catalog checks and targeted `ANALYZE`;
5. start only the contract-compatible Hub with `serve --migrate=false`; and
6. verify readiness and API/audit invariants before releasing the fence.

| Schema                     | Allowed Hub         | Read columns              | Write columns                                       |
| -------------------------- | ------------------- | ------------------------- | --------------------------------------------------- |
| 137                        | pre-expand          | legacy canonical          | legacy canonical                                    |
| 138 or later, pre-contract | expand-compatible   | legacy canonical          | legacy canonical plus `*_instant`                   |
| contract version or later  | contract-compatible | canonical instant         | canonical instant plus `*_legacy_raw`               |
| contract rolled back       | expand-compatible   | restored legacy canonical | restored legacy canonical plus restored `*_instant` |

A pre-expand or expand-compatible Hub is never started on a contracted schema, and the contract-compatible Hub is never started on a pre-contract schema.

## 7. Inspection, manifest, and backfill command

Add a Distr-owned operator command with dry-run as the default. It operates only on the configured Distr database.

Required modes:

- `inspect`: read-only; emit counts, raw checksum, writer interval, and unresolved inventory without row payloads or secrets;
- `validate-manifest`: offline plus read-only database identity/count/checksum validation;
- `apply`: require an approved manifest, stopped/fenced Hub, matching schema/count/checksum, and a verified backup/restore reference;
- `verify`: read-only; reproduce conversions and check shadows, ledger, counts, and invariants; and
- `prepare-contract`: require a latest complete `VERIFIED` manifest plus backup, isolated-restore, fence, release, and allocated-migration evidence, then persist the one-use expiring contract gate.

`apply` inserts the immutable manifest/cell ledger in one bounded transaction. A root fills its proven or attested shadows; a later revision fills only newly promoted, unchanged-raw cells whose shadows are still null and never replays previously resolved cells. Unresolved shadows remain null. Reapplying the same manifest is a verified no-op; a conflicting manifest or disallowed raw change aborts.

After writers resume, verification distinguishes immutable history from legitimate lifecycle evolution. Execution `created_at`, `callback_deadline_at`, and event `created_at` remain equal to provenance. A changed execution `updated_at` is valid only as an exact UTC-naive/instant pair at or after the root manifest's verification instant, which remains the stable lifecycle baseline for every later decision revision. An originally null `started_at` or `completed_at` may remain null or transition once to such a paired value; an originally populated value remains equal to provenance. An unchanged unresolved raw value retains a null shadow. These rules let the expand writer advance live executions without reinterpreting the original ambiguous wall clock.

Every manifest is a complete decision snapshot, never a delta. A later manifest may be approved after writers resume, but it is a decision revision over the same original fenced schema-137 snapshot: it names the manifest it supersedes and preserves the source version, snapshot interval, counts, database identity, raw-set checksum, exact cell keys, raw values, and raw-cell checksums. It repeats immutable resolved/null decisions, including every resolved cell's conversion and evidence fields, and supplies new evidence only when an unchanged-raw `UNRESOLVED` cell becomes `PROVEN` or `ATTESTED`. The manifest-level tool version may change to identify the tool that produced the new revision, while the release-fixed conversion-expression version may not. Live `updated_at` or originally-null start/completion evolution is verified separately against provenance and paired shadows and is never substituted into manifest cells. Applying the revision verifies prior resolved cells without replaying their writes, accepts only the defined paired lifecycle evolution, and fills only a newly promoted cell whose retained raw value is still current and whose shadow remains null. Contract eligibility is evaluated against one latest `VERIFIED` complete manifest rather than by combining decisions from multiple manifests.

For the dated pilot evidence baseline, the first approved manifest backfills exactly 18 cells and records 12 unresolved cells. It does not make the contract migration eligible.

## 8. Logical contract migration

The contract migration receives the next unused contiguous number and is written and shipped only after an independently verified manifest decision revision resolves every populated cell in the retained original fenced snapshot and separate live lifecycle verification passes. Later unrelated additive migrations may therefore proceed while historical timestamp evidence is still being resolved. A genuinely empty database follows a separate zero-history bootstrap path because it has no values to attest.

Deployment preflight must first prove that Hub/external writers are fenced and that the backup plus isolated-restore evidence matches the target release and logical-dataset fingerprint. Before invoking golang-migrate, the supported migration wrapper requires the matching unexpired contract gate and validates all of these assertions while the timestamp-migration advisory lock is held:

- the current schema version is exactly the predecessor recorded by the gate, is clean, and still contains the migration-138 expand contract;
- the original schema-137 counts, execution/event IDs, database identity, and raw checksum are reproduced from the latest verified complete manifest plus its append-only provenance, never from current mutable lifecycle columns;
- one provenance decision exists for every expected cell;
- no non-null cell is unresolved;
- every converted value reproduces from raw value plus explicit offset;
- the current database still contains every retained original row and passes the separate lifecycle-evolution verifier against the stable root baseline, while every post-expand row has complete matching legacy/instant pairs;
- both future indexes are valid and cover the expected instant columns; and
- temporal and status invariants pass.

For a zero-history bootstrap, preflight instead proves that both execution tables and the manifest/provenance tables are empty. The contract SQL rechecks that empty shape inside its transaction and proceeds without manufacturing a human approval or a historical manifest. This keeps clean install and full-down migration paths deterministic while never weakening the non-empty path.

Golang-migrate marks the target version dirty before executing its SQL, so the migration file does not inspect `schema_migrations` or use expected assertion failures as normal control flow. Contract runs as one migration-file transaction managed by the PostgreSQL simple-query execution path, with `SET LOCAL lock_timeout` and `statement_timeout` first; it does not embed a second `BEGIN`/`COMMIT` pair:

1. acquire bounded locks on both tables;
2. rename original columns to retained `*_legacy_raw` names;
3. rename instant shadows to the canonical names;
4. restore required defaults and nullability: canonical instant `created_at`, `updated_at`, and event `created_at` use `CURRENT_TIMESTAMP`; their retained legacy-raw peers use `CURRENT_TIMESTAMP AT TIME ZONE 'UTC'`; `callback_deadline_at` remains explicitly written; canonical `created_at`, `updated_at`, `callback_deadline_at`, and event `created_at` are non-null, while `started_at` and `completed_at` retain their existing nullable semantics;
5. rename each current legacy index to its `*_legacy_raw` name and each prebuilt `*_instant_next` index to the original canonical index name;
6. verify catalog types, defaults, counts, checksums, index definitions, ordering, and temporal invariants; and
7. commit only after all assertions pass.

Targeted `ANALYZE ExternalExecution, ExternalExecutionEvent` runs as an explicit post-commit operator step before the contract Hub starts. Statistics are operationally important but are not part of atomic correctness and must not extend the exclusive-lock transaction. The raw legacy columns, legacy indexes, and provenance tables remain through the rollback and audit-retention window. They are not dropped by the contract migration.

## 9. Rollback and recovery

### Expand dirty-marker recovery

Unexpected migration-138 failure has a narrow audited marker-recovery command. It accepts only an exact
`PREDECESSOR_137` catalog (migration DDL rolled back) or exact `EXPAND_138` catalog (migration DDL committed but the
clean-version write failed). `PREDECESSOR_137` selects marker 137 and `EXPAND_138` selects marker 138; the observed
dirty marker may be 137 or 138 and never selects the force target. Empty predecessor history and durable expand
`ZERO_HISTORY` use the literal no-manifest sentinel `-`. Non-empty predecessor history requires the exact approved
root document. Pre-apply manifest-required expand history requires that same root document; already verified
evidence requires the exact original `APPROVED` document whose content matches the stored `VERIFIED` tip.

No-manifest recovery requires its complete captured evidence bundle and active fence to predate migration. It does
not retrofit an interrupted ordinary zero-history release, which creates neither record; that failure requires
verified restore or escalation.

The supported Compose wrapper revalidates the active writer fence, fenced image, captured evidence checksum, and
manifest mode before invoking recovery. It records checksummed plan/result evidence plus durable numbered archives
for interrupted results. A valid retained plan may be applied after the marker is already clean, and a valid retained
result is reused without another Apply. The runner's single confined golang-migrate `Force` call repairs only
`schema_migrations`; recovery performs no DDL or external-execution data mutation and never starts Hub, persists
compatibility, or clears the fence. Raw/manual Force, direct marker edits, mixed, unknown, contract-gated, and
contracted catalogs, and any attempt to bypass failed preflight remain prohibited.

The result action is `FORCED` when Apply observes and repairs the exact planned dirty status. An exact retained-plan
retry that observes the catalog-selected marker already clean records `OBSERVED_ALREADY_CLEAN` without another
Force. Both actions are `SUCCEEDED` with clean post-status at the planned force version.

After empty-history predecessor repair, normal `timestamp-expand-cancel` exits the staged fence only after its
unchanged-schema checks pass and persists the source image identity. The operator stops there for rollback. Before a
forward ordinary zero-history release, the exact target image reference, commit, and digest are restored together
from the original immutable handoff. After non-empty predecessor repair, the operator reruns normal
`timestamp-expand-apply` with the same approved root evidence. After expand repair with manifest evidence, the
operator reruns normal apply/finalization with identical approved content and evidence. After
`EXPAND_138`/`ZERO_HISTORY` repair, Hub remains stopped and fenced and the operator escalates because this release has
no no-manifest finalizer for that state.

The later contract release must define and independently approve its own recovery procedure. This expand command is
not a contract migration recovery mechanism.

Before contract, rollback uses the untouched original columns and previous Hub image.

After contract, rollback:

1. stops/fences the contract Hub and verifies the same manifest/database identity;
2. renames each canonical instant column back to its `*_instant` name;
3. renames each active canonical instant index back to `*_instant_next` and each retained `*_legacy_raw` index back to its canonical index name;
4. renames each retained `*_legacy_raw` column back to its canonical legacy name and restores the paired pre-contract defaults/nullability;
5. preserves every post-contract row through the contract Hub's continuing UTC-naive legacy dual-write;
6. validates exact raw checksums, counts, index definitions, API behavior, and schema cleanliness;
7. starts only the expand-compatible Hub with `serve --migrate=false`; and
8. falls back to the verified isolated-restore procedure if any assertion fails before restart.

A blanket reverse `AT TIME ZONE 'UTC'` is prohibited because it cannot reproduce a non-UTC original wall value.

## 10. Deployment gates for the Distr database

Although this change is confined to the Distr control-plane database, deployment remains mechanically gated:

1. accepted source commit and immutable image digest;
2. mandatory release workflow green; generic vulnerability suppression remains prohibited, while only the exact
   reviewed, expiring, fail-closed PR-050 policy may accept findings;
3. Hub/external writers stopped or fenced before backup;
4. PostgreSQL and object-store backup checksummed;
5. full isolated restore and schema/data checksum proof;
6. exact database-specific manifest and raw snapshot checksum;
7. migration dry run and unresolved inventory reviewed;
8. bounded maintenance window, lock timeout, statement timeout, and rollback command;
9. post-start `/ready`, login, schema, image-digest, audit, task/lock, and execution-history checks; and
10. retained evidence bundle and previous-known-good release.

If audited dirty-marker recovery was required, the retained release evidence additionally includes its checksummed
plan, result, and interrupted-result archives. Marker repair is not a deployment gate completion: the applicable
normal finalization path must still pass, or `EXPAND_138`/`ZERO_HISTORY` must remain stopped and fenced for escalation.

Any tutorial or demo-data cleanup is a separate later workflow and is never combined with this timestamp migration.

## 11. Testing and verification

### 11.1 Migration matrix

Run pinned PostgreSQL 16 and PostgreSQL 18.4 jobs covering:

- clean install through migration 138;
- clean install through the dynamically numbered contract/latest schema and full down to zero with no synthetic history;
- 137 -> 138 -> 137 before any applied manifest;
- UTC, Asia/Bangkok, and one DST fold/gap session;
- fixed naive sentinels, null values, and event values;
- exact catalog/default/index assertions;
- lock/statement timeout behavior; and
- clean preflight refusal before golang-migrate, externally bounded advisory-lock waiting, and both audited dirty-recovery catalog cases.

### 11.2 Five-execution compatibility fixture

Create a five-execution/five-event fixture matching the live aggregate shape:

- four `SUCCEEDED`, one `TIMED_OUT`;
- one event per execution;
- 18 `PROVEN` UTC decisions and 12 `UNRESOLVED` decisions.

Prove:

- expand/backfill populates exactly 18 historical shadows;
- contract eligibility fails with all 12 unresolved cells listed;
- a separately approved attestation/evidence manifest resolves them without editing prior ledger rows;
- contract then succeeds without changing IDs, statuses, messages, payload hashes, checksums, sequences, or counts; and
- rollback restores exact raw values.

### 11.3 Repository/API behavior

- existing external-execution repository/API/worker tests remain green;
- new writes populate both representations atomically;
- deadline expiration is timezone-independent;
- API keys, null behavior, ordering, callback sequence rules, and status transitions are unchanged;
- JSON serializes known instants with `Z` on UTC and non-UTC hosts; and
- the expand and contract binaries each refuse the incompatible schema shape; and
- full Go, vet, release lint, Angular, build, container, license, vulnerability, migration, and live-demo gates
  pass; the vulnerability gate permits only the exact reviewed, expiring, fail-closed PR-050 policy.

## 12. Migration numbering

- 138: external-execution timestamp expand/provenance
- 139 onward: existing roadmap work uses the next contiguous version (the currently documented PR-056 allocation shifts from 138 to 139)
- logical `external-execution-timestamp-contract`: receives the next unused version only in its independently shippable release; if it follows the currently planned roadmap migrations, its expected version is 163

The implementation plan must update every committed roadmap allocation and cross-reference before a conflicting migration file merges. The repository validator must always see one contiguous sequence with no placeholders or gaps.

## 13. Acceptance criteria

The hybrid prerequisite is complete only when:

- migration 138, dual-write, command, and tests are independently approved;
- a real Distr backup has been restored and verified in isolation;
- the exact Distr database has a verified manifest and complete provenance ledger;
- the latest verified decision revision proves or explicitly attests every populated cell in the retained original fenced schema-137 snapshot without editing raw history (the 2026-07-15 baseline contained 30);
- the dynamically numbered contract migration is independently approved, applied, and rollback-rehearsed;
- public API and execution behavior are unchanged;
- pilot history and audit evidence are intact; and
- any dirty-marker recovery retained its complete checksummed audit records and was followed by the applicable
  normal finalization, while expand zero-history repair remained stopped and fenced for escalation; and
- no out-of-scope repository or client application database was touched.
