# Control Plane Operator and Adopter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement PR-079 through PR-083, prove the complete community control plane on two neutral targets, add safe sample retirement, publish operator/cutover evidence, and then adopt the accepted release for Choice TP DEV as the first external client profile.

**Architecture:** Add paginated operator read models over immutable core records, an Angular control room with legacy-compatible routes, neutral executor/observer fixtures, performance/failure validation, and allowlist-only sample retirement. Keep the Choice TP inventory, CI mappings, adapters, campaign, and cleanup allowlist in an isolated adopter repository/worktree.

**Tech Stack:** Go/PostgreSQL read models, Angular 22, Vitest, Playwright, Node-based fixture/load runners, Docker Compose, OCI, Jenkins-compatible scripts, PowerShell, mise.

## Global Constraints

- PR-055 through PR-078 and both subsystem exit gates must be accepted first.
- Migration 160 is reserved for operator read-model indexes/projections; 161 is reserved for sample retirement/tombstones.
- All list APIs are server-paginated with default 50/maximum 100 and deterministic cursor/order.
- UI drawers cannot hide a blocking approval/preflight fact; every fact is also available on a detail route and API.
- Existing `/deployments` and `/deployments/:deploymentTargetId` links remain compatible. Static child routes must be matched before the legacy ID redirect.
- Role-based UI E2E uses generated fixture users/tokens only; no real credential enters code, screenshots, traces, or artifacts.
- Neutral proof must pass before Choice TP enrollment or mutation.
- The current `C:\Users\pc\Desktop\repository\emlo-env-settings` checkout is behind its remote branch and contains unrelated user modifications. Do not edit, reset, clean, stash, or commit that checkout. Create a separate clean worktree for adopter changes.
- Choice TP audit/release/plan/task/observation history is protected. Cleanup can target only exact hello-distr/tutorial/demo IDs proven by ownership and reverse-reference queries.
- Live Distr, Jenkins, ECR, server, and cleanup operations require the plan's explicit preflight/approval gates; never print supplied credentials.

---

## Task 1: PR-079 — Operator Read Models and Paginated APIs

**Files:**

- Create: `internal/migrations/sql/160_operator_control_plane_read_models.up.sql`
- Create: `internal/migrations/sql/160_operator_control_plane_read_models.down.sql`
- Create: `internal/types/operator_read_model.go`
- Create: `internal/operatorqueries/fleet.go`
- Create: `internal/operatorqueries/fleet_test.go`
- Create: `internal/operatorqueries/releases.go`
- Create: `internal/operatorqueries/plans.go`
- Create: `internal/operatorqueries/campaigns.go`
- Create: `internal/operatorqueries/executions.go`
- Create: `internal/operatorqueries/reconciliation.go`
- Create: `internal/operatorqueries/audit.go`
- Create: `internal/db/operator_queries.go`
- Create: `internal/db/operator_queries_test.go`
- Create: `api/operator_control_plane.go`
- Create: `api/operator_control_plane_test.go`
- Create: `internal/mapping/operator_control_plane.go`
- Create: `internal/handlers/operator_control_plane.go`
- Create: `internal/handlers/operator_control_plane_test.go`
- Modify: `internal/routing/routing.go`
- Create: `hack/control-plane-read-model-benchmark.mjs`
- Create: `hack/control-plane-scale-fixture.mjs`
- Create: `docs/adr/0066-operator-read-models-and-route-compatibility.md`
- Create: `docs/fork/PR-079_OPERATOR_CONTROL_PLANE_READ_MODELS.md`

Migration 160 adds covering/partial indexes and explicitly maintained projection tables only where measured query plans require them. It must not duplicate ownership identities or become a write source of truth.

