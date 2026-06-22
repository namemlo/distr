# PR-047b - Observability Tracing

## Summary

PR-047b adds the tracing-only observability slice. It introduces a small tracing abstraction, wraps the existing OpenTelemetry provider stack, gates tracing with `observability_tracing`, adds HTTP request spans, and records task execution lifecycle spans from task state transitions.

## Feature Flag

- `observability_tracing`
- When disabled, the service registry uses no-op tracer providers and HTTP tracing middleware is not installed.
- Existing OTEL exporter environment variables are only used to create exporters when this flag is enabled.

## Tracing

- `internal/observability/tracing`
- `Tracer`, `Span`, `SpanContext`
- `NoopTracer`
- `OtelTracer`

HTTP spans include:

- `http.method`
- `http.route`
- `http.status_code`
- `http.status_class`

Task spans include:

- `task.id`
- `task.status`

Task terminal spans use persisted task start and completion timestamps. Failed task spans are marked as errors.

## Compatibility

- No database migration is required.
- No public API endpoints are added.
- Existing metrics behavior from PR-047a is unchanged.
- Existing RBAC, authentication, action registry, deployment process logic, and task transition semantics are unchanged.
- Existing OTEL provider/exporter configuration is reused only when tracing is enabled.

## Deferred Work

- PR-047c: observability dashboards and link templates.
- Step-level tracing is intentionally excluded.
- Log correlation and business-level spans are intentionally excluded.
- Exporter customization is intentionally excluded.

## Verification

- Tracing package tests cover OTEL span attributes, error status, HTTP route/status spans, no-op tracer behavior, and task lifecycle spans.
- Task queue repository tests cover tracing hooks after task transitions.
- Service tests cover no-op provider behavior when tracing is disabled.
- Feature flag tests cover backend and frontend flag plumbing.
