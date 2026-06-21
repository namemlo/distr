# PR-024 - Structured step events and logs

## Scope

PR-024 adds durable step-run lifecycle events, redacted step log transport, and bounded step output storage on top of the Task Queue and Agent Task Lease model.

It adds:

- `StepRunEvent` rows scoped to an organization, Task, StepRun, TaskLease, and deployment target agent.
- `StepRunLogChunk` rows for redacted stdout, stderr, and system log chunks.
- `StepRunOutput` rows for bounded named outputs, with sensitive outputs stored without plaintext values.
- Hidden agent-authenticated event ingestion.
- Task timeline and task log read APIs.
- Agent client helpers and generated manifest endpoint configuration.
- Centralized redaction for common token, password, secret, API key, and bearer-token shapes.

## Feature flag

Step event APIs are visible only when the PR-024 flag and the prerequisite task/lease flags are enabled:

```text
DISTR_EXPERIMENTAL_FEATURE_FLAGS=task_queue,agent_task_leases,step_events
```

The task timeline and task log read endpoints also continue to require the existing Task Queue prerequisite flags.

Generated Docker and Kubernetes manifests include:

```text
DISTR_STEP_EVENT_ENDPOINT_TEMPLATE
```

The agent client treats a missing, disabled, or absent step-event endpoint as no-op compatibility behavior.

## Database

Migration `125_step_events` adds:

- `StepRunEvent`
- `StepRunLogChunk`
- `StepRunOutput`
- composite uniqueness constraints on `StepRun` and `TaskLease` so event rows can enforce same-organization task, step, lease, and agent ownership

`StepRunEvent` stores:

- organization ID
- task ID
- step run ID
- task lease ID
- agent deployment target ID
- monotonically increasing per-step/per-lease sequence number
- lifecycle event type
- optional message, progress percent, redacted details, and redaction marker
- a redacted canonical payload hash used to validate idempotent replays

The `(step_run_id, task_lease_id, sequence)` uniqueness rule makes retries idempotent. Reposting an already-recorded sequence for the same lease returns the stored event only when the canonical redacted payload matches the stored payload hash. A changed replay returns `409 Conflict`.

`StepRunLogChunk` stores bounded redacted log bodies with stream, severity, and deterministic chunk ordering.

`StepRunOutput` stores immutable per-event named output history. A StepRun is limited to the same bounded set of distinct output names across its events; repeated names are allowed on later events to preserve output history. Sensitive outputs and outputs containing detected secret patterns are marked redacted; sensitive output values are not stored.

## API

Hidden agent-authenticated endpoint:

```http
POST /api/v1/agents/{id}/step-runs/{stepRunId}/events
```

Request body:

```json
{
  "leaseToken": "opaque-token",
  "sequence": 1,
  "type": "STARTED",
  "message": "starting",
  "progressPercent": 0,
  "details": {},
  "logs": [
    {
      "stream": "stdout",
      "severity": "info",
      "body": "started"
    }
  ],
  "outputs": [
    {
      "name": "url",
      "value": "https://example.com"
    }
  ]
}
```

Supported event types:

- `STARTED`
- `PROGRESS`
- `LOG`
- `OUTPUT`
- `SUCCEEDED`
- `FAILED`

The endpoint:

- requires the path agent ID to match the authenticated deployment target
- requires the StepRun to belong to a Task assigned to that deployment target and organization
- requires the lease token to match the Task's lease
- rejects expired or released leases for new events
- accepts exact replay of an already-recorded sequence
- rejects replay of an already-recorded sequence when the redacted payload changed
- rejects sequence gaps
- rejects events that would exceed the StepRun distinct output-name limit
- redacts messages, details, log bodies, and non-sensitive output values before persistence

Malformed UUIDs return `404 Not Found`. Invalid payloads return `400 Bad Request`. Missing or cross-organization resources return `404 Not Found`. Expired leases, released leases, invalid state transitions, and sequence gaps return `409 Conflict`.

Read endpoints:

```http
GET /api/v1/tasks/{id}/timeline
GET /api/v1/tasks/{id}/logs
```

Both preserve organization isolation and return `404 Not Found` for missing or cross-organization Tasks.

Timeline and log reads order events by StepRun sort order, TaskLease attempt, event sequence, and stable row IDs so reclaimed leases with sequence numbers starting at `1` do not reorder earlier attempt history.

## Lifecycle behavior

`STARTED` transitions a pending StepRun to `RUNNING`.

`PROGRESS`, `LOG`, and `OUTPUT` require the StepRun to be `RUNNING` and do not otherwise change StepRun state.

`SUCCEEDED` transitions the StepRun to `SUCCEEDED`. If all StepRuns for the Task are terminal and none failed, the Task transitions to `SUCCEEDED` and its active leases are released.

`FAILED` transitions the StepRun to `FAILED`, transitions the Task to `FAILED`, and releases active leases.

## Non-goals

PR-024 does not add:

- action execution adapters
- Compose, OCI, file, webhook, or deployment adapters
- release promotion execution
- deployment planning changes
- task cancellation
- task completion endpoint
- approvals
- guided failure
- retention
- notifications
- Angular task timeline UI
- PR-025 or later behavior

Those features remain later roadmap work.

## Compatibility notes

Existing Environment, Lifecycle, Channel, Release Bundle, Deployment Process, Process Snapshot, Variable Snapshot, Deployment Plan preview/UI, Task Queue create/read APIs, locks/concurrency semantics, deployment target, deployment, release-name, frontend planning UI, and current deployment behavior are unchanged.

Existing agents without `DISTR_STEP_EVENT_ENDPOINT_TEMPLATE` continue to run unchanged. Agents that receive the endpoint can post structured events, but no existing agent execution loop is changed in PR-024.
