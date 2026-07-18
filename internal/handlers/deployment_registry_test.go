package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/authn/authinfo"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/types"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/oaswrap/spec/adapter/chiopenapi"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

func TestDeploymentRegistryMutationAccessMiddleware(t *testing.T) {
	tests := []struct {
		name   string
		method string
		role   types.UserRole
		flags  []featureflags.Key
		want   int
		called bool
	}{
		{
			name:   "admin mutation succeeds when enabled",
			method: http.MethodPost,
			role:   types.UserRoleAdmin,
			flags:  []featureflags.Key{featureflags.KeyOperatorControlPlaneV2},
			want:   http.StatusNoContent,
			called: true,
		},
		{
			name:   "read only mutation is unauthorized",
			method: http.MethodPost,
			role:   types.UserRoleReadOnly,
			flags:  []featureflags.Key{featureflags.KeyOperatorControlPlaneV2},
			want:   http.StatusForbidden,
		},
		{
			name:   "disabled mutation is hidden",
			method: http.MethodDelete,
			role:   types.UserRoleAdmin,
			want:   http.StatusNotFound,
		},
		{
			name:   "reads remain available while disabled",
			method: http.MethodGet,
			role:   types.UserRoleReadOnly,
			want:   http.StatusNoContent,
			called: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			called := false
			handler := deploymentRegistryMutationAccessMiddlewareWithFlags(tt.flags)(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					called = true
					w.WriteHeader(http.StatusNoContent)
				}),
			)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(tt.method, "/api/v1/deployment-registry/scopes", nil)
			userAuth := testChannelAuth()
			userAuth.role = tt.role
			request = request.WithContext(auth.Authentication.NewContext(request.Context(), userAuth))

			handler.ServeHTTP(recorder, request)

			g.Expect(recorder.Code).To(Equal(tt.want))
			g.Expect(called).To(Equal(tt.called))
		})
	}
}

