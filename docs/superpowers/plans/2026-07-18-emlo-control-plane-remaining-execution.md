# EMLO Distr Enterprise Control Plane Remaining Execution Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Finish PR-059 through PR-083, integrate and verify the full enterprise control plane, publish one immutable Distr Hub image, upgrade `distr.emlotech.com`, and demonstrate the standard client workflow with `choice-tp-dev` without rewriting Choice TP audit history.

**Architecture:** Keep feature work in numbered, isolated PR slices and integrate them in migration order. Use fast focused checks while implementing; run the PostgreSQL matrix, full repository, containers, browsers, performance, failure matrix, and security/release gates once on the complete branch. Treat release manifests and frozen plans as immutable inputs, execution/observation/audit as append-only outcomes, Jenkins as build-once/external executor, ECR digests as runtime identity, and Distr as the operator control plane.

**Tech Stack:** Go, PostgreSQL migrations 142–162, Angular 22, Vitest, Playwright, Docker Compose, OCI/ECR, Jenkins, PowerShell, Bash, GitHub pull requests, and `mise`.

## Global Constraints

- The authoritative integration branch is `codex/emlo-control-plane-pilot`; this plan starts from commit `0e77464bec547b5a7de95cbd3a8cd4755e6dfb99`.
- PRs merge in numeric order. Parallel workers may implement subsequent slices on synthetic bases, but final rebases must preserve migration order and expose every dependency explicitly.
- During implementation, run focused compile, unit, mapping/handler, migration-lint, vet, changed-line lint, formatting, and diff checks. Defer live PostgreSQL, the full Go/Angular suites, containers, Playwright, 10-minute load, and vulnerability/license scans to Task 8.
- This deferral is an explicit 2026-07-18 execution decision that supersedes the older per-PR live-database/full-suite timing only. It does not weaken any acceptance requirement: no slice receives final program acceptance until Task 8 proves the complete matrix.
- Do not reduce, stub, or bypass a required feature merely to avoid a deferred test.
- Distr repository, Distr control-plane database, ECR publication, and Distr server deployment are inside the approved scope.
- Before changing Suria, `remittance-b2c-backend`, MC, `transaction-api`, any client service pipeline, client runtime, or client workload database, stop and obtain explicit user approval with the exact repository/service/database and intended mutation.
- Preserve Choice TP releases, plans, tasks, attempts, callbacks, locks, observations, checksums, reconciliation, and audit rows. Never edit historical rows to represent a new run.
- The approved `hello-distr`/tutorial/demo cleanup has already completed. Do not repeat live deletion. A future cleanup is allowed only when a fresh exact-ID preview finds newly introduced sample records, reverse-reference proof is clean, and the user approves the preview checksum.
- Never store or print supplied Jenkins, AWS, server, SSH, application, or database credentials.
- Build the final Hub image exactly once from the accepted source commit. Jenkins may publish an ECR candidate and a checksummed three-value handoff; it must not deploy the server or modify a database.
- A mutable tag is never runtime identity. The handoff must bind `DISTR_IMAGE_REF`, `DISTR_RELEASE_COMMIT`, and `DISTR_IMAGE_DIGEST`.
- A client changelog starts at that target/component's last independently observed successful release, not the latest global release.
- A companion-service constraint names exact versions/digests and order. If the observed provider already satisfies the requirement, attach that proof; otherwise prepare a separate provider release/plan and obtain approval before touching its runtime.
- The core remains client-neutral and must scale beyond 20 clients with more than 20 services each. The neutral fixture exercises at least 1,000 targets, 649 placements, 100 components, and a 500-step wave; Choice TP DEV supplies the real first-client inventory proof.
- Match the enterprise behavior benchmark—immutable releases/process/config, accumulated target-specific changes, scoped multi-client deployment, approvals, freezes, progressive waves, previous-state deployment, reconciliation, and audit—without copying Octopus branding or creating EMLO-specific core entities.

---

## Authoritative Plans and Records

This orchestration plan sequences, but does not duplicate or weaken, the exact file lists, interfaces, and test cases in:

- `docs/superpowers/plans/2026-07-14-control-plane-foundations.md` — PR-055 through PR-065.
- `docs/superpowers/plans/2026-07-14-control-plane-governance-execution.md` — PR-066 through PR-078.
- `docs/superpowers/plans/2026-07-14-control-plane-operator-adoption.md` — PR-079 through PR-083 and ADOPTER-01 through ADOPTER-06.
- `docs/superpowers/plans/2026-07-14-enterprise-operator-control-plane-program.md` — AC-01 through AC-80 ownership.
- `C:\Users\pc\Documents\Codex\2026-06-22\ple\emlo-distr-standard-workflow-review.md` — EMLO architecture, deployment standards, server checkpoint, and Choice TP preservation record.
- `C:\Users\pc\Documents\Codex\2026-06-22\ple\emlo-choice-tp-loyalty-pilot-runbook.md` — historical Choice TP A/B/A evidence and the next-client checklist.

## Current Checkpoint

### Integrated and published to the integration branch

| Slice               | Result                                                                                                  |
| ------------------- | ------------------------------------------------------------------------------------------------------- |
| PR-054A             | Timestamp-expand implementation is in the base; deferred PostgreSQL 16/18 acceptance remains in Task 8. |
| PR-055              | V2 feature isolation integrated as `9f54b504`.                                                          |
| PR-056              | Canonical registry and migration `139` integrated via GitHub PR #69 as `aa9f95c1`.                      |
| Jenkins publication | Build-once/ECR-only pipeline integrated via GitHub PR #70 as `701766ef`.                                |
| PR-057              | Registry import/classification/setup UI and migration `140` integrated via GitHub PR #71 as `e0cd3658`. |
| PR-058              | Immutable target configuration and migration `141` integrated via GitHub PR #72 as `0e77464b`.          |

