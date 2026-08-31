# PR-084 - Review-Material Admission Decisions

## Purpose

Separate persistent operator `GO/NO_GO` review evidence from scheduler
admission evaluation, expose the current review material in the operator UI,
and enforce four-eyes approval again at admission and execution time.

## Contract

- Decisions bind the exact plan checksum and a checksum over a non-empty,
  complete current observed-state set. Each checksum item includes the active
  desired revision, observation identity, artifact/config/schema/capability/
  platform/topology/runtime facts, receive time, and freshness boundary.
- Every decision is append-only, expires, and records authorization evidence
  bound to the exact current ADMIT evaluation and approval request revision.
- Later decisions must supersede the current tip; `NO_GO` may explicitly
  revoke it.
- Review material is available only when every frozen plan baseline has a
  current independent observation. Missing observations fail closed.
- Admission revalidates the current approval-group membership revision,
  excludes the executor from requirements carrying
  `executor_cannot_approve`, and uses deterministic matching when protected
  requirements require distinct approvers.
- The scheduler passes the exact admission evaluation ID and decision checksum
  into protocol-v2 task creation. The task transaction rechecks that exact
  latest ADMIT, its current approval request ID/revision, the observed-state
  CAS, the latest GO, current approval eligibility for the executing actor,
  and authorization before any preflight, task, persistent task lock,
  step-run, or external mutation.
- Missing, `NO_GO`, expired, revoked/superseded, checksum-invalid, or stale
  evidence fails closed.
- Untouched v1 task creation remains compatible.

## Surface

Migration 165 adds `ReviewAdmissionDecision`. The public API provides append
and complete-history list routes plus
`GET /api/v1/deployment-plans/{id}/review-material` for the current checksums,
decision state, admission validity, and blockers. The plan detail UI displays
`MISSING`, `GO`, `NO_GO`, and `STALE`, requires typed confirmation of the exact
review-material checksum, and renders every retained decision with actor,
comment, time, plan revision, idempotency key, expiry, checksums,
authorization evidence, supersession, revocation, and material invalidation.
No agent protocol changes are required.

GO records produced before authorization evidence included admission and
approval identities cannot authorize new v2 task creation. They remain
readable history, but operators must append a fresh GO under the current
admission and approval evidence.

## Verification Boundary

Focused governance, repository, handler, route, and Angular component checks
cover four-eyes reevaluation, incomplete observation material, current
admission validity, exact admission handoff, full append-only history,
decision states, disabled controls, and checksum-bound submission. No live
system, client runtime, workload database, Jenkins job, registry, or external
executor is accessed.