```go
type PageRequest struct { Cursor string; Limit int }
type Page[T any] struct { Items []T; NextCursor string; Total *int64 }
type FleetRow struct { Customer, Environment, Target, Unit, Component, ActiveRelease, PendingRelease, ObservedState, Drift, LastExecution, Enrollment string }

func ListFleet(context.Context, types.FleetFilter, PageRequest) (Page[types.FleetRow], error)
func ListOperatorReleases(context.Context, types.ReleaseFilter, PageRequest) (Page[types.OperatorReleaseRow], error)
func GetOperatorPlan(context.Context, uuid.UUID, uuid.UUID) (*types.OperatorPlanDetail, error)
func ListOperatorCampaigns(context.Context, types.CampaignFilter, PageRequest) (Page[types.OperatorCampaignRow], error)
func ListOperatorExecutions(context.Context, types.ExecutionFilter, PageRequest) (Page[types.OperatorExecutionRow], error)
func ListOperatorReconciliation(context.Context, types.ReconciliationFilter, PageRequest) (Page[types.OperatorReconciliationRow], error)
func SearchOperatorAudit(context.Context, types.AuditFilter, PageRequest) (Page[types.OperatorAuditRow], error)
```

API root: `/api/v1/control-plane`; endpoints `/fleet`, `/releases`, `/plans`, `/campaigns`, `/executions`, `/reconciliation`, and `/audit` with details/compare/evidence subroutes.

- [ ] Add query tests for every filter, stable cursor, empty/partial/stale/unknown state, shared-unit blast radius, cross-org isolation, and maximum page enforcement.
- [ ] Implement the deterministic scale-fixture generator; seed 1,000 targets/649+ placements/100-component release/500-step wave and capture `EXPLAIN (ANALYZE, BUFFERS)` before indexes.
- [ ] Implement queries/indexes until warm p95/p99 fixture thresholds are met without unbounded response or N+1 queries.
- [ ] Add scoped API handlers and mapping; plan detail exposes all approval/preflight blockers, checksums, graph, changes, and evidence links.
- [ ] Verify and commit.

```powershell
go test ./internal/operatorqueries ./internal/db ./api ./internal/mapping ./internal/handlers -run 'Operator|Fleet|ReadModel|Pagination' -count=1
node hack/control-plane-scale-fixture.mjs --targets 1000 --placements 649 --agents 100 --components 100 --steps 500 --out work/control-plane-scale.json
node hack/control-plane-read-model-benchmark.mjs --fixture work/control-plane-scale.json --runs 20
mise run lint:migrations
git add internal/migrations/sql/160_* internal/types/operator_read_model.go internal/operatorqueries internal/db/operator_queries* api/operator_control_plane* internal/mapping/operator_control_plane.go internal/handlers/operator_control_plane* internal/routing/routing.go docs hack/control-plane-read-model-benchmark.mjs hack/control-plane-scale-fixture.mjs
git commit -m "feat: add paginated operator read models"
```

Expected benchmark: fleet/list/detail page size 100 warm p95 ≤2 s and p99 ≤5 s; no cross-organization row.

## Task 2: PR-080 — Angular Operator Control Room and Role E2E

**Files:**

