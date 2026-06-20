package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestDeploymentProcessHandlersCreateAndReadRevision(t *testing.T) {
	ctx := deploymentProcessHandlerDBTestContext(t)
	g := NewWithT(t)
	deps := createDeploymentProcessHandlerDependencies(t, ctx)

	createProcessRecorder := httptest.NewRecorder()
	createProcessRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/deployment-processes",
		strings.NewReader(`{"applicationId":"`+deps.applicationID.String()+`","name":" Standard deploy "}`),
	)
	createProcessRequest = createProcessRequest.WithContext(authenticatedDeploymentProcessHandlerContext(ctx, deps.orgID))

	createDeploymentProcessHandler().ServeHTTP(createProcessRecorder, createProcessRequest)

	g.Expect(createProcessRecorder.Code).To(Equal(http.StatusOK))
	var process api.DeploymentProcess
	g.Expect(json.Unmarshal(createProcessRecorder.Body.Bytes(), &process)).To(Succeed())
	g.Expect(process.Name).To(Equal("Standard deploy"))

	createRevisionRecorder := httptest.NewRecorder()
	createRevisionRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/deployment-processes/"+process.ID.String()+"/revisions",
		strings.NewReader(`{
			"description":" initial ",
			"steps":[
				{"key":"prepare","name":"Prepare","actionType":"script","executionLocation":"hub","sortOrder":10},
				{
					"key":"deploy",
					"name":"Deploy",
					"actionType":"script",
					"executionLocation":"hub",
					"inputBindings":{"script":"make deploy"},
					"channelIds":["`+deps.channelID.String()+`"],
					"environmentIds":["`+deps.environmentID.String()+`"],
					"sortOrder":20,
					"dependencies":["prepare"]
				}
			]
		}`),
	)
	createRevisionRequest.SetPathValue("deploymentProcessId", process.ID.String())
	createRevisionRequest = createRevisionRequest.WithContext(
		authenticatedDeploymentProcessHandlerContext(ctx, deps.orgID),
	)

	createDeploymentProcessRevisionHandler().ServeHTTP(createRevisionRecorder, createRevisionRequest)

	g.Expect(createRevisionRecorder.Code).To(Equal(http.StatusOK))
	var revision api.DeploymentProcessRevision
	g.Expect(json.Unmarshal(createRevisionRecorder.Body.Bytes(), &revision)).To(Succeed())
	g.Expect(revision.RevisionNumber).To(Equal(1))
	g.Expect(revision.Description).To(Equal("initial"))
	g.Expect(revision.Steps).To(HaveLen(2))
	g.Expect(revision.Steps[1].Dependencies).To(Equal([]string{"prepare"}))

	listRecorder := httptest.NewRecorder()
	listRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/deployment-processes/"+process.ID.String()+"/revisions",
		nil,
	)
	listRequest.SetPathValue("deploymentProcessId", process.ID.String())
	listRequest = listRequest.WithContext(authenticatedDeploymentProcessHandlerContext(ctx, deps.orgID))

	getDeploymentProcessRevisionsHandler().ServeHTTP(listRecorder, listRequest)

	g.Expect(listRecorder.Code).To(Equal(http.StatusOK))
	var revisions []api.DeploymentProcessRevision
	g.Expect(json.Unmarshal(listRecorder.Body.Bytes(), &revisions)).To(Succeed())
	g.Expect(revisions).To(HaveLen(1))

	getRecorder := httptest.NewRecorder()
	getRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/deployment-processes/"+process.ID.String()+"/revisions/"+revision.ID.String(),
		nil,
	)
	getRequest.SetPathValue("deploymentProcessId", process.ID.String())
	getRequest.SetPathValue("revisionId", revision.ID.String())
	getRequest = getRequest.WithContext(authenticatedDeploymentProcessHandlerContext(ctx, deps.orgID))

	getDeploymentProcessRevisionHandler().ServeHTTP(getRecorder, getRequest)

	g.Expect(getRecorder.Code).To(Equal(http.StatusOK))
}

