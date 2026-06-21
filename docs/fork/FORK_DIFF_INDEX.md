# Fork Diff Index

This file tracks generic fork additions and upstream-facing changes introduced after the upstream base recorded in `docs/fork/UPSTREAM_BASE.md`.

## Current Status

PR-000 through PR-026 are implemented locally. PR-026 adds the Docker-agent `distr.oci.job` one-shot container action with digest-only image references, trusted agent allowlists for registries/networks/mounts, lease-time secret resolution, StepRun redaction, deterministic-container idempotency, and Docker hardening defaults while preserving existing Compose and legacy resource-poll deployment behavior.

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
- Agent protocol changes: Docker agents advertise `distr.oci.job` version `1`, claim a task lease through the existing lease flow, heartbeat before and during job execution, enforce trusted `DISTR_OCI_JOB_ALLOWED_*` policy, run digest-only allowlisted OCI images through Docker, stop containers on timeout/cancellation, and reuse deterministic containers only when trusted operation labels match for retry/reclaim/restart idempotency.
- Documentation: Added PR-026 notes and ADR-0026.
- Tests: Action registry schema/order tests, Docker capability tests, OCI action validation/execution/redaction/expected-exit/idempotency/restart/collision/timeout/cancellation/bounded-output tests, task lease OCI secret resolution tests, and StepRun OCI redaction tests were added. Focused `go test ./internal/actionregistry`, Docker-agent tests with dummy agent endpoint environment values, and focused live PostgreSQL `internal/db` tests passed locally.
- Upstream contribution notes: Community-neutral OCI one-shot action adapter; no adopter-specific terminology, no file/webhook behavior, no UI workflow, and no arbitrary host shell execution.
- Compatibility notes: Existing Environment, Lifecycle, Channel, Release Bundle, Deployment Process, Process Snapshot, Variable Snapshot, Deployment Plan preview/UI, Task Queue create/read APIs, locks/concurrency semantics, deployment target, deployment, release-name, frontend planning UI, Kubernetes agent behavior, Compose action behavior, and legacy Docker resource-poll deployment behavior are unchanged when no OCI job task lease is claimed. No file render action, webhook action, Helm typed action, approvals, guided failure, retention, notifications, Angular task timeline UI, or PR-027 behavior is added in PR-026.
