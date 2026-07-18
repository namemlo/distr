# Fork Diff Index

This file tracks generic fork additions and upstream-facing changes introduced after the upstream base recorded in `docs/fork/UPSTREAM_BASE.md`.

## Current Status

PR-000 through PR-056 and the speculative PR-058 backend slice are implemented locally. PR-054A timestamp-expand runtime, migration, audited dirty-marker
recovery, and operator documentation are implemented locally; final acceptance remains pending. PR-055 establishes
default-off, layered kill switches for the operator control plane and executor protocol v2 without changing v1
behavior. PR-056 adds the organization-scoped canonical deployment registry; its isolated PostgreSQL integration
legs remain mandatory in CI. PR-058 adds immutable target configuration snapshots and bounded object verification;
it remains based directly on the PR-056 checkpoint until PR-057 is accepted and migration 140 is available.

## Tracking Template

Use one entry per pull request:

```markdown
## PR-000 - Baseline and fork records

- Status:
- Upstream base:
- Feature flag:
- User-facing behavior:
- Database changes:
- API changes:
- UI changes:
- Agent protocol changes:
- Documentation:
- Tests:
- Upstream contribution notes:
- Compatibility notes:
```

## Entries

### PR-000 - Baseline and fork records

- Status: Complete for documentation-only baseline; local build limitations are recorded in `docs/fork/UPSTREAM_BASE.md`.
- Upstream base: `b49fb27eb6270d7a71eed82b12e47eec1217c4cf`
- Feature flag: None
- User-facing behavior: None
- Database changes: None
- API changes: None
- UI changes: None
- Agent protocol changes: None
- Documentation: Added fork baseline, fork diff index, ADR template, root Codex instructions, and master roadmap.
- Tests: See `docs/fork/UPSTREAM_BASE.md`.
- Upstream contribution notes: Documentation-only fork baseline.
- Compatibility notes: No runtime compatibility impact.

### PR-001 - Experimental feature flag framework

- Status: Implemented locally; focused tests, builds, and diff-scoped lint passed.
- Upstream base: `b49fb27eb6270d7a71eed82b12e47eec1217c4cf`
- Feature flag: Added instance-level experimental flags configured by `DISTR_EXPERIMENTAL_FEATURE_FLAGS`.
- User-facing behavior: Admins can view registered experimental roadmap flags and enabled state in Organization Settings.
- Database changes: None.
- API changes: Added `GET /api/v1/experimental-feature-flags`, admin-only.
- UI changes: Added readonly Experimental features table on Organization Settings and Angular service support.
- Agent protocol changes: None.
- Documentation: Added PR-001 user story/API notes and ADR-0001.
- Tests: `mise run test`, Angular `ng test`, `mise run build:hub:community`, Docker agent build, Kubernetes agent build, direct migration validation, touched-file Prettier check, and diff-scoped Go lint passed.
- Upstream contribution notes: Community-neutral abstraction; no adopter-specific logic.
- Compatibility notes: Existing deployments and agents are unchanged. Unknown configured flag keys are rejected at Hub startup.

### PR-002 - Environment domain model

- Status: Implemented locally; backend, frontend, migration, lint, and build verification completed.
- Upstream base: `b49fb27eb6270d7a71eed82b12e47eec1217c4cf`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=environments`.
- User-facing behavior: Admins can manage Environments from a new feature-flagged Environments page.
- Database changes: Added organization-scoped `Environment` table with unique names per organization and non-negative sort order.
- API changes: Added feature-flagged CRUD endpoints under `/api/v1/environments`.
- UI changes: Added Environments route, sidebar link, Angular service, and CRUD table/dialog UI with loading, error, empty, create, update, and delete states.
- Agent protocol changes: None.
- Documentation: Added PR-002 notes and ADR-0002.
- Tests: `go test -p=1 ./...`, Angular `ng test --watch=false`, `pnpm run build:community`, hub community binary build, Docker agent build, Kubernetes agent build, direct migration validation, touched-file Prettier check, and diff-scoped Go lint passed. Full repo `pnpm lint` and full repo Go lint still report pre-existing formatting issues outside the PR-owned diff.
- Upstream contribution notes: Community-neutral environment model; no adopter-specific logic.
- Compatibility notes: Existing deployment targets, deployments, and agents are unchanged. No target-to-environment assignment is added in PR-002.

### PR-003 - Lifecycle domain model

- Status: Implemented locally; backend, frontend, migration, lint, and build verification completed.
- Upstream base: `b49fb27eb6270d7a71eed82b12e47eec1217c4cf`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=lifecycles`; the UI also requires `environments` so phase environments can be selected.
- User-facing behavior: Admins can manage Lifecycles and ordered phases from a new feature-flagged Lifecycles page.
- Database changes: Added organization-scoped `Lifecycle`, `LifecyclePhase`, and `LifecyclePhaseEnvironment` tables with unique lifecycle names per organization, unique phase names/orders per lifecycle, non-negative phase counters, and environment references.
- API changes: Added feature-flagged CRUD endpoints under `/api/v1/lifecycles` plus phase list/replace endpoints under `/api/v1/lifecycles/{lifecycleId}/phases`.
- UI changes: Added Lifecycles route, sidebar link, Angular service/types, and CRUD table/dialog UI with a dynamic phase editor.
- Agent protocol changes: None.
- Documentation: Added PR-003 notes and ADR-0003.
- Tests: `go test ./api ./internal/mapping ./internal/handlers ./internal/lifecycle ./internal/db`, `go test -p=1 ./...`, Angular `ng test --watch=false`, `pnpm run build:community`, hub community binary build, Docker agent build, Kubernetes agent build, direct migration validation, touched-file Prettier check, and diff-scoped Go lint passed. Full repo `pnpm lint` and full repo Go lint still report pre-existing formatting issues outside the PR-owned diff.
- Upstream contribution notes: Community-neutral lifecycle model; no adopter-specific logic.
- Compatibility notes: Existing deployment targets, deployments, releases, and agents are unchanged. No channel link, release promotion, approval, retention, or deployment execution behavior is added in PR-003.

### PR-004 - Channel domain model

- Status: Implemented locally; backend, frontend, migration, lint, and build verification completed.
- Upstream base: `b49fb27eb6270d7a71eed82b12e47eec1217c4cf`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=channels`; the UI also requires `environments` and `lifecycles`.
- User-facing behavior: Admins can manage application-scoped Channels from a new feature-flagged Channels page.
- Database changes: Added organization-scoped `Channel` table linked to `Application` and `Lifecycle`, with unique names per organization/application and one default Channel per organization/application.
- API changes: Added feature-flagged CRUD endpoints under `/api/v1/channels`.
- UI changes: Added Channels route, sidebar link, Angular service/types, and CRUD table/dialog UI with application and lifecycle selectors.
- Agent protocol changes: None.
- Documentation: Added PR-004 notes and ADR-0004.
- Tests: `go test ./api ./internal/mapping ./internal/handlers ./internal/db ./internal/routing`, live PostgreSQL Channel repository tests with `DISTR_TEST_DATABASE_URL` set, `go test -p=1 ./...`, Angular `ng test --watch=false`, `pnpm run build:community`, hub community binary build, Docker agent build, Kubernetes agent build, direct migration validation, touched-file Prettier check, diff-scoped Go lint, and changed-file Unicode scan passed.
- Upstream contribution notes: Community-neutral Channel model; no adopter-specific logic.
- Compatibility notes: Existing Environment, Lifecycle, deployment target, deployment, release, and agent behavior is unchanged. No SemVer/source-rule engine, release bundles, promotion, approval, retention, or deployment execution behavior is added in PR-004.

### PR-005 - SemVer and source-rule engine

- Status: Implemented locally; backend, frontend, migration, lint, and build verification completed.
- Upstream base: `b49fb27eb6270d7a71eed82b12e47eec1217c4cf`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=channels`.
- User-facing behavior: Admins can configure Channel version ranges, prerelease patterns, and allowed source branch/tag globs on the feature-flagged Channels page.
- Database changes: Added Channel text-array rule columns for allowed version ranges, prerelease patterns, source branches, and source tags.
- API changes: Extended Channel CRUD payloads with rule arrays and added `POST /api/v1/channels/{channelId}/validate-version` for organization-scoped rule validation.
- UI changes: Added Channel editor text areas for rule lists and Angular service support for version/source validation.
- Agent protocol changes: None.
- Documentation: Added PR-005 notes and ADR-0005.
- Tests: `go test ./internal/channelrules ./api ./internal/mapping ./internal/handlers ./internal/db ./internal/routing`, live PostgreSQL Channel repository and validation handler tests with `DISTR_TEST_DATABASE_URL` set, `go test -p=1 ./...`, Angular `ng test --watch=false`, `pnpm run build:community`, hub community binary build, Docker agent build, Kubernetes agent build, direct migration validation, touched-file Prettier check, diff-scoped Go lint, and changed-file Unicode scan passed.
- Upstream contribution notes: Community-neutral SemVer/source-rule engine; no adopter-specific logic.
- Compatibility notes: Existing Environment, Lifecycle, deployment target, deployment, release, and agent behavior is unchanged. No Release Bundle, promotion, approval, retention, deployment planning, execution, or agent behavior is added in PR-005.

### PR-006 - Release Bundle foundation

- Status: Implemented locally; backend, API, repository, canonicalization, migration, lint, and build verification completed.
- Upstream base: `b49fb27eb6270d7a71eed82b12e47eec1217c4cf`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=release_bundles`.
- User-facing behavior: Feature-flagged API callers can create, list, read, update, and delete draft Release Bundles with components.
- Database changes: Added organization-scoped `ReleaseBundle` and `ReleaseBundleComponent` tables, release-number uniqueness per organization/application, component-key uniqueness per bundle, draft status storage, canonical payload storage, and checksum storage.
- API changes: Added feature-flagged draft CRUD endpoints under `/api/v1/release-bundles`.
- UI changes: None. Release Bundle UI remains PR-008.
- Agent protocol changes: None.
- Documentation: Added PR-006 notes and ADR-0006.
- Tests: API validation, canonical checksum, mapping, handler, live PostgreSQL repository tests, migration checks, and focused Go tests were added.
- Upstream contribution notes: Community-neutral Release Bundle foundation; no adopter-specific component or registry behavior.
- Compatibility notes: Existing Environment, Lifecycle, Channel, deployment target, deployment, release-name, and agent behavior is unchanged. No publication, validation service, promotion, approval, retention, planning, execution, or agent behavior is added in PR-006.

### PR-007 - Release validation and publication

- Status: Implemented locally; backend, API, repository, validation, migration, lint, and build verification completed.
- Upstream base: `b49fb27eb6270d7a71eed82b12e47eec1217c4cf`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=release_bundles`.
- User-facing behavior: Feature-flagged API callers can validate draft Release Bundles, publish valid drafts, block published bundles, and archive published or blocked bundles.
- Database changes: Added Release Bundle publication actor/time columns and append-only Release Bundle audit events for publish, block, archive, and rejected transitions.
- API changes: Added feature-flagged `validate`, `publish`, `block`, and `archive` endpoints under `/api/v1/release-bundles/{releaseBundleId}`.
- UI changes: None. Release Bundle UI remains PR-008.
- Agent protocol changes: None.
- Documentation: Added PR-007 notes and ADR-0007.
- Tests: API validation, release validation, mapping, handler, live PostgreSQL repository tests, migration checks, and focused Go tests were added.
- Upstream contribution notes: Community-neutral validation and publication behavior; no adopter-specific component, registry, promotion, or deployment behavior.
- Compatibility notes: Existing Environment, Lifecycle, Channel, deployment target, deployment, release-name, and agent behavior is unchanged. No Release UI, CI release API, promotion, approval, retention, planning, execution, or agent behavior is added in PR-007.

### PR-008 - Release UI

- Status: Implemented locally; Angular route, service, component, tests, lint, and build verification completed.
- Upstream base: `b49fb27eb6270d7a71eed82b12e47eec1217c4cf`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=release_bundles`; the UI route/sidebar also require `environments`, `lifecycles`, and `channels`.
- User-facing behavior: Vendor admins can list, create, edit, validate, publish, block, archive, delete, and inspect Release Bundles from a feature-flagged Release Bundles page.
- Database changes: None.
- API changes: None. The UI uses the existing PR-006/PR-007 Release Bundle endpoints.
- UI changes: Added Release Bundles route, sidebar link, Angular service/types, list/detail views, draft editor, component editor, validation result display, and status-aware action confirmations.
- Agent protocol changes: None.
- Documentation: Added PR-008 notes and ADR-0008.
- Tests: Angular service, component, and feature-flag tests were added.
- Upstream contribution notes: Community-neutral Release Bundle UI; no adopter-specific terminology, provider logic, or Octopus UI/assets.
- Compatibility notes: Existing Environment, Lifecycle, Channel, deployment target, deployment, release-name, and agent behavior is unchanged. No CI release API, lifecycle eligibility, promotion, deployment planning, approval, retention, execution, notification, or agent behavior is added in PR-008.

### PR-009 - CI release API and CLI

- Status: Implemented locally; backend, migration, CLI, examples, lint, tests, and builds completed.
- Upstream base: `b49fb27eb6270d7a71eed82b12e47eec1217c4cf`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=release_bundles`.
- User-facing behavior: CI systems can idempotently create draft Release Bundles, validate them, and explicitly publish them through the public API or `distr release` CLI commands.
- Database changes: Added Release Bundle source metadata columns and organization-scoped `ReleaseBundleIdempotencyKey` records with hashed keys, request checksums, Release Bundle references, and unique organization/key enforcement.
- API changes: Added optional `Idempotency-Key` support on `POST /api/v1/release-bundles`, optional `sourceMetadata`, strict OCI sha256 digest validation, and structured idempotency conflict responses.
- UI changes: Updated shared Angular Release Bundle types for optional source metadata only; no Release UI behavior change.
- Agent protocol changes: None.
- Documentation: Added PR-009 notes, ADR-0009, release CLI guide, CI tutorial, and neutral Jenkins, GitHub Actions, GitLab CI, and curl examples.
- Tests: Focused API, canonicalization, mapping, handler, repository, and CLI tests passed; live PostgreSQL Release Bundle repository and handler tests passed with `DISTR_TEST_DATABASE_URL` set; `go test -p=1 ./...`, Angular tests, migration-pair validation, touched-file Prettier checks, diff-scoped Go lint, community frontend build, community Hub build, Docker agent build, Kubernetes agent build, CLI local/linux builds, example parse checks, secret scan, and changed-file Unicode scan passed.
- Upstream contribution notes: Community-neutral CI API and CLI; no Jenkins-only, registry-provider-specific, or adopter-specific core behavior.
- Compatibility notes: Existing clients that omit `Idempotency-Key` retain previous create behavior. Existing Environment, Lifecycle, Channel, deployment target, deployment, release-name, and agent behavior is unchanged. No lifecycle eligibility, promotion, deployment planning, approval, retention, execution, notification, or agent behavior is added in PR-009.

