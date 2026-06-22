# ADR-0047a - Observability Metrics

## Status

Accepted

## Context

The roadmap calls for an observability package with metrics, trace spans, external link templates, and dashboard examples. That is too large for one safe PR because metrics, tracing, and dashboard templates have different runtime risks and dependencies. The repository already has deployment-target metrics and a Prometheus server path, so the first slice should reuse that direction without introducing tracing or vendor-specific systems.

## Decision

Split PR-047 and implement only the metrics foundation first. PR-047a adds:

- an `internal/observability/metrics` package with recorder abstractions,
- a Prometheus recorder implementation,
- HTTP request metrics middleware,
- task execution transition hooks,
- `observability_metrics` experimental feature flag gating.

The metrics server remains controlled by `METRICS_ENABLED=true`, and `observability_metrics` must also be enabled before `/metrics` is exposed for this observability slice. When the flag is disabled, the new HTTP middleware is not installed.

## Consequences

- Metrics can be reviewed and rolled out independently from tracing and dashboard work.
- Every new core metric includes the required `service`, `environment`, and `version` labels.
- Request-path instrumentation stays lightweight and only runs when enabled.
- Task duration is recorded from persisted task start/completion timestamps after terminal transitions.
- Existing deployment-target metrics, logging, tracing, RBAC, authentication, and action registry behavior stay unchanged.

## Alternatives Considered

- Add metrics, tracing, and dashboards together: rejected because it would create a large, hard-to-review observability PR.
- Use OpenTelemetry metrics immediately: rejected for this slice to avoid pulling tracing/exporter decisions into metrics plumbing.
- Add business metrics now: rejected because PR-047a is limited to system-level HTTP and task execution metrics.
