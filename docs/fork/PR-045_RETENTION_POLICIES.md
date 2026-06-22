# PR-045 - Retention Policies

## Summary

PR-045 adds a feature-flagged retention-policy backend surface for release and task history cleanup planning. The first slice is intentionally preview-first: API callers can create policies, preview cleanup candidates, and record dry-run cleanup jobs, but destructive apply is rejected until a later reviewed implementation adds deletion.

## Feature Flag

- `retention_policies`
- API access also requires `task_queue`, `release_bundles`, and `environments`.

## Database

- Adds `RetentionPolicy` for organization-scoped retention settings.
- Adds `ReleaseBundle.retention_protected` for future manual protection of important releases.
- Adds `RetentionCleanupJob` for immutable dry-run cleanup plans and counts.

## API

- `GET /api/v1/retention-policies`
- `POST /api/v1/retention-policies`
- `GET /api/v1/retention-policies/{retentionPolicyId}`
- `POST /api/v1/retention-policies/{retentionPolicyId}/preview`
- `POST /api/v1/retention-policies/{retentionPolicyId}/cleanup-jobs`

## Preview Rules

- Release candidates are releases outside the configured last-N successful release window.
- Releases currently deployed to any target are safety-blocked when protection is enabled.
- Releases marked `retention_protected` are safety-blocked when protection is enabled.
- Failed or canceled deployment tasks become candidates after the configured retention window.
- Production environments can use a longer failed-task retention window than non-production environments.
- Step log chunks are grouped by task once they exceed the configured log retention window.

## Safety Behavior

- Cleanup jobs are dry-run only in this PR.
- Requests with `dryRun=false` are rejected.
- Audit-retention settings are stored on the policy but audit deletion is not implemented in this PR.
- Component digest metadata is not deleted by this PR.

## Compatibility

Existing deployment, release-bundle, task-queue, task-lease, agent, runbook, deployment-timeline, rolling, blue-green, and traffic-provider behavior is unchanged unless the `retention_policies` experimental flag is enabled and callers use the new endpoints.

## Verification

- Repository tests cover release candidates, currently-deployed safety blocks, failed-task candidates, dry-run cleanup jobs, and apply rejection.
- Handler tests cover request normalization, malformed IDs, and feature-flag rejection.
