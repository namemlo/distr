package api

import (
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestExecutionV2EventRequestValidation(t *testing.T) {
	g := NewWithT(t)
	request := ExecutionV2EventRequest{
		ExecutorID: "executor-a", ExecutionID: uuid.New(), AttemptNumber: 1, StepKey: "deploy",
		FenceGeneration: 2, EventSequence: 1, Status: types.ExecutionEventStatusRunning,
		PayloadChecksum: "sha256:" + repeatAPIHex("ab"), OccurredAt: time.Now().UTC(),
	}
	g.Expect(request.Validate()).To(Succeed())
	request.EventSequence = 0
	g.Expect(request.Validate()).To(MatchError(ContainSubstring("eventSequence")))
}

func TestExecutionV2RuntimeEvidenceRequestValidationAndConversion(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 8, 30, 6, 7, 8, 987654321, time.UTC)
	request := validExecutionV2RuntimeEvidenceRequest(now)

	g.Expect(request.Validate()).To(Succeed())
	orgID, targetID, attemptID := uuid.New(), uuid.New(), uuid.New()
	input := request.ToTypes(orgID, targetID, attemptID)
	g.Expect(input.OrganizationID).To(Equal(orgID))
	g.Expect(input.DeploymentTargetID).To(Equal(targetID))
	g.Expect(input.AttemptID).To(Equal(attemptID))
	g.Expect(input.SchemaVersion).To(Equal(types.ExecutionRuntimeEvidenceSchemaV2))
	g.Expect(input.ExpectedObservedStateVersion).To(Equal(int64(12)))
	g.Expect(input.PreExecutionServiceConfigChecksum).
		To(Equal(request.PreExecutionServiceConfigChecksum))
	g.Expect(input.ResultServiceConfigChecksum).To(Equal(request.ResultServiceConfigChecksum))
	g.Expect(input.CapturedAt).To(Equal(now.Truncate(time.Microsecond)))

	legacy := validExecutionV2RuntimeEvidenceRequest(now)
	legacy.SchemaVersion = types.ExecutionRuntimeEvidenceSchemaV1
	legacy.PreExecutionServiceConfigChecksum = ""
	legacy.ResultServiceConfigChecksum = ""
	g.Expect(legacy.Validate()).To(Succeed())
	legacy.PreExecutionServiceConfigChecksum = "sha256:" + repeatAPIHex("cd")
	g.Expect(legacy.Validate()).To(MatchError(ContainSubstring("v1 service config checksums")))

	cases := []struct {
		name   string
		mutate func(*ExecutionV2RuntimeEvidenceRequest)
		field  string
	}{
		{"event identity", func(r *ExecutionV2RuntimeEvidenceRequest) { r.EventIdentity = uuid.Nil }, "eventIdentity"},
		{"schema", func(r *ExecutionV2RuntimeEvidenceRequest) { r.SchemaVersion = "v2" }, "schemaVersion"},
		{"executor bound", func(r *ExecutionV2RuntimeEvidenceRequest) { r.ExecutorID = strings.Repeat("x", 129) }, "executorId"},
		{"caller required", func(r *ExecutionV2RuntimeEvidenceRequest) { r.CallerIdentity = " " }, "callerIdentity"},
		{"caller bound", func(r *ExecutionV2RuntimeEvidenceRequest) { r.CallerIdentity = strings.Repeat("x", 513) }, "callerIdentity"},
		{"audience required", func(r *ExecutionV2RuntimeEvidenceRequest) { r.Audience = "" }, "audience"},
		{"state version", func(r *ExecutionV2RuntimeEvidenceRequest) { r.ExpectedObservedStateVersion = 0 }, "expectedObservedStateVersion"},
		{"checksum", func(r *ExecutionV2RuntimeEvidenceRequest) { r.ResultChecksum = "bad" }, "resultChecksum"},
		{"pre service config checksum", func(r *ExecutionV2RuntimeEvidenceRequest) { r.PreExecutionServiceConfigChecksum = "bad" }, "preExecutionServiceConfigChecksum"},
		{"result service config checksum", func(r *ExecutionV2RuntimeEvidenceRequest) { r.ResultServiceConfigChecksum = "bad" }, "resultServiceConfigChecksum"},
		{"platform", func(r *ExecutionV2RuntimeEvidenceRequest) { r.Platform = "jenkins" }, "platform"},
		{"health", func(r *ExecutionV2RuntimeEvidenceRequest) { r.HealthStatus = "OK" }, "healthStatus"},
		{"reference bound", func(r *ExecutionV2RuntimeEvidenceRequest) { r.EvidenceReference = strings.Repeat("x", 2049) }, "evidenceReference"},
		{"captured time", func(r *ExecutionV2RuntimeEvidenceRequest) { r.CapturedAt = time.Time{} }, "capturedAt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := validExecutionV2RuntimeEvidenceRequest(now)
			tc.mutate(&candidate)
			NewWithT(t).Expect(candidate.Validate()).To(MatchError(ContainSubstring(tc.field)))
		})
	}
}

