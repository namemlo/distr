# PR-054A - External-Execution Timestamp Expand

## Status

Tasks 1-10 are implemented locally. Migration 138, paired dual-write runtime support, manifest/provenance tooling,
startup compatibility, fenced Compose orchestration, the PostgreSQL 16.14/18.4 workflow definition, and
community-neutral operator documentation are present.
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
manifest/provenance metadata, contract-gate foundations, and durable zero-history proof. It does not replace legacy
columns or make unresolved cells canonical.

### Public API impact

None.

### Frontend/UI impact

None.

### Agent/protocol impact

None.

### Security impact

No runtime security boundary changes. The accepted design requires explicit evidence before a historical wall clock
may become an instant.

### Backward-compatibility impact

The expand release preserves implemented PR history, public field names, null behavior, existing execution records,
and legacy read behavior. Downgrade to schema 137 is supported only before any manifest is applied; later recovery
uses compatible retained columns or verified backup restore.

## Validation

Run `node hack/pr054a-validate-timestamp-expand.mjs`, `bash hack/validate-migrations.sh`, and
`bash hack/test-server-compose-timestamp-expand.sh` to verify the package, migration sequence, and fenced
orchestration. The focused GitHub workflow runs the exact serialized timestamp-expand Go packages on PostgreSQL
16.14 and 18.4. Task 11 must record Docker-capable matrix and acceptance evidence before publication or deployment.

## Operator Evidence and Release Record

- Compatibility gate: PostgreSQL 16.14 and PostgreSQL 18.4.
- Database impact: additive migration 138; six nullable instant shadows, future indexes, manifest/provenance tables,
  contract-gate foundation, and durable zero-history state.
- API impact: none; expand reads and public JSON remain on the legacy columns.
- UI impact: none.
- Agent protocol impact: none.
- Deployment impact: non-empty schema 137 databases require fenced capture, backup and isolated restore, independent
  manifest review, explicit migration, apply/verify, and `serve --migrate=false`.
- Rollback limit: schema 138 may return to 137 only before any manifest is applied; afterward recovery uses retained
  legacy columns with a compatible binary or verified backup restore.
- Contract status: excluded from this PR; unresolved cells remain fail-closed.

The retained release record names the source commit, image digest, schema before/after, manifest and database
identity checksums, backup and restore checksums, component release identity, dependency manifest identity,
operator, reviewer, and previous-known-good digest. It contains no adopter credentials or workload data.
