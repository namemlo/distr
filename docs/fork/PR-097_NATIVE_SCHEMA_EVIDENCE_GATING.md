# PR-097: Native Schema Evidence Gating

## Purpose

Require current, checksummed schema reports and migration decisions before an
immutable Target Deployment Plan can be published, admitted, or executed.

## Generic user story

As a release operator, I need each database-bound component to prove its exact
current schema state and application/schema compatibility so that a stale or
incomplete migration assumption cannot create admission evidence, acquire an
execution lock, or mutate task state.

## Evidence contract

- Target Config Snapshots carry immutable `adapter_input` objects using
  `application/vnd.distr.schema-report.v1+json` and
  `application/vnd.distr.migration-evidence.v1+json`.
- The Hub verifies each raw object's reference, optional version ID, media
  type, size, and SHA-256 checksum before strict bounded decoding.
- Parsed documents carry linked internal checksums and exact organization,
  placement, target, config snapshot, Component Release, database boundary,
  schema state, issue, and expiry facts.
- `COMPATIBLE_NO_MIGRATION_REQUIRED` proves that no structured migration is
  needed. `MIGRATION_BOUND` binds every ordered contract and source/result
  schema transition.
- Both decisions require the exact prior/target application by
  current/intermediate/result schema compatibility matrix.

## Planning, admission, and execution

- Draft validation derives evidence requirements from database-bound component
  instances and structured migration contracts.
- Missing, ambiguous, wrong-scope, wrong-release, expired, checksum-invalid,
  stale-current, contract-incomplete, or matrix-incomplete evidence produces a
  stable `schema_evidence_*` blocker before a preview can be published.
- Canonical plan bytes freeze the requirements, object identities, parsed
  reports, migration decisions, contract bindings, and mixed-version facts.
- Publication revalidates the canonical evidence before plan insertion.
- Admission revalidates before authorization or admission-evidence mutation.
- Protocol-v2 task creation revalidates before task lookup, advisory locks,
  preflight persistence, or task/step/lock mutation.

## API, data, and compatibility

- Draft-validation responses add `schemaEvidenceRequirements` and
  `schemaEvidence`; published-plan responses add `schemaEvidence`.
- Database changes: none. Evidence is frozen in the existing canonical plan
  payload, so schema target 169 is unchanged.
- UI changes: none.
- Agent/executor protocol changes: none.
- Feature flags: uses the existing default-off `operator_control_plane_v2`
  planning and execution boundary; no new flag is added.
- Existing v2 plans without requirements or structured migration contracts
  remain compatible. Historical structured-migration plans without frozen
  evidence fail closed for new admission or execution.

## Security and scope

The implementation is community-neutral and accepts only the two typed,
bounded JSON evidence documents. Errors do not expose object bodies or foreign
scope details. No live system, client workload database, observer runtime, or
executor is contacted, and no persistence migration, runtime mutation, or
cleanup is performed.

## Verification

Focused Go tests cover strict parsing, checksums, decision semantics, target and
release scope, expected-current state, expiry, ordered migration binding,
mixed-version completeness, bounded S3 reads, canonical ordering, API mapping,
frozen-plan revalidation, and pre-mutation gate ordering. Full Go, migration,
formatting, and diff checks provide the repository-level gate.
