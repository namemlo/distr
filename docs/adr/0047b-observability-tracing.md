# ADR-0047b - Observability Tracing

## Status

Accepted

## Context

PR-047a added the metrics-only observability slice. The next roadmap step is tracing, but tracing must stay separate from dashboards, log correlation, exporter customization, and business-level instrumentation. The repository already had OpenTelemetry dependencies and provider construction, so PR-047b standardizes that runtime behavior behind an explicit feature flag instead of introducing a second tracing stack.

## Decision

Implement PR-047b as the tracing-only observability core:

- add `internal/observability/tracing` with `Tracer`, `Span`, `SpanContext`, `NoopTracer`, and `OtelTracer`,
- add HTTP request tracing middleware with route, method, and status-code attributes,
- add task transition tracing for task start markers and terminal task lifecycle spans,
- gate runtime tracing with `observability_tracing`.

When `observability_tracing` is disabled, the service registry builds no-op providers and the HTTP tracing middleware is not installed. Existing OTEL exporter environment variables are only evaluated for exporter creation when tracing is enabled.

## Consequences

- HTTP request and task execution spans can be reviewed independently from dashboards and links.
- Trace context flows through existing request contexts without refactoring handlers or task execution semantics.
- Existing metrics remain unchanged.
- Existing provider/exporter settings are reused when tracing is enabled; no new vendor exporter or dashboard integration is introduced.
- The feature flag provides a hard off path for request middleware and external exporter activation.

## Alternatives Considered

- Keep tracing always active: rejected because PR-047b requires feature-flagged rollout and a no-op off path.
- Add dashboards and trace links now: rejected because those belong to PR-047c.
- Persist trace context across task rows: rejected for this slice because it would require schema and execution-flow changes beyond minimal tracing instrumentation.
