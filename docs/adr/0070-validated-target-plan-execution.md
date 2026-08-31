# ADR-0070: Enable Validated Target Deployment Plan Execution

## Status

Accepted

## Context

Migration 145 deliberately forced every target deployment plan v2 row to remain sealed and `BLOCKED` with a
`target_plan_execution_deferred` issue until the fenced executor, admission, approval, desired-state, independent
observation, reconciliation, and audit stack existed. Those generic control-plane capabilities are now present, but
the original database guard still prevents a fully validated plan from entering the existing approval and task
workflow.

## Decision

- Migration 163 replaces only the v2 plan publication guards. Existing `BLOCKED` plans and their issues remain
  immutable and are never promoted in place.
- A newly published plan may seal as `READY` only after draft validation returns no issues, its target and step graph
  are complete and bounded, no blocker issue exists, and the publication audit event is present.
- After sealing, every plan field remains immutable. The only allowed plan transition is the existing status-only
  `READY` to `EXECUTED` transition performed atomically when tasks are created.
- The deferred commit guard accepts only sealed `READY` or `EXECUTED` target plans. Migration rollback refuses while
  any executable v2 plan exists, because restoring the migration-145 invariant would make retained history invalid.
- Protocol v1 behavior, executor payloads, approval checks, task locks, and adopter integrations are unchanged.

## Consequences

Validated target plans can use the already implemented approval, admission, task, and fenced execution path without
rewriting historical plans. Migration 164 additionally freezes native desired/observed lineage on planning evidence
and the exact dependency-policy checksum on Product Releases. Operators must include migrations 163 and 164 in clean-install, upgrade, restart, and safe-down
evidence. A failed validation or any blocker still prevents publication rather than producing an executable plan.

## Alternatives Considered

- Updating existing blocked plans was rejected because it would rewrite retained approval and audit lineage.
- Removing the database guards was rejected because application validation alone is not a sufficient integrity
  boundary for executable plans.
- Keeping all plans blocked was rejected because the fenced executor and admission prerequisites now exist and the
  remaining invariant prevents their use.

## Validation

- Focused database, planning, desired-state, execution-protocol, and migration tests must pass.
- PostgreSQL clean-install, 162-to-164 upgrade, restart, and down/refusal evidence remains a release gate.
- Rollout must verify that existing blocked rows are byte-for-byte unchanged and a new validated plan reaches
  `READY`, approval, task creation, and status-only `EXECUTED` transition.
