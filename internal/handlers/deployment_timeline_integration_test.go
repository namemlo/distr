package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestDeploymentTimelineHandlersListCompareAndRedeploy(t *testing.T) {
	ctx := taskHandlerDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyTaskHandlerPlan(t, ctx, "cluster-a")
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(tasks).To(HaveLen(1))

	listRecorder := httptest.NewRecorder()
	listRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/deployment-timeline?applicationId="+deps.plan.ApplicationID.String(),
		nil,
	)
	listRequest = listRequest.WithContext(authenticatedReleaseBundleHandlerContext(ctx, deps.orgID, deps.actorID))

	getDeploymentTimelineHandler().ServeHTTP(listRecorder, listRequest)

	g.Expect(listRecorder.Code).To(Equal(http.StatusOK))
	var timeline api.DeploymentTimeline
	g.Expect(json.Unmarshal(listRecorder.Body.Bytes(), &timeline)).To(Succeed())
	g.Expect(timeline.Items).To(HaveLen(1))
	g.Expect(timeline.Items[0].TaskID).To(Equal(tasks[0].ID))
	g.Expect(timeline.Items[0].ActorUserAccountID).To(Equal(&deps.actorID))
	g.Expect(timeline.Items[0].Components).To(HaveLen(1))
	g.Expect(timeline.Items[0].RedeployAvailable).To(BeTrue())

	compareRecorder := httptest.NewRecorder()
	compareRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/deployment-timeline/compare?baseTaskId="+tasks[0].ID.String()+"&compareTaskId="+tasks[0].ID.String(),
		nil,
	)
	compareRequest = compareRequest.WithContext(authenticatedReleaseBundleHandlerContext(ctx, deps.orgID, deps.actorID))

	compareDeploymentTimelineHandler().ServeHTTP(compareRecorder, compareRequest)

	g.Expect(compareRecorder.Code).To(Equal(http.StatusOK))
	var comparison api.DeploymentTimelineComparison
	g.Expect(json.Unmarshal(compareRecorder.Body.Bytes(), &comparison)).To(Succeed())
	g.Expect(comparison.Base.TaskID).To(Equal(tasks[0].ID))
	g.Expect(comparison.Compare.TaskID).To(Equal(tasks[0].ID))
	g.Expect(comparison.Process.Changed).To(BeFalse())

	redeployRecorder := httptest.NewRecorder()
	redeployRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/deployment-timeline/"+tasks[0].ID.String()+"/redeploy",
		nil,
	)
	redeployRequest.SetPathValue("taskId", tasks[0].ID.String())
	redeployRequest = redeployRequest.WithContext(authenticatedReleaseBundleHandlerContext(ctx, deps.orgID, deps.actorID))

	redeployDeploymentTimelineTaskHandler().ServeHTTP(redeployRecorder, redeployRequest)

	g.Expect(redeployRecorder.Code).To(Equal(http.StatusOK))
	var redeploy api.DeploymentTimelineRedeploy
	g.Expect(json.Unmarshal(redeployRecorder.Body.Bytes(), &redeploy)).To(Succeed())
	g.Expect(redeploy.Warning).To(ContainSubstring("Deploy previous release"))
	g.Expect(redeploy.Plan.ID).NotTo(Equal(deps.plan.ID))
	g.Expect(redeploy.Plan.ReleaseBundleID).To(Equal(deps.plan.ReleaseBundleID))
	g.Expect(redeploy.Plan.Targets).To(HaveLen(1))
}

func TestDeploymentTimelineHandlersRejectInvalidQueryIDs(t *testing.T) {
	ctx := taskHandlerDBTestContext(t)
	g := NewWithT(t)
	authCtx := authenticatedReleaseBundleHandlerContext(ctx, uuid.New(), uuid.New())

	listRecorder := httptest.NewRecorder()
	listRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/deployment-timeline?applicationId=not-a-uuid",
		nil,
	)
	listRequest = listRequest.WithContext(authCtx)

	getDeploymentTimelineHandler().ServeHTTP(listRecorder, listRequest)

	g.Expect(listRecorder.Code).To(Equal(http.StatusBadRequest))

	compareRecorder := httptest.NewRecorder()
	compareRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/deployment-timeline/compare?baseTaskId=not-a-uuid&compareTaskId=also-bad",
		nil,
	)
	compareRequest = compareRequest.WithContext(authCtx)

	compareDeploymentTimelineHandler().ServeHTTP(compareRecorder, compareRequest)

	g.Expect(compareRecorder.Code).To(Equal(http.StatusBadRequest))
}

func TestDeploymentTimelineFeatureFlagMiddlewareRejectsDisabledAPI(t *testing.T) {
	g := NewWithT(t)
	called := false
	handler := deploymentTimelineFeatureFlagMiddleware(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/deployment-timeline", nil)

	handler.ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusForbidden))
	g.Expect(called).To(BeFalse())
}
