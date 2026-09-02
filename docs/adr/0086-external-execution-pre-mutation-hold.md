# ADR 0086: One-use external-execution pre-mutation hold

## Status

Accepted for an experimental pilot.

## Context

Operators need a deterministic way to demonstrate that a deployment dependency or
manual intervention can stop an external executor before it mutates a target. A
failure injected inside Jenkins or a client service is too late: it can run after
the adapter has already accepted the request and it couples the proof to an
adopter-specific pipeline.

Distr already has a precise boundary for callback-mode webhook execution. The Hub
persists an `ExternalExecution` as `QUEUED`, atomically claims it as `RUNNING`, and
only then invokes the external executor.

## Decision

Add a default-off experimental control named
`external_execution_pre_mutation_hold`. It is effective only when
`operator_control_plane_v2` is also enabled and an exact JSON binding is supplied
through `DISTR_EXTERNAL_EXECUTION_PRE_MUTATION_HOLD_JSON`.

The binding includes a unique control ID, organization, deployment plan, frozen
plan checksum, deployment target, component, non-secret reason, and absolute UTC
expiry. Distr computes a canonical control checksum over all fields. A separate,
absolute release-file path supplies the protected operator response channel.

At the external-execution dispatch claim, Distr compares every bound field with
the queued execution. For the first exact match it performs one transaction that:

1. locks the control checksum;
2. verifies that no prior consumed audit event exists;
3. appends `external_execution.pre_mutation_hold.armed` and
   `external_execution.pre_mutation_hold.waiting` audit events;
4. leaves the execution `QUEUED` with `triggerAttempts=0` and an explicit WAITING
   message available through the existing external-execution read model.

The Hub records WAITING progress and continues heartbeating the task lease. Its
existing target/component `TaskResourceLock` therefore remains acquired and
conflicting work is rejected by the normal concurrency policy. The Hub does not
invoke the adapter while it polls the protected release file.

Only an exact-bound `RELEASE_FAIL` document resolves the hold before expiry. A
mismatched, malformed, or stale document is inert. At expiry the Hub resolves the
hold as `TIMED_OUT`. Resolution performs one transaction that appends the matching
release/timeout event and the one-use `consumed` event, then records the external
execution as `FAILED` with zero trigger attempts. Normal task failure completion
then releases its lease and resource locks. The consumed audit event prevents the
same control checksum from pausing a retry, including after a Hub restart.

## Consequences

- No executor-adapter, client application, or client database change is needed.
- No new database schema is needed; append-only control-plane audit history stores
  the one-use state and remains available through existing audit/evidence APIs.
- A malformed configured binding or relative release-file path fails Hub
  initialization. Non-matching bindings and release documents are inert.
- Removing the experimental flag is the kill switch. Historical armed, waiting,
  resolution, and consumed evidence remains readable.
- The control is for deterministic acceptance demonstrations, not a general chaos
  testing system or a substitute for policy, approval, or dependency admission.

## Alternatives rejected

- Fail inside Jenkins or the service: this cannot prove pre-mutation behavior and
  requires adopter-specific changes.
- An in-memory one-shot flag: a restart could re-arm it or lose the evidence.
- A new mutable hold table: unnecessary while the append-only audit log can provide
  durable single-consumption state under an advisory lock.
