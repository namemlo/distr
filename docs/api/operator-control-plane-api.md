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

Fleet items additively expose `observedArtifactDigest`,
`observedConfigChecksum`, `observedPlatform`, `observedSchemaVersion`,
`observedCapabilityChecksum`, and `observedHealth` from the current trusted,
accepted observation. `observedEvidenceChecksum` is present when exactly one
current observation supplies the Fleet row. When current observations conflict,
the row retains its conflict state and leaves the singular identity fields empty
rather than selecting an arbitrary runtime identity.

Execution detail `observations[]` facts additively expose `artifactDigest`,
`configChecksum`, `platform`, `schemaVersion`, `capabilityChecksum`, and
`health` from the exact verified or terminal `ObservedComponentState` row. The
fact's existing `checksum` remains that observation's evidence checksum.

Execution detail includes additive `locks`, `leases`, and `coordination`
projections. Lock facts expose created/acquired/released times, policy, current
conflict, and a derived release reason. Lease facts expose executor, attempt,
heartbeat, expiry, release, state, and a derived release reason. Coordination
exposes active and unreleased counts, fence generation/expiry/release,
timeout/reconciliation state, and terminal `zeroLockClosure`. Current conflict
is a point-in-time fact, not a fabricated historical event.

Campaign detail includes additive `coordination` state: persisted admission
blocking, `pausePending`, `noNewExposure`, `inFlightMemberCount` for `ADMITTED`
plus `RUNNING`, reconciliation, scheduler fence/lease state, task lock/lease
counts, and terminal `zeroLockClosure`. These fields are derived from native
campaign-run, member-run, lineage, lock, and lease records.

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

Component Release v2 and Product Release component responses additively expose
`migrationContracts`. Every `database` or `data` declaration must have one
checksum-bound full contract. Product validation rejects incomplete contracts,
checksum drift, duplicate IDs, missing dependencies, and cycles. Runtime-only
declarations remain compatible without a database contract.

#### Complete Product Release response

Create, get, and publish return the same complete Product Release read model.
The response contains these top-level fields:

| Field                                                                  | Ownership                                                                      |
| ---------------------------------------------------------------------- | ------------------------------------------------------------------------------ |
| `id`, `createdAt`, `updatedAt`, `applicationId`, `channelId`, `status` | Product Release lifecycle identity                                             |
| `canonicalChecksum`                                                    | Immutable Product Release canonical identity                                   |
| `graphChecksum`                                                        | Frozen capability-graph checksum; must equal `graph.checksum`                  |
| `publishedByUserAccountId`, `publishedAt`                              | Optional publication facts                                                     |
| `manifest`                                                             | Target-neutral `distr.product-release/v1` manifest                             |
| `graph`                                                                | Verified frozen graph, including order and unresolved target-stage constraints |

`manifest` always contains `schema`, `product`, `version`,
`dependencyPolicyVersion`, `dependencyPolicyChecksum`, `releaseNotes`,
`requiredPlatforms`, `components`, and product-level `requirements`.

Every `manifest.components[]` entry contains:

| Field                                            | Meaning                                                                                                          |
| ------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------- |
| `componentReleaseId`, `componentReleaseChecksum` | Exact immutable Component Release pin                                                                            |
| `componentKey`, `version`, `platforms`           | Frozen component identity and supported platforms                                                                |
| `artifacts[]`                                    | `key`, `type`, `mediaType`, immutable manifest `digest`, and exact `platforms[].platform` / `platforms[].digest` |
| `provides[]`                                     | Capability `name` and exact `version`                                                                            |
| `requires[]`                                     | Capability `name`, semver `range`, `resolutionStage`, and `allowedModes`                                         |
| `migrations[]`                                   | Declaration `key`, `type`, `order`, `compatibility`, `failurePolicy`, and `description`                          |
| `migrationContracts[]`                           | Complete structured migration safety contract                                                                    |

