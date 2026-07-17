# PR-054A - External-Execution Timestamp Expand

## Status

Tasks 1-10 are implemented locally. Migration 138, paired dual-write runtime support, manifest/provenance tooling,
startup compatibility, fenced Compose orchestration, audited dirty-marker recovery, the PostgreSQL 16.14/18.4
workflow definition, and community-neutral operator documentation are present.
Task 11 acceptance, Docker-capable PostgreSQL matrix execution, publication, and deployment remain pending.

## Generic User Story

As a community operator, I want historical wall-clock execution values to remain distinguishable from proven
instants, so that later schema work can preserve evidence without silently inventing an original UTC offset.

## Decisions

- Physical expand migration: migration 138.
- Architecture decision: ADR-0055.
- Durable zero-history proof: `ExternalExecutionTimestampExpandState`.
- Logical contract migration: the next unused contiguous number only when the contract release is shippable; 163 is conditional on every currently planned migration landing first.

## Scope

- Ship additive migration 138 without reserving a physical contract migration.
- Retain legacy timestamp reads and public fields while dual-writing future instants to paired shadow columns.
- Provide complete manifest/provenance tooling, startup compatibility checks, and fenced deployment orchestration.
- Provide one audited, catalog-proven migration-138 dirty-marker repair path without adding a general Force
  mechanism or release finalizer.
- Preserve all implemented roadmap history, execution/audit evidence, and existing PR identifiers.
- Keep unresolved historical values fail-closed; contract eligibility and canonical instant reads remain later work.

## Evidence Index

| Evidence                       | Source                                                                                                                            |
| ------------------------------ | --------------------------------------------------------------------------------------------------------------------------------- |
| Accepted architecture decision | [`ADR-0055`](../adr/0055-external-execution-timestamp-instants.md)                                                                |
| Approved hybrid design         | [`External-execution TIMESTAMPTZ hybrid design`](../superpowers/specs/2026-07-15-external-execution-timestamptz-hybrid-design.md) |
| Implementation plan            | [`External-execution timestamp expand`](../superpowers/plans/2026-07-15-external-execution-timestamp-expand.md)                   |
| Extension allocation ledger    | [`Enterprise operator control-plane program`](../superpowers/plans/2026-07-14-enterprise-operator-control-plane-program.md)       |
| Deterministic allocation check | [`pr054a-validate-timestamp-expand.mjs`](../../hack/pr054a-validate-timestamp-expand.mjs)                                         |

## Required Impact Report

### Database/schema impact

Migration 138 additively adds six nullable instant shadows, paired future defaults, future indexes, immutable
manifest/provenance metadata, retention tombstones, contract-gate foundations, and durable zero-history proof. It
does not replace legacy columns or make unresolved cells canonical. Organization retention deletes execution/event
source rows only through a transaction-local authorized operation that atomically records a complete append-only
timestamp tombstone set. A superseding manifest may resolve an already retained unresolved cell as provenance only;
the tombstone remains unchanged, its deletion must precede the promoting manifest's applied time, and no deleted
shadow is recreated. Verification and readiness expose live resolved/unresolved shadows separately from deleted
resolved/unresolved evidence.

### Public API impact

None.

### Frontend/UI impact

None.

### Agent/protocol impact

None.

### Security impact

Timestamp source deletion gains an application-integrity and accidental-delete guard under the release's trusted
database-owner credential model. It requires explicit evidence before a historical wall clock may become an
instant, rejects context-free source deletion, and accepts a missing source at readiness only when immutable
authorized tombstones preserve its complete timestamp evidence. Custom GUCs and append-only triggers are not a
database privilege boundary against that trusted owner. Separate least-privilege Hub/migrator roles and direct
ledger-insert denial are deferred hardening.

### Backward-compatibility impact

The expand release preserves implemented PR history, public field names, null behavior, existing execution records,
legacy read behavior, and ordinary organization retention. Tenant cleanup may remove its source rows without
invalidating other tenants, while unexplained source loss still prevents readiness. Downgrade to schema 137 is
supported only for an untouched `ZERO_HISTORY` transition or before a non-empty manifest is applied and before
retention. Any tombstone, any post-expand execution/event under `ZERO_HISTORY`, or any `APPLIED`/`VERIFIED` manifest
blocks downgrade; later recovery uses compatible retained columns or verified backup restore.
Migration 138 down repeats these checks under exclusive locks in its own transaction, closing the gap between the
runner's read-only preflight and migration execution; a refusal returns the migration marker to clean schema 138
through bounded cleanup even when the caller cancels while the lock is pending.

### Adjacent functional blocker

PR-054A proves timestamp preservation for the real direct Organization -> ExternalExecution ->
ExternalExecutionEvent cascade with all nine execution/event foreign-key objects retained. It does not assert that
all pre-existing organization graphs are purgeable. A valid modern plan/target/task graph reproduces organization
cleanup failure at `deploymentplantarget_target_fk` because the target is cascaded while the plan target still
references it with `ON DELETE RESTRICT`. Full organization purge ordering is a separate functional blocker/backlog
item and must be assessed before overall EMLO completion.

## Audited Dirty-Marker Recovery

The operator-facing command is:

