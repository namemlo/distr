package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

type campaignControlServiceStub struct {
	mutationCalls int
}

func (stub *campaignControlServiceStub) ApplyCampaignControl(
	context.Context,
	types.CampaignControlInput,
) (*types.CampaignControlResult, error) {
	stub.mutationCalls++
	return &types.CampaignControlResult{}, nil
}

func (stub *campaignControlServiceStub) ExcludeCampaignMember(
	context.Context,
	types.CampaignMemberControlInput,
) (*types.CampaignExclusion, error) {
	stub.mutationCalls++
	return &types.CampaignExclusion{}, nil
}

func (stub *campaignControlServiceStub) RetryCampaignMember(
	context.Context,
	types.CampaignMemberControlInput,
) (*types.DeploymentPlan, error) {
	stub.mutationCalls++
	return &types.DeploymentPlan{}, nil
}

func TestCampaignControlMutationsAuthorizeRunBeforeService(t *testing.T) {
	userAuth := testChannelAuth()
	organizationID := *userAuth.CurrentOrgID()
	runID := uuid.New()
	requestID := uuid.New()
	memberRunID := uuid.New()

	for name, testCase := range map[string]struct {
		body       string
		newHandler func(CampaignControlService, campaignRuntimeAuthorizer) http.Handler
	}{
		"pause": {
			body: `{"requestId":"` + requestID.String() + `","expectedVersion":1,"reason":"operator request"}`,
			newHandler: func(service CampaignControlService, authorizer campaignRuntimeAuthorizer) http.Handler {
				return campaignControlHandler(service, authorizer, types.CampaignControlKindPause)
			},
		},
		"retry": {
			body: `{"requestId":"` + requestID.String() + `","expectedVersion":1,"reason":"operator request","memberRunId":"` + memberRunID.String() + `","protocolVersion":"v2"}`,
			newHandler: func(service CampaignControlService, authorizer campaignRuntimeAuthorizer) http.Handler {
				return campaignMemberControlHandler(service, authorizer, true)
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)
			service := &campaignControlServiceStub{}
			authorizer := &campaignRuntimeAuthorizerStub{err: apierrors.ErrForbidden}
			handler := testCase.newHandler(service, authorizer)
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(testCase.body))
			request.SetPathValue("id", runID.String())
			request = request.WithContext(auth.Authentication.NewContext(request.Context(), userAuth))
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			g.Expect(response.Code).To(Equal(http.StatusForbidden))
			g.Expect(service.mutationCalls).To(Equal(0))
			g.Expect(authorizer.organizationID).To(Equal(organizationID))
			g.Expect(authorizer.runID).To(Equal(runID))
		})
	}
}
