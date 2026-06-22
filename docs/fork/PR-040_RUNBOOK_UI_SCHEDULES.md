# PR-040 - Runbook UI and schedules

PR-040 adds the first feature-flagged Runbooks UI on top of the PR-039 runbook APIs. It gives operators a place to author runbooks, create revisions, publish revisions, and inspect published revision state without changing task execution or scheduler behavior.

## Scope

Included:

- feature-flagged `/runbooks` route and sidebar entry for vendor administrators
- typed Angular runbook service and models for the existing PR-039 endpoints
- editor tab for listing, filtering, creating, updating, deleting, and selecting runbooks
- revision editor for creating ordered runbook steps with retry, condition, permission, dependency, and input-binding fields
- publish action for immutable runbook revision snapshots
- history and schedules tabs as UI surfaces for later execution and scheduler APIs

Not included:

- runbook execution orchestration or task creation
- run-now backend endpoint or run history persistence
- cron/interval scheduler persistence or background worker changes
- database schema changes
- action registry, webhook action, task lease, or agent protocol changes
- approval, guided failure, retention, notification, or RBAC expansion

## UI

The Runbooks page is gated by `DISTR_EXPERIMENTAL_FEATURE_FLAGS=runbooks` and visible to vendor administrators.

The editor tab loads applications and runbooks, supports client-side filtering, and opens dialogs for runbook metadata and revision authoring. Selecting a runbook fetches the single runbook record and its revision list, keeping the UI wired to both list and detail APIs.

The history tab is intentionally read-only until runbook execution APIs exist. The schedules tab exposes the intended configuration surface, but save/run controls are disabled until scheduler endpoints land in a later PR.

## API

The Angular service wraps the existing PR-039 API surface:

- `GET /api/v1/runbooks`
- `POST /api/v1/runbooks`
- `GET /api/v1/runbooks/{runbookId}`
- `PUT /api/v1/runbooks/{runbookId}`
- `DELETE /api/v1/runbooks/{runbookId}`
- `GET /api/v1/runbooks/{runbookId}/revisions`
- `POST /api/v1/runbooks/{runbookId}/revisions`
- `GET /api/v1/runbooks/{runbookId}/revisions/{revisionId}`
- `POST /api/v1/runbooks/{runbookId}/revisions/{revisionId}/publish`

## Verification

Focused frontend tests cover:

- runbook API service URLs and methods
- runbook feature-flag observable
- Runbooks page load, filter, and load-error behavior
- runbook creation payloads
- runbook detail and revision-history selection
- structured revision creation payloads
- publish action wiring
- invalid input-binding JSON rejection