Each structured migration contract contains `id`, `checksum`, `componentKey`,
`databaseResourceKey`, `expectedSourceVersion`, `expectedSourceChecksum`,
`resultingVersion`, `resultingSchemaChecksum`, `phase`, optional `dependsOn`,
`lockType`, `lockTimeoutSeconds`, `operationalImpact`, `backupRequired`, optional
`backupVerifier`, `preconditionProbes`, `postconditionProbes`, `retryClass`,
optional `idempotencyKey`, `reversibility`,
`previousApplicationCompatibility`, `recoveryProcedureReference`,
`requiresForwardFix`, optional `adapterType`, optional `artifactDigest`, and
`evidenceRetentionDays`. Every probe contains `name`, `reference`, and
`expectedChecksum`.

The `graph` contains `nodes`, `edges`, `topologicalOrder`, and `checksum`.
Component nodes bind their Component Release identity. Requirement nodes carry
the capability, version range, resolution stage, allowed modes, and unresolved
state. Edges carry `key`, `from`, `to`, capability/range, optional frozen
provider version, resolution stage, allowed modes, and ordering semantics.

Product Release responses fail closed with `409` when a required dependency-
policy checksum, Component Release contract snapshot, graph checksum, graph
integrity proof, or frozen identity match is unavailable. Clients must not
treat a partial cached manifest as deployable.

### Plans and approvals

| Method      | Route                                       | Purpose                                                                  |
| ----------- | ------------------------------------------- | ------------------------------------------------------------------------ |
| `POST`      | `/deployment-plan-drafts`                   | Create mutable plan draft                                                |
| `GET/PATCH` | `/deployment-plan-drafts/{id}`              | Inspect/edit with optimistic revision                                    |
| `POST`      | `/deployment-plan-drafts/{id}/validate`     | Resolve exact target preview and blockers                                |
| `POST`      | `/deployment-plan-drafts/{id}/publish`      | Publish immutable plan/checksum                                          |
| `GET`       | `/deployment-plans`                         | List published plans                                                     |
| `GET`       | `/deployment-plans/{id}`                    | Get one plan                                                             |
| `POST`      | `/deployment-plans/{id}/approval-requests`  | Request checksum-bound approval                                          |
| `POST`      | `/deployment-plans/{id}/previous-state`     | Create a new compatible previous-state plan                              |
| `POST`      | `/deployment-plans/{id}/baseline-adoptions` | Adopt exact existing runtime without deployment                          |
| `POST`      | `/deployment-plans/{id}/tasks`              | Create durable tasks for a ready plan                                    |
| `GET/POST`  | `/deployment-plans/{id}/review-decisions`   | Read or append observed-state-bound GO/NO_GO evidence                    |
| `GET`       | `/deployment-plans/{id}/review-material`    | Read current checksums, admission validity, decision state, and blockers |
| `GET`       | `/approval-requests`                        | List approval work                                                       |
| `GET`       | `/approval-requests/{id}`                   | Get immutable request/decision evidence                                  |
| `POST`      | `/approval-requests/{id}/decisions`         | Append an authorized decision                                            |

Any material release, config, provider, migration, baseline, policy, or campaign
change invalidates reuse of the old approval.

#### Source-history validation

For a non-bootstrap target plan, draft validation compares the authoritative
baseline and selected Component Release source facts before publishing an
accumulated changelog. Repository identity, immutable contract commit, and the
`ReleaseBundle.source_revision` projection must agree. Releases on the
candidate contract's ordered component change path are included in path order;
published sibling releases outside that path are excluded.

Validation returns a blocking issue in the existing `issues[]` response:

| Code                        | Meaning                                                                               |
| --------------------------- | ------------------------------------------------------------------------------------- |
| `source_history_unverified` | Required baseline, candidate, or declared component-path proof is missing or invalid. |
| `source_history_divergent`  | Repository identity, commit order/projection, or release-to-commit mapping conflicts. |

Publication recomputes the preview and refuses either issue before persisting
an immutable plan. An equal baseline/candidate source commit is valid and adds
no source-notes delta. Component-scoped changelogs may end before the candidate
repository head when later commits do not touch that component.

This validation is a consistency check over immutable Distr release facts. It
does not independently prove Git parent edges. A cryptographic ancestry claim
requires an authenticated source-host or CI attestation that binds the exact
repository, baseline commit, candidate commit, and ancestry result.

