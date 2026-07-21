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

Rollback takes an access-exclusive lock over every protocol-v2 evidence table
before checking retained data, then refuses while any attempt, fence, intent or
event exists. A concurrent writer therefore cannot race the refusal check and
the destructive downgrade.

## API and authentication

Executor endpoints are under:

- `POST /api/executor/v2/executions/claim`
- `POST /api/executor/v2/attempts/{id}/heartbeat`
- `POST /api/executor/v2/attempts/{id}/events`
- `POST /api/executor/v2/attempts/{id}/complete`

They use the existing agent/executor bearer credential boundary and both v2
process flags. Organization scope comes only from the authenticated credential.

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

## Synthetic-base seam

The prepared PR-075 worktree includes PR-063 but not final PR-066 through
PR-074. This change therefore defines the admission and attempt-creation
interfaces without copying speculative authorization, campaign or adapter
storage into PR-075. `executionruntime.Dependencies` is the production router
injection seam; integration must bind it to those predecessor implementations
after the numbered commits are present. Missing dependencies remain fail-closed.
