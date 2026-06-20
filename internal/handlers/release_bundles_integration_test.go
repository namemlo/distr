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
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
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

func TestReleaseBundleCreateHandlerUsesIdempotencyKey(t *testing.T) {
	ctx := channelHandlerDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleHandlerDependencies(t, ctx)
	body := releaseBundleRequestBody(applicationID, channelID, versionID, "2026.06.20", "1.2.3")

	firstRecorder := httptest.NewRecorder()
	firstRequest := httptest.NewRequest(http.MethodPost, "/api/v1/release-bundles", strings.NewReader(body))
	firstRequest.Header.Set("Idempotency-Key", "ci-run-123")
	firstRequest = firstRequest.WithContext(authenticatedChannelHandlerContext(ctx, orgID))

	createReleaseBundleHandler().ServeHTTP(firstRecorder, firstRequest)

	g.Expect(firstRecorder.Code).To(Equal(http.StatusOK))
	var first api.ReleaseBundle
	g.Expect(json.Unmarshal(firstRecorder.Body.Bytes(), &first)).To(Succeed())

	secondRecorder := httptest.NewRecorder()
	secondRequest := httptest.NewRequest(http.MethodPost, "/api/v1/release-bundles", strings.NewReader(body))
	secondRequest.Header.Set("Idempotency-Key", " ci-run-123 ")
	secondRequest = secondRequest.WithContext(authenticatedChannelHandlerContext(ctx, orgID))

	createReleaseBundleHandler().ServeHTTP(secondRecorder, secondRequest)

	g.Expect(secondRecorder.Code).To(Equal(http.StatusOK))
	var second api.ReleaseBundle
	g.Expect(json.Unmarshal(secondRecorder.Body.Bytes(), &second)).To(Succeed())
	g.Expect(second.ID).To(Equal(first.ID))

	listed, err := db.GetReleaseBundlesByOrganizationID(ctx, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(listed).To(HaveLen(1))
}

func TestReleaseBundleCreateHandlerReturnsStructuredIdempotencyConflict(t *testing.T) {
	ctx := channelHandlerDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleHandlerDependencies(t, ctx)

	firstRecorder := httptest.NewRecorder()
	firstRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/release-bundles",
		strings.NewReader(releaseBundleRequestBody(applicationID, channelID, versionID, "2026.06.20", "1.2.3")),
	)
	firstRequest.Header.Set("Idempotency-Key", "ci-conflict")
	firstRequest = firstRequest.WithContext(authenticatedChannelHandlerContext(ctx, orgID))

	createReleaseBundleHandler().ServeHTTP(firstRecorder, firstRequest)
	g.Expect(firstRecorder.Code).To(Equal(http.StatusOK))

	conflictRecorder := httptest.NewRecorder()
	conflictRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/release-bundles",
		strings.NewReader(releaseBundleRequestBody(applicationID, channelID, versionID, "2026.06.21", "1.2.3")),
	)
	conflictRequest.Header.Set("Idempotency-Key", "ci-conflict")
	conflictRequest = conflictRequest.WithContext(authenticatedChannelHandlerContext(ctx, orgID))

	createReleaseBundleHandler().ServeHTTP(conflictRecorder, conflictRequest)

	g.Expect(conflictRecorder.Code).To(Equal(http.StatusConflict))
	var response api.ErrorResponse
	g.Expect(json.Unmarshal(conflictRecorder.Body.Bytes(), &response)).To(Succeed())
	g.Expect(response.Code).To(Equal(api.ErrorCodeIdempotencyKeyReusedWithDifferentRequest))
	g.Expect(response.Message).To(Equal("idempotency key was already used with a different release bundle request"))
}