func TestRegistryImportJSONBodyRejectsUnknownFieldsAndTrailingValues(t *testing.T) {
	g := NewWithT(t)
	for _, body := range []string{
		`{"previewChecksum":"sha256:` + strings.Repeat("a", 64) + `","unknown":true}`,
		`{"previewChecksum":"sha256:` + strings.Repeat("a", 64) + `"} {}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		recorder := httptest.NewRecorder()
		_, err := deploymentRegistryImportJSONBody[api.RegistryImportApplyRequest](recorder, request)
		g.Expect(err).To(HaveOccurred())
		g.Expect(recorder.Code).To(Equal(http.StatusBadRequest))
	}
}

func TestRegistryImportJSONBodyPreservesPlacementMetadataAndRejectsSourcePath(t *testing.T) {
	g := NewWithT(t)
	targetID, environmentID := uuid.New(), uuid.New()
	checksum := strings.Repeat("a", 64)
	validBody := `{
		"sourceKind":"compose",
		"toolName":"scanner",
		"toolVersion":"1.0",
		"parameters":{},
		"evidenceReference":"evidence://sha256/` + checksum + `",
		"evidenceChecksum":"` + checksum + `",
		"sourcePlacements":[{
			"rootKey":"choice-tp-dev",
			"physicalName":"choice-api"
		}],
		"roots":[{
			"key":"choice-tp-dev",
			"name":"Choice TP DEV",
			"deliveryModel":"external",
			"classification":"external",
			"deploymentTargetId":"` + targetID.String() + `",
			"environmentId":"` + environmentID.String() + `",
			"physicalIdentity":"compose:choice-tp-dev",
			"placements":[{
				"componentKey":"api",
				"physicalName":"choice-api",
				"configNamespace":"choice-config",
				"databaseBoundary":"choice-db",
				"healthAdapter":"choice-health",
				"renamedFrom":"choice-api-old"
			}]
		}]
	}`
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(validBody))

	decoded, err := deploymentRegistryImportJSONBody[api.RegistryImportPreviewRequest](
		recorder, request,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(decoded.Roots[0].Placements).To(ConsistOf(api.RegistryImportCandidatePlacement{
		ComponentKey: "api", PhysicalName: "choice-api",
		ConfigNamespace: "choice-config", DatabaseBoundary: "choice-db",
		HealthAdapter: "choice-health", RenamedFrom: "choice-api-old",
	}))

	withSourcePath := strings.Replace(
		validBody,
		`"physicalIdentity":"compose:choice-tp-dev",`,
		`"physicalIdentity":"compose:choice-tp-dev","sourcePath":"C:\\private\\compose.yaml",`,
		1,
	)
	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(withSourcePath))
	_, err = deploymentRegistryImportJSONBody[api.RegistryImportPreviewRequest](
		recorder, request,
	)
	g.Expect(err).To(HaveOccurred())
	g.Expect(recorder.Code).To(Equal(http.StatusBadRequest))
}

func TestDeploymentRegistryRoutedAuthorizationAndFeatureFlag(t *testing.T) {
	const scopesPath = "/api/v1/deployment-registry/scopes/"
	tests := []struct {
		name          string
		flags         []featureflags.Key
		authenticated bool
		role          types.UserRole
		superAdmin    bool
		method        string
		path          string
		body          string
		want          int
	}{
		{
			name:          "admin mutation reaches validation when enabled",
			flags:         []featureflags.Key{featureflags.KeyOperatorControlPlaneV2},
			authenticated: true,
			role:          types.UserRoleAdmin,
			method:        http.MethodPost,
			path:          scopesPath,
			body:          `{}`,
			want:          http.StatusBadRequest,
		},
		{
			name:          "read write mutation reaches validation when enabled",
			flags:         []featureflags.Key{featureflags.KeyOperatorControlPlaneV2},
			authenticated: true,
			role:          types.UserRoleReadWrite,
			method:        http.MethodPost,
			path:          scopesPath,
			body:          `{}`,
			want:          http.StatusBadRequest,
		},
		{
			name:          "disabled mutation is hidden",
			authenticated: true,
			role:          types.UserRoleAdmin,
			method:        http.MethodPost,
			path:          scopesPath,
			body:          `{}`,
			want:          http.StatusNotFound,
		},
		{
			name:          "read only mutation is forbidden",
			flags:         []featureflags.Key{featureflags.KeyOperatorControlPlaneV2},
			authenticated: true,
			role:          types.UserRoleReadOnly,
			method:        http.MethodPost,
			path:          scopesPath,
			body:          `{}`,
			want:          http.StatusForbidden,
		},
		{
			name:          "super admin mutation is forbidden",
			flags:         []featureflags.Key{featureflags.KeyOperatorControlPlaneV2},
			authenticated: true,
			role:          types.UserRoleAdmin,
			superAdmin:    true,
			method:        http.MethodPost,
			path:          scopesPath,
			body:          `{}`,
			want:          http.StatusForbidden,
		},
		{
			name:   "unauthenticated mutation is forbidden",
			flags:  []featureflags.Key{featureflags.KeyOperatorControlPlaneV2},
			method: http.MethodPost,
			path:   scopesPath,
			body:   `{}`,
			want:   http.StatusForbidden,
		},
		{
			name:   "unauthenticated read is forbidden",
			method: http.MethodGet,
			path:   scopesPath,
			want:   http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			router := deploymentRegistryRoutedTestHandler(tt.flags)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(
				tt.method,
				tt.path,
				strings.NewReader(tt.body),
			)
			if tt.authenticated {
				channelAuth := testChannelAuth()
				channelAuth.role = tt.role
				var userAuth authinfo.AuthInfoWithUserAndOrganization = channelAuth
				if tt.superAdmin {
					userAuth = deploymentRegistrySuperAdminAuth{channelTestAuth: channelAuth}
				}
				request = request.WithContext(
					auth.Authentication.NewContext(request.Context(), userAuth),
				)
			}

			router.ServeHTTP(recorder, request)

			g.Expect(recorder.Code).To(Equal(tt.want), recorder.Body.String())
		})
	}
}

func TestDeploymentRegistryListRequestFromHTTPDistinguishesOmittedLimit(t *testing.T) {
	t.Run("omitted limit preserves internal default", func(t *testing.T) {
		g := NewWithT(t)
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(
			http.MethodGet,
			"/api/v1/deployment-registry/scopes?cursor=eyJ2IjoxfQ",
			nil,
		)

		listRequest, ok := deploymentRegistryListRequestFromHTTP(recorder, request)

		g.Expect(ok).To(BeTrue())
		g.Expect(listRequest.Limit).To(BeZero())
		g.Expect(recorder.Body.String()).To(BeEmpty())
	})

	t.Run("explicit zero limit is rejected", func(t *testing.T) {
		g := NewWithT(t)
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(
			http.MethodGet,
			"/api/v1/deployment-registry/scopes?limit=0",
			nil,
		)

		_, ok := deploymentRegistryListRequestFromHTTP(recorder, request)

		g.Expect(ok).To(BeFalse())
		g.Expect(recorder.Code).To(Equal(http.StatusBadRequest))
		g.Expect(recorder.Body.String()).To(Equal(
			"limit must be between 1 and 100 when provided\n",
		))
	})
}

func deploymentRegistryRoutedTestHandler(enabledFlags []featureflags.Key) http.Handler {
	baseRouter := chi.NewRouter()
	openAPIRouter := chiopenapi.NewRouter(baseRouter)
	openAPIRouter.Route("/api/v1/deployment-registry", func(r chiopenapi.Router) {
		deploymentRegistryRouterWithFlags(r, enabledFlags)
	})
	return baseRouter
}

type deploymentRegistrySuperAdminAuth struct {
	channelTestAuth
}

func (deploymentRegistrySuperAdminAuth) IsSuperAdmin() bool {
	return true
}

func TestCreateDeploymentScopeHandlerRejectsInvalidPayloadBeforeDatabaseAccess(t *testing.T) {
	g := NewWithT(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/deployment-registry/scopes",
		strings.NewReader(`{"key":"Invalid Key","name":"Scope","deliveryModel":"shared","managementState":"managed"}`),
	)
	ctx := internalctx.WithLogger(request.Context(), zap.NewNop())
	request = request.WithContext(auth.Authentication.NewContext(ctx, testChannelAuth()))

	createDeploymentScopeHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusBadRequest))
}

func TestDeploymentUnitSubscribersFromCreateRequestPreservesAtomicMembership(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	customerIDs := []uuid.UUID{uuid.New(), uuid.New()}

	subscribers := deploymentUnitSubscribersFromCreateRequest(
		organizationID,
		api.CreateDeploymentUnitRequest{
			SubscriberCustomerOrganizationIDs: customerIDs,
		},
	)

	g.Expect(subscribers).To(HaveLen(2))
	for index, subscriber := range subscribers {
		g.Expect(subscriber.OrganizationID).To(Equal(organizationID))
		g.Expect(subscriber.DeploymentUnitID).To(Equal(uuid.Nil))
		g.Expect(subscriber.CustomerOrganizationID).To(Equal(customerIDs[index]))
	}
}

func TestDeploymentRegistryHandlersRejectMalformedUUIDPathValues(t *testing.T) {
	tests := []struct {
		name    string
		pathKey string
		handler http.Handler
	}{
		{name: "scope", pathKey: "scopeId", handler: getDeploymentScopeHandler()},
		{name: "assignment", pathKey: "assignmentId", handler: getTargetEnvironmentAssignmentHandler()},
		{name: "unit", pathKey: "unitId", handler: getDeploymentUnitHandler()},
		{name: "subscriber", pathKey: "subscriberId", handler: getDeploymentUnitSubscriberHandler()},
		{name: "definition", pathKey: "definitionId", handler: getComponentDefinitionHandler()},
		{name: "alias", pathKey: "aliasId", handler: getComponentAliasHandler()},
		{name: "instance", pathKey: "instanceId", handler: getComponentInstanceHandler()},
		{name: "placement", pathKey: "unitId", handler: getDeploymentRegistryPlacementHandler()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/not-a-uuid", nil)
			request.SetPathValue(tt.pathKey, "not-a-uuid")
			ctx := internalctx.WithLogger(request.Context(), zap.NewNop())
			request = request.WithContext(auth.Authentication.NewContext(ctx, testChannelAuth()))

			tt.handler.ServeHTTP(recorder, request)

			g.Expect(recorder.Code).To(Equal(http.StatusNotFound))
		})
	}
}

func TestHandleDeploymentRegistryWriteErrorUsesStableNonLeakingResponses(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
		body string
	}{
		{
			name: "foreign identity",
			err:  apierrors.ErrNotFound,
			want: http.StatusNotFound,
			body: "404 page not found\n",
		},
		{
			name: "duplicate identity",
			err:  apierrors.ErrAlreadyExists,
			want: http.StatusConflict,
			body: "deployment scope already exists\n",
		},
		{
			name: "protected delete",
			err:  apierrors.ErrConflict,
			want: http.StatusConflict,
			body: "deployment scope is in use\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodDelete, "/deployment-scope/"+uuid.NewString(), nil)
			request = request.WithContext(internalctx.WithLogger(request.Context(), zap.NewNop()))

			handleDeploymentRegistryWriteError(
				recorder,
				request,
				zap.NewNop(),
				"delete",
				"deployment scope",
				tt.err,
			)

			g.Expect(recorder.Code).To(Equal(tt.want))
			g.Expect(recorder.Body.String()).To(Equal(tt.body))
			g.Expect(strings.ToLower(recorder.Body.String())).NotTo(Or(
				ContainSubstring("sqlstate"),
				ContainSubstring("constraint"),
				ContainSubstring("foreign key"),
				ContainSubstring("pgconn"),
			))
		})
	}
}
