package executionruntime

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/distr-sh/distr/internal/executionprotocol"
	"github.com/distr-sh/distr/internal/executionworker"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

type productionRuntimeRepositoryStub struct{}

func (productionRuntimeRepositoryStub) EvaluateExecutionV2Admission(
	context.Context, executionworker.AdmissionRequest,
) (executionworker.AdmissionDecision, error) {
	return executionworker.AdmissionDecision{}, nil
}

func (productionRuntimeRepositoryStub) LoadFrozenAttemptInputs(
	context.Context, executionworker.CreateAttemptRequest,
) (executionworker.FrozenAttemptInputs, error) {
	return executionworker.FrozenAttemptInputs{}, nil
}

type productionCampaignBridgeStub struct{}

func (productionCampaignBridgeStub) CancelCampaignExecution(context.Context, uuid.UUID) error {
	return nil
}

func (productionCampaignBridgeStub) RetryCampaignExecution(
	context.Context, uuid.UUID, types.RetryDisposition,
) error {
	return nil
}

func TestNewProductionDependenciesBindsEveryRuntimeService(t *testing.T) {
	g := NewWithT(t)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	g.Expect(err).NotTo(HaveOccurred())
	signer, err := executionprotocol.NewEd25519IntentSigner(
		executionprotocol.PublicKeyFingerprint(publicKey), privateKey,
	)
	g.Expect(err).NotTo(HaveOccurred())

	dependencies, err := NewProductionDependencies(ProductionConfig{
		Flags: featureflags.NewRegistry([]featureflags.Key{
			featureflags.KeyOperatorControlPlaneV2, featureflags.KeyExecutorProtocolV2,
		}),
		Signer: signer, Repository: productionRuntimeRepositoryStub{},
		CampaignBridge: productionCampaignBridgeStub{},
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(dependencies.ProtocolDispatcher).NotTo(BeNil())
	g.Expect(dependencies.ReconciliationEvidenceVerifier).NotTo(BeNil())
	g.Expect(dependencies.ReconciliationObserverGate).NotTo(BeNil())
	g.Expect(dependencies.CampaignControlCoordinator).NotTo(BeNil())
}

func TestTaskCampaignBridgeValidatesCancelScopeAndDispatchesExplicitRetry(t *testing.T) {
	g := NewWithT(t)
	orgID, executionID := uuid.New(), uuid.New()
	task := types.Task{ID: executionID, OrganizationID: orgID}
	loaded := 0
	retried := false
	bridge := NewTaskCampaignControlBridge(
		func(context.Context) (uuid.UUID, error) { return orgID, nil },
		func(_ context.Context, id, requestedOrgID uuid.UUID) (*types.Task, error) {
			loaded++
			g.Expect(id).To(Equal(executionID))
			g.Expect(requestedOrgID).To(Equal(orgID))
			return &task, nil
		},
		func(_ context.Context, value types.Task) error {
			retried = true
			g.Expect(value).To(Equal(task))
			return nil
		},
	)
	g.Expect(bridge.CancelCampaignExecution(context.Background(), executionID)).To(Succeed())
	g.Expect(bridge.RetryCampaignExecution(
		context.Background(), executionID, types.RetryDispositionAllowed,
	)).To(Succeed())
	g.Expect(loaded).To(Equal(2))
	g.Expect(retried).To(BeTrue())
}
