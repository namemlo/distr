# ADR-0031: Webhook Idempotent Replay

## Status

Accepted

## Context

PR-028 added secure webhook execution, PR-029 hardened the network boundary, and PR-030 bounded runtime resources. The remaining risk was replay: if an agent re-entered the same webhook step after a success had already been recorded, it could emit duplicate lifecycle events and send another external HTTP request.

The Hub already stores append-only step events and uses StepRun status to avoid leasing succeeded steps. However, an agent process can still receive or re-run a stale lease object in tests or unusual recovery paths. The agent needed a replay preflight based on stored events before any external side effect.

## Decision

Generated agent manifests now include `DISTR_TASK_TIMELINE_ENDPOINT_TEMPLATE`, a hidden agent-authenticated endpoint scoped to the deployment target and task. The agent client exposes an optional `GetTaskTimeline` helper; missing endpoints remain a no-op for older manifests.

Webhook execution checks the task timeline before it records a STARTED event. A stored SUCCEEDED event for the same step run is treated as authoritative. The agent reconstructs the stored webhook outputs and skips all new events, DNS resolution, signing, transport setup, and HTTP requests.

If the timeline contains prior non-terminal webhook events for the step run but no SUCCEEDED event, the agent fails closed instead of re-sending the external request. This favors exactly-once external side-effect protection over retrying an outcome that cannot be proven from local state.

## Consequences

Webhook replay after stored success is deterministic and side-effect free.

Interrupted webhook replay may require operator intervention because the agent refuses to guess whether the remote endpoint already applied the request.

The behavior is webhook-specific. Other target-executed actions can adopt the same timeline preflight later if they need action-specific replay semantics.

