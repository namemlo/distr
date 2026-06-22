package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
)

const namespace = "distr"

type Counter interface {
	Inc(labelValues ...string)
	Add(value float64, labelValues ...string)
}

type Gauge interface {
	Set(value float64, labelValues ...string)
	Inc(labelValues ...string)
	Dec(labelValues ...string)
}

type Histogram interface {
	Observe(value float64, labelValues ...string)
}

type Recorder interface {
	ObserveHTTPRequest(HTTPObservation)
	ObserveTask(TaskObservation)
}

type BaseLabels struct {
	Service     string
	Environment string
	Version     string
}

type HTTPObservation struct {
	Method     string
	Route      string
	StatusCode int
	Duration   time.Duration
}

type TaskObservation struct {
	Status   string
	Duration time.Duration
}

type NoopRecorder struct{}

func (NoopRecorder) ObserveHTTPRequest(HTTPObservation) {}
func (NoopRecorder) ObserveTask(TaskObservation)        {}

type PrometheusRecorder struct {
	base BaseLabels

	httpRequests *prometheus.CounterVec
	httpErrors   *prometheus.CounterVec
	httpLatency  *prometheus.HistogramVec
	taskRuns     *prometheus.CounterVec
	taskDuration *prometheus.HistogramVec
}

var _ Recorder = (*PrometheusRecorder)(nil)
var _ prometheus.Collector = (*PrometheusRecorder)(nil)

func NewPrometheusRecorder(base BaseLabels) *PrometheusRecorder {
	recorder := &PrometheusRecorder{base: normalizeBaseLabels(base)}
	httpLabels := []string{"service", "environment", "version", "method", "route", "status_code", "status_class"}
	taskLabels := []string{"service", "environment", "version", "status"}

	recorder.httpRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "http_requests_total",
			Help:      "Total number of HTTP requests.",
		},
		httpLabels,
	)
	recorder.httpErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "http_errors_total",
			Help:      "Total number of HTTP requests returning 4xx or 5xx responses.",
		},
		httpLabels,
	)
	recorder.httpLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "http_request_duration_seconds",
			Help:      "HTTP request latency in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
		httpLabels,
	)
	recorder.taskRuns = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "task_executions_total",
			Help:      "Total number of task execution state transitions.",
		},
		taskLabels,
	)
	recorder.taskDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "task_duration_seconds",
			Help:      "Task execution duration in seconds for terminal task states.",
			Buckets:   prometheus.DefBuckets,
		},
		taskLabels,
	)

	return recorder
}

func (r *PrometheusRecorder) Describe(ch chan<- *prometheus.Desc) {
	r.httpRequests.Describe(ch)
	r.httpErrors.Describe(ch)
	r.httpLatency.Describe(ch)
	r.taskRuns.Describe(ch)
	r.taskDuration.Describe(ch)
}

func (r *PrometheusRecorder) Collect(ch chan<- prometheus.Metric) {
	r.httpRequests.Collect(ch)
	r.httpErrors.Collect(ch)
	r.httpLatency.Collect(ch)
	r.taskRuns.Collect(ch)
	r.taskDuration.Collect(ch)
}

func (r *PrometheusRecorder) ObserveHTTPRequest(observation HTTPObservation) {
	if r == nil {
		return
	}
	labels := r.httpLabelValues(observation)
	r.httpRequests.WithLabelValues(labels...).Inc()
	r.httpLatency.WithLabelValues(labels...).Observe(observation.Duration.Seconds())
	if observation.StatusCode >= http.StatusBadRequest {
		r.httpErrors.WithLabelValues(labels...).Inc()
	}
}

func (r *PrometheusRecorder) ObserveTask(observation TaskObservation) {
	if r == nil {
		return
	}
	labels := []string{r.base.Service, r.base.Environment, r.base.Version, normalizeLabel(observation.Status)}
	r.taskRuns.WithLabelValues(labels...).Inc()
	if observation.Duration > 0 {
		r.taskDuration.WithLabelValues(labels...).Observe(observation.Duration.Seconds())
	}
}

func HTTPMiddleware(recorder Recorder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if recorder == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
			started := time.Now()
			next.ServeHTTP(ww, r)
			statusCode := ww.Status()
			if statusCode == 0 {
				statusCode = http.StatusOK
			}
			recorder.ObserveHTTPRequest(HTTPObservation{
				Method:     r.Method,
				Route:      routePattern(r),
				StatusCode: statusCode,
				Duration:   time.Since(started),
			})
		})
	}
}

func (r *PrometheusRecorder) httpLabelValues(observation HTTPObservation) []string {
	statusCode := observation.StatusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	return []string{
		r.base.Service,
		r.base.Environment,
		r.base.Version,
		normalizeLabel(observation.Method),
		normalizeLabel(observation.Route),
		strconv.Itoa(statusCode),
		statusClass(statusCode),
	}
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

func normalizeBaseLabels(labels BaseLabels) BaseLabels {
	return BaseLabels{
		Service:     normalizeLabelOrDefault(labels.Service, "hub"),
		Environment: normalizeLabelOrDefault(labels.Environment, "unknown"),
		Version:     normalizeLabelOrDefault(labels.Version, "unknown"),
	}
}

func normalizeLabel(value string) string {
	return normalizeLabelOrDefault(value, "unknown")
}

func normalizeLabelOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
