# Community Release Upgrade Checklist

Use this checklist for an operator-controlled upgrade to the PR-050 release package.

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
