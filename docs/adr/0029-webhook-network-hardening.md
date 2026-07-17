# ADR-0029: Webhook Network Hardening

## Status

Accepted

## Context

PR-028 introduced the Docker-agent `distr.webhook` action. It already enforced HTTPS URLs, host allowlisting, unsafe IP checks during transport dialing, proxy bypass, bounded request and response bodies, signed JSON requests, retries, and redaction.

The remaining hardening risk was connection-level drift between validation and transport behavior. DNS results could be checked only inside the dial path, test transports could bypass that path, and redirect targets needed explicit DNS validation even though redirects are not followed.

## Decision

The Docker agent now resolves webhook hostnames before any webhook attempt is emitted. It validates every resolved IP address with the existing unsafe-IP policy and fails before HTTP transport use if any result is unsafe.

The outbound transport dials the pinned validated address for the original target. If the transport is ever asked to dial a different host, it resolves and validates that host before dialing.

Redirects remain disabled with `http.ErrUseLastResponse`, but redirect targets are still URL-validated and DNS-validated before the response is returned.

The webhook transport keeps `Proxy = nil`; proxy environment variables are intentionally ignored.

TLS certificate verification remains the Go default. The agent does not add a PR-029 configuration flag to trust self-signed certificates.

DNS, validation, TLS, and oversized-body failures are non-retryable. Transient body read failures remain retryable.

## Consequences

Webhook execution has a stronger trust boundary: the destination is allowlisted, resolved, validated, and pinned before a request attempt is made.

Operators who need private webhook destinations must explicitly allow those hostnames or host:port values in `DISTR_WEBHOOK_ALLOWED_PRIVATE_HOSTS`.

Self-signed webhook endpoints are rejected by default. If support for private trust roots is needed later, it should be introduced as an explicit configuration and documented separately.
