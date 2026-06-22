package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestPrometheusRecorderRecordsCoreMetricsWithBaseLabels(t *testing.T) {
	g := NewWithT(t)
	registry := prometheus.NewRegistry()
	recorder := NewPrometheusRecorder(BaseLabels{
		Service:     "hub",
		Environment: "test",
		Version:     "v1",
	})
	g.Expect(registry.Register(recorder)).To(Succeed())

	recorder.ObserveHTTPRequest(HTTPObservation{
		Method:     http.MethodGet,
		Route:      "/api/v1/tasks",
		StatusCode: http.StatusInternalServerError,
		Duration:   150 * time.Millisecond,
	})
	recorder.ObserveTask(TaskObservation{
		Status:   "succeeded",
		Duration: 2 * time.Second,
	})

	families, err := registry.Gather()
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(counterValue(g, families, "distr_http_requests_total", map[string]string{
		"service":      "hub",
		"environment":  "test",
		"version":      "v1",
		"method":       "GET",
		"route":        "/api/v1/tasks",
		"status_code":  "500",
		"status_class": "5xx",
	})).To(Equal(float64(1)))
	g.Expect(counterValue(g, families, "distr_http_errors_total", map[string]string{
		"service":      "hub",
		"environment":  "test",
		"version":      "v1",
		"method":       "GET",
		"route":        "/api/v1/tasks",
		"status_code":  "500",
		"status_class": "5xx",
	})).To(Equal(float64(1)))
	g.Expect(histogramCount(g, families, "distr_http_request_duration_seconds", map[string]string{
		"service":      "hub",
		"environment":  "test",
		"version":      "v1",
		"method":       "GET",
		"route":        "/api/v1/tasks",
		"status_code":  "500",
		"status_class": "5xx",
	})).To(Equal(uint64(1)))
	g.Expect(counterValue(g, families, "distr_task_executions_total", map[string]string{
		"service":     "hub",
		"environment": "test",
		"version":     "v1",
		"status":      "succeeded",
	})).To(Equal(float64(1)))
	g.Expect(histogramCount(g, families, "distr_task_duration_seconds", map[string]string{
		"service":     "hub",
		"environment": "test",
		"version":     "v1",
		"status":      "succeeded",
	})).To(Equal(uint64(1)))
}

func TestHTTPMiddlewareRecordsRouteStatusAndLatency(t *testing.T) {
	g := NewWithT(t)
	recorder := &recordingRecorder{}
	router := chi.NewRouter()
	router.Use(HTTPMiddleware(recorder))
	router.Get("/api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Millisecond)
		w.WriteHeader(http.StatusCreated)
	})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil))

	g.Expect(recorder.http).To(HaveLen(1))
	g.Expect(recorder.http[0].Method).To(Equal(http.MethodGet))
	g.Expect(recorder.http[0].Route).To(Equal("/api/v1/tasks"))
	g.Expect(recorder.http[0].StatusCode).To(Equal(http.StatusCreated))
	g.Expect(recorder.http[0].Duration).To(BeNumerically(">", 0))
}

type recordingRecorder struct {
	http  []HTTPObservation
	tasks []TaskObservation
}

func (r *recordingRecorder) ObserveHTTPRequest(observation HTTPObservation) {
	r.http = append(r.http, observation)
}

func (r *recordingRecorder) ObserveTask(observation TaskObservation) {
	r.tasks = append(r.tasks, observation)
}

func counterValue(g Gomega, families []*dto.MetricFamily, name string, labels map[string]string) float64 {
	metric := findMetric(g, families, name, labels)
	g.Expect(metric.GetCounter()).NotTo(BeNil(), "metric %s is not a counter", name)
	return metric.GetCounter().GetValue()
}

func histogramCount(g Gomega, families []*dto.MetricFamily, name string, labels map[string]string) uint64 {
	metric := findMetric(g, families, name, labels)
	g.Expect(metric.GetHistogram()).NotTo(BeNil(), "metric %s is not a histogram", name)
	return metric.GetHistogram().GetSampleCount()
}

func findMetric(g Gomega, families []*dto.MetricFamily, name string, labels map[string]string) *dto.Metric {
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			if hasLabels(metric, labels) {
				return metric
			}
		}
	}
	g.Expect(nil).NotTo(BeNil(), "metric %s with labels %#v not found", name, labels)
	return &dto.Metric{}
}

func hasLabels(metric *dto.Metric, labels map[string]string) bool {
	actual := map[string]string{}
	for _, label := range metric.GetLabel() {
		actual[label.GetName()] = label.GetValue()
	}
	for key, value := range labels {
		if actual[key] != value {
			return false
		}
	}
	return true
}
