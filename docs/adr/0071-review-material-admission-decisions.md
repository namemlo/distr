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
canonical review-material checksum, a non-empty complete current observed-state
set checksum, actor authorization evidence, expiry, reason, and idempotency
key. Observed material includes the active desired revision and exact current
observation identity plus artifact, configuration, schema, capability,
platform, topology, runtime, receive-time, and freshness facts.

The first decision has no parent. Every later decision must supersede the
current tip. A `NO_GO` may additionally revoke that tip. Rows cannot be updated,
truncated, or ordinarily deleted; correction and revocation are new rows.

Before protocol-v2 task mutation, the task transaction locks the current
decision and observed-state rows, requires the tip to be an unexpired `GO`,
recomputes both checksums, and revalidates current `plan.execute`
authorization. It also re-evaluates approval using current active group
membership, excludes the executing actor from protected
`executor_cannot_approve` requirements, and assigns distinct actors across
protected requirements deterministically. The scheduler hands task creation
the exact admission evaluation ID and decision checksum; the transaction
requires it to remain the latest ADMIT and to bind the same current approval
request ID/revision. GO authorization evidence is reconstructed against the
admission and approval current when the GO was recorded. This gate runs before
preflight persistence, task insertion, persistent task locks, step runs, or
external execution materialization. Existing v1 task creation remains
unchanged.

The current review-material read model fails closed unless every frozen plan
baseline has a current independent observation. It reports exact plan,
observed-state, and review-material checksums, whether the latest admission is a
current `ADMIT` backed by currently eligible approval evidence, the latest
append-only review decision, blockers, and whether a new decision can be
recorded.

The API adds:

- `POST /api/v1/deployment-plans/{id}/review-decisions`
- `GET /api/v1/deployment-plans/{id}/review-decisions`
- `GET /api/v1/deployment-plans/{id}/review-material`

The decision-list route returns the complete retained append-only chain in
newest-first order. The plan detail UI renders actor, comment, time, plan
revision, idempotency key, expiry, plan/observed/review/decision checksums,
authorization evidence, supersession, revocation, and material invalidation
instead of showing only the current tip.

## Consequences

Review evidence is durable and replayable, and stale or negative decisions
fail closed without task or external mutation. Operators must append a fresh
decision when observed state changes. Historical GO rows whose authorization
evidence predates admission/approval identity binding remain readable but
cannot authorize new v2 tasks. The plan detail UI surfaces current, negative,
stale, and missing states, disables decision controls when the material or
admission is invalid, and requires typed confirmation of the exact
review-material checksum before appending `GO` or `NO_GO` evidence.

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

Focused tests cover checksum binding, `GO` expiry/staleness, `NO_GO`, incomplete
observation material, current approval authority, executor separation,
cross-requirement distinct approvers, API validation, route publication,
append-only migration shape, UI decision states and submission, and ordering
of the task gate before every mutating operation. UI and handler tests also
cover the complete history and lineage fields. Full PostgreSQL and release
gates remain part of final integration verification.
