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

func TestReleaseBundleHandlersCreateReadUpdateDeleteDraft(t *testing.T) {
	ctx := channelHandlerDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleHandlerDependencies(t, ctx)

	createRecorder := httptest.NewRecorder()
	createRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/release-bundles",
		strings.NewReader(releaseBundleRequestBody(applicationID, channelID, versionID, "2026.06.20", "1.2.3")),
	)
	createRequest = createRequest.WithContext(authenticatedChannelHandlerContext(ctx, orgID))

	createReleaseBundleHandler().ServeHTTP(createRecorder, createRequest)

	g.Expect(createRecorder.Code).To(Equal(http.StatusOK))
	var created api.ReleaseBundle
	g.Expect(json.Unmarshal(createRecorder.Body.Bytes(), &created)).To(Succeed())
	g.Expect(created.CanonicalChecksum).To(HavePrefix("sha256:"))
	g.Expect(created.Components).To(HaveLen(1))

	getRecorder := httptest.NewRecorder()
	getRequest := httptest.NewRequest(http.MethodGet, "/api/v1/release-bundles/"+created.ID.String(), nil)
	getRequest.SetPathValue("releaseBundleId", created.ID.String())
	getRequest = getRequest.WithContext(authenticatedChannelHandlerContext(ctx, orgID))

	getReleaseBundleHandler().ServeHTTP(getRecorder, getRequest)

	g.Expect(getRecorder.Code).To(Equal(http.StatusOK))

	updateRecorder := httptest.NewRecorder()
	updateRequest := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/release-bundles/"+created.ID.String(),
		strings.NewReader(releaseBundleRequestBody(applicationID, channelID, versionID, "2026.06.20", "1.2.4")),
	)
	updateRequest.SetPathValue("releaseBundleId", created.ID.String())
	updateRequest = updateRequest.WithContext(authenticatedChannelHandlerContext(ctx, orgID))

	updateReleaseBundleHandler().ServeHTTP(updateRecorder, updateRequest)

	g.Expect(updateRecorder.Code).To(Equal(http.StatusOK))
	var updated api.ReleaseBundle
	g.Expect(json.Unmarshal(updateRecorder.Body.Bytes(), &updated)).To(Succeed())
	g.Expect(updated.CanonicalChecksum).NotTo(Equal(created.CanonicalChecksum))
	g.Expect(updated.Components[0].Version).To(Equal("1.2.4"))

	deleteRecorder := httptest.NewRecorder()
	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/release-bundles/"+created.ID.String(), nil)
	deleteRequest.SetPathValue("releaseBundleId", created.ID.String())
	deleteRequest = deleteRequest.WithContext(authenticatedChannelHandlerContext(ctx, orgID))

	deleteReleaseBundleHandler().ServeHTTP(deleteRecorder, deleteRequest)

	g.Expect(deleteRecorder.Code).To(Equal(http.StatusNoContent))
}

func TestReleaseBundleHandlersReturnNotFoundForCrossOrganizationReferences(t *testing.T) {
	ctx := channelHandlerDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleHandlerDependencies(t, ctx)
	otherOrgID, otherApplicationID, otherChannelID, otherVersionID := createReleaseBundleHandlerDependencies(t, ctx)
	_ = otherApplicationID
	_ = otherChannelID
	_ = otherVersionID

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/release-bundles",
		strings.NewReader(releaseBundleRequestBody(applicationID, channelID, versionID, "2026.06.20", "1.2.3")),
	)
	request = request.WithContext(authenticatedChannelHandlerContext(ctx, otherOrgID))

	createReleaseBundleHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusNotFound))
	_, err := db.GetReleaseBundlesByOrganizationID(ctx, orgID)
	g.Expect(err).NotTo(HaveOccurred())
}

func createReleaseBundleHandlerDependencies(
	t *testing.T,
	ctx context.Context,
) (uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	orgID, applicationID, lifecycleID := createChannelHandlerDependencies(t, ctx)
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
	version := types.ApplicationVersion{
		Name:            "1.2.3",
		ApplicationID:   applicationID,
		LinkTemplate:    "https://example.com/{{.version}}",
		ComposeFileData: []byte("services: {}\n"),
	}
	if err := db.CreateApplicationVersion(ctx, &version); err != nil {
		t.Fatalf("create application version: %v", err)
	}
	return orgID, applicationID, channel.ID, version.ID
}

func releaseBundleRequestBody(
	applicationID uuid.UUID,
	channelID uuid.UUID,
	versionID uuid.UUID,
	releaseNumber string,
	componentVersion string,
) string {
	return `{
		"applicationId":"` + applicationID.String() + `",
		"channelId":"` + channelID.String() + `",
		"releaseNumber":"` + releaseNumber + `",
		"releaseNotes":"Initial release",
		"sourceRevision":"abc123",
		"components":[
			{
				"key":"api",
				"name":"API",
				"type":"application_version",
				"version":"` + componentVersion + `",
				"applicationVersionId":"` + versionID.String() + `"
			}
		]
	}`
}