func TestReleaseBundleCreateHandlerWithoutIdempotencyKeyPreservesDuplicateBehavior(t *testing.T) {
	ctx := channelHandlerDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleHandlerDependencies(t, ctx)
	body := releaseBundleRequestBody(applicationID, channelID, versionID, "2026.06.20", "1.2.3")

	firstRecorder := httptest.NewRecorder()
	firstRequest := httptest.NewRequest(http.MethodPost, "/api/v1/release-bundles", strings.NewReader(body))
	firstRequest = firstRequest.WithContext(authenticatedChannelHandlerContext(ctx, orgID))
	createReleaseBundleHandler().ServeHTTP(firstRecorder, firstRequest)
	g.Expect(firstRecorder.Code).To(Equal(http.StatusOK))

	secondRecorder := httptest.NewRecorder()
	secondRequest := httptest.NewRequest(http.MethodPost, "/api/v1/release-bundles", strings.NewReader(body))
	secondRequest = secondRequest.WithContext(authenticatedChannelHandlerContext(ctx, orgID))
	createReleaseBundleHandler().ServeHTTP(secondRecorder, secondRequest)

	g.Expect(secondRecorder.Code).To(Equal(http.StatusBadRequest))
}

func TestReleaseBundleHandlersValidatePublishBlockAndArchive(t *testing.T) {
	ctx := channelHandlerDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleHandlerDependencies(t, ctx)
	actorID := createReleaseBundleHandlerUser(t, ctx, orgID)
	bundle := types.ReleaseBundle{
		OrganizationID: orgID,
		ApplicationID:  applicationID,
		ChannelID:      channelID,
		ReleaseNumber:  "1.2.3",
		ReleaseNotes:   "Initial release",
		SourceRevision: "abc123",
		Components: []types.ReleaseBundleComponent{
			{
				Key:                  "api",
				Name:                 "API",
				Type:                 types.ReleaseBundleComponentTypeApplicationVersion,
				Version:              "1.2.3",
				ApplicationVersionID: &versionID,
			},
		},
	}
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())

	validateRecorder := httptest.NewRecorder()
	validateRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/release-bundles/"+bundle.ID.String()+"/validate",
		nil,
	)
	validateRequest.SetPathValue("releaseBundleId", bundle.ID.String())
	validateRequest = validateRequest.WithContext(authenticatedReleaseBundleHandlerContext(ctx, orgID, actorID))

	validateReleaseBundleHandler().ServeHTTP(validateRecorder, validateRequest)

	g.Expect(validateRecorder.Code).To(Equal(http.StatusOK))
	var validationResponse api.ReleaseBundleValidationResponse
	g.Expect(json.Unmarshal(validateRecorder.Body.Bytes(), &validationResponse)).To(Succeed())
	g.Expect(validationResponse.Valid).To(BeTrue())

	publishRecorder := httptest.NewRecorder()
	publishRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/release-bundles/"+bundle.ID.String()+"/publish",
		nil,
	)
	publishRequest.SetPathValue("releaseBundleId", bundle.ID.String())
	publishRequest = publishRequest.WithContext(authenticatedReleaseBundleHandlerContext(ctx, orgID, actorID))

	publishReleaseBundleHandler().ServeHTTP(publishRecorder, publishRequest)

	g.Expect(publishRecorder.Code).To(Equal(http.StatusOK))
	var published api.ReleaseBundle
	g.Expect(json.Unmarshal(publishRecorder.Body.Bytes(), &published)).To(Succeed())
	g.Expect(published.Status).To(Equal(types.ReleaseBundleStatusPublished))
	g.Expect(published.PublishedByUserAccountID).To(Equal(&actorID))
	g.Expect(published.PublishedAt).NotTo(BeNil())

	blockRecorder := httptest.NewRecorder()
	blockRequest := httptest.NewRequest(http.MethodPost, "/api/v1/release-bundles/"+bundle.ID.String()+"/block", nil)
	blockRequest.SetPathValue("releaseBundleId", bundle.ID.String())
	blockRequest = blockRequest.WithContext(authenticatedReleaseBundleHandlerContext(ctx, orgID, actorID))

	blockReleaseBundleHandler().ServeHTTP(blockRecorder, blockRequest)

	g.Expect(blockRecorder.Code).To(Equal(http.StatusOK))
	var blocked api.ReleaseBundle
	g.Expect(json.Unmarshal(blockRecorder.Body.Bytes(), &blocked)).To(Succeed())
	g.Expect(blocked.Status).To(Equal(types.ReleaseBundleStatusBlocked))

	archiveRecorder := httptest.NewRecorder()
	archiveRequest := httptest.NewRequest(http.MethodPost, "/api/v1/release-bundles/"+bundle.ID.String()+"/archive", nil)
	archiveRequest.SetPathValue("releaseBundleId", bundle.ID.String())
	archiveRequest = archiveRequest.WithContext(authenticatedReleaseBundleHandlerContext(ctx, orgID, actorID))

	archiveReleaseBundleHandler().ServeHTTP(archiveRecorder, archiveRequest)

	g.Expect(archiveRecorder.Code).To(Equal(http.StatusOK))
	var archived api.ReleaseBundle
	g.Expect(json.Unmarshal(archiveRecorder.Body.Bytes(), &archived)).To(Succeed())
	g.Expect(archived.Status).To(Equal(types.ReleaseBundleStatusArchived))
}

