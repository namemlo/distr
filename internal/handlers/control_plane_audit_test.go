package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/auth"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

func TestControlPlaneAuditHandlersRejectInvalidInputBeforeDatabaseAccess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler http.Handler
		method  string
		target  string
		body    string
	}{
		{
			name:    "event cursor",
			handler: getControlPlaneAuditEventsHandler(),
			method:  http.MethodGet,
			target:  "/api/v1/control-plane-audit/events?afterSequence=-1",
		},
		{
			name:    "evidence bundle",
			handler: createControlPlaneEvidenceBundleHandler(),
			method:  http.MethodPost,
			target:  "/api/v1/control-plane-audit/evidence-bundles",
			body:    `{}`,
		},
		{
			name:    "export sink",
			handler: createAuditExportSinkHandler(),
			method:  http.MethodPost,
			target:  "/api/v1/control-plane-audit/export-sinks",
			body:    `{"name":"","kind":"webhook"}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(test.method, test.target, strings.NewReader(test.body))
			ctx := internalctx.WithLogger(request.Context(), zap.NewNop())
			request = request.WithContext(auth.Authentication.NewContext(ctx, testReleaseBundleAuth()))

			test.handler.ServeHTTP(recorder, request)

			g.Expect(recorder.Code).To(Equal(http.StatusBadRequest))
		})
	}
}

func TestAuditExportSinkInputUsesAuthenticatedOrganizationAndSafeDefaults(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	organizationID := uuid.New()
	input := auditExportSinkInput(organizationID, api.CreateAuditExportSinkRequest{
		Name:              "  Security archive  ",
		Kind:              types.AuditExportSinkKindSIEM,
		EndpointReference: "  secret://audit/siem  ",
		ConfigChecksum:    "sha256:" + strings.Repeat("a", 64),
	})

	g.Expect(input.OrganizationID).To(Equal(organizationID))
	g.Expect(input.Name).To(Equal("Security archive"))
	g.Expect(input.EndpointReference).To(Equal("secret://audit/siem"))
	g.Expect(input.Enabled).To(BeTrue())
}

func TestControlPlaneAuditFeatureFlagMiddlewareRejectsDisabledAPI(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	called := false
	handler := controlPlaneAuditFeatureFlagMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/control-plane-audit/events", nil)

	handler.ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusForbidden))
	g.Expect(called).To(BeFalse())
}