- Modify: `package.json`
- Modify: `pnpm-lock.yaml`
- Create: `playwright.control-plane.config.ts`
- Modify: `frontend/ui/src/app/app-logged-in.routes.ts`
- Create: `frontend/ui/src/app/app-logged-in.routes.spec.ts`
- Modify: `frontend/ui/src/app/components/side-bar/side-bar.component.ts`
- Modify: `frontend/ui/src/app/components/side-bar/side-bar.component.html`
- Create: `frontend/ui/src/app/types/operator-control-plane.ts`
- Create: `frontend/ui/src/app/services/operator-control-plane.service.ts`
- Create: `frontend/ui/src/app/services/operator-control-plane.service.spec.ts`
- Create: `frontend/ui/src/app/control-plane/fleet/fleet.component.ts`
- Create: `frontend/ui/src/app/control-plane/fleet/fleet.component.html`
- Create: `frontend/ui/src/app/control-plane/fleet/fleet.component.spec.ts`
- Create: `frontend/ui/src/app/control-plane/releases/releases.component.ts`
- Create: `frontend/ui/src/app/control-plane/releases/releases.component.html`
- Create: `frontend/ui/src/app/control-plane/releases/releases.component.spec.ts`
- Create: `frontend/ui/src/app/control-plane/plans/plan-list.component.ts`
- Create: `frontend/ui/src/app/control-plane/plans/plan-list.component.html`
- Create: `frontend/ui/src/app/control-plane/plans/plan-list.component.spec.ts`
- Create: `frontend/ui/src/app/control-plane/plans/plan-detail.component.ts`
- Create: `frontend/ui/src/app/control-plane/plans/plan-detail.component.html`
- Create: `frontend/ui/src/app/control-plane/plans/plan-detail.component.spec.ts`
- Create: `frontend/ui/src/app/control-plane/campaigns/campaigns.component.ts`
- Create: `frontend/ui/src/app/control-plane/campaigns/campaigns.component.html`
- Create: `frontend/ui/src/app/control-plane/campaigns/campaigns.component.spec.ts`
- Create: `frontend/ui/src/app/control-plane/campaigns/campaign-detail.component.ts`
- Create: `frontend/ui/src/app/control-plane/campaigns/campaign-detail.component.html`
- Create: `frontend/ui/src/app/control-plane/campaigns/campaign-detail.component.spec.ts`
- Create: `frontend/ui/src/app/control-plane/executions/executions.component.ts`
- Create: `frontend/ui/src/app/control-plane/executions/executions.component.html`
- Create: `frontend/ui/src/app/control-plane/executions/executions.component.spec.ts`
- Create: `frontend/ui/src/app/control-plane/executions/execution-detail.component.ts`
- Create: `frontend/ui/src/app/control-plane/executions/execution-detail.component.html`
- Create: `frontend/ui/src/app/control-plane/executions/execution-detail.component.spec.ts`
- Create: `frontend/ui/src/app/control-plane/approvals/approvals.component.ts`
- Create: `frontend/ui/src/app/control-plane/approvals/approvals.component.html`
- Create: `frontend/ui/src/app/control-plane/approvals/approvals.component.spec.ts`
- Create: `frontend/ui/src/app/control-plane/reconciliation/reconciliation.component.ts`
- Create: `frontend/ui/src/app/control-plane/reconciliation/reconciliation.component.html`
- Create: `frontend/ui/src/app/control-plane/reconciliation/reconciliation.component.spec.ts`
- Create: `frontend/ui/src/app/control-plane/audit/audit.component.ts`
- Create: `frontend/ui/src/app/control-plane/audit/audit.component.html`
- Create: `frontend/ui/src/app/control-plane/audit/audit.component.spec.ts`
- Create: `frontend/ui/src/app/control-plane/setup/setup.component.ts`
- Create: `frontend/ui/src/app/control-plane/setup/setup.component.html`
- Create: `frontend/ui/src/app/control-plane/setup/setup.component.spec.ts`
- Create: `frontend/ui/e2e/control-plane.spec.ts`
- Create: `frontend/ui/e2e/fixtures/control-plane.ts`
- Create: `docs/fork/PR-080_OPERATOR_CONTROL_ROOM_UI.md`

**Routes:**

```text
/fleet
/releases
/releases/:releaseId
/deployments/targets
/deployments/targets/:deploymentTargetId
/deployments/plans
/deployments/plans/:planId
/deployments/campaigns
/deployments/campaigns/:campaignId
/deployments/executions
/deployments/executions/:executionId
/approvals
/reconciliation
/audit
/setup
```

`/deployments` redirects to `/deployments/targets`. Static `targets|plans|campaigns|executions` children precede `{path: ':deploymentTargetId'}`; the legacy detail route redirects to `/deployments/targets/:deploymentTargetId` while preserving query parameters and fragment.

- [ ] Add route tests first for static precedence, legacy redirect/query/fragment preservation, process/scoped feature gate, and unauthorized redirect.
- [ ] Add service tests for cursor/filter serialization, immutable action idempotency keys, and structured error rendering.
- [ ] Build fleet matrix, release graph/compare, plan review, campaign wave control, execution timeline, approval inbox, reconciliation, audit, and setup pages. Each handles loading, empty, unauthorized, error, partial, stale, unknown, disabled, and paginated states.
- [ ] Plan review visibly renders target/baseline/config/provider/migration/change/risk/approval/window/adapter/intent checksums and blockers outside drawers.
- [ ] Mutations require proportional confirmation and navigate to the resulting immutable/draft revision.
- [ ] Add `@playwright/test` and `pnpm test:e2e:control-plane`; fixture provisions vendor admin, scoped approver, executor operator, audit viewer, and unauthorized user.
- [ ] E2E covers setup/import, component/product release, shared target comparison, approve/invalidate, campaign pause/resume, execution/previous state, drift/reconcile, audit export/deep links, legacy links, and all error/state variants.
- [ ] Verify and commit.

