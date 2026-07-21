# ADR-0064: Fenced executor protocol v2

- Status: Accepted
- Date: 2026-07-18

## Context

External execution protocol v1 intentionally provides claim-before-dispatch,
at-most-once delivery and late-callback rejection. An outcome whose delivery
cannot be proven requires a new immutable plan under ADR-0052. Those guarantees
must remain unchanged for plans frozen to `protocol_version=v1`.

Protocol v2 needs retry-safe execution across executor and Hub restarts without
allowing a stale worker, duplicate callback, or mutable artifact/config input to
change the approved operation.

## Decision

Plans frozen to v2 create an `ExecutionAttempt` with a stable
`(execution_id, attempt_number, step_key)` identity. A separately persisted
`ExecutionFence` owns the resource key, monotonically increasing generation and
bounded lease. Every heartbeat, event and completion presents the current
generation. Lease loss fences the attempt, increments the generation and
releases its lease/resource claim.

`ExecutionEvent` is append-only. Its idempotency identity is
`(execution_id, attempt_number, step_key, event_sequence)`. An exact replay
returns the original fact even after terminal completion or delivery-window
expiry; it does not append progress. A conflicting duplicate or out-of-order
sequence is rejected.

The Hub signs these exact canonical intent bytes:

```text
distr.execution-intent.v2
<sha256 checksum>
<canonical JSON payload>
```

The algorithm is Ed25519. `keyId` is the `sha256:` fingerprint of the public
key frozen by the versioned adapter/config revision. The private key is obtained
only through the configured secret-provider signer and is never stored in an
adapter, plan, intent, event, database row, log or API response. Rotation
publishes a new revision/key fingerprint. Trust policies can overlap old and
new public keys during a bounded rollout and retain explicit revocation
evidence.

The signed payload pins the plan checksum, immutable artifact digest, immutable
configuration checksum, adapter revision, resource key, fence generation and
validity interval. Executors verify the checksum, key fingerprint, signature,
expiry, revocation and expected artifact/config values before executing.

Admission is deny-by-default and requires both process flags, scoped enrollment,
an approved and admitted immutable plan, and successful adapter preflight.
Those dependencies are narrow interfaces so their evidence remains owned by
PR-066 through PR-074.

The Hub service registry binds those interfaces into the production router.
The repository reads the exact v2 task, executed immutable plan and attached
passing preflight snapshot, while intent creation reloads the frozen plan,
release, target configuration, step adapter identity and resource lock. An
ordinary replay reuses the stored attempt inputs; only an explicit authorized
retry advances attempt and fence generation.

Executors poll through an atomic lease endpoint. The authenticated bearer
credential is the sole source of organization and deployment-target scope. The
poll declares the exact adapter revision and trusted public-key fingerprint it
can execute; the Hub locks and claims only a pending attempt whose immutable
adapter and signed intent match both values and whose intent and fence remain
valid. Concurrent pollers use `FOR UPDATE SKIP LOCKED`, and an empty poll returns
no content rather than exposing another target's work.

## Compatibility boundary

This decision supersedes only v1 delivery semantics for plans already frozen to
v2. It does not alter `ExternalExecution`, `ExternalExecutionEvent`, their state
transition functions, v1 callback routes, or ADR-0052 retry rules.

## Consequences

- A stale generation can never commit progress or a terminal outcome.
- An expired intent cannot be extended by heartbeat. Expiry recovery fences the
  attempt, advances the generation and releases the active resource claim.
- Repeated task dispatch reuses only an existing attempt with the same immutable
  execution identity and frozen inputs.
- Exact duplicate delivery and callbacks are safe; conflicting duplicates fail
  securely.
- Terminal completion and fencing release leases for restart/retry.
- Key rotation changes immutable adapter/config revision evidence rather than
  mutating an already approved intent.
- A missing enrollment/approval/adapter implementation keeps v2 admission
  closed; it is not approximated from v1 authority.
