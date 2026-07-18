# PR-065: Structured Migration Plans

## Outcome

PR-065 turns database change declarations into typed, immutable planning and
recovery facts. A plan can require and verify a backup before any mutation,
freeze source/result schema versions and probes, serialize target/database
locks, and retain the exact retry input checksum and recovery evidence.

The existing v1 deployment-plan request and canonical payload remain
compatible. Protocol v2 remains review-only until the fenced executor work is
delivered.

## Generic user story

As an operator deploying a multi-component product, I want every database
change to declare its source and resulting schema, operational lock, backup,
probes, retry safety, compatibility, and recovery procedure so that the plan
can fail closed before mutation and recovery can be reviewed as a new,
append-only plan.

## Contract and graph

`MigrationContract` records:

- a stable migration ID and SHA-256 contract checksum;
- component and database-resource identity;
- source/result schema versions and phase;
- dependencies, lock type/timeout, and estimated impact;
- required backup verifier;
- bounded checksum-bound precondition and postcondition probes;
- retry class and stable idempotency key;
- reversibility, previous-application compatibility, recovery procedure, and
  forward-fix requirement; and
- evidence-retention duration and adapter/artifact references.

`ExpandMigrationGraph` adds this fail-closed order:

```text
backup.create
  -> backup.verify
  -> migration.validate (precondition)
  -> migration.apply
  -> migration.validate (postcondition)
  -> dependent component mutation
```

The expansion rejects an unordered conflict on the same database lock.
Repeating the same retry-safe expansion is idempotent only when the frozen
apply input checksum is unchanged. Restore actions are never inserted into a
normal deployment graph.

## Recovery

`BuildRecoveryPlan` always creates a new v2 draft that supersedes the failed
plan and stores the canonical recovery graph in its preview payload.

- Reversible completed migrations are reversed in reverse dependency order.
- Forward-only migrations reject automatic reverse and require forward-fix.
- Manual and forward-fix requests contain no automatic mutation shortcut.
- Restore requires a separately approved recovery request with a frozen
  backup ID/checksum, database resource, data-loss boundary, procedure
  version, approver groups, operator scope, validation probes, and timeout.
- Restore execution is followed by restore verification.
- Recovery evidence retention is mandatory.

## Typed actions

The built-in registry adds:

```text
database.backup.create
database.backup.verify
database.migration.apply
database.migration.validate
database.migration.reverse
database.restore.execute
database.restore.verify
```

Inputs are bounded JSON schemas. Credentials are secret references rather than
values. Executable migration artifacts are immutable digests. Outputs contain
bounded references/checksums and explicitly redacted evidence. The restore
schema requires separate recovery-plan and approval identities, preventing a
normal-plan shortcut. The existing action-definitions API additively returns
these schemas; no new mutation route is introduced.

## Data model

Migration 147:

- adds `step_input_checksum`, `retry_class`, `cancellation_behavior`,
  `observation_requirement`, `target_lock_key`, and `database_lock_key` to
  `DeploymentPlanStep`; and
- creates append-only `DeploymentPlanMigration` records with tenant-scoped
  plan/step foreign keys, frozen contract facts, bounded probe JSON, recovery
  classification, and evidence retention.

Rollback acquires `ACCESS EXCLUSIVE` locks and refuses while structured
migration or step execution evidence exists.

## Preflight

Migration preflight fails independently on:

- missing or unverified required backup evidence;
- source schema mismatch;
- unavailable target lock;
- unavailable database lock;
- unavailable typed adapter; or
- failed precondition probes.

A failed backup check therefore prevents every downstream mutation node.

## Compatibility and security

- Empty additive step metadata uses database defaults and `omitempty`, so
  legacy canonical plan bytes do not gain new fields.
- Existing v1 API, UI, and agent behavior is unchanged.
- Protocol v2 database actions are planning contracts only in this PR.
- No raw database credentials, probe query text, or unbounded action evidence
  is accepted.
- Database restore is manual, separately approved, and never an automatic
  rollback side effect.

## Stale-base replay seams

This synthetic branch starts at PR-063 and intentionally does not contain
PR-064 migration 146 or its exact change-set implementation.

When replaying PR-065 after PR-064:

1. keep migration 146 before migration 147;
2. reconcile the identical `PlannedState` base fields in
   `internal/types/deployment_plan.go` and retain any PR-064 extensions;
3. preserve PR-064 canonical baseline/change/risk fields while keeping the
   additive migration fields;
4. keep the v2 canonical graph as the source for persisted step execution
   metadata; and
5. rerun migration lint and the combined PR-064/065 focused gate.

## Verification scope

Focused tests cover contract validation, backup-before-mutation ordering,
stable retry/checksum behavior, database-lock conflicts, bounded probes,
reverse dependency recovery, forward-only/manual recovery, separately
approved restore, evidence retention, action schemas, migration preflight,
step persistence, migration SQL shape, and legacy canonical omission.