func TestReleaseBundleEligibilityHandlerExplainsPublishedBundle(t *testing.T) {
	ctx := channelHandlerDBTestContext(t)
	g := NewWithT(t)
	deps := createReleaseBundleEligibilityHandlerDependencies(t, ctx)
	actorID := createReleaseBundleHandlerUser(t, ctx, deps.orgID)
	bundle := types.ReleaseBundle{
		OrganizationID: deps.orgID,
		ApplicationID:  deps.applicationID,
		ChannelID:      deps.channelID,
		ReleaseNumber:  "2026.06.20",
		ReleaseNotes:   "Initial release",
		SourceRevision: "abc123",
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
	_, publishResult, err := db.PublishReleaseBundle(ctx, bundle.ID, deps.orgID, actorID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(publishResult.Valid).To(BeTrue())

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/release-bundles/"+bundle.ID.String()+"/eligibility?environmentId="+deps.devEnvironmentID.String(),
		nil,
	)
	request.SetPathValue("releaseBundleId", bundle.ID.String())
	request = request.WithContext(authenticatedReleaseBundleHandlerContext(ctx, deps.orgID, actorID))

	getReleaseBundleEligibilityHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusOK))
	var response api.ReleaseBundleEligibilityResponse
	g.Expect(json.Unmarshal(recorder.Body.Bytes(), &response)).To(Succeed())
	g.Expect(response.ReleaseBundleID).To(Equal(bundle.ID))
	g.Expect(response.ApplicationID).To(Equal(deps.applicationID))
	g.Expect(response.ChannelID).To(Equal(deps.channelID))
	g.Expect(response.LifecycleID).To(Equal(deps.lifecycleID))
	g.Expect(response.EnvironmentID).To(Equal(deps.devEnvironmentID))
	g.Expect(response.EngineReady).To(BeTrue())
	g.Expect(response.Eligible).To(BeTrue())
	g.Expect(response.TargetPhase).NotTo(BeNil())
	g.Expect(response.TargetPhase.Name).To(Equal("Development"))
	g.Expect(response.Reasons).To(BeEmpty())
}

