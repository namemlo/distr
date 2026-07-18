# Community Release Upgrade Checklist

Use this checklist for an operator-controlled upgrade to the PR-050 release package.

## Migration 138 Decision Path

This section overrides the generic migration order only when crossing from schema 137 to migration 138.

1. Run `distr migrate --check` while the current Hub is still running.
2. For proven zero history, stop/fence writers, verify they are stopped, back up PostgreSQL and object storage,
   verify both backups through isolated restore, run the explicit migration, and start only the expand-compatible
   Hub with `serve --migrate=false`.
3. For a non-empty schema 137 database, run `timestamp-expand-capture`. Keep the Hub stopped while an independent
   reviewer classifies every cell and seals the complete manifest.
4. Run `timestamp-expand-apply`; it revalidates the fence and evidence, performs a dry run, migrates to 138, applies
   and verifies provenance, checks startup compatibility, starts the digest-pinned Hub, and clears the fence only
   after health and history checks pass.
5. Retain the evidence directory and previous-known-good image until release acceptance.

Before migration 138 starts, `timestamp-expand-cancel` may resume the previous image only when schema 137 and the
captured database identity remain unchanged. After an `APPLIED` or `VERIFIED` manifest exists,
`migrate --to 137` is intentionally refused. Recovery then completes the expand release or restores the verified
database and object-store backups before the previous image resumes.

Unresolved cells remain fail-closed and do not block the additive expand schema. They do block the separately
planned contract release.

### Dirty migration 137/138 branch

If migration 138 leaves `schema_migrations` dirty at version 137 or 138, keep every Hub writer stopped and preserve
the active timestamp fence. Do not update `schema_migrations` by hand and do not invoke raw `migrate force`. Use only
the audited wrapper documented in the
[server Compose deployment runbook](../operations/server-docker-compose-deploy.md#audited-dirty-marker-recovery):

```bash
./deploy/server-docker-compose/deploy.sh \
  timestamp-expand-recover-dirty \
  <approved-manifest-or-> \
  <evidence-dir> \
  <operator-identity> \
  <reason>
```

The literal `-` means no manifest and is not standard input. Use it only for proven empty
`PREDECESSOR_137` history or durable `EXPAND_138` `ZERO_HISTORY`. Non-empty predecessor history requires the exact
`APPROVED` root document. `EXPAND_138` with pre-apply manifest-required history requires that same root document;
already verified evidence requires the exact `APPROVED` document whose content matches the stored `VERIFIED` tip.
Partial, mixed, unknown, and contract-gated catalogs remain refused.

Both `-` branches require the complete capture bundle and active `CAPTURED_WRITERS_STOPPED` fence to have existed
before migration. They do not recover an interrupted ordinary zero-history `release`, which creates neither record.
Do not reconstruct them after failure; keep writers stopped and restore the verified backup or escalate.

Recovery repairs only the marker selected by the catalog: `PREDECESSOR_137` selects clean 137 and `EXPAND_138`
selects clean 138. The observed dirty version does not choose the target. Recovery never starts Hub, persists
compatibility, or clears the fence. After empty-history clean 137, run normal `timestamp-expand-cancel` after its
unchanged-schema checks pass to exit the staged fence. Cancel persists the previous/source image identity. Stop there
for rollback; to proceed forward, copy the exact target `DISTR_IMAGE_REF`, `DISTR_RELEASE_COMMIT`, and
`DISTR_IMAGE_DIGEST` together from the original immutable handoff into `.env`, run `check`, then restart ordinary
zero-history `release`. After non-empty clean 137, rerun normal `timestamp-expand-apply` with the same approved
manifest and evidence. After clean 138 with manifest evidence, rerun normal `timestamp-expand-apply` with identical
approved content and evidence. After clean 138 `ZERO_HISTORY`, stop and escalate because no current no-manifest
finalizer exists.

Retain the checksummed recovery plan, result, and every numbered interrupted-result archive. Every retry must use
the same manifest mode and exact content/checksum, evidence directory, non-secret operator identity, quoted
non-secret reason, and active fence. The source manifest pathname may differ only when the approved bytes and staged
checksum remain identical.

## Before Upgrade

- Back up PostgreSQL.
- Export current feature flags and environment variables.
- Record Hub and agent versions.
- Run `hack/validate-migrations.sh`.
- Run a dry-run legacy compatibility inspection where PR-049 metadata is needed.
- Confirm live PostgreSQL tests have passed in the release candidate environment.

## Upgrade Order

1. Stop background workers if the deployment environment separates workers from the Hub.
2. Apply schema migrations.
3. Start the new Hub.
4. Confirm `/ready` and `/docs/openapi.json`.
5. Keep existing agents running for direct deployment compatibility.
6. Upgrade agents only after their required capabilities are documented for the target flows.
7. Run the operator smoke test.
8. Run optional PR-049 compatibility backfill with `--apply` only after reviewing dry-run counts.

## Recovery

- Prefer forward fixes for partially completed compatibility metadata.
- Re-run idempotent backfills instead of editing metadata manually.
- Keep original deployment and deployment revision rows intact.
- Do not attempt automatic reverse migration for data that an older binary cannot understand.

## Downgrade Limits

Downgrade is limited when:

- new schema objects are used by the running Hub;
- compatibility backfill metadata has been consumed by timeline consumers;
- feature-flagged resources were created after the older version was deployed.

If downgrade is required, validate it against a restored backup before touching production data.
