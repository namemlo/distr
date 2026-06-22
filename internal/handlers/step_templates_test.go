package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/middleware"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

func TestStepTemplateImportRequestValidation(t *testing.T) {
	tests := []struct {
		name    string
		request api.ImportStepTemplateRequest
	}{
		{name: "missing source", request: api.ImportStepTemplateRequest{Name: "HTTP", Version: "1.0.0", ActionType: "distr.http.check", ExecutionLocation: "hub"}},
		{name: "missing name", request: api.ImportStepTemplateRequest{SourceType: "builtin", SourceRef: "builtin/http", Version: "1.0.0", ActionType: "distr.http.check", ExecutionLocation: "hub"}},
		{name: "missing version", request: api.ImportStepTemplateRequest{SourceType: "builtin", SourceRef: "builtin/http", Name: "HTTP", ActionType: "distr.http.check", ExecutionLocation: "hub"}},
		{name: "invalid source type", request: api.ImportStepTemplateRequest{SourceType: "bad", SourceRef: "builtin/http", Name: "HTTP", Version: "1.0.0", ActionType: "distr.http.check", ExecutionLocation: "hub"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			err := tt.request.Validate()

			g.Expect(err).To(HaveOccurred())
		})
	}
}

func TestStepTemplateResponses(t *testing.T) {
	g := NewWithT(t)
	id := uuid.New()
	versionID := uuid.New()

	responses := stepTemplateResponses([]types.StepTemplate{{
		ID:         id,
		SourceType: types.StepTemplateSourceBuiltin,
		SourceRef:  "builtin/http",
		Name:       "HTTP",
		Versions: []types.StepTemplateVersion{{
			ID:          versionID,
			Version:     "1.0.0",
			ActionType:  "distr.http.check",
			InputSchema: map[string]any{"type": "object"},
		}},
	}})

	g.Expect(responses).To(HaveLen(1))
	g.Expect(responses[0].ID).To(Equal(id))
	g.Expect(responses[0].SourceType).To(Equal("builtin"))
	g.Expect(responses[0].Versions[0].ID).To(Equal(versionID))
	g.Expect(responses[0].Versions[0].InputSchema).To(HaveKeyWithValue("type", "object"))
}

func TestImportStepTemplateHandlerRejectsInvalidPayloadBeforeDatabaseAccess(t *testing.T) {
	g := NewWithT(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/step-templates/import", strings.NewReader(`{"sourceType":"bad"}`))
	request = request.WithContext(authenticatedChannelHandlerContext(request.Context(), uuid.New()))

	importStepTemplateHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusBadRequest))
}

func TestHandleStepTemplateWriteError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "duplicate install", err: apierrors.ErrAlreadyExists, want: http.StatusBadRequest},
		{name: "invalid import", err: apierrors.ErrBadRequest, want: http.StatusBadRequest},
		{name: "missing reference", err: apierrors.ErrNotFound, want: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/step-templates/import", nil)

			handleStepTemplateWriteError(recorder, request, zap.NewNop(), "test", tt.err)

			g.Expect(recorder.Code).To(Equal(tt.want))
		})
	}
}

func TestStepTemplatesFeatureFlagMiddlewareRejectsDisabledAPI(t *testing.T) {
	g := NewWithT(t)
	called := false
	handler := middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyStepTemplates)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/step-templates", nil)

	handler.ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusForbidden))
	g.Expect(called).To(BeFalse())
}
