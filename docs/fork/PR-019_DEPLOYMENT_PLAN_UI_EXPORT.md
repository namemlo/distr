# PR-019 - Deployment Plan UI and export

## Scope

PR-019 implements the feature-flagged Angular administration UI for the PR-018 Deployment Plan foundation.

It adds:

- Deployment Plans route and sidebar entry.
- Angular Deployment Plan API service and typed models.
- Plan list with status, issue counts, target counts, and checksum display.
- Plan creation from:
  - a published Release Bundle
  - an Environment
  - selected Deployment Targets
- Plan detail preview with:
  - blockers
  - warnings
  - resolved targets
  - resolved steps
  - resolved variables with redacted values preserved
  - canonical checksum
- Client-side JSON and Markdown export of the API plan payload.

## Feature flag

The UI is visible only when the full PR-018 planning stack is enabled:

```text
DISTR_EXPERIMENTAL_FEATURE_FLAGS=deployment_plans,release_bundles,deployment_processes,scoped_variables_v2,channels,lifecycles,environments
```

The API remains protected by the backend middleware added in PR-018.

## Non-goals

PR-019 does not add:

- new database tables or migrations
- new backend API endpoints
- task queue behavior
- execution behavior
- locks or leases
- approvals
- rollback or rollout waves
- notifications
- runbooks
- agent protocol changes
- remote dry-run execution

## Export behavior

JSON export serializes the Deployment Plan response returned by the PR-018 API.

Markdown export summarizes:

- plan status and checksum
- Release Bundle, Application, Channel, and Environment
- blockers and warnings
- targets
- steps
- variables

Redacted variables remain redacted in the UI and Markdown export.

## Compatibility notes

Existing Environment, Lifecycle, Channel, Release Bundle, Deployment Process, Process Snapshot, Variable Snapshot, deployment target, deployment, release-name, backend planning, and agent behavior is unchanged.

## Verification

Passed locally:

- Focused Angular tests:
  - `pnpm exec ng test --watch=false --include frontend/ui/src/app/services/deployment-plans.service.spec.ts --include frontend/ui/src/app/services/feature-flag.service.spec.ts --include frontend/ui/src/app/deployment-plans/deployment-plans.component.spec.ts`
- Full Angular tests:
  - `pnpm exec ng test --watch=false`
- Full Go tests with live PostgreSQL:
  - `DISTR_TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable go test -p=1 ./...`
- Migration validation:
  - `hack/validate-migrations.sh`
- Touched-file Prettier:
  - `pnpm exec prettier --check <changed supported files>`
- Diff-scoped Go lint:
  - `golangci-lint run --new-from-rev=fork/main ./api/... ./internal/...`
- Community frontend build:
  - `pnpm run build:community`
- Community Hub, Docker agent, and Kubernetes agent binary builds.
- Unicode bidi, zero-width, BOM, and replacement-character scan over changed files.

Known existing warning:

- Angular test/build output still includes the existing Node localStorage warning and CSS selector warning seen in earlier PR verification.