```powershell
pnpm exec ng test --watch=false --include 'frontend/ui/src/app/control-plane/**/*.spec.ts' --include frontend/ui/src/app/app-logged-in.routes.spec.ts
pnpm exec playwright test frontend/ui/e2e/control-plane.spec.ts --config playwright.control-plane.config.ts
pnpm run build:community
git diff --check
git add package.json pnpm-lock.yaml playwright.control-plane.config.ts frontend/ui docs
git commit -m "feat: add operator control room"
```

## Task 3: PR-081 — Neutral End-to-End and Performance Proof

**Files:**

- Create: `examples/control-plane-e2e/compose.yaml`
- Create: `examples/control-plane-e2e/README.md`
- Create: `examples/control-plane-e2e/fixture.json`
- Create: `examples/control-plane-e2e/run.mjs`
- Create: `examples/control-plane-e2e/external-executor.mjs`
- Create: `examples/control-plane-e2e/observer.mjs`
- Create: `examples/control-plane-e2e/reference-executor/main.go`
- Create: `examples/control-plane-e2e/reference-executor/main_test.go`
- Modify: `hack/control-plane-scale-fixture.mjs`
- Create: `hack/control-plane-load-test.mjs`
- Create: `hack/control-plane-failure-matrix.mjs`
- Create: `docs/release/control-plane-neutral-proof.md`
- Create: `docs/fork/PR-081_NEUTRAL_CONTROL_PLANE_PROOF.md`

The fixture creates two separately configured neutral targets. Target A uses the HTTP external-executor adapter; Target B uses a deterministic reference adapter. Both use separately registered observers and the same component/product release digests.

- [ ] Write reference-executor tests for signed intent, fencing, idempotent operations, status/cancel, bounded logs, and restart persistence.
- [ ] Implement a fixture with provider/consumer capability, target-specific config, one retry-safe migration, two waves, approvals/windows, and independent observations.
- [ ] Run publish → product DAG → config snapshots → target plans → approvals → campaign → v2 execution → observations → active state on both targets.
- [ ] Run failure matrix: duplicate dispatch/event, pre/post-ack crash, stale fence, callback loss, timeout, cancel, restart, observer mismatch, drift/reconcile, previous-state B-to-A, v1 regression, and v2 kill switch.
- [ ] Generate the scale fixture and assert the spec section 20.9 thresholds with raw p50/p95/p99 and hardware/build/dataset metadata.
- [ ] Verify zero adopter names/paths in core/fixture and commit.

```powershell
go test ./examples/control-plane-e2e/reference-executor -count=1 -race
node examples/control-plane-e2e/run.mjs --mode clean
node hack/control-plane-failure-matrix.mjs --fixture examples/control-plane-e2e/fixture.json
node hack/control-plane-scale-fixture.mjs --targets 1000 --placements 649 --agents 100 --components 100 --steps 500 --out work/control-plane-scale.json
node hack/control-plane-load-test.mjs --fixture work/control-plane-scale.json --duration 10m --rate 100
rg -n -i 'emlo|choice[ -]?tp|remittance|jenkins|amazon ecr' examples/control-plane-e2e hack/control-plane-failure-matrix.mjs hack/control-plane-load-test.mjs hack/control-plane-scale-fixture.mjs
```

Expected: test/fixture/load commands exit 0; the final scan has no matches in the PR-081 community fixture/harness files.

```powershell
git add examples/control-plane-e2e hack/control-plane-* docs
git commit -m "test: prove neutral control plane execution"
```

## Task 4: PR-082 — Allowlisted Sample Retirement and Audit Tombstones

**Files:**