### Existing work that still requires completion or ordered integration

| Slice      | Current evidence                                | Remaining gate                                                                                                                     |
| ---------- | ----------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| PR-059     | Head `0157f6ea`; 5 dirty paths                  | Finish transaction-safe snapshot reuse and checkpoint-bound `(created_at,id)` cursor chain; commit, rebase, review, merge.         |
| PR-060     | Clean head `f646b9c1`                           | Backport final migration-143 lineage columns from PR-061, rerun focused checks, rebase, merge.                                     |
| PR-061     | Head `c0dacdd2`; 16 dirty paths                 | Finish source/build and reviewed-document binding plus one-batch cursor behavior; commit, rebase on PR-060, merge.                 |
| PR-062     | Clean head `a0f4f94b`; migration `144`          | Rebase on PR-061, rerun focused checks, merge.                                                                                     |
| PR-063     | Head `ea6567b6`; 20 dirty paths                 | Finish immutable blocked v2 publication, config proof, platform/supersession/dependency/lock/bounds/audit fixes; commit and merge. |
| PR-064     | Head `ea6567b6`; 27 dirty paths                 | Finish exact baseline/change/risk persistence and UI contract; rebase on final PR-063 and merge.                                   |
| PR-065     | Clean worktree at `ea6567b6`                    | Implement migration 147 and typed database migration/recovery graph; rebase on PR-064 and merge.                                   |
| PR-066     | Clean reviewed head `bae54958`; migration `148` | Rebase on PR-065 and merge.                                                                                                        |
| PR-067     | Clean reviewed head `10f8f5f3`; migration `149` | Rebase on PR-066 and merge.                                                                                                        |
| PR-068     | Clean reviewed head `1b75c969`; migration `150` | Rebase on PR-067 and merge.                                                                                                        |
| PR-069     | Clean reviewed head `f13d6f48`; migration `151` | Rebase on PR-068 and merge.                                                                                                        |
| PR-070     | Head `3a335355`; 6 dirty paths                  | Complete admission/override implementation, rebase on PR-069, review, merge.                                                       |
| PR-071     | Clean synthetic stack at `43a57716`             | Implement immutable campaigns, rebase on PR-070, review, merge.                                                                    |
| PR-072–073 | No implementation worktree                      | Implement scheduler/thresholds, then operational controls.                                                                         |
| PR-074     | Clean worktree at `ea6567b6`                    | Implement versioned adapter resolution and later rebase on PR-073.                                                                 |
| PR-075–076 | No implementation worktree                      | Implement fenced executor v2, then cancel/status/callback reconciliation.                                                          |
| PR-077     | Clean worktree at `ea6567b6`                    | Implement independent desired/observed state and later rebase on PR-076.                                                           |
| PR-078     | No implementation worktree                      | Implement correlated audit/export and instrument every v2 privileged mutation after PR-070–077 stabilize.                          |
| PR-079–083 | Not started                                     | Implement operator API/UI, neutral proof, safe sample retirement, and release hardening.                                           |

### Live baseline that must remain true

- `distr.emlotech.com` is currently healthy on schema `138`, `dirty=false`.
- The live running ECR index digest recorded by the accepted promotion is `sha256:f510aa26a7f8aa178e0cbee4d1d5f57b19f59536cbeaccc1ef78ff8956293cbc`.
- The Choice TP 24-table fingerprint before and after promotion/cleanup is `sha256:8c5da420606062141fb01d947bd3209c281dce4a96802047dd96e22a8b22be10`.
- Protected evidence currently includes 3 releases, 6 plans, 5 tasks, 5 external executions, 5 events, 10 preflight runs, 70 checks, 1 observed-state row, and 4 observations.
- Protected historical execution evidence is `A → B → A → A`: the B-to-A plan proves previous-state deployment and the fourth A observation proves post-fix resource-lock release. The failed first A attempt also remains evidence.
- The prior approved cleanup removed only its exact demo deployment, target, and application IDs. It did not touch the Choice TP application, target, customer, workload database, or runtime.

## Dependency and Merge Order

```text
PR-059
  -> PR-060 -> PR-061 -> PR-062 -> PR-063 -> PR-064 -> PR-065
  -> PR-066 -> PR-067 -> PR-068 -> PR-069 -> PR-070
  -> PR-071 -> PR-072 -> PR-073 -> PR-074 -> PR-075 -> PR-076
  -> PR-077 -> PR-078
  -> PR-079 -> PR-080 -> PR-081 -> PR-082 -> PR-083
  -> final verification -> ECR publication -> Distr deployment
  -> neutral proof accepted -> Choice TP adoption/approved runtime proof
```

Parallel implementation lanes are permitted:

- Lane A: PR-059/061 closure and PR-063/064/065 foundations.
- Lane B: PR-070/071 campaign admission and PR-072/073 campaign execution.
- Lane C: PR-074/075/077 standalone domain cores.
- Lane D: PR-078 audit core and PR-079/080 planning only until their dependencies compile.

Only the merge lane is strictly serial.

---

### Task 0: Reconcile Status and Architecture-Decision Allocations

