# PR-084 - Review-Material Admission Decisions

## Purpose

Add the smallest community-neutral control that separates persistent operator
`GO/NO_GO` review evidence from scheduler admission evaluation.

## Contract

- Decisions bind the exact plan checksum and a checksum over the current
  independently observed-state set.
- Every decision is append-only, expires, and records current authorization
  evidence.
- Later decisions must supersede the current tip; `NO_GO` may explicitly
  revoke it.
- Protocol-v2 task creation rechecks the decision, observed-state CAS, expiry,
  and authorization inside the task transaction before any preflight, task,
  lock, step-run, or external mutation.
- Missing, `NO_GO`, expired, revoked/superseded, checksum-invalid, or stale
  evidence fails closed.
- Untouched v1 task creation remains compatible.

## Surface

Migration 165 adds `ReviewAdmissionDecision`. The public API adds append and
list routes below each deployment plan. The slice is API-first and adds no new
UI workspace or agent protocol.

## Verification Boundary

Focused domain, API, repository-source, route, migration-lint, and diff checks
are required for this slice. No live system, client runtime, workload database,
Jenkins job, registry, or external executor is accessed.
