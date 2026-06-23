package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/featureflags"
	. "github.com/onsi/gomega"
)

func TestValidateConfigAsCodeHandlerReturnsValidationResult(t *testing.T) {
	g := NewWithT(t)
	recorder := httptest.NewRecorder()
	requestBody := api.ConfigAsCodeValidateRequest{
		Documents: []api.ConfigAsCodeValidateDocumentRequest{
			{
				Content: `
apiVersion: distr.sh/v1alpha1
kind: Channel
metadata:
  name: stable
  path: channels/stable.yaml
spec:
  description: Stable channel
`,
			},
		},
	}
	body, err := json.Marshal(requestBody)
	g.Expect(err).NotTo(HaveOccurred())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/config-as-code/validate", bytes.NewReader(body))

	validateConfigAsCodeHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusOK))
	var response api.ConfigAsCodeValidateResponse
	g.Expect(json.Unmarshal(recorder.Body.Bytes(), &response)).To(Succeed())
	g.Expect(response.Valid).To(BeTrue())
	g.Expect(response.Documents).To(HaveLen(1))
	g.Expect(response.Documents[0].Kind).To(Equal("Channel"))
	g.Expect(response.Documents[0].APIVersion).To(Equal("distr.sh/v1alpha1"))
	g.Expect(response.Documents[0].CanonicalChecksum).To(MatchRegexp(`^[0-9a-f]{64}$`))
	g.Expect(response.Errors).To(BeEmpty())
}

func TestValidateConfigAsCodeHandlerReturnsRedactedValidationErrors(t *testing.T) {
	g := NewWithT(t)
	recorder := httptest.NewRecorder()
	requestBody := api.ConfigAsCodeValidateRequest{
		Documents: []api.ConfigAsCodeValidateDocumentRequest{
			{
				Content: `
apiVersion: distr.sh/v1alpha1
kind: VariableSetDefinition
metadata:
  name: prod-vars
  path: variable-sets/prod.yaml
spec:
  variables:
    - name: DATABASE_PASSWORD
      type: string
      default: plaintext-fixture-value
`,
			},
		},
	}
	body, err := json.Marshal(requestBody)
	g.Expect(err).NotTo(HaveOccurred())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/config-as-code/validate", bytes.NewReader(body))

	validateConfigAsCodeHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusOK))
	var response api.ConfigAsCodeValidateResponse
	g.Expect(json.Unmarshal(recorder.Body.Bytes(), &response)).To(Succeed())
	g.Expect(response.Valid).To(BeFalse())
	g.Expect(response.Errors).To(HaveLen(1))
	g.Expect(response.Errors[0].DocumentIndex).To(Equal(0))
	g.Expect(response.Errors[0].Path).To(Equal("$[0].spec.variables[0].default"))
	g.Expect(response.Errors[0].Message).To(ContainSubstring("plaintext secret values are not allowed"))
	g.Expect(response.Errors[0].Message).NotTo(ContainSubstring("plaintext-fixture-value"))
}

func TestConfigAsCodeFeatureFlagMiddlewareReturnsNotFoundWhenDisabled(t *testing.T) {
	g := NewWithT(t)
	called := false
	handler := configAsCodeFeatureFlagMiddlewareWithFlags(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/config-as-code/validate", nil)

	handler.ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusNotFound))
	g.Expect(called).To(BeFalse())
}

func TestConfigAsCodeFeatureFlagMiddlewareAllowsEnabledRequests(t *testing.T) {
	g := NewWithT(t)
	called := false
	handler := configAsCodeFeatureFlagMiddlewareWithFlags([]featureflags.Key{featureflags.KeyConfigAsCode})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusAccepted)
	}))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/config-as-code/validate", nil)

	handler.ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusAccepted))
	g.Expect(called).To(BeTrue())
}
