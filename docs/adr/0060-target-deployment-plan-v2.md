# ADR-0060: Immutable Target Deployment Plan v2

## Status

Accepted.

## Context

A Product Release is intentionally target-neutral. It freezes component
release identities, exact checksums, platform coverage, and a symbolic
capability graph, but it cannot safely authorize a deployment by itself.
Execution also needs an exact customer or shared-unit placement, environment
assignment, physical component instances, target configuration snapshot,
dependency bindings, expected observed state, and an acyclic step graph.

The existing `DeploymentPlan` and `POST /api/v1/deployment-plans` contract is
the production v1 path. Its IDs, payloads, checksums, and execution behavior
must remain readable and unchanged while the controlled operator workflow is
introduced.

## Decision

### Mutable draft, immutable publication

`DeploymentPlanDraft` is the only mutable planning resource. It identifies one
published Product Release, Deployment Unit, active environment assignment,
Target Config Snapshot, and executor protocol. Updates use an exact positive
`expectedRevision`; a mismatch returns a conflict. A correction may name one
superseded plan and must include a non-empty reason.

Validation reloads all facts from the authenticated organization. It returns a
preview checksum without trusting target, organization, release, configuration,
or observation facts supplied by the caller. Publication requires the same
draft revision and preview checksum, locks the draft row, recomputes the
preview, and atomically inserts the plan and its frozen facts. Concurrent
publication is serialized by the row lock and a unique plan-per-draft
constraint. Repeating publication is idempotent only when the canonical
checksum is identical.

Once a draft has a published plan, the draft is immutable. A published v2 plan,
its requirement resolutions, and its step edges are append-only. Migration 145
refuses rollback while any v2 draft or plan evidence exists.

### Exact resolver inputs

The resolver receives organization-scoped facts loaded by the repository:

- the published Product Release checksum and every symbolic target-stage
  requirement;
- exactly one active target/environment assignment and Deployment Unit;
- the Deployment Unit subscriber-set checksum;
- the Target Config Snapshot ID, canonical checksum, platform, immutable object
  verification facts, feature flags, and physical component bindings;
- exact Component Release IDs, release checksums, platform artifact digests,
  and provenance eligibility;
- registry Component Instances; and
- trusted provider observations and expected-state checksums when a requirement
  is not supplied by the selected Product Release.

Every target requirement must resolve to exactly one allowed mode:

| Mode | Required frozen evidence |
| --- | --- |
| `included` | Pinned Product Release component, matching platform artifact, provenance eligibility, and physical instance in the selected unit |
| `pinned_existing` | Exact component release, physical instance, trusted observation, and matching current expected-state version/checksum |
| `shared_provider` | Exact provider unit, immutable subscriber-set checksum, component release, trusted observation, and expected state |
| `approved_external` | Approved external binding and trusted observation evidence |
| `feature_disabled` | An immutable false feature-flag fact from the selected Target Config Snapshot |

No fallback order silently chooses between multiple bindings. Zero matches is
unresolved; more than one match is ambiguous. Foreign, retired, mismatched,
unverified, or stale facts are blockers.

### Canonical target graph

The planner creates stable steps for configuration verification, component
migrations, component deploys, health observations, and target-requirement
verification. Every step freezes its key, execution location, target and
database lock keys, timeout, retry class, cancellation behavior, input
checksum, and required observation. Product capability edges require provider
health before consumer deployment. Requirement verification precedes the
consumer deployment.

Steps, edges, resolutions, pins, bindings, and verification facts are sorted
before canonical JSON encoding. The graph must be acyclic and its checksum is
part of the plan checksum.

### Versioned adapter freeze

Component Release v2 contracts declare exact adapter capabilities by typed
step kind. Target configuration selects an enabled implementation,
implementation version, scope, and immutable config checksum. Planning freezes
the assignment ID, implementation ID/version, capability/version, target
scope, Target Config Snapshot, config checksum, key ID, public-key
fingerprint, opaque signing-key provider reference, and non-reversible signing
key version fingerprint into an append-only plan-step adapter record.

The release declaration remains authoritative; an assignment cannot weaken or
replace it. Start-time preflight reloads current adapter state. Missing,
disabled, or drifted adapter state blocks execution and requires exact
restoration or a new target plan revision. Private key bytes remain exclusively
in the configured secret provider.

### Protocol boundary

`protocolVersion=v1` is publishable only when every generated step and
requirement binding is explicitly v1-compatible. The existing v1 plan creation
route is unchanged.

`protocolVersion=v2` plans are published as non-executable, blocked plans until
PR-075 installs the fenced executor protocol. The draft and immutable plan are
still reviewable and auditable; the current task-creation path cannot execute
them because it accepts only `READY` plans.

### API and authorization

The additive routes are:

- `POST /api/v1/deployment-plan-drafts`
- `GET /api/v1/deployment-plan-drafts/{id}`
- `PATCH /api/v1/deployment-plan-drafts/{id}`
- `POST /api/v1/deployment-plan-drafts/{id}/validate`
- `POST /api/v1/deployment-plan-drafts/{id}/publish`

All routes require a vendor organization role and both `deployment_plans` and
`operator_control_plane_v2` feature flags. Create, update, validate, and
publish additionally require read-write/admin access and reject super-admin
mutation. Repository predicates and composite foreign keys independently
enforce organization isolation.

## Consequences

- A configuration-only change creates a new Target Config Snapshot and plan,
  not a new Product Release.
- Plan review can explain every exact release, configuration, placement,
  provider, expected-state, and graph decision.
- Optimistic preview checks prevent approving or publishing a stale resolution.
- Existing v1 plans, payloads, checksums, endpoints, and execution remain
  compatible.
- PR-064 can add baselines and change sets without changing draft identity.
- PR-066 can replace the authorization seam with scoped grants without
  weakening the current route guards.
- PR-075 owns v2 execution enablement; this ADR deliberately does not add an
  executor bypass.
