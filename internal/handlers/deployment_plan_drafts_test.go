package handlers

import (
	"errors"
	"net/http"
	"os"
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	. "github.com/onsi/gomega"
)

func TestDeploymentPlanDraftPublicErrorIsTenantSafe(t *testing.T) {
	g := NewWithT(t)
	status, message := deploymentPlanDraftPublicError(
		apierrors.NewConflict("foreign organization checksum and database detail"),
	)
	g.Expect(status).To(Equal(http.StatusConflict))
	g.Expect(message).To(Equal("deployment plan draft conflicts with current immutable state"))
	g.Expect(message).NotTo(ContainSubstring("foreign organization"))

	status, message = deploymentPlanDraftPublicError(errors.New("postgres credential"))
	g.Expect(status).To(Equal(http.StatusInternalServerError))
	g.Expect(message).To(Equal("deployment plan draft operation failed"))
}

func TestDeploymentPlanDraftRoutesHaveFlagAndMutationGuards(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("deployment_plan_drafts.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)
	g.Expect(text).To(ContainSubstring(
		"middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyOperatorControlPlaneV2)",
	))
	g.Expect(text).To(ContainSubstring(
		"middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyDeploymentPlans)",
	))
	g.Expect(text).To(ContainSubstring("middleware.RequireReadWriteOrAdmin"))
	g.Expect(text).To(ContainSubstring("middleware.BlockSuperAdmin"))
	g.Expect(text).To(ContainSubstring(`Patch("/", updateDeploymentPlanDraftHandler())`))
	g.Expect(text).To(ContainSubstring(`Post("/validate"`))
	g.Expect(text).To(ContainSubstring(`Post("/publish"`))
}
