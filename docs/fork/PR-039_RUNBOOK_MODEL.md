# PR-039 - Runbook model

PR-039 adds a versioned runbook model for deployment-independent operational workflows. It provides the backend model, immutable published snapshots, and a reserved task discriminator that later PRs can use for runbook execution.

## Scope

Included:

- organization/application-scoped `Runbook` records
- append-only `RunbookRevision` records with ordered steps and dependencies
- immutable `RunbookSnapshot` publication with canonical payload and checksum
- runbook step validation using the existing action registry and restricted condition language
- dependency and output-reference cycle checks
- feature-flagged runbook CRUD, revision, and publish APIs
- `Task.task_type` discriminator with existing task creation defaulting to `deployment`

Not included:

- runbook UI, schedules, or history page
- runbook execution planning or orchestration
- runbook task creation, leasing, or agent execution
- deployment task schema changes beyond the defaulted discriminator
- Docker or Kubernetes agent protocol changes
- approval, guided failure, or notification behavior

## API

The `runbooks` experimental feature flag gates:

- `GET /api/v1/runbooks`
- `POST /api/v1/runbooks`
- `GET /api/v1/runbooks/{runbookId}`
- `PUT /api/v1/runbooks/{runbookId}`
- `DELETE /api/v1/runbooks/{runbookId}`
- `GET /api/v1/runbooks/{runbookId}/revisions`
- `POST /api/v1/runbooks/{runbookId}/revisions`
- `GET /api/v1/runbooks/{runbookId}/revisions/{revisionId}`
- `POST /api/v1/runbooks/{runbookId}/revisions/{revisionId}/publish`

Publishing a revision creates or returns an immutable snapshot for that revision. The snapshot stores a canonical JSON payload and checksum so later execution can rely on a stable runbook definition even if new revisions are created.

## Task Type

`Task.task_type` is added with allowed values `deployment` and `runbook`. Existing deployment task creation explicitly writes `deployment`; `runbook` is reserved for future runbook execution PRs and is not leased or executed in this PR.

## Verification

Focused tests cover:

- runbook request validation
- handler guardrails and feature flag protection
- mapping to API response types
- repository create/list/update/delete/revision/publish behavior
- canonical snapshot stability
- invalid dependency graph rejection
- migration schema coverage
- existing deployment task creation defaulting to `deployment`
