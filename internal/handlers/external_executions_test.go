package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	. "github.com/onsi/gomega"
)

func TestRespondExternalExecutionErrorMapsPublicStatuses(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "not found", err: apierrors.ErrNotFound, want: http.StatusNotFound},
		{name: "bad request", err: apierrors.NewBadRequest("invalid"), want: http.StatusBadRequest},
		{name: "conflict", err: apierrors.NewConflict("stale"), want: http.StatusConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			recorder := httptest.NewRecorder()
			respondExternalExecutionError(recorder, tt.err)
			g.Expect(recorder.Code).To(Equal(tt.want))
		})
	}
}

func TestRespondExternalExecutionErrorHidesInternalDetails(t *testing.T) {
	g := NewWithT(t)
	recorder := httptest.NewRecorder()
	respondExternalExecutionError(recorder, errors.New("database password=do-not-leak"))
	g.Expect(recorder.Code).To(Equal(http.StatusInternalServerError))
	g.Expect(recorder.Body.String()).NotTo(ContainSubstring("do-not-leak"))
}