**Files:**

- Modify: `docs/adr/README.md`
- Modify/fold: `docs/adr/0057-deployment-registry-import-evidence.md`
- Preserve: `docs/adr/0057-immutable-target-config-snapshots.md`
- Modify/fold before PR-064 merge: `docs/adr/0060a-exact-plan-baselines-and-previous-state.md`
- Modify: `docs/fork/FORK_DIFF_INDEX.md`
- Modify: the checkbox/status sections of the three canonical implementation plans.

- [ ] Keep ADR-0057 assigned to PR-058. Fold PR-057's registry-import decision evidence into `docs/fork/PR-057_DEPLOYMENT_REGISTRY_IMPORT.md`, remove the duplicate ADR allocation, and update every link/index.
- [ ] Fold the unallocated PR-064 ADR-0060A content into `docs/fork/PR-064_EXACT_PLAN_CHANGESET.md` unless the enterprise allocation ledger is explicitly amended and reviewed before PR-064 merge.
- [ ] Track three separate states for every slice: implementation present, focused verification passed, and final acceptance passed. Do not use a checked implementation box as evidence for a deferred live/full gate.
- [ ] Correct the stale fork index and plan checkboxes as each numbered PR actually merges; do not pre-check future work.
- [ ] Run a link/reference scan and formatting check.

```powershell
rg -n '0057-deployment-registry-import|0060a-exact-plan' docs
pnpm exec prettier --check docs/adr docs/fork docs/superpowers/plans
git diff --check
```

### Task 1: Close and Merge PR-059

**Files:**

- Worktree: `C:\Users\pc\Documents\Codex\2026-06-22\ple\worktrees\codex-pr059-review-fixes`
- Modify: `cmd/hub/cmd/backfill_target_config_snapshots.go`
- Modify: `cmd/hub/cmd/backfill_target_config_snapshots_test.go`
- Modify: `internal/db/target_config_snapshots.go`
- Modify: `internal/migrations/sql/142_release_contract_v1_extraction.up.sql`
- Modify: `internal/types/target_config_snapshot.go`
- Canonical task: `docs/superpowers/plans/2026-07-14-control-plane-foundations.md`, Task 5.

**Interfaces:**

- Consumes: PR-058 immutable target configuration at integration commit `0e77464b`.
- Produces: append-only `ReleaseContractV1ExtractionLineage`, bounded restartable checkpoints, and canonical snapshots that PR-060 may reference.

- [ ] Complete the named-constraint `ON CONFLICT ... DO NOTHING RETURNING` path so an existing checksum-identical snapshot is reused without aborting the serializable transaction or duplicating children.
- [ ] Complete a monotonic `(created_at,id)` high-water cursor bound to the checkpoint chain so concurrent inserts cannot be omitted or silently included outside the captured batch boundary.
- [ ] Prove two distinct v1 plans can reference one canonical snapshot and a pre-existing PR-058 snapshot retains its original creator and child graph.
- [ ] Replace stale documentation for removed `--after-plan-id` with the implemented `--predecessor-checkpoint-id` chain and add the unchanged-database cursor-chain regression.
- [ ] Run the focused fast gate.

```powershell
go test ./internal/targetconfig ./internal/db ./cmd/hub/cmd -run 'TargetConfig|V1Extraction|BackfillCheckpoint' -count=1
go vet ./internal/targetconfig ./internal/db ./cmd/hub/cmd
git diff --check
```

- [ ] Commit the closure fixes, rebase/squash only the PR-059 delta onto `0e77464b`, rerun the gate, push, open the PR, and merge after all fast GitHub checks are green with zero failures.
- [ ] Record the merge commit, migration `142`, focused-test command, deferred tests, and review verdict in `.superpowers/sdd/progress.md`.

### Task 2: Close PR-061, Backport Its Forward Schema to PR-060, and Merge PR-060/061

**Files:**

- PR-060 worktree: `C:\Users\pc\Documents\Codex\2026-06-22\ple\worktrees\codex-pr060-component-release-v2`
- PR-061 worktree: `C:\Users\pc\Documents\Codex\2026-06-22\ple\worktrees\codex-pr061-provenance-backfill`
- Modify in both slices as appropriate: `internal/migrations/sql/143_component_release_contract_v2.up.sql`
- Modify in both slices as appropriate: `internal/migrations/sql/143_component_release_contract_v2.down.sql`
- PR-061 core: `internal/db/release_backfill.go`, `internal/db/release_provenance.go`, `internal/releasebundles/provenance.go`, CLI/tests/docs.
- Canonical tasks: foundations Tasks 6 and 7.

**Interfaces:**

- PR-060 produces Component Release v2 artifact/evidence/capability/migration declarations.
- PR-061 consumes those declarations and produces verified, immutable provenance facts plus a restartable release backfill.

- [ ] Finish PR-061 persistence and preflight for exact `sourceCommit` and `buildId`.
- [ ] Bind every reviewed evidence row to immutable document reference, document SHA-256, selected row reference/digest, and media type.
- [ ] Make one CLI invocation process no more than `--batch-size` rows and return a deterministic next cursor; do not loop over the whole tenant internally.
- [ ] Commit PR-061 and extract the exact migration-143 forward/down hunks.
- [ ] Apply only those schema-foundation hunks to PR-060, preserving PR ownership: PR-060 creates the columns/constraints; PR-061 implements verification/backfill behavior.
- [ ] Replay PR-061 without retaining a migration diff after the migration-143 schema foundation is accepted in PR-060; PR-061 intentionally owns no migration number.
- [ ] Run the focused gates.