func TestReleaseBundleEligibilityHandlerReturnsNotFoundForCrossOrganizationReferences(t *testing.T) {
	ctx := channelHandlerDBTestContext(t)
	g := NewWithT(t)
	deps := createReleaseBundleEligibilityHandlerDependencies(t, ctx)
	otherDeps := createReleaseBundleEligibilityHandlerDependencies(t, ctx)
	actorID := createReleaseBundleHandlerUser(t, ctx, deps.orgID)
	bundle := types.ReleaseBundle{
		OrganizationID: deps.orgID,
		ApplicationID:  deps.applicationID,
		ChannelID:      deps.channelID,
		ReleaseNumber:  "2026.06.20",
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

	crossOrgEnvironmentRecorder := httptest.NewRecorder()
	crossOrgEnvironmentRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/release-bundles/"+bundle.ID.String()+"/eligibility?environmentId="+otherDeps.devEnvironmentID.String(),
		nil,
	)
	crossOrgEnvironmentRequest.SetPathValue("releaseBundleId", bundle.ID.String())
	crossOrgEnvironmentRequest = crossOrgEnvironmentRequest.WithContext(
		authenticatedReleaseBundleHandlerContext(ctx, deps.orgID, actorID),
	)

	getReleaseBundleEligibilityHandler().ServeHTTP(crossOrgEnvironmentRecorder, crossOrgEnvironmentRequest)

	g.Expect(crossOrgEnvironmentRecorder.Code).To(Equal(http.StatusNotFound))

	crossOrgBundleRecorder := httptest.NewRecorder()
	crossOrgBundleRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/release-bundles/"+bundle.ID.String()+"/eligibility?environmentId="+deps.devEnvironmentID.String(),
		nil,
	)
	crossOrgBundleRequest.SetPathValue("releaseBundleId", bundle.ID.String())
	crossOrgBundleRequest = crossOrgBundleRequest.WithContext(
		authenticatedReleaseBundleHandlerContext(ctx, otherDeps.orgID, actorID),
	)

	getReleaseBundleEligibilityHandler().ServeHTTP(crossOrgBundleRecorder, crossOrgBundleRequest)

	g.Expect(crossOrgBundleRecorder.Code).To(Equal(http.StatusNotFound))
}

func TestReleaseBundlePublishHandlerReturnsValidationErrors(t *testing.T) {
	ctx := channelHandlerDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, lifecycleID := createChannelHandlerDependencies(t, ctx)
	actorID := createReleaseBundleHandlerUser(t, ctx, orgID)
	channel := types.Channel{
		OrganizationID:       orgID,
		ApplicationID:        applicationID,
		LifecycleID:          lifecycleID,
		Name:                 "Preview",
		AllowedVersionRanges: []string{">=2.0.0 <3.0.0"},
	}
	g.Expect(db.CreateChannel(ctx, &channel)).To(Succeed())
	version := types.ApplicationVersion{
		Name:            "1.2.3",
		ApplicationID:   applicationID,
		LinkTemplate:    "https://example.com/{{.version}}",
		ComposeFileData: []byte("services: {}\n"),
	}
	g.Expect(db.CreateApplicationVersion(ctx, &version)).To(Succeed())
	bundle := types.ReleaseBundle{
		OrganizationID: orgID,
		ApplicationID:  applicationID,
		ChannelID:      channel.ID,
		ReleaseNumber:  "1.2.3",
		Components: []types.ReleaseBundleComponent{
			{
				Key:                  "api",
				Type:                 types.ReleaseBundleComponentTypeApplicationVersion,
				Version:              "1.2.3",
				ApplicationVersionID: &version.ID,
			},
		},
	}
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/release-bundles/"+bundle.ID.String()+"/publish", nil)
	request.SetPathValue("releaseBundleId", bundle.ID.String())
	request = request.WithContext(authenticatedReleaseBundleHandlerContext(ctx, orgID, actorID))

	publishReleaseBundleHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusBadRequest))
	var response api.ReleaseBundleValidationResponse
	g.Expect(json.Unmarshal(recorder.Body.Bytes(), &response)).To(Succeed())
	g.Expect(response.Valid).To(BeFalse())
	g.Expect(response.Errors).To(ContainElement(api.ReleaseBundleValidationIssue{
		Field:   "components.api.version",
		Rule:    ">=2.0.0 <3.0.0",
		Message: "version does not match an allowed range",
	}))
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

