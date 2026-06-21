# ADR-0013: Process Snapshots

## Status

Accepted for PR-013.

## Context

PR-011 introduced organization/application-scoped Deployment Processes and append-only revisions. PR-012 added the administration UI for those existing APIs. The roadmap next requires immutable process snapshots linked to Release Bundles without adding variables, deployment planning, execution, approvals, retention, notifications, or agent behavior.

Release Bundles need a stable reference to the exact process revision selected for a release. That link must remain organization-scoped and application-scoped so a Release Bundle cannot expose or use a process revision from another organization or application.

## Decision

Add a `ProcessSnapshot` table and a nullable `ReleaseBundle.process_snapshot_id`.

Release Bundle create/update requests may provide `deploymentProcessRevisionId`. The repository validates that the revision belongs to a Deployment Process in the current organization and Release Bundle application, creates an immutable snapshot for that revision when necessary, and links the Release Bundle to the snapshot inside the same transaction.

`ProcessSnapshot` rows are unique by `deployment_process_revision_id`. Repeated requests for the same revision reuse the existing snapshot instead of creating duplicate snapshots.

The snapshot canonical payload stores deterministic JSON bytes and a SHA-256 checksum. The payload excludes mutable database timestamps and includes the Deployment Process revision identity, revision number, description, and ordered step content.

Expose a read-only endpoint:

```http
GET /api/v1/release-bundles/{releaseBundleId}/process-snapshot
```

The endpoint requires both `release_bundles` and `deployment_processes` experimental feature flags. It returns not found for missing, unlinked, or cross-organization Release Bundles.

Existing draft update semantics are preserved by retaining an existing snapshot link when an update omits `deploymentProcessRevisionId`. Supplying a different revision ID on a draft update replaces the link with that revision's snapshot. Existing non-draft mutation guards continue to apply.

## Consequences

- Release Bundles can reference immutable Deployment Process snapshots without adding planning or execution behavior.
- Cross-organization and cross-application process revision references return not found.
- The database prevents Release Bundles from linking to snapshots outside their organization/application.
- A Deployment Process revision referenced by a snapshot cannot be deleted through cascading process deletion.
- Existing Release Bundle clients that omit `deploymentProcessRevisionId` keep draft CRUD compatibility.
- Later roadmap PRs can add variables, deployment planning, approval, execution, and agent behavior against the immutable snapshot link.