func TestExecutionV2CompletionRequiresRuntimeEvidenceOnlyForSuccess(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 8, 30, 6, 7, 8, 0, time.UTC)
	evidenceID := uuid.New()
	checksum := "sha256:" + repeatAPIHex("ab")

	success := ExecutionV2CompletionRequest{
		ExecutorID: "executor-a", FenceGeneration: 2,
		Status: types.ExecutionAttemptStatusSucceeded, CompletedAt: now,
	}
	g.Expect(success.Validate()).To(MatchError(ContainSubstring("runtime evidence")))
	success.RuntimeEvidenceID = &evidenceID
	success.RuntimeEvidenceChecksum = checksum
	g.Expect(success.Validate()).To(Succeed())
	input := success.ToTypes(uuid.New(), uuid.New(), uuid.New())
	g.Expect(input.RuntimeEvidenceID).To(Equal(evidenceID))
	g.Expect(input.RuntimeEvidenceChecksum).To(Equal(checksum))

	failed := ExecutionV2CompletionRequest{
		ExecutorID: "executor-a", FenceGeneration: 2,
		Status: types.ExecutionAttemptStatusFailed, CompletedAt: now,
		RuntimeEvidenceID: &evidenceID, RuntimeEvidenceChecksum: checksum,
	}
	g.Expect(failed.Validate()).To(MatchError(ContainSubstring("only allowed")))
	failed.RuntimeEvidenceID = nil
	failed.RuntimeEvidenceChecksum = ""
	g.Expect(failed.Validate()).To(Succeed())
}

func TestExecutionV2ClaimRequiresExecutorIdentity(t *testing.T) {
	g := NewWithT(t)
	request := ExecutionV2ClaimRequest{
		AttemptID: uuid.New(), ExecutorID: "executor-a", ExpectedGeneration: 1,
		LeaseSeconds: 30,
	}
	g.Expect(request.Validate()).To(Succeed())
	request.ExecutorID = " "
	g.Expect(request.Validate()).To(MatchError(ContainSubstring("executorId")))
}

func TestCancelStatusAndReconciliationRequests(t *testing.T) {
	g := NewWithT(t)
	cancel := ExecutionCancelRequest{
		IdempotencyKey: "cancel-1", Reason: "operator requested",
	}
	g.Expect(cancel.Validate()).To(Succeed())
	cancel.Reason = ""
	g.Expect(cancel.Validate()).To(MatchError(ContainSubstring("reason")))

	status := ExecutionStatusRequest{
		IdempotencyKey: "status-1", Reason: "callback missing", ExpiresInSeconds: 60,
	}
	g.Expect(status.Validate()).To(Succeed())

	reconciliation := ExecutionReconciliationRequest{
		Evidence: types.SignedReconciliationEvidence{
			Payload:   []byte(`{"outcome":"UNKNOWN"}`),
			Checksum:  "sha256:" + repeatAPIHex("ef"),
			KeyID:     "sha256:" + repeatAPIHex("ab"),
			Signature: "signed",
		},
	}
	g.Expect(reconciliation.Validate()).To(Succeed())
	reconciliation.Evidence.Checksum = "not-a-checksum"
	g.Expect(reconciliation.Validate()).To(MatchError(ContainSubstring("signed reconciliation")))
}

func TestReconciliationEvidenceConversionPreservesAttemptBinding(t *testing.T) {
	g := NewWithT(t)
	evidence := types.ReconciliationEvidence{
		OrganizationID: uuid.New(), ExecutionID: uuid.New(), AttemptID: uuid.New(),
		StatusQueryID: uuid.New(), EventIdentity: uuid.New(),
		Outcome: types.ReconciliationOutcomeUnknown,
	}
	input := ReconciliationEvidenceToTypes(evidence, types.SignedReconciliationEvidence{})
	g.Expect(input.AttemptID).To(Equal(evidence.AttemptID))
}

func TestExecutionStatusRequestPreservesRequestedTTL(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 7, 18, 6, 0, 0, 0, time.UTC)
	request := ExecutionStatusRequest{
		IdempotencyKey: "status-1", Reason: "callback missing", ExpiresInSeconds: 75,
	}
	input := request.ToTypes(uuid.New(), uuid.New(), uuid.New(), now)
	g.Expect(input.RequestedTTLSeconds).To(Equal(75))
}

func validExecutionV2RuntimeEvidenceRequest(capturedAt time.Time) ExecutionV2RuntimeEvidenceRequest {
	checksum := "sha256:" + repeatAPIHex("ab")
	return ExecutionV2RuntimeEvidenceRequest{
		SchemaVersion: types.ExecutionRuntimeEvidenceSchemaV2,
		EventIdentity: uuid.New(), IntentChecksum: checksum, ExecutorID: "executor-a",
		CallerIdentity: "target:choice-tp-dev", Audience: "adapter:compose@2",
		FenceGeneration: 3, ExpectedObservedStateVersion: 12,
		ExpectedObservedStateChecksum: checksum, PreExecutionImageDigest: checksum,
		PreExecutionConfigChecksum: checksum, ResultImageDigest: checksum,
		PreExecutionServiceConfigChecksum: checksum,
		ResultConfigChecksum:              checksum, ResultServiceConfigChecksum: checksum,
		Platform:       types.DeploymentTargetPlatformLinuxAMD64,
		HealthStatus:   types.TargetComponentHealthHealthy,
		ResultChecksum: checksum, EvidenceReference: "jenkins://job/choice-tp-dev/42",
		EvidenceChecksum: checksum, CapturedAt: capturedAt,
	}
}

func repeatAPIHex(pair string) string {
	result := ""
	for range 32 {
		result += pair
	}
	return result
}