```powershell
go test ./internal/releasecontracts ./internal/releasebundles ./internal/db ./api ./internal/mapping ./internal/handlers ./cmd/hub/cmd -run 'ComponentRelease|Provenance|ReleaseBackfill' -count=1
go vet ./internal/releasecontracts ./internal/releasebundles ./internal/db ./cmd/hub/cmd
mise run lint:migrations
git diff --check
```

- [ ] Rebase/squash PR-060 on the merged PR-059 commit, review, push, and merge.
- [ ] Rebase/squash PR-061 on the merged PR-060 commit, review, push, and merge.

### Task 3: Integrate PR-062 and Close PR-063

**Files:**

- PR-062 worktree: `C:\Users\pc\Documents\Codex\2026-06-22\ple\worktrees\codex-pr062-product-release-dag`
- PR-063 worktree: `C:\Users\pc\Documents\Codex\2026-06-22\ple\worktrees\codex-pr063-target-plan-v2`
- PR-063 modifies migration `145`, deployment-plan draft/repository/API/mapping/handler/task-queue/planning files listed in foundations Task 9.

**Interfaces:**

- PR-062 produces the immutable Product Release capability DAG.
- PR-063 resolves that DAG with target configuration and publishes a sealed target plan.

- [ ] Rebase PR-062 on merged PR-061 and retain fail-closed PR-061 and PR-067 verifier seams.
- [ ] Finish PR-063 so a published v2 parent and every child are immutable and `BLOCKED`/non-executable until PR-075.
- [ ] Replace the unavailable runtime verifier registration with the real PR-058 target-config object verifier; verify the object rather than accepting a copied checksum.
- [ ] Resolve platform by membership in the component's platform set; do not reject a valid multi-platform release.
- [ ] Restrict supersession to one tip for the same organization, deployment unit, environment, application, and target.
- [ ] Make prerequisite edges gate the first executable mutation, including database migration steps.
- [ ] Keep the documented v1-compatible path reachable and byte/status compatible.
- [ ] Acquire `ACCESS EXCLUSIVE` before down-migration data checks, cap collections, batch repository loads, and persist creator/updater/publisher/audit facts.
- [ ] Run focused gates and merge PR-062, then PR-063.

```powershell
go test ./internal/productrelease ./internal/planning ./internal/db ./api ./internal/mapping ./internal/handlers -run 'ProductRelease|Capability|DeploymentPlanV2|RequirementResolver|Supersed' -count=1
go vet ./internal/productrelease ./internal/planning ./internal/db ./api ./internal/mapping ./internal/handlers
mise run lint:migrations
git diff --check
```

### Task 4: Finish PR-064 and Implement PR-065

**Files:**

- PR-064 worktree: `C:\Users\pc\Documents\Codex\2026-06-22\ple\worktrees\codex-pr064-plan-changes`
- PR-065 worktree: `C:\Users\pc\Documents\Codex\2026-06-22\ple\worktrees\codex-pr065-migration-graph`
- Canonical tasks: foundations Tasks 10 and 11.

**Interfaces:**

- PR-064 produces exact baseline, change, risk, bootstrap, and previous-state facts.
- PR-065 consumes the plan graph and produces typed backup/migration/validation/recovery nodes.

- [ ] Finish migration `146`, exact last-trusted healthy desired revision and observation checksum, deterministic image/config/provider/schema changes, accumulated skipped releases, bootstrap evidence, stale CAS, risk entries, forward-only block, and B-to-A as new append-only history.
- [ ] Rebase PR-064 on final PR-063 and resolve plan type/API/UI conflicts without dropping PR-063's sealed blocked state.
- [ ] Reconcile the nine known overlapping PR-063/064 deployment plan/draft API, repository, mapping, graph, and type files explicitly. Do not accept a blind conflict resolution without semantic tests.
- [ ] Implement migration `147` and these exact action types:

```text
database.backup.create
database.backup.verify
database.migration.apply
database.migration.validate
database.migration.reverse
database.restore.execute
database.restore.verify
```

- [ ] Require backup verification before mutation, stable retry keys, target/database locks, probes, reverse-dependency recovery, forward-only/manual recovery, and separately approved restore with no normal-plan shortcut.
- [ ] Keep v1 unchanged and retain bounded/redacted action input/evidence.
- [ ] Run focused gates and merge PR-064, then PR-065.

```powershell
go test ./internal/planning ./internal/migrationplanning ./internal/actionregistry ./internal/deploymentpreflight ./internal/db ./api ./internal/mapping ./internal/handlers -run 'Baseline|ChangeSet|PreviousState|Migration|Backup|Recovery|Restore' -count=1
go vet ./internal/planning ./internal/migrationplanning ./internal/actionregistry ./internal/deploymentpreflight ./internal/db
mise run lint:migrations
git diff --check
```

### Task 5: Rebase and Merge Reviewed Governance PR-066 Through PR-069, Then Finish PR-070

**Files:**

- Worktrees: `codex-pr066-scoped-authorization`, `codex-pr067-deployment-policies`, `codex-pr068-approvals`, `codex-pr069-maintenance-calendars`, and `codex-pr070-admission`.
- Replace placeholder authorization wiring in `internal/handlers/operator_control_plane_mutations.go`, `internal/handlers/approvals.go`, and `internal/handlers/maintenance_calendars.go`.
- Canonical tasks: governance Tasks 1–5.

