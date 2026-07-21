package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/types"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/oaswrap/spec/adapter/chiopenapi"
	. "github.com/onsi/gomega"
)

func TestLeaseExecutionV2HandlerUsesCredentialScopeAndFrozenIdentity(t *testing.T) {
	g := NewWithT(t)
	credential := executionV2LeaseTestAuth{orgID: uuid.New(), targetID: uuid.New()}
	var captured types.LeaseExecutionV2Request
	attemptID := uuid.New()
	handler := leaseExecutionV2HandlerWith(func(
		_ context.Context,
		request types.LeaseExecutionV2Request,
	) (*types.ExecutionV2Lease, error) {
		captured = request
		return &types.ExecutionV2Lease{
			Attempt: types.ExecutionAttempt{ID: attemptID},
			Intent:  types.SignedExecutionIntent{KeyID: request.KeyID},
		}, nil
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/executor/v2/executions/lease",
		strings.NewReader(`{
			"executorId":"executor-a",
			"adapterRevision":"adapter.compose@2",
			"keyId":"sha256:`+repeatHandlerHex("ab")+`",
			"leaseSeconds":60
		}`),
	)
	request = request.WithContext(auth.AgentAuthentication.NewContext(request.Context(), credential))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusOK))
	g.Expect(captured.OrganizationID).To(Equal(credential.orgID))
	g.Expect(captured.DeploymentTargetID).To(Equal(credential.targetID))
	g.Expect(captured.ExecutorID).To(Equal("executor-a"))
	g.Expect(captured.AdapterRevision).To(Equal("adapter.compose@2"))
	g.Expect(captured.KeyID).To(Equal("sha256:" + repeatHandlerHex("ab")))
	var response api.ExecutionV2LeaseResponse
	g.Expect(json.Unmarshal(recorder.Body.Bytes(), &response)).To(Succeed())
	g.Expect(response.Attempt.ID).To(Equal(attemptID))
	g.Expect(response.Intent.KeyID).To(Equal(captured.KeyID))
}

func TestLeaseExecutionV2HandlerReturnsNoContentWhenNoAttemptMatches(t *testing.T) {
	g := NewWithT(t)
	credential := executionV2LeaseTestAuth{orgID: uuid.New(), targetID: uuid.New()}
	handler := leaseExecutionV2HandlerWith(func(
		context.Context,
		types.LeaseExecutionV2Request,
	) (*types.ExecutionV2Lease, error) {
		return nil, nil
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/executor/v2/executions/lease",
		strings.NewReader(`{
			"executorId":"executor-a",
			"adapterRevision":"adapter.compose@2",
			"keyId":"sha256:`+repeatHandlerHex("ab")+`",
			"leaseSeconds":60
		}`),
	)
	request = request.WithContext(auth.AgentAuthentication.NewContext(request.Context(), credential))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusNoContent))
	g.Expect(recorder.Body.Len()).To(Equal(0))
}

func TestExecutionV2ExecutorRouterPublishesAtomicLeaseContract(t *testing.T) {
	g := NewWithT(t)
	base := chi.NewRouter()
	openAPI := chiopenapi.NewRouter(base)
	openAPI.Route("/api/executor/v2", ExecutionV2ExecutorRouter)
	document, err := openAPI.MarshalJSON()
	g.Expect(err).NotTo(HaveOccurred())
	var schema struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	g.Expect(json.Unmarshal(document, &schema)).To(Succeed())
	g.Expect(schema.Paths).To(HaveKey("/api/executor/v2/executions/lease"))
	g.Expect(schema.Paths["/api/executor/v2/executions/lease"]).To(HaveKey("post"))
}

type executionV2LeaseTestAuth struct {
	orgID    uuid.UUID
	targetID uuid.UUID
}

func (a executionV2LeaseTestAuth) CurrentDeploymentTargetID() uuid.UUID { return a.targetID }
func (a executionV2LeaseTestAuth) CurrentOrgID() uuid.UUID              { return a.orgID }
func (a executionV2LeaseTestAuth) Token() any                           { return nil }

func repeatHandlerHex(pair string) string {
	result := ""
	for range 32 {
		result += pair
	}
	return result
}