- Create: `internal/migrations/sql/161_sample_domain_retirement.up.sql`
- Create: `internal/migrations/sql/161_sample_domain_retirement.down.sql`
- Create: `internal/types/sample_retirement.go`
- Create: `internal/retirement/preview.go`
- Create: `internal/retirement/preview_test.go`
- Create: `internal/retirement/apply.go`
- Create: `internal/retirement/apply_test.go`
- Create: `internal/db/sample_retirement.go`
- Create: `internal/db/sample_retirement_test.go`
- Create: `api/sample_retirement.go`
- Create: `internal/handlers/sample_retirement.go`
- Create: `internal/handlers/sample_retirement_test.go`
- Create: `cmd/hub/cmd/retire_sample_domain.go`
- Create: `cmd/hub/cmd/retire_sample_domain_test.go`
- Modify: `internal/routing/routing.go`
- Create: `docs/adr/0067-sample-retirement-and-audit-tombstones.md`
- Create: `docs/operations/sample-domain-retirement.md`
- Create: `docs/fork/PR-082_SAMPLE_DOMAIN_RETIREMENT.md`

Migration 161 creates `SampleRetirementJob`, `SampleRetirementItem`, `SampleRetirementCheckpoint`, and `AuditSubjectTombstone`. Jobs require immutable backup/restore-proof references/checksums, an exact ID allowlist, ownership markers, preview checksum, approval ID, and applied counts. Audit events are never deleted.

```go
func PreviewSampleRetirement(context.Context, types.SampleRetirementRequest) (*types.SampleRetirementPreview, error)
func VerifyRetirementReverseReferences(context.Context, uuid.UUID) (types.ReferenceReport, error)
func ApplySampleRetirement(context.Context, uuid.UUID, string) (*types.SampleRetirementResult, error)
func VerifySampleRetirement(context.Context, uuid.UUID) (*types.SampleRetirementVerification, error)
```

Routes: `POST /api/v1/sample-retirements/preview`, `GET /{id}`, `POST /{id}/apply`, `POST /{id}/verify`. CLI mirrors preview/apply/verify and requires the preview checksum on apply.

- [ ] Test exact allowlist only; reject wildcard/name-pattern/age-only/cross-org/protected reverse reference/missing restore proof/stale preview.
- [ ] Test interruption/restart, repeated apply no-op, exact counts, transaction boundaries, tombstone lineage, external audit retention, and no application audit deletion.
- [ ] Use a neutral fixture to retire only tutorial/demo records while preserving release/plan/execution/observation history.
- [ ] Verify and commit.

```powershell
go test ./internal/retirement ./internal/db ./api ./internal/handlers ./cmd/hub/cmd -run 'SampleRetirement|AuditTombstone|Cleanup' -count=1
mise run lint:migrations
git add internal/migrations/sql/161_* internal/types/sample_retirement.go internal/retirement internal/db/sample_retirement* api/sample_retirement.go internal/handlers/sample_retirement* cmd/hub/cmd/retire_sample_domain* internal/routing/routing.go docs
git commit -m "feat: retire allowlisted sample domains safely"
```

## Task 5: PR-083 — Release Hardening and Cutover Contract

**Files:**

- Create: `docs/release/enterprise-control-plane-acceptance.md`
- Create: `docs/operations/enterprise-client-deployment.md`
- Create: `docs/operations/control-plane-backup-restore.md`
- Create: `docs/operations/control-plane-v1-v2-rollback.md`
- Create: `docs/operations/control-plane-campaign-incident.md`
- Create: `docs/api/operator-control-plane-api.md`
- Create: `docs/fork/PR-083_ENTERPRISE_CONTROL_PLANE_HARDENING.md`
- Create: `hack/control-plane-acceptance-check.mjs`
- Create: `hack/control-plane-migration-matrix.ps1`
- Create: `hack/control-plane-adopter-term-scan.mjs`
- Modify: `docs/release/community-release-readiness.md`
- Modify: `docs/fork/UPGRADE_GUIDE.md`
- Modify: `docs/fork/FORK_DIFF_INDEX.md`
- Modify: `docs/architecture/community-release-overview.md`
- Modify: `docs/api/community-release-api-index.md`
- Modify: `docs/security/release-hardening-checklist.md`