```bash
./deploy/server-docker-compose/deploy.sh \
  timestamp-expand-recover-dirty \
  <approved-manifest-or-> \
  <evidence-dir> \
  <operator-identity> \
  <reason>
```

The literal `-` selects no-manifest mode and is not standard input. Only exact `PREDECESSOR_137` and `EXPAND_138`
catalogs are accepted. The catalog selects marker 137 or 138 respectively; the observed dirty marker may be either
137 or 138 and never selects the target. Partial, mixed, unknown, and contract-gated catalogs are refused.

Empty predecessor history uses `-`; non-empty predecessor history uses the exact approved root manifest. Durable
expand `ZERO_HISTORY` uses `-`. Pre-apply manifest-required expand history uses the exact `APPROVED` root document;
already verified evidence uses the exact `APPROVED` document whose content matches the stored `VERIFIED` tip. The
wrapper binds the plan to the active writer fence, exact fenced image, evidence bundle, catalog checksum, manifest
mode and document checksum, non-secret operator identity, and non-secret reason.

Both no-manifest branches require that complete capture bundle and active `CAPTURED_WRITERS_STOPPED` fence to predate
migration. The wrapper cannot reconstruct them after an interrupted ordinary zero-history `release`; that failure
requires verified restore or escalation.

The command repairs only `schema_migrations`. It performs no DDL or external-execution data mutation and never starts
Hub, persists compatibility, or clears the fence. After empty-history clean predecessor repair, normal
`timestamp-expand-cancel` exits the staged fence only after its unchanged-schema checks pass and persists the source
image identity. Stop there for rollback. To proceed forward, restore the exact target `DISTR_IMAGE_REF`,
`DISTR_RELEASE_COMMIT`, and `DISTR_IMAGE_DIGEST` together from the original immutable handoff, run `check`, then
restart ordinary zero-history `release`. Non-empty predecessor outcomes rerun normal `timestamp-expand-apply` with
the same approved root evidence. Clean expand outcomes with manifest evidence rerun normal `timestamp-expand-apply`
with identical approved content and evidence. Clean expand `ZERO_HISTORY` remains stopped and fenced and must be
escalated because no no-manifest finalizer currently exists.

The retained evidence set is:

- `timestamp-dirty-recovery-plan.json` and `.sha256`;
- `timestamp-dirty-recovery-result.json` and `.sha256`; and
- `timestamp-dirty-recovery-result.interrupted-NNN.partial` and `.sha256`.

Retries use the same manifest mode and exact approved content/checksum, evidence directory, identity, reason, and
active fence. The source manifest pathname may differ only when the staged approved bytes remain identical. A valid
retained plan may continue after the marker is already clean at its catalog-selected target. A valid retained result
is reused without another recovery Apply. Direct marker edits, raw/manual `migrate force`, and Force calls outside
the audited recovery runner remain prohibited.

A fresh marker repair records `action: "FORCED"`. A retained-plan retry that finds the catalog-selected marker
already clean records `action: "OBSERVED_ALREADY_CLEAN"`. Both results are `SUCCEEDED` with clean `postStatus` at
`forceVersion`.

## Validation

Run `node hack/pr054a-validate-timestamp-expand.mjs`, `bash hack/validate-migrations.sh`, and
both the default and focused recovery Compose harnesses:

```bash
bash hack/test-server-compose-timestamp-expand.sh
DISTR_TIMESTAMP_TEST_GROUP=dirty-recovery \
  bash hack/test-server-compose-timestamp-expand.sh
```

These commands verify the package, migration sequence, fenced orchestration, recovery artifact/retry contract, and
non-finalization boundary. The focused GitHub workflow runs the exact serialized timestamp-expand Go packages on
PostgreSQL 16.14 and 18.4. Task 11 must record Docker-capable matrix and acceptance evidence before publication or
deployment.

## Operator Evidence and Release Record

- Compatibility gate: PostgreSQL 16.14 and PostgreSQL 18.4.
- Database impact: additive migration 138; six nullable instant shadows, future indexes, manifest/provenance and
  deletion-tombstone tables, contract-gate foundation, and durable zero-history state.
- API impact: none; expand reads and public JSON remain on the legacy columns.
- UI impact: none.
- Agent protocol impact: none.
- Deployment impact: non-empty schema 137 databases require fenced capture, backup and isolated restore, independent
  manifest review, explicit migration, apply/verify, and `serve --migrate=false`.
- Dirty-recovery impact: only the audited wrapper may repair a proven dirty marker; retained plan/result/archive
  evidence is mandatory, and normal release finalization remains a separate step.
- Rollback limit: schema 138 may return to 137 only while retention evidence is empty, no manifest is
  `APPLIED`/`VERIFIED`, and an immutable `ZERO_HISTORY` transition has no post-expand execution/event rows; afterward
  recovery uses retained legacy columns with a compatible binary or verified backup restore. The down migration
  rechecks these conditions while exclusively locking the timestamp source/evidence tables.
- Contract status: excluded from this PR; unresolved cells remain fail-closed.

The retained release record names the source commit, image digest, schema before/after, manifest and database
identity checksums, backup and restore checksums, component release identity, dependency manifest identity,
operator, reviewer, and previous-known-good digest. It contains no adopter credentials or workload data.
