# PR-030 - Webhook runtime isolation

PR-030 hardens the Docker-agent `distr.webhook` runtime envelope added in PR-028 and network-hardened in PR-029. It keeps the same action schema, outputs, and hidden task-lease APIs while bounding request execution, retry loops, transport waits, response buffering, and attempt metric hooks.

## Scope

Included:

- per-webhook execution deadline applied inside `runWebhookAction`
- global retry ceiling enforced even if callers bypass JSON input validation
- connect timeout for outbound webhook dialing
- TLS handshake timeout and response header timeout on the webhook transport
- maximum response header bytes on the webhook transport
- response body streaming cutoff at the existing maximum body size
- cancellation propagation through HTTP requests and retry backoff
- non-blocking in-process attempt metric sink
- security contract coverage for deadline, cancellation, retry cap, and streaming cutoff behavior

Not included:

- UI changes
- database changes
- new public HTTP endpoints
- changes to the webhook action input/output schema
- configurable timeout knobs beyond the existing `timeoutSeconds` and retry fields
- arbitrary host shell execution
- generic plugin execution

## Behavior

Webhook execution now creates a bounded run context from `timeoutSeconds` inside `runWebhookAction`, not only in the task-lease wrapper. DNS resolution, signing, every HTTP attempt, response reads, and retry backoff all share that same run context.

Retries remain controlled by the action input, but execution clamps attempts to the global webhook maximum before looping. This protects direct internal callers as well as decoded task inputs.

The default webhook HTTP transport now disables proxy use, dials only through the resolved and validated destination from PR-029, and applies fixed connect, TLS handshake, response header, and response header-size limits. Response bodies continue to be streamed through a `LimitReader` so oversized responses stop at `webhookMaxResponseBodyBytes + 1`.

Webhook attempt metrics are emitted through an optional channel sink. The send is non-blocking and has no background goroutine, so a missing, full, or slow sink cannot block webhook execution or leak goroutines.

## Verification

Focused Docker-agent tests cover:

- direct `runWebhookAction` deadline propagation
- retry attempts capped to the global maximum
- transport connect, TLS handshake, response header, and max-header-byte limits
- non-blocking webhook attempt metrics
- cancellation during retry backoff
- streaming response cutoff at the body-size limit
- the expanded `TestWebhookActionSecurityContractSuite`

