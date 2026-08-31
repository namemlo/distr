# ADR-0074: Executor Runtime Trust Contract

## Status

Accepted

## Context

Executor protocol v2 signs the desired plan, artifact, configuration, adapter,
and fence. That prevents a caller from changing the requested deployment, but
it does not prove which runtime state the executor started from, which adapter
audience received the request, or what artifact/configuration was healthy when
the executor reported success. A successful callback could therefore complete
an attempt without retained runtime proof.

Independent observation remains the authority for promoting desired state.
Executor evidence must strengthen the callback boundary without replacing that
independent observation.

## Decision

New protocol-v2 attempts use runtime contract `v3` and signed intent schema
`distr.execution-intent/v3`. The immutable attempt and intent bind:

- the verified baseline state version and checksum;
- the current image digest, current configuration checksum, and platform;
- a target-scoped caller identity and exact adapter-assignment audience; and
- the existing desired artifact/configuration, adapter revision, and fence.

Attempt creation fails closed when the plan step has no single authoritative,
non-bootstrap verified-v2 baseline for its exact component instance.

The executor records one immutable evidence row through
`POST /api/executor/v2/attempts/{attemptId}/runtime-evidence`. The row binds the
retained intent checksum, executor, fence, caller, audience, baseline
preconditions, pre-execution state, resulting state, platform, health result,
external evidence reference/checksum, and capture time. Exact replay returns
the retained row; a conflicting duplicate is rejected.

`SUCCEEDED` completion requires the retained evidence ID and its server-derived
canonical checksum. The evidence must be healthy and its result image and
configuration must exactly match the signed desired values. Failed, cancelled,
and timed-out completion remain possible without success evidence.

Executor runtime evidence never writes observed component state and never
promotes an active desired revision. The independent observer remains the sole
promotion authority.

## Consequences

- Existing rows are retained explicitly as `legacy-v2`; no historical trust
  facts are fabricated. Pending legacy attempts are excluded from new leases
  and claims after upgrade so they cannot execute and then become unable to
  prove success. Already in-flight legacy attempts remain failure, cancellation,
  or timeout only.
- New v2 execution is stricter and cannot bootstrap from an unverified state.
- Executors must preserve and return the canonical runtime-evidence binding
  before reporting success.
- Operators can see executor runtime evidence alongside intent, event,
  reconciliation, and independent-observation evidence.
- Migration 167 is additive and append-only. Downgrade refuses while v3
  attempts or runtime evidence remain.

## Alternatives Considered

- Trust executor success alone: rejected because it proves neither the starting
  state nor the resulting healthy runtime.
- Let executor evidence promote desired state: rejected because the executor is
  not independent from the deployment action.
- Bind the intent to a claimed executor ID: rejected because intents are signed
  before leasing; doing so would require mutable or re-signed intents.
- Backfill retained attempts with synthetic v3 values: rejected because it
  would create trust assertions that were never observed.

## Validation

- Signed-intent golden, binding, tamper, and trust-policy tests.
- Exact baseline derivation and bootstrap/missing-baseline rejection tests.
- Runtime-evidence API validation, authenticated scope, canonical checksum,
  replay/conflict, lease/fence/intent, and completion-gate tests.
- Migration shape, append-only guard, exact foreign-key lineage, and down
  refusal tests.
- Ordered migration certification is extended through migration 167; live
  PostgreSQL and final release gates remain separately required.
