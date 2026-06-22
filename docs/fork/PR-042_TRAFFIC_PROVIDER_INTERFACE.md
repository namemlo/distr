# PR-042 - Traffic-provider interface

PR-042 adds a provider-agnostic traffic-control layer for future progressive delivery work. It defines the traffic-provider contract, capability model, provider registry, and a webhook reference provider without wiring database persistence, APIs, schedulers, tasks, agents, or blue-green behavior.

## Scope

Included:

- `internal/traffic/provider` package
- `TrafficProvider` contract with `Prepare`, `Shift`, `Verify`, `Rollback`, and `Cleanup`
- rollout context, target-set, prepared-target, and operation request/response models
- provider capability advertisement with operation support checks
- provider registry/factory for selecting implementations by type
- `WebhookTrafficProvider` reference implementation
- webhook operation payloads with operation, rollout context, targets, parameters, and idempotency key header
- HTTPS-by-default webhook URL validation with explicit local/test HTTP allowance

Not included:

- database schema or repository persistence
- public API exposure
- scheduler, task queue, task lease, or worker execution wiring
- agent protocol changes
- Envoy, Nginx, cloud load balancer, or proxy-specific adapters
- blue-green, canary, or weighted-traffic strategy logic
- UI changes

## Provider Contract

Traffic providers expose:

- `Prepare`
- `Shift`
- `Verify`
- `Rollback`
- `Cleanup`

The contract uses abstract rollout context and target sets so future rolling and blue-green controllers can call providers without depending on a specific proxy, load balancer, or network product.

## Webhook Provider

The webhook reference provider sends a JSON payload to a configured endpoint for each operation. It includes the operation name, rollout context, target set, and operation parameters. When an idempotency key is present on the rollout context, the provider forwards it as the `Idempotency-Key` header.

The provider requires HTTPS by default. HTTP is allowed only when explicitly configured, which keeps local tests simple without relaxing production defaults.

## Verification

Focused Go tests cover:

- default registry webhook provider construction
- capability operation checks
- webhook operation payloads and idempotency header forwarding
- prepare response decoding
- non-success webhook status failure handling
- unsafe webhook configuration rejection
- duplicate and unknown provider registry handling
