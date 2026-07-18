# PR-064: Exact Baseline, Change Set, and Previous State

## Outcome

PR-064 makes every target plan explain exactly what changes from the last
trusted healthy state for each physical Component Instance. The plan response
and validation preview expose:

- `baselines`: active desired-state revision/checksum plus independent
  observation evidence;
- `changes`: exact image, configuration, provider, schema, topology, and
  accumulated release-note differences;
- `risks`: deterministic policy review entries; and
- `bootstrap`: an explicit first-deployment marker.

The existing v1 deployment-plan route and historical payloads remain
compatible.

## Decision record

Status: Accepted.

ADR-0060 freezes target-specific plans, but a release or configuration preview
alone cannot answer what changed for one placement. A target may skip releases,
use a target-specific configuration, change a capability provider, or have an
executor report success without the independent healthy observation required by
the operating model.

Redeploying an earlier release does not mutate history. If plan B is the active
desired state and plan A remains compatible, returning to A creates a new B-to-A
plan with independent approval, execution, observation, and audit history.

PR-064 therefore makes three decisions:

1. Each Component Instance freezes one exact healthy baseline when a target
   plan is published.
2. Change and risk evidence compares bounded immutable facts in canonical
   order; release notes are operator context and never replace the baseline.
3. Previous-state requests create a new immutable plan and never mutate or
   reuse either source plan.

## Baseline selection

The repository reloads the current target/component desired state under the
authenticated organization. A candidate is accepted only when:

- desired and observed revision/checksum are exact matches;
- the observation is `HEALTHY`;
- release, image, platform, and configuration facts are present; and
- it is the newest exact matching observation by observed time, state
  revision, and observation ID.

The frozen projection is one of:

- `verified_v2`: independent observation evidence belongs to a successful
  protocol-v2 plan/execution and may become execution-authoritative only after
  PR-075 installs the fenced executor;
- `legacy_projection`: useful comparison evidence that cannot authorize
  protocol-v2 execution; or
- `bootstrap`: no verified healthy observation exists.

No semantic-version ordering or mutable latest-release pointer selects the
baseline.

## Change and risk model

Entries are deterministic and bounded. They compare exact immutable values,
not global version pointers:

| Entry          | Compared facts                                                            |
| -------------- | ------------------------------------------------------------------------- |
| `image`        | Component Release ID/version, platform, digest                            |
| `config`       | Target Config Snapshot ID and canonical checksum                          |
| `provider`     | Exact requirement/provider binding checksum                               |
| `schema`       | Ordered migration/schema declaration and checksum                         |
| `topology`     | Deployment Unit, physical instance, subscriber set, graph                 |
| `source_notes` | Published component notes after the baseline through the selected release |

Bootstrap plans retain the bootstrap marker and also emit the exact target
image, config, provider, schema, topology, and selected-release note facts.
Release-note candidates are restricted to published immutable product-release
lineage in the same application. The planner retains the target and baseline
bounds deterministically and fails closed with `planning_limit_exceeded` when
more than 128 component releases fall within the range.

Risk classification calls out bootstrap approval, non-authoritative v2
baselines, provider/topology blast radius, planning limits, and forward-only
schema transitions. All baseline/change/risk rows carry the organization,
actor, canonical checksum, and stable sort order. Update, delete, and truncate
are rejected except the existing explicit organization-retention boundary.

## Previous-state workflow

Request:

```http
POST /api/v1/deployment-plans/{currentPlanId}/previous-state
Content-Type: application/json

{
  "successfulDeploymentPlanId": "00000000-0000-0000-0000-000000000000",
  "reason": "Restore the last verified compatible state"
}
```

The source must be an executed plan for the same organization, application,
environment, and Deployment Unit, with independent healthy observations for
every physical Component Instance and component release pair. New observations
persist that exact instance identity; a shared Component Release ID cannot
collapse coverage for multiple instances. The current plan must still be the
latest placement plan and must not contain a forward-only schema transition.

The response is a new immutable target plan. It references current plan B in
`supersedesDeploymentPlanId`, source plan A in
`previousStateSourcePlanId`, and generates a fresh checksum/change/risk set.
A retry of the same B/A request returns the existing new plan.

Creation runs in one serializable transaction that locks both tenant-scoped
plans, rejects a stale current plan, verifies independent healthy observations
for every source component, blocks forward-only schema recovery, resolves
current B against desired A, and publishes the new history.

## Data model

Migration 146 adds:

- `DeploymentPlanBaseline`;
- `DeploymentPlanChangeEntry`;
- `DeploymentPlanRiskEntry`;
- `DeploymentPlan.bootstrap`; and
- `DeploymentPlan.previous_state_source_plan_id`.
- `TargetComponentObservation.component_instance_id` for exact per-instance
  execution evidence.

Composite tenant foreign keys cover the plan, source plan, Component Instance,
execution, observation, Component Release, Target Config Snapshot, and actor.
Migration rollback refuses while any PR-064 evidence exists.

## Compatibility and deferred gates

- Existing `distr.deployment-plan/v1` rows receive additive defaults only.
- Protocol-v2 plans remain blocked until PR-075; PR-064 adds no executor
  bypass.
- PR-067 will provide persisted effective policy input; the current classifier
  fails closed for bootstrap, forward-only changes, and non-authoritative v2
  baselines.
- Migration 146 stacks after PR-063 migration 145 and must be rebased with the
  final PR-058 through PR-063 migration chain before integration.
- No Suria, B2C, MC, transaction-api, client workload runtime, or client
  database is changed.

## Consequences

- Operators receive a target-specific change log for approval.
- Legacy state remains useful without becoming a protocol-v2 trust bypass.
- First deployments are explicit bootstrap operations governed by policy.
- Forward-only database changes require a forward fix.
- Existing v1 rows, APIs, canonical payloads, checksums, and execution remain
  unchanged.
- PR-067 may replace the conservative default risk policy without changing the
  persisted evidence contract.
- PR-075 remains the only slice that may enable protocol-v2 execution.

Focused Go, migration-lint, and TypeScript checks are the slice gate. Live
PostgreSQL 16/18, containers, full repository suites, browser automation, and
deployment remain the final integrated stack gate.