### PR-010 - Lifecycle eligibility engine

- Status: Implemented locally; focused backend, mapping, handler, repository, live PostgreSQL, lint, and build verification completed.
- Upstream base: `b49fb27eb6270d7a71eed82b12e47eec1217c4cf`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=release_bundles,channels,lifecycles,environments`.
- User-facing behavior: Feature-flagged API callers can explain whether a Release Bundle is eligible for a lifecycle environment and receive structured blocking reasons.
- Database changes: None.
- API changes: Added `GET /api/v1/release-bundles/{releaseBundleId}/eligibility?environmentId={environmentId}`.
- UI changes: None.
- Agent protocol changes: None.
- Documentation: Added PR-010 notes and ADR-0010.
- Tests: Focused lifecycle, mapping, handler, repository, and live PostgreSQL tests were added.
- Upstream contribution notes: Community-neutral lifecycle eligibility explanation; no adopter-specific release, promotion, or deployment behavior.
- Compatibility notes: Existing Environment, Lifecycle, Channel, Release Bundle, deployment target, deployment, release-name, and agent behavior is unchanged. No promotion execution, deployment planning, approval, retention, notification, UI workflow, or agent behavior is added in PR-010.

### PR-011 - Deployment Process schema

- Status: Implemented locally; backend, API, repository, migration, mapping, handler, and live PostgreSQL verification completed.
- Upstream base: `b49fb27eb6270d7a71eed82b12e47eec1217c4cf`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=deployment_processes`.
- User-facing behavior: Feature-flagged API callers can manage organization/application-scoped Deployment Processes and append immutable revisions with ordered steps and validated dependencies.
- Database changes: Added `DeploymentProcess`, `DeploymentProcessRevision`, `DeploymentProcessStep`, `DeploymentProcessStepDependency`, `DeploymentProcessStepChannel`, and `DeploymentProcessStepEnvironment` tables with process-name uniqueness per organization/application, unique step keys/orders per revision, scoped step Channel/Environment references, and composite Channel/application/organization FK protection.
- API changes: Added feature-flagged CRUD endpoints under `/api/v1/deployment-processes` plus revision list/create/get endpoints under `/api/v1/deployment-processes/{deploymentProcessId}/revisions`.
- UI changes: None. Process editor UI remains PR-012.
- Agent protocol changes: None.
- Documentation: Added PR-011 notes and ADR-0011.
- Tests: API validation, mapping, handler, live PostgreSQL repository and handler integration, and migration checks were added.
- Upstream contribution notes: Community-neutral Deployment Process schema; no adopter-specific action types, providers, or business logic.
- Compatibility notes: Existing Environment, Lifecycle, Channel, Release Bundle, deployment target, deployment, release-name, and agent behavior is unchanged. No Step Template API, Release Bundle process snapshot/link, variable, deployment planning, approval, retention, execution, notification, UI workflow, or agent behavior is added in PR-011.

### PR-012 - Process editor UI

- Status: Implemented locally; Angular route, service, component, tests, lint, and build verification completed.
- Upstream base: `b49fb27eb6270d7a71eed82b12e47eec1217c4cf`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=deployment_processes`; the UI route/sidebar also require `environments`, `lifecycles`, and `channels`.
- User-facing behavior: Vendor admins can list, create, edit, delete, inspect revision history, view revision details, and create new structured revisions for Deployment Processes from a feature-flagged Deployment Processes page.
- Database changes: None.
- API changes: None. The UI uses the existing PR-011 Deployment Process CRUD and revision endpoints.
- UI changes: Added Deployment Processes route, sidebar link, Angular service/types, list view, process form, revision history view, revision detail view, and structured step revision editor.
- Agent protocol changes: None.
- Documentation: Added PR-012 notes and ADR-0012.
- Tests: Angular service, component, and feature-flag tests were added.
- Upstream contribution notes: Community-neutral Deployment Process UI; no adopter-specific terminology, provider logic, or Octopus UI/assets.
- Compatibility notes: Existing Environment, Lifecycle, Channel, Release Bundle, deployment target, deployment, release-name, and agent behavior is unchanged. No process snapshots, Release Bundle process links, variables, deployment planning, approval, retention, execution, notification, or agent behavior is added in PR-012.

### PR-013 - Process snapshots

- Status: Implemented locally; backend, API, repository, migration, mapping, handler, Angular service, and live PostgreSQL verification completed.
- Upstream base: `b49fb27eb6270d7a71eed82b12e47eec1217c4cf`
- Feature flag: Existing Release Bundle create/update behavior uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=release_bundles`; the read-only process snapshot endpoint also requires `deployment_processes`.
- User-facing behavior: API callers can link a draft Release Bundle to an immutable Deployment Process revision snapshot and read the linked snapshot.
- Database changes: Added `ProcessSnapshot`, nullable `ReleaseBundle.process_snapshot_id`, deterministic snapshot payload/checksum storage, unique snapshot per Deployment Process revision, and composite organization/application foreign keys for Release Bundle snapshot links.
- API changes: Added optional `deploymentProcessRevisionId` to Release Bundle create/update requests, optional `processSnapshotId` to Release Bundle responses, and `GET /api/v1/release-bundles/{releaseBundleId}/process-snapshot`.
- UI changes: Updated shared Angular Release Bundle types and service support for the read-only process snapshot endpoint. No screen, route, sidebar, or selector UI is added.
- Agent protocol changes: None.
- Documentation: Added PR-013 notes and ADR-0013.
- Tests: API validation, process snapshot canonicalization, mapping, handler, live PostgreSQL repository and handler integration, migration checks, and Angular service tests were added.
- Upstream contribution notes: Community-neutral Process Snapshot model; no adopter-specific terminology, provider logic, or Octopus UI/assets.
- Compatibility notes: Existing Environment, Lifecycle, Channel, deployment target, deployment, release-name, and agent behavior is unchanged. Existing Release Bundle clients may omit `deploymentProcessRevisionId`. No variable, deployment planning, approval, retention, execution, notification, or agent behavior is added in PR-013.

### PR-014 - Variable types and sets

- Status: Implemented locally; backend, API, repository, migration, mapping, handler, Angular UI, and live PostgreSQL verification completed.
- Upstream base: `4752a5b2ae25192a5158194ac3b9dd8225325bbf`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=scoped_variables_v2`.
- User-facing behavior: Vendor admins can manage organization-scoped Variable Sets with typed variables, optional Application links, and safe Secret references from a feature-flagged admin UI.
- Database changes: Added `VariableSet`, `VariableSetApplication`, and `Variable` tables with organization scoping, Variable Set name uniqueness per organization, Variable key uniqueness per set, typed JSON default checks, organization-scoped Application links, and restricted same-organization Secret references.
- API changes: Added feature-flagged CRUD endpoints under `/api/v1/variable-sets`.
- UI changes: Added Variable Sets route, sidebar link, Angular service/types, list view, form, typed variable editor, Application selectors, safe Secret selector, loading/error/empty/confirmation states, and Angular tests.
- Agent protocol changes: None.
- Documentation: Added PR-014 notes and ADR-0014.
- Tests: API validation, mapping, feature-flag, handler, live PostgreSQL repository and handler integration, migration checks, and Angular service/component tests were added.
- Upstream contribution notes: Community-neutral Variable Set model; no adopter-specific terminology, provider logic, or plaintext secret storage.
- Compatibility notes: Existing Environment, Lifecycle, Channel, Release Bundle, Deployment Process, deployment target, deployment, release-name, and agent behavior is unchanged. No scoped resolution, variable snapshots, deployment planning, approval, retention, execution, notification, or agent behavior is added in PR-014.

### PR-015 - Scoped variable resolver

- Status: Implemented; backend resolver, API, repository, migration, mapping, handler, Angular UI, and focused verification completed.
- Upstream base: `e78ee16d4f60ae25a4727a80c7b37d512400a736`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=scoped_variables_v2`.
- User-facing behavior: Vendor admins can add scoped values to Variables, see duplicate-scope warnings, and preview deterministic resolution with safe trace output from the Variable Sets page.
- Database changes: Added `VariableScopedValue` with organization-scoped foreign keys, allowed scope-shape checks, one payload source per scoped value, duplicate-scope uniqueness per Variable, and composite constraints for same-organization scope references.
- API changes: Extended Variable Set CRUD payloads/responses with `scopedValues` and added feature-flagged `POST /api/v1/variables/resolve-preview`.
- UI changes: Extended the Variable Sets page with scoped-value editing, optional organization lookup selectors, resolution preview modal, redacted Secret reference display, and trace output.
- Agent protocol changes: None.
- Documentation: Added PR-015 notes and ADR-0015.
- Tests: Pure resolver, API validation, mapping, feature-flag, handler, live PostgreSQL repository and handler integration, migration checks, and Angular service/component tests were added.
- Upstream contribution notes: Community-neutral scoped resolver model; no adopter-specific terminology, provider execution logic, or plaintext secret exposure.
- Compatibility notes: Existing Environment, Lifecycle, Channel, Release Bundle, Deployment Process, deployment target, deployment, release-name, and agent behavior is unchanged. No variable snapshots, drift detection, deployment planning, approval, retention, execution, notification, runbook persistence, or agent behavior is added in PR-015.

### PR-016 - Variable snapshots and drift

- Status: Implemented locally; backend snapshot repository, drift comparison, API, migration, mapping, handler, Angular UI, and focused verification completed.
- Upstream base: `ddd8b3bc88e09efc371aecace29700a4301bdadc`
- Feature flag: Release Bundle publication keeps `release_bundles`; snapshot reads require `release_bundles` and `scoped_variables_v2`; drift API/UI requires `scoped_variables_v2`.
- User-facing behavior: Vendor admins can see read-only configuration drift categories on deployment details when scoped variables are enabled. API callers can read redacted Variable snapshots for published Release Bundles.
- Database changes: Added `VariableSnapshot`, `VariableSnapshotValue`, nullable `ReleaseBundle.variable_snapshot_id`, canonical snapshot payload/checksum storage, organization/application/channel foreign keys, and a redaction check preventing plaintext values on redacted rows.
- API changes: Added feature-flagged `GET /api/v1/variable-snapshots/{variableSnapshotId}` and `GET /api/v1/deployments/{deploymentId}/configuration-drift`.
- UI changes: Added a feature-flagged deployment detail configuration drift panel with loading, no-drift, API-error, and drift-category states.
- Agent protocol changes: None.
- Documentation: Added PR-016 notes and ADR-0016.
- Tests: Drift comparator, mapping, feature-flag, handler, live PostgreSQL repository and handler integration, migration checks, and Angular service/component tests were added.
- Upstream contribution notes: Community-neutral Variable snapshot and drift model; no adopter-specific terminology, provider execution logic, or plaintext secret exposure.
- Compatibility notes: Existing Environment, Lifecycle, Channel, Release Bundle draft/edit, Deployment Process, deployment target, deployment, release-name, and agent behavior is unchanged. No action registry, deployment planning, promotion execution, approval, retention, notification, runbook persistence, or agent behavior is added in PR-016.

### PR-017 - Built-in action registry

- Status: Implemented locally; action registry, API, validation, mapping, handler, Angular service, and verification completed.
- Upstream base: `f3cb5d607ae301cb8bd35ee95e7b161fcf016801`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=deployment_processes`.
- User-facing behavior: Feature-flagged API callers and Angular services can list built-in Deployment Process action definitions for `distr.preflight`, `distr.http.check`, and `distr.wait`.
- Database changes: None.
- API changes: Added `GET /api/v1/action-definitions` and Deployment Process revision validation for registered action types and JSON-schema-valid `inputBindings`.
- UI changes: Added Angular service/type support for reading action definitions and updated existing Deployment Process examples to use registered generic actions. No new route or sidebar entry is added.
- Agent protocol changes: None.
- Documentation: Added PR-017 notes and ADR-0017.
- Tests: Action registry, API validation, mapping, handler, live PostgreSQL Deployment Process repository, process snapshot canonicalization, and Angular service tests were added or updated.
- Upstream contribution notes: Community-neutral built-in action metadata; no adopter-specific action names or execution logic.
- Compatibility notes: Existing Environment, Lifecycle, Channel, Release Bundle, Process Snapshot, Variable Set, deployment target, deployment, release-name, and agent behavior is unchanged. No Deployment Plan foundation, task queue, execution, approvals, retention, notifications, runbooks, Step Templates, Compose/Helm/OCI/file/webhook adapters, or agent behavior is added in PR-017.

### PR-018 - Deployment Plan foundation

- Status: Implemented locally; backend, API, repository, migration, mapping, handler, and live PostgreSQL verification completed.
- Upstream base: `84d1bf887976ee85652461ad9e02e5d0e445abfc`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=deployment_plans,release_bundles,deployment_processes,scoped_variables_v2,channels,lifecycles,environments`.
- User-facing behavior: Feature-flagged API callers can create and inspect resolved Deployment Plan previews with selected targets, resolved process steps, resolved variable snapshot values, and structured blockers/warnings.
- Database changes: Added `DeploymentPlan`, `DeploymentPlanTarget`, `DeploymentPlanStep`, `DeploymentPlanVariable`, and `DeploymentPlanIssue` tables with organization-scoped composite references to Release Bundles, Environments, Process Snapshots, Variable Snapshots, and Deployment Targets.
- API changes: Added `GET /api/v1/deployment-plans`, `POST /api/v1/deployment-plans`, and `GET /api/v1/deployment-plans/{deploymentPlanId}`.
- UI changes: None. Plan UI, export, and checksum display remain PR-019.
- Agent protocol changes: None.
- Documentation: Added PR-018 notes and ADR-0018.
- Tests: API validation, mapping, feature-flag, handler, live PostgreSQL repository and handler integration, and migration checks were added.
- Upstream contribution notes: Community-neutral Deployment Plan foundation; no adopter-specific terminology, provider execution logic, or target mutation.
- Compatibility notes: Existing Environment, Lifecycle, Channel, Release Bundle, Deployment Process, Process Snapshot, Variable Snapshot, deployment target, deployment, release-name, and agent behavior is unchanged. No Plan UI, JSON/Markdown export, task queue, execution, locks, approvals, retention, notifications, runbooks, rollout waves, or agent behavior is added in PR-018.

