# PR-077 Report - Desired State, Independent Observation, and Drift

## Status

Implemented on `codex/pr-077-observation` from initial checkpoint
`ea6567b6d9b6679c960dde633f42ac7066240bde`. The change is local only. No push,
merge, deployment, external service, client database, or runtime mutation was
performed.

## Changes

- Added migration 159 with pending and active desired revisions, desired and
  observation heads, executor reports, observer registrations, append-only
  independent observed state, drift cases/events, and reconciliation actions.
- Added desired-state admission and promotion rules. Pending admission leaves
  active state untouched; only an exact independently verified observation
  advances one component. Terminal failure, cancellation, partial/unknown
  evidence, conflict, and timeout preserve the prior active revision.
- Added observer trust, scope, freshness, clock-skew, monotonic sequence,
  idempotent replay, out-of-order retention, conflict quarantine, and
  capture-deadline enforcement.
- Added exact artifact/config/schema/capability/platform/topology/health gates,
  including executor-success/runtime-wrong handling and explicit lock release
  plus quarantine on timeout/conflict.
- Added drift classification and time-bounded accepted deviations without
  rewriting desired history.
- Added `internal/observation.CampaignVerifier`, which structurally implements
  the exact PR-072 `campaigns.CampaignObservationVerifier` method:
  `VerifyCampaignObservation(context.Context, organizationID, observationID,
  checksum) error`. It fails closed when unwired and delegates to an exact
  current/trusted/accepted/complete observation lookup when configured with
  `db.CampaignObservationRepository{}`.
- Added observer registration/observation and drift/reconciliation API types,
  mappings, handlers, tenant-safe errors, feature/RBAC gates, and routes.
  Observer tokens use the separate `Observer` authorization scheme and only
  SHA-256 fingerprints are stored.
- Added ADR-0065, PR-077 fork documentation, and the fork diff index entry.

## TDD Evidence

Observed RED failures before implementation included:

1. Missing desired/observation/reconciliation types and functions across the
   initial domain tests.
2. Missing API request validation, mappings, repository, migration, handler,
   and route contracts.
3. Missing `CampaignVerifier`.
4. A regression test proving that different observer envelope checksums
   incorrectly produced conflict even when independently measured runtime
   state agreed.
5. A regression test proving that a caller-supplied verified gate could not
   yet be revalidated against the exact stored observation.
6. A regression test proving that evidence captured after the pending
   observation deadline was incorrectly accepted.
7. Missing executor-report persistence and a regression proving that terminal
   executor failure/cancellation/unknown outcomes could incorrectly verify.
8. A regression proving that observation cancellation was not preserved as a
   terminal desired-state outcome.

Each red failure was followed by the minimal implementation and a focused
green rerun.

## Verification

- Focused non-race domain/repository/API/mapping/handler/routing suite: pass.
- `go vet` over all changed Go package surfaces: pass.
- `golangci-lint run --new-from-rev=HEAD` over all changed Go package
  surfaces: pass with `0 issues`.
- `git diff --check`: pass.
- Migration 159 contract/pair tests: pass.
- Required race command: blocked before package compilation because the
  repository sets `CGO_ENABLED=0`; overriding it reaches `cgo: C compiler
  "gcc" not found`.
- `mise run lint:migrations`: blocked by untrusted local `mise.toml`. Running
  the same validator directly with Git Bash correctly recognized migration
  159 but failed global continuity because this synthetic base intentionally
  lacks predecessor migrations 140-142 and 146-158.
- Full `go test ./... -run '^$' -count=1`: attempted, but Windows exhausted
  virtual memory while compiling the repository in parallel. The PR-077
  packages compiled successfully in that run; this is not claimed as a full
  repository pass.
- Live PostgreSQL migration/repository/handler verification was not run because
  no client/external database operation is authorized and the ordered
  predecessor migration stack is absent.

## Commit

Feature commit subject: `feat: verify independent observed state`.

The immutable commit SHA is reported in the agent handoff after the commit is
created; a commit cannot include its own final SHA in its contents.

## Integration Seams and Concerns

- Ordered integration must include PR-072 commit `8b88db20` and wire
  `observation.CampaignVerifier{Store: db.CampaignObservationRepository{}}`
  into the scheduler's `campaigns.CampaignObservationVerifier` dependency.
  No synthetic-base import of the future package was added.
- Migration 159 must be applied only after migrations 146-158 are integrated.
  The global migration linter is expected to fail on this isolated synthetic
  branch and must pass after ordered integration.
- Migration 159 and repository SQL have contract/source coverage but still
  require live PostgreSQL 16/18 upgrade, rollback-refusal, concurrency,
  replay/conflict, tenant-isolation, and handler integration proof.
- The race suite requires a Windows C compiler or a Linux CI runner with CGO
  enabled.
- Existing `TargetComponentState` and `TargetComponentObservation` remain
  unchanged legacy/executor projections. No v1 state or checksum is rewritten.
