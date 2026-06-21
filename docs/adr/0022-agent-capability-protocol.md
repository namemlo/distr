# ADR-0022 - Agent capability protocol

## Status

Accepted

## Context

PR-020 and PR-021 introduced durable Tasks and resource locks but intentionally left agent task leasing, heartbeats, execution adapters, and completion for later roadmap pull requests. PR-022 needs a small protocol boundary so agents can advertise what they can support before execution features arrive.

The protocol must stay generic, organization-scoped, feature-flagged, and safe for existing agents that know nothing about capability reporting.

## Decision

Add a versioned `v1` capability report stored per deployment target.

The report includes:

- agent version
- supported runtimes
- supported built-in action types and versions
- operating system and architecture
- available tooling
- strategy capabilities
- compatibility warnings

The Hub exposes `POST /api/v1/agents/{id}/capabilities` behind the `agent_capabilities` experimental flag. The endpoint uses existing agent token authentication and requires the path ID to match the authenticated deployment target.

Generated Docker and Kubernetes manifests include `DISTR_CAPABILITIES_ENDPOINT`. Agents post a report once per process/config cycle and treat a missing endpoint, disabled endpoint, or absent route as a no-op.

PR-022 agents report runtime and tooling support with an empty supported-action list because execution adapters are not part of this PR. The protocol still accepts action type/version entries from agents that actually support them.

Deployment Plan resolution checks existing reports and adds an `agent_action_unsupported` blocker when a reported target does not support an included step action at version `1`. Missing reports do not block plans in PR-022.

## Consequences

- Capability reports are organization-scoped and cannot be written across organizations.
- Report upserts replace the action capability list atomically.
- Reported incompatible agents block affected Deployment Plans before execution features exist.
- Older agents and disabled feature flags preserve existing behavior.
- Later lease, heartbeat, and execution PRs can reuse the report tables and protocol version without changing PR-022 API semantics.

## Alternatives Considered

Blocking plans when a target has no capability report was rejected for PR-022 because existing agents do not report capabilities and the feature is still experimental.

Adding leases, heartbeats, task claims, or execution adapters now was rejected because those behaviors belong to PR-023 and later roadmap work.

Using an unversioned report shape was rejected because agent protocol compatibility needs an explicit version boundary before execution behavior is introduced.