### PR-019 - Deployment Plan UI and export

- Status: Implemented locally; Angular route, sidebar gating, service, types, component UI, and focused Angular verification completed.
- Upstream base: `12b2a398d480d641c49a2d5c218f772a02f04da2`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=deployment_plans,release_bundles,deployment_processes,scoped_variables_v2,channels,lifecycles,environments`.
- User-facing behavior: Vendor admins can list Deployment Plans, create a plan from a published Release Bundle, Environment, and selected targets, inspect blockers, warnings, resolved targets, steps, variables, and copy-safe checksums, and export the plan as JSON or Markdown.
- Database changes: None.
- API changes: None. PR-019 consumes the PR-018 `GET /api/v1/deployment-plans`, `POST /api/v1/deployment-plans`, and `GET /api/v1/deployment-plans/{deploymentPlanId}` API.
- UI changes: Added Deployment Plans route, sidebar feature gating, Angular service/types, list view, create modal, detail preview modal, checksum display, JSON export, Markdown export, loading, empty, validation, and API-error states.
- Agent protocol changes: None.
- Documentation: Added PR-019 notes and ADR-0019.
- Tests: Angular service, feature-flag, and Deployment Plans component tests were added or updated.
- Upstream contribution notes: Community-neutral plan preview UI; no adopter-specific terminology, provider execution logic, or Octopus assets.
- Compatibility notes: Existing Environment, Lifecycle, Channel, Release Bundle, Deployment Process, Process Snapshot, Variable Snapshot, deployment target, deployment, release-name, backend planning behavior, and agent behavior is unchanged. No task queue, execution, locks, approvals, retention, notifications, runbooks, rollout waves, or agent behavior is added in PR-019.

### PR-020 - Durable task queue

- Status: Implemented locally; backend, API, repository, migration, mapping, handler, feature-flag, and live PostgreSQL verification completed.
- Upstream base: `dd83b703aea7ad8dd9b47b3bf62f936883c90cf0`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=task_queue,deployment_plans,release_bundles,deployment_processes,scoped_variables_v2,channels,lifecycles,environments`.
- User-facing behavior: Feature-flagged API callers can create durable Tasks from READY Deployment Plans, list/read queued Tasks in deterministic order, and transition Task state through the guarded PR-020 state machine.
- Database changes: Added `Task_queue_order_seq`, `Task`, and `StepRun` with organization-scoped composite references to Deployment Plans, Deployment Plan targets, Deployment Plan steps, and Deployment Targets.
- API changes: Added `POST /api/v1/deployment-plans/{deploymentPlanId}/tasks`, `GET /api/v1/tasks`, `GET /api/v1/tasks/{taskId}`, and `POST /api/v1/tasks/{taskId}/state`.
- UI changes: None. No Task Queue Angular route, sidebar entry, or page is added in PR-020.
- Agent protocol changes: None.
- Documentation: Added PR-020 notes and ADR-0020.
- Tests: API validation, mapping, feature-flag, handler, live PostgreSQL repository and handler integration, migration checks, and focused Go tests were added.
- Upstream contribution notes: Community-neutral durable task queue foundation; no adopter-specific terminology, provider logic, or execution behavior.
- Compatibility notes: Existing Environment, Lifecycle, Channel, Release Bundle, Deployment Process, Process Snapshot, Variable Snapshot, Deployment Plan preview/UI, deployment target, deployment, release-name, frontend planning UI, and agent behavior is unchanged. No locks, concurrency policies, leases, heartbeats, agent capabilities, agent task endpoints, execution adapters, approvals, cancellation, timelines, logs, or guided failure behavior is added in PR-020.

### PR-021 - Locks and concurrency

- Status: Implemented locally; backend, API, repository, migration, mapping, handler, documentation, and live PostgreSQL verification completed.
- Upstream base: `5910e5a99d8b8108e64dd7675c18be11d20c314e`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=task_queue,deployment_plans,release_bundles,deployment_processes,scoped_variables_v2,channels,lifecycles,environments`.
- User-facing behavior: Feature-flagged API callers can create Tasks with default deployment-target locks, optional additional lock resources, and `QUEUE`, `REJECT_NEW`, `CANCEL_OLDER`, or `ALLOW_PARALLEL` concurrency policies.
- Database changes: Added `TaskResourceLock`, lock indexes, lock backfill for existing Tasks, and terminal Task status `CANCELED`.
- API changes: Extended `POST /api/v1/deployment-plans/{deploymentPlanId}/tasks` with an optional concurrency body and added `locks` to Task responses.
- UI changes: None. No Task Queue Angular route, sidebar entry, or page is added in PR-021.
- Agent protocol changes: None.
- Documentation: Added PR-021 notes and ADR-0021.
- Tests: API validation, mapping, handler, live PostgreSQL repository and handler integration, migration checks, and race-condition tests were added.
- Upstream contribution notes: Community-neutral lock resource and concurrency policy model; no adopter-specific terminology, provider logic, or execution behavior.
- Compatibility notes: Existing Environment, Lifecycle, Channel, Release Bundle, Deployment Process, Process Snapshot, Variable Snapshot, Deployment Plan preview/UI, deployment target, deployment, release-name, frontend planning UI, and agent behavior is unchanged. No agent capability protocol, leases, heartbeats, agent task endpoints, execution adapters, approvals, guided failure, timelines, logs, or agent changes are added in PR-021.

### PR-022 - Agent capability protocol

- Status: Implemented locally; backend, API, repository, migration, handler, agent client, generated manifests, documentation, and live PostgreSQL verification completed.
- Upstream base: `214d7fe68b54a6eea615dbd7e10193e2372e4d9c`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=agent_capabilities` for the agent capability report endpoint.
- User-facing behavior: Feature-flagged agents can advertise protocol, runtime, tooling, strategy, and action-version support. PR-022 agents initially report no execution action support. Deployment Plan resolution blocks reported targets that cannot support included target-executed action steps.
- Database changes: Added `AgentCapabilityReport` and `AgentActionCapability` with organization-scoped deployment-target references, one current report per target, and atomic action capability replacement on upsert.
- API changes: Added hidden agent-authenticated `POST /api/v1/agents/{id}/capabilities`.
- UI changes: None. No Angular route, sidebar entry, or page is added in PR-022.
- Agent protocol changes: Docker and Kubernetes agents can post protocol `v1` capability reports once per process/config cycle. Missing, disabled, or absent capability endpoints are treated as no-op for compatibility.
- Documentation: Added PR-022 notes and ADR-0022.
- Tests: API validation, agent client, handler, live PostgreSQL repository, Deployment Plan compatibility, migration checks, and focused Go tests were added.
- Upstream contribution notes: Community-neutral capability protocol; no adopter-specific terminology, provider logic, or execution behavior.
- Compatibility notes: Existing Environment, Lifecycle, Channel, Release Bundle, Deployment Process, Process Snapshot, Variable Snapshot, Deployment Plan preview/UI, Task Queue, locks/concurrency, deployment target, deployment, release-name, and frontend planning UI behavior is unchanged except for blockers when an agent has explicitly reported incompatible target-executed action support. No leases, heartbeats, task claims, task completion, execution adapters, approvals, guided failure, timelines, logs, or PR-023 behavior is added in PR-022.

### PR-023 - Agent task leases

- Status: Implemented locally; backend, API, repository, migration, handler, agent client, generated manifests, documentation, and live PostgreSQL verification completed.
- Upstream base: `a56e5c9661c1469f20a1d58978120228bf3921c3`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=task_queue,agent_task_leases` for hidden agent lease endpoints.
- User-facing behavior: Feature-flagged agents can claim the next queued target-executed Task for their deployment target and heartbeat an active lease with an opaque token. No work returns `204 No Content`.
- Database changes: Added `TaskLease`, a Task target/org uniqueness constraint for composite lease references, hashed lease token storage, lease attempt tracking, and a one-active-lease partial unique index.
- API changes: Added hidden agent-authenticated `POST /api/v1/agents/{id}/lease` and `POST /api/v1/agents/{id}/tasks/{taskId}/heartbeat`.
- UI changes: None. No Angular route, sidebar entry, or page is added in PR-023.
- Agent protocol changes: Docker and Kubernetes manifests include optional lease and heartbeat endpoint variables. Agent client helpers can claim and heartbeat leases but existing agent loops do not execute leased Tasks.
- Documentation: Added PR-023 notes and ADR-0023.
- Tests: API validation, agent client, manifest, handler, live PostgreSQL repository, migration checks, expired-lease reclaim, and feature-flag tests were added.
- Upstream contribution notes: Community-neutral lease and heartbeat protocol; no adopter-specific terminology, provider logic, or execution behavior.
- Compatibility notes: Existing Environment, Lifecycle, Channel, Release Bundle, Deployment Process, Process Snapshot, Variable Snapshot, Deployment Plan preview/UI, Task Queue create/read APIs, locks/concurrency semantics, deployment target, deployment, release-name, frontend planning UI, and current agent deployment behavior are unchanged. No step events, logs, task completion, execution adapters, approvals, guided failure, timelines, or PR-024 behavior is added in PR-023.

### PR-024 - Structured step events and logs

- Status: Implemented locally; backend, API, repository, migration, handler, agent client, generated manifests, documentation, live PostgreSQL verification, lint, tests, and builds completed.
- Upstream base: `d8fdb6bbb0273c597c0f66404d6899a8f8f40e53`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=task_queue,agent_task_leases,step_events` for hidden agent ingestion; task timeline/log reads also require the Task Queue prerequisite flags.
- User-facing behavior: Feature-flagged API callers can read a Task's structured StepRun event timeline and redacted StepRun logs.
- Database changes: Added `StepRunEvent`, `StepRunLogChunk`, and `StepRunOutput` with organization-scoped composite references to Task, StepRun, TaskLease, and deployment target agent, ordered per-step/per-lease sequences, canonical replay payload hashes, immutable per-event outputs, bounded log/output storage, and idempotent replay uniqueness.
- API changes: Added hidden agent-authenticated `POST /api/v1/agents/{id}/step-runs/{stepRunId}/events` plus `GET /api/v1/tasks/{taskId}/timeline` and `GET /api/v1/tasks/{taskId}/logs`.
- UI changes: None. No Angular route, sidebar entry, or page is added in PR-024.
- Agent protocol changes: Docker and Kubernetes manifests include optional `DISTR_STEP_EVENT_ENDPOINT_TEMPLATE`. Agent client helpers can post structured step events but existing agent loops do not execute leased Tasks or emit execution events in PR-024.
- Documentation: Added PR-024 notes and ADR-0024.
- Tests: API validation, redaction, mapping, feature-flag, manifest, agent client, handler, repository, lifecycle transition, canonical idempotent replay, immutable output history, lease-attempt ordering, output bound enforcement, organization isolation, expired lease, and migration checks were added. Focused Go tests, `go test -p=1 ./...`, live PostgreSQL repository tests, Angular tests with watch disabled, migration-pair validation, touched-file Prettier checks, diff-scoped Go lint, community frontend build, community Hub build, Docker agent build, Kubernetes agent build, and changed-file Unicode scan passed locally.
- Upstream contribution notes: Community-neutral structured step event/log protocol; no adopter-specific terminology, provider logic, or execution behavior.
- Compatibility notes: Existing Environment, Lifecycle, Channel, Release Bundle, Deployment Process, Process Snapshot, Variable Snapshot, Deployment Plan preview/UI, Task Queue create/read APIs, locks/concurrency semantics, deployment target, deployment, release-name, frontend planning UI, and current deployment behavior are unchanged. No action execution adapters, Compose/OCI/file/webhook adapters, release promotion execution, task cancellation, approvals, guided failure, retention, notifications, Angular task timeline UI, or PR-025 behavior is added in PR-024.

### PR-025 - Compose deployment action adapter

- Status: Implemented locally; action registry, Docker agent capability report, task lease execution path, Compose action adapter, local task-sourced deployment state, documentation, focused tests, and ADR completed.
- Upstream base: `29fcd992`
- Feature flag: Adds no new flag. End-to-end execution uses existing `agent_capabilities`, `task_queue`, `agent_task_leases`, and `step_events` feature-flagged endpoints.
- User-facing behavior: Target-executed Tasks can run `distr.compose.deploy` on Docker agents that advertise action version `1`; the agent emits structured started/progress/succeeded/failed step events and non-secret outputs for project, strategy, status, and local state.
- Database changes: None. Reuses AgentCapabilityReport, AgentActionCapability, Task, StepRun, TaskLease, StepRunEvent, StepRunLogChunk, and StepRunOutput from earlier roadmap PRs.
- API changes: None. Reuses hidden capability, lease, heartbeat, and step-event endpoints.
- UI changes: None. No Angular route, sidebar entry, or page is added in PR-025.
- Agent protocol changes: Docker agents advertise `distr.compose.deploy` version `1`, claim a task lease before the legacy resource-poll path, heartbeat before and during step execution, wrap the existing Compose or Swarm apply path, and mark task-created local deployment state with `source: "task"` so legacy cleanup skips it.
- Documentation: Added PR-025 notes and ADR-0025.
- Tests: Action registry schema/order tests, Docker capability tests, Compose action input/execution event tests, task lease heartbeat/execution tests, and cleanup compatibility tests were added. Focused `go test ./internal/actionregistry` and Docker-agent tests with dummy agent endpoint environment values passed locally.
- Upstream contribution notes: Community-neutral Docker Compose action adapter; no adopter-specific terminology, no OCI/file/webhook behavior, and no new provider-specific core dependency beyond the existing Docker agent Compose dependencies.
- Compatibility notes: Existing Environment, Lifecycle, Channel, Release Bundle, Deployment Process, Process Snapshot, Variable Snapshot, Deployment Plan preview/UI, Task Queue create/read APIs, locks/concurrency semantics, deployment target, deployment, release-name, frontend planning UI, Kubernetes agent behavior, and legacy Docker resource-poll deployment behavior are unchanged when no task lease is claimed. No OCI job action, file render action, webhook action, Helm typed action, approvals, guided failure, retention, notifications, Angular task timeline UI, or PR-026 behavior is added in PR-025.

### PR-026 - OCI one-shot job action

