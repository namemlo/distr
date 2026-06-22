# ADR-0043: Blue-Green Strategy

## Status

Accepted

## Context

PR-041 added rolling state-machine primitives and PR-042 added a provider-agnostic traffic-provider contract. The next roadmap step is blue-green deployment behavior: active and inactive slots, health verification, traffic shift, observation, promotion, retention, and rollback.

The first blue-green implementation should define lifecycle semantics without binding to a database schema, UI, scheduler, agent protocol, or a specific load balancer product.

## Decision

Add `internal/deployments/bluegreen` with a pure lifecycle model. The package defines slots, slot states, lifecycle phases, health checks, retention policies, promotion semantics, and rollback semantics.

Traffic shift and rollback are represented as PR-042 provider request plans. The package does not call providers directly; later orchestration can decide when to execute those requests.

Do not change the rolling state machine, traffic-provider contract, webhook provider, task queue, task leases, agents, public APIs, or UI in this PR.

## Consequences

Future orchestration can persist and execute blue-green lifecycle state using the same semantics.

Traffic-provider implementations remain decoupled from blue-green domain state.

Existing deployment plans, rolling strategy, traffic provider interface, task leases, Docker/Kubernetes agents, runbooks, webhook actions, and release-bundle behavior remain unchanged.
