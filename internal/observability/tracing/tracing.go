package tracing

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
)

type Attribute struct {
	Key   string
	Value any
}

type SpanContext struct {
	TraceID string
	SpanID  string
	Sampled bool
}

type SpanStartOptions struct {
	Attributes []Attribute
	StartTime  time.Time
}

type SpanEndOptions struct {
	EndTime time.Time
}

type Tracer interface {
	Enabled() bool
	Start(context.Context, string, SpanStartOptions) (context.Context, Span)
}

type Span interface {
	End(SpanEndOptions)
	SetName(string)
	SetAttributes(...Attribute)
	RecordError(error)
	Context() SpanContext
}

type Tracers struct {
	Default Tracer
	Agent   Tracer
}

type NoopTracer struct{}

func (NoopTracer) Enabled() bool {
	return false
}

func (NoopTracer) Start(ctx context.Context, _ string, _ SpanStartOptions) (context.Context, Span) {
	return ctx, NoopSpan{}
}

type NoopSpan struct{}

func (NoopSpan) End(SpanEndOptions)         {}
func (NoopSpan) SetName(string)             {}
func (NoopSpan) SetAttributes(...Attribute) {}
func (NoopSpan) RecordError(error)          {}
func (NoopSpan) Context() SpanContext       { return SpanContext{} }

type OtelTracer struct {
	tracer oteltrace.Tracer
}

func NewOtelTracer(provider oteltrace.TracerProvider, instrumentationName string) *OtelTracer {
	if provider == nil {
		return &OtelTracer{tracer: oteltrace.NewNoopTracerProvider().Tracer(instrumentationName)}
	}
	return &OtelTracer{tracer: provider.Tracer(instrumentationName)}
}

func (t *OtelTracer) Enabled() bool {
	return t != nil && t.tracer != nil
}

func (t *OtelTracer) Start(ctx context.Context, name string, options SpanStartOptions) (context.Context, Span) {
	if !t.Enabled() {
		return ctx, NoopSpan{}
	}
	startOptions := []oteltrace.SpanStartOption{
		oteltrace.WithAttributes(toOtelAttributes(options.Attributes)...),
	}
	if !options.StartTime.IsZero() {
		startOptions = append(startOptions, oteltrace.WithTimestamp(options.StartTime))
	}
	ctx, span := t.tracer.Start(ctx, name, startOptions...)
	return ctx, otelSpan{span: span}
}

type otelSpan struct {
	span oteltrace.Span
}

func (s otelSpan) End(options SpanEndOptions) {
	if s.span == nil {
		return
	}
	if options.EndTime.IsZero() {
		s.span.End()
		return
	}
	s.span.End(oteltrace.WithTimestamp(options.EndTime))
}

func (s otelSpan) SetName(name string) {
	if s.span != nil {
		s.span.SetName(name)
	}
}

func (s otelSpan) SetAttributes(attrs ...Attribute) {
	if s.span != nil {
		s.span.SetAttributes(toOtelAttributes(attrs)...)
	}
}

func (s otelSpan) RecordError(err error) {
	if s.span == nil || err == nil {
		return
	}
	s.span.RecordError(err)
	s.span.SetStatus(codes.Error, err.Error())
}

func (s otelSpan) Context() SpanContext {
	if s.span == nil {
		return SpanContext{}
	}
	spanContext := s.span.SpanContext()
	return SpanContext{
		TraceID: spanContext.TraceID().String(),
		SpanID:  spanContext.SpanID().String(),
		Sampled: spanContext.IsSampled(),
	}
}

type TaskObservation struct {
	TaskID      string
	Status      string
	StartedAt   *time.Time
	CompletedAt *time.Time
}

func HTTPMiddleware(tracer Tracer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if tracer == nil || !tracer.Enabled() {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, span := tracer.Start(r.Context(), r.Method, SpanStartOptions{
				Attributes: []Attribute{{Key: "http.method", Value: r.Method}},
			})
			ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
			r = r.WithContext(ctx)
			var recovered any
			defer func() {
				statusCode := ww.Status()
				if statusCode == 0 {
					statusCode = http.StatusOK
				}
				route := routePattern(r)
				span.SetName(strings.TrimSpace(r.Method + " " + route))
				span.SetAttributes(
					Attribute{Key: "http.method", Value: r.Method},
					Attribute{Key: "http.route", Value: route},
					Attribute{Key: "http.status_code", Value: statusCode},
					Attribute{Key: "http.status_class", Value: statusClass(statusCode)},
				)
				if recovered != nil {
					span.RecordError(fmt.Errorf("panic: %v", recovered))
				}
				span.End(SpanEndOptions{})
				if recovered != nil {
					panic(recovered)
				}
			}()
			defer func() {
				recovered = recover()
			}()
			next.ServeHTTP(ww, r)
		})
	}
}

func ObserveTaskTransition(ctx context.Context, tracer Tracer, observation TaskObservation) {
	if tracer == nil || !tracer.Enabled() {
		return
	}
	status := normalizeStatus(observation.Status)
	attrs := []Attribute{
		{Key: "task.id", Value: observation.TaskID},
		{Key: "task.status", Value: status},
	}
	if status == "running" {
		startTime := valueTime(observation.StartedAt)
		_, span := tracer.Start(ctx, "task.execution.start", SpanStartOptions{
			Attributes: attrs,
			StartTime:  startTime,
		})
		span.End(SpanEndOptions{EndTime: startTime})
		return
	}
	if observation.StartedAt == nil || observation.CompletedAt == nil {
		return
	}
	_, span := tracer.Start(ctx, "task.execution", SpanStartOptions{
		Attributes: attrs,
		StartTime:  *observation.StartedAt,
	})
	if status == "failed" {
		span.RecordError(errors.New("task failed"))
	}
	span.End(SpanEndOptions{EndTime: *observation.CompletedAt})
}

func routePattern(r *http.Request) string {
	if routeContext := chi.RouteContext(r.Context()); routeContext != nil {
		if pattern := routeContext.RoutePattern(); pattern != "" {
			return pattern
		}
	}
	return "unmatched"
}

func statusClass(statusCode int) string {
	if statusCode <= 0 {
		return "unknown"
	}
	return strconv.Itoa(statusCode/100) + "xx"
}

func normalizeStatus(status string) string {
	if status == "" {
		return "unknown"
	}
	return strings.ToLower(status)
}

func valueTime(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

func toOtelAttributes(attrs []Attribute) []attribute.KeyValue {
	otelAttrs := make([]attribute.KeyValue, 0, len(attrs))
	for _, attr := range attrs {
		if attr.Key == "" {
			continue
		}
		switch value := attr.Value.(type) {
		case string:
			otelAttrs = append(otelAttrs, attribute.String(attr.Key, value))
		case bool:
			otelAttrs = append(otelAttrs, attribute.Bool(attr.Key, value))
		case int:
			otelAttrs = append(otelAttrs, attribute.Int(attr.Key, value))
		case int64:
			otelAttrs = append(otelAttrs, attribute.Int64(attr.Key, value))
		case float64:
			otelAttrs = append(otelAttrs, attribute.Float64(attr.Key, value))
		default:
			otelAttrs = append(otelAttrs, attribute.String(attr.Key, fmt.Sprint(value)))
		}
	}
	return otelAttrs
}