- Status: Implemented locally; action registry, Docker agent capability report, OCI job adapter, lease-time secret resolution, StepRun redaction, documentation, focused tests, and ADR completed.
- Upstream base: `a566573e`
- Feature flag: Adds no new flag. End-to-end execution uses existing `agent_capabilities`, `task_queue`, `agent_task_leases`, and `step_events` feature-flagged endpoints.
- User-facing behavior: Target-executed Tasks can run `distr.oci.job` on Docker agents that advertise action version `1`; the agent emits structured started/progress/succeeded/failed step events and non-secret outputs for container name, exit code, and status.
- Database changes: None. Reuses AgentCapabilityReport, AgentActionCapability, Task, StepRun, TaskLease, StepRunEvent, StepRunLogChunk, StepRunOutput, and Secret from earlier roadmap PRs.
- API changes: None. Reuses hidden capability, lease, heartbeat, and step-event endpoints.
- UI changes: None. No Angular route, sidebar entry, or page is added in PR-026.
- Agent protocol changes: Docker agents advertise `distr.oci.job` version `1`, claim a task lease through the existing lease flow, heartbeat before and during job execution, enforce trusted `DISTR_OCI_JOB_ALLOWED_*` policy, run digest-only allowlisted OCI images with canonicalized read-only host mounts, disabled Docker log retention, and non-persistent mounted secret env files staged under configured `DISTR_OCI_JOB_SECRET_STAGING_DIR` through Docker, stop containers on timeout/cancellation, and reuse deterministic containers only when trusted operation labels match for retry/reclaim/restart idempotency without replaying retained raw logs.
- Documentation: Added PR-026 notes and ADR-0026.
- Tests: Action registry schema/order tests, Docker capability tests, OCI action validation/execution/redaction/expected-exit/idempotency/restart/collision/timeout/cancellation/bounded-output tests, task lease OCI secret resolution tests, and StepRun OCI redaction tests were added. Focused `go test ./internal/actionregistry`, Docker-agent tests with dummy agent endpoint environment values, and focused live PostgreSQL `internal/db` tests passed locally.
- Upstream contribution notes: Community-neutral OCI one-shot action adapter; no adopter-specific terminology, no file/webhook behavior, no UI workflow, and no arbitrary host shell execution.
- Compatibility notes: Existing Environment, Lifecycle, Channel, Release Bundle, Deployment Process, Process Snapshot, Variable Snapshot, Deployment Plan preview/UI, Task Queue create/read APIs, locks/concurrency semantics, deployment target, deployment, release-name, frontend planning UI, Kubernetes agent behavior, Compose action behavior, and legacy Docker resource-poll deployment behavior are unchanged when no OCI job task lease is claimed. No file render action, webhook action, Helm typed action, approvals, guided failure, retention, notifications, Angular task timeline UI, or PR-027 behavior is added in PR-026.

### PR-027 - File render action

- Status: Implemented locally; action registry, Docker agent capability report, file render adapter, lease-time secret resolution, StepRun redaction, documentation, focused tests, and ADR completed.
- Upstream base: `b496dc84`
- Feature flag: Adds no new flag. End-to-end execution uses existing `agent_capabilities`, `task_queue`, `agent_task_leases`, and `step_events` feature-flagged endpoints.
- User-facing behavior: Target-executed Tasks can run `distr.file.render` on Docker agents that advertise action version `1`; the agent emits structured started/progress/succeeded/failed step events and non-secret outputs for destination path, changed state, and optional backup path.
- Database changes: None. Reuses AgentCapabilityReport, AgentActionCapability, Task, StepRun, TaskLease, StepRunEvent, StepRunLogChunk, StepRunOutput, and Secret from earlier roadmap PRs.
- API changes: None. Reuses hidden capability, lease, heartbeat, and step-event endpoints.
- UI changes: None. No Angular route, sidebar entry, or page is added in PR-027.
- Agent protocol changes: Docker agents advertise `distr.file.render` version `1`, claim a task lease through the existing lease flow, heartbeat before and during file render execution, enforce trusted `DISTR_FILE_RENDER_ALLOWED_ROOTS` policy, render only scoped `${name}` and `${secrets.name}` placeholders, canonicalize relative destinations under an `os.Root` handle for the configured root, reject traversal, symlink escapes, and destination swaps, back up existing regular files from opened descriptors using the more restrictive of existing and target modes, write atomically through same-directory private `0600` temporary files and root-relative rename, apply final mode/owner/group to private temp descriptors before rename, default secret-rendered files to `0600` when mode is omitted, no-op only when desired bytes and metadata already match, and use atomic replacement for equal-content metadata changes.
- Documentation: Added PR-027 notes and ADR-0027.
- Tests: Action registry schema/order tests, Docker capability tests, file render validation/write/backup/mode/private-temp/tamper/equal-content-metadata-rollback/backup-non-widening/redaction/idempotency/symlink/path-swap/destination-swap/cancellation/task-lease dispatch tests, task lease file-render secret resolution tests, and Hub-side StepRun file-render secret redaction tests were added. Focused `go test ./internal/actionregistry`, Docker-agent tests with dummy agent endpoint environment values, and focused live PostgreSQL `internal/db` tests passed locally.
- Upstream contribution notes: Community-neutral file render action adapter; no adopter-specific terminology, no webhook/UI behavior, and no arbitrary host shell execution.
- Compatibility notes: Existing Environment, Lifecycle, Channel, Release Bundle, Deployment Process, Process Snapshot, Variable Snapshot, Deployment Plan preview/UI, Task Queue create/read APIs, locks/concurrency semantics, deployment target, deployment, release-name, frontend planning UI, Kubernetes agent behavior, Compose action behavior, OCI job behavior, and legacy Docker resource-poll deployment behavior are unchanged when no file render task lease is claimed. No webhook action, Helm typed action, approvals, guided failure, retention, notifications, Angular task timeline UI, or PR-028 behavior is added in PR-027.

### PR-028 - Webhook action

- Status: Implemented locally; action registry, Docker agent capability report, webhook adapter, lease-time secret resolution, StepRun redaction, documentation, focused tests, and ADR completed.
- Upstream base: `4826c357`
- Feature flag: Adds no new flag. End-to-end execution uses existing `agent_capabilities`, `task_queue`, `agent_task_leases`, and `step_events` feature-flagged endpoints.
- User-facing behavior: Target-executed Tasks can run `distr.webhook` on Docker agents that advertise action version `1`; the agent emits structured started/progress/succeeded/failed step events and non-secret outputs for status code, attempts, and declared response values.
- Database changes: None. Reuses AgentCapabilityReport, AgentActionCapability, Task, StepRun, TaskLease, StepRunEvent, StepRunLogChunk, StepRunOutput, and Secret from earlier roadmap PRs.
- API changes: None. Reuses hidden capability, lease, heartbeat, and step-event endpoints.
- UI changes: None. No Angular route, sidebar entry, or page is added in PR-028.
- Agent protocol changes: Docker agents advertise `distr.webhook` version `1`, claim a task lease through the existing lease flow, heartbeat before and during webhook execution, enforce trusted `DISTR_WEBHOOK_ALLOWED_HOSTS` and `DISTR_WEBHOOK_ALLOWED_PRIVATE_HOSTS` policy, reject non-HTTPS URLs and URL credentials, disable redirects, sign bounded JSON requests with timestamp/body-digest/signature/idempotency headers, retry only configured transient failures, preserve the same idempotency key across attempts and lease reclaim, and emit only declared bounded outputs.
- Documentation: Added PR-028 notes and ADR-0028.
- Tests: Action registry schema/order tests, Docker capability tests, webhook validation/signing/retry/idempotency/output/redaction/task-lease dispatch tests, task lease webhook secret resolution tests, and Hub-side StepRun webhook secret redaction tests were added. Focused `go test ./internal/actionregistry`, Docker-agent tests with dummy agent endpoint environment values, and focused live PostgreSQL `internal/db` tests passed locally.
- Upstream contribution notes: Community-neutral webhook action adapter; no adopter-specific terminology, no UI behavior, no arbitrary host shell execution, and no generic plugin runner.
- Compatibility notes: Existing Environment, Lifecycle, Channel, Release Bundle, Deployment Process, Process Snapshot, Variable Snapshot, Deployment Plan preview/UI, Task Queue create/read APIs, locks/concurrency semantics, deployment target, deployment, release-name, frontend planning UI, Kubernetes agent behavior, Compose action behavior, OCI job behavior, file render behavior, and legacy Docker resource-poll deployment behavior are unchanged when no webhook task lease is claimed. No Helm typed action, approvals, guided failure, retention, notifications, Angular task timeline UI, or PR-029 behavior is added in PR-028.

### PR-029 - Webhook network hardening

- Status: Implemented locally; DNS preflight validation, pinned dialing, redirect target DNS validation, proxy environment regression coverage, TLS verification coverage, retry classification tests, documentation, and ADR completed.
- Upstream base: `18c6f32b`
- Feature flag: Adds no new flag. Hardens the existing `distr.webhook` Docker-agent execution path introduced in PR-028.
- User-facing behavior: Webhook actions keep the same inputs and outputs, but unsafe DNS results now fail before any webhook attempt is emitted or HTTP transport is used. Default TLS certificate verification remains enforced.
- Database changes: None.
- API changes: None.
- UI changes: None. No Angular route, sidebar entry, or page is added in PR-029.
- Agent protocol changes: The Docker agent pre-resolves webhook hostnames through the default resolver, validates every resolved IP with the existing unsafe-IP policy, pins the validated address for the outbound dial, revalidates redirect targets including DNS even though redirects are not followed, ignores `HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY`, and treats DNS, validation, TLS, and oversized-body failures as non-retryable.
- Documentation: Added PR-029 notes and ADR-0029.
- Tests: Added Docker-agent tests for unsafe DNS preflight before attempts, pinned DNS dialing, redirect target DNS validation, explicit proxy environment bypass, untrusted TLS certificate rejection, nil body digest equivalence, and retry classification. Docker-agent focused tests passed locally with dummy agent endpoint environment values.
- Upstream contribution notes: Community-neutral webhook hardening; no adopter-specific terminology, no UI behavior, no arbitrary host shell execution, and no generic plugin runner.
- Compatibility notes: Existing Environment, Lifecycle, Channel, Release Bundle, Deployment Process, Process Snapshot, Variable Snapshot, Deployment Plan preview/UI, Task Queue create/read APIs, locks/concurrency semantics, deployment target, deployment, release-name, frontend planning UI, Kubernetes agent behavior, Compose action behavior, OCI job behavior, file render behavior, webhook request/response schema, and legacy Docker resource-poll deployment behavior are unchanged. No approvals, guided failure, retention, notifications, Angular task timeline UI, or PR-030 behavior is added in PR-029.

### PR-030 - Webhook runtime isolation

- Status: Implemented locally; direct-run deadline propagation, global retry ceiling enforcement, transport runtime limits, bounded response streaming coverage, cancellation coverage, non-blocking metrics hook, documentation, and ADR completed.
- Upstream base: `60aaebc3`
- Feature flag: Adds no new flag. Hardens the existing `distr.webhook` Docker-agent execution path introduced in PR-028 and network-hardened in PR-029.
- User-facing behavior: Webhook actions keep the same inputs and outputs, but execution is now bounded across DNS resolution, connect, TLS handshake, response headers, response body streaming, retry loops, and retry backoff.
- Database changes: None.
- API changes: None.
- UI changes: None. No Angular route, sidebar entry, or page is added in PR-030.
- Agent protocol changes: The Docker agent derives a webhook run context from `timeoutSeconds`, clamps retry attempts to the global ceiling even for direct internal callers, applies fixed connect/TLS/response-header/max-header-byte transport limits, preserves the existing response body cap through streaming reads, propagates cancellation through HTTP requests and retry backoff, and emits best-effort non-blocking in-process attempt metrics.
- Documentation: Added PR-030 notes and ADR-0030.
- Tests: Added Docker-agent tests for direct-run deadline propagation, global retry ceiling enforcement, transport runtime limits, non-blocking attempt metrics, cancellation during retry backoff, response streaming cutoff, and the expanded webhook security contract suite. Docker-agent focused tests passed locally with dummy agent endpoint environment values.
- Upstream contribution notes: Community-neutral webhook runtime hardening; no adopter-specific terminology, no UI behavior, no arbitrary host shell execution, and no generic plugin runner.
- Compatibility notes: Existing Environment, Lifecycle, Channel, Release Bundle, Deployment Process, Process Snapshot, Variable Snapshot, Deployment Plan preview/UI, Task Queue create/read APIs, locks/concurrency semantics, deployment target, deployment, release-name, frontend planning UI, Kubernetes agent behavior, Compose action behavior, OCI job behavior, file render behavior, webhook request/response schema, and legacy Docker resource-poll deployment behavior are unchanged. No approvals, guided failure, retention, notifications, Angular task timeline UI, or PR-031 behavior is added in PR-030.

### PR-031 - Webhook idempotent replay

- Status: Implemented locally; hidden agent task timeline read, optional agent client helper, manifest endpoint wiring, webhook replay preflight, deterministic success-output reconstruction, interrupted replay fail-closed behavior, documentation, and ADR completed.
- Upstream base: `543b8e08`
- Feature flag: Adds no new flag. Uses the existing `task_queue`, `agent_task_leases`, and `step_events` gated agent protocol.
- User-facing behavior: Webhook actions keep the same inputs and outputs, but re-entering a step with stored success no longer emits new events or sends another external HTTP request. Re-entering a step with incomplete stored webhook history fails closed before any new external side effect.
- Database changes: None. Reuses StepRunEvent and StepRunOutput from PR-024.
- API changes: Added hidden agent-authenticated `GET /api/v1/agents/{id}/tasks/{taskId}/timeline`, scoped to the authenticated deployment target.
- UI changes: None. No Angular route, sidebar entry, or page is added in PR-031.
- Agent protocol changes: Docker and Kubernetes manifests include optional `DISTR_TASK_TIMELINE_ENDPOINT_TEMPLATE`. Docker agents use the task timeline before webhook execution to suppress replayed successes and refuse incomplete replays before DNS, signing, transport setup, or HTTP requests.
- Documentation: Added PR-031 notes and ADR-0031.
- Tests: Added agent client timeline tests, manifest endpoint tests, and `TestWebhookActionIdempotentReplaySuite` for duplicate execution, zero-network replay, interrupted replay fail-closed behavior, and stored output reconstruction. Docker-agent, agentclient, agentmanifest, and handler tests passed locally.
- Upstream contribution notes: Community-neutral webhook replay hardening; no adopter-specific terminology, no UI behavior, no arbitrary host shell execution, and no generic plugin runner.
- Compatibility notes: Existing Environment, Lifecycle, Channel, Release Bundle, Deployment Process, Process Snapshot, Variable Snapshot, Deployment Plan preview/UI, Task Queue create/read APIs, locks/concurrency semantics, deployment target, deployment, release-name, frontend planning UI, Kubernetes agent behavior, Compose action behavior, OCI job behavior, file render behavior, webhook request/response schema, and legacy Docker resource-poll deployment behavior are unchanged. Older manifests without `DISTR_TASK_TIMELINE_ENDPOINT_TEMPLATE` continue to run without replay preflight until refreshed. No approvals, guided failure, retention, notifications, Angular task timeline UI, or PR-032 behavior is added in PR-031.

