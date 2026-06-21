# ADR-0024 - Structured step events and logs

## Status

Accepted

## Context

PR-020 introduced durable Tasks and StepRuns. PR-021 added locks and concurrency semantics. PR-022 added agent capability reporting. PR-023 added agent task leases and heartbeats. The next roadmap step needs agents to report step lifecycle events, redacted logs, and step outputs without adding action execution adapters or promotion behavior.

The event protocol must preserve organization isolation, validate agent and lease ownership, support retries, avoid plaintext secret persistence, and remain safe for agents that do not know about step events.

## Decision

Add a feature-flagged structured step event model:

- `StepRunEvent`
- `StepRunLogChunk`
- `StepRunOutput`

Add hidden agent-authenticated ingestion:

```http
POST /api/v1/agents/{id}/step-runs/{stepRunId}/events
```

The endpoint requires:

- `step_events`
- `agent_task_leases`
- `task_queue`
- existing agent token authentication
- path agent ID matching the authenticated deployment target
- StepRun ownership by the current organization
- Task ownership by the authenticated deployment target
- matching TaskLease token hash

Event ingestion runs in one transaction. It locks the Task, StepRun, and TaskLease, validates the requested sequence, accepts exact replay of an existing sequence, rejects sequence gaps, updates StepRun and Task state for lifecycle events, redacts event payloads, and persists events, logs, and outputs.

`STARTED` moves a StepRun from `PENDING` to `RUNNING`. `SUCCEEDED` and `FAILED` move the StepRun to terminal states. A failed StepRun fails the Task. When all StepRuns are terminal and none failed, the Task succeeds. Terminal Task updates release active leases through the existing task update path.

Add read endpoints:

```http
GET /api/v1/tasks/{id}/timeline
GET /api/v1/tasks/{id}/logs
```

These endpoints are organization-scoped and feature-flagged. They expose stored structured event and redacted log data for existing Tasks only.

Generated Docker and Kubernetes manifests include an optional `DISTR_STEP_EVENT_ENDPOINT_TEMPLATE`. The agent client exposes a helper that no-ops when the endpoint is missing or disabled.

## Consequences

- Agents can report durable step lifecycle progress without adding action execution in PR-024.
- Event retries are idempotent by `(step_run_id, task_lease_id, sequence)`.
- Step logs and outputs are bounded before persistence.
- Common secret shapes are centrally redacted before storage and response.
- Sensitive outputs are stored without plaintext values.
- Existing agents remain compatible because the endpoint template is optional.

## Alternatives Considered

Appending unstructured deployment logs to the existing deployment log table was rejected because StepRun events need task, step, lease, agent, and sequence semantics that are distinct from deployment revision logs.

Allowing out-of-order event sequences was rejected because ordered replay and deterministic timelines are harder to reason about during agent retry or restart.

Storing sensitive output plaintext and only redacting on read was rejected because the storage layer should not contain known secret values.

Adding task completion, action execution adapters, Compose execution, approvals, guided failure, or UI timeline behavior now was rejected because those behaviors belong to PR-025 and later roadmap work.
