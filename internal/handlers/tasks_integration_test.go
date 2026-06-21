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

func TestTaskHandlersCreateListReadAndTransitionTask(t *testing.T) {
	ctx := taskHandlerDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyTaskHandlerPlan(t, ctx, "cluster-a")

	createRecorder := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/deployment-plans/"+deps.plan.ID.String()+"/tasks", nil)
	createRequest.SetPathValue("deploymentPlanId", deps.plan.ID.String())
	createRequest = createRequest.WithContext(authenticatedReleaseBundleHandlerContext(ctx, deps.orgID, deps.actorID))

	createTasksForDeploymentPlanHandler().ServeHTTP(createRecorder, createRequest)

	g.Expect(createRecorder.Code).To(Equal(http.StatusOK))
	var created []api.Task
	g.Expect(json.Unmarshal(createRecorder.Body.Bytes(), &created)).To(Succeed())
	g.Expect(created).To(HaveLen(1))
	g.Expect(created[0].Status).To(Equal(types.TaskStatusQueued))
	g.Expect(created[0].StepRuns).To(HaveLen(1))

	listRecorder := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
	listRequest = listRequest.WithContext(authenticatedReleaseBundleHandlerContext(ctx, deps.orgID, deps.actorID))

	getTasksHandler().ServeHTTP(listRecorder, listRequest)

	g.Expect(listRecorder.Code).To(Equal(http.StatusOK))
	var listed []api.Task
	g.Expect(json.Unmarshal(listRecorder.Body.Bytes(), &listed)).To(Succeed())
	g.Expect(listed).To(HaveLen(1))
	g.Expect(listed[0].ID).To(Equal(created[0].ID))

	getRecorder := httptest.NewRecorder()
	getRequest := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+created[0].ID.String(), nil)
	getRequest.SetPathValue("taskId", created[0].ID.String())
	getRequest = getRequest.WithContext(authenticatedReleaseBundleHandlerContext(ctx, deps.orgID, deps.actorID))

	getTaskHandler().ServeHTTP(getRecorder, getRequest)

	g.Expect(getRecorder.Code).To(Equal(http.StatusOK))
	var fetched api.Task
	g.Expect(json.Unmarshal(getRecorder.Body.Bytes(), &fetched)).To(Succeed())
	g.Expect(fetched.ID).To(Equal(created[0].ID))

	transitionRecorder := httptest.NewRecorder()
	transitionRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/tasks/"+created[0].ID.String()+"/state",
		strings.NewReader(`{"status":"RUNNING"}`),
	)
	transitionRequest.SetPathValue("taskId", created[0].ID.String())
	transitionRequest = transitionRequest.WithContext(
		authenticatedReleaseBundleHandlerContext(ctx, deps.orgID, deps.actorID),
	)

	transitionTaskStateHandler().ServeHTTP(transitionRecorder, transitionRequest)

	g.Expect(transitionRecorder.Code).To(Equal(http.StatusOK))
	var transitioned api.Task
	g.Expect(json.Unmarshal(transitionRecorder.Body.Bytes(), &transitioned)).To(Succeed())
	g.Expect(transitioned.Status).To(Equal(types.TaskStatusRunning))
	g.Expect(transitioned.StartedAt).NotTo(BeNil())
}

func TestTaskHandlersReturnNotFoundForCrossOrganizationPlan(t *testing.T) {
	ctx := taskHandlerDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyTaskHandlerPlan(t, ctx, "cluster-a")
	otherDeps := createReadyTaskHandlerPlan(t, ctx, "cluster-b")

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/deployment-plans/"+otherDeps.plan.ID.String()+"/tasks", nil)
	request.SetPathValue("deploymentPlanId", otherDeps.plan.ID.String())
	request = request.WithContext(authenticatedReleaseBundleHandlerContext(ctx, deps.orgID, deps.actorID))

	createTasksForDeploymentPlanHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusNotFound))
}

func TestTaskHandlersRejectTaskCreationWhenReleaseBundleIsBlocked(t *testing.T) {
	ctx := taskHandlerDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyTaskHandlerPlan(t, ctx, "cluster-a")
	blocked, err := db.BlockReleaseBundle(ctx, deps.plan.ReleaseBundleID, deps.orgID, deps.actorID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(blocked.Status).To(Equal(types.ReleaseBundleStatusBlocked))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/deployment-plans/"+deps.plan.ID.String()+"/tasks", nil)
	request.SetPathValue("deploymentPlanId", deps.plan.ID.String())
	request = request.WithContext(authenticatedReleaseBundleHandlerContext(ctx, deps.orgID, deps.actorID))

	createTasksForDeploymentPlanHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusConflict))
}

func TestTaskHandlersRejectMalformedUUIDs(t *testing.T) {
	g := NewWithT(t)

	createRecorder := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/deployment-plans/not-a-uuid/tasks", nil)
	createRequest.SetPathValue("deploymentPlanId", "not-a-uuid")
	createTasksForDeploymentPlanHandler().ServeHTTP(createRecorder, createRequest)
	g.Expect(createRecorder.Code).To(Equal(http.StatusNotFound))

	getRecorder := httptest.NewRecorder()
	getRequest := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/not-a-uuid", nil)
	getRequest.SetPathValue("taskId", "not-a-uuid")
	getTaskHandler().ServeHTTP(getRecorder, getRequest)
	g.Expect(getRecorder.Code).To(Equal(http.StatusNotFound))
}

func taskHandlerDBTestContext(t *testing.T) context.Context {
	t.Helper()
	return deploymentPlanHandlerDBTestContext(t)
}

type taskHandlerPlanDeps struct {
	orgID   uuid.UUID
	actorID uuid.UUID
	plan    *types.DeploymentPlan
}

func createReadyTaskHandlerPlan(t *testing.T, ctx context.Context, targetName string) taskHandlerPlanDeps {
	t.Helper()
	g := NewWithT(t)
	deps := createReleaseBundleEligibilityHandlerDependencies(t, ctx)
	_, revision := createReleaseBundleHandlerProcessRevision(t, ctx, deps.orgID, deps.applicationID)
	targetID := createVariableSnapshotHandlerDockerTarget(t, ctx, deps.orgID, targetName)
	actorID := createReleaseBundleHandlerUser(t, ctx, deps.orgID)
	bundle := types.ReleaseBundle{
		OrganizationID:              deps.orgID,
		ApplicationID:               deps.applicationID,
		ChannelID:                   deps.channelID,
		DeploymentProcessRevisionID: &revision.ID,
		ReleaseNumber:               "2026.06.21",
		ReleaseNotes:                "Initial release",
		SourceRevision:              "abc123",
		Components: []types.ReleaseBundleComponent{
			{
				Key:                  "api",
				Name:                 "API",
				Type:                 types.ReleaseBundleComponentTypeApplicationVersion,
				Version:              "1.2.3",
				ApplicationVersionID: &deps.versionID,
			},
		},
	}
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())
	published, result, err := db.PublishReleaseBundle(ctx, bundle.ID, deps.orgID, actorID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Valid).To(BeTrue())
	plan, err := db.CreateDeploymentPlan(ctx, types.CreateDeploymentPlanRequest{
		OrganizationID:  deps.orgID,
		ReleaseBundleID: published.ID,
		EnvironmentID:   deps.devEnvironmentID,
		TargetIDs:       []uuid.UUID{targetID},
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(plan.Status).To(Equal(types.DeploymentPlanStatusReady))
	return taskHandlerPlanDeps{orgID: deps.orgID, actorID: actorID, plan: plan}
}