### PR-032 - Webhook key rotation

- Status: Implemented locally; multi-key signing, versioned request metadata, audit outputs, verification helper, lease-time rotated secret resolution, redaction support, documentation, and ADR completed.
- Upstream base: `910bfcc4`
- Feature flag: Adds no new flag. Extends the existing `distr.webhook` Docker-agent action introduced in PR-028.
- User-facing behavior: Webhook actions may now use legacy `signingSecret` or ordered `signingSecrets`; rotated-key executions sign with the latest key and emit non-secret signing key audit outputs.
- Database changes: None. Reuses existing Secret, TaskLease, StepRunEvent, and StepRunOutput storage.
- API changes: None. The action registry contract for `distr.webhook` accepts `signingSecrets` and reserves the new built-in audit outputs.
- UI changes: None. No Angular route, sidebar entry, or page is added in PR-032.
- Agent protocol changes: Docker agents include `X-Distr-Key-Version` on webhook attempts, record `signingKeyVersion` and `keyRotationApplied` outputs, reject ambiguous or invalid key rotation configuration, and can verify signatures against active or previous keys without accepting mismatched version headers.
- Documentation: Added PR-032 notes and ADR-0032.
- Tests: Added Docker-agent key rotation signing, validation, verification, audit-output, and redaction tests; action registry rotation schema tests; and task lease rotated secret resolution coverage.
- Upstream contribution notes: Community-neutral webhook key lifecycle support; no adopter-specific terminology, no UI behavior, no arbitrary host shell execution, and no generic plugin runner.
- Compatibility notes: Existing single-secret webhook inputs remain valid and map to signing key version 1. PR-031 stored success events without key-version outputs remain replayable. Existing Environment, Lifecycle, Channel, Release Bundle, Deployment Process, Process Snapshot, Variable Snapshot, Deployment Plan preview/UI, Task Queue create/read APIs, locks/concurrency semantics, deployment target, deployment, release-name, frontend planning UI, Kubernetes agent behavior, Compose action behavior, OCI job behavior, file render behavior, and legacy Docker resource-poll deployment behavior are unchanged.

### PR-033 - Webhook tenant isolation

- Status: Implemented locally; lease context propagation, lease-scoped hidden timeline read, organization metadata mapping, tenant-bound signing metadata, replay boundary checks, documentation, and ADR completed.
- Upstream base: `f4d20c0e`
- Feature flag: Adds no new flag. Hardens the existing `distr.webhook` Docker-agent action and PR-031 hidden replay timeline endpoint.
- User-facing behavior: Webhook actions keep the same inputs and outputs, but replayed stored success history is only trusted when it matches the active organization, task lease, and authenticated agent. Outbound signed webhook requests now include `X-Distr-Tenant-ID`.
- Database changes: None. Reuses existing organization, lease, agent, TaskLease, StepRunEvent, StepRunLogChunk, and StepRunOutput fields.
- API changes: Hidden agent task lease responses include `organizationId` and `agentId`. Hidden agent task timeline reads require `leaseId` and return organization metadata on timeline/event/log/output payloads. Public task timeline behavior is unchanged.
- UI changes: None. No Angular route, sidebar entry, or page is added in PR-033.
- Agent protocol changes: Docker agents pass the active task lease id when reading stored timeline history, bind webhook HMAC canonical data to the lease organization id, send `X-Distr-Tenant-ID`, and reject replay history with mismatched organization or agent identity before DNS, signing, transport setup, or HTTP requests.
- Documentation: Added PR-033 notes and ADR-0033.
- Tests: Added Docker-agent tenant-bound signature and replay boundary tests plus agent client `leaseId` timeline coverage.
- Upstream contribution notes: Community-neutral webhook authorization-boundary hardening; no adopter-specific terminology, no UI behavior, no arbitrary host shell execution, and no generic plugin runner.
- Compatibility notes: Existing webhook input shape and output names are unchanged. Existing public task timeline reads remain unchanged. Older agent clients without `leaseId` cannot use the hidden agent replay timeline until refreshed, but agents without `DISTR_TASK_TIMELINE_ENDPOINT_TEMPLATE` continue to run without replay preflight as before.

### PR-034 - Webhook audit trail

- Status: Implemented locally; deterministic audit export, chained audit hashes, strict replay verification, redaction-boundary tests, action registry schema updates, documentation, and ADR completed.
- Upstream base: `c5f7f13f`
- Feature flag: Adds no new flag. Strict stored-history replay verification is controlled by `STRICT_REPLAY_VERIFY=true`.
- User-facing behavior: Webhook actions keep the same configured inputs and declared outputs, but successful executions now emit reserved non-secret audit outputs for forensic replay integrity.
- Database changes: None. Reuses existing StepRunEvent and StepRunOutput storage.
- API changes: None. The action registry output schema now includes reserved `auditChainRoot`, `auditEventHash`, and `auditTrail` outputs for `distr.webhook`.
- UI changes: None. No Angular route, sidebar entry, or page is added in PR-034.
- Agent protocol changes: Docker agents record deterministic webhook audit events for target resolution, each HTTP attempt, and final completion. Replay verifies stored audit chains when present and fails closed for missing audit outputs when `STRICT_REPLAY_VERIFY=true`.
- Documentation: Added PR-034 notes and ADR-0034.
- Tests: Added `TestWebhookActionAuditTrailIntegritySuite` plus reserved audit-output validation coverage.
- Upstream contribution notes: Community-neutral webhook observability and replay-integrity hardening; no adopter-specific terminology, no UI behavior, no arbitrary host shell execution, and no generic plugin runner.
- Compatibility notes: Existing webhook inputs remain valid. Existing PR-031/PR-032 stored successes without audit outputs remain replayable unless strict replay verification is explicitly enabled.

### PR-035 - Webhook self-contained runtime

- Status: Implemented locally; self-contained runtime flag, cached-only resolution, local timeline replay path, duplicate-step local replay coverage, documentation, and ADR completed.
- Upstream base: `164df4a7`
- Feature flag: `WEBHOOK_SELF_CONTAINED_MODE=true` enables self-contained runtime behavior. `DISTR_WEBHOOK_RESOLVED_IP_CACHE` supplies cached host-to-IP mappings for non-IP webhook targets.
- User-facing behavior: Webhook actions keep the same configured inputs and outputs. When self-contained mode is enabled, webhook execution does not call the hidden task timeline endpoint and does not perform live DNS lookup for non-IP hosts.
- Database changes: None. This PR does not introduce a Docker-agent local database.
- API changes: None. Existing hidden timeline APIs remain available for normal mode.
- UI changes: None. No Angular route, sidebar entry, or page is added in PR-035.
- Agent protocol changes: Docker agents can opt into self-contained mode, replay from a local in-process event mirror during a lease execution, and fail closed when cached resolution is missing for a non-IP webhook host. The final configured outbound webhook HTTP request remains unchanged.
- Documentation: Added PR-035 notes and ADR-0035.
- Tests: Added `TestWebhookActionSelfContainedRuntimeSuite` plus focused regression coverage for existing replay, audit, validation, and heartbeat paths.
- Upstream contribution notes: Community-neutral webhook autonomy hardening; no adopter-specific terminology, no UI behavior, no arbitrary host shell execution, and no generic plugin runner.
- Compatibility notes: Existing agents remain on normal timeline and DNS behavior unless `WEBHOOK_SELF_CONTAINED_MODE=true` is configured. Self-contained replay is in-process for this PR and does not persist across agent restarts.

### PR-036 - Webhook policy engine

- Status: Implemented locally; policy package, tenant/agent/corridor limits, circuit breaker, retry-storm and endpoint-failure controls, replay enforcement, documentation, and ADR completed.
- Upstream base: `7579d8d8`
- Feature flag: Adds no single feature flag. Policy controls are enabled by setting `DISTR_WEBHOOK_TENANT_RPS`, `DISTR_WEBHOOK_AGENT_RPS`, `DISTR_WEBHOOK_AGENT_CONCURRENCY`, `DISTR_WEBHOOK_CORRIDOR_RPS`, `DISTR_WEBHOOK_OPEN_CIRCUIT_HOSTS`, `DISTR_WEBHOOK_MAX_RETRY_ATTEMPTS`, or `DISTR_WEBHOOK_ENDPOINT_FAILURE_LIMIT`.
- User-facing behavior: Webhook actions keep existing required inputs and outputs. Optional `corridor` and `priority` metadata can now be supplied for policy evaluation.
- Database changes: None. Policy state is in-process for this PR.
- API changes: None. The action registry input schema accepts optional `corridor` and `priority` fields for `distr.webhook`.
- UI changes: None. No Angular route, sidebar entry, or page is added in PR-036.
- Agent protocol changes: Docker agents evaluate policy after replay validation and before DNS, signing, or outbound HTTP. Replay must also pass policy before returning stored success.
- Documentation: Added PR-036 notes and ADR-0036.
- Tests: Added `TestWebhookActionPolicyEngineSuite`, `internal/policy` unit tests, and action registry policy-field validation coverage.
- Upstream contribution notes: Community-neutral webhook governance hardening; no adopter-specific terminology, no UI behavior, no arbitrary host shell execution, and no generic plugin runner.
- Compatibility notes: With no policy env vars configured, existing webhook behavior is allowed. Configured policy limits fail closed on invalid values and deny before network/signing work.

### PR-037 - Step Template import UI

- Status: Implemented locally; schema, repository, API, Angular import UI, route/sidebar integration, documentation, and ADR completed.
- Upstream base: `330d8939`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=step_templates`; the UI route/sidebar also require `environments`, `lifecycles`, `channels`, and `deployment_processes`.
- User-facing behavior: Vendor admins can view a built-in Step Template catalog, preview default input bindings, install catalog templates into their organization, and list installed template versions.
- Database changes: Added organization-scoped `StepTemplate` and `StepTemplateVersion` tables with unique source installs per organization and version uniqueness per template.
- API changes: Added feature-flagged `GET /api/v1/step-templates`, `GET /api/v1/step-templates/{stepTemplateId}`, and `POST /api/v1/step-templates/import`.
- UI changes: Added Step Templates route, sidebar link, Angular service/types, catalog table, preview dialog, import action, installed-template table, and error/loading states.
- Agent protocol changes: None.
- Documentation: Added PR-037 notes and ADR-0037.
- Tests: Repository, migration, handler, integration, Angular service, and Angular component tests were added.
- Upstream contribution notes: Community-neutral Step Template catalog/install surface; no adopter-specific terminology, external marketplace dependency, or execution behavior.
- Compatibility notes: Existing Deployment Process, Task Queue, action execution, Docker/Kubernetes agents, and webhook behavior are unchanged. Installed templates are additive and gated behind the `step_templates` experimental flag.

### PR-038 - Output variables and conditions

- Status: Implemented locally; restricted condition package, deployment process validation, output-reference cycle checks, output-name validation, deployment-plan blocker handling, documentation, and ADR completed.
- Upstream base: `2a21d0c1`
- Feature flag: Uses the existing `deployment_processes` and step-event surfaces; no new feature flag is added.
- User-facing behavior: Invalid process step conditions are rejected during revision validation, output references must target known steps, and stable output names are enforced for step event writes.
- Database changes: None. Reuses DeploymentProcessStep condition fields and StepRunOutput from earlier roadmap PRs.
- API changes: Tightened validation for deployment process revision `condition` fields and agent step event output names.
- UI changes: None.
- Agent protocol changes: None.
- Documentation: Added PR-038 notes and ADR-0038.
- Tests: Condition parser/evaluator tests, deployment process validation tests, step-event output validation tests, and deployment-plan invalid-condition blocker coverage were added.
- Upstream contribution notes: Community-neutral condition/output foundation; no adopter-specific terminology, script execution, or general-purpose expression language.
- Compatibility notes: Existing valid conditions and output names continue to work. Invalid free-form conditions and unstable output names are rejected before they can become workflow references.

### PR-039 - Runbook model

- Status: Implemented locally; backend model, API, repository, migration, canonical snapshots, task type discriminator, documentation, and ADR completed.
- Upstream base: `72219691`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=runbooks`.
- User-facing behavior: Feature-flagged API callers can create, list, read, update, and delete Runbooks; create and read immutable revisions; and publish a revision snapshot.
- Database changes: Added `Runbook`, `RunbookRevision`, `RunbookStep`, `RunbookStepDependency`, and `RunbookSnapshot` tables. Added defaulted `Task.task_type` constrained to `deployment` or `runbook`.
- API changes: Added feature-flagged endpoints under `/api/v1/runbooks` for CRUD, revision list/create/read, and revision publication.
- UI changes: None. Runbook UI and schedules remain PR-040.
- Agent protocol changes: None. Existing deployment task creation writes `deployment`; `runbook` task execution is reserved for future PRs.
- Documentation: Added PR-039 notes and ADR-0039.
- Tests: API validation, mapping, handler, repository, canonical snapshot, migration, and deployment task default tests were added.
- Upstream contribution notes: Community-neutral runbook foundation; no adopter-specific terminology, scheduler, script runner, or external workflow engine.
- Compatibility notes: Existing Deployment Process, Task Queue, task lease, step event, Docker/Kubernetes agent, release bundle, and deployment behavior is unchanged. No runbook UI, schedule, execution, approval, guided failure, retention, notification, or agent behavior is added in PR-039.

### PR-040 - Runbook UI and schedules

