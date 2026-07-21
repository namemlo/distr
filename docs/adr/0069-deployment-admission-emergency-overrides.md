# ADR-0069: Checksum-Bound Deployment Admission and Emergency Overrides

## Status

Accepted

## Context

Published v2 deployment plans must not create tasks merely because their immutable content is valid. Execution also
depends on the current approval result, pinned maintenance-calendar and freeze revisions, mandatory gate evidence,
scoped authorization, and effective enrollment. Operators need a narrowly defined emergency path for shortening
pre-approved waits without weakening integrity or mandatory health protections.

Admission is time-sensitive, but an ordinary clock advance must not invalidate an otherwise unchanged approved
plan. Changes to a policy, published calendar/freeze rule, approval revision, campaign revision, or emergency
override are material and must produce new checksum-bound evidence.

## Decision

- Evaluate admission as a pure function over immutable plan, policy, calendar, freeze, approval, gate, campaign,
  and optional override evidence.
- Persist every scheduler decision in append-only `AdmissionEvaluation` rows. A scheduler idempotency key may replay
  only the exact same decision checksum. Persistence reevaluates internal sealed material and verifies both
  checksums before writing the recomputed decision.
- Separate `material_checksum` from `decision_checksum`. The material checksum omits the evaluation clock and exact
  temporal result, while the decision checksum includes them.
- Persist emergency accelerations in append-only `EmergencyOverride` rows bound to the exact plan and effective
  policy checksums, actor, current approved-and-eligible approval evidence, expiry, and canonical override checksum.
  Idempotent replay includes the current approval IDs, revisions, states, evidence checksums, and override checksum.
- Allow acceleration only for gate keys present in every applicable override rule. Integrity, required evidence,
  backup, provenance, observation, and mandatory health gates are permanently protected. A calendar or freeze wait
  is shortened only when its trusted remaining duration is no greater than the requested maximum; an unbounded
  approval wait cannot be accelerated.
- Require both `operator_control_plane_v2` and `executor_protocol_v2`, PR-066 scoped `plan.execute` or
  `emergency.override` authorization, and effective organization/selected-environment enrollment at the same
  database decision instant before mutation.
- Keep `CreateTasksForDeploymentPlan` unchanged for v1. The only v2 entry point is
  `CreateTasksForAdmittedV2Plan`, which requires frozen v2 schema/protocol identity, records an `ADMIT` decision, and
  then delegates to the existing creator.

Exact evaluation, creation, and expiry instants use `TIMESTAMPTZ`. Admission evaluation uses the database clock,
not a caller-supplied instant. Mandatory gate evidence comes from a trusted producer bound to the frozen plan and
policy checksums, not from the API body. Temporal evidence also retains the IANA zone, timezone rule version, local
time, UTC offset, immutable rule IDs, evaluator identity, and trusted remaining wait supplied by ADR-0062.

## Consequences

Admission history is reproducible and tenant-scoped, retries cannot silently replace prior evidence, and emergency
operation remains auditable without becoming a general bypass. A changed material input requires a new immutable
revision or override. Existing v1 task status, preflight, step-run, and event behavior remains unchanged while the
new flags are off.

The PR-066 authorization/enrollment package and PR-063 v2 deployment-plan columns are stacked dependencies. The
shared authorizer preserves the legacy-role compatibility fallback only inside PR-066, keeps credential roles as
the upper bound, and denies super-admin mutation.
