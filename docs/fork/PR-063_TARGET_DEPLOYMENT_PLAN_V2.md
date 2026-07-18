# PR-063: Target Deployment Plan v2

## Outcome

PR-063 adds the reviewed, immutable planning boundary between a target-neutral
Product Release and execution. Operators can create a draft for one exact
Deployment Unit, validate its resolved requirements and step graph, and publish
the same preview checksum as an immutable `DeploymentPlan`.

The existing `POST /api/v1/deployment-plans` v1 workflow is unchanged.

## Workflow

1. Create a draft with a published Product Release, Deployment Unit,
   environment-assignment, Target Config Snapshot, and protocol.
2. Update the draft with `expectedRevision`. Any concurrent edit returns
   `409 Conflict`.
3. Validate the draft. The server reloads the current organization-scoped
   registry, release, provenance, configuration, and observed-state facts.
4. Review:
   - exact placement and subscriber-set checksum;
   - Product Release and Component Release checksums;
   - platform-specific artifact digests and provenance status;
   - Target Config Snapshot checksum, immutable object facts, feature flags,
     and physical Component Instances;
   - one binding for every symbolic target requirement;
   - expected-state version/checksum; and
   - the stable acyclic step graph and preview checksum.
5. Publish using the returned draft revision and preview checksum. A changed
   fact or edit causes a conflict and requires revalidation.
6. A repeated publish returns the existing plan only when its checksum is
   identical.

## Requirement resolution

The resolver supports the Product Release modes `included`,
`pinned_existing`, `shared_provider`, `approved_external`, and
`feature_disabled`. The selected mode must be declared by the symbolic
requirement and must have exactly one valid evidence binding. Unresolved,
ambiguous, cross-organization, stale, platform-mismatched, configuration-
mismatched, or provenance-ineligible bindings block publication.

The resolver does not choose a convenient first row. Inputs and outputs are
sorted and checksummed, so database row order cannot change a preview.

## Data model

Migration 145 adds:

- `DeploymentPlanDraft`: mutable optimistic builder with revision and preview
  fields;
- `DeploymentPlanResolvedRequirement`: append-only exact provider and
  expected-state bindings;
- `DeploymentPlanStepEdge`: append-only acyclic graph edges; and
- additive v2 identity fields on the existing `DeploymentPlan`:
  `plan_schema`, `draft_id`, `deployment_unit_id`,
  `target_config_snapshot_id`, `protocol_version`,
  `supersedes_deployment_plan_id`, and `supersede_reason`.

Composite foreign keys carry `organization_id` through product release,
placement, configuration, provider release, observation, Component Instance,
and superseded-plan references. Published facts cannot be updated or deleted
outside the explicit organization-retention boundary. Rollback refuses to
discard existing v2 evidence.

## Step graph

The canonical graph contains:

- target configuration verification;
- declared component migration steps in stable order;
- exact component deploy and health steps;
- provider-health-to-consumer-deploy edges from the Product Release graph; and
- verification steps for target-bound requirements.

Each step freezes target/database lock keys, timeout, retry class,
cancellation behavior, exact input checksum, and observation requirement.
Cycles block publication.

## Protocol behavior

- `v1`: publish succeeds only when every step and resolution is explicitly
  compatible with the current executor projection.
- `v2`: publication is allowed for review and audit, but the plan remains
  `BLOCKED` with `protocol_v2_execution_deferred` until PR-075 adds fenced
  execution. No current executor route is bypassed.

## Routes and controls

| Method | Route | Purpose |
| --- | --- | --- |
| `POST` | `/api/v1/deployment-plan-drafts` | Create draft |
| `GET` | `/api/v1/deployment-plan-drafts/{id}` | Read draft/publication link |
| `PATCH` | `/api/v1/deployment-plan-drafts/{id}` | Optimistic draft update |
| `POST` | `/api/v1/deployment-plan-drafts/{id}/validate` | Resolve and preview |
| `POST` | `/api/v1/deployment-plan-drafts/{id}/publish` | Atomically publish exact preview |

All routes require vendor organization context, `deployment_plans`, and
`operator_control_plane_v2`. Mutations require read-write/admin and block
super-admin. The route shape leaves the authorization seam available for
PR-066 scoped grants.

## Compatibility

- Existing v1 plan rows receive only additive defaults.
- Existing v1 API request/response fields and plan creation remain available.
- Historical canonical payloads and checksums are not rewritten.
- No agent, Suria, B2C, MC, transaction-api, client workload, or client
  database behavior changes in this PR.

## Focused verification

The slice covers deterministic resolution of every mode, one active placement,
ambiguity and unresolved blockers, configuration/release/platform/provenance
mismatches, stale expected state, stable graph/checksum/order, cycles,
tenant-scoped queries and constraints, publication locking/idempotency,
append-only migration guards, rollback refusal, route feature flags/RBAC, and
the flags-off v1 compatibility boundary.

Live PostgreSQL migration execution, full repository tests, containers,
browser tests, and deployment are final integration gates after the numbered
stack is rebased.