- Status: Implemented locally; feature-flagged route/sidebar, typed Angular runbook service, editor, revision publish controls, read-only history/schedule surfaces, documentation, and ADR completed.
- Upstream base: `e6699c11`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=runbooks`.
- User-facing behavior: Vendor administrators can open the Runbooks page, list and filter runbooks, create/update/delete runbook metadata, create structured revisions, view revisions, and publish revisions. History and schedules are visible as non-submitting surfaces until execution and scheduler APIs exist.
- Database changes: None.
- API changes: None. The UI consumes the PR-039 `/api/v1/runbooks` CRUD, revision, and publish endpoints.
- UI changes: Added the Runbooks route, sidebar entry, Angular service, typed frontend models, editor tab, history tab, schedules tab, and focused component/service tests.
- Agent protocol changes: None. Runbook run-now, scheduling, task lease, and agent execution behavior remains future scope.
- Documentation: Added PR-040 notes and ADR-0040.
- Tests: Runbook service, Runbooks component, and feature-flag frontend tests were added.
- Upstream contribution notes: Community-neutral UI shell over existing runbook APIs; no adopter-specific scheduling, runner, script execution, or external workflow engine assumptions.
- Compatibility notes: Existing Deployment Process, Task Queue, task lease, step event, Docker/Kubernetes agent, webhook, release bundle, and deployment behavior is unchanged.

### PR-041 - Rolling deployment strategy

- Status: Implemented locally; rolling deployment state-machine package, window selection, per-target states, failure-threshold decisions, documentation, and ADR completed.
- Upstream base: `93626b7d`
- Feature flag: None.
- User-facing behavior: None. PR-041 adds backend-only rolling semantics for later scheduler/task integration.
- Database changes: None.
- API changes: None.
- UI changes: None.
- Agent protocol changes: None. Existing deployment task lease and agent execution behavior is unchanged.
- Documentation: Added PR-041 notes and ADR-0041.
- Tests: Rolling state-machine tests cover target normalization, window limits, terminal-window advancement, pause/abort threshold actions, percentage thresholds, and invalid configuration rejection.
- Upstream contribution notes: Community-neutral rolling semantics; no adopter-specific deployment provider, traffic switch, task scheduler, or UI assumptions.
- Compatibility notes: Existing Deployment Process, Deployment Plan, Task Queue, task lease, Docker/Kubernetes agent, runbook, webhook, release bundle, and deployment behavior is unchanged.

### PR-042 - Traffic-provider interface

- Status: Implemented locally; traffic-provider contract, capability model, provider registry, webhook reference provider, documentation, and ADR completed.
- Upstream base: `9d0e81d9`
- Feature flag: None.
- User-facing behavior: None. PR-042 adds backend-only provider abstractions for later progressive-delivery controllers.
- Database changes: None.
- API changes: None.
- UI changes: None.
- Agent protocol changes: None.
- Documentation: Added PR-042 notes and ADR-0042.
- Tests: Traffic-provider tests cover registry construction, capability checks, webhook payloads, idempotency forwarding, prepare response decoding, non-success status handling, unsafe configuration rejection, duplicate provider rejection, and unknown provider rejection.
- Upstream contribution notes: Community-neutral traffic abstraction with webhook reference provider; no Envoy, Nginx, cloud load balancer, scheduler, task execution, or blue-green assumptions.
- Compatibility notes: Existing Deployment Process, Deployment Plan, Task Queue, task lease, rolling state machine, Docker/Kubernetes agent, runbook, webhook action, release bundle, and deployment behavior is unchanged.

### PR-043 - Blue-green strategy

- Status: Implemented locally; blue-green lifecycle package, slot states, health verification gating, traffic shift/rollback request planning, promotion, retention policy handling, documentation, and ADR completed.
- Upstream base: `d6d490c6`
- Feature flag: None.
- User-facing behavior: None. PR-043 adds backend-only lifecycle semantics for later orchestration.
- Database changes: None.
- API changes: None.
- UI changes: None.
- Agent protocol changes: None.
- Documentation: Added PR-043 notes and ADR-0043.
- Tests: Blue-green lifecycle tests cover slot initialization, invalid configuration rejection, health-check gating, shift request planning, promotion retention behavior, and rollback request planning.
- Upstream contribution notes: Community-neutral blue-green lifecycle semantics; no Envoy, Nginx, cloud load balancer, scheduler, task execution, provider invocation, or UI assumptions.
- Compatibility notes: Existing Deployment Process, Deployment Plan, Task Queue, task lease, rolling state machine, traffic-provider interface, Docker/Kubernetes agent, runbook, webhook action, release bundle, and deployment behavior is unchanged.

### PR-044 - Deployment timeline and compare

- Status: Implemented locally; timeline repository, task actor persistence, API, Angular UI, documentation, and ADR completed.
- Upstream base: `288c87b0`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=deployment_timeline`; the API and UI also require `task_queue`, `deployment_plans`, `release_bundles`, `deployment_processes`, `scoped_variables_v2`, `channels`, `lifecycles`, and `environments`.
- User-facing behavior: Vendor admins can open Deployment Timeline, filter deployment history, select two entries to compare, open task logs, see the last successful deployment per application/environment/target, and create a deployment plan for a previous release.
- Database changes: Added nullable `Task.actor_user_account_id` with an organization/actor index for timeline display.
- API changes: Added `GET /api/v1/deployment-timeline`, `GET /api/v1/deployment-timeline/compare`, and `POST /api/v1/deployment-timeline/{taskId}/redeploy`.
- UI changes: Added Deployment Timeline route, sidebar link, Angular service/types, timeline table, local filters, compare panel, task-log links, and deploy-previous-release confirmation.
- Agent protocol changes: None.
- Documentation: Added PR-044 notes and ADR-0044.
- Tests: Repository tests cover timeline filtering, last-successful marking, actor persistence, compare, and deploy-previous-release plan creation. Handler tests cover list, compare, and redeploy. Angular service and component tests cover requests, filtering, compare, and confirmation.
- Upstream contribution notes: Community-neutral timeline and comparison surface; no adopter-specific terminology, provider-specific orchestration, scheduler behavior, or rollback claim.
- Compatibility notes: Existing tasks remain valid with a null actor. Deploy previous release creates a new deployment plan and does not reverse external state or database changes. Existing deployment execution, task leases, rolling and blue-green primitives, traffic-provider interface, Docker/Kubernetes agents, runbooks, webhook action, and release-bundle behavior are unchanged.

### PR-045 - Retention policies

- Status: Implemented locally; repository, API, feature flag, documentation, and ADR completed.
- Upstream base: `50e862a0`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=retention_policies`; the API also requires `task_queue`, `release_bundles`, and `environments`.
- User-facing behavior: Feature-flagged API callers can create retention policies, preview release/task/log cleanup candidates, inspect safety blocks, and record dry-run cleanup jobs.
- Database changes: Added `RetentionPolicy`, `RetentionCleanupJob`, and `ReleaseBundle.retention_protected`.
- API changes: Added `/api/v1/retention-policies` list/create/get endpoints plus policy preview and dry-run cleanup-job endpoints.
- UI changes: No page is added in this PR. The frontend feature flag model recognizes `retention_policies` for future UI gating.
- Agent protocol changes: None.
- Documentation: Added PR-045 notes and ADR-0045.
- Tests: Repository tests cover release candidates, currently-deployed safety blocks, failed-task candidates, dry-run cleanup jobs, and apply rejection. Handler tests cover request normalization, malformed IDs, and feature-flag rejection.
- Upstream contribution notes: Community-neutral retention planning surface; no adopter-specific terminology, provider-specific cleanup worker, scheduler, or destructive deletion.
- Compatibility notes: Cleanup apply is explicitly rejected in this PR. Existing deployment execution, task leases, deployment timeline, rolling and blue-green primitives, traffic-provider interface, Docker/Kubernetes agents, runbooks, webhook action, and release-bundle behavior are unchanged.

### PR-046 - Expanded RBAC

- Status: Implemented locally; permission constants, built-in role definitions, organization-scoped permission middleware, documentation, and ADR completed.
- Upstream base: `a3bda802`
- Feature flag: None.
- User-facing behavior: None. Existing persisted role values and token claims remain unchanged.
- Database changes: None.
- API changes: No new endpoints. Existing mutation routes that use `RequireReadWriteOrAdmin` now pass through mutation permission checks.
- UI changes: None.
- Agent protocol changes: None.
- Documentation: Added PR-046 notes and ADR-0046.
- Tests: Type tests cover built-in role permissions, permission parsing, and isolated role-definition copies. Middleware tests cover organization-scoped permission allow/deny behavior, super-admin compatibility, and unsupported scope rejection.
- Upstream contribution notes: Community-neutral RBAC foundation; no adopter-specific policy language, external IAM dependency, or role migration.
- Compatibility notes: Organization scope is enforced first. Application, environment, tenant/customer, and tag-set scopes are known but unsupported until later PRs add policy bindings.

### PR-047a - Observability metrics

- Status: Implemented locally; metrics recorder abstraction, Prometheus recorder, HTTP middleware, task transition hooks, feature flag, documentation, and ADR completed.
- Upstream base: `0b6f7d53`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=observability_metrics`; the metrics server also requires `METRICS_ENABLED=true`.
- User-facing behavior: None. PR-047a exposes Prometheus metrics only when the feature flag and metrics server env var are enabled.
- Database changes: None.
- API changes: No API endpoints are added. The existing metrics server route exposes `/metrics` only when observability metrics are enabled.
- UI changes: No page is added. The frontend feature flag model recognizes `observability_metrics` for future UI gating.
- Agent protocol changes: None.
- Documentation: Added PR-047a notes and ADR-0047a.
- Tests: Metrics tests cover Prometheus output, base labels, HTTP counters/errors/latency, and task counters/duration. Service tests cover metrics router gating. Task queue tests cover transition hooks. Feature flag tests cover backend and frontend flag plumbing.
- Upstream contribution notes: Community-neutral metrics foundation; no OpenTelemetry spans, dashboards, external vendor exporters, or business metrics.
- Compatibility notes: Existing deployment-target metrics, tracing, logging, RBAC, authentication, action registry, task queue behavior, and agent protocol behavior are unchanged except for optional metrics observations when the flag is enabled.

### PR-047b - Observability tracing

- Status: Implemented locally; tracing abstraction, OpenTelemetry wrapper, HTTP middleware, task transition spans, feature flag, documentation, and ADR completed.
- Upstream base: `6a82cf6f`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=observability_tracing`.
- User-facing behavior: None. PR-047b emits tracing spans only when the feature flag is enabled.
- Database changes: None.
- API changes: No API endpoints are added.
- UI changes: No page is added. The frontend feature flag model recognizes `observability_tracing` for future UI gating.
- Agent protocol changes: None.
- Documentation: Added PR-047b notes and ADR-0047b.
- Tests: Tracing tests cover OTEL span attributes, HTTP request spans, no-op tracer behavior, and task lifecycle spans. Service tests cover disabled no-op providers. Task queue tests cover transition hooks. Feature flag tests cover backend and frontend flag plumbing.
- Upstream contribution notes: Community-neutral tracing foundation; no dashboards, metrics changes, log correlation, exporter customization, step-level tracing, or business spans.
- Compatibility notes: Existing metrics, RBAC, authentication, action registry, deployment process logic, task transition semantics, and agent protocol behavior are unchanged except for optional tracing observations when the flag is enabled.

### PR-047c1 - Observability static dashboards

- Status: Implemented locally; static Grafana dashboard definitions, read-only dashboard catalog API, feature flag, documentation, and ADR completed.
- Upstream base: `060f2369`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=observability_dashboards`.
- User-facing behavior: None. PR-047c1 exposes static dashboard templates only when the feature flag is enabled.
- Database changes: None.
- API changes: Added `GET /api/v1/observability/dashboards`, returning static dashboard definitions and versions.
- UI changes: No page is added. The frontend feature flag model recognizes `observability_dashboards` for future UI gating.
- Agent protocol changes: None.
- Documentation: Added PR-047c1 notes and ADR-0047c1.
- Tests: Dashboard tests cover static definition count, JSON validity, and immutable copies. Handler tests cover response shape and disabled flag behavior. Feature flag tests cover backend and frontend flag plumbing.
- Upstream contribution notes: Community-neutral static dashboard catalog; no correlation links, Grafana API integration, dashboard UI, alerting, log correlation, runtime analytics engine, metrics changes, or tracing changes.
- Compatibility notes: Existing metrics, tracing, RBAC, authentication, action registry, deployment process logic, task transition semantics, and agent protocol behavior are unchanged.

### PR-047c2 - Observability correlation links

- Status: Implemented locally; pure correlation link builders, Grafana base URL config, feature flag, documentation, and ADR completed.
- Upstream base: `73f4d0c4`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=observability_correlation`.
- User-facing behavior: None. PR-047c2 adds pure link-building utilities for future callers.
- Database changes: None.
- API changes: None. The dashboard catalog endpoint from PR-047c1 is unchanged.
- UI changes: No page is added. The frontend feature flag model recognizes `observability_correlation` for future UI gating.
- Agent protocol changes: None.
- Documentation: Added PR-047c2 notes and ADR-0047c2.
- Tests: Correlation tests cover trace, metrics, dashboard, unified context, deterministic label ordering, and empty-base behavior. Feature flag tests cover backend and frontend flag plumbing.
- Upstream contribution notes: Community-neutral link utilities; no dashboard UI, API enrichment, Grafana API calls, alerting, log correlation, storage, metrics changes, or tracing changes.
- Compatibility notes: Existing dashboards, metrics, tracing, RBAC, authentication, action registry, deployment process logic, task transition semantics, and agent protocol behavior are unchanged.

### PR-047c3 - Observability dashboard API enrichment

- Status: Implemented locally; dashboard API correlation metadata, static query templates, documentation, and ADR completed.
- Upstream base: `bf01b424`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=observability_dashboards,observability_correlation`.
- User-facing behavior: None. When correlation is enabled, the dashboard catalog includes optional trace link templates, metric query templates, and correlation hints.
- Database changes: None.
- API changes: Extended `GET /api/v1/observability/dashboards` with optional `traceLinkTemplate`, `metricsQueryTemplate`, and `correlationHints` fields.
- UI changes: None.
- Agent protocol changes: None.
- Documentation: Added PR-047c3 notes and ADR-0047c3.
- Tests: Dashboard tests cover static correlation metadata and immutable copies. Handler tests cover base responses, enriched metadata, and deterministic output.
- Upstream contribution notes: Community-neutral API metadata layer; no dashboard UI, Grafana API integration, alerting, log correlation, storage, metrics changes, or tracing changes.
- Compatibility notes: Existing dashboards, metrics, tracing, RBAC, authentication, action registry, deployment process logic, task transition semantics, and agent protocol behavior are unchanged.

### PR-047d - Observability documentation pack

