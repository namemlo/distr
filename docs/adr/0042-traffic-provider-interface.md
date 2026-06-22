# ADR-0042: Traffic Provider Interface

## Status

Accepted

## Context

The roadmap separates rolling state, traffic-provider abstractions, and blue-green behavior. PR-041 introduced rolling state-machine semantics. Before blue-green or traffic shifting can be implemented, the fork needs a provider-agnostic contract that does not assume Envoy, Nginx, cloud load balancers, or any adopter-specific routing product.

The first implementation should be usable by future controllers but remain detached from persistence, API exposure, schedulers, task queues, and agents.

## Decision

Add `internal/traffic/provider` with a `TrafficProvider` interface covering `Prepare`, `Shift`, `Verify`, `Rollback`, and `Cleanup`. Add rollout context, target-set, prepared-target, request, response, capability, and registry types around that interface.

Add a webhook reference provider selected through the registry. The webhook provider posts a generic operation payload to a configured endpoint, forwards idempotency keys, decodes prepare results, and treats non-2xx responses as provider failures. The provider requires HTTPS unless explicitly configured to allow HTTP for local/test use.

Do not add database persistence, public APIs, scheduler workers, task queue changes, agent protocol changes, traffic-product-specific adapters, blue-green logic, or UI changes in this PR.

## Consequences

Future rolling and blue-green controllers can depend on one stable provider contract.

Community adapters can implement Envoy, Nginx, cloud load balancer, or custom routing integrations outside core assumptions.

Existing deployment, task lease, runbook, webhook action, rolling state-machine, Docker/Kubernetes agent, and release-bundle behavior remains unchanged.
