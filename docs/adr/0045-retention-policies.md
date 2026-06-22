# ADR-0045 - Retention Policies

## Status

Accepted

## Context

The fork needs retention controls for deployment history, failed tasks, logs, artifacts, runbook runs, and audit records. Deleting this data is high risk because some release bundles may still be deployed, some records have compliance minimums, and operators need a reviewable preview before any cleanup job mutates data.

## Decision

Introduce organization-scoped `RetentionPolicy` records and a `RetentionCleanupJob` table that stores immutable dry-run cleanup plans. The first implementation supports:

- last-N successful release retention,
- currently-deployed release safety blocks,
- retention-protected release safety blocks,
- failed/canceled deployment task retention by environment production class,
- step-log cleanup previews grouped by task,
- apply rejection for non-dry-run cleanup jobs.

The policy schema also stores audit-retention settings, but audit deletion is deferred. Release bundle digest metadata is retained because the preview-only slice does not delete release bundle components or artifacts.

## Consequences

- Operators can see cleanup impact before deletion exists.
- Future destructive cleanup can consume the stored preview shape without changing the public policy API.
- The feature stays behind `retention_policies`, plus existing release/task/environment feature dependencies.
- Existing rows are unaffected until a caller creates policies or dry-run jobs.

## Alternatives Considered

- Immediate destructive cleanup: rejected because preview and safety review need to land first.
- Hard-coded retention windows: rejected because production and non-production history need different retention windows.
- UI-first implementation: deferred because the roadmap PR only requires preview, safety rules, and cleanup jobs; API and repository safety are the critical foundation.