The plan review UI exposes the existing baseline-adoption and admission
mutations. It binds plan, Product Release, and target-configuration checksums
from the immutable plan detail instead of asking the operator to retype them.
The component observation/provenance array remains explicit because the API
does not currently expose a complete baseline-adoption request projection.
Previous-state creation remains available when the current plan is already
`EXECUTED`; the selected source plan must itself satisfy the server-side
successful-state and exact-placement guards.

Target-plan validation additively returns `migrationContracts`, ordered in dependency
order. Each entry freezes exact source/result schema checksums, backup and probe
requirements, lock/retry/recovery facts, artifact digest, and component/database
identity. The graph contains `migration:<id>:backup:*`, `precondition`, `apply`,
and `validate` steps as applicable; publication retains the same contract in
the immutable plan migration evidence.

#### Schema report and migration evidence

Every planned Component Release that is bound to a component instance with a
database boundary, or that carries a structured migration contract, requires
two immutable Target Config Snapshot objects of kind `adapter_input`:

| Document           | Schema                        | Media type                                         |
| ------------------ | ----------------------------- | -------------------------------------------------- |
| Schema report      | `distr.schema-report/v1`      | `application/vnd.distr.schema-report.v1+json`      |
| Migration evidence | `distr.migration-evidence/v1` | `application/vnd.distr.migration-evidence.v1+json` |

The schema report binds the exact organization, deployment scope/unit,
environment assignment/environment, deployment target, Target Config
Snapshot, Component Release ID/checksum/version, database resource key, current
schema version/checksum, issue time, expiry time, and internal checksum.
Migration evidence repeats that scope and component identity, references the
schema-report checksum, records `expectedCurrent`, and selects exactly one
decision:

- `COMPATIBLE_NO_MIGRATION_REQUIRED` permits no migration bindings; or
- `MIGRATION_BOUND` binds every applicable contract in dependency order with
  its checksum and exact source/result schema version/checksum.

Both decisions include `mixedVersionEvidence`. Validation requires the exact
Cartesian product of prior and target application versions with the current
schema and every intermediate/result schema state. Every entry must be
compatible; missing, negative, duplicate, or extra facts are blockers.

The outer Target Config object reference, optional version ID, media type,
size, and lowercase SHA-256 checksum are verified before a bounded 256-KiB
strict JSON read. Unknown fields and trailing JSON are rejected. Each internal
checksum is the lowercase SHA-256 of Distr's normalized, whitespace-free JSON
form with its own `checksum` field omitted; migration evidence references the
exact internal schema-report checksum.

Draft validation responses add `schemaEvidenceRequirements` and
`schemaEvidence`. Published-plan responses add `schemaEvidence`, including the
frozen object bindings, report, decision, contract bindings, and compatibility
facts. The same bundles are part of the canonical plan checksum.

Validation can return `schema_evidence_missing`,
`schema_evidence_ambiguous`, `schema_evidence_wrong_kind`,
`schema_evidence_unavailable`, `schema_evidence_object_mismatch`,
`schema_evidence_scope_mismatch`, `schema_evidence_component_mismatch`,
`schema_evidence_schema_mismatch`, `schema_evidence_checksum_mismatch`,
`schema_evidence_not_yet_valid`, `schema_evidence_expired`,
`schema_evidence_expected_current_stale`,
`schema_evidence_decision_mismatch`,
`schema_evidence_migration_binding_incomplete`, or
`schema_evidence_mixed_version_incomplete`. Publication cannot proceed while
any blocker exists.

Admission revalidates the frozen canonical checksum, scope, freshness,
expected-current state, contract chain, and mixed-version matrix before
authorization or admission-evidence mutation. Protocol-v2 task creation repeats
the check before task lookup, advisory locks, preflight persistence, or task
mutation. A failed frozen-plan check returns `409` and performs none of those
mutations.

This contract does not query a live system or client workload database and does
not mutate a runtime. No database migration is added; schema target 170 remains
current and validated evidence is retained only in canonical plan bytes.

Protocol-v2 task creation also requires the latest persistent review decision
to be an unexpired `GO`. The decision binds the plan and the current
independently observed-state checksum. A later `NO_GO`, supersession,
revocation, observed-state change, expiry, or failed authorization recheck
blocks before task or external mutation. The scheduler supplies the exact
latest ADMIT identity/checksum to task creation, which rechecks the same current
approval request revision and reconstructs GO authorization evidence against
the admission and approval current when the GO was recorded.

