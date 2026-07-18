# PR-064 Report - Exact Baseline, Change Set, and Previous State

## Status

Implemented locally on `codex/pr-064-exact-plan-changes`. No rebase, push,
merge, deployment, external-system mutation, or client-system change was
performed.

## Baseline and Commit

- Initial HEAD: `ea6567b6d9b6679c960dde633f42ac7066240bde`
  (`feat: resolve immutable target deployment plans`).
- Initial worktree: 27 dirty/untracked paths (15 modified, 12 untracked).
- Local conventional commit: `feat: calculate exact target deployment changes`.
- The initial untracked ADR
  `docs/adr/0060a-exact-plan-baselines-and-previous-state.md` was deliberately
  removed because the consolidated plan allocates only ADR-0060 across this
  boundary. Its accepted context, decision, and consequences were preserved in
  `docs/fork/PR-064_EXACT_PLAN_CHANGESET.md`. All other initial dirty paths were
  preserved.

## Changed Files

- Planning and tests:
  `internal/planning/baseline.go`,
  `internal/planning/baseline_test.go`,
  `internal/planning/changeset.go`,
  `internal/planning/changeset_test.go`,
  `internal/planning/risk.go`,
  `internal/planning/risk_test.go`, and
  `internal/planning/graph.go`.
- Persistence, migration, and tests:
  `internal/migrations/sql/146_deployment_plan_baseline_changes.up.sql`,
  `internal/migrations/sql/146_deployment_plan_baseline_changes.down.sql`,
  `internal/db/deployment_plan_changes.go`,
  `internal/db/deployment_plan_changes_test.go`,
  `internal/db/deployment_plan_drafts.go`, and
  `internal/db/deployment_plans.go`.
- Types, API, mapping, handlers, and tests:
  `internal/types/deployment_plan.go`,
  `internal/types/plan_v2.go`,
  `api/deployment_plan.go`,
  `api/deployment_plan_draft.go`,
  `api/deployment_plan_test.go`,
  `internal/mapping/deployment_plan.go`,
  `internal/mapping/deployment_plan_draft.go`,
  `internal/mapping/deployment_plan_test.go`,
  `internal/handlers/deployment_plans.go`, and
  `internal/handlers/deployment_plans_test.go`.
- Frontend contract:
  `frontend/ui/src/app/types/deployment-plan.ts`.
- Documentation:
  `docs/fork/PR-064_EXACT_PLAN_CHANGESET.md` and
  `docs/fork/FORK_DIFF_INDEX.md`.
- SDD evidence: `.superpowers/sdd/pr064-report.md`.

## PR-063 Overlap Files

These nine files are PR-064 deltas on PR-063-owned foundation surfaces. Replay
the PR-064 changes after the final PR-063 commit rather than treating the
checkpoint versions as authoritative:

1. `api/deployment_plan.go`
2. `api/deployment_plan_draft.go`
3. `internal/db/deployment_plan_drafts.go`
4. `internal/db/deployment_plans.go`
5. `internal/mapping/deployment_plan.go`
6. `internal/mapping/deployment_plan_draft.go`
7. `internal/planning/graph.go`
8. `internal/types/deployment_plan.go`
9. `internal/types/plan_v2.go`

## Behavior

- Selects the newest independent healthy observation that exactly matches the
  active desired revision/checksum; freezes legacy, verified-v2, or bootstrap
  projection evidence.
- Pins platform in the observation checksum and keeps legacy evidence
  non-authoritative for protocol-v2 execution.
- Produces deterministic image, config, provider, schema, topology, source-note,
  baseline-authority, bootstrap, previous-state, and planning-limit changes.
- Classifies risks in canonical order independent of caller ordering.
- Persists tenant-fenced, actor-attributed, append-only baseline/change/risk
  evidence through migration 146.
- Exposes additive baseline, change, risk, bootstrap, and previous-state fields
  without removing v1 response fields.
- Creates a new B-to-A immutable plan under serializable transaction, stale-CAS,
  exact-placement, independent-observation, forward-only, and lineage checks.

## RED-GREEN Evidence

1. Stable risk order:
   `go test ./internal/planning -run TestClassifyDeploymentRiskHasStableOrderForEquivalentChanges -count=1`
   initially failed because reversed input produced a different risk order and
   different canonical checksums. It passed after sorting a cloned change list
   by component key, change kind, and component instance.
2. Platform-bound observation checksum:
   `go test ./internal/planning -run TestSelectVerifiedBaselineObservationChecksumPinsPlatform -count=1`
   initially failed because amd64 and arm64 evidence produced the same checksum.
   It passed after adding platform to the canonical observation evidence.

## Verification

- `go test ./internal/planning ./internal/db ./api ./internal/mapping ./internal/handlers -run 'Baseline|ChangeSet|PreviousState|Risk' -count=1`
  - pass.
- `go test ./internal/planning ./internal/db ./api ./internal/mapping ./internal/handlers -count=1`
  - pass.
- `go test ./internal/db -run 'TestMigration146|TestDeploymentPlanChangeRepository' -count=1`
  - pass.
- `go vet ./internal/planning ./internal/db ./api ./internal/mapping ./internal/handlers`
  - pass.
- `pnpm install --frozen-lockfile` - pass after sequentially repairing the local
  dependency tree.
- `pnpm exec tsc --ignoreConfig --noEmit --target ES2022 --module ESNext --moduleResolution bundler --strict --skipLibCheck frontend/ui/src/app/types/deployment-plan.ts`
  - pass.
- `pnpm exec prettier --check frontend/ui/src/app/types/deployment-plan.ts docs/fork/PR-064_EXACT_PLAN_CHANGESET.md docs/fork/FORK_DIFF_INDEX.md`
  - pass.
- `git diff --check` - pass.
- ADR reference scan - pass; no `ADR-0060A` or removed ADR path remains.

## Deferred Gates and Concerns

- `mise run lint:migrations` cannot start on this Windows checkout because
  `mise.toml` sources Bash through `/bin/bash`. Running the exact validation
  script with Git Bash reaches the migration check but fails on missing
  predecessor migrations 140, 141, and 142. Migration 146 itself is paired and
  its static migration contract tests pass. Final migration lint must run after
  PR-057 through PR-059 are integrated.
- The project-wide frontend TypeScript command reaches compilation but fails on
  pre-existing release-contract union accesses in
  `deployment-plans.component.ts` and missing generated
  `agent-changelog.json`/`version.json`. The changed PR-064 TypeScript contract
  passes an isolated strict compile and formatting check.
- No live PostgreSQL 16/18 migration, transaction race, cross-tenant repository,
  or routed-handler integration test was run in this speculative worktree.
- Final integration must replay the nine overlap-file deltas after the final
  PR-063 and rerun migration lint, full frontend compilation, and live database
  tests.
