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
active runtime.

`restore-apply` repeats those validations, stops the active containers, switches
the immutable image plus both volume names through one atomic `.env`
replacement, then verifies readiness, image revision, schema, object aggregate,
and protected history. Failed candidate volumes and old active volumes remain
available until a later separately reviewed retirement.

## Protected-history contract

- `protected-history fingerprint` validates and prints the artifact identity,
  record root, and record count.
- `protected-history compare --require-exact` requires identical schema, scope,
  and records; additions are violations in this mode.
- `--approved-retirement-allowlist` accepts only exact missing record keys and
  hashes bound to the baseline plus an approved sample-retirement preview.
- Schema 138 through 165 use the stable client audit projection; schema 166 and
  later retain the expanded v2 projection.

## Compatibility and recovery

Existing `rollback` remains application-only and existing Compose volume names
remain unchanged when the optional volume variables are absent. The restore
commands refuse active timestamp fences, unsafe paths, mutable tags, mismatched
checksums, image-label drift, changed candidate volumes, dirty or unexpected
schema, and protected-history differences.

No live restore is claimed by repository tests. External S3 or managed database
recovery remains provider-owned and must implement an equivalent isolated
restore and atomic endpoint switch.

## Validation

See ADR-0073 and `hack/test-server-compose-restore.sh`.
