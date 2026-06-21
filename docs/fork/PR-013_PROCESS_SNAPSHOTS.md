# PR-013 Process Snapshots

## Scope

PR-013 adds immutable Deployment Process snapshots linked from Release Bundles.

Included:

- Optional `deploymentProcessRevisionId` on draft Release Bundle create/update requests.
- Idempotent immutable `ProcessSnapshot` creation for the referenced Deployment Process revision.
- Release Bundle `processSnapshotId` persistence and response exposure.
- Organization/application consistency checks for snapshot creation and Release Bundle links.
- Read-only `GET /api/v1/release-bundles/{releaseBundleId}/process-snapshot`.
- Backend, API, mapping, handler, repository, canonicalization, migration, Angular service, and live PostgreSQL tests.

Excluded:

- Variable sets, variable snapshots, or scoped-variable resolution.
- Step Template CRUD or built-in action registry behavior.
- Release promotion, deployment planning, approvals, task queues, retention, notifications, execution, or agent changes.
- Release Bundle UI process selectors or deployment workflow changes.

Those features remain PR-014 or later roadmap work.

## Feature Flags

Existing Release Bundle CRUD remains behind the `release_bundles` experimental feature flag.

The read-only process snapshot endpoint is additionally gated by `deployment_processes`, because it exposes Deployment Process revision content.

## Database

Migration `116_process_snapshots` adds:

- `ProcessSnapshot`
- nullable `ReleaseBundle.process_snapshot_id`

`ProcessSnapshot` stores:

- organization/application scope
- Deployment Process ID
- Deployment Process revision ID
- revision number
- canonical payload bytes
- canonical SHA-256 checksum

Snapshots are unique per Deployment Process revision, so repeated Release Bundle requests for the same revision reuse the same immutable snapshot.

Composite foreign keys enforce:

- the referenced Deployment Process belongs to the current organization/application
- the referenced revision belongs to that Deployment Process and organization
- a Release Bundle can link only to a snapshot in the same organization/application

The down migration removes PR-013 schema and repairs Release Bundle canonical payload/checksum values without the `processSnapshotId` field.

## API

Create/update Release Bundle requests may include:

```json
{
  "deploymentProcessRevisionId": "00000000-0000-0000-0000-000000000000"
}
```

When present, the revision must belong to a Deployment Process in the same organization and application as the Release Bundle. Missing, cross-organization, or cross-application revision references return `404 Not Found`.

Release Bundle responses include:

```json
{
  "processSnapshotId": "00000000-0000-0000-0000-000000000000"
}
```

Read-only snapshot endpoint:

```http
GET /api/v1/release-bundles/{releaseBundleId}/process-snapshot
```

The response includes the snapshot checksum and the immutable revision content.

Draft update semantics:

- Supplying a new `deploymentProcessRevisionId` links the draft to that revision's snapshot.
- Omitting `deploymentProcessRevisionId` preserves any existing snapshot link on that draft.
- Existing non-draft update/delete protections remain unchanged.

## Canonical Checksum

Process snapshot payloads are deterministic JSON bytes.

They include the application ID, Deployment Process ID, Deployment Process revision ID, revision number, description, and ordered steps including action metadata, inputs, conditions, Channel/Environment scopes, target tags, failure mode, timeout, retry counters, required permissions, sort order, and dependencies.

The stored checksum is `sha256:<hex>`.

Release Bundle canonical payloads now include `processSnapshotId` when a bundle is linked to a snapshot. Existing bundles without a snapshot omit the field for compatibility.

## Compatibility

Existing Environment, Lifecycle, Channel, deployment target, deployment, release-name, and agent behavior is unchanged.

Existing Release Bundle draft clients may continue omitting `deploymentProcessRevisionId`.

PR-013 adds no variables, release promotion, deployment planning, approval, retention, execution, notification, or agent behavior.
