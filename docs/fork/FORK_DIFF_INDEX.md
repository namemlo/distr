# Fork Diff Index

This file tracks generic fork additions and upstream-facing changes introduced after the upstream base recorded in `docs/fork/UPSTREAM_BASE.md`.

## Current Status

PR-000 through PR-009 are implemented locally. PR-009 adds idempotent CI Release Bundle creation, generic source metadata, strict OCI digest validation, release CLI commands, and neutral CI examples without adding lifecycle promotion, deployment planning, execution, or agent behavior.

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