**Interfaces:**

- PR-066 produces scoped authorization and tenant/environment enrollment.
- PR-067 produces immutable effective policy snapshots.
- PR-068 produces checksum-bound approvals.
- PR-069 produces versioned calendar/freeze decisions.
- PR-070 composes those facts into one append-only admission decision.

- [ ] Transplant only the authoritative feature commits in order, discarding duplicate synthetic prerequisite commits: PR-066 `6233d1f9` + `bae54958`; PR-067 `4c2eb9af` + `9fd18dce` + `10f8f5f3`; PR-068 `1b75c969`; PR-069 `5bdad62a` + `93b67306` + `f13d6f48`.
- [ ] Wire PR-066 scoped authorization into PR-067 policy mutations, PR-068 approval mutations, and PR-069 calendar/freeze mutations. Remove fail-closed placeholder authorizers only when the real tenant/scope checks and denial tests pass.
- [ ] For each rebase, run its exact focused command from the governance plan, strict-review the resulting diff, push, and merge only after fast CI is green.
- [ ] Finish migration `152`, pure deterministic admission, append-only temporal evidence, and checksum-bound emergency override.
- [ ] Include `internal/types/admission.go` in PR-070's owned file/add list, then add repository, API, mapping, handlers, routing, scoped authorization/enrollment, and documentation; the current evaluator/tests alone are not a complete slice.
- [ ] Allow only named accelerations; never bypass integrity, provenance, backup, evidence, observation, or mandatory health gates.
- [ ] Make `CreateTasksForAdmittedV2Plan` the only v2 task-creation wrapper; do not alter shared v1 task creation.
- [ ] Require PR-066 authorization/enrollment and retain the flags-off v1 regression.

```powershell
go test ./internal/authorization ./internal/governance ./internal/scheduling ./internal/db ./api ./internal/handlers -run 'Authorization|Enrollment|DeploymentPolicy|Approval|Calendar|Freeze|Admission|EmergencyOverride' -count=1
go vet ./internal/authorization ./internal/governance ./internal/scheduling ./internal/db ./api ./internal/handlers
mise run lint:migrations
git diff --check
```

### Task 6: Implement and Merge PR-071 Through PR-078

**Files and contracts:** Use governance Tasks 6–13 exactly.

| PR  | Migration | Deliverable                                                                                | Fast focused proof                                                                                                  |
| --- | --------: | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------- |
| 071 |       153 | Immutable campaign drafts/revisions/waves/members/prerequisites                            | Stable membership/order/checksum, frozen plan/approval/observation facts, bake validation, tenant authority.        |
| 072 |       154 | Persisted campaign run state machine, fenced scheduler lease, thresholds and bake          | Legal transitions, deterministic order, duplicate tick, lease loss, threshold stop, restart.                        |
| 073 |       155 | Idempotent pause/resume/retry/exclude/cancel controls                                      | Safe point, no new admission, concurrent controls, uncertain outcome, v1/v2 retry split.                            |
| 074 |       156 | Versioned adapter implementations/capabilities/assignments and frozen plan-step resolution | Exact capability/version/target/config match, disabled/ambiguous/drift fail closed.                                 |
| 075 |       157 | Signed fenced executor protocol v2                                                         | Golden intent, tamper/key/expiry, idempotent events, heartbeat, stale fence, crash boundaries, locks, v1 unchanged. |
| 076 |       158 | Cancel/status/callback-loss reconciliation                                                 | Proven success/failure/unknown, status before retry, cancellability, reconciliation identity.                       |
| 077 |       159 | Pending/active desired state, independent observations, drift and reconciliation           | Trust/freshness/sequence, observer mismatch, partial/unknown/failure, prior active retention.                       |
| 078 |       160 | Correlated append-only audit, deterministic evidence bundles, external export              | Complete correlation, tenant isolation, redaction, ordered idempotent retry, sink failure/lag visibility.           |

- [x] Use separate worktrees for PR-071, PR-074, PR-075, PR-077, and PR-078 domain cores while the sequential campaign/executor dependencies stabilize.
- [x] PR-072 defines a compile-safe `CampaignObservationVerifier` interface. Until PR-077 registers the independent observer implementation, any prerequisite requiring an observation ID/checksum remains blocked; never bind it to the legacy executor projection.
- [x] PR-075 uses Ed25519 over the canonical intent payload and SHA-256 checksum. `keyId` resolves to the versioned public-key fingerprint frozen in the PR-074 adapter/config revision; the private key remains in the configured secret provider. Rotation publishes a new adapter/config revision with an overlap interval and explicit revocation evidence.
- [x] After PR-077 is final, instrument every v2 privileged mutation/state transition through PR-078's single append helper in the same transaction or outbox boundary.
- [x] PR-078 cross-cutting instrumentation covers plan draft/publication, policy, approval, calendar/freeze, admission/override, campaign/control, adapter assignment/resolution, execution/control, observation/desired-state, drift, and reconciliation repositories/handlers. Its final diff includes those owning files, not only `internal/auditexport`.
- [x] Rebase and merge only in order 071 → 078.
- [x] Run the exact focused command in each canonical PR task while its packages exist. After PR-078 is stacked, run this combined governance smoke; do not use it as a substitute for the per-slice commands.

