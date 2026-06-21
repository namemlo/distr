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
	. "github.com/onsi/gomega"
)

func TestDeploymentPlanHandlersCreateAndReadPlan(t *testing.T) {
	ctx := deploymentPlanHandlerDBTestContext(t)
	g := NewWithT(t)
	deps := createReleaseBundleEligibilityHandlerDependencies(t, ctx)
	_, revision := createReleaseBundleHandlerProcessRevision(t, ctx, deps.orgID, deps.applicationID)
	targetID := createVariableSnapshotHandlerDockerTarget(t, ctx, deps.orgID, "cluster-a")
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

	createRecorder := httptest.NewRecorder()
	createRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/deployment-plans",
		strings.NewReader(`{
			"releaseBundleId":"`+published.ID.String()+`",
			"environmentId":"`+deps.devEnvironmentID.String()+`",
			"targetIds":["`+targetID.String()+`"]
		}`),
	)
	createRequest = createRequest.WithContext(authenticatedReleaseBundleHandlerContext(ctx, deps.orgID, actorID))

	createDeploymentPlanHandler().ServeHTTP(createRecorder, createRequest)

	g.Expect(createRecorder.Code).To(Equal(http.StatusOK))
	var created api.DeploymentPlan
	g.Expect(json.Unmarshal(createRecorder.Body.Bytes(), &created)).To(Succeed())
	g.Expect(created.Status).To(Equal(types.DeploymentPlanStatusReady))
	g.Expect(created.ReleaseBundleID).To(Equal(published.ID))
	g.Expect(created.ProcessSnapshotID).To(Equal(published.ProcessSnapshotID))
	g.Expect(created.Targets).To(HaveLen(1))
	g.Expect(created.Steps).To(HaveLen(1))

	getRecorder := httptest.NewRecorder()
	getRequest := httptest.NewRequest(http.MethodGet, "/api/v1/deployment-plans/"+created.ID.String(), nil)
	getRequest.SetPathValue("deploymentPlanId", created.ID.String())
	getRequest = getRequest.WithContext(authenticatedReleaseBundleHandlerContext(ctx, deps.orgID, actorID))

	getDeploymentPlanHandler().ServeHTTP(getRecorder, getRequest)

	g.Expect(getRecorder.Code).To(Equal(http.StatusOK))
	var fetched api.DeploymentPlan
	g.Expect(json.Unmarshal(getRecorder.Body.Bytes(), &fetched)).To(Succeed())
	g.Expect(fetched.ID).To(Equal(created.ID))
	g.Expect(fetched.CanonicalChecksum).To(Equal(created.CanonicalChecksum))
}

func TestDeploymentPlanHandlersReturnNotFoundForCrossOrganizationTarget(t *testing.T) {
	ctx := deploymentPlanHandlerDBTestContext(t)
	g := NewWithT(t)
	deps := createReleaseBundleEligibilityHandlerDependencies(t, ctx)
	otherDeps := createReleaseBundleEligibilityHandlerDependencies(t, ctx)
	_, revision := createReleaseBundleHandlerProcessRevision(t, ctx, deps.orgID, deps.applicationID)
	otherTargetID := createVariableSnapshotHandlerDockerTarget(t, ctx, otherDeps.orgID, "cluster-b")
	actorID := createReleaseBundleHandlerUser(t, ctx, deps.orgID)
	bundle := types.ReleaseBundle{
		OrganizationID:              deps.orgID,
		ApplicationID:               deps.applicationID,
		ChannelID:                   deps.channelID,
		DeploymentProcessRevisionID: &revision.ID,
		ReleaseNumber:               "2026.06.21",
		Components: []types.ReleaseBundleComponent{
			{
				Key:                  "api",
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

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/deployment-plans",
		strings.NewReader(`{
			"releaseBundleId":"`+published.ID.String()+`",
			"environmentId":"`+deps.devEnvironmentID.String()+`",
			"targetIds":["`+otherTargetID.String()+`"]
		}`),
	)
	request = request.WithContext(authenticatedReleaseBundleHandlerContext(ctx, deps.orgID, actorID))

	createDeploymentPlanHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusNotFound))
}

func TestDeploymentPlanGetHandlerRejectsMalformedUUID(t *testing.T) {
	g := NewWithT(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/deployment-plans/not-a-uuid", nil)
	request.SetPathValue("deploymentPlanId", "not-a-uuid")

	getDeploymentPlanHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusNotFound))
}

func deploymentPlanHandlerDBTestContext(t *testing.T) context.Context {
	t.Helper()
	return channelHandlerDBTestContext(t)
}
