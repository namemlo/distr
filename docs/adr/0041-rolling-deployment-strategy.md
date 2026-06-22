# ADR-0041: Rolling Deployment Strategy

## Status

Accepted

## Context

The roadmap calls for rolling deployment support before traffic-provider and blue-green work. Rolling deployments need deterministic target windows, per-target state, and failure-threshold handling before scheduler or task execution wiring can safely use them.

Existing deployment plans already contain ordered deployment targets, and task execution already has its own queue and lease model. PR-041 should define reusable rolling semantics without changing those execution paths yet.

## Decision

Add an `internal/deployments` package with a pure rolling state machine. The package defines rolling target states, rollout phases, window configuration, failure-threshold configuration, and state transitions.

`StartNextWindow` starts only the next pending subset and respects both `window_size` and `maximum_unavailable`. A new window cannot start until the current window has only terminal targets. Failure thresholds support absolute and percentage limits and can pause or abort the rollout.

Do not add database persistence, public APIs, UI, scheduler workers, task queue changes, traffic-provider integration, or agent protocol changes in this PR.

## Consequences

Later execution work can reuse the same state-machine semantics when persistence and scheduler integration are introduced.

Rolling behavior remains deterministic and testable without requiring a live task queue or agent.

Existing deployment plans, task leases, Docker/Kubernetes agents, runbooks, webhook actions, and release-bundle behavior remain unchanged.