```powershell
go test ./internal/campaigns ./internal/campaignworker ./internal/adapterresolution ./internal/executionprotocol ./internal/executionworker ./internal/desiredstate ./internal/observation ./internal/reconciliation ./internal/auditexport ./internal/db ./api ./internal/handlers -run 'Campaign|Adapter|ExecutionV2|Fence|Cancel|StatusQuery|Desired|Observation|Drift|Reconciliation|ControlPlaneAudit|EvidenceBundle|AuditExport' -count=1
go vet ./internal/campaigns ./internal/campaignworker ./internal/adapterresolution ./internal/executionprotocol ./internal/executionworker ./internal/desiredstate ./internal/observation ./internal/reconciliation ./internal/auditexport
mise run lint:migrations
git diff --check
```

### Task 7: Implement PR-079 Through PR-083

**Files and contracts:** Use `docs/superpowers/plans/2026-07-14-control-plane-operator-adoption.md`, Tasks 1–5 exactly.

| PR  | Deliverable                                                                           | Implementation-time gate                                                          |
| --- | ------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------- |
| 079 | Migration 161, paginated operator read models/APIs, scale fixture/benchmark harness   | Query/filter/cursor/isolation tests; smoke benchmark only.                        |
| 080 | Angular operator control room and role-based E2E fixtures                             | Component/service/route tests and community build; defer full Playwright run.     |
| 081 | Neutral two-target executor/observer/previous-state/failure/load harness              | Reference executor tests and a short clean smoke; defer full failure/load matrix. |
| 082 | Migration 162, exact-ID sample retirement and audit tombstones                        | Unit/repository/neutral-fixture tests only; no live cleanup.                      |
| 083 | AC-01..AC-80 acceptance ledger, runbooks, migration/release scripts, cutover contract | Static acceptance checker and documentation/build smoke.                          |

- [ ] PR-079 exposes fleet, release, plan, campaign, execution, reconciliation, and audit pages with default page 50, maximum 100, stable cursors, tenant isolation, no N+1 query, and all blockers/checksums visible.
- [ ] PR-079's scale evidence proves the generic schema/read models support more than 20 client organizations with more than 20 service placements per client without per-client code or unbounded queries.
- [ ] PR-080 keeps `/deployments` and legacy target links compatible, then adds fleet, release, plan, campaign, execution, approval, reconciliation, audit, and setup routes with loading/empty/unauthorized/error/partial/stale/unknown/disabled states.
- [ ] PR-080 modifies the existing `frontend/ui/src/app/app-logged-in.routes.spec.ts` and preserves PR-058 setup/config-snapshot coverage; it does not recreate or overwrite that file.
- [ ] PR-081 proves the generic community workflow on two neutral targets and contains no EMLO, Choice TP, remittance, Jenkins, or ECR adopter terms.
- [ ] PR-082 rejects wildcard/name/age cleanup, requires backup and restore-proof checksums, exact ownership, clean reverse references, immutable preview checksum, approval, restartability, tombstones, and no audit deletion.
- [ ] PR-083 assigns one primary evidence owner to every AC-01..AC-80 row and documents the standard client workflow, version/changelog semantics, provider constraints, approvals/campaign/execution/observation, previous state, backup/recovery, incident handling, and flags.
- [ ] PR-083 also updates `.github/workflows/community-release-hardening.yaml`, `deploy/jenkins/Jenkinsfile.hub-image`, `deploy/jenkins/publish-hub-image.sh`, and `deploy/server-docker-compose/deploy.sh` so the enforced release produces a real SBOM/provenance artifact, migration report, and post-deploy operator/API/UI/flag/audit acceptance bundle. The existing empty-package SPDX fallback cannot count as release evidence.

```powershell
go test ./internal/operatorqueries ./internal/retirement ./internal/db ./api ./internal/mapping ./internal/handlers ./cmd/hub/cmd -run 'Operator|Fleet|ReadModel|Pagination|SampleRetirement|AuditTombstone|Cleanup' -count=1
pnpm exec ng test --watch=false --include 'frontend/ui/src/app/control-plane/**/*.spec.ts' --include frontend/ui/src/app/app-logged-in.routes.spec.ts
pnpm run build:community
go test ./examples/control-plane-e2e/reference-executor -count=1
node examples/control-plane-e2e/run.mjs --mode clean --smoke
node hack/control-plane-acceptance-check.mjs docs/release/enterprise-control-plane-acceptance.md
mise run lint:migrations
git diff --check
```

### Task 8: Run the Deferred Complete-Branch Verification Gate

**Files:**

- Create/update evidence under `docs/release`, `work/control-plane-*`, and the PR-083 acceptance ledger.
- Execute against the final PR-083 integration commit only.

**Interfaces:**

- Consumes: migrations 138–162 and all accepted features.
- Produces: one release candidate commit with complete automated and neutral-fixture evidence.

- [ ] Run clean install and migration 138 → 162 on PostgreSQL 16.14 and 18.4, safe down/refusal, interrupted checkpoint restart, v1-only flags off, mixed v1/v2, and v2-history flags off.
- [ ] Close PR-054A's deferred PostgreSQL service-container acceptance and revalidate integrated PR-055–058 as part of the same complete-branch matrix.
- [ ] Run full Go, Angular, Playwright, Hub/agent builds, Compose neutral E2E, failure matrix, 10-minute load, migration/lint/format, dependency/license/vulnerability/secret, and adopter-term scans.
- [ ] Fix failures in the owning PR domain; do not weaken the gate.
- [ ] Rerun the failed focused gate, then rerun the complete gate from a clean state.

