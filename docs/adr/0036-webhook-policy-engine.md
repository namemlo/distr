# ADR-0036: Webhook Policy Engine

## Status

Accepted

## Context

PR-028 through PR-035 made webhook execution secure, network-hardened, bounded, replay-safe, key-rotation aware, tenant-isolated, auditable, and optionally self-contained. The remaining gap is governance: an otherwise valid webhook can still overload a tenant, agent, corridor, or endpoint if execution is not admitted through a central policy layer.

The Docker agent already owns the final execution decision for `distr.webhook`, so it is the narrowest place to enforce local runtime policy before DNS resolution, signing, and outbound HTTP.

## Decision

Introduce `internal/policy` with a deterministic webhook policy engine. The engine evaluates tenant, agent, corridor, retry, endpoint failure, and circuit-breaker controls from a single input object and returns an allow or deny decision plus a release callback for concurrency accounting.

Docker-agent webhook execution now evaluates policy after replay validation and before DNS, signing, or HTTP execution. Stored-success replay must also pass policy before returning, so replay cannot bypass governance.

Policy configuration is environment-driven and optional. The default configuration preserves existing behavior by allowing all requests. Invalid policy configuration fails closed.

The first implementation uses in-process fixed-window counters and circuit state. That keeps PR-036 schema-free and action-version-compatible. A future distributed deployment can provide Redis-backed or server-backed counters behind the same policy package boundary.

## Consequences

Webhook executions can be blocked before network or signing work when tenant, agent, corridor, retry, endpoint-failure, or circuit-breaker controls deny the request.

Replay remains safe from external side effects but is no longer a governance bypass.

Rate and circuit state is local to the Docker-agent process in this PR. It is deterministic and testable, but it is not a cluster-wide distributed limiter until a later backend is introduced.

