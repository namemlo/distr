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
- Added the review-fix `internal/observation.CampaignResolver` structural seam,
  resolving organization plus canonical `ComponentInstance.id` plus frozen
  checksum to an exact trusted observation ID/checksum for subsequent verifier
  fencing. Migration 159 now tenant-fences campaign prerequisite evidence to
  that observation with a composite foreign key.
- Added observer registration/observation and drift/reconciliation API types,
  mappings, handlers, tenant-safe errors, feature/RBAC gates, and routes.
  Observer tokens use the separate `Observer` authorization scheme and only
  SHA-256 fingerprints are stored.
- Added ADR-0065, PR-077 fork documentation, and the fork diff index entry.
- Hardened gate evaluation so evidence must be accepted, captured after
  admission and by the deadline, and still within its persisted freshness
  window. Promotion now re-reads all eligible observers and the execution-bound
  executor report inside its serializable transaction.
- Added a campaign-compatible stable runtime checksum that excludes mutable
  observer/evidence metadata while preserving the full envelope checksum for
  replay fencing. Campaign resolution now also requires complete, healthy,
  current, trusted evidence with a non-empty evidence reference inside its
  persisted freshness window.
- Bound pending desired revisions to a concrete execution-protocol-v2 attempt
  and its task, plan, target, organization, and execution lineage. Terminal
  observation gates fence unresolved attempts, release execution fences, and
  release task leases/resource locks only after all sibling component gates
  are terminal.
- Added the always-registered, indexed, multi-replica-safe deadline sweep using
  bounded batches and `FOR UPDATE SKIP LOCKED`.
- Fenced retained same-sequence conflict evidence without changing exact replay
  idempotence, and automatically opens drift or executor/observer mismatch
  cases from newly accepted and terminal evidence.
- Drift freshness is now classified directly from `FreshUntil`, with an exact
  boundary remaining fresh.
- Hardened migration rollback refusal by locking all evidence tables before
  checking them and including standalone observer registrations.
- Bound every non-timeout terminal result to trusted observation identity,
  exact replay to the full envelope and component scope, component instances to
  their Deployment Units, executor reports to pending executions, and drift
  cases to one active/observed placement.
- Wired observation ingestion and executor-report recording into production
  reconciliation. `CREATE_PLAN` now assigns work; restore/close resolves only
  after current trusted evidence proves the active desired material.
- Added retention-safe dependency deletion under the existing authorized
  organization-retention transaction setting and hardened evidence triggers
  against identity/timestamp mutation.

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
9. Missing frozen-prerequisite resolver and campaign-evidence tenant foreign
   key coverage.
10. Gate tests proving stale or pre-admission evidence was incorrectly
    eligible, while eligible pre-deadline evidence needed to survive a later
    post-deadline sample.
11. Promotion tests proving caller hints could omit changed repository state
    and independent-observer conflicts.
12. Terminal-state tests proving non-timeout outcomes could be recorded
    without observation identity.
13. Replay tests proving sequence/evidence-only equality did not cover the
    full persisted material or component scope.
14. Repository boundary tests for component/Deployment Unit lineage,
    execution-bound reports, per-component sequence reuse, retention ordering,
    drift placement, and evidence-proven reconciliation.
15. Stable runtime-checksum compatibility with the frozen PR-071 checksum,
    conflict fencing, `FreshUntil` boundaries, execution-v2 lineage, deadline
    sweep/lock release, automatic drift cases, and rollback TOCTOU coverage.

Each red failure was followed by the minimal implementation and a focused
green rerun.

## Verification

- Focused non-race domain/repository/API/mapping/handler/routing suite: pass.
- `go vet` over all changed Go package surfaces: pass.
- `golangci-lint run --new-from-rev=HEAD`: pass with `0 issues` (the runner
  emitted one non-fatal generated-file-filter warning for a removed temporary
  copy of an unrelated blue-green test).
- `git diff --check`: pass.
- Migration 159 contract/pair tests: pass.
- Five behavioral PostgreSQL repository tests compile and cover lifecycle
  promotion, observer conflict re-evaluation, placement/execution/replay
  boundaries, retention deletion, and drift reconciliation. They skip locally
  because `DISTR_TEST_DATABASE_URL` is not set.
- Required race command: blocked before package compilation because the
  repository sets `CGO_ENABLED=0`; overriding it reaches `cgo: C compiler
  "gcc" not found`.
- `mise run lint:migrations`: blocked by untrusted local `mise.toml`. Running
  the same validator directly with Git Bash correctly recognized migration
  159 but failed global continuity because this synthetic base intentionally
  lacks predecessor migrations 140-142 and 146-158.
- Full serial `go test -p 1 ./... -count=1`: exceeded the five-minute command
  bound without emitting a test failure. The focused PR-077 packages pass; this
  is not claimed as a full repository pass.
- Live PostgreSQL migration/repository verification was not run because
  `DISTR_TEST_DATABASE_URL` is absent. No external/client database was used.

## Commit

Feature commit subject: `feat: verify independent observed state`.
Review-hardening commit subject: `fix: harden observed state verification`.

The immutable commit SHA is reported in the agent handoff after the commit is
created; a commit cannot include its own final SHA in its contents.

## Integration Seams and Concerns

- Ordered integration must include the canonical campaign lineage changes and wire
  `observation.CampaignVerifier{Store: db.CampaignObservationRepository{}}`
  into the scheduler's `campaigns.CampaignObservationVerifier` dependency.
  No synthetic-base import of the future package was added.
- Wire `observation.CampaignResolver{Store:
  db.CampaignObservationRepository{}}` into the resolver seam and fence its
  returned ID/checksum with `CampaignVerifier`. The resolver requires canonical
  `ComponentInstance.id`; it does not accept a plan-local target-component
  projection.
- Migration 159 must be applied only after migrations 146-158 are integrated.
  The global migration linter is expected to fail on this isolated synthetic
  branch and must pass after ordered integration.
- Migration 159 and repository SQL have contract/source plus compiled
  behavioral coverage but still require live PostgreSQL 16/18 upgrade,
  rollback-refusal, concurrency, replay/conflict, tenant-isolation, retention,
  and handler integration execution.
- The race suite requires a Windows C compiler or a Linux CI runner with CGO
  enabled.
- Existing `TargetComponentState` and `TargetComponentObservation` remain
  unchanged legacy/executor projections. No v1 state or checksum is rewritten.