func TestDeploymentProcessHandlersReturnNotFoundForCrossOrganizationReferences(t *testing.T) {
	ctx := deploymentProcessHandlerDBTestContext(t)
	g := NewWithT(t)
	deps := createDeploymentProcessHandlerDependencies(t, ctx)
	otherDeps := createDeploymentProcessHandlerDependencies(t, ctx)

	createProcessRecorder := httptest.NewRecorder()
	createProcessRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/deployment-processes",
		strings.NewReader(`{"applicationId":"`+otherDeps.applicationID.String()+`","name":"Cross org"}`),
	)
	createProcessRequest = createProcessRequest.WithContext(authenticatedDeploymentProcessHandlerContext(ctx, deps.orgID))

	createDeploymentProcessHandler().ServeHTTP(createProcessRecorder, createProcessRequest)

	g.Expect(createProcessRecorder.Code).To(Equal(http.StatusNotFound))

	process := types.DeploymentProcess{
		OrganizationID: deps.orgID,
		ApplicationID:  deps.applicationID,
		Name:           "Standard deploy",
	}
	g.Expect(db.CreateDeploymentProcess(ctx, &process)).To(Succeed())

	createRevisionRecorder := httptest.NewRecorder()
	createRevisionRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/deployment-processes/"+process.ID.String()+"/revisions",
		strings.NewReader(`{
			"steps":[
				{"key":"prepare","name":"Prepare","actionType":"script","executionLocation":"hub","sortOrder":10},
				{
					"key":"deploy",
					"name":"Deploy",
					"actionType":"script",
					"executionLocation":"hub",
					"channelIds":["`+otherDeps.channelID.String()+`"],
					"environmentIds":["`+deps.environmentID.String()+`"],
					"sortOrder":20,
					"dependencies":["prepare"]
				}
			]
		}`),
	)
	createRevisionRequest.SetPathValue("deploymentProcessId", process.ID.String())
	createRevisionRequest = createRevisionRequest.WithContext(
		authenticatedDeploymentProcessHandlerContext(ctx, deps.orgID),
	)

	createDeploymentProcessRevisionHandler().ServeHTTP(createRevisionRecorder, createRevisionRequest)

	g.Expect(createRevisionRecorder.Code).To(Equal(http.StatusNotFound))
}

func TestDeploymentProcessHandlerRejectsDuplicateName(t *testing.T) {
	ctx := deploymentProcessHandlerDBTestContext(t)
	g := NewWithT(t)
	deps := createDeploymentProcessHandlerDependencies(t, ctx)
	process := types.DeploymentProcess{
		OrganizationID: deps.orgID,
		ApplicationID:  deps.applicationID,
		Name:           "Standard deploy",
	}
	g.Expect(db.CreateDeploymentProcess(ctx, &process)).To(Succeed())

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/deployment-processes",
		strings.NewReader(`{"applicationId":"`+deps.applicationID.String()+`","name":"Standard deploy"}`),
	)
	request = request.WithContext(authenticatedDeploymentProcessHandlerContext(ctx, deps.orgID))

	createDeploymentProcessHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusBadRequest))
}

type deploymentProcessHandlerDependencies struct {
	orgID         uuid.UUID
	applicationID uuid.UUID
	lifecycleID   uuid.UUID
	channelID     uuid.UUID
	environmentID uuid.UUID
}

func deploymentProcessHandlerDBTestContext(t *testing.T) context.Context {
	t.Helper()
	return channelHandlerDBTestContext(t)
}

func authenticatedDeploymentProcessHandlerContext(ctx context.Context, orgID uuid.UUID) context.Context {
	return authenticatedChannelHandlerContext(ctx, orgID)
}

func createDeploymentProcessHandlerDependencies(
	t *testing.T,
	ctx context.Context,
) deploymentProcessHandlerDependencies {
	t.Helper()
	orgID, applicationID, lifecycleID := createChannelHandlerDependencies(t, ctx)
	environment := types.Environment{
		OrganizationID: orgID,
		Name:           "Environment " + uuid.NewString(),
	}
	if err := db.CreateEnvironment(ctx, &environment); err != nil {
		t.Fatalf("create environment: %v", err)
	}
	channel := types.Channel{
		OrganizationID: orgID,
		ApplicationID:  applicationID,
		LifecycleID:    lifecycleID,
		Name:           "Stable",
		IsDefault:      true,
	}
	if err := db.CreateChannel(ctx, &channel); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	return deploymentProcessHandlerDependencies{
		orgID:         orgID,
		applicationID: applicationID,
		lifecycleID:   lifecycleID,
		channelID:     channel.ID,
		environmentID: environment.ID,
	}
}