```powershell
mise run lint:migrations
pwsh -File hack/control-plane-migration-matrix.ps1 -FromMigration 138 -ToMigration 162 -DatabaseUrl $env:DISTR_CONTROL_PLANE_TEST_DATABASE_URL
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
node hack/control-plane-scale-fixture.mjs --targets 1000 --placements 649 --agents 100 --components 100 --steps 500 --out work/control-plane-scale.json
node hack/control-plane-load-test.mjs --fixture work/control-plane-scale.json --duration 10m --rate 100
node hack/pr050-validate-release-hardening.mjs
pnpm audit --prod --audit-level high
govulncheck ./...
node hack/pr050-license-scan.mjs
trivy fs --scanners vuln,secret,license --exit-code 1 .
node hack/control-plane-adopter-term-scan.mjs --base fork/codex/emlo-control-plane-pilot
git diff --check
```

Expected: every command exits `0`; migration version is `162` and `dirty=false`; the neutral fixture has no adopter terms; no unresolved Critical/Important review finding remains.

### Task 9: Publish Once and Deploy the Accepted Distr Candidate

**Files:**

- `deploy/jenkins/Jenkinsfile.hub-image`
- `deploy/jenkins/publish-hub-image.sh`
- `deploy/server-docker-compose/deploy.sh`
- Checksummed Jenkins artifact: `dist/release-<immutable-candidate-tag>.env` and `.sha256`

**Interfaces:**

- Jenkins consumes one reviewed 40-character source commit and produces one ECR digest plus checksummed handoff.
- The server consumes the exact commit/ref/digest triplet, upgrades only the Distr control-plane database, and records append-only deployment evidence.

- [ ] Open the final consolidation pull request from `codex/emlo-control-plane-pilot` to `fork/main`; merge only after Task 8 is green and the AC ledger shows no missing community evidence.
- [ ] Freeze the resulting accepted `fork/main` commit and verify the worktree is clean and pushed. Never deploy the feature-branch commit directly.
- [ ] Run the Jenkins build-once pipeline. Verify requested commit equals checkout commit, OCI revision equals commit, ECR tags are immutable, the pulled digest matches the pushed digest, and the handoff contains exactly the bound image ref, commit, and digest.
- [ ] Take and checksum a read-only live Distr control-plane database backup. Record live schema, dirty flag, running digest, readiness, Choice TP 24-table fingerprint, protected row counts, and audit-head checksums.
- [ ] Restore the backup into isolated PostgreSQL 18.4, apply migrations 139–162 with the candidate digest, start an isolated acceptance Hub, run readiness/API/Choice TP read checks, and verify the protected fingerprint/counts.
- [ ] On `194.233.69.203`, stage the checksummed handoff and exact deployment package. Pull by digest, verify OCI revision, apply the supported migration/release command, and wait for local readiness.
- [ ] Verify public `https://distr.emlotech.com/ready`, running digest, schema `162`, `dirty=false`, login/static assets, operator APIs/routes, feature flags, audit export, and append-only deployment audit.
- [ ] Recompute the Choice TP fingerprint and protected counts. Any mismatch stops sign-off and triggers evidence-preserving diagnosis.
- [ ] Retain backup, restore proof, candidate handoff, ECR digest, migration report, health output, audit bundle, and rollback/forward-fix decision.

Application rollback may switch only to an image recorded compatible with schema `162`. If no exact compatibility record exists, keep the Hub fenced and use the documented forward-fix or approved Distr control-plane database restore path.

### Task 10: Prepare Choice TP DEV Adoption Without Client Mutation

**Files:**

- Use a new clean `emlo-env-settings` worktree; never modify the existing dirty/behind checkout.
- Produce the exact ADOPTER-01 inventory artifacts and `pipeline-map.json` defined in the operator/adopter plan.

**Interfaces:**

- Consumes: accepted PR-083 release, read-only environment source, and approved read-only target discovery.
- Produces: complete placement inventory, pipeline binding list, dependency constraints, and a no-write Distr import preview.

- [ ] Account for every discovered Choice TP DEV physical service and placement as managed, external, observe-only, shared, or ignored with an owner and reason; no unclassified placement is allowed.
- [ ] Record component key, repository, exact pipeline/job, requested ref, build output, current direct-deploy behavior, and owner.
- [ ] Generate the immutable release/dependency manifest rules:

```text
human release version
source commit
exact built commit
Jenkins build ID
platform digest(s)
SBOM/provenance references
target's last healthy observed version/digest/checksum
accumulated code/config/migration/dependency changelog
env-settings commit and config object checksums
provided and required companion-service versions/digests
intended deployment order
health gate
rollback group and migration compatibility
```

- [ ] For an MC plan requiring `transaction-api`, verify the independently observed provider version/digest. If it satisfies the constraint, attach the evidence and deploy only MC. If it does not, prepare a separate transaction-api release and provider-first plan, then stop for explicit approval before any transaction-api or client-runtime mutation.
- [ ] Preview the Distr registry import and compare counts/checksum. Apply only after all omissions/conflicts are resolved; applying the Distr import is inside Distr scope and does not deploy a client service.
- [ ] Rotate or replace temporary pilot webhook/callback/executor credentials in the approved secret stores, retain key-version activation/revocation evidence, and verify the old version cannot authorize a new intent after the overlap window. Never put secret values in the plan or evidence bundle.

### Task 11: Explicit Approval Gates for External Repositories and Client Runtime