type releaseBundleEligibilityHandlerDependencies struct {
	orgID             uuid.UUID
	applicationID     uuid.UUID
	channelID         uuid.UUID
	lifecycleID       uuid.UUID
	versionID         uuid.UUID
	devEnvironmentID  uuid.UUID
	prodEnvironmentID uuid.UUID
}

func createReleaseBundleEligibilityHandlerDependencies(
	t *testing.T,
	ctx context.Context,
) releaseBundleEligibilityHandlerDependencies {
	t.Helper()
	orgID, applicationID, lifecycleID := createChannelHandlerDependencies(t, ctx)
	devEnvironment := types.Environment{
		OrganizationID: orgID,
		Name:           "Development",
		SortOrder:      10,
	}
	g := NewWithT(t)
	g.Expect(db.CreateEnvironment(ctx, &devEnvironment)).To(Succeed())
	prodEnvironment := types.Environment{
		OrganizationID: orgID,
		Name:           "Production",
		SortOrder:      20,
		IsProduction:   true,
	}
	g.Expect(db.CreateEnvironment(ctx, &prodEnvironment)).To(Succeed())
	_, err := db.ReplaceLifecyclePhases(ctx, lifecycleID, orgID, []types.LifecyclePhase{
		{
			Name:                         "Development",
			SortOrder:                    10,
			EnvironmentIDs:               []uuid.UUID{devEnvironment.ID},
			MinimumSuccessfulDeployments: 1,
		},
		{
			Name:                         "Production",
			SortOrder:                    20,
			EnvironmentIDs:               []uuid.UUID{prodEnvironment.ID},
			MinimumSuccessfulDeployments: 1,
		},
	})
	g.Expect(err).NotTo(HaveOccurred())
	channel := types.Channel{
		OrganizationID: orgID,
		ApplicationID:  applicationID,
		LifecycleID:    lifecycleID,
		Name:           "Stable",
		IsDefault:      true,
	}
	g.Expect(db.CreateChannel(ctx, &channel)).To(Succeed())
	version := types.ApplicationVersion{
		Name:            "1.2.3",
		ApplicationID:   applicationID,
		LinkTemplate:    "https://example.com/{{.version}}",
		ComposeFileData: []byte("services: {}\n"),
	}
	g.Expect(db.CreateApplicationVersion(ctx, &version)).To(Succeed())
	return releaseBundleEligibilityHandlerDependencies{
		orgID:             orgID,
		applicationID:     applicationID,
		channelID:         channel.ID,
		lifecycleID:       lifecycleID,
		versionID:         version.ID,
		devEnvironmentID:  devEnvironment.ID,
		prodEnvironmentID: prodEnvironment.ID,
	}
}

func authenticatedReleaseBundleHandlerContext(ctx context.Context, orgID, userID uuid.UUID) context.Context {
	ctx = internalctx.WithLogger(ctx, zap.NewNop())
	channelAuth := testChannelAuth()
	channelAuth.orgID = orgID
	channelAuth.userID = userID
	return auth.Authentication.NewContext(ctx, channelAuth)
}

func createReleaseBundleHandlerUser(t *testing.T, ctx context.Context, orgID uuid.UUID) uuid.UUID {
	t.Helper()
	var userID uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO UserAccount (email) VALUES (@email) RETURNING id`,
		pgx.NamedArgs{"email": "release-bundle-" + uuid.NewString() + "@example.com"},
	).Scan(&userID); err != nil {
		t.Fatalf("create user account: %v", err)
	}
	if _, err := internalctx.GetDb(ctx).Exec(
		ctx,
		`INSERT INTO Organization_UserAccount (organization_id, user_account_id, user_role)
		VALUES (@organizationId, @userId, 'admin')`,
		pgx.NamedArgs{"organizationId": orgID, "userId": userID},
	); err != nil {
		t.Fatalf("create organization user account: %v", err)
	}
	return userID
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
