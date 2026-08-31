# Operator Control-Plane API

This guide maps the operator workflow to the additive API surface. The generated
OpenAPI document at `/docs/openapi.json` is authoritative for request and
response schemas.

## Access and gates

Operator routes require authenticated vendor organization context and enforce
organization scope before reading or mutating records. Mutations additionally
require the route's scoped permission and role; super-admin mutation is blocked
where separation of duties requires an organization actor.

The v2 surface is default-off:

- `operator_control_plane_v2` gates the umbrella control-plane capability.
- `executor_protocol_v2` additionally gates v2 execution controls.
- organization/environment enrollment controls scoped eligibility.
- domain prerequisites such as release bundles, deployment plans, processes,
  variables, channels, lifecycles, and environments remain effective.

When a required process flag is disabled, gated routes fail closed, commonly as
`404`. Disabling v2 does not delete history or change untouched v1 behavior.

## Common conventions

- All user/operator routes are below `/api/v1`.
- Path IDs are UUIDs and are always resolved inside the authenticated
  organization.
- Published release, config, plan, campaign, approval, and evidence inputs are
  checksum-bound.
- Mutations use idempotency/optimistic concurrency where the domain contract
  defines it. A conflicting duplicate returns the current conflict; it does not
  overwrite an immutable revision.
- Operator read lists use filter-bound descending keyset cursors, default page
  size 50, and maximum 100.
- Empty, partial, stale, unknown, and permission-denied states are distinct from
  success.
- Credentials and secret values never belong in request examples or evidence.
  Use opaque `secret:` references and non-reversible fingerprints.

## Read-model API

The read-only operator surface is rooted at:

```text
/api/v1/control-plane
```

| Method | Route                                            | Purpose                                                                                 |
| ------ | ------------------------------------------------ | --------------------------------------------------------------------------------------- |
| `GET`  | `/fleet`                                         | Deployment units, component placements, desired/observed state, health, drift, coverage |
| `GET`  | `/releases`                                      | Bounded Component/Product Release list                                                  |
| `GET`  | `/releases/{releaseId}`                          | Release detail                                                                          |
| `GET`  | `/releases/{releaseId}/evidence`                 | Supply-chain and checksum evidence                                                      |
| `GET`  | `/releases/{releaseId}/compare/{otherReleaseId}` | Exact release comparison                                                                |
| `GET`  | `/plans`                                         | Bounded deployment-plan list                                                            |
| `GET`  | `/plans/{planId}`                                | Baseline, change set, DAG, blockers, approvals                                          |
| `GET`  | `/plans/{planId}/evidence`                       | Plan evidence                                                                           |
| `GET`  | `/plans/{planId}/compare/{otherPlanId}`          | Exact plan comparison                                                                   |
| `GET`  | `/campaigns`                                     | Campaign revisions/runs and current state                                               |
| `GET`  | `/campaigns/{campaignId}`                        | Waves, members, thresholds, prerequisites                                               |
| `GET`  | `/campaigns/{campaignId}/evidence`               | Campaign evidence                                                                       |
| `GET`  | `/executions`                                    | Execution/attempt state and uncertainty                                                 |
| `GET`  | `/executions/{executionId}`                      | Step DAG, locks, events, observations                                                   |
| `GET`  | `/executions/{executionId}/evidence`             | Execution evidence                                                                      |
| `GET`  | `/reconciliation`                                | Drift and unknown cases                                                                 |
| `GET`  | `/reconciliation/{reconciliationId}`             | Reconciliation detail                                                                   |
| `GET`  | `/reconciliation/{reconciliationId}/evidence`    | Reconciliation evidence                                                                 |
| `GET`  | `/audit`                                         | Correlated audit list                                                                   |
| `GET`  | `/audit/{auditEventId}`                          | Audit-event detail                                                                      |
| `GET`  | `/audit/{auditEventId}/evidence`                 | Correlated evidence                                                                     |

Read models do not become execution inputs and do not copy write authority from
the canonical domain records.

## Workflow route families

### Registry and setup

`/api/v1/deployment-registry` provides import preview/apply and
organization-scoped coverage, scopes, assignments, units, subscribers,
definitions, aliases, instances, and placement reads. Use it to account for
every placement before planning.

Adapter implementations/assignments, observer registrations, policies,
calendars, freezes, environments, and authorization have their own `/api/v1`
route families. The generated OpenAPI document lists their exact schemas.

### Releases and configuration

| Route family               | Principal operations                                                    |
| -------------------------- | ----------------------------------------------------------------------- |
| `/release-bundles`         | List/get, validate, publish, block, archive Component Release contracts |
| `/product-releases`        | Create/get, validate capability graph, inspect graph, publish           |
| `/target-config-snapshots` | Create/list/get/verify immutable target config metadata                 |

