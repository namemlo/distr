package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	. "github.com/onsi/gomega"
)

func TestExecutionV2ErrorMapping(t *testing.T) {
	g := NewWithT(t)
	cases := []struct {
		err  error
		code int
	}{
		{apierrors.NewBadRequest("bad"), http.StatusBadRequest},
		{apierrors.NewConflict("conflict"), http.StatusConflict},
		{apierrors.ErrNotFound, http.StatusNotFound},
		{errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		recorder := httptest.NewRecorder()
		respondExecutionV2Error(recorder, tc.err)
		g.Expect(recorder.Code).To(Equal(tc.code))
	}
}