- [ ] Generate the AC-01..AC-80 ledger with owning PR/task, automated test, manual/fixture evidence, status, and artifact checksum. Fail on a missing/duplicate primary evidence owner or absent community evidence; mark only AC-01, AC-02, AC-48, AC-49, AC-52, AC-54, AC-55, AC-64, and adopter AC-79 `pending-adopter` with exact ADOPTER task owners.
- [ ] Run migration-137 → 161 upgrade, clean install, safe down/refusal, checkpoint restart, v1-only flags-off, mixed v1/v2, v2 flag-off history, and current upstream compatibility tests.
- [ ] Run full Go, Angular, Playwright, Hub/agent builds, Compose E2E, failure matrix, scale/load, migration validation, lint/format, dependency/license, vulnerability, secret, and adopter-term scans.
- [ ] Document the exact standard client workflow, CI publish-only boundary, version/changelog semantics, dependency/provider resolution, approval/campaign/execution/observation/reconciliation, previous-state plan, backup/recovery, incident controls, and rollback flags.
- [ ] Produce immutable community image digest, SBOM/provenance references, database migration report, and release-readiness sign-off. Do not deploy to an adopter yet.
- [ ] Commit.

```powershell
mise run lint:migrations
if ([string]::IsNullOrWhiteSpace($env:DISTR_CONTROL_PLANE_TEST_DATABASE_URL)) { throw 'DISTR_CONTROL_PLANE_TEST_DATABASE_URL is required for the migration matrix' }
pwsh -File hack/control-plane-migration-matrix.ps1 -FromMigration 137 -ToMigration 161 -DatabaseUrl $env:DISTR_CONTROL_PLANE_TEST_DATABASE_URL
node hack/control-plane-acceptance-check.mjs docs/release/enterprise-control-plane-acceptance.md
go test -p=1 ./... -count=1
mise run lint:go
pnpm exec ng test --watch=false
pnpm exec playwright test --config playwright.control-plane.config.ts
pnpm run build:community
mise run build:hub:community
mise run build:agent:docker
mise run build:agent:kubernetes
node examples/control-plane-e2e/run.mjs --mode clean
node hack/control-plane-failure-matrix.mjs --fixture examples/control-plane-e2e/fixture.json
node hack/control-plane-load-test.mjs --fixture work/control-plane-scale.json --duration 10m --rate 100
node hack/pr050-validate-release-hardening.mjs
pnpm audit --prod --audit-level high
govulncheck ./...
node hack/pr050-license-scan.mjs
trivy fs --scanners vuln,secret,license --exit-code 1 .
node hack/control-plane-adopter-term-scan.mjs --base fork/codex/emlo-control-plane-pilot
git diff --check
git add docs hack/control-plane-acceptance-check.mjs hack/control-plane-migration-matrix.ps1 hack/control-plane-adopter-term-scan.mjs
git commit -m "docs: certify enterprise control plane release"
```

## Task 6: ADOPTER-01 — Create a Clean Choice TP DEV Inventory Worktree

This and later adopter tasks run only after PR-083 is accepted.

**Worktree and files:**

- Source repository: `C:\Users\pc\Desktop\repository\emlo-env-settings`
- New clean worktree: `C:\Users\pc\Desktop\repository\worktrees\emlo-env-settings-choice-tp-control-plane`
- Create: `control-plane/inventory/choice-tp-dev.source.json`
- Create: `control-plane/inventory/choice-tp-dev.registry-import.json`
- Create: `control-plane/inventory/choice-tp-dev.coverage.json`
- Create: `control-plane/inventory/choice-tp-dev.pipeline-map.json`
- Create: `control-plane/inventory/choice-tp-dev.dependencies.json`
- Create: `control-plane/README.md`

