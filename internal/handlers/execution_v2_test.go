package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestImportReconciliationStatusWithDispatchDispatchesCommittedTask(t *testing.T) {
	g := NewWithT(t)
	committed := false
	dispatched := false
	task := &types.Task{}

	err := importReconciliationStatusWithDispatch(
		context.Background(),
		types.ReconciliationStatusInput{},
		func(context.Context, types.ReconciliationStatusInput) (*types.Task, error) {
			committed = true
			return task, nil
		},
		func(_ context.Context, gotTask types.Task) error {
			g.Expect(committed).To(BeTrue())
			g.Expect(gotTask).To(Equal(*task))
			dispatched = true
			return nil
		},
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(dispatched).To(BeTrue())
}

func TestImportReconciliationStatusWithDispatchDoesNotDispatchWithoutTask(t *testing.T) {
	g := NewWithT(t)
	dispatchErr := errors.New("dispatch must not run")

	err := importReconciliationStatusWithDispatch(
		context.Background(),
		types.ReconciliationStatusInput{},
		func(context.Context, types.ReconciliationStatusInput) (*types.Task, error) {
			return nil, nil
		},
		func(context.Context, types.Task) error { return dispatchErr },
	)

	g.Expect(err).NotTo(HaveOccurred())
}

func TestTerminalExecutionEventHandlerDoesNotDispatchOrAdvanceDesiredState(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("execution_v2.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)
	_, afterStart, found := strings.Cut(text, "func recordExecutionV2EventHandler()")
	g.Expect(found).To(BeTrue())
	body, _, found := strings.Cut(afterStart, "func completeExecutionV2Handler()")
	g.Expect(found).To(BeTrue())
	g.Expect(body).To(ContainSubstring("db.RecordExecutionEvent"))
	g.Expect(body).NotTo(ContainSubstring("DispatchReadyTaskSteps"))
	g.Expect(body).NotTo(ContainSubstring("Desired"))
}

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