The review-material route returns `MISSING`, `GO`, `NO_GO`, or `STALE`, the
exact plan/observed/review checksums, the latest append-only decision, current
admission validity, blockers, and `canDecide`. Material is incomplete when any
frozen baseline lacks a current independent observation. The operator plan UI
uses this response to disable unsafe controls and requires typed confirmation
of the exact review-material checksum before appending a decision.

The review-decision collection returns the complete retained append-only chain
in newest-first order. Each row includes actor, comment, record/expiry times,
plan revision, idempotency key, plan/observed/review/decision checksums,
authorization evidence, and supersession/revocation references. The plan UI
uses those fields to show the complete history and identify revoked,
superseded, expired, or material-invalidated decisions.

Baseline adoption is available only for an initial sealed `READY` native-v2
bootstrap plan with no task, external-execution, pending-desired, or active-
desired history. The request supplies an idempotency key, reason, exact plan,
Product Release, and target-config checksums, and one component entry for every
frozen plan pin. Each entry binds component instance/key, Component Release,
source commit/build, provenance verification and policy, artifact digest,
platform, config/schema/capability/topology, current observation and observer,
and observation evidence/state/runtime checksums. Health classification, use,
and policy checksum are not accepted from the adoption caller: they must already
exist on the authenticated immutable observation selected by the request.

`schemaVersion` and `capabilityChecksum` are independent observed runtime facts.
They do not need to equal the Component Release application SemVer or canonical
checksum. The response exposes that release-owned SemVer separately as
`components[].applicationVersion`; the deferred guard still requires every
release-owned and observation-owned fact to match its authoritative source.

An exact replay returns the retained immutable result. Changed material under
the same key returns `409`. Success returns `ADOPTED` with
`deploymentPerformed=false`, `taskCount=0`, `lockCount=0`, and
`executionCount=0`; no task,
task lock, attempt, external execution, or executor report is created. The plan
enters its successful terminal lifecycle state only after the deferred database
guard verifies every active head, current observation head, release/config pin,
and correlated `baseline_adoption.adopted` audit event.

Observer health-policy evidence requires an exact
`evidence://sha256/<64-lowercase-hex>` reference whose digest matches the
retained observation evidence checksum. `LEGACY_LIVENESS_ONLY` must be paired
with `BASELINE_OR_ROLLBACK_ONLY`. Its retained artifact should contain portable logical probe paths,
HTTP status, response size, and response checksum. Ephemeral transport addresses
must not be used as canonical evidence. This classification cannot be written as
execution-sourced desired-state promotion; later deployments retain the normal
standard-readiness observation gate, and provider discovery will not use legacy
liveness for `pinned_existing` or shared-provider promotion. Concurrent exact
idempotent requests re-read and return the committed adoption; changed material
under the same key remains a conflict.

New target plans use dependency provider evidence version 2. For
`pinned_existing`, `shared_provider`, and `approved_external`, validation and
published-plan responses include `observationFreshUntil`,
`observationTrusted`, and `observationCurrent`; all three must authorize the
server-selected planning instant. `approved_external` also includes
`providerApprovalRequestId`, `providerApprovalChecksum`,
`contractProbeObservationId`, and `contractProbeEvidenceChecksum`. The
approval is the provider deployment plan's exact, unexpired approved request;
the probe is a separate native observation evidence binding. Missing, stale,
superseded, or checksum-invalid evidence blocks publication.

The operator plan-detail route also exposes the authoritative typed
`detail.requirementResolutions[]` collection. Each row retains:

- requirement and consumer keys, capability, version range, selected `mode`,
  and deterministic `sortOrder`;
- provider Component Release, deployment-unit, and component-instance
  identities, version, platform, release/provenance checksums, and subscriber
  set checksum;
- expected state version/checksum plus observation, active-desired revision,
  and observed-component-state identities;
- provider evidence version, freshness, trusted/current decisions, and optional
  provider approval and contract-probe identities/checksums; and
- the canonical `bindingChecksum` and v1-compatibility decision.

