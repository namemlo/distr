package handlers

import (
	"errors"
	"net/http"
	"os"
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	. "github.com/onsi/gomega"
)

func TestAdapterPublicErrorIsTenantSafe(t *testing.T) {
	g := NewWithT(t)
	status, message := adapterPublicError(apierrors.NewBadRequest("foreign target and credential detail"))
	g.Expect(status).To(Equal(http.StatusBadRequest))
	g.Expect(message).To(Equal("adapter request is invalid"))
	g.Expect(message).NotTo(ContainSubstring("foreign target"))

	status, message = adapterPublicError(errors.New("postgres credential"))
	g.Expect(status).To(Equal(http.StatusInternalServerError))
	g.Expect(message).To(Equal("internal server error"))
}

func TestAdapterRoutesHaveV2AndMutationGuards(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("adapters.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)
	g.Expect(text).To(ContainSubstring(
		"middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyOperatorControlPlaneV2)",
	))
	g.Expect(text).To(ContainSubstring("middleware.RequireReadWriteOrAdmin"))
	g.Expect(text).To(ContainSubstring("middleware.BlockSuperAdmin"))
	g.Expect(text).To(ContainSubstring("AdapterImplementationsRouter"))
	g.Expect(text).To(ContainSubstring("AdapterAssignmentsRouter"))
}