- [ ] Record the dirty/behind state of the existing checkout and leave it untouched.
- [ ] Fetch remote and create branch `codex/choice-tp-control-plane` in the separate worktree from current remote default HEAD.
- [ ] Run the pinned inventory tool against the version-controlled environment source and approved read-only target discovery. Store tool/version/parameters/source commit/raw report reference/checksum in `choice-tp-dev.source.json`; do not store secret content.
- [ ] Map all physical services to logical definitions/instances; classify managed, external, observe-only, shared, and ignored items. Coverage must account for every discovered placement and explain the difference between environment directories and actual runtime services.
- [ ] Produce `pipeline-map.json` with component key, source repo, exact pipeline/Jenkinsfile or job name, requested ref, build output, current direct-deploy behavior, and owner. This discovery output is the binding file list for ADOPTER-02.
- [ ] Declare the money-changing consumer requirement and transaction-provider capability/binding modes in `dependencies.json`.
- [ ] Preview the Distr import and compare counts/checksum; do not apply yet.
- [ ] Review/commit the inventory artifacts with no credentials or config values.

## Task 7: ADOPTER-02 — Convert CI to Build-Once/Publish-Only

**Shared files in the clean `emlo-env-settings` adopter worktree:**

- Create: `control-plane/ci/publish-component-release.ps1`
- Create: `control-plane/ci/component-release.schema.json`
- Create: `control-plane/ci/provenance-policy.json`
- Create: `control-plane/ci/README.md`
- Modify: `control-plane/inventory/choice-tp-dev.pipeline-map.json` with resulting per-repository branches, commits, PRs, job checks, and publication evidence

- [ ] Group `pipeline-map.json` by unique `sourceRepo`. For each source repository, leave its current checkout untouched, fetch the remote default branch, create `C:\Users\pc\Desktop\repository\worktrees\<repository-name>-distr-publish`, create branch `codex/distr-publish-only`, and modify only the exact pipeline paths recorded for that repository. Review, verify, push, and merge one repository PR at a time. Shared schema/script/policy artifacts remain in the clean `emlo-env-settings` worktree.
- [ ] Add a shared pipeline step that verifies requested ref versus actual commit, builds/tests once, pushes immutable platform digests, creates SBOM/provenance, and publishes Component Release v2 using scoped credentials.
- [ ] Make branch/dependency/environment/shared-library mismatch fail publication eligibility.
- [ ] Disable direct target mutation in the new pipeline path; retain the old path behind a named, owner/date-limited rollback switch during pilot.
- [ ] For each component, run publish-only CI and attach build ID, commit, digest, provenance/SBOM, release ID/checksum, and changelog to the pipeline map.
- [ ] Reject version/platform digest conflicts and verify the same digest is planned for both neutralized target-config variants.
- [ ] Review/commit; no Jenkins secret/token is stored.

## Task 8: ADOPTER-03 — Register Choice TP DEV Adapters, Observer, and Policies

**Files:**

- Create: `control-plane/adapters/choice-tp-dev.executor.json`
- Create: `control-plane/adapters/choice-tp-dev.observer.json`
- Create: `control-plane/policies/choice-tp-dev.policy.json`
- Create: `control-plane/calendars/choice-tp-dev.calendar.json`
- Create: `control-plane/config/choice-tp-dev.snapshot-source.json`
- Create: `control-plane/runbooks/choice-tp-dev-preflight.md`

- [ ] Register scoped external-executor capability/version/config inputs and independent observer identity/trust/freshness/sequence rules.
- [ ] Reference secrets through the approved provider only; store version fingerprints, never values.
- [ ] Publish and bind a DEV policy with four-eyes approval, maintenance window, freeze behavior, wave/bake thresholds, emergency limits, and audit export ownership.
- [ ] Create/verify the immutable config snapshot and component mappings.
- [ ] Exercise signed intent, status, cancel, duplicate event, pre/post-ack crash, stale fence, restart, callback loss, redaction, and observer mismatch without deploying a business release.
- [ ] Apply the previously reviewed registry import only after all coverage/conflict blockers are resolved.

## Task 9: ADOPTER-04 — Choice TP DEV A/B/A Pilot

**Files:**

- Create: `control-plane/products/choice-tp-dev-A.product-release.json`
- Create: `control-plane/products/choice-tp-dev-B.product-release.json`
- Create: `control-plane/campaigns/choice-tp-dev-A-to-B.campaign.json`
- Create: `control-plane/evidence/choice-tp-dev-A-B-A.md`

