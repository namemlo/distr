# PR-025 - Compose deployment action adapter

## Scope

PR-025 wraps the existing Docker agent Compose deployment behavior as the first target-executed typed action adapter.

It adds:

- built-in `distr.compose.deploy` action registry metadata and input/output schemas
- Docker agent capability reporting for `distr.compose.deploy` version `1`
- Docker agent task-lease execution for Compose deploy steps
- structured step events and outputs for Compose action execution
- Compose options for pull policy, health waiting, timeout, and Compose or Swarm strategy
- task-sourced local deployment state so legacy cleanup does not remove typed task deployments

## Feature flags

PR-025 does not introduce a new feature flag.

End-to-end typed execution still depends on the existing prerequisite feature flags and hidden endpoints from earlier roadmap PRs:

```text
DISTR_EXPERIMENTAL_FEATURE_FLAGS=agent_capabilities,task_queue,agent_task_leases,step_events
```

The Docker agent remains compatible when optional capability, lease, heartbeat, or step-event endpoints are missing or disabled.

## Database

No database migration is added in PR-025.

PR-025 reuses:

- `AgentCapabilityReport` and `AgentActionCapability`
- `Task`
- `StepRun`
- `TaskLease`
- `StepRunEvent`
- `StepRunLogChunk`
- `StepRunOutput`

## API

No new HTTP endpoint is added in PR-025.

PR-025 reuses:

```http
POST /api/v1/agents/{id}/capabilities
POST /api/v1/agents/{id}/lease
POST /api/v1/agents/{id}/tasks/{taskId}/heartbeat
POST /api/v1/agents/{id}/step-runs/{stepRunId}/events
```

The action registry now exposes `distr.compose.deploy` with this input shape:

```json
{
  "applicationVersion": {
    "composeFile": "services:\n  web:\n    image: nginx:latest\n",
    "registryAuth": {
      "registry.example.com": {
        "username": "user",
        "password": "secret"
      }
    }
  },
  "projectName": "distr-preview",
  "environmentFile": "PORT=8080\n",
  "pullPolicy": "missing",
  "waitForHealthy": true,
  "timeoutSeconds": 120,
  "strategy": "compose"
}
```

Supported `pullPolicy` values are:

- `always`
- `missing`
- `if_not_present`
- `never`

Supported `strategy` values are:

- `compose`
- `swarm`

`waitForHealthy` is supported only for the Compose strategy.

## Agent behavior

The Docker agent now reports this supported action:

```json
{
  "actionType": "distr.compose.deploy",
  "versions": ["1"]
}
```

On each loop, after capability reporting and before the legacy resource deployment path, the Docker agent tries to claim one task lease. If a lease is returned, the agent executes the leased steps and then starts the next loop. If no lease is returned, it continues to the existing resource-poll deployment path.

For `distr.compose.deploy`, the agent:

- heartbeats the task lease before and during execution
- emits `STARTED`
- forwards Compose event-processor progress as `PROGRESS`
- applies the Compose or Swarm deployment through the existing Docker agent code
- emits `SUCCEEDED` with `projectName`, `strategy`, `status`, and local deployment state outputs
- emits `FAILED` with the error message and stderr-style status details when apply fails

The typed path uses the existing registry authentication helper and does not add new plaintext secret outputs.

## UI

No Angular route, sidebar entry, or page is added in PR-025.

## Compatibility notes

Existing Docker resource-poll deployment behavior remains available and unchanged when no task lease is claimed.

Legacy local deployment records continue to be cleaned up when the resource response no longer includes them. Task-sourced local deployment records are marked with `source: "task"` and are skipped by legacy cleanup so task deployments are not removed by resource polling.

Existing Kubernetes agent behavior is unchanged in PR-025.

## Non-goals

PR-025 does not add:

- OCI one-shot job actions
- file render actions
- webhook actions
- Helm typed actions
- approvals
- guided failure
- task cancellation
- runbooks
- retention
- timeline UI
- new database tables
- new public API routes

Those features remain later roadmap work.

## Verification

Focused verification:

```text
go test ./internal/actionregistry
go test ./internal/actionregistry ./cmd/agent/docker
```

The Docker-agent package requires the existing agent environment variables during package initialization. Local tests used dummy endpoint values for `DISTR_TARGET_ID`, `DISTR_TARGET_SECRET`, `DISTR_LOGIN_ENDPOINT`, `DISTR_MANIFEST_ENDPOINT`, `DISTR_RESOURCE_ENDPOINT`, `DISTR_STATUS_ENDPOINT`, `DISTR_METRICS_ENDPOINT`, `DISTR_LOGS_ENDPOINT`, and `DISTR_AGENT_LOGS_ENDPOINT`.
