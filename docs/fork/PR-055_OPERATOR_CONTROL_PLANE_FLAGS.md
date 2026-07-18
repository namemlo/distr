# PR-055 - Operator Control-Plane v2 Isolation Boundary

## Generic User Story

As an operator, I want incomplete control-plane and executor-v2 capabilities isolated behind explicit, layered
kill switches so that existing deployments keep their current behavior while the new workflow is built and
verified incrementally.

## Scope

- Register the process-wide `operator_control_plane_v2` umbrella flag.
- Register the process-wide `executor_protocol_v2` flag independently, but make it effective only when the
  umbrella flag is also effective.
- Keep both flags off by default.
- Expose both keys, descriptions, and effective states through the existing admin feature-flag response.
- Keep historical reads and every existing v1 write/execution path unchanged.
- Prohibit shared or production enablement until PR-083 completes the hardening gate.

## Effective-State Rules

| Configured flags            | Operator control plane v2 | Executor protocol v2 |
| --------------------------- | ------------------------- | -------------------- |
| neither                     | disabled                  | disabled             |
| `operator_control_plane_v2` | enabled                   | disabled             |
| `executor_protocol_v2`      | disabled                  | disabled             |
| both                        | enabled                   | enabled              |

Parsing accepts either registered key so configuration errors remain explicit. The registry reports effective,
not merely configured, state; therefore executor v2 cannot be activated by itself.

## Required Impact Report

### Database/schema impact

None.

### Public API impact

No new route. `GET /api/v1/experimental-feature-flags` adds two registered entries. This is an additive response
change on the existing experimental admin endpoint.

### Frontend/UI impact

The typed feature-flag key union accepts both entries, and Organization Settings can render their labels,
descriptions, milestone, and effective state. PR-055 adds no operator workflow page.

### Agent/protocol impact

None. The existing agent protocol and ADR-0052 external-execution v1 contract are untouched. PR-055 does not add
executor-v2 routes, attempts, retries, or conversion of in-flight work.

### Feature-flag impact

Both flags are default-off process kill switches until resource enrollment is added in PR-066. Removing the
umbrella from the configured keys and restarting the Hub makes executor v2 ineffective even if its key remains
configured. Shared and production environments must keep both disabled until PR-083.

### Security impact

Positive isolation boundary. Incomplete v2 writes and execution cannot become reachable through a partially
configured executor flag. No secret, credential, adopter identity, or infrastructure-provider value is stored.

### Backward-compatibility impact

Existing v1 APIs, deployments, tasks, callbacks, agents, and historical reads continue unchanged. Unknown flag
keys still fail startup validation. `all` includes the two new registered keys, but must not be used in shared or
production environments before PR-083.

## Validation

- Backend tests prove parsing, deterministic registry order, unknown-key rejection, and layered effective state.
- Frontend tests prove both typed keys, labels, and enabled/disabled response state.
- Full feature-flag regressions and the community build run before merge.
- Diff and credential-safety checks confirm the slice remains community-neutral.
