# ADR-0052: External Execution Callbacks And Observed State

## Status

Accepted

## Context

Some deployments are executed by an external system while Distr remains the release, planning, state, and audit
control plane. A synchronous Hub webhook response proves only that the external system accepted a request. It does
not prove which image and configuration reached the target, whether the target became healthy, or whether the
request later failed. The Hub must also resume safely after restart without creating a second external deployment.

## Decision

Add a provider-neutral `ExternalExecution` record for Hub webhook steps configured with
`completionMode=callback`. Freeze the plan, target, component, expected state version and checksum, release image,
platform, contracts, and versioned configuration reference before the outbound request.

The Hub sends only system-owned callback headers, including the external execution ID, plan checksum, expected
state identity, and callback URL. Process inputs cannot override these headers or the reserved observed-state output
names. The existing HTTPS destination policy, allowlist, signing, retries, idempotency key, response bounds, and
redaction controls still apply to the trigger request.

Callback mode does not expose synchronous response-body outputs because an accepted response can be lost across a
process stop. The external executor reports queue/build identity through `providerReference` and all deployment
results through the durable callback. Existing response-mode webhooks retain their original declared-output budget.

Expose organization-scoped authenticated endpoints to read an execution and record callbacks. A callback carries a
strictly increasing sequence, provider reference, bounded message, and state. Exact replays are idempotent;
conflicting replays, stale sequences, invalid transitions, and updates after terminal state return conflicts.

A successful callback must report the exact frozen version, immutable image digest, platform, contracts, versioned
configuration reference, configuration checksum, and `HEALTHY` result. The transaction verifies the active
target-component task lock, performs an optimistic expected-state update, and appends a target-component
observation linked to the external execution.

The Hub worker first commits a `QUEUED` execution as `RUNNING`, then invokes the external executor. After restart it
waits on an existing `RUNNING` execution and never invokes that execution again. This makes the dispatch boundary
at-most-once. If the Hub stops after committing the claim but before delivery, the execution times out and an
operator creates a new plan. The stable idempotency key remains a second defense at the external executor.

Callbacks received after the durable deadline atomically transition the execution to `TIMED_OUT` and are rejected.
Callback history is capped at 256 events, with the final slot reserved for a terminal event. Provider URLs cannot
contain user information or query parameters, preventing credential-bearing URLs from entering task outputs.

## Consequences

Distr can distinguish requested state from externally observed state and retain a provider-neutral audit history.
An external executor cannot silently substitute another image, architecture, or configuration and still report
success. Hub restarts do not trigger a second deployment for the same external execution.

Migration 136 is additive. It adds external execution and event tables, versioned configuration references on
target-component state and observations, and the organization-scoped Release Bundle key required by the new
foreign key. Downgrade clears observation references before removing PR-052 records and added columns, so an
up/down/up cycle remains valid after real callbacks.

The callback endpoint uses normal Distr bearer or personal-access-token authentication plus vendor organization and
read-write role enforcement. Provider credentials remain in Distr's secret store and callback payloads never carry
them.

## Alternatives Considered

- Treat an accepted webhook response as deployment success. Rejected because acceptance is not observed runtime
  state.
- Store provider-specific Jenkins records. Rejected because the control-plane contract must support any external
  executor.
- Allow callbacks to overwrite the current target state unconditionally. Rejected because another deployment may
  have changed the component after planning.
- Retrigger every callback-mode step after Hub restart. Rejected because external idempotency support cannot be
  assumed to be perfect.
- Allow callback-mode steps to bind synchronous response outputs. Rejected because those values cannot be recovered
  safely when delivery succeeds but the response is lost.

## Validation

- Callback validation, canonical hash, transition, expected-versus-observed, and checksum unit tests.
- Hub worker tests for dispatch-before-webhook ordering, running resume, terminal recovery, callback races, and outputs.
- Authenticated handler, mapping, registry, reserved-header/output, and internal-error redaction tests.
- PostgreSQL 18 migration up/down/up validation across the complete migration chain.
- Live PostgreSQL repository tests for prepare replay, callback replay/conflict, deadline enforcement, lock-release
  serialization, terminal protection, observed-state projection, organization isolation, and event sequencing.
