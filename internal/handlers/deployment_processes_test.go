package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/middleware"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

func TestDeploymentProcessFromCreateUpdateRequest(t *testing.T) {
	g := NewWithT(t)
	orgID := uuid.New()
	applicationID := uuid.New()

	process := deploymentProcessFromCreateUpdateRequest(orgID, api.CreateUpdateDeploymentProcessRequest{
		ApplicationID: applicationID,
		Name:          " Standard deploy ",
		Description:   "description",
		SortOrder:     20,
	})

	g.Expect(process).To(Equal(types.DeploymentProcess{
		OrganizationID: orgID,
		ApplicationID:  applicationID,
		Name:           "Standard deploy",
		Description:    "description",
		SortOrder:      20,
	}))
}

func TestDeploymentProcessRevisionFromCreateRequest(t *testing.T) {
	g := NewWithT(t)
	orgID := uuid.New()
	processID := uuid.New()
	channelID := uuid.New()
	environmentID := uuid.New()

	revision := deploymentProcessRevisionFromCreateRequest(orgID, processID, api.CreateDeploymentProcessRevisionRequest{
		Description: " initial ",
		Steps: []api.DeploymentProcessStepRequest{
			{
				Key:                 " deploy ",
				Name:                " Deploy ",
				ActionType:          " distr.http.check ",
				ExecutionLocation:   " hub ",
				InputBindings:       map[string]any{"url": "https://example.com/health"},
				Condition:           " channel == stable ",
				ChannelIDs:          []uuid.UUID{channelID},
				EnvironmentIDs:      []uuid.UUID{environmentID},
				TargetTags:          []string{"linux"},
				FailureMode:         " fail ",
				TimeoutSeconds:      120,
				RetryPolicy:         api.DeploymentProcessStepRetryPolicyRequest{MaxAttempts: 3, IntervalSeconds: 10},
				RequiredPermissions: []string{"deploy:write"},
				SortOrder:           20,
				Dependencies:        []string{"prepare"},
			},
		},
	})

	g.Expect(revision.OrganizationID).To(Equal(orgID))
	g.Expect(revision.DeploymentProcessID).To(Equal(processID))
	g.Expect(revision.Description).To(Equal("initial"))
	g.Expect(revision.Steps).To(Equal([]types.DeploymentProcessStep{
		{
			Key:                  "deploy",
			Name:                 "Deploy",
			ActionType:           "distr.http.check",
			ExecutionLocation:    "hub",
			InputBindings:        map[string]any{"url": "https://example.com/health"},
			Condition:            "channel == stable",
			ChannelIDs:           []uuid.UUID{channelID},
			EnvironmentIDs:       []uuid.UUID{environmentID},
			TargetTags:           []string{"linux"},
			FailureMode:          "fail",
			TimeoutSeconds:       120,
			RetryMaxAttempts:     3,
			RetryIntervalSeconds: 10,
			RequiredPermissions:  []string{"deploy:write"},
			SortOrder:            20,
			Dependencies:         []string{"prepare"},
		},
	}))
}

func TestDeploymentProcessHandlersRejectInvalidPayloadsBeforeDatabaseAccess(t *testing.T) {
	processID := uuid.New()

	tests := []struct {
		name    string
		handler http.Handler
		method  string
		url     string
		body    string
		path    map[string]string
	}{
		{
			name:    "create process",
			handler: createDeploymentProcessHandler(),
			method:  http.MethodPost,
			url:     "/api/v1/deployment-processes",
			body:    `{"name":" "}`,
		},
		{
			name:    "create revision",
			handler: createDeploymentProcessRevisionHandler(),
			method:  http.MethodPost,
			url:     "/api/v1/deployment-processes/" + processID.String() + "/revisions",
			body: `{
				"steps":[{
					"key":"deploy",
					"name":"Deploy",
					"actionType":"distr.preflight",
					"executionLocation":"hub",
					"inputBindings":{},
					"dependencies":["missing"]
				}]
			}`,
			path: map[string]string{"deploymentProcessId": processID.String()},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(tt.method, tt.url, strings.NewReader(tt.body))
			for key, value := range tt.path {
				request.SetPathValue(key, value)
			}
			ctx := internalctx.WithLogger(request.Context(), zap.NewNop())
			request = request.WithContext(auth.Authentication.NewContext(ctx, testChannelAuth()))

			tt.handler.ServeHTTP(recorder, request)

			g.Expect(recorder.Code).To(Equal(http.StatusBadRequest))
		})
	}
}

func TestDeploymentProcessHandlersRejectMalformedUUIDPathValues(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		path    map[string]string
	}{
		{
			name:    "get process",
			handler: getDeploymentProcessHandler(),
			method:  http.MethodGet,
			path:    map[string]string{"deploymentProcessId": "not-a-uuid"},
		},
		{
			name:    "update process",
			handler: updateDeploymentProcessHandler(),
			method:  http.MethodPut,
			path:    map[string]string{"deploymentProcessId": "not-a-uuid"},
		},
		{
			name:    "delete process",
			handler: deleteDeploymentProcessHandler(),
			method:  http.MethodDelete,
			path:    map[string]string{"deploymentProcessId": "not-a-uuid"},
		},
		{
			name:    "list revisions",
			handler: getDeploymentProcessRevisionsHandler(),
			method:  http.MethodGet,
			path:    map[string]string{"deploymentProcessId": "not-a-uuid"},
		},
		{
			name:    "create revision",
			handler: createDeploymentProcessRevisionHandler(),
			method:  http.MethodPost,
			path:    map[string]string{"deploymentProcessId": "not-a-uuid"},
		},
		{
			name:    "get revision process id",
			handler: getDeploymentProcessRevisionHandler(),
			method:  http.MethodGet,
			path:    map[string]string{"deploymentProcessId": "not-a-uuid", "revisionId": uuid.NewString()},
		},
		{
			name:    "get revision id",
			handler: getDeploymentProcessRevisionHandler(),
			method:  http.MethodGet,
			path:    map[string]string{"deploymentProcessId": uuid.NewString(), "revisionId": "not-a-uuid"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(tt.method, "/api/v1/deployment-processes/not-a-uuid", nil)
			for key, value := range tt.path {
				request.SetPathValue(key, value)
			}
			ctx := internalctx.WithLogger(request.Context(), zap.NewNop())
			request = request.WithContext(auth.Authentication.NewContext(ctx, testChannelAuth()))

			tt.handler.ServeHTTP(recorder, request)

			g.Expect(recorder.Code).To(Equal(http.StatusNotFound))
		})
	}
}

func TestHandleDeploymentProcessWriteError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "duplicate name", err: apierrors.ErrAlreadyExists, want: http.StatusBadRequest},
		{name: "invalid payload", err: apierrors.ErrBadRequest, want: http.StatusBadRequest},
		{name: "missing scoped reference", err: apierrors.ErrNotFound, want: http.StatusNotFound},
		{name: "unsafe mutation", err: apierrors.ErrConflict, want: http.StatusConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/deployment-processes", nil)
			request = request.WithContext(internalctx.WithLogger(request.Context(), zap.NewNop()))

			handleDeploymentProcessWriteError(recorder, request, zap.NewNop(), "test", tt.err)

			g.Expect(recorder.Code).To(Equal(tt.want))
		})
	}
}

func TestDeploymentProcessesFeatureFlagMiddlewareRejectsDisabledAPI(t *testing.T) {
	g := NewWithT(t)
	called := false
	handler := middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyDeploymentProcesses)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/deployment-processes", nil)

	handler.ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusForbidden))
	g.Expect(called).To(BeFalse())
}
