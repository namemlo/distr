package tracing

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	. "github.com/onsi/gomega"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestOtelTracerRecordsSpanAttributesAndErrors(t *testing.T) {
	g := NewWithT(t)
	recorder := tracetest.NewSpanRecorder()
	provider := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))
	tracer := NewOtelTracer(provider, "test-service")

	ctx, span := tracer.Start(context.Background(), "task.execution", SpanStartOptions{
		Attributes: []Attribute{{Key: "task.status", Value: "running"}},
	})
	span.SetAttributes(Attribute{Key: "task.id", Value: "task-1"})
	span.RecordError(errors.New("boom"))
	span.End(SpanEndOptions{})

	g.Expect(ctx).NotTo(BeNil())
	g.Expect(span.Context().TraceID).NotTo(BeEmpty())
	ended := recorder.Ended()
	g.Expect(ended).To(HaveLen(1))
	g.Expect(ended[0].Name()).To(Equal("task.execution"))
	g.Expect(stringAttribute(ended[0], "task.status")).To(Equal("running"))
	g.Expect(stringAttribute(ended[0], "task.id")).To(Equal("task-1"))
	g.Expect(ended[0].Status().Code).To(Equal(codes.Error))
}

func TestHTTPMiddlewareCreatesRequestSpan(t *testing.T) {
	g := NewWithT(t)
	recorder := tracetest.NewSpanRecorder()
	provider := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))
	tracer := NewOtelTracer(provider, "test-service")
	router := chi.NewRouter()
	router.Use(HTTPMiddleware(tracer))
	router.Get("/api/v1/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/tasks/task-1", nil))

	ended := recorder.Ended()
	g.Expect(ended).To(HaveLen(1))
	g.Expect(ended[0].Name()).To(Equal("GET /api/v1/tasks/{id}"))
	g.Expect(stringAttribute(ended[0], "http.method")).To(Equal(http.MethodGet))
	g.Expect(stringAttribute(ended[0], "http.route")).To(Equal("/api/v1/tasks/{id}"))
	g.Expect(intAttribute(ended[0], "http.status_code")).To(Equal(int64(http.StatusCreated)))
}

func TestNoopTracerSkipsHTTPMiddleware(t *testing.T) {
	g := NewWithT(t)
	router := chi.NewRouter()
	router.Use(HTTPMiddleware(NoopTracer{}))
	router.Get("/status", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/status", nil))

	g.Expect(recorder.Code).To(Equal(http.StatusNoContent))
	_, span := NoopTracer{}.Start(context.Background(), "noop", SpanStartOptions{})
	span.RecordError(errors.New("ignored"))
	span.End(SpanEndOptions{})
	g.Expect(span.Context().TraceID).To(BeEmpty())
}

func TestObserveTaskTransitionRecordsLifecycleSpan(t *testing.T) {
	g := NewWithT(t)
	recorder := tracetest.NewSpanRecorder()
	provider := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))
	tracer := NewOtelTracer(provider, "test-service")
	started := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	completed := started.Add(2 * time.Second)

	ObserveTaskTransition(context.Background(), tracer, TaskObservation{
		TaskID:      "task-1",
		Status:      "failed",
		StartedAt:   &started,
		CompletedAt: &completed,
	})

	ended := recorder.Ended()
	g.Expect(ended).To(HaveLen(1))
	g.Expect(ended[0].Name()).To(Equal("task.execution"))
	g.Expect(ended[0].StartTime()).To(Equal(started))
	g.Expect(ended[0].EndTime()).To(Equal(completed))
	g.Expect(stringAttribute(ended[0], "task.id")).To(Equal("task-1"))
	g.Expect(stringAttribute(ended[0], "task.status")).To(Equal("failed"))
	g.Expect(ended[0].Status().Code).To(Equal(codes.Error))
}

func stringAttribute(span trace.ReadOnlySpan, key string) string {
	for _, attr := range span.Attributes() {
		if string(attr.Key) == key {
			return attr.Value.AsString()
		}
	}
	return ""
}

func intAttribute(span trace.ReadOnlySpan, key string) int64 {
	for _, attr := range span.Attributes() {
		if string(attr.Key) == key {
			return attr.Value.AsInt64()
		}
	}
	return 0
}
