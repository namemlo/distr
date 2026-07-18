# PR-063 Completion Report

## Initial State

- Initial HEAD: `ea6567b6d9b6679c960dde633f42ac7066240bde`
- Branch: `codex/pr-063-target-plan-v2`
- Preserved input: all 20 pre-existing dirty/untracked paths were retained and reviewed in place.
- Scope: PR-063 control-plane files only. No rebase, push, merge, deployment, external access, client workload, or non-Distr mutation was performed.

## Changes

- Replaced copied target-config checksums with an injected object-verification boundary that records independently observed reference, version, media type, size, and checksum. An unavailable verifier fails closed.
- Corrected required-platform handling so the selected target platform must be a member of a multi-platform Product Release instead of matching every platform.
- Added deterministic plan bounds for components, requirements, steps, edges, and canonical payload size.
- Gated a consumer's first executable step, including its first migration, on provider health and target-requirement verification.
- Preserved the protocol-v1 compatibility projection while keeping every target-plan-v2 publication sealed, immutable, `BLOCKED`, and unreachable from task creation until PR-075.
- Added publisher and draft actor attribution, append-only draft audit events, transaction serialization, draft row locking, immutable child sealing, and downgrade refusal.
- Scoped supersession to one sealed current lineage tip with the same organization, deployment unit, environment, application, and deployment target.
- Batched release/provenance/provider loading and froze selected-platform provenance facts, exact expected state, subscriber membership, component instances, and deterministic checksums.
- Kept the existing v1 deployment-plan route, schema defaults, task creation, mapping, and API fields additive and compatible.

## TDD and Review

- The worktree arrived with the hardening tests and implementation already dirty. The first fresh focused run in this completion turn was green, so no new red result is claimed.
- Self-review covered tenant predicates and composite foreign keys, mutation authorization, deterministic ordering, query batching, immutable publication, supersession races, graph prerequisite reachability, v1 compatibility, task-creation denial, payload/resource bounds, and public error redaction.
- No additional in-scope defect remained after the review. The PR-058 adapter remains an explicit integration prerequisite rather than substituting database checksums for object-provider evidence.

## Verification

Passed:

```text
go test ./internal/planning ./internal/db ./api ./internal/mapping ./internal/handlers -run 'PlanDraft|ResolveTarget|TargetPlanGraph' -count=1
go test ./internal/planning ./internal/db ./api ./internal/mapping ./internal/handlers -count=1
go vet ./internal/planning ./internal/db ./api ./internal/mapping ./internal/handlers
gofmt -d <all changed Go files>
git diff --check
```

Migration lint was executed through Git Bash and stopped only on the known missing prerequisite migrations:

```text
Validating migrations 0 through 145
Version 140: 0 up/down files
Version 141: 0 up/down files
Version 142: 0 up/down files
```

Migration 145's focused structural test passed. Live PostgreSQL migration execution was not attempted on this isolated speculative branch.

## Commit

- Conventional commit: `feat: harden immutable target deployment plans`
- Final commit SHA is reported in the task handoff after commit creation.

## Concerns and Deferred Gates

- This isolated branch deliberately omits prerequisite migrations 140-142 and the PR-058/059/061 Go packages. Migration lint and live repository/publication tests must be rerun after the numbered stack is integrated.
- `DeploymentPlanDraftsRouterWithVerifier` is the PR-063 injection point. During stack integration it must receive the real PR-058 `targetconfig.ObjectVerifier` adapter backed by the configured object store. The default unavailable verifier intentionally blocks validation/publication; copied database checksums are not accepted as observations.
- Live PostgreSQL tests must prove the deferred sealing trigger, concurrent publication idempotency/conflict behavior, scoped supersession serialization, append-only retention exception, tenant fences, and migration rollback refusal.
- Full repository tests/builds, containers, browser/E2E checks, deployment, and adopter/client validation remain later integration gates.

