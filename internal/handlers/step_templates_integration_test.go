package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/distr-sh/distr/api"
	. "github.com/onsi/gomega"
)

func TestStepTemplateHandlersImportListAndReadTenantTemplate(t *testing.T) {
	ctx := stepTemplateHandlerDBTestContext(t)
	g := NewWithT(t)
	orgID, _, _ := createChannelHandlerDependencies(t, ctx)
	otherOrgID, _, _ := createChannelHandlerDependencies(t, ctx)
	actorID := createReleaseBundleHandlerUser(t, ctx, orgID)

	createRecorder := httptest.NewRecorder()
	createRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/step-templates/import",
		strings.NewReader(`{
			"sourceType":"builtin",
			"sourceRef":"builtin/http-health-check",
			"name":"HTTP health check",
			"description":"Checks that an HTTP endpoint returns a healthy status.",
			"category":"Health",
			"version":"1.0.0",
			"actionType":"distr.http.check",
			"executionLocation":"hub",
			"inputSchema":{"type":"object","additionalProperties":true},
			"outputSchema":{"type":"object","additionalProperties":true},
			"defaultInputBindings":{"url":"https://example.com/health"},
			"minimumAgentVersion":"1.0.0",
			"compatibleActionVersion":"1",
			"runtimeCompatibilityNotes":"Uses the built-in HTTP check action."
		}`),
	)
	createRequest = createRequest.WithContext(authenticatedReleaseBundleHandlerContext(ctx, orgID, actorID))

	importStepTemplateHandler().ServeHTTP(createRecorder, createRequest)

	g.Expect(createRecorder.Code).To(Equal(http.StatusOK))
	var imported api.StepTemplate
	g.Expect(json.Unmarshal(createRecorder.Body.Bytes(), &imported)).To(Succeed())
	g.Expect(imported.ID).NotTo(BeZero())
	g.Expect(imported.InstalledByUserAccountID).To(Equal(&actorID))
	g.Expect(imported.Versions).To(HaveLen(1))
	g.Expect(imported.Versions[0].DefaultInputBindings).To(HaveKeyWithValue("url", "https://example.com/health"))

	listRecorder := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/step-templates", nil)
	listRequest = listRequest.WithContext(authenticatedReleaseBundleHandlerContext(ctx, orgID, actorID))

	getStepTemplatesHandler().ServeHTTP(listRecorder, listRequest)

	g.Expect(listRecorder.Code).To(Equal(http.StatusOK))
	var list []api.StepTemplate
	g.Expect(json.Unmarshal(listRecorder.Body.Bytes(), &list)).To(Succeed())
	g.Expect(list).To(HaveLen(1))
	g.Expect(list[0].ID).To(Equal(imported.ID))

	getRecorder := httptest.NewRecorder()
	getRequest := httptest.NewRequest(http.MethodGet, "/api/v1/step-templates/"+imported.ID.String(), nil)
	getRequest.SetPathValue("stepTemplateId", imported.ID.String())
	getRequest = getRequest.WithContext(authenticatedReleaseBundleHandlerContext(ctx, orgID, actorID))

	getStepTemplateHandler().ServeHTTP(getRecorder, getRequest)

	g.Expect(getRecorder.Code).To(Equal(http.StatusOK))
	var fetched api.StepTemplate
	g.Expect(json.Unmarshal(getRecorder.Body.Bytes(), &fetched)).To(Succeed())
	g.Expect(fetched.ID).To(Equal(imported.ID))

	crossOrgRecorder := httptest.NewRecorder()
	crossOrgRequest := httptest.NewRequest(http.MethodGet, "/api/v1/step-templates/"+imported.ID.String(), nil)
	crossOrgRequest.SetPathValue("stepTemplateId", imported.ID.String())
	crossOrgRequest = crossOrgRequest.WithContext(authenticatedChannelHandlerContext(ctx, otherOrgID))

	getStepTemplateHandler().ServeHTTP(crossOrgRecorder, crossOrgRequest)

	g.Expect(crossOrgRecorder.Code).To(Equal(http.StatusNotFound))
}

func stepTemplateHandlerDBTestContext(t *testing.T) context.Context {
	t.Helper()
	return channelHandlerDBTestContext(t)
}
