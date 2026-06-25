# Operator Smoke Test

This checklist verifies a release candidate with local infrastructure.

## Preflight

- Back up the database.
- Record current Hub and agent versions.
- Record enabled experimental feature flags.
- Confirm `DATABASE_URL`, `JWT_SECRET`, `DISTR_HOST`, storage, and email settings.
- Confirm no production secret values are copied into local demo files.

## Local Infrastructure

Start the normal development dependencies:

```shell
docker compose up -d
```

Build and start the Community Hub:

```shell
mise run build:hub:community
dist/distr serve
```

Verify readiness and OpenAPI output:

```shell
curl -sf http://localhost:8080/ready
curl -sf http://localhost:8080/docs/openapi.json -o /tmp/distr-openapi.json
```

## Smoke Journey

Run:

```shell
node examples/community-e2e/live-demo.mjs --require-running-hub
```

Then manually verify:

- registration and login work;
- organization and application setup works;
- deployment target creation works;
- existing direct deployment behavior still works;
- release bundle, process, plan, task, and timeline pages remain feature-flagged as expected;
- logs and events do not reveal secret values;
- cleanup removes demo-created state.

## Upgrade and Rollback Notes

Schema rollback, application binary rollback, and data backfill recovery are separate decisions.

- A binary rollback is acceptable only when the older binary can read the current schema.
- A schema rollback is acceptable only before forward-only data changes have been depended on.
- PR-049 compatibility metadata can be removed without deleting original deployment history.
- Backfill recovery should be forward-fix-first unless a tested down migration is known to preserve required data.

## Exit Criteria

- The live community demo command passes.
- No demo data remains after cleanup.
- No plaintext secret values appear in logs, task events, leases, or local output files.
- Accepted scanner findings are recorded in the release notes.