The Product Release owns allowed constraints and provider-first graph order.
The immutable Target Deployment Plan alone owns the selected target mode and
provider evidence. A release UI must correlate the plan's Product Release ID
and checksum before rendering `included`, `pinned_existing`, or any other
selected mode.

`distr.product-release/v1` has no compatibility-group or rollback-group field.
Those execution/recovery bindings belong to the frozen process, Target
Deployment Plan, and review material and must not be inferred from component
order or duplicated into the Product Release read model.

The standard controlled-client policy requires four-eyes approval. At
admission and again before task creation, Distr revalidates current active
approval-group membership, excludes the executing actor from requirements with
`executor_cannot_approve`, and prevents reuse of one actor across requirements
that require `distinct_approvers`. Preparing plans or reading evidence does not
authorize mutation of a client runtime, client workload database, or
companion-service runtime; each such scope must be named in the separately
approved execution boundary.

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
misreport in-flight work as cancelled. The campaign read model keeps
`pausePending` visible until the native safe-point transition clears the
persisted request, and maps persisted admission blocking directly to
`noNewExposure`.

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

New protocol-v2 attempts carry signed runtime contract `v3`. The intent binds
the exact verified baseline state version/checksum, current image/configuration,
platform, target-scoped caller, and adapter-assignment audience in addition to
the desired artifact/configuration and fence.

| Method | Executor route                                           | Purpose                                                   |
| ------ | -------------------------------------------------------- | --------------------------------------------------------- |
| `POST` | `/api/executor/v2/attempts/{attemptId}/runtime-evidence` | Retain immutable pre/result runtime proof for the attempt |
| `POST` | `/api/executor/v2/attempts/{attemptId}/complete`         | Complete; `SUCCEEDED` binds retained evidence ID/checksum |

The runtime-evidence response returns a server canonical checksum. A successful
completion must return that exact row ID and canonical checksum. Unhealthy,
conflicting, stale-fence, expired-lease, wrong-caller/audience, wrong baseline,
or wrong result image/configuration evidence cannot authorize success. Failed,
cancelled, and timed-out outcomes do not require success evidence.
The request schema is explicitly
`distr.execution-runtime-evidence/v1`; unknown or missing schema versions fail
closed.

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
Executor runtime evidence never substitutes for or creates that independent
observation.

### Audit and evidence export

| Method     | Route                                                   | Purpose                                         |
| ---------- | ------------------------------------------------------- | ----------------------------------------------- |
| `GET`      | `/api/v1/control-plane-audit/events`                    | Paginated correlated events                     |
| `POST`     | `/api/v1/control-plane-audit/evidence-bundles`          | Deterministic checksum-bound bundle             |
| `GET/POST` | `/api/v1/control-plane-audit/export-sinks`              | Inspect/register allowlisted sink configuration |
| `GET`      | `/api/v1/control-plane-audit/export-status`             | Check checkpoint, lag, attempts, failures       |
| `POST`     | `/api/v1/protected-history-artifacts`                   | Export, store, verify, and retain exact history |
| `GET`      | `/api/v1/protected-history-artifacts/{id}`              | Read immutable retained metadata                |
| `GET`      | `/api/v1/protected-history-artifacts/{id}/verification` | Verify exact stored object without SQL mutation |

Failed export does not delete primary events or advance the checkpoint.

Protected-history create accepts only `customerOrganizationIds`,
`deploymentTargetIds`, a distinct `reviewerUserAccountId`, and an
`idempotencyKey`. Organization and issuer come from authentication. The Hub
validates exact organization scope and current issuer/reviewer membership,
exports the database itself, uses a checksum-addressed create-only object, and
requires exact readback before atomically retaining metadata and the correlated
`protected_history.retained` event. Exact idempotent replay returns the same
row; changed request material or object bytes returns `409 Conflict`. The
dedicated `PROTECTED_HISTORY_OBJECT_STORE_ENABLED` and
`PROTECTED_HISTORY_S3_*` configuration never falls back to registry or
target-config storage.

The audit UI can create a scoped retained artifact, open it by retained ID,
and request readback verification. A distinct reviewer user-account ID remains
mandatory. The API does not currently provide artifact listing or reviewer
directory discovery, so operators must retain the returned artifact ID and
obtain the reviewer ID through their existing identity-administration process.

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