- Status: Implemented locally; observability reference guide, examples, Grafana static integration guide, feature flag matrix, and fork notes completed.
- Upstream base: `a1143ec3`
- Feature flag: None added. Documents existing `observability_metrics`, `observability_tracing`, `observability_dashboards`, and `observability_correlation` flags.
- User-facing behavior: None. Documentation only.
- Database changes: None.
- API changes: None.
- UI changes: None.
- Agent protocol changes: None.
- Documentation: Added `docs/observability/` reference pages and PR-047d notes.
- Tests: Markdown formatting, local markdown link validation, docs-only diff check, and whitespace check.
- Upstream contribution notes: Community-neutral documentation for the observability suite; no runtime behavior, Grafana provisioning, dashboard UI, alerting, log correlation, or storage changes.
- Compatibility notes: Existing dashboards, metrics, tracing, correlation, RBAC, authentication, action registry, deployment process logic, task transition semantics, and agent protocol behavior are unchanged.

### PR-048 - Config as Code foundation

- Status: Implemented locally; strict typed validation package, validation API, authority persistence, mutation guards, frontend authority service/badge, examples, documentation, and ADR completed.
- Upstream base: `30b893ab`
- Feature flag: Uses `DISTR_EXPERIMENTAL_FEATURE_FLAGS=config_as_code`.
- User-facing behavior: Users can validate Config as Code YAML/JSON documents and see Git-managed authority on supported resource pages. Git-managed edit/delete/revision/import/publish controls are disabled where applicable, and backend mutation guards remain authoritative for all supported resource families.
- Database changes: Added `ConfigAsCodeAuthority` and `ConfigAsCodeAuthorityAuditEvent` tables for org-scoped authority state, non-secret authority-change audit records, and repository-path constraints aligned with API validation.
- API changes: Added `POST /api/v1/config-as-code/validate` and authority APIs under `/api/v1/config-as-code/authorities`.
- UI changes: Added frontend config-as-code types/service, feature flag support, reusable authority badge, and read-only state for Git-managed deployment processes, channels, lifecycles, variable sets, step templates, and runbooks.
- Agent protocol changes: None.
- Documentation: Added ADR-0048, `docs/config-as-code/`, examples, and PR-048 fork notes.
- Tests: Added parser/schema/checksum/secret-safety tests, duplicate JSON key tests, strict channel rule/source/variable semantic tests, repository-path tests including drive-relative paths, authority race tests, feature flag tests, handler tests, DB migration/authority tests, runbook publish guard tests, frontend service tests, and UI guard tests across supported resource pages. Live PostgreSQL authority repository tests require `DISTR_TEST_DATABASE_URL`.
- Upstream contribution notes: Community-neutral Config as Code validation and authority foundation; no Git provider integrations, repository credentials, import/apply/export workflows, branch protection, sync/reconciliation, secret resolution, planner, deployment, task, or agent behavior changes.
- Compatibility notes: Existing resources default to `DATABASE_MANAGED`; reads remain available for Git-managed resources, while normal database mutation paths return `409 Conflict`.

### PR-049 - Compatibility and migration release

- Status: Implemented locally; compatibility adapter, metadata migration, backfill, timeline read-through, command, documentation, and benchmark fixtures completed.
- Upstream base: `ac744097`
- Feature flag: None.
- User-facing behavior: Existing direct deployment history appears in deployment timeline as `legacy_deployment` entries with unavailable advanced dimensions explicitly marked unavailable.
- Database changes: Added additive `DeploymentCompatibilityMetadata` table with deterministic synthetic release identity, checksum, canonical payload, availability flags, and organization/revision uniqueness.
- API changes: None. The public deployment API and agent-facing behavior remain unchanged.
- UI changes: None. Existing timeline consumers receive additional source/availability fields.
- Agent protocol changes: None.
- Documentation: Added PR-049 notes, ADR-0049, upgrade guide, and PR-049 performance fixture guide.
- Tests: Added adapter tests, live PostgreSQL repository/backfill tests, timeline legacy-entry tests, command tests, migration schema tests, and opt-in benchmarks.
- Upstream contribution notes: Community-neutral compatibility layer; no adopter-specific migration UI, destructive cleanup, provider behavior, or agent protocol change.
- Compatibility notes: Original `Deployment` and `DeploymentRevision` rows are not rewritten. Compatibility metadata can be removed without deleting original history. Legacy entries do not fabricate process snapshots, variable snapshots, channels, environments, actors, task logs, or executable redeploy plans.

### PR-050 - Community release hardening

- Status: Implemented locally; release-hardening documentation, security checklist, isolated live community demo wrapper, API-only live release-to-task journey, neutral demo verifier, upstream contribution breakdown, validation scripts, and CI gates completed.
- Upstream base: `fcc472a9`
- Feature flag: None added. Documents existing experimental feature flags and release gates.
- User-facing behavior: None at runtime. Operators and contributors gain release-readiness docs, smoke-test checklists, an isolated live local demo wrapper, an API-only live release-to-task verification path, and a credential-free deterministic verifier.
- Database changes: None.
- API changes: None.
- UI changes: None.
- Agent protocol changes: None.
- Documentation: Added ADR-0050, PR-050 fork notes, release readiness, security, operations, upgrade, architecture, API, and upstream contribution docs.
- Tests: Added `internal/handlers/pr050_community_live_demo_test.go`, `examples/community-e2e/live-demo.mjs`, `examples/community-e2e/run-demo.mjs`, `hack/pr050-license-scan.mjs`, and `hack/pr050-validate-release-hardening.mjs` for the API-only live release-to-task journey, isolated demo orchestration, deterministic demo, Node+Go dependency-license checks, link, impact-section, index, credential-scanner self-tests, static-analysis gate checks, and secret-safety checks.
- Upstream contribution notes: Provides dependency-ordered upstream slices with compatibility and licensing notes.
- Compatibility notes: Existing deployment, advanced roadmap, API, UI, schema, and agent behavior is unchanged.

### PR-051 - Execution preflight and target-component locks

- Status: Implemented locally; live PostgreSQL migration and repository verification remains part of the deployment gate.
- Upstream base: `570a3bb6`.
- Feature flag: Uses the existing `deployment_plans` and `task_queue` feature surfaces.
- User-facing behavior: Operators see persisted execution-time pass/fail checks on Deployment Plan detail and exports.
- Database changes: Added execution preflight runs/checks and the `target_component` task lock resource.
- API changes: Deployment Plan responses include ordered preflight runs and checks.
- UI changes: Deployment Plan detail shows the latest preflight checksum, status, target, component, and result.
- Agent protocol changes: None.
- Documentation: Added PR-051 notes and ADR-0051.
- Tests: Added evaluator, migration, repository, API mapping, and Angular regression coverage.
- Upstream contribution notes: Community-neutral preflight and concurrency primitives; no provider or adopter names.
- Compatibility notes: Runtime status no longer changes new plan checksums; legacy payloads remain checksum-valid, and existing target locks remain in place.

### PR-052 - External execution callbacks and observed state

- Status: Implemented locally; focused and live PostgreSQL verification completed.
- Upstream base: `2ddd5715`.
- Feature flag: Uses the existing `deployment_plans`, `task_queue`, `step_events`, and Hub webhook feature surfaces.
- User-facing behavior: Callback-mode Hub webhooks remain running until an authenticated external executor reports a terminal result; exact observed image, platform, configuration identity, and health are retained.
- Database changes: Added external execution/event history, versioned configuration references on target-component state/history, callback sequence and payload hashes, and optimistic observed-state projection.
- API changes: Added organization-scoped external execution read and callback endpoints; callback mode adds reserved execution headers to the outbound webhook.
- UI changes: No new page in this slice. Existing task events and Deployment Timeline receive progress and terminal state; the read model is available for the following operator detail UI slice.
- Agent protocol changes: None.
- Documentation: Added PR-052 notes and ADR-0052.
- Tests: Added callback validation/state tests, registry and security tests, Hub trigger/restart/race tests, mapping/handler tests, migration checks, and live PostgreSQL repository coverage.
- Upstream contribution notes: Provider-neutral external execution contract; no Jenkins or adopter-specific schema, API, labels, or behavior.
- Compatibility notes: Existing webhooks default to synchronous response completion. Callback steps require a component and versioned immutable release configuration.

### PR-053 - Operator execution detail and previous release

- Status: Implemented locally; focused Angular and live PostgreSQL 18 verification completed.
- Upstream base: `dc8fd69b`.
- Feature flag: Uses the existing `deployment_timeline`, `deployment_plans`, `task_queue`, and `step_events` feature surfaces.
- User-facing behavior: Operators can inspect structured task progress in Deployment Timeline and create a reviewed new plan from a successful historical deployment.
- Database changes: None.
- API changes: No endpoint or response additions. Previous-release creation returns conflict for non-successful source tasks, and timeline availability reflects that rule.
- UI changes: Reused the existing Timeline side panel for Execution and Comparison tabs, added active-task polling, current-versus-historical review, and exact task/plan deep links.
- Agent protocol changes: None.
- Documentation: Added PR-053 notes and ADR-0053.
- Tests: Added Angular service/component coverage and PostgreSQL 18 repository and authenticated handler verification.
- Upstream contribution notes: Community-neutral operator workflow; no adopter or external-executor names, schema, or labels.
- Compatibility notes: Successful task history remains eligible; previous release creates a new immutable plan and does not reverse migrations or external side effects.

### PR-054 - Immutable config execution inputs

- Status: Implemented locally; focused release-contract, mapping, migration, and live PostgreSQL repository verification completed.
- Upstream base: `ba93237f`.
- Feature flag: Uses the existing release-contract and callback external-execution feature surfaces.
- User-facing behavior: External executors receive frozen immutable references and checksums for both service configuration and compose inputs.
- Database changes: Migration 137 adds expected compose reference/checksum fields to external executions.
- API changes: External-execution `expectedState` adds `composeReference` and `composeChecksum`.
- UI changes: None.
- Agent protocol changes: None.
- Documentation: Added PR-054 notes and ADR-0054.
- Tests: Added content-address validation, external-execution persistence, API mapping, and migration coverage.
- Upstream contribution notes: Provider-neutral immutable execution inputs; no adopter or CI-provider names, credentials, or labels.
- Compatibility notes: Versioned object references remain valid. Content-addressed S3 objects are accepted only when their path digest matches the declared checksum.

### PR-054A - External-execution timestamp expand

- Status: Implemented locally; CI matrix configured; independent review and real PostgreSQL 16.14/18.4
  service-container legs remain pending Task 11.
- Feature flag: None.
- User-facing behavior: Operators gain the audited `timestamp-expand-recover-dirty` CLI; public API and UI behavior
  remain unchanged.
- Database changes: Migration 138 adds nullable instant shadows, paired future defaults, future indexes, immutable
  timestamp manifest/provenance metadata, authorized append-only retention tombstones, contract-gate foundation,
  and durable zero-history proof.
- API changes: None.
- UI changes: None.
- Agent protocol changes: None.
- Documentation: Added ADR-0055, the approved hybrid design, fenced Compose procedure, release/upgrade/smoke/security
  guidance, and PostgreSQL 16.14/18.4 gates. The operating package now also documents audited dirty recovery.
- Tests: Local non-database validators, migration-pair validation, and the Compose orchestration harness passed. The
  focused CI matrix is configured for pinned PostgreSQL 16.14 and 18.4; both real service-container legs remain
  pending Task 11.
  The focused dirty-recovery harness covers manifest/no-manifest binding, retained evidence, clean retry,
  interrupted result archiving, one Apply, and non-finalization.
- Upstream contribution notes: Community-neutral control-plane migration; no adopter repository, application
  database, credentials, host names, or cleanup behavior.
- Compatibility notes: Expand continues reading legacy timestamps and writes paired legacy/instant values. Contract
  eligibility and canonical instant reads are separate later work. Organization retention preserves timestamp
  evidence through tombstones; a retained unresolved cell may be resolved later as provenance-only evidence, and
  deletion must predate that promotion. Readiness separates live shadow and deleted-evidence decision buckets;
  unexplained source deletion remains a failure. Downgrade is refused after retention, post-expand `ZERO_HISTORY`
  rows, or manifest application, with an exclusive-lock recheck inside migration 138 down. The trusted-owner GUC is
  an integrity guard, not a privilege boundary; least-privilege roles remain deferred. The separate modern
  organization purge-order failure at `deploymentplantarget_target_fk` remains a functional blocker outside
  PR-054A. Dirty recovery is catalog-proven marker repair only; it leaves Hub stopped and fenced and never replaces
  normal timestamp-expand finalization.

### PR-055 - Operator control-plane v2 isolation boundary

- Status: Implemented locally; focused backend and frontend feature-flag verification completed.
- Upstream base: `50c0bec4`.
- Feature flag: Adds process-wide `operator_control_plane_v2` and `executor_protocol_v2` flags. Executor protocol v2 is effective only while the operator control-plane v2 umbrella is also effective.
- User-facing behavior: Admins can see both registered flags and their effective state in Organization Settings. Both remain disabled unless explicitly configured.
- Database changes: None.
- API changes: No new endpoint. The existing experimental-feature-flags response includes both keys and reports layered effective state.
- UI changes: Extends the typed experimental-feature-flag contract; no new route or workflow is exposed.
- Agent protocol changes: None. Existing agent and external-execution v1 behavior is unchanged.
- Documentation: Added PR-055 notes plus feature-flag and upgrade guidance for layered kill switches and safe rollout.
- Tests: Added parsing, deterministic registry ordering, layered effectiveness, TypeScript key, label, and state coverage.
- Upstream contribution notes: Community-neutral isolation primitives; no adopter, infrastructure provider, or CI-provider logic.
- Compatibility notes: Historical reads and all v1 writes continue unchanged. Neither new flag may be enabled in a shared or production environment before PR-083 hardening completes.

### PR-056 - Canonical deployment registry identity

- Status: Implemented locally; the full serial Go suite in normal non-live mode, focused tests, the exact isolated
  PostgreSQL 16.14 registry suite, and the operation-exact routed API integration suite pass. PostgreSQL 18 remains
  mandatory in CI.
- Upstream base: `6e4fdd2d`.
- Feature flag: `operator_control_plane_v2` gates POST, PUT, and DELETE only; authenticated registry reads remain
  available while disabled.
- User-facing behavior: Operators and later setup/import flows receive stable identities for scopes, target
  environment assignments, physical units, shared subscribers, component definitions, aliases, instances, and
  aggregate placements.
