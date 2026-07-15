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

Protected resources now use `409 Conflict` consistently instead of returning a client error or reporting an
expected database restriction as a server fault.

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

## Known Limitations

- Config as Code does not sync, apply, poll, or reconcile a Git repository.
- Retention cleanup apply is not enabled by PR-045.
- Compatibility metadata does not fabricate historical process, variable, channel, environment, task, or log data.
- The deterministic advanced-flow verifier supplements the live Hub smoke test and API-only live release-to-task journey; run all layers before tagging.
- Feature flags are still experimental and should not be removed without a later stabilization PR.
