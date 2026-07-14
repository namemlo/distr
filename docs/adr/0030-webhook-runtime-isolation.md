# ADR-0030: Webhook Runtime Isolation

## Status

Accepted

## Context

PR-028 introduced `distr.webhook`, and PR-029 hardened the network boundary with DNS preflight validation, pinned dialing, redirect validation, proxy bypass coverage, TLS verification coverage, and retry classification.

The remaining runtime risk was unbounded execution at lower layers or from direct internal callers: a retry loop could exceed the intended global ceiling if validation was bypassed, request contexts were not guaranteed inside `runWebhookAction`, response headers had no explicit size or wait limit, and future attempt metrics needed a non-blocking shape.

## Decision

`runWebhookAction` now derives its own execution context from `timeoutSeconds` when one is configured. That context is used for DNS resolution, each HTTP request, response reads, and retry backoff. The task-lease wrapper still applies the same timeout before calling the action, so direct callers and task execution share the same runtime budget.

Webhook retry attempts are clamped to the global maximum before the retry loop. Input decoding still rejects invalid retry policy values, but the runtime also protects callers that construct `webhookActionInput` directly.

The default webhook transport keeps `Proxy = nil`, uses the PR-029 pinned dialing path, and applies fixed connect, TLS handshake, response header, and response-header-byte limits. Response bodies remain capped through streaming reads rather than full unbounded buffering.

Attempt metrics use an optional channel sink with a non-blocking send. The webhook runtime does not spawn a goroutine for metrics emission, so a blocked sink drops the metric instead of blocking execution or leaking workers.

## Consequences

Webhook execution has a single bounded runtime envelope across DNS, connect, TLS, response headers, response bodies, retries, and backoff.

The webhook schema remains compatible with PR-028 and PR-029. Operators still use `timeoutSeconds`, `retry.maxAttempts`, and `retry.backoffSeconds`; this ADR only tightens how those values are enforced.

Attempt metrics are best-effort. A full sink may drop metrics, which is intentional because webhook execution must not depend on metrics ingestion availability.
