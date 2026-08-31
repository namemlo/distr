# PR-089: Native Structured Migration Wiring

## Outcome

PR-089 connects the complete migration-147 safety contract to the normal
Component Release, Product Release, and Target Deployment Plan workflow.
Database and data changes can no longer reach native target planning with only
a descriptive migration key.

## Publication flow

1. CI publishes a Component Release v2 contract containing a symbolic
   declaration and a checksum-bound `migrationContracts` entry.
2. Component publication verifies the 1:1 declaration/contract mapping,
   component ownership, normalized canonical checksum, backup/probe facts,
   retry identity, recovery classification, and immutable artifact digest.
3. Product Release creation snapshots the complete child contract. Product
   validation orders the cross-component migration DAG and rejects duplicates,
   missing dependencies, and cycles.
4. Product canonical JSON freezes the complete contract per component.

Runtime declarations do not require a database contract. A `database` or
`data` declaration without one is a publication blocker.

## Target-plan flow

Target planning copies the Product Release contracts into exact release pins
and verifies each database resource against the bound physical component
instance. It expands every contract into:

```text
backup:create -> backup:verify -> precondition -> apply -> validate -> deploy
```

Backup steps are present only when required. Contract dependencies add
validate-to-entry edges. Product capability and resolved target requirement
gates precede the consumer's migration entry steps. Structured apply adapters
use `migration:<migrationId>:apply`; generic `component.migrate` remains only
for runtime declarations.

The validation response and immutable target-plan JSON expose the ordered full
contracts. Published plans project them into the existing
`DeploymentPlanMigration` relation with exact apply/validate step keys.

## Data and compatibility

- Database changes: none. Migration 147 is reused; migration 169 is not added.
- API changes: additive `migrationContracts` on Product Release components and
  target-plan validation.
- Canonical changes: additive fields use `omitempty`; releases and plans with
  no structured migrations preserve their prior shape.
- Agent protocol: existing typed migration actions are reused. No executor
  bypass, restore shortcut, or credential transport is added.
- Scope: community-neutral; no adopter, environment, CI vendor, registry, or
  database-provider special case.

## Verification

Focused Go tests exercise canonical stability and drift, validation blockers,
graph expansion and ordering, adapter binding, API mapping, complete relational
projection, exact resulting schema state, and legacy omission. Live
PostgreSQL, deployment, and production verification remain separate release
gates.
