package api

import (
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestExecutionV2EventRequestValidation(t *testing.T) {
	g := NewWithT(t)
	request := ExecutionV2EventRequest{
		ExecutionID: uuid.New(), AttemptNumber: 1, StepKey: "deploy",
		FenceGeneration: 2, EventSequence: 1, Status: types.ExecutionEventStatusRunning,
		PayloadChecksum: "sha256:" + repeatAPIHex("ab"), OccurredAt: time.Now().UTC(),
	}
	g.Expect(request.Validate()).To(Succeed())
	request.EventSequence = 0
	g.Expect(request.Validate()).To(MatchError(ContainSubstring("eventSequence")))
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

func repeatAPIHex(pair string) string {
	result := ""
	for range 32 {
		result += pair
	}
	return result
}