- Database changes: Migration 139 adds seven public organization-owned registry tables plus private append-only
  `ComponentInstanceRename` evidence. Composite tenant foreign keys are deferred `NO ACTION` constraints so
  organization retention can cascade the complete registry graph under its exact transaction-local marker while
  ordinary subscriber/history deletion remains blocked. The schema also provides
  non-overlapping assignment intervals, active-identity uniqueness, idempotent canonical-text normalization and
  checks matching repository behavior, deterministic page indexes for every list resource, and immutable
  shared-unit subscriber checksums and memberships. Initial membership is atomically sealed; later direct
  subscriber-row mutations fail at the schema boundary. Subscriber checksums sort native UUID values rather than
  collation-sensitive text. Down migration refuses while registry or rename-evidence rows exist.
- API changes: Adds organization-scoped CRUD/list routes below `/api/v1/deployment-registry`; lists use a versioned
  opaque keyset cursor with default 50 and maximum 100. Shared unit creation carries the initial subscriber customer
  IDs and matching checksum in one request. Standalone subscriber POST/DELETE return conflict after sealing; PUT
  succeeds only as an exact no-op and otherwise conflicts. Alias PUT, alias DELETE, and instance DELETE return
  conflict when durable rename evidence protects the identity.
- UI changes: None.
- Agent protocol changes: None.
- Documentation: Added ADR-0056 and PR-056 fork notes.
- Tests: Added pure topology/validation, migration/repository, retention of a sealed shared unit, atomic
  shared-membership and direct SQL normalization/mutation guards, multi-hop and concurrent rename evidence,
  native-UUID checksum ordering with an explicit text collation, runtime bounded-query placement assembly,
  deterministic repeatable-read placement consistency, zero-row/concurrent write races, every routed resource and
  authorization mode, protected-delete, pagination, stable subscriber compatibility, and complete generated
  OpenAPI contract coverage.
- Upstream contribution notes: Community-neutral deployment identity model; no adopter, cloud, database product,
  CI provider, credential, or infrastructure-specific behavior.
- Compatibility notes: Existing v1 APIs and execution remain unchanged. Placement roots and relations are batch
  loaded in seven queries within one repeatable-read snapshot. Every accepted rename hop remains append-only;
  subscriber-set changes require a new unit identity; foreign IDs and zero-row write races return 404 without
  tenant leakage.

### Operations checkpoint - Jenkins Hub image publication

- Status: Implemented locally with mocked publication-boundary verification.
- User-facing behavior: A Pipeline-from-SCM job builds one exact reviewed Hub commit for Linux AMD64, publishes one immutable ECR candidate, and archives a checksummed digest handoff.
- Database, API, UI, and agent changes: None.
- Documentation: Added the generic Jenkins job configuration, immutable publication guarantees, handoff contract, and separate deployment gate.
- Tests: Added commit, tag, checkout, collision, repository-immutability, OCI identity, platform, credential-redaction, handoff, checksum, and publish-only pipeline checks.
- Upstream contribution notes: Generic release engineering only; no adopter, server, credential, deployment, or runtime assumptions are embedded in product behavior.
- Compatibility notes: Reuses the existing `image-check`, `build`, and `push` helper commands. Publication never invokes `deploy` or `release`.

### PR-057 - Deployment registry import and classification

- Status: Implemented on the speculative PR-056 branch; live PostgreSQL integration is deferred to the final gate.
- Upstream base: `2ca41ab8`.
- Feature flag: `operator_control_plane_v2` gates preview, decision, and apply mutations.
- User-facing behavior: Operators receive deterministic preview, explicit classification, checksum-bound apply,
  bounded diagnostics, exact coverage, and visible adapter-declared source-placement omissions without conflating
  explicit retirements.
- Database changes: Migration 140 adds organization-scoped import, root, placement, and append-only decision
  evidence with actor attribution, content-addressed report references, exact counts/omissions, state, and checkpoints.
- API changes: Adds preview, decision, apply, import detail, and coverage routes under `/deployment-registry`;
  preview accepts an optional bounded `sourcePlacements` identity baseline.
- UI changes: Setup UI is implemented in the separate PR-057 frontend slice.
- Agent protocol changes: None.
- Documentation: PR-057 architecture-decision evidence is folded into its fork note; ADR-0057 remains uniquely
  allocated to PR-058.
- Tests: Added fast pure import/checksum/classification/coverage/sanitization tests, static migration and assignment
  reuse contract tests, and deferred live PostgreSQL omission persistence/apply coverage.
- Upstream contribution notes: Community-neutral normalized adapter input; no adopter, provider, hostname, path,
  credential, or raw report data.
- Compatibility notes: Existing registry identities, v1 APIs, deployment execution, agents, and historical
  checksums remain unchanged. Apply is blocked by unresolved decisions, conflicts, or omissions.

### PR-058 - Immutable target config snapshots

- Status: Backend implemented on the prepared speculative PR-056 checkpoint; live PostgreSQL and final integration
  gates remain deferred.
- Upstream base: `2ca41ab8`.
- Feature flag: Uses `operator_control_plane_v2` for create and verify; authenticated reads remain available while
  disabled.
- User-facing behavior: Environment owners can create, list, inspect, and verify immutable target configuration
  evidence without exposing secret values.
- Database changes: Migration 141 adds one immutable snapshot parent and four immutable child tables, exact
  canonical bytes/checksum, composite tenant/placement constraints, mutation guards, retention exception, and
  guarded downgrade.
- API changes: Adds create/list/get/verify below `/api/v1/target-config-snapshots`; no update/delete route exists.
- UI changes: Separate UI slice consumes the non-secret API contract.
- Agent protocol changes: None.
- Documentation: Added ADR-0057 and PR-058 fork notes.
- Tests: Added deterministic canonicalization, validation, S3 verification, migration/repository, API, mapping,
  handler, authorization, routing, and redaction coverage. Live PostgreSQL cases require
  `DISTR_TEST_DATABASE_URL`.
- Upstream contribution notes: Provider-neutral immutable configuration evidence; no adopter, CI provider,
  credential, host, or client-specific behavior.
- Compatibility notes: Existing v1 reads and execution remain unchanged. PR-059 owns restartable v1 extraction;
  PR-058 does not rewrite history.

### PR-059 - v1 target config extraction and lineage

- Status: Backend and operator CLI implemented on the PR-058 backend checkpoint; live PostgreSQL and final
  integration gates remain deferred.
- Upstream base: `3853ae81`.
- Feature flag: Does not switch reads or execution; existing v1 behavior remains authoritative when
  `operator_control_plane_v2` is disabled.
- User-facing behavior: Operators can create a deterministic dry-run checkpoint, review stable blocked reasons,
  follow an applied predecessor checkpoint through an immutable root source membership, apply an actor-bound
  approved checksum in atomic restartable batches, and print persisted lineage.
- Database changes: Migration 142 adds immutable `BackfillCheckpoint`, immutable
  `BackfillCheckpointSourceMembership`, and append-only `ReleaseContractV1ExtractionLineage` evidence with an
  organization-member actor, one-successor predecessor chains, checksum-bound root source membership,
  `(created_at, plan_id)` source bounds, organization-scoped source/derived-snapshot constraints, and guarded
  downgrade.
- API changes: None.
- UI changes: None.
- Agent protocol changes: None.
- Documentation: Added PR-059 extraction/backfill notes and an upgrade procedure.
- Tests: Added pure deterministic extraction, exact object evidence, variable/secret boundaries, logical
  component resolution, command cursor/approval, atomic repository restart/concurrency/rollback, mixed-history,
  and migration contract checks. Live PostgreSQL remains a final integration gate.
- Upstream contribution notes: Community-neutral compatibility extraction; no adopter, CI-provider, database
  product, credential, host, or client-specific behavior.
- Compatibility notes: Original v1 release and plan IDs, exact canonical bytes, checksums, reads, and execution are
  never changed. Ambiguous, unverifiable, or unrepresentable sources remain blocked; retries create/reuse
  canonical snapshots and append lineage idempotently in a single transaction.

### PR-060 - Component Release Contract v2

- Status: Implemented on the prepared speculative branch; fast pure/unit/compile verification completed and
  integration gates deferred until PR-059 integration.
- Upstream base: `2ca41ab8` (PR-056 checkpoint).
- Feature flag: `operator_control_plane_v2` gates new component-release create, update, and publish writes. A
  disabled update checks both the tenant-scoped stored release and incoming payload, preventing v2 downgrade.
  Historical reads and v1 writes retain existing behavior.
- User-facing behavior: CI can publish one target-neutral component/version with immutable manifest and
  per-platform digests, source intent and resolved commit, capabilities, migrations, changes, and evidence.
- Database changes: Migration 143 adds release kind/schema metadata and normalized artifact, evidence, capability,
  and migration facts without rewriting historical payloads or checksums.
- API changes: Existing `/api/v1/release-bundles` accepts discriminated v1/v2 contracts and returns additive
  `kind` and `releaseContractSchema` fields.
- UI changes: Release Bundle detail retains v1 content, labels embedded contract schema separately from storage
  classification, and adds v2 artifact/platform, capability, migration, and evidence summaries.
- Agent protocol changes: None.
- Documentation: Added ADR-0058 and PR-060 fork notes.
- Tests: Added strict parser, bounded credential/userinfo/PEM and embedded absolute-path detection,
  portable immutable evidence and package-reference parsing, collection/string/payload and outer-projection limits,
  irrelevant projection-field rejection, exact contract-source projection and source-policy binding,
  type-preserving artifact/component bijection, deterministic non-null collection canonicalization, legacy schema
  rendering, API/gate, migration, and deferred PostgreSQL publication/idempotency/full-lineage conflict coverage.
- Upstream contribution notes: Community-neutral release identity; no adopter, CI provider, registry, target,
  credential, or infrastructure-specific core behavior.
- Compatibility notes: Embedded v1 remains `distr.release-contract/v1`; additive row metadata classifies it as
  `legacy`/`distr.release/v1`. Component publish skips target Variable Snapshots, exact publish retry is idempotent,
  and blocked or archived history still fences the complete artifact identity.

### PR-061 - Release provenance verification and safe v1-to-v2 backfill

- Status: Implemented on the prepared speculative branch; focused provenance, backfill, CLI, and compile
  verification completed, with live PostgreSQL and full-repository gates deferred until integration.
- Upstream base: PR-060 Component Release Contract v2.
- Feature flag: Uses `operator_control_plane_v2` for new component-release publication. Historical reads and
  untouched v1 behavior retain their existing gates.
- User-facing behavior: Component publication verifies signed in-toto/Sigstore provenance offline against frozen
  trusted roots and policy. Operators can preview and apply a checkpointed v1-to-v2 release backfill without
  changing historical release evidence.
- Database changes: No new migration is allocated to PR-061. It consumes the additive, organization-scoped,
  append-only evidence-verification and release-lineage/checkpoint relations reserved with the Component Release
  v2 schema foundation. Verification receipts include the exact source repository/commit and
  builder/invocation ID.
  Backfill checkpoints bind the reviewed document reference/SHA-256, and lineage binds the selected reviewed
  artifact row. Stored verification facts and blocker diagnostics are bounded and redacted.
- API changes: No new route family. Existing component publication fails closed for missing, untrusted, malformed,
  expired, oversized, tampered, or policy-mismatched provenance. The release-bundle preflight seam exposes the same
  bounded verification facts without coupling to the future target-plan package. Signed dependency
  repository/commit and invocation/builder values must exactly match the Component Release.
- UI changes: None.
- CLI changes: Existing `distr release` flags remain compatible; create adds optional local `--schema v1|v2`
  assertion, publish adds optional `--provenance-file`, and v2 text output includes schema, canonical checksum, and
  artifact/platform digests. Added dry-run-by-default `distr backfill-release-contract-v2` with organization,
  checkpoint, batch, stable-cursor, and bounded reviewed artifact-evidence options; dry-run reports
  `wouldDerive` while persisted `derived` remains zero. Apply mutates at most one bounded batch, validates the
  byte-exact evidence document on resume, and returns `nextCursor`/`awaitingEvidence` without permanently blocking
  unreviewed rows.
- Agent protocol changes: None.
- Documentation: Added PR-061 fork notes and updated the release CLI and community API index.
- Tests: Added trusted and invalid provenance cases, exact subject/source/build/policy matching,
  malformed/oversized/tampered inputs, bounded persistence and migration constraints, publication/preflight gates,
  backfill reviewed-media-type/dry-run/checkpoint checksum/one-batch/blocker and v1 immutability coverage, and
  v1/v2 CLI compatibility.
- Upstream contribution notes: Community-neutral offline supply-chain verification and additive compatibility
  migration; no adopter, CI provider, registry, target, credential, or infrastructure-specific behavior.
- Compatibility notes: Backfill never changes v1 IDs, JSON/canonical bytes, checksums, statuses, or historical
  references, and never fabricates provenance. Disabling v2 leaves untouched v1 reads/executions functional; it
  does not delete derived v2 rows or perform a lossy reverse conversion.

### PR-062 - Product Release capability graph

- Status: Implemented on the prepared speculative branch; focused graph, canonical, API, repository, mapping, and
  handler checks completed, with integration gates deferred until the numbered predecessors are integrated.
- Upstream base: `c5c33af4` (PR-060 checkpoint).
- Feature flag: `release_bundles` and `operator_control_plane_v2` guard the Product Release route family; create and
  publish also require vendor read-write/admin authority and block super-admin mutation.
- User-facing behavior: Release managers pin exact published Component Releases, inspect capability validation and
  graph order, retain explicit target-deferred requirements, and publish an immutable Product Release.
- Database changes: Migration 144 adds organization-scoped child pins with frozen v2 contract snapshots,
  provider-to-consumer capability edges, and byte bounds for product/component versions and indexed graph values.
- API changes: Adds `/api/v1/product-releases` create, get, validate, publish, and graph routes with tenant-safe
  errors and canonical/graph checksum visibility.
- UI changes: None.
- Agent protocol changes: None.
- Documentation: Added ADR-0059 and PR-062 fork notes.
- Tests: Added exact cycle, missing/ambiguous/incompatible provider, unpublished/foreign/duplicate child,
  all-field migration equality, bounded collections, deterministic batch locking, fail-closed provenance/policy
  adapters, product-stage gap, platform, symbolic target node, deterministic checksum, migration, API, and handler
  coverage.
- Upstream contribution notes: Community-neutral product composition and dependency resolution; no adopter,
  infrastructure, customer, target, credential, or secret behavior.
- Compatibility notes: The parent remains `ReleaseBundle(kind=product)`; v1 and Component Release v2 history is not
  rewritten. Generic Release Bundle mutations cannot bypass the Product Release workflow. Publication remains
  unavailable until PR-061 and PR-067 register exact organization-scoped provenance and immutable published-policy
  verifiers.
