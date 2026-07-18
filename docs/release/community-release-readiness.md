# Community Release Readiness

This document is the release-readiness package for the roadmap through PR-050. It is community-neutral and
does not depend on adopter-specific infrastructure.

## Release Notes

The community fork adds release-management primitives on top of existing Distr deployment behavior:

- release bundles and publication checks;
- environments, lifecycles, and channels;
- deployment processes, snapshots, plans, tasks, locks, leases, and step events;
- scoped variables, drift views, approvals, rollout groups, guided failure, freezes, subscriptions, runbooks,
  retention, expanded RBAC, observability, Config as Code validation, and compatibility metadata.

Existing direct application-version deployments remain supported.

### Protected-delete API compatibility

The four protected-reference cases below now use `409 Conflict` instead of the previous generic `400`/`500`
responses.

| Delete operation                                             | Previous protected-reference response | New response                                                             |
| ------------------------------------------------------------ | ------------------------------------- | ------------------------------------------------------------------------ |
| `DELETE /api/v1/applications/{applicationId}`                | `400 Bad Request`                     | `409`, `text/plain; charset=utf-8`, body `application is in use\n`       |
| `DELETE /api/v1/deployment-targets/{deploymentTargetId}`     | `500 Internal Server Error`           | `409`, `text/plain; charset=utf-8`, body `deployment target is in use\n` |
| `DELETE /api/v1/artifacts/{artifactId}`                      | `500 Internal Server Error`           | `409`, `text/plain; charset=utf-8`, body `artifact is in use\n`          |
| `DELETE /api/v1/secrets/{secretId}` for a database reference | `500 Internal Server Error`           | `409`, `text/plain; charset=utf-8`, body `secret is in use\n`            |

Artifact entitlement protection is unchanged: it remains `400 Bad Request` with
`text/plain; charset=utf-8` and body
`bad request: Cannot delete artifact: it is referenced in one or more entitlements.\n`.

Secret deletion has two intentional `409` response media types:

| Protection source           | Content type                | Response body                   |
| --------------------------- | --------------------------- | ------------------------------- |
| Affected deployed workloads | `application/json`          | `{"affectedDeployments":[...]}` |
| Another database resource   | `text/plain; charset=utf-8` | `secret is in use\n`            |

Secret API clients must branch on the response `Content-Type` before decoding a `409`; they must not assume every
Secret conflict is JSON. Unexpected Application delete failures remain `500 Internal Server Error` but now return
the generic body `Internal Server Error\n` instead of exposing database details.

## Feature-Flag Inventory

Keep experimental flags enabled only for the surfaces being evaluated.

| Flag                        | Area                       | Release note                                                             |
| --------------------------- | -------------------------- | ------------------------------------------------------------------------ |
| `environments`              | Environments               | Required by lifecycle and release workflows.                             |
| `lifecycles`                | Lifecycles                 | Requires environments for phase selection.                               |
| `channels`                  | Channels                   | Used by release rules and Config as Code examples.                       |
| `release_bundles`           | Release bundles            | Required by CI release API and release UI.                               |
| `deployment_processes`      | Deployment processes       | Required by process revisions and planning.                              |
| `scoped_variables_v2`       | Variable sets and resolver | Required by snapshots, drift, and planning.                              |
| `deployment_plans`          | Deployment plans           | Required by plan preview and task creation.                              |
| `task_queue`                | Durable tasks              | Required by task execution.                                              |
| `agent_capabilities`        | Agent capabilities         | Required when advanced plans validate target action support.             |
| `agent_task_leases`         | Agent task leases          | Required when agents claim queued tasks.                                 |
| `step_events`               | Step events                | Required for task timeline, logs, and bounded outputs.                   |
| `deployment_timeline`       | Timeline and compare       | Requires advanced task and compatibility data.                           |
| `retention_policies`        | Retention                  | Preview and dry-run only until destructive apply is separately reviewed. |
| `observability_metrics`     | Metrics                    | Requires `METRICS_ENABLED=true` for metrics exposure.                    |
| `observability_tracing`     | Tracing                    | Emits spans only when enabled.                                           |
| `observability_dashboards`  | Dashboard catalog          | Static dashboard catalog API.                                            |
| `observability_correlation` | Correlation links          | Adds link and query templates where supported.                           |
| `config_as_code`            | Config as Code             | Validation and authority guards only; no sync/apply workflow.            |

