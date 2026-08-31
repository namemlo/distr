# ADR-0071: Review-Material Admission Decisions

## Status

Accepted

## Context

PR-070 records scheduler evaluation outcomes (`ADMIT`, `WAIT`, and `BLOCK`),
but those outcomes are not a durable operator release decision. A reviewer must
be able to record `GO` or `NO_GO` against the exact material they inspected,
including the current independently observed runtime state. A prior `GO` must
not authorize task creation after that state changes, after authorization is
lost, after expiry, or after a later decision supersedes or revokes it.

## Decision

Add an append-only `ReviewAdmissionDecision` chain behind the existing v2
feature flags. Each decision binds the immutable plan revision/checksum, a
canonical review-material checksum, the current observed-state set checksum,
actor authorization evidence, expiry, reason, and idempotency key.

The first decision has no parent. Every later decision must supersede the
current tip. A `NO_GO` may additionally revoke that tip. Rows cannot be updated,
truncated, or ordinarily deleted; correction and revocation are new rows.

Before protocol-v2 task mutation, the task transaction locks the current
decision and observed-state rows, requires the tip to be an unexpired `GO`,
recomputes both checksums, and revalidates current `plan.execute`
authorization. This gate runs before preflight persistence, task insertion,
locks, step runs, or external execution materialization. Existing v1 task
creation remains unchanged.

The API adds:

- `POST /api/v1/deployment-plans/{id}/review-decisions`
- `GET /api/v1/deployment-plans/{id}/review-decisions`

## Consequences

Review evidence is durable and replayable, and stale or negative decisions
fail closed without task or external mutation. Operators must append a fresh
decision when observed state changes. The initial slice is API-first and adds
no new UI workflow; the operator read model may surface this chain later.

Migration 165 is additive. Its down migration refuses while any review
decision exists.

## Alternatives Considered

- Reusing `AdmissionEvaluation` was rejected because it conflates a scheduler
  gate result with a human review decision.
- Updating one mutable decision row was rejected because it destroys
  supersession and revocation evidence.
- Checking only the plan checksum was rejected because the actual runtime may
  change after review.

## Validation

Focused tests cover checksum binding, `GO` expiry/staleness, `NO_GO`, API
validation, route publication, append-only migration shape, and ordering of the
task gate before every mutating operation. Full PostgreSQL and release gates
remain part of final integration verification.
