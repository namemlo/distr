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

## Timestamp Expand Smoke

After `timestamp-expand-apply`, run:

```bash
: "${DISTR_TIMESTAMP_EVIDENCE_DIR:?export the retained timestamp evidence directory}"
export DISTR_TIMESTAMP_APPROVED_MANIFEST="${DISTR_TIMESTAMP_APPROVED_MANIFEST:-$DISTR_TIMESTAMP_EVIDENCE_DIR/approved-manifest.json}"
if ! DISTR_TIMESTAMP_MANIFEST_ID="$(
  jq -er \
    '.id | strings | select(test("^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"))' \
    "$DISTR_TIMESTAMP_APPROVED_MANIFEST" 2>/dev/null
)"; then
  printf '%s\n' "approved timestamp manifest is missing a valid top-level id" >&2
  exit 1
fi
export DISTR_TIMESTAMP_MANIFEST_ID
./deploy/server-docker-compose/deploy.sh health
./deploy/server-docker-compose/deploy.sh ps
export DISTR_COMPOSE_ENV_FILE="$(realpath deploy/server-docker-compose/.env)"
docker compose \
  --env-file "$DISTR_COMPOSE_ENV_FILE" \
  -f deploy/server-docker-compose/docker-compose.yml \
  --profile timestamp-operator \
  run --rm --user "$(id -u):$(id -g)" timestamp-operator \
  external-execution-timestamps verify \
  --manifest-id "$DISTR_TIMESTAMP_MANIFEST_ID"
```

Confirm all of the following before accepting the release:

- `/ready` succeeds and the running Hub uses the reviewed immutable image digest;
- `schema_migrations` reports clean schema 138;
- the manifest state is `VERIFIED`, or the durable state proves zero history;
- execution and event counts, IDs, statuses, sequences, messages, hashes, and references match pre-release evidence;
- every converted shadow reproduces from its raw value and explicit offset;
- unresolved cells remain null and visible as `UNRESOLVED`;
- no duplicate callback sequence or unexpected task lock exists;
- login, execution-history reads, task progress, and audit views remain available; and
- the evidence directory, backup checksums, restore checksums, fence record, and previous image digest are retained.

This smoke test does not authorize deleting execution history, provenance, audit records, or timestamp evidence.

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
