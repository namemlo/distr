# Fork Upgrade Guide

## Current Timestamp Procedure

Migration 138 uses the current
[Community Release Upgrade Checklist](../upgrade/community-release-upgrade-checklist.md#migration-138-decision-path).
The remainder of this file is the historical PR-049/schema-131 compatibility guide and does not override the
migration-138 fence, manifest, backup, restore, or downgrade rules.

If migration 138 is dirty at marker 137 or 138, follow the checklist's
[dirty migration branch](../upgrade/community-release-upgrade-checklist.md#dirty-migration-137138-branch) and the
[audited recovery decision tree](../operations/server-docker-compose-deploy.md#audited-dirty-marker-recovery).
`timestamp-expand-recover-dirty` repairs only the catalog-proven marker. It does not start Hub, persist timestamp
compatibility, or clear the fence. Resume the applicable normal timestamp-expand flow afterward; a repaired
`EXPAND_138`/`ZERO_HISTORY` state remains stopped and fenced and must be escalated because no current no-manifest
finalizer exists. The no-manifest branch requires a timestamp fence and complete capture bundle that predate
migration; an interrupted ordinary zero-history release without them requires verified restore or escalation.

## PR-049 Compatibility Metadata

Supported source range: any fork build before PR-049 that has direct `Deployment` and `DeploymentRevision`
history and can migrate through schema version `131`.

1. Back up the database.
2. Deploy the PR-049 Hub binary.
3. Run database migrations.
4. Run a dry run:

   ```sh
   distr backfill-legacy-deployments --organization-id <org-id>
   ```

5. Review `scanned`, `eligible`, `projected`, `alreadyPresent`, `skipped`, `failed`, and `lastCursor`.
6. Apply when ready:

   ```sh
   distr backfill-legacy-deployments --organization-id <org-id> --apply --batch-size 500
   ```

7. Resume with the reported cursor if needed:

   ```sh
   distr backfill-legacy-deployments \
     --organization-id <org-id> \
     --apply \
     --cursor-created-at <timestamp> \
     --cursor-revision-id <deployment-revision-id>
   ```

## Verification

Use operator SQL or repository checks to confirm:

```sql
SELECT count(*) FROM DeploymentRevision dr
JOIN Deployment d ON d.id = dr.deployment_id
JOIN DeploymentTarget dt ON dt.id = d.deployment_target_id
WHERE dt.organization_id = '<org-id>';

SELECT count(*) FROM DeploymentCompatibilityMetadata
WHERE organization_id = '<org-id>';
```

Timeline reads continue to work while the backfill is incomplete. Rows without stored compatibility metadata can
still be projected on read paths added by PR-049.

## Compatibility

Old agents and existing deployment APIs keep their current behavior. PR-049 does not change lease payloads,
deployment execution, direct deployment endpoints, or stored deployment revision values.

Compatibility metadata can be removed without deleting original deployment history:

```sql
DELETE FROM DeploymentCompatibilityMetadata WHERE organization_id = '<org-id>';
```

Historical process snapshots, variable snapshots, channels, environments, actors, and logs are not reconstructed
from present mutable state. Legacy timeline entries report those dimensions as unavailable.

## Downgrade

Downgrade requires stopping the PR-049 binary, deleting compatibility metadata if desired, and running the down
migration for schema `131`. Original deployment history remains in `Deployment` and `DeploymentRevision`.

## Troubleshooting

- Duplicate rows: rerun with `--apply`; inserts are idempotent by organization and deployment revision.
- Invalid historical references: fix the source row or leave it skipped; do not guess missing channel or
  environment history.
- Interrupted batches: rerun from `lastCursor`.
- Partial batches: timeline remains readable and existing deployment execution is unchanged.
