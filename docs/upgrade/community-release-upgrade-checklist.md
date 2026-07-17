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