## Compatibility Matrix

| Area                           | Supported                                  | Notes                                                                                                                                                |
| ------------------------------ | ------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| Existing direct deployments    | Yes                                        | Existing deployment API and agent behavior remain supported.                                                                                         |
| Legacy timeline visibility     | Yes                                        | PR-049 projects legacy deployments as `legacy_deployment` timeline entries.                                                                          |
| Advanced release flows         | Experimental                               | Use the feature flags listed above.                                                                                                                  |
| Previous agents                | Supported for existing deployment behavior | New advanced task actions require advertised capabilities.                                                                                           |
| PostgreSQL migrations          | Forward                                    | Validate migrations and back up before upgrade.                                                                                                      |
| Downgrade after data backfill  | Limited                                    | Schema rollback and data rollback are different operations; do not promise automatic reverse migration.                                              |
| Provider-neutral demo          | Yes                                        | See `examples/community-e2e/`; live mode uses isolated Compose dependencies and an API-only release-to-task journey through Hub and agent endpoints. |
| Provider-specific integrations | Out of scope                               | Keep cloud, CI, and traffic-provider examples outside core unless generic.                                                                           |

## Release-Execution Checkpoints

Follow the
[authoritative release-execution matrix](../operations/server-docker-compose-deploy.md#authoritative-release-execution-matrix)
once per completed checkpoint/PR, not per edit. Focused preflight precedes the exact-tree commit; that commit is built
once into an immutable ECR digest tied to its OCI revision and platform. Validate that same digest on an isolated
server runtime and PostgreSQL 18 clone, without live or client mutation, then require the functional, migration,
dirty-recovery, backup/restore, platform, and dependency gates.

Isolated validation and ECR publication are not live promotion. Promotion is a separate recorded action using only
the proven digest and only after environment policy, required authorization, and live preconditions pass; follow it
with running-digest, schema, health/readiness, and audit verification. A failed checkpoint requires a new commit and
digest. Never overwrite or reuse a candidate tag as release identity. The current candidate is **NOT LIVE PROMOTED**.

## External-Execution Timestamp Expand Gate

Migration 138 is a Distr control-plane expand release. It retains every legacy external-execution timestamp, adds
nullable instant shadows, immutable provenance, and authorized append-only deletion tombstones, and leaves public
API fields and agent behavior unchanged. Organization retention may remove source rows only while atomically
recording the complete tombstone set; unexplained source loss remains fail-closed at readiness. A later reviewed
manifest may resolve a retained unresolved cell as provenance-only evidence without changing its tombstone or
creating a live shadow, but only when deletion predates the promoting manifest's applied time. Readiness reports
resolved/unresolved live shadows separately from resolved/unresolved deleted evidence. Downgrade is refused after
retention, after any post-expand `ZERO_HISTORY` row, or after manifest application; migration 138 down repeats those
checks under exclusive source/evidence table locks.
PostgreSQL compatibility is gated on the exact images `postgres:16.14-alpine3.23` and
`postgres:18.4-alpine3.23`.

Before deployment, retain one release record containing:

- source commit and reviewed change range;
- immutable Hub image digest;
- schema version before and after deployment;
- manifest ID, raw-cell checksum, decision-content checksum, and database-identity checksum;
- database and object-store backup checksums;
- isolated-restore verification checksums;
- component release identity, dependency manifest identity, operator, reviewer, and timestamps; and
- previous-known-good image digest and recovery evidence.

A component release never implicitly deploys another component. Each component has its own immutable release and
change log. A coordinated rollout uses an explicit dependency DAG or product manifest whose reviewed entries name
the exact component releases; dependency relationships never trigger hidden deployments.

The migration decision path is fixed:

1. Run the read-only `distr migrate --check`.
2. If the result proves zero external-execution history, the ordinary release may stop writers, back up, run the
   explicit migration, and start the Hub with `serve --migrate=false`.
3. If the database is non-empty at exact schema 137, use `timestamp-expand-capture`, retain the backup and isolated
   restore evidence, independently review and seal the complete manifest, then use `timestamp-expand-apply`.
4. Require schema 138, a clean migration state, a `VERIFIED` manifest or durable zero-history proof, and a matching
   image digest. `timestamp-expand-apply` runs its embedded isolated-acceptance and final-Hub readiness, integrity,
   audit, count, lock, sequence, and image gates while the writer fence remains held; it clears the fence only after
   every embedded gate passes.
5. The dedicated operator smoke test runs after apply returns and the fence has been safely cleared. It provides
   post-apply release-acceptance evidence and does not replace or weaken the embedded pre-clear gates.

An interrupted migration is not release acceptance. For a dirty marker at version 137 or 138, the only permitted
repair is the audited `timestamp-expand-recover-dirty` wrapper with the active fence, exact fenced image and evidence
bundle, catalog-selected force target, and either the exact approved manifest or the literal no-manifest sentinel
`-`. Direct `schema_migrations` edits and raw `migrate force` are release blockers.

Before release readiness can continue after recovery, retain and validate:

- `timestamp-dirty-recovery-plan.json` and its checksum sidecar;
- `timestamp-dirty-recovery-result.json` and its checksum sidecar;
- every `timestamp-dirty-recovery-result.interrupted-NNN.partial` archive and sidecar;
- the unchanged active fence, exact evidence-bundle checksum, and exact target image identity; and
- the same manifest mode and exact approved content/checksum, non-secret operator identity, and non-secret reason on
  every retry. The source pathname may differ only when the staged approved bytes remain identical.

No-manifest recovery requires a complete capture bundle and active timestamp fence that predate migration. It cannot
be retrofitted after an interrupted ordinary zero-history `release`; without those records, keep writers stopped and
restore the verified backup or escalate.

A valid retained result avoids another recovery Apply, but it still does not finalize the release. Recovery performs
marker repair only: Hub remains stopped, compatibility is not persisted, and the fence is not cleared. A clean
empty-history `PREDECESSOR_137` outcome must run normal `timestamp-expand-cancel` after its unchanged-schema checks
pass to exit the staged fence. Cancel persists the source image identity. Stop there for rollback; to proceed forward,
restore the exact target `DISTR_IMAGE_REF`, `DISTR_RELEASE_COMMIT`, and `DISTR_IMAGE_DIGEST` together from the original
immutable handoff, run `check`, then restart ordinary zero-history `release`. A non-empty predecessor outcome must
rerun normal `timestamp-expand-apply` with the same approved root evidence. A clean `EXPAND_138` outcome with approved
manifest evidence must rerun normal `timestamp-expand-apply` with identical approved content and evidence. A clean
`EXPAND_138` durable `ZERO_HISTORY` outcome remains stopped and fenced and requires escalation because no current
no-manifest finalizer exists.

`UNRESOLVED` cells remain visible with null shadows and fail closed. Expand acceptance does not claim contract
eligibility and does not authorize a contract migration.

The transaction-local retention context assumes trusted database-owner credentials and prevents accidental
application-path deletion; it is not a database privilege boundary. Least-privilege runtime/migrator roles remain
deferred hardening. Timestamp-retention compatibility also does not resolve the separate modern organization purge
ordering blocker reproduced at `deploymentplantarget_target_fk`.

## Release Gate Checklist

- `DISTR_DEMO_DISPOSABLE_HUB=true node examples/community-e2e/live-demo.mjs --require-running-hub`
- `node hack/pr050-validate-release-hardening.mjs`
- `node hack/pr050-license-scan.mjs` after `pnpm install --frozen-lockfile` and Go modules are available
- `go test -p=1 ./...`
- `go vet ./...`
- `golangci-lint run --config=.golangci.release.yml ./...`
- `pnpm run lint`
- live PostgreSQL-backed Go integration tests with `DISTR_TEST_DATABASE_URL`; the API-only live demo does not read DB credentials
- all Angular tests and community frontend build
- community Hub, Docker agent, and Kubernetes agent builds
- migration-pair validation
- dependency vulnerability scan
- Node package and Go module license scan
- secret scan
- documentation link and example validation
- operator smoke test
- retained dirty-recovery evidence plus successful normal finalization, when marker repair was required

## Known Limitations

- Config as Code does not sync, apply, poll, or reconcile a Git repository.
- Retention cleanup apply is not enabled by PR-045.
- Compatibility metadata does not fabricate historical process, variable, channel, environment, task, or log data.
- The deterministic advanced-flow verifier supplements the live Hub smoke test and API-only live release-to-task journey; run all layers before tagging.
- Feature flags are still experimental and should not be removed without a later stabilization PR.
