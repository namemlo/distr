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

func TestRunbookFromCreateUpdateRequest(t *testing.T) {
	g := NewWithT(t)
	orgID := uuid.New()
	applicationID := uuid.New()

	runbook := runbookFromCreateUpdateRequest(orgID, api.CreateUpdateRunbookRequest{
		ApplicationID: applicationID,
		Name:          " Rotate keys ",
		Description:   "description",
		SortOrder:     20,
	})

	g.Expect(runbook).To(Equal(types.Runbook{
		OrganizationID: orgID,
		ApplicationID:  applicationID,
		Name:           "Rotate keys",
		Description:    "description",
		SortOrder:      20,
	}))
}

func TestRunbookRevisionFromCreateRequest(t *testing.T) {
	g := NewWithT(t)
	orgID := uuid.New()
	runbookID := uuid.New()

	revision := runbookRevisionFromCreateRequest(orgID, runbookID, api.CreateRunbookRevisionRequest{
		Description: " initial ",
		Steps: []api.RunbookStepRequest{
			{
				Key:                 " verify ",
				Name:                " Verify ",
				ActionType:          " distr.preflight ",
				ExecutionLocation:   " hub ",
				InputBindings:       map[string]any{},
				Condition:           " always() ",
				FailureMode:         " fail ",
				TimeoutSeconds:      120,
				RetryPolicy:         api.RunbookStepRetryPolicyRequest{MaxAttempts: 3, IntervalSeconds: 10},
				RequiredPermissions: []string{"runbook:execute"},
				SortOrder:           20,
				Dependencies:        []string{"prepare"},
			},
		},
	})

	g.Expect(revision.OrganizationID).To(Equal(orgID))
	g.Expect(revision.RunbookID).To(Equal(runbookID))
	g.Expect(revision.Description).To(Equal("initial"))
	g.Expect(revision.Steps).To(Equal([]types.RunbookStep{
		{
			Key:                  "verify",
			Name:                 "Verify",
			ActionType:           "distr.preflight",
			ExecutionLocation:    "hub",
			InputBindings:        map[string]any{},
			Condition:            "always()",
			FailureMode:          "fail",
			TimeoutSeconds:       120,
			RetryMaxAttempts:     3,
			RetryIntervalSeconds: 10,
			RequiredPermissions:  []string{"runbook:execute"},
			SortOrder:            20,
			Dependencies:         []string{"prepare"},
		},
	}))
}

func TestRunbookHandlersRejectInvalidPayloadsBeforeDatabaseAccess(t *testing.T) {
	runbookID := uuid.New()

	tests := []struct {
		name    string
		handler http.Handler
		method  string
		url     string
		body    string
		path    map[string]string
	}{
		{
			name:    "create runbook",
			handler: createRunbookHandler(),
			method:  http.MethodPost,
			url:     "/api/v1/runbooks",
			body:    `{"name":" "}`,
		},
		{
			name:    "create revision",
			handler: createRunbookRevisionHandler(),
			method:  http.MethodPost,
			url:     "/api/v1/runbooks/" + runbookID.String() + "/revisions",
			body: `{
				"steps":[{
					"key":"verify",
					"name":"Verify",
					"actionType":"distr.preflight",
					"executionLocation":"hub",
					"inputBindings":{},
					"dependencies":["missing"]
				}]
			}`,
			path: map[string]string{"runbookId": runbookID.String()},
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

func TestRunbookHandlersRejectMalformedUUIDPathValues(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		path    map[string]string
	}{
		{
			name:    "get runbook",
			handler: getRunbookHandler(),
			method:  http.MethodGet,
			path:    map[string]string{"runbookId": "not-a-uuid"},
		},
		{
			name:    "update runbook",
			handler: updateRunbookHandler(),
			method:  http.MethodPut,
			path:    map[string]string{"runbookId": "not-a-uuid"},
		},
		{
			name:    "delete runbook",
			handler: deleteRunbookHandler(),
			method:  http.MethodDelete,
			path:    map[string]string{"runbookId": "not-a-uuid"},
		},
		{
			name:    "list revisions",
			handler: getRunbookRevisionsHandler(),
			method:  http.MethodGet,
			path:    map[string]string{"runbookId": "not-a-uuid"},
		},
		{
			name:    "create revision",
			handler: createRunbookRevisionHandler(),
			method:  http.MethodPost,
			path:    map[string]string{"runbookId": "not-a-uuid"},
		},
		{
			name:    "get revision runbook id",
			handler: getRunbookRevisionHandler(),
			method:  http.MethodGet,
			path:    map[string]string{"runbookId": "not-a-uuid", "revisionId": uuid.NewString()},
		},
		{
			name:    "get revision id",
			handler: getRunbookRevisionHandler(),
			method:  http.MethodGet,
			path:    map[string]string{"runbookId": uuid.NewString(), "revisionId": "not-a-uuid"},
		},
		{
			name:    "publish revision id",
			handler: publishRunbookRevisionHandler(),
			method:  http.MethodPost,
			path:    map[string]string{"runbookId": uuid.NewString(), "revisionId": "not-a-uuid"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(tt.method, "/api/v1/runbooks/not-a-uuid", nil)
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

func TestHandleRunbookWriteError(t *testing.T) {
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
			request := httptest.NewRequest(http.MethodPost, "/api/v1/runbooks", nil)
			request = request.WithContext(internalctx.WithLogger(request.Context(), zap.NewNop()))

			handleRunbookWriteError(recorder, request, zap.NewNop(), "test", tt.err)

			g.Expect(recorder.Code).To(Equal(tt.want))
		})
	}
}

func TestRunbooksFeatureFlagMiddlewareRejectsDisabledAPI(t *testing.T) {
	g := NewWithT(t)
	called := false
	handler := middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyRunbooks)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/runbooks", nil)

	handler.ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusForbidden))
	g.Expect(called).To(BeFalse())
}
