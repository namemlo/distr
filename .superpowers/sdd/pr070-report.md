# PR-070 Implementation Report

## Status

IMPLEMENTED_WITH_STACK_DEPENDENCIES

## Initial HEAD

`3a335355a6a0dbf25684d097a278b37960cd8c92`

Branch: `codex/pr-070-admission`

The six inherited untracked files were preserved and completed:

- `internal/types/admission.go`
- `internal/scheduling/admission.go`
- `internal/scheduling/admission_test.go`
- `internal/db/admission_test.go`
- `internal/migrations/sql/152_deployment_admission_overrides.up.sql`
- `internal/migrations/sql/152_deployment_admission_overrides.down.sql`

## Changed Files

- `.superpowers/sdd/pr070-report.md`
- `api/admission.go`
- `api/admission_test.go`
- `docs/adr/0063-deployment-admission-emergency-overrides.md`
- `docs/api/community-release-api-index.md`
- `docs/fork/FORK_DIFF_INDEX.md`
- `docs/fork/PR-070_DEPLOYMENT_ADMISSION_OVERRIDES.md`
- `internal/db/admission.go`
- `internal/db/admission_test.go`
- `internal/db/admission_v1_test.go`
- `internal/handlers/admission.go`
- `internal/handlers/admission_test.go`
- `internal/handlers/deployment_plans.go`
- `internal/mapping/admission.go`
- `internal/mapping/admission_test.go`
- `internal/migrations/sql/152_deployment_admission_overrides.down.sql`
- `internal/migrations/sql/152_deployment_admission_overrides.up.sql`
- `internal/routing/admission_test.go`
- `internal/scheduling/admission.go`
- `internal/scheduling/admission_test.go`
- `internal/scheduling/admitted_task_creation.go`
- `internal/scheduling/admitted_task_creation_test.go`
- `internal/types/admission.go`

## Implemented Behavior

- Pure `ADMIT` / `WAIT` / `BLOCK` evaluation over immutable plan, effective-policy, approval, calendar, freeze,
  campaign, mandatory-gate, and optional emergency-override evidence.
- Separate material and decision checksums so ordinary clock movement does not change approved material.
- Append-only, tenant-scoped evaluation and override persistence with exact checksum binding, guarded retention,
  expiry, protected-gate rejection, and scheduler replay conflict detection.
- Required scoped `plan.execute` and `emergency.override` authorization callbacks carrying organization,
  environment, and deployment-unit scope. Missing PR-066 integration fails closed.
- Both-flag route gate and the two requested POST endpoints.
- Concrete DB and scheduling `CreateTasksForAdmittedV2Plan` wrappers. Frozen v2 identity and an `ADMIT` result are
  required before delegating to the unchanged shared task creator.
- Flags-off v1 regression covering no policy, approval, calendar, admission, or enrollment prerequisite and
  unchanged plan/task/step-run/event state.

## TDD and Verification

Observed RED:

- Repository test failed on missing `resolveAdmissionPersistenceReplay`.
- API/mapping tests failed on missing admission contracts and mappings.
- Admitted-v2 wrapper tests failed on missing wrapper/dependency contract.
- Handler tests failed on missing scope/flag handlers.
- Retention-marker contract failed until migration 152 used the existing organization-retention marker.
- Override lifetime test failed because a future-created override was initially accepted.
- Full routing construction panicked when a second `/{deploymentPlanId}` router was mounted; routes were moved into
  the existing deployment-plan subrouter.

Fresh GREEN:

- `go test ./internal/scheduling ./internal/db ./api ./internal/handlers -run
  'Admission|EmergencyOverride|CreateTasksForAdmittedV2Plan|V1TaskCreation' -count=1`
- `go test ./internal/mapping ./internal/routing -run 'Admission|EmergencyOverride' -count=1`
- `go test ./internal/routing -count=1`
- `go vet ./internal/scheduling ./internal/db ./api ./internal/mapping ./internal/handlers ./internal/routing`

Migration lint:

- `mise run lint:migrations` cannot invoke the shell script directly under this Windows `cmd` task runner.
- Running the exact script with Git Bash validates through migration 152 but reports missing stacked migrations
  141-148. Migration 152 itself is a correctly named up/down pair and its live test migration was exercised by the
  focused DB suite.

## Commit

Conventional commit message: `feat: gate plans with deployment admission`

The final hash is reported in the task handoff because a commit cannot embed its own hash.

## Concerns and Dependency Seams

- PR-066 is not in this branch. The default authorization adapter therefore fails closed. During stack integration,
  wire `types.AdmissionAuthorizationContext` to PR-066 `plan.execute` / `emergency.override` authorization plus
  effective `ControlPlaneEnrollment`; do not add a legacy-role fallback.
- PR-063 is not in this branch. The repository reads `plan_schema` and `protocol_version` through a
  schema-tolerant `to_jsonb` snapshot, which rejects current v1 rows and consumes the real immutable v2 columns once
  PR-063 is integrated.
- Migrations 141-148 are absent from this speculative branch, so repository-wide migration sequence lint remains an
  integration-stack gate rather than a migration-152 defect.
- PR-071 campaign revisions are not present. The optional campaign evidence type/checksum seam is pinned but no
  campaign repository lookup is added in PR-070.
