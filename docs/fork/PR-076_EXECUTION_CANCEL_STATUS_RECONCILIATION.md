# PR-076: Execution cancel, status and reconciliation

## Summary

This slice adds explicit execution cancel requests, status queries and
append-only callback-loss reconciliation evidence. It never converts a missing
callback into success. Success and failure require a proven reconciliation
outcome; otherwise the attempt becomes `UNKNOWN`.

## Schema

Migration 158 adds:

- `ExecutionCancelRequest`
- `ExecutionStatusQuery`
- append-only `ExecutionReconciliationEvent`

It extends the protocol-v2 attempt status constraint with `UNKNOWN`. Every
control row is organization- and execution-scoped. Cancel and status duplicate
keys are idempotent only when their immutable request fields match. Every
reconciliation import requires a new event identity.

Rollback is refused while control/reconciliation evidence or unknown attempts
exist.

## API

Operator endpoints:

- `POST /api/v1/executions/{id}/cancel`
- `POST /api/v1/executions/{id}/status-queries`
- `POST /api/v1/executions/{id}/reconciliation-events`

Executor endpoints:

- `GET /api/executor/v2/attempts/{id}/cancel`
- `POST /api/executor/v2/attempts/{id}/cancel-acknowledgements`
- `GET /api/executor/v2/attempts/{id}/status-query`

Operator mutations require the existing vendor organization, read-write/admin,
non-super-admin and layered v2 feature gates. Executor polling and
acknowledgement use the credential-derived organization and current fence.

## State and retry rules

- Terminal and non-cancellable attempts reject cancel requests.
- An accepted cancel acknowledgement does not invent terminal completion; the
  executor must still report completion or reconciliation evidence.
- A callback at or after the frozen intent expiry is rejected.
- Acknowledged delivery requires a reported, unexpired status query before
  retry.
- Proven success maps to `SUCCEEDED`; proven failure maps to `FAILED`; an
  unproven outcome maps to `UNKNOWN`.
- Retry is allowed only when the frozen step is declared retry-safe, the
  operation is proven incomplete, the reconciliation outcome is `UNKNOWN`, and
  retry was explicitly requested.
- Reconciliation terminalizes the attempt and releases its fence lease.

## Synthetic-base campaign seam

The prepared branch does not include PR-071 through PR-073 campaign storage or
handlers. `CampaignExecutionControlBridge` therefore defines the cancel/retry
binding without copying those predecessor implementations. Integration must
connect campaign member controls to the stored cancel/status/reconciliation
facts after those commits are present.
