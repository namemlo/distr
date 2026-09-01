# ADR-0081: Lock and Lease Lifecycle Read Model

## Status

Accepted

## Context

Distr already retains execution fences, task resource locks, task leases,
campaign admission state, scheduler leases, and campaign-to-task lineage. The
operator read model exposed fence generation but did not expose acquisition,
conflict, expiry, release, pause-pending, no-new-exposure, or terminal closure
facts. Operators therefore could not distinguish an active coordination state
from a completed run whose ownership had been released.

The lifecycle source of truth must remain the native scheduler and execution
records. A second lock table, migration, or UI-owned lifecycle would create
conflicting authority.

## Decision

Add authorization-scoped projections to the existing execution and campaign
detail responses.

Execution detail returns:

- every retained `TaskResourceLock`, including created/acquired/released times,
  current derived conflict state, policy, and a transparently derived release
  reason;
- every retained `TaskLease`, including executor, lease attempt, heartbeat,
  expiry, release, state, and derived release reason;
- fence generation, lease expiry, release, timeout/reconciliation state, active
  and unreleased counts, and terminal zero-lock closure.

Campaign detail returns persisted admission blocking, pause-pending,
no-new-exposure, reconciliation, scheduler fence/lease state, the count of
members in `ADMITTED` or `RUNNING`, active/unreleased task coordination counts,
and terminal zero-lock closure.

Current lock conflict is explicitly a point-in-time projection. Distr does not
invent a historical conflict timestamp because no native conflict event stores
one. Release reasons are labelled as derived from retained terminal or expiry
state when the native row does not carry a reason column.

## Consequences

- Operators can prove acquisition, retry generation, expiry, release, and
  zero-lock closure without querying the database directly.
- Read complexity remains bounded to the existing single detail statement by
  using tenant-scoped lateral aggregates.
- Existing clients receive additive JSON fields and remain compatible.
- There is no schema, scheduler, executor protocol, or mutation-path change.
- An expired but unreleased task lease remains visible and prevents a false
  zero-lock-closure claim.

## Alternatives Considered

- Add a new lifecycle table: rejected because it would duplicate native state
  and require consistency repair.
- Infer coordination from generic task/step messages in the UI: rejected
  because text matching is incomplete and not an auditable contract.
- Show only fence generation: rejected because fencing alone does not prove
  task lock or executor lease release.

## Validation

- Go tests assert tenant-scoped lock, lease, conflict, fence, campaign lineage,
  count, and zero-lock-closure projections.
- Angular tests assert active, conflict, expired, released, timeout,
  reconciliation, pause-pending, no-new-exposure, and empty/terminal states.
- Formatting, focused backend tests, and focused UI tests run before merge.
- No live environment or client database is required for validation.
