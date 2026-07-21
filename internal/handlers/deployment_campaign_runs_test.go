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
	"github.com/onsi/gomega"
)

type campaignRuntimeAuthorizerStub struct {
	revisionID     uuid.UUID
	runID          uuid.UUID
	organizationID uuid.UUID
	err            error
}

func (stub *campaignRuntimeAuthorizerStub) AuthorizeCampaignRevision(
	_ context.Context,
	organizationID uuid.UUID,
	revisionID uuid.UUID,
) error {
	stub.organizationID = organizationID
	stub.revisionID = revisionID
	return stub.err
}

func (stub *campaignRuntimeAuthorizerStub) AuthorizeCampaignRun(
	_ context.Context,
	organizationID uuid.UUID,
	runID uuid.UUID,
) error {
	stub.organizationID = organizationID
	stub.runID = runID
	return stub.err
}

type campaignRunServiceStub struct {
	mutationCalls int
}

func (stub *campaignRunServiceStub) StartCampaignRun(
	context.Context,
	types.CampaignRunStartInput,
) (*types.CampaignRun, error) {
	stub.mutationCalls++
	return &types.CampaignRun{}, nil
}

func (stub *campaignRunServiceStub) GetCampaignRun(
	context.Context,
	uuid.UUID,
	uuid.UUID,
) (*types.CampaignRun, error) {
	return &types.CampaignRun{}, nil
}

func (stub *campaignRunServiceStub) TransitionCampaignRun(
	context.Context,
	types.CampaignTransition,
) (*types.CampaignRun, error) {
	stub.mutationCalls++
	return &types.CampaignRun{}, nil
}

func TestCampaignRunHandlerReturnsMappedRun(t *testing.T) {
	g := gomega.NewWithT(t)
	runID := uuid.New()
	handler := GetDeploymentCampaignRunHandler(func(
		_ *http.Request,
		id uuid.UUID,
	) (*types.CampaignRun, error) {
		g.Expect(id).To(gomega.Equal(runID))
		return &types.CampaignRun{ID: id, State: types.CampaignRunStateRunning, Version: 2}, nil
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/deployment-campaign-runs/"+runID.String(), nil)
	request.SetPathValue("campaignRunId", runID.String())
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)
	g.Expect(response.Code).To(gomega.Equal(http.StatusOK))
	g.Expect(response.Body.String()).To(gomega.ContainSubstring(runID.String()))
	g.Expect(response.Body.String()).To(gomega.ContainSubstring(`"state":"RUNNING"`))
}

func TestCampaignRunMutationsAuthorizeResolvedRuntimeBeforeService(t *testing.T) {
	userAuth := testChannelAuth()
	organizationID := *userAuth.CurrentOrgID()
	revisionID := uuid.New()
	runID := uuid.New()

	for name, testCase := range map[string]struct {
		body       string
		pathName   string
		pathValue  string
		assertions func(*testing.T, *campaignRuntimeAuthorizerStub)
	}{
		"start": {
			body: `{"campaignRevisionId":"` + revisionID.String() + `"}`,
			assertions: func(t *testing.T, authorizer *campaignRuntimeAuthorizerStub) {
				gomega.NewWithT(t).Expect(authorizer.revisionID).To(gomega.Equal(revisionID))
			},
		},
		"transition": {
			body:      `{"expectedVersion":1,"to":"RUNNING","reason":"approved"}`,
			pathName:  "campaignRunId",
			pathValue: runID.String(),
			assertions: func(t *testing.T, authorizer *campaignRuntimeAuthorizerStub) {
				gomega.NewWithT(t).Expect(authorizer.runID).To(gomega.Equal(runID))
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			service := &campaignRunServiceStub{}
			authorizer := &campaignRuntimeAuthorizerStub{err: apierrors.ErrForbidden}
			var handler http.Handler
			if name == "start" {
				handler = startDeploymentCampaignRunHandler(service, authorizer)
			} else {
				handler = transitionDeploymentCampaignRunHandler(service, authorizer)
			}
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(testCase.body))
			if testCase.pathName != "" {
				request.SetPathValue(testCase.pathName, testCase.pathValue)
			}
			request = request.WithContext(auth.Authentication.NewContext(request.Context(), userAuth))
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			gomega.NewWithT(t).Expect(response.Code).To(gomega.Equal(http.StatusForbidden))
			gomega.NewWithT(t).Expect(service.mutationCalls).To(gomega.Equal(0))
			gomega.NewWithT(t).Expect(authorizer.organizationID).To(gomega.Equal(organizationID))
			testCase.assertions(t, authorizer)
		})
	}
}
