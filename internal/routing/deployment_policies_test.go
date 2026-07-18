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

func TestDeploymentPolicyRoutesArePublishedInOpenAPI(t *testing.T) {
	g := NewWithT(t)
	const policyVersionsPath = "/api/v1/deployment-policies/{policyId}/versions"
	tracer := obsertracing.NoopTracer{}
	router := NewRouter(
		zap.NewNop(),
		nil,
		nil,
		nil,
		nil,
		nil,
		obsertracing.Tracers{Default: tracer, Agent: tracer},
		nil,
		nil,
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
	for path, methods := range map[string][]string{
		"/api/v1/deployment-policies": {
			http.MethodGet,
			http.MethodPost,
		},
		"/api/v1/deployment-policies/{policyId}/versions/{versionId}/publish": {
			http.MethodPost,
		},
		policyVersionsPath: {
			http.MethodGet,
		},
		"/api/v1/deployment-policies/bindings": {
			http.MethodGet,
			http.MethodPost,
		},
	} {
		g.Expect(document.Paths).To(HaveKey(path))
		for _, method := range methods {
			g.Expect(document.Paths[path]).To(HaveKey(strings.ToLower(method)))
		}
	}

	for path, responseSchema := range map[string]string{
		"/api/v1/deployment-policies":          "#/components/schemas/ApiDeploymentPolicyPage",
		policyVersionsPath:                     "#/components/schemas/ApiDeploymentPolicyVersionPage",
		"/api/v1/deployment-policies/bindings": "#/components/schemas/ApiDeploymentPolicyBindingPage",
	} {
		operation := readDeploymentRegistryOpenAPIOperation(
			t,
			document.Paths,
			path,
			strings.ToLower(http.MethodGet),
		)
		g.Expect(operation.Parameters).To(ContainElements(
			HaveField("Name", "cursor"),
			HaveField("Name", "limit"),
		))
		errorStatuses := []string{"400", "403"}
		if path == policyVersionsPath {
			errorStatuses = append(errorStatuses, "404")
		}
		expectDeploymentRegistryResponseStatuses(
			t,
			operation,
			append([]string{"200"}, errorStatuses...)...,
		)
		expectDeploymentRegistryJSONResponse(t, operation, responseSchema)
		expectDeploymentRegistryPlainTextResponses(t, operation, errorStatuses...)
	}
}
