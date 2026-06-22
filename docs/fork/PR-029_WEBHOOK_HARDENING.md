# PR-029 - Webhook network hardening

PR-029 hardens the Docker-agent `distr.webhook` action added in PR-028. It keeps the same action inputs, outputs, and hidden task-lease APIs while tightening connection-level safety around DNS, redirects, proxy configuration, TLS, signing, and retry classification.

## Scope

Included:

- pre-transport DNS resolution for webhook hostnames
- validation of every resolved IP with the existing unsafe-IP policy
- pinning the validated DNS result for the outbound dial
- DNS validation of redirect targets even though redirects are not followed
- explicit regression coverage that proxy environment variables do not affect webhook traffic
- default TLS certificate verification coverage
- retry classification coverage for transient body reads versus non-retryable DNS, validation, and oversized-body failures
- deterministic nil-body request signing coverage

Not included:

- UI changes
- database changes
- new public HTTP endpoints
- generic plugin execution
- arbitrary host shell execution
- approval policy behavior

## Behavior

Before a webhook attempt is emitted, the Docker agent parses and validates the configured target URL. For hostnames, the agent resolves the host through the default resolver, validates all returned addresses, and rejects the action if any resolved IP is unsafe unless that host is explicitly allowed through `DISTR_WEBHOOK_ALLOWED_PRIVATE_HOSTS`.

The outbound transport dials the pinned validated IP for the original request. This prevents a second DNS answer from changing the destination between validation and connect.

Redirects remain disabled. If a server returns a redirect, the redirect destination is still URL-validated and DNS-validated before the response is returned to normal status handling.

Webhook transports continue to set `Proxy = nil`, so `HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY` cannot influence webhook routing.

TLS verification uses Go's default HTTPS verification. Self-signed or otherwise untrusted certificates fail by default.

## Retry Rules

Retryable:

- EOF or unexpected EOF while reading a response body
- temporary or timeout body/network read errors
- configured retryable response statuses while attempts remain

Non-retryable:

- URL or input validation failures
- DNS resolution failures
- unsafe IP policy failures
- TLS verification failures
- oversized response bodies
- permanent transport errors

## Verification

Focused Docker-agent tests cover:

- unsafe DNS preflight before attempts
- pinned resolved-address dialing
- redirect target DNS validation
- proxy environment bypass
- untrusted TLS certificate rejection
- nil body and empty object digest equivalence
- retry classification

