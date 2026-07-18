package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	. "github.com/onsi/gomega"
)

func TestProductReleasePublicErrorIsTenantSafe(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		want       string
	}{
		{name: "not found", err: apierrors.ErrNotFound, wantStatus: http.StatusNotFound, want: "product release not found"},
		{
			name: "bad request", err: apierrors.NewBadRequest("foreign child 72af"),
			wantStatus: http.StatusBadRequest, want: "product release request is invalid",
		},
		{
			name: "conflict", err: apierrors.NewConflict("checksum secret"),
			wantStatus: http.StatusConflict, want: "product release conflicts with immutable state",
		},
		{
			name: "unknown", err: errors.New("postgres host and credential detail"),
			wantStatus: http.StatusInternalServerError, want: "internal server error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			status, message := productReleasePublicError(tt.err)
			g.Expect(status).To(Equal(tt.wantStatus))
			g.Expect(message).To(Equal(tt.want))
			g.Expect(message).NotTo(ContainSubstring("foreign child"))
			g.Expect(message).NotTo(ContainSubstring("credential"))
		})
	}
}

func TestStrictProductReleaseBodyRejectsUnknownFields(t *testing.T) {
	g := NewWithT(t)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/product-releases",
		strings.NewReader(`{"schema":"distr.product-release/v1","targetId":"forbidden"}`),
	)
	response := httptest.NewRecorder()
	_, err := strictProductReleaseBody(response, request)
	g.Expect(err).To(MatchError(ContainSubstring("unknown field")))
	g.Expect(response.Code).To(Equal(http.StatusBadRequest))
}

func TestProductReleaseRoutesHaveV2AndMutationGuards(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("product_releases.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)
	g.Expect(text).To(ContainSubstring(
		"middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyOperatorControlPlaneV2)",
	))
	g.Expect(text).To(ContainSubstring("middleware.RequireReadWriteOrAdmin"))
	g.Expect(text).To(ContainSubstring("middleware.BlockSuperAdmin"))
	g.Expect(text).To(ContainSubstring(`Post("/publish"`))
	g.Expect(text).To(ContainSubstring(`Post("/", createProductReleaseHandler())`))
}
