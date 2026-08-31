# PR-085 - Native Baseline Adoption

## Purpose

Create legitimate native desired-state lineage for an already running healthy
placement without representing an unperformed deployment as successful
execution.

## Contract

- Adoption requires one sealed `READY` native-v2 bootstrap plan and exact
  plan, Product Release, target-config, component, artifact, provenance,
  platform, topology, observer, and observer-retained health evidence.
- The immutable outcome is `ADOPTED`, never `DEPLOYED` or executor success.
- Success records `deploymentPerformed=false`, zero tasks, zero task locks, and
  zero executions. No pending desired state or executor report is synthesized.
- Adopted active desired revisions use `BASELINE_ADOPTION` source lineage with
  nullable task/execution lineage; ordinary promotion remains `EXECUTION`.
- Exact idempotent replay, including concurrent unique/serialization races,
  re-reads and returns the committed outcome. Changed bytes under the same
  organization-scoped key conflict.
- Scoped `plan.execute`, deployment-unit scope, environment enrollment, feature
  flags, tenant isolation, and super-admin mutation blocking remain mandatory.
- `LEGACY_LIVENESS_ONLY` evidence is visibly restricted to
  `BASELINE_OR_ROLLBACK_ONLY`. Health kind/use/policy come only from the
  authenticated immutable observation, not the adoption request. Its evidence
  reference is `evidence://sha256/<digest>` and exactly matches the observation
  evidence checksum; the artifact uses portable
  logical probe identities; transient observation transport addresses are not
  canonical evidence, and native provider discovery excludes it from promotion.
- Release application version and checksum are independent from observed schema
  version and capability checksum. PR-090/migration 169 makes that separation
  explicit in validation, database guards, and the adoption read model.

## Surface

Migration 166 adds `BaselineAdoption` and `BaselineAdoptionComponent`, and adds
source and health-policy lineage to `ActiveDesiredRevision`. The public API adds
`POST /api/v1/deployment-plans/{id}/baseline-adoptions`. No UI or agent protocol
surface changes in this slice. Task and external-execution exclusion also guards
later plan/tenant reassignment, not only insertion.

## Verification Boundary

Focused Go, migration-contract, migration-matrix, and strict OpenSpec validation
are required. No live client, server, workload database, registry, executor, or
runtime mutation is performed. Live PostgreSQL up/down/refusal behavior remains
part of the release matrix. Downgrade refuses retained adoption lineage or
observation health-policy evidence rather than silently dropping either.
