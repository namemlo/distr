# ADR-0088: Governed Baseline Adoption

## Status

Accepted

## Context

Native baseline adoption changes authoritative desired state and moves an
immutable deployment plan from `READY` to `EXECUTED`, even though it correctly
creates no deployment task or execution record. The original adoption path
required scoped `plan.execute` authorization and exact runtime evidence, but it
did not require the approval, admission, and persistent review decision that
govern an ordinary native-v2 execution. That allowed an operator to bypass the
review workflow by choosing adoption instead of task execution.

## Decision

Treat baseline adoption as a governed plan outcome. Before any desired-state or
plan mutation, the same serializable transaction must prove:

- a current approval request that is approved and eligible for the adopting
  actor;
- the exact current `ADMIT` evaluation ID and decision checksum supplied by the
  caller;
- a current, unexpired persistent `GO` decision over the same plan and observed
  state;
- the exact GO decision ID and canonical checksum supplied by the caller; and
- transaction-time scoped `plan.execute` authorization.

The immutable baseline-adoption request uses
`distr.baseline-adoption-request/v2` and includes both admission and GO
identities. The correlated `baseline_adoption.adopted` audit event links the
approval request and admission evaluation in typed audit columns and records
the approval revision plus admission and GO checksums in its immutable payload.

The operation remains community-neutral: it introduces no client name,
provider, registry, CI system, or deployment implementation assumption.

## Consequences

Baseline adoption can no longer bypass approval, admission, or GO review. A
stale approval, observation, admission, GO decision, or caller-supplied checksum
fails closed before mutation. Exact idempotent replay of an already committed
adoption remains read-only and returns the retained outcome.

The operator UI enables adoption only when the current review material reports
valid `GO` and `ADMIT` evidence and sends those immutable identities to the API.

## Validation

API validation, repository contract tests, handler authorization checks, and UI
component/service tests cover fail-closed evidence requirements, immutable
request binding, audit correlation, and disabled controls for stale review
material.
