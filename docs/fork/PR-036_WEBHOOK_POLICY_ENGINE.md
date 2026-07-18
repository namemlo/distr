# PR-036 - Webhook policy engine

PR-036 adds a central policy enforcement layer for Docker-agent `distr.webhook` execution. The policy gate runs after tenant-scoped replay validation and before DNS resolution, signing, and HTTP transport.

## Scope

Included:

- `internal/policy` package with deterministic webhook policy evaluation
- tenant and agent fixed-window request limits
- per-agent concurrent execution limit
- corridor-level request limits
- per-host circuit breaker deny list with half-open probe support
- retry-storm and endpoint-failure blocking
- optional webhook input metadata: `corridor` and `priority`
- policy enforcement on both new execution and replay-return paths
- fail-closed behavior when policy config is invalid or policy evaluation errors

Not included:

- Redis-backed distributed counters
- database schema changes
- UI changes
- public policy-management APIs
- priority queue scheduling beyond accepting the optional priority field
- action version bump

## Configuration

All policy settings are optional. With no settings, the policy engine allows existing webhook behavior.

- `DISTR_WEBHOOK_TENANT_RPS`: max webhook policy admissions per tenant per second
- `DISTR_WEBHOOK_AGENT_RPS`: max webhook policy admissions per agent per second
- `DISTR_WEBHOOK_AGENT_CONCURRENCY`: max concurrent webhook executions per agent
- `DISTR_WEBHOOK_CORRIDOR_RPS`: comma, semicolon, or newline separated `corridor=limit` entries, for example `PH=10,MY=5`
- `DISTR_WEBHOOK_OPEN_CIRCUIT_HOSTS`: comma, semicolon, or newline separated endpoint hosts to block immediately
- `DISTR_WEBHOOK_MAX_RETRY_ATTEMPTS`: maximum allowed configured webhook retry attempts before treating input as a retry storm
- `DISTR_WEBHOOK_ENDPOINT_FAILURE_LIMIT`: repeated endpoint failure threshold before blocking later requests to the same host

Invalid numeric or corridor policy configuration fails closed during policy evaluation.

## Behavior

`executeWebhookStep` now enforces this order:

1. replay validation and stored-output reconstruction
2. webhook policy evaluation
3. DNS or cached self-contained resolution
4. signing
5. outbound webhook HTTP execution

Replay is not a bypass. If a stored success is found, the replay-return path still evaluates policy before returning success without emitting events or sending HTTP.

Policy denial is recorded as a webhook failure for new executions. Replay denial returns before emitting new events.

## Verification

Focused tests cover:

- tenant rate limiting blocks before DNS
- corridor saturation blocks before DNS
- open circuit breaker blocks before DNS and HTTP
- retry storm blocks at the policy layer
- replay still enforces policy without DNS or HTTP
- agent concurrency slots release after completion
- endpoint failure limit blocks later requests
- action registry accepts optional `corridor` and `priority`
