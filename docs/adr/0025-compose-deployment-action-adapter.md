# ADR-0025 - Compose deployment action adapter

## Status

Accepted

## Context

PR-020 through PR-024 added durable Tasks, StepRuns, locks, agent capabilities, task leases, heartbeats, and structured step events. PR-025 is the first execution adapter and needs to run Docker Compose deployment work through that typed task protocol while preserving the existing Docker agent resource-poll deployment behavior.

The existing Docker agent already knows how to authenticate registry access, render temporary Compose and environment files, run Compose or Swarm deployment commands, save local deployment state, and send legacy deployment revision status. The typed action should reuse that behavior instead of creating a parallel Compose executor.

## Decision

Add a built-in action registry entry for:

```text
distr.compose.deploy
```

The action accepts:

- `applicationVersion.composeFile`
- `applicationVersion.registryAuth`
- `projectName`
- `environmentFile`
- `pullPolicy`
- `waitForHealthy`
- `timeoutSeconds`
- `strategy`

The Docker agent advertises `distr.compose.deploy` version `1` through the existing capability report protocol. On each loop, it checks for a task lease after reporting capabilities and before the legacy resource deployment path. When no lease endpoint is configured, no work is available, or the endpoint is disabled, the agent continues to the existing resource-poll behavior.

When a lease contains a `distr.compose.deploy` step, the Docker agent:

- heartbeats the lease before and during step execution
- validates and decodes the typed action input
- injects the declared Compose project name into the Compose file
- converts the step into the existing `api.AgentDeployment` shape
- reuses the existing Docker Compose or Swarm deployment path
- emits structured `STARTED`, `PROGRESS`, `SUCCEEDED`, or `FAILED` events through the PR-024 event endpoint
- emits non-secret outputs for `projectName`, `strategy`, `status`, and observed local state

Typed task deployments save local Docker-agent state with `source: "task"`. The legacy cleanup path skips task-sourced local records so a typed task deployment is not removed by the next resource-poll response that does not list the step-run ID as a legacy deployment.

## Consequences

- Docker Compose deployment can run through the target-executed task protocol.
- Existing Docker agent deployment behavior remains the implementation source of truth for Compose and Swarm apply.
- Existing resource-poll deployments continue to work when task lease endpoints are absent or no task is leased.
- The agent can report structured action progress and terminal events for Compose work.
- The planner and capability checks can see Docker agents that support `distr.compose.deploy` version `1`.
- Task-sourced local state is protected from legacy deployment cleanup.

## Alternatives Considered

Adding a new Compose execution implementation was rejected because it would duplicate existing Docker agent behavior and increase drift between legacy and typed deployments.

Executing typed leased tasks after the legacy resource-poll deployment loop was rejected because a long-running legacy deployment could delay queued target-executed tasks and because the task protocol already provides explicit leases and heartbeats.

Saving task deployments as indistinguishable legacy deployment records was rejected because the existing cleanup loop removes local records not present in the resource response. A task-sourced marker keeps backward compatibility while avoiding immediate cleanup of typed deployments.

Adding OCI, file render, webhook, approvals, cancellation, task timeline UI, or broader runbook execution behavior was rejected because those belong to later roadmap PRs.
