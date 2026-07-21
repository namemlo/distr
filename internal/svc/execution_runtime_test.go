package svc

import (
	"testing"

	"github.com/distr-sh/distr/internal/featureflags"
	. "github.com/onsi/gomega"
)

func TestNewExecutionRuntimeDependenciesBuildsProductionServices(t *testing.T) {
	g := NewWithT(t)
	dependencies, err := newExecutionRuntimeDependencies(
		[]byte("0123456789abcdef0123456789abcdef"),
		featureflags.NewRegistry([]featureflags.Key{
			featureflags.KeyOperatorControlPlaneV2, featureflags.KeyExecutorProtocolV2,
		}),
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(dependencies.ProtocolDispatcher).NotTo(BeNil())
	g.Expect(dependencies.ReconciliationEvidenceVerifier).NotTo(BeNil())
	g.Expect(dependencies.ReconciliationObserverGate).NotTo(BeNil())
	g.Expect(dependencies.CampaignControlCoordinator).NotTo(BeNil())
}

func TestNewExecutionRuntimeDependenciesRejectsMissingRootSecret(t *testing.T) {
	_, err := newExecutionRuntimeDependencies(nil, featureflags.NewRegistry(nil))
	NewWithT(t).Expect(err).To(MatchError(ContainSubstring("root secret")))
}
