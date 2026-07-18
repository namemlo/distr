package handlers

import (
	"net/http"
	"os"
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	. "github.com/onsi/gomega"
)

func TestReconciliationRoutesRequireOperatorFlagAndMutationAuthority(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("reconciliation.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)
	g.Expect(text).To(ContainSubstring("DriftCasesRouter"))
	g.Expect(text).To(ContainSubstring("ReconciliationActionsRouter"))
	g.Expect(text).To(ContainSubstring("featureflags.KeyOperatorControlPlaneV2"))
	g.Expect(text).To(ContainSubstring("middleware.RequireReadWriteOrAdmin"))
	g.Expect(text).To(ContainSubstring("middleware.BlockSuperAdmin"))
	g.Expect(text).To(ContainSubstring(`Post("/resolve"`))
}

func TestReconciliationPublicErrorIsTenantSafe(t *testing.T) {
	g := NewWithT(t)
	status, message := reconciliationPublicError(
		apierrors.NewConflict("foreign organization active revision"),
	)
	g.Expect(status).To(Equal(http.StatusConflict))
	g.Expect(message).To(Equal("reconciliation conflicts with current case state"))
	g.Expect(message).NotTo(ContainSubstring("foreign"))
}
