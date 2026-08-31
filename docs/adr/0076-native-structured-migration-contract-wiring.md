# ADR-0076: Native Structured Migration Contract Wiring

## Status

Accepted.

## Context

Migration 147 introduced the complete `MigrationContract`, safe graph
expansion, append-only `DeploymentPlanMigration` storage, and migration
preflight/recovery evidence. The native Component Release, Product Release,
and Target Deployment Plan flows still carried only the earlier symbolic
`MigrationDeclaration`. As a result, publication did not freeze the complete
contract, target planning emitted one generic `component.migrate` step, and
the migration-147 rows were left empty.

Database and data migrations must retain their exact source/result schema,
checksums, backup and probe requirements, locks, retry identity, recovery
classification, artifact digest, and dependency graph from build publication
through target-plan execution. Existing releases and plans without structured
migrations must keep their historical canonical bytes.

## Decision

`migrationContracts` is an additive, `omitempty` collection on Component
Release v2, each frozen Product Release component, and each target-plan release
pin. Target-plan canonical JSON also contains the ordered aggregate
`migrationContracts` collection.

Component Release validation requires every `database` or `data` declaration
to have exactly one complete structured contract with the same ID and
component key. Runtime declarations remain compatible without a database
contract. Contract checksums are calculated from normalized canonical content
and checksum drift is rejected.

Product Release snapshots copy the complete child contracts. Validation
checks declaration ownership, checksum integrity, global ID uniqueness,
missing dependencies, and cycles. Canonical Product Release bytes include the
complete contracts, so changing any migration fact changes the release
checksum.

Target planning validates the contract against the pinned component and the
physical `ComponentInstance.DatabaseBoundary`. It orders the complete
migration DAG and calls `migrationplanning.ExpandMigrationGraph` for each
contract. The expanded graph freezes backup creation/verification,
preconditions, apply, postconditions, locks, retry identity, and exact input
checksums. Product and target requirement gates precede every component entry
step, and component deployment waits for migration validation. Adapter
requirements bind structured migrations to `migration:<id>:apply`.

Publication projects every complete contract and its apply/validate step keys
into the existing migration-147 `DeploymentPlanMigration` table. No new schema
migration is introduced.

## Consequences

- Native release and plan checksums now cover the full database-change safety
  contract.
- Planning fails closed on missing, drifted, cyclic, cross-component, or
  database-boundary-mismatched contracts.
- Planned schema state uses the exact resulting version/checksum and explicit
  forward-fix facts instead of a synthetic declaration summary.
- Existing releases and plans with no structured migrations retain their
  previous JSON shape because all new collections are omitted when empty.
- Runtime-only symbolic migrations remain available for non-database work.

## Alternatives Considered

Creating migration 169 was rejected because migration 147 already provides
the required immutable relational storage. Keeping full contracts only in the
target plan was rejected because Product Release publication would not freeze
what CI built. Continuing to emit a generic `component.migrate` step was
rejected because it discards backup, probes, lock, retry, and recovery facts.

## Validation

Focused tests cover Component Release 1:1 validation and checksum drift,
Product Release canonical freezing and graph validation, target graph safety
expansion, adapter step keys, database-boundary validation, complete
migration-147 projection, exact planned schema state, API mapping, and
historical empty-field omission. No live system or client database is required
for this wiring change.
