package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/distr-sh/distr/internal/auth"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/middleware"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

func TestVariableSnapshotHandlersRejectMalformedUUIDPathValues(t *testing.T) {
	g := NewWithT(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/variable-snapshots/not-a-uuid", nil)
	request.SetPathValue("variableSnapshotId", "not-a-uuid")
	ctx := internalctx.WithLogger(request.Context(), zap.NewNop())
	request = request.WithContext(auth.Authentication.NewContext(ctx, testVariableSetAuth()))

	getVariableSnapshotHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusNotFound))
}

func TestVariableSnapshotsFeatureFlagMiddlewareRejectsDisabledAPI(t *testing.T) {
	g := NewWithT(t)
	called := false
	handler := variableSnapshotsFeatureFlagMiddleware(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/variable-snapshots/"+uuid.NewString(), nil)

	handler.ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusForbidden))
	g.Expect(called).To(BeFalse())
}

func TestDeploymentConfigurationDriftFeatureFlagMiddlewareRejectsDisabledAPI(t *testing.T) {
	g := NewWithT(t)
	called := false
	handler := middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyScopedVariablesV2)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/deployments/"+uuid.NewString()+"/configuration-drift", nil)

	handler.ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusForbidden))
	g.Expect(called).To(BeFalse())
}

func TestConfigurationDriftHandlerRejectsMalformedUUIDPathValue(t *testing.T) {
	g := NewWithT(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/deployments/not-a-uuid/configuration-drift", nil)
	request.SetPathValue("deploymentId", "not-a-uuid")
	ctx := internalctx.WithLogger(request.Context(), zap.NewNop())
	ctx = auth.Authentication.NewContext(ctx, testVariableSetAuth())
	request = request.WithContext(ctx)

	getDeploymentConfigurationDriftHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusNotFound))
}
