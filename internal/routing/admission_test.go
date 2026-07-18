package routing

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	obsertracing "github.com/distr-sh/distr/internal/observability/tracing"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

func TestAdmissionRoutesArePublishedInOpenAPI(t *testing.T) {
	g := NewWithT(t)
	tracer := obsertracing.NoopTracer{}
	router := NewRouter(
		zap.NewNop(),
		nil,
		nil,
		nil,
		nil,
		nil,
		obsertracing.Tracers{Default: tracer, Agent: tracer},
	)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "/docs/openapi.json", nil),
	)

	g.Expect(recorder.Code).To(Equal(http.StatusOK))
	var document struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	g.Expect(json.Unmarshal(recorder.Body.Bytes(), &document)).To(Succeed())
	for _, path := range []string{
		"/api/v1/deployment-plans/{deploymentPlanId}/admission",
		"/api/v1/deployment-plans/{deploymentPlanId}/emergency-overrides",
	} {
		g.Expect(document.Paths).To(HaveKey(path))
		g.Expect(document.Paths[path]).To(HaveKey(strings.ToLower(http.MethodPost)))
	}
}