Publication records immutable target-neutral releases. It does not deploy.

Product Release lifecycle operations are:

| Method | Route                                           | Purpose                                       |
| ------ | ----------------------------------------------- | --------------------------------------------- |
| `POST` | `/product-releases`                             | Create a target-neutral Product Release draft |
| `GET`  | `/product-releases/{productReleaseId}`          | Read the immutable manifest or current draft  |
| `POST` | `/product-releases/{productReleaseId}/validate` | Validate the capability graph                 |
| `GET`  | `/product-releases/{productReleaseId}/graph`    | Inspect the frozen capability graph           |
| `POST` | `/product-releases/{productReleaseId}/publish`  | Publish and freeze a valid Product Release    |

### Plans and approvals

| Method      | Route                                      | Purpose                                     |
| ----------- | ------------------------------------------ | ------------------------------------------- |
| `POST`      | `/deployment-plan-drafts`                  | Create mutable plan draft                   |
| `GET/PATCH` | `/deployment-plan-drafts/{id}`             | Inspect/edit with optimistic revision       |
| `POST`      | `/deployment-plan-drafts/{id}/validate`    | Resolve exact target preview and blockers   |
| `POST`      | `/deployment-plan-drafts/{id}/publish`     | Publish immutable plan/checksum             |
| `GET`       | `/deployment-plans`                        | List published plans                        |
| `GET`       | `/deployment-plans/{id}`                   | Get one plan                                |
| `POST`      | `/deployment-plans/{id}/approval-requests` | Request checksum-bound approval             |
| `POST`      | `/deployment-plans/{id}/previous-state`    | Create a new compatible previous-state plan |
| `POST`      | `/deployment-plans/{id}/tasks`             | Create durable tasks for a ready plan       |
| `GET/POST`  | `/deployment-plans/{id}/review-decisions`  | Read or append observed-state-bound GO/NO_GO evidence |
| `GET`       | `/approval-requests`                       | List approval work                          |
| `GET`       | `/approval-requests/{id}`                  | Get immutable request/decision evidence     |
| `POST`      | `/approval-requests/{id}/decisions`        | Append an authorized decision               |

Any material release, config, provider, migration, baseline, policy, or campaign
change invalidates reuse of the old approval.

Protocol-v2 task creation also requires the latest persistent review decision
to be an unexpired `GO`. The decision binds the plan and the current
independently observed-state checksum. A later `NO_GO`, supersession,
revocation, observed-state change, expiry, or failed authorization recheck
blocks before task or external mutation.

The standard controlled-client policy requires four-eyes approval: the
execution requester cannot decide the same plan or campaign approval. A
distinct authorized organization actor must append the decision. Preparing
plans or reading evidence does not authorize mutation of a client runtime,
client workload database, or companion-service runtime; each such scope must be
named in the separately approved execution boundary.

### Campaigns

| Method      | Route                                        | Purpose                                           |
| ----------- | -------------------------------------------- | ------------------------------------------------- |
| `POST`      | `/deployment-campaign-drafts`                | Create campaign draft                             |
| `GET/PATCH` | `/deployment-campaign-drafts/{id}`           | Inspect/edit draft                                |
| `POST`      | `/deployment-campaign-drafts/{id}/validate`  | Validate membership, waves, prerequisites, policy |
| `POST`      | `/deployment-campaign-drafts/{id}/publish`   | Publish immutable revision                        |
| `POST`      | `/deployment-campaign-runs`                  | Start an eligible campaign run                    |
| `GET`       | `/deployment-campaign-runs/{id}`             | Get run state                                     |
| `POST`      | `/deployment-campaign-runs/{id}/transitions` | Apply an allowed state transition                 |
| `POST`      | `/deployment-campaigns/{id}/pause`           | Stop new admissions                               |
| `POST`      | `/deployment-campaigns/{id}/resume`          | Resume persisted work after gate evaluation       |
| `POST`      | `/deployment-campaigns/{id}/retry`           | Retry an eligible member under protocol rules     |
| `POST`      | `/deployment-campaigns/{id}/exclude`         | Exclude with retained reason/history              |
| `POST`      | `/deployment-campaigns/{id}/cancel`          | Request typed cancellation                        |

For every `/deployment-campaigns/{id}/*` control route, `{id}` is the
campaign-run ID returned by `POST /deployment-campaign-runs`; it is not a
campaign draft or immutable campaign-revision ID.

Control requests are authorized, idempotent, and audited. Pause does not
misreport in-flight work as cancelled.

