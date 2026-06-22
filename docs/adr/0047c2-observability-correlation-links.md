# ADR-0047c2 - Observability Correlation Links

## Status

Accepted

## Context

PR-047c1 introduced static dashboard definitions and a read-only dashboard catalog. The next planned work is correlation between traces, metrics, and dashboards. That should start as pure link-building utilities before any API response enrichment or UI work.

## Decision

Implement PR-047c2 as a pure correlation link layer:

- add `internal/observability/correlation`,
- provide deterministic builders for Grafana Explore trace links, Grafana Explore metrics links, and static dashboard links,
- add `CorrelationContext`, `TimeRange`, `UnifiedObservabilityInput`, and `UnifiedObservabilityContext`,
- add `OBSERVABILITY_GRAFANA_BASE_URL` as config for future callers,
- add `observability_correlation` as the rollout flag.

The builders are stateless and do not call Grafana APIs. The dashboard API remains unchanged in this slice.

## Consequences

- Link formatting can be reviewed independently before the dashboard endpoint exposes optional link metadata.
- Existing metrics, tracing, and dashboard definitions remain unchanged.
- No storage, background processing, sampling, alerting, or analytics engine is introduced.
- Future API or UI work can reuse the same deterministic link builders.

## Alternatives Considered

- Enrich the dashboard API in this PR: rejected because that is the next safe slice, PR-047c3.
- Call Grafana APIs to validate links: rejected because PR-047c2 is pure link construction only.
- Add log correlation now: rejected because log correlation is explicitly out of scope.
