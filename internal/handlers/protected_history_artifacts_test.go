package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/auth"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/protectedhistory"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

type protectedHistoryArtifactServiceStub struct {
	request protectedhistory.CreateRetentionRequest
	result  *protectedhistory.RetainedArtifact
}

type protectedHistoryArtifactTestAuth struct {
	releaseBundleTestAuth
	userID uuid.UUID
}

func (authentication protectedHistoryArtifactTestAuth) CurrentUserID() uuid.UUID {
	return authentication.userID
}

func (service *protectedHistoryArtifactServiceStub) Retain(
	_ context.Context,
	request protectedhistory.CreateRetentionRequest,
) (*protectedhistory.RetainedArtifact, error) {
	service.request = request
	return service.result, nil
}

func (service *protectedHistoryArtifactServiceStub) Get(
	context.Context, uuid.UUID, uuid.UUID,
) (*protectedhistory.RetainedArtifact, error) {
	return service.result, nil
}

func (service *protectedHistoryArtifactServiceStub) Verify(
	context.Context, uuid.UUID, uuid.UUID,
) (*protectedhistory.ObjectIdentity, error) {
	return &protectedhistory.ObjectIdentity{}, nil
}

func TestCreateProtectedHistoryArtifactUsesAuthenticatedIssuerAndOrganization(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	authentication := protectedHistoryArtifactTestAuth{
		releaseBundleTestAuth: testReleaseBundleAuth(),
		userID:                uuid.New(),
	}
	organizationID := *authentication.CurrentOrgID()
	issuerID := authentication.CurrentUserID()
	reviewerID := uuid.New()
	retained := &protectedhistory.RetainedArtifact{ID: uuid.New(), CreatedAt: time.Now().UTC()}
	service := &protectedHistoryArtifactServiceStub{result: retained}
	body, err := json.Marshal(map[string]any{
		"customerOrganizationIds": []uuid.UUID{uuid.New()},
		"reviewerUserAccountId":   reviewerID,
		"idempotencyKey":          "history:before-upgrade",
	})
	g.Expect(err).NotTo(HaveOccurred())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	ctx := internalctx.WithLogger(request.Context(), zap.NewNop())
	request = request.WithContext(auth.Authentication.NewContext(ctx, authentication))

	createProtectedHistoryArtifactHandler(service).ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusCreated))
	g.Expect(service.request.OrganizationID).To(Equal(organizationID))
	g.Expect(service.request.Scope.OrganizationID).To(Equal(organizationID.String()))
	g.Expect(service.request.IssuerUserAccountID).To(Equal(issuerID))
	g.Expect(service.request.ReviewerUserAccountID).To(Equal(reviewerID))
}

func TestCreateProtectedHistoryArtifactRejectsCallerSuppliedHistoryMaterial(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	authentication := protectedHistoryArtifactTestAuth{
		releaseBundleTestAuth: testReleaseBundleAuth(),
		userID:                uuid.New(),
	}
	service := &protectedHistoryArtifactServiceStub{}
	body := `{
  "customerOrganizationIds":["` + uuid.NewString() + `"],
  "reviewerUserAccountId":"` + uuid.NewString() + `",
  "idempotencyKey":"history:before-upgrade",
  "artifact":{"records":[]}
}`
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	ctx := internalctx.WithLogger(request.Context(), zap.NewNop())
	request = request.WithContext(auth.Authentication.NewContext(ctx, authentication))

	createProtectedHistoryArtifactHandler(service).ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusBadRequest))
	g.Expect(recorder.Body.String()).To(ContainSubstring("unknown field"))
}
