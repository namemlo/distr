# PR-086: Guarded Restore and Protected-History Continuity

## Generic user story

As a self-hosted operator, I need to restore an earlier database, object store,
and immutable Hub image without overwriting the active or failed data, and I
need exact evidence that protected customer execution history is unchanged.

## Scope

This slice adds no migration, API route, UI surface, agent protocol, client
adapter, CI-provider dependency, or adopter-specific inventory. It extends the
server Docker Compose operator helper and the existing protected-history CLI.

## Operator behavior

`restore-plan` creates new labeled PostgreSQL and RustFS volumes, restores
checksum-bound backups, verifies the prior handoff/image/schema, and seals exact
protected-history and object-volume comparison evidence without touching the
active runtime. The sealed `distr.release-restore-snapshot/v1` manifest binds the
plan ID, prior handoff checksum, both backup checksums, protected-history
baseline checksum, target digest reference, and target release commit; its own
checksum is recorded in `restore-plan.env`.

`restore-apply` repeats those validations, proves the running Hub digest and
actual PostgreSQL/RustFS mounts match the configured source identity, and writes
a durable `PREPARED` journal before outage. After stopping the active containers and recording
`SOURCE_STOPPED`, it mounts the stopped source PostgreSQL volume in isolation and
compares live protected history with the sealed baseline. Only equality permits
the immutable image plus both volume names to be switched through one atomic
`.env` replacement. Readiness, image revision, schema, object aggregate, and
protected history must then pass before `TARGET_VERIFIED` and `COMMITTED`.
Failed candidate volumes and old active volumes remain available until a later
separately reviewed retirement.

`restore-recover` validates the journal checksum and exact sealed identities. It
closes a verified pre-stop `PREPARED` state, restores and verifies the source for
`SOURCE_STOPPED`, `IDENTITY_SWITCHED`, or `RECOVERY_REQUIRED`, and finalizes a
`TARGET_VERIFIED` switch only when the matching checksummed applied-state record
exists. `COMMITTED` and `RECOVERED` are idempotent; mismatches fail closed.

## Protected-history contract

- `protected-history fingerprint` validates and prints the artifact identity,
  record root, and record count.
- `protected-history compare --require-exact` requires identical schema, scope,
  and records; additions are violations in this mode.
- Approved retirement requires `--approved-retirement-allowlist`,
  `--retirement-approval`, and `--sample-membership` together. The exact-ID
  allowlist is authorized by protected approval/job records and exact protected
  ownership/retirement-item records; no artifact self-authorizes.
- Schemas 138 through 165 use the complete schema-138 whole-row projection
  across release, deployment, task, execution, log, observation, and timestamp
  audit history. Schema 166 is explicit; 167 adds
  `ExecutionRuntimeEvidence`; 168 adds
  `DeploymentPlanResolvedRequirement`; and 169 adds
  `BaselineAdoptionComponent`. Unknown later schemas are refused.

## Compatibility and recovery

Existing `rollback` remains application-only and existing Compose volume names
remain unchanged when the optional volume variables are absent. The restore
commands refuse active timestamp fences, unsafe paths, mutable tags, mismatched
checksums, image-label drift, changed candidate volumes, dirty or unexpected
schema, protected-history differences, unresolved switch journals, and journal
checksum or identity drift. No volume is deleted by apply or recovery.

No live restore is claimed by repository tests. External S3 or managed database
recovery remains provider-owned and must implement an equivalent isolated
restore and atomic endpoint switch.

## Validation

Focused Go tests cover projection registration and three-artifact authorization.
See ADR-0073 and `hack/test-server-compose-restore.sh` for aggregate snapshot,
stopped-source fence, journal, recovery, retention, and command-arity coverage.