- [ ] Before ADOPTER-02, show the user the exact repository/pipeline file list from `pipeline-map.json`, the intended build-once/publish-only changes, and confirm that no direct deployment occurs. Obtain approval for every Suria/B2C/MC/transaction-api/client repository that would be edited.
- [ ] Before ADOPTER-04 or ADOPTER-05 execution, show the target, services, versions/digests, config checksums, dependencies/order, migration/backup classification, health gate, previous-known-good state, and rollback/forward-fix method. Obtain approval for the named client runtime and any client workload database action.
- [ ] A missing approval blocks only the external/client mutation. Continue Distr-only configuration, neutral fixtures, documentation, and read-only evidence work.

### Task 12: Demonstrate the Complete Standard Workflow in the UI

Use `choice-tp-dev` as the sample client after the neutral gate and required external approvals.

The historical `A → B → A → A` pilot remains protected evidence and does not substitute for this fresh protocol-v2, publish-only, independent-observer demonstration.

- [ ] **Setup/coverage:** Show registry import preview, exact counts, classifications, omissions/conflicts, and 100% accounted placements.
- [ ] **Version/build:** Show Component Release version, source commit, built commit, Jenkins build, platform digest, provenance/SBOM, and immutable release checksum.
- [ ] **Changelog:** Show the accumulated code/config/migration/dependency delta from `choice-tp-dev`'s last healthy observed baseline, including skipped releases.
- [ ] **Manifest/constraints:** Show Product Release dependency DAG, exact provided/required versions/digests, target resolution mode, and provider-first order.
- [ ] **Configuration:** Show target-config snapshot metadata, object version/checksum/fingerprint, placement binding, and no secret values.
- [ ] **Plan:** Show baseline/observation checksum, image/config/provider/schema changes, risks, backup/migration/recovery nodes, policy, approvals, window/freeze, adapter, intent, and canonical plan checksum.
- [ ] **Campaign:** Show immutable membership, waves, bake/threshold policy, prerequisites, pause/resume/restart behavior, and no new exposure while paused.
- [ ] **Execution:** Show signed intent, attempt/fence generation, idempotency identity, status/cancel/reconciliation evidence, and exact Jenkins executor correlation.
- [ ] **Observation:** Show independent actual digest/config/schema/capability/health evidence, desired-state promotion, drift classification, and reconciliation.
- [ ] **Previous state:** Create and execute a new B-to-A plan; do not edit the A-to-B plan. Show exact current-versus-previous changes and retained B history.
- [ ] **Audit:** Export one deterministic evidence bundle correlating release, config, plan, approval, campaign, execution, adapter, observation, reconciliation, actor, outcome, and checksums.
- [ ] **Fleet:** Show every managed Choice TP placement and every explicit external/observe-only/ignored placement; report any remaining mutable/direct-deploy path with owner and expiry.

### Task 13: Preserve Choice TP and Hand Off the Standard

- [ ] Compare the final Choice TP protected fingerprint/counts with the recorded pre-deployment baseline.
- [ ] Do not apply another cleanup when no new exact allowlisted sample IDs exist.
- [ ] If new sample records exist, create only a PR-082 preview and stop for approval; apply/verify must be restartable and leave audit tombstones.
- [ ] Retain release, plan, approval, campaign, execution, observation, reconciliation, audit-bundle, backup, restore, deployment, and UI proof IDs/checksums.
- [ ] Update the review and runbook with the final schema/digest, AC-01..AC-80 evidence, standard next-client packet, dependency example, rollback/forward-fix rules, and named operational owners.

## Completion Gate

The goal is complete only when all of the following are proven by current evidence:

- [ ] PR-055 through PR-083 are accepted and integrated in order.
- [ ] After ADOPTER-01 through ADOPTER-06, the nine `pending-adopter` AC rows are replaced by retained evidence and AC-01..AC-80 has no missing or duplicate primary owner.
- [ ] Migrations 138–162 pass clean install, upgrade, refusal, restart, PostgreSQL 16.14, and PostgreSQL 18.4 gates.
- [ ] Full Go/Angular/Playwright/build/Compose/failure/load/release scans pass on the exact published commit.
- [ ] The published ECR digest has the accepted OCI revision and is the digest running on `distr.emlotech.com`.
- [ ] The live Distr database is schema `162`, `dirty=false`, and ready.
- [ ] Neutral two-target execution, failure, previous-state, and scale proof passes before Choice TP mutation.
- [ ] Choice TP DEV inventory has zero unclassified placements.
- [ ] Scale evidence proves the same generic workflow supports the EMLO operating shape of more than 20 clients and more than 20 services per client.
- [ ] Every approved managed placement uses build-once/publish-only artifacts, immutable config/plan, controlled execution, and independent observation.
- [ ] The UI visibly demonstrates versioning, accumulated changelog, manifests, companion constraints, planning, approvals, campaigns, execution, observation, previous state, and audit.
- [ ] Choice TP protected history/fingerprint remains intact; only separately approved exact sample IDs are retired.
- [ ] No Suria, B2C, MC, transaction-api, client runtime, or client workload database mutation occurred without its explicit approval evidence.

## Execution Handoff

The user has already selected **Subagent-Driven** execution. Resume with fresh workers in dependency-safe worktrees, two-stage review per slice, fast implementation gates, and numbered integration. The first resumed slice is PR-059; Tasks 2–7 may use parallel implementation lanes while Task 1 owns the merge lane.
