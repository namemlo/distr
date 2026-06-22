# ADR-0028 - Webhook action

## Status

Accepted

## Context

PR-025 through PR-027 introduced target-executed Compose, OCI job, and file render actions through the task lease, heartbeat, and StepRun event protocol. Some deployment processes also need to notify external systems or invoke narrow integration endpoints after a target-side step.

The action sends outbound network requests from customer-controlled agents, so it must not become an SSRF primitive, leak secrets through headers, signatures, request or response bodies, or persist unbounded remote responses.

## Decision

Add a built-in action registry entry for:

```text
distr.webhook
```

The Docker agent advertises `distr.webhook` version `1` and executes it through the existing task lease, heartbeat, StepRun event, output, retry, and reclaim infrastructure.

The action accepts:

- `url`
- `method`
- `headers`
- `secretHeaders`
- `body`
- `sensitiveBody`
- `signingSecret`
- `timeoutSeconds`
- `retry`
- `expectedStatusCodes`
- `idempotencyKey`
- `outputs`

Outbound access is controlled by trusted Docker-agent configuration:

```text
DISTR_WEBHOOK_ALLOWED_HOSTS
DISTR_WEBHOOK_ALLOWED_PRIVATE_HOSTS
```

Action input cannot grant outbound network access. URLs must be HTTPS, must not include credentials, must target allowlisted hosts, and loopback, link-local, private, multicast, and unspecified addresses require an explicit private-host allowlist. The agent disables redirects so a trusted URL cannot silently redirect to another destination.

Public `headers` may not carry authorization-like or reserved signing/idempotency headers. Secret headers are declared in `secretHeaders` as stored secret keys. The server resolves `secretHeaders` and `signingSecret` only while building an authenticated agent lease. Stored Deployment Plans and Process Snapshots keep secret keys only. StepRun event ingestion redacts those resolved values from messages, details, logs, and non-sensitive outputs.

Each request sends deterministic HMAC-SHA256 signing headers over canonical data containing method, path and query, timestamp, idempotency key, and body digest. The same idempotency key is preserved across retry, lease reclaim, and restart by using the action input key or the stable task-lease step key. Request and response bodies are bounded. Response bodies are not logged; only declared JSON Pointer outputs are extracted, type-checked, and emitted. Sensitive declared outputs are emitted as sensitive redacted outputs without retaining the remote value.

The action emits:

- `STARTED` before validation/execution
- `PROGRESS` before each webhook attempt
- `SUCCEEDED` with non-sensitive `statusCode`, `attempts`, and declared non-sensitive outputs
- `FAILED` with redacted errors

## Consequences

- Target-executed tasks can call narrow external integration endpoints without adding a general shell or plugin runner.
- Secrets remain out of stored plans, snapshots, events, logs, outputs, and returned errors.
- Agent operators must configure trusted outbound hosts before the action can run.
- Retries preserve the same idempotency key so receivers can safely deduplicate.

## Alternatives Considered

Allowing arbitrary HTTPS destinations from action input was rejected because Deployment Process authors must not grant network access from target hosts.

Following redirects was rejected for PR-028 because disabling redirects is simpler and prevents redirect-based SSRF.

Persisting full response bodies was rejected because remote APIs may return secrets or large payloads.

Adding UI, approvals, outbox notifications, subscription hooks, Helm actions, or generic plugins was rejected because those belong to later roadmap PRs.

## Validation

Validation added in PR-028 covers:

- action registry schema/order and Docker capability reporting
- unsafe URL, host policy, plaintext public authorization header, output declaration, and retry policy rejection
- deterministic canonical signing and HMAC signatures
- Docker-agent dispatch, heartbeat integration, lifecycle events, retry, stable idempotency key reuse, bounded response handling, declared output extraction, and agent-side redaction
- lease-time webhook secret resolution without storing plaintext secrets in plans or snapshots
- Hub-side webhook StepRun event/log/output redaction and read-path persistence checks
