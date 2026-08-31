# PR-084 - Review-Material Admission Decisions

## Purpose

Separate persistent operator `GO/NO_GO` review evidence from scheduler
admission evaluation, expose the current review material in the operator UI,
and enforce four-eyes approval again at admission and execution time.

## Contract

- Decisions bind the exact plan checksum and a checksum over the current
  independently observed-state set.
- Every decision is append-only, expires, and records current authorization
  evidence.
- Later decisions must supersede the current tip; `NO_GO` may explicitly
  revoke it.
- Review material is available only when every frozen plan baseline has a
  current independent observation. Missing observations fail closed.
- Admission revalidates the current approval-group membership revision,
  excludes the executor from requirements carrying
  `executor_cannot_approve`, and uses deterministic matching when protected
  requirements require distinct approvers.
- Protocol-v2 task creation rechecks the decision, observed-state CAS, expiry,
  current approval eligibility for the executing actor, and authorization
  inside the task transaction before any preflight, task, persistent task
  lock, step-run, or external mutation.
- Missing, `NO_GO`, expired, revoked/superseded, checksum-invalid, or stale
  evidence fails closed.
- Untouched v1 task creation remains compatible.

## Surface

Migration 165 adds `ReviewAdmissionDecision`. The public API provides append
and list routes plus `GET /api/v1/deployment-plans/{id}/review-material` for the
current checksums, decision state, admission validity, and blockers. The plan
detail UI displays `MISSING`, `GO`, `NO_GO`, and `STALE`, requires typed
confirmation of the exact review-material checksum, and appends supersession
or revocation lineage. No agent protocol changes are required.

## Verification Boundary

Focused governance, repository, handler, route, and Angular component checks
cover four-eyes reevaluation, incomplete observation material, current
admission validity, decision states, disabled controls, and checksum-bound
submission. No live system, client runtime, workload database, Jenkins job,
registry, or external executor is accessed.
