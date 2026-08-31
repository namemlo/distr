# PR-085 - Native Baseline Adoption

## Purpose

Create legitimate native desired-state lineage for an already running healthy
placement without representing an unperformed deployment as successful
execution.

## Contract

- Adoption requires one sealed `READY` native-v2 bootstrap plan and exact
  plan, Product Release, target-config, component, artifact, provenance,
  platform, topology, observer, and health evidence.
- The immutable outcome is `ADOPTED`, never `DEPLOYED` or executor success.
- Success records `deploymentPerformed=false`, zero tasks, zero task locks, and
  zero executions. No pending desired state or executor report is synthesized.
- Adopted active desired revisions use `BASELINE_ADOPTION` source lineage with
  nullable task/execution lineage; ordinary promotion remains `EXECUTION`.
- Exact idempotent replay returns the retained outcome. Changed bytes under the
  same organization-scoped key conflict.
- Scoped `plan.execute`, deployment-unit scope, environment enrollment, feature
  flags, tenant isolation, and super-admin mutation blocking remain mandatory.
- `LEGACY_LIVENESS_ONLY` evidence is visibly restricted to
  `BASELINE_OR_ROLLBACK_ONLY`. Its immutable evidence artifact uses portable
  logical probe identities; transient observation transport addresses are not
  canonical evidence, and native provider discovery excludes it from promotion.

## Surface

Migration 166 adds `BaselineAdoption` and `BaselineAdoptionComponent`, and adds
source and health-policy lineage to `ActiveDesiredRevision`. The public API adds
`POST /api/v1/deployment-plans/{id}/baseline-adoptions`. No UI or agent protocol
surface changes in this slice.

## Verification Boundary

Focused Go, migration-contract, migration-matrix, and strict OpenSpec validation
are required. No live client, server, workload database, registry, executor, or
runtime mutation is performed. Live PostgreSQL up/down/refusal behavior remains
part of the release matrix.