### Execution controls

Operator v2 controls require both process flags:

| Method | Route                                             | Purpose                                      |
| ------ | ------------------------------------------------- | -------------------------------------------- |
| `POST` | `/executions/{executionId}/cancel`                | Request cancellation for cancellable steps   |
| `POST` | `/executions/{executionId}/status-queries`        | Request fresh adapter status                 |
| `POST` | `/executions/{executionId}/reconciliation-events` | Import authenticated reconciliation evidence |

Executor claim, acknowledgement, heartbeat, ordered events, completion,
cancel/status acknowledgement, and status response use the separately
authenticated `/api/executor/v2` surface. Operators must not call executor
routes with user credentials.

### Observation and reconciliation

| Method     | Route                              | Purpose                                             |
| ---------- | ---------------------------------- | --------------------------------------------------- |
| `POST`     | `/api/observer/v1/observations`    | Ingest independently authenticated runtime evidence |
| `POST/GET` | `/api/v1/observer-registrations`   | Register/list observer trust boundaries             |
| `GET`      | `/api/v1/observations`             | List retained observations                          |
| `GET`      | `/api/v1/drift-cases`              | List drift and unknown cases                        |
| `POST`     | `/api/v1/drift-cases/{id}/resolve` | Record approved reconciliation action               |
| `GET`      | `/api/v1/reconciliation-actions`   | List reconciliation actions                         |

Executor completion is provisional until the required independent observation
matches the pending desired revision.

### Audit and evidence export

| Method     | Route                                          | Purpose                                         |
| ---------- | ---------------------------------------------- | ----------------------------------------------- |
| `GET`      | `/api/v1/control-plane-audit/events`           | Paginated correlated events                     |
| `POST`     | `/api/v1/control-plane-audit/evidence-bundles` | Deterministic checksum-bound bundle             |
| `GET/POST` | `/api/v1/control-plane-audit/export-sinks`     | Inspect/register allowlisted sink configuration |
| `GET`      | `/api/v1/control-plane-audit/export-status`    | Check checkpoint, lag, attempts, failures       |

Failed export does not delete primary events or advance the checkpoint.

### Sample retirement

Register the append-only ownership and recovery evidence before creating a
preview:

| Method | Route                                                               | Purpose                                               |
| ------ | ------------------------------------------------------------------- | ----------------------------------------------------- |
| `POST` | `/api/v1/sample-retirement-evidence/ownership`                      | Register ownership evidence for exact sample IDs      |
| `POST` | `/api/v1/sample-retirement-evidence/recovery`                       | Register backup or restore-proof evidence             |
| `POST` | `/api/v1/sample-retirements/preview`                                | Create an immutable exact-ID preview                  |
| `GET`  | `/api/v1/sample-retirements/{sampleRetirementId}`                   | Inspect the preview, evidence, and current state      |
| `POST` | `/api/v1/sample-retirements/{sampleRetirementId}/approval-requests` | Request checksum-bound approval                       |
| `POST` | `/api/v1/sample-retirements/{sampleRetirementId}/apply`             | Apply an approved preview                             |
| `POST` | `/api/v1/sample-retirements/{sampleRetirementId}/verify`            | Verify counts, tombstones, and retained audit history |

Sample retirement is not a general retention API and never deletes application
audit events. Follow
[Sample Domain Retirement](../operations/sample-domain-retirement.md).

## Standard API sequence

```text
registry coverage
  -> component/product release validation and publication
  -> target config snapshot
  -> plan draft validate/publish
  -> approval request/decision
  -> campaign draft validate/publish/start
  -> execution events/status controls
  -> independent observation
  -> drift/reconciliation when needed
  -> audit evidence bundle
```

Each response must be checked for immutable IDs/checksums and explicit blockers.
An HTTP success from the executor is not deployment success without the
required observation.

## Error and uncertainty handling

| Outcome                | Meaning                                                                 |
| ---------------------- | ----------------------------------------------------------------------- |
| `400`                  | Invalid request shape or unsupported selector/state                     |
| `401/403`              | Authentication or scoped authorization failure                          |
| `404`                  | Resource not visible in organization scope or feature disabled          |
| `409`                  | Stale checksum/revision, invalid transition, lock, or material conflict |
| `UNKNOWN` domain state | Outcome cannot be proven; query status and reconcile                    |

Do not use error differences to probe another organization. API errors and
retained evidence redact secret, credential, and foreign-tenant details.

## Proof boundary

This document describes the integrated contract. The generated OpenAPI and
route tests remain authoritative for the running build. It does not claim that
the API has been exercised against staging, production, or an adopter.
