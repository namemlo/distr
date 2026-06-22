# Observability Tracing

Tracing is enabled by `observability_tracing`. When disabled, the service registry uses no-op tracer providers and HTTP tracing middleware is not installed.

## Provider Behavior

The tracing slice reuses the existing OpenTelemetry provider and exporter configuration. Existing OTEL exporter environment variables are read only when tracing is enabled.

The tracing abstraction includes:

- `Tracer`
- `Span`
- `SpanContext`
- `NoopTracer`
- `OtelTracer`

## Span Shape

HTTP spans include:

- `http.method`
- `http.route`
- `http.status_code`
- `http.status_class`

Task lifecycle spans include:

- `task.id`
- `task.status`

Failed task spans are marked as errors. Terminal task spans use persisted task start and completion timestamps where available.

## Example Flow

A typical trace review starts with the inbound HTTP request and follows the work that request caused:

```text
HTTP request span
  -> task lifecycle span
  -> action execution evidence from task status, step events, or logs
```

The current tracing slice records HTTP and task lifecycle spans. It does not add step-level or action-specific spans. If an operator needs action detail, they should combine the trace ID with task IDs, step events, and dashboard metrics until a future slice adds deeper action instrumentation.

## Boundaries

Tracing does not:

- change metrics behavior,
- add dashboard API fields,
- emit business-level spans,
- add step-level tracing,
- add exporter customization,
- add log correlation.
