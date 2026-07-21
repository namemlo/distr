package executionruntime

import (
	"context"
	"testing"

	"github.com/distr-sh/distr/internal/executionprotocol"
	"github.com/distr-sh/distr/internal/executionworker"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

type runtimeAdmissionGate struct{}

func (runtimeAdmissionGate) EvaluateExecutionV2Admission(
	context.Context, executionworker.AdmissionRequest,
) (executionworker.AdmissionDecision, error) {
	return executionworker.AdmissionDecision{
		OperatorFlag: true, ExecutorFlag: true, ScopedEnrollment: true,
		PlanApproved: true, PlanAdmitted: true, AdapterPreflight: true,
	}, nil
}

type runtimeAttemptCreator struct{ calls int }

func (c *runtimeAttemptCreator) CreateExecutionAttempt(
	_ context.Context, request executionworker.CreateAttemptRequest,
) (*types.ExecutionAttempt, error) {
	c.calls++
	return &types.ExecutionAttempt{ID: uuid.New(), OrganizationID: request.OrganizationID}, nil
}

type runtimeEvidenceVerifier struct{ evidence types.ReconciliationEvidence }

func (v runtimeEvidenceVerifier) VerifyReconciliationEvidence(
	context.Context, types.SignedReconciliationEvidence,
) (types.ReconciliationEvidence, error) {
	return v.evidence, nil
}

type runtimeObserverGate struct{}

func (runtimeObserverGate) AuthorizeReconciliationObserver(
	context.Context, types.ReconciliationEvidence,
) error {
	return nil
}

type runtimeCampaignBridge struct{ canceled uuid.UUID }

func (b *runtimeCampaignBridge) CancelCampaignExecution(_ context.Context, id uuid.UUID) error {
	b.canceled = id
	return nil
}

func (*runtimeCampaignBridge) RetryCampaignExecution(
	context.Context, uuid.UUID, types.RetryDisposition,
) error {
	return nil
}

func TestDependenciesInjectEveryExecutionV2RuntimeSeam(t *testing.T) {
	g := NewWithT(t)
	creator := &runtimeAttemptCreator{}
	dispatcher := executionworker.NewProtocolDispatcher(nil, executionworker.NewDispatcher(
		runtimeAdmissionGate{}, creator,
	))
	evidence := types.ReconciliationEvidence{ExecutionID: uuid.New()}
	bridge := &runtimeCampaignBridge{}
	ctx := Dependencies{
		ProtocolDispatcher:             dispatcher,
		ReconciliationEvidenceVerifier: runtimeEvidenceVerifier{evidence: evidence},
		ReconciliationObserverGate:     runtimeObserverGate{},
		CampaignControlCoordinator:     executionprotocol.NewCampaignControlCoordinator(bridge),
	}.Inject(context.Background())

	task := types.Task{
		ID: uuid.New(), OrganizationID: uuid.New(), DeploymentTargetID: uuid.New(),
		EnvironmentID: uuid.New(), DeploymentPlanID: uuid.New(),
		ProtocolVersion: types.ExecutionProtocolVersionV2,
		StepRuns:        []types.StepRun{{ID: uuid.New(), StepKey: "deploy", Status: types.StepRunStatusPending}},
	}
	g.Expect(executionworker.DispatchCreatedTasks(ctx, []types.Task{task})).To(Succeed())
	g.Expect(creator.calls).To(Equal(1))
	verified, err := executionprotocol.VerifyImportedReconciliationEvidence(
		ctx, types.SignedReconciliationEvidence{},
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(verified).To(Equal(evidence))
	g.Expect(executionprotocol.BridgeCampaignCancelIfConfigured(ctx, evidence.ExecutionID)).To(Succeed())
	g.Expect(bridge.canceled).To(Equal(evidence.ExecutionID))
}
