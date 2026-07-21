# PR-075: Fenced executor protocol v2

## Summary

This slice adds the isolated protocol-v2 persistence, canonical Ed25519-signed
intent contract, fenced state machine, executor API models/routes and
deny-by-default dispatcher interfaces. Existing external execution v1 behavior
is unchanged.

## Schema

Migration 157 adds `ExecutionAttempt`, `ExecutionFence`, append-only
`ExecutionIntent`, append-only `ExecutionEvent`, and the frozen
`Task.protocol_version`. It does not modify `ExternalExecution` or
`ExternalExecutionEvent`.

Rollback takes an access-exclusive lock over `Task` and every protocol-v2
evidence table before checking retained data. It refuses while any task remains
frozen to v2 or any attempt, fence, intent or event exists, so a concurrent
writer cannot race the refusal check and destructive downgrade.

## API and authentication

Executor endpoints are under:

- `POST /api/executor/v2/executions/lease`
- `POST /api/executor/v2/executions/claim`
- `POST /api/executor/v2/attempts/{id}/heartbeat`
- `POST /api/executor/v2/attempts/{id}/events`
- `POST /api/executor/v2/attempts/{id}/complete`

They use the existing agent/executor bearer credential boundary and both v2
process flags. Organization scope comes only from the authenticated credential.
The atomic lease poll also derives deployment-target scope from that credential,
selects with `FOR UPDATE SKIP LOCKED`, and returns `204 No Content` when no
eligible attempt exists. A candidate must be pending, have an unexpired intent
and unreleased fence, and exactly match the executor's frozen adapter revision
and intent signing-key fingerprint. The explicit claim route remains available
for clients that already know an attempt ID.

## Security and compatibility

- Intent signatures use Ed25519 over a domain-separated checksum and canonical
  payload.
- `keyId` is a public-key fingerprint; private keys remain behind the signer
  secret-provider interface.
- Payloads pin immutable plan, artifact, config and adapter revision evidence.
- Ordered event identity rejects conflicting duplicates and stale fences.
- Exact event delivery replays return the stored fact even after the attempt
  becomes terminal or its delivery window closes; they never append progress.
- Duplicate task dispatch returns the existing attempt only when every frozen
  target/task/step/plan/artifact/config/adapter/resource input matches.
- Claiming an expired lease or intent fences the attempt, increments the
  generation and releases the resource. Heartbeats cannot extend an expired
  signed intent.
- Admission requires explicit interfaces for scoped enrollment, approval,
  admission and adapter preflight. A missing dependency denies dispatch.
- V1 task/external-execution statuses, events and retry semantics remain
  unchanged when v2 flags are disabled.

## Production runtime binding

The service registry now creates and injects `executionruntime.Dependencies`
into the real API router. The binding includes the signed protocol dispatcher,
durable task/plan/preflight admission repository, frozen-input loader,
authenticated reconciliation observer gate and campaign-control coordinator.
Normal dispatch reuses the latest matching frozen attempt; an explicit,
retry-authorized campaign handoff alone advances the attempt and fence
generation. Missing or mismatched durable evidence remains fail-closed.
