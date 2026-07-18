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
