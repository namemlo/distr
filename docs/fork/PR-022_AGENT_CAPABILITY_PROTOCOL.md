# PR-022 - Agent capability protocol

## Scope

PR-022 adds a generic agent capability advertisement protocol.

It adds:

- `AgentCapabilityReport` rows scoped to an organization and deployment target.
- `AgentActionCapability` rows for supported action types and versions.
- Protocol version `v1`.
- Action version `1` for the existing built-in action registry.
- A feature-flagged agent endpoint for capability reports.
- Docker and Kubernetes agent capability reporting.
- Deployment Plan compatibility blockers when a reported agent does not support an included action.

## Feature flag

The agent capability endpoint is visible only when the agent capability flag is enabled:

```text
DISTR_EXPERIMENTAL_FEATURE_FLAGS=agent_capabilities
```

If the flag is disabled, `POST /api/v1/agents/{id}/capabilities` returns `403 Forbidden`.

Generated Docker and Kubernetes agent manifests include `DISTR_CAPABILITIES_ENDPOINT`. The agent client treats a missing endpoint, `403 Forbidden`, or `404 Not Found` as a no-op so older installs and disabled flags keep existing behavior.

## Database

Migration `123_agent_capabilities` adds:

- `AgentCapabilityReport`
- `AgentActionCapability`

`AgentCapabilityReport` stores:

- organization ID
- deployment target ID
- protocol version
- agent version
- supported runtimes
- operating system
- architecture
- available tooling
- strategy capabilities
- compatibility warnings

`AgentActionCapability` stores supported action type/version sets for each report.

Composite foreign keys preserve organization isolation. Each deployment target has at most one current capability report, and upserts replace the action capability list atomically inside one transaction.

## API

`POST /api/v1/agents/{id}/capabilities`

Request:

```json
{
  "protocolVersion": "v1",
  "agentVersion": "0.0.0-dev",
  "supportedRuntimes": ["docker"],
  "supportedActions": [
    {
      "actionType": "distr.http.check",
      "versions": ["1"]
    }
  ],
  "operatingSystem": "linux",
  "architecture": "amd64",
  "availableTooling": ["docker", "docker-compose"],
  "strategyCapabilities": ["docker-compose"]
}
```

Validation trims string fields, rejects empty values, rejects duplicate runtimes/action types/action versions, rejects unknown action types, and rejects unsupported protocol versions.

The path `{id}` must match the authenticated deployment target. Malformed IDs, path mismatches, and cross-organization targets return not found or unauthorized through the existing agent authentication flow.

## Agent behavior

Docker agents report:

- runtime `docker`
- tooling `docker`, `docker-compose`
- strategy capability `docker-compose`

Kubernetes agents report:

- runtime `kubernetes`
- tooling `kubernetes`, `helm`
- strategy capability `helm`

Both agents initially advertise runtime/tooling support with an empty action list because PR-022 does not add execution adapters. The report happens once per process/config cycle and does not add leases, heartbeats, task claims, task completion, execution adapters, logs, timelines, or deployment execution behavior.

The protocol accepts action capabilities when an agent really supports them. Later execution PRs can add supported action types and versions without changing the `v1` report shape.

## Compatibility validation

Deployment Plan resolution now checks capability reports for selected targets. If a target has a report and an included target-executed plan step uses an unsupported action type/version, including an empty supported-action list, the plan gets an `agent_action_unsupported` blocker.

Hub-executed steps are not checked against agent capabilities. Targets without capability reports do not block existing plans. This keeps PR-022 additive for older agents and disabled feature flags while still blocking known incompatible reported agents.

## Non-goals

PR-022 does not add:

- leases or heartbeats
- agent task endpoints
- task claiming
- task completion
- execution adapters
- approvals
- guided failure
- step events, logs, or timeline APIs
- Angular task queue UI
- deployment target mutation

Those features remain PR-023 or later roadmap work.

## Compatibility notes

Existing Environment, Lifecycle, Channel, Release Bundle, Deployment Process, Process Snapshot, Variable Snapshot, Deployment Plan preview/UI, Task Queue, locks/concurrency, deployment target, deployment, release-name, and frontend planning UI behavior is unchanged except for capability blockers when an agent has explicitly reported incompatible target-executed action support.
