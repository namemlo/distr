package executionprotocol

import (
	"testing"

	"github.com/distr-sh/distr/internal/externalexecution"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestProtocolV1RegressionWithExecutionV2FlagsDisabled(t *testing.T) {
	g := NewWithT(t)
	flags := featureflags.NewRegistry(nil)
	g.Expect(flags.IsEnabled(featureflags.KeyOperatorControlPlaneV2)).To(BeFalse())
	g.Expect(flags.IsEnabled(featureflags.KeyExecutorProtocolV2)).To(BeFalse())

	statuses := []types.ExternalExecutionStatus{
		types.ExternalExecutionStatusQueued,
		types.ExternalExecutionStatusRunning,
		types.ExternalExecutionStatusSucceeded,
		types.ExternalExecutionStatusFailed,
		types.ExternalExecutionStatusCanceled,
		types.ExternalExecutionStatusTimedOut,
	}
	expected := map[[2]types.ExternalExecutionStatus]bool{
		{types.ExternalExecutionStatusQueued, types.ExternalExecutionStatusRunning}:    true,
		{types.ExternalExecutionStatusQueued, types.ExternalExecutionStatusSucceeded}:  true,
		{types.ExternalExecutionStatusQueued, types.ExternalExecutionStatusFailed}:     true,
		{types.ExternalExecutionStatusQueued, types.ExternalExecutionStatusCanceled}:   true,
		{types.ExternalExecutionStatusRunning, types.ExternalExecutionStatusRunning}:   true,
		{types.ExternalExecutionStatusRunning, types.ExternalExecutionStatusSucceeded}: true,
		{types.ExternalExecutionStatusRunning, types.ExternalExecutionStatusFailed}:    true,
		{types.ExternalExecutionStatusRunning, types.ExternalExecutionStatusCanceled}:  true,
	}
	for _, from := range statuses {
		for _, to := range statuses {
			g.Expect(externalexecution.CanCallbackTransition(from, to)).
				To(Equal(expected[[2]types.ExternalExecutionStatus{from, to}]),
					"v1 transition changed: %s -> %s", from, to)
		}
	}
	g.Expect(externalexecution.ValidateCallbackSequence(
		externalexecution.MaxEventCount, types.ExternalExecutionStatusSucceeded,
	)).To(Succeed())
	g.Expect(externalexecution.ValidateCallbackSequence(
		externalexecution.MaxEventCount, types.ExternalExecutionStatusRunning,
	)).To(MatchError(ContainSubstring("terminal")))
}