- [ ] Pin A and B Component Releases and publish Product Releases with the explicit transaction-provider dependency.
- [ ] Create the A-to-B plan from the exact last healthy observed A baseline; review image/config/provider/migration/changelog/risk/checksum.
- [ ] Obtain scoped four-eyes approvals and current window/freeze admission; freeze a one-target campaign.
- [ ] Execute B through protocol v2 and require independent digest/config/schema/capability/health observation before success.
- [ ] Create a new B-to-A previous-state plan, approve/campaign/execute/observe it, and prove B history remains append-only.
- [ ] Retain release, plan, approval, campaign, execution, observation, reconciliation, and audit bundle IDs/checksums in the evidence file.

## Task 10: ADOPTER-05 — Full Choice TP DEV Placement Proof

**Files:**

- Create: `control-plane/products/choice-tp-dev-full.product-release.json`
- Create: `control-plane/campaigns/choice-tp-dev-full.campaign.json`
- Create: `control-plane/evidence/choice-tp-dev-full-coverage.json`
- Create: `control-plane/evidence/choice-tp-dev-cutover-report.md`

- [ ] For every managed placement, prove publish-only CI, immutable release/config, target plan/DAG, adapter execution, and independent observation.
- [ ] For every non-managed placement, retain explicit external/observe-only classification and owner.
- [ ] Prove the transaction provider is either pinned healthy, included ahead of consumer, shared with upstream observation prerequisite, approved external, or explicitly disabled by policy.
- [ ] Run deterministic waves with threshold/bake/pause/resume/restart evidence.
- [ ] Report remaining mutable/direct deploy paths; cutover requires zero or a dated owner-approved exception.

## Task 11: ADOPTER-06 — Preserve Choice TP and Retire Only Demo Data

**Files:**

- Create: `control-plane/cleanup/choice-tp-protected-boundary.json`
- Create: `control-plane/cleanup/demo-retirement-allowlist.json`
- Create: `control-plane/cleanup/demo-retirement-preview.json`
- Create: `control-plane/evidence/demo-retirement-result.md`

- [ ] Export and checksum a database backup and audit inventory; restore-verify in an isolated validation context.
- [ ] Build the protected boundary from every Choice TP organization/customer/registry/config/release/target/unit/plan/task/step/attempt/event/callback/checksum/lock/approval/campaign/desired-state/observation/drift/reconciliation/audit ID, including failed and unknown executions, previous-known-good artifacts, and active/pending baselines.
- [ ] Preview only exact hello-distr/tutorial/demo-owned IDs. Reject name patterns, age filters, wildcard cascades, or any reverse reference to protected history/previous-known-good state.
- [ ] Reconcile exact before counts and preview checksum; obtain cleanup approval.
- [ ] Apply the idempotent retirement job, resume it after one controlled interruption, and re-run apply as a no-op.
- [ ] Verify exact after counts, tombstone/audit lineage, Choice TP login, registry/config, release, plan, task/step, failed attempt/callback, lock, execution, active/pending state, observation/drift/reconciliation, previous-known-good, and audit evidence.
- [ ] Retain backup, restore proof, allowlist, approval, applied counts, and verification result.

## Final Adoption Exit Gate

- [ ] Community PR-055 through PR-083 are accepted; Choice TP tasks have replaced every `pending-adopter` AC row with retained evidence, making the AC-01..AC-80 ledger complete.
- [ ] Neutral two-target proof and scale/failure thresholds pass before any Choice TP mutation.
- [ ] The Choice TP DEV registry accounts for every discovered physical service and contains no unclassified placement.
- [ ] Component pipelines are publish-only for the controlled path; artifact digests/provenance are immutable.
- [ ] A/B/A and full-placement deployments pass independent observation and append-only audit.
- [ ] Choice TP history remains present; only approved demo/tutorial ownership records are retired with tombstones.
- [ ] The cutover report, operator runbook, version/changelog process, dependency workflow, backup/recovery path, and rollback/forward-fix rules are handed off to named owners.