## Reviewer Fix Follow-up

### Findings closed

- Empty `TargetConfigSnapshotObject` sets now fail closed before validation can produce a preview. No copied checksum or empty-set success path remains.
- Target-config object verification is capped at 100 objects, matching PR-058, and rejects oversized sets before the verifier is called. The database query also reads at most 101 rows so the overflow can be detected without loading an unbounded set.
- Migration 145's child-seal trigger now checks the old parent for every update/delete and the new parent for inserts or reparenting. Updating from an unsealed plan into a sealed v2 plan is rejected, as are updates away from a sealed v2 plan.
- Observed provider queries read at most 4,097 rows and fail above the 4,096-row limit. Included, disabled, and observed candidate expansion shares an 8,192-candidate accumulator bound, including the final cross-source merge.
- Existing tasks can no longer bypass target-plan-v2 execution denial. Legacy retries remain idempotent after a v1 plan has transitioned from `READY` to `EXECUTED`.

### RED evidence

The first compile-oriented red run identified the missing enforcement helpers:

```text
go test ./internal/db -run 'TargetConfigVerificationRejects|TargetPlanProviderBounds|ObservedProviderQuery|ExistingTasksCannotBypass|Migration145' -count=1

undefined: verifyTargetPlanConfigObjects
undefined: validateTargetPlanProviderRowCount
undefined: appendTargetPlanCandidate
undefined: reuseExistingDeploymentPlanTasks
```

After compatibility stubs preserved the old unsafe behavior, the executable regressions failed for the intended reasons:

```text
TestMigration145HasTenantFencesImmutabilityAndRollbackRefusal:
  missing NEW-parent reparenting guard
TestTargetConfigVerificationRejectsEmptyObjectSet:
  expected an error, got nil
TestTargetConfigVerificationRejectsOversizedSetBeforeVerifierCalls:
  expected an error, got nil
TestTargetPlanProviderBoundsRejectRowsAndCandidateCrossProduct:
  expected an error, got nil
TestObservedProviderQueryAppliesDatabaseRowLimit:
  missing LIMIT @providerRowLimit
TestExistingTasksCannotBypassTargetPlanV2Denial:
  expected an error, got nil
```

Self-review added one further red compatibility proof:

```text
TestExistingLegacyTasksRemainIdempotentAfterExecution:
  conflict: deployment plan must be READY before tasks can be created
```

### GREEN evidence

```text
go test ./internal/db -run 'TargetConfigVerificationRejects|TargetPlanProviderBounds|ObservedProviderQuery|ExistingTasksCannotBypass|ExistingLegacyTasksRemainIdempotent|Migration145' -count=1
ok github.com/distr-sh/distr/internal/db

go test ./internal/planning ./internal/db ./api ./internal/mapping ./internal/handlers -run 'PlanDraft|ResolveTarget|TargetPlanGraph|TargetConfigVerification|TargetPlanProvider|ObservedProviderQuery|ExistingTasksCannotBypass|ExistingLegacyTasksRemainIdempotent|Migration145' -count=1
ok github.com/distr-sh/distr/internal/planning
ok github.com/distr-sh/distr/internal/db
ok github.com/distr-sh/distr/api
ok github.com/distr-sh/distr/internal/mapping
ok github.com/distr-sh/distr/internal/handlers

go test ./internal/planning ./internal/db ./api ./internal/mapping ./internal/handlers -count=1
ok github.com/distr-sh/distr/internal/planning
ok github.com/distr-sh/distr/internal/db
ok github.com/distr-sh/distr/api
ok github.com/distr-sh/distr/internal/mapping
ok github.com/distr-sh/distr/internal/handlers

go vet ./internal/planning ./internal/db ./api ./internal/mapping ./internal/handlers
exit 0

git diff --check
exit 0
```

### Follow-up commit

- Conventional commit: `fix: close target plan review gaps`
- Final SHA is reported in the task handoff.
