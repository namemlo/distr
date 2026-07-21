# PR-070 - Deployment Admission and Emergency Overrides

## Generic User Story

As a fleet operator, I want each v2 deployment start to retain the exact approval, policy, scheduling, gate, and
override evidence that authorized it so task creation cannot race ahead of governance or silently reuse stale
decisions.

## Contract

- `POST /api/v1/deployment-plans/{id}/admission` evaluates and appends one tenant-scoped admission decision.
  The repository obtains the exact evaluation instant from the database clock and resolves mandatory gate evidence
  through a trusted producer bound to the frozen plan revision and policy checksum. The request body cannot supply
  either value.
- `POST /api/v1/deployment-plans/{id}/emergency-overrides` creates an expiring, approval-backed acceleration bound
  to the exact plan and effective-policy checksums.
- Both routes are absent unless `operator_control_plane_v2` and `executor_protocol_v2` are effective.
- Repository mutation requires scoped `plan.execute` or `emergency.override` authorization using the plan's
  organization, environment, and optional deployment-unit scope. The same scope must be effectively enrolled.
  The adapter resolves scopes and checks PR-066 authorization plus enrollment at the database decision instant.
- Closed maintenance windows and active freezes return `WAIT`. Missing or stale approval and failed mandatory
  gates return `BLOCK`.
- Emergency overrides may shorten only the strict intersection of policy-whitelisted gates. They cannot accelerate
  integrity, required evidence, backup, provenance, observation, or mandatory health. Every backing approval must
  be both eligible and `APPROVED`, and a calendar or freeze wait is shortened only when its trusted remaining
  duration is within `maxAccelerationSeconds`. Approval waits remain closed because they have no bounded duration.
- Scheduler retries with the same idempotency key return the original row only when the decision checksum matches.
  Reusing the key for different evidence conflicts.
- `CreateTasksForAdmittedV2Plan` is the sole v2 wrapper. It requires
  `distr.target-deployment-plan/v2` plus frozen protocol `v2`, records `ADMIT`, then delegates to the existing task
  creator. The shared v1 creator is unchanged.

## Persistence and Evidence

Migration 152 adds append-only `EmergencyOverride` and `AdmissionEvaluation` tables. Evaluations pin plan and
optional campaign revisions, policy version IDs/checksum, calendar version IDs, freeze revision IDs, approval
request revision, optional override checksum, exact temporal and gate evidence, actor, and separate material and
decision checksums. Overrides pin accelerations, reason, actor, eligible approval revisions/checksums, expiry,
idempotency key, approval state, and canonical checksum. Persistence accepts only an internal sealed evaluation,
reevaluates it, verifies the material and decision checksums, and persists the recomputed result.

Updates and ordinary deletes are rejected. Organization-retention deletion uses the explicit transaction-local
retention marker. The guarded down migration refuses to cross 152 while evidence exists.

## Compatibility

The routes and v2 wrapper are default-off. Direct v1 `CreateTasksForDeploymentPlan` behavior is not gated or
modified: a ready v1 plan with no policy, approval, calendar, enrollment, override, or admission rows still creates
the same queued task and pending step-run state, marks the plan executed, and emits no new step event.

PR-063 supplies the frozen v2 plan columns and PR-066 supplies the shared scoped authorization/enrollment evaluator.
The mandatory-gate producer is also an explicit trusted repository seam. This branch reads plan identity through a
schema-tolerant snapshot and keeps admission closed until those stacked dependencies and the trusted producer are
integrated.

## Verification

Focused coverage includes closed windows, active freezes, missing approvals, clock-versus-material checksum
behavior, policy/calendar/override binding, acceleration whitelist, protected mandatory gates, scheduler replay
idempotency including approval evidence, rejection of caller-supplied clock/gate evidence, sealed-evaluation
recomputation, bounded calendar/freeze acceleration, API validation, response mapping, scope denial before
persistence, both-flag route hiding, admitted-v2 delegation, migration structure, and the flags-off v1 regression.
