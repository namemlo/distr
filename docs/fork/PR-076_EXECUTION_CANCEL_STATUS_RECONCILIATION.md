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
- append-only `CampaignMemberTaskExecution`
- append-only `ExecutionCampaignControlHandoff`

It extends the protocol-v2 attempt status constraint with `UNKNOWN`. Every
control row is organization- and execution-scoped. Cancel and status duplicate
keys are idempotent only when their immutable request fields, including the
requested status-query TTL, match. Reconciliation evidence is bound to the
exact status query and attempt in both repository checks and foreign keys.
Every new reconciliation fact requires a new event identity; an exact signed
request replay returns the stored fact so interrupted campaign delivery can be
resumed safely.

Rollback locks the task/campaign parents, attempt and all control/evidence
tables before checking and is refused while control/reconciliation evidence,
campaign lineage/handoffs or unknown attempts exist.

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
- An exact reconciliation replay is idempotent. This allows a failed campaign
  control handoff to retry after the evidence transaction has committed;
  conflicting reuse of the event identity remains rejected.

## Production campaign handoff

`CampaignExecutionControlBridge` is bound in the service registry to the exact,
immutable campaign-run/member-run/task lineage persisted by the campaign
scheduler in the same transaction as task creation. After a cancel fact commits,
the bridge records an idempotent member-scoped `CANCEL_REQUESTED` handoff keyed
by that cancel-request ID; it never infers membership from a reusable plan ID or
cancels an entire campaign run. Non-campaign tasks remain a no-op. An allowed
reconciliation retry reloads the task and enters explicit protocol-v2 retry
dispatch, which advances the attempt identity and fence generation; ordinary
request replay continues returning the existing attempt.

This isolated PR provides `BindCampaignMemberTaskExecution` and its immutable,
idempotent persistence contract. In the final stacked branch, the PR-072
admitted-member scheduler must call it for every task returned by
`CreateTasksForAdmittedV2Plan` before dispatch. The binding cannot be inferred
later from `deployment_plan_id`, because the same plan can participate in more
than one campaign run.
