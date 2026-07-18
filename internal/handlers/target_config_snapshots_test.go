package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/targetconfig"
	"github.com/distr-sh/distr/internal/types"
	"github.com/go-chi/chi/v5"
	"github.com/oaswrap/spec/adapter/chiopenapi"
	. "github.com/onsi/gomega"
)

func TestTargetConfigSnapshotMutationAccess(t *testing.T) {
	tests := []struct {
		name   string
		method string
		role   types.UserRole
		flags  []featureflags.Key
		want   int
		called bool
	}{
		{
			name: "admin create succeeds when enabled", method: http.MethodPost,
			role:  types.UserRoleAdmin,
			flags: []featureflags.Key{featureflags.KeyOperatorControlPlaneV2},
			want:  http.StatusNoContent, called: true,
		},
		{
			name: "read only create is forbidden", method: http.MethodPost,
			role:  types.UserRoleReadOnly,
			flags: []featureflags.Key{featureflags.KeyOperatorControlPlaneV2},
			want:  http.StatusForbidden,
		},
		{
			name: "disabled create is hidden", method: http.MethodPost,
			role: types.UserRoleAdmin, want: http.StatusNotFound,
		},
		{
			name: "reads remain available while disabled", method: http.MethodGet,
			role: types.UserRoleReadOnly, want: http.StatusNoContent, called: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			called := false
			handler := targetConfigMutationAccessMiddlewareWithFlags(tt.flags)(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					called = true
					w.WriteHeader(http.StatusNoContent)
				}),
			)
			request := httptest.NewRequest(tt.method, "/api/v1/target-config-snapshots", nil)
			userAuth := testChannelAuth()
			userAuth.role = tt.role
			request = request.WithContext(auth.Authentication.NewContext(request.Context(), userAuth))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			g.Expect(recorder.Code).To(Equal(tt.want))
			g.Expect(called).To(Equal(tt.called))
		})
	}
}

func TestTargetConfigSnapshotRoutesRejectUnknownFieldsAndExposeNoMutators(t *testing.T) {
	g := NewWithT(t)
	router := targetConfigSnapshotRoutedTestHandler(
		[]featureflags.Key{featureflags.KeyOperatorControlPlaneV2},
	)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/target-config-snapshots/",
		strings.NewReader(`{"unexpected":"secret-value"}`),
	)
	request = request.WithContext(auth.Authentication.NewContext(request.Context(), testChannelAuth()))
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusBadRequest))
	g.Expect(recorder.Body.String()).NotTo(ContainSubstring("secret-value"))

	for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete} {
		request = httptest.NewRequest(
			method,
			"/api/v1/target-config-snapshots/"+uuidForTargetConfigHandlerTest(),
			nil,
		)
		request = request.WithContext(auth.Authentication.NewContext(request.Context(), testChannelAuth()))
		recorder = httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		g.Expect(recorder.Code).To(SatisfyAny(
			Equal(http.StatusMethodNotAllowed),
			Equal(http.StatusNotFound),
		))
	}
}

func targetConfigSnapshotRoutedTestHandler(enabledFlags []featureflags.Key) http.Handler {
	baseRouter := chi.NewRouter()
	openAPIRouter := chiopenapi.NewRouter(baseRouter)
	openAPIRouter.Route("/api/v1/target-config-snapshots", func(r chiopenapi.Router) {
		targetConfigSnapshotsRouterWithDependencies(
			r,
			enabledFlags,
			func(context.Context) targetconfig.ObjectVerifier {
				return targetConfigVerifierStub{}
			},
		)
	})
	return baseRouter
}

type targetConfigVerifierStub struct{}

func (targetConfigVerifierStub) Verify(
	context.Context,
	types.TargetConfigSnapshotObject,
) (types.VerifiedTargetConfigObject, error) {
	return types.VerifiedTargetConfigObject{}, nil
}

func uuidForTargetConfigHandlerTest() string {
	return "11111111-1111-4111-8111-111111111111"
}
