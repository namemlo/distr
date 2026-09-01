# PR-094: Lock and Lease Lifecycle Read Model

## Purpose

Expose the retained execution and campaign coordination lifecycle required for
operator proof of lock acquisition, conflict, lease expiry, release, admission
blocking, pause safe points, and terminal zero-lock closure.

## Scope

- Add execution lock, lease, and coordination summary contracts.
- Add campaign coordination summary contracts.
- Project native records in the existing authorization-scoped detail queries.
- Render dedicated execution and campaign lifecycle cards.
- Add focused Go and Angular coverage and operator API documentation.

## Compatibility

- Additive JSON only; no route is removed or renamed.
- No database migration or new lifecycle authority.
- No scheduler, executor, agent protocol, or mutation behavior changes.
- Current conflict and release reason are clearly labelled derived facts.

## Community Boundary

The implementation is generic. It contains no client names, environment
addresses, CI credentials, registry credentials, service names, or deployment
assumptions.

## Validation

Focused validation covers SQL projection shape, retained retry generations,
lock and lease state, fence release, campaign pause/admission state, in-flight
members, reconciliation, zero-lock closure, and explicit empty/partial UI
states. No live system is contacted or claimed.
