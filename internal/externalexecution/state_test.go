package externalexecution

import (
	"testing"

	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestCallbackStateTransitions(t *testing.T) {
	g := NewWithT(t)
	g.Expect(CanCallbackTransition(types.ExternalExecutionStatusQueued, types.ExternalExecutionStatusRunning)).To(BeTrue())
	g.Expect(CanCallbackTransition(types.ExternalExecutionStatusRunning, types.ExternalExecutionStatusSucceeded)).To(BeTrue())
	g.Expect(CanCallbackTransition(types.ExternalExecutionStatusRunning, types.ExternalExecutionStatusFailed)).To(BeTrue())
	g.Expect(CanCallbackTransition(types.ExternalExecutionStatusRunning, types.ExternalExecutionStatusCanceled)).To(BeTrue())
	g.Expect(CanCallbackTransition(types.ExternalExecutionStatusSucceeded, types.ExternalExecutionStatusRunning)).To(BeFalse())
	g.Expect(CanCallbackTransition(types.ExternalExecutionStatusTimedOut, types.ExternalExecutionStatusSucceeded)).To(BeFalse())
}

func TestObservedStateChecksumIsCanonical(t *testing.T) {
	g := NewWithT(t)
	state := types.ExternalExecutionObservedState{
		Version: "1.4.2", Image: "repo/loyalty-api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Platform: types.DeploymentTargetPlatformLinuxAMD64, Contracts: []string{"z.v1", "a.v1"},
		ConfigReference: "s3://bucket/config?versionId=v42",
		ConfigChecksum:  "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Health:          types.TargetComponentHealthHealthy,
	}
	first, err := ObservedStateChecksum(state)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(first).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))

	state.Contracts = []string{"a.v1", "z.v1"}
	second, err := ObservedStateChecksum(state)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(second).To(Equal(first))
}

func TestObservedStateMustMatchFrozenExecution(t *testing.T) {
	g := NewWithT(t)
	expected := types.ExternalExecutionExpectedState{
		Version: "1.4.2", Image: "repo/loyalty-api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Platform: types.DeploymentTargetPlatformLinuxAMD64, Contracts: []string{"loyalty.v1"},
		ConfigReference: "s3://bucket/config?versionId=v42",
		ConfigChecksum:  "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}
	actual := types.ExternalExecutionObservedState{
		Version: expected.Version, Image: expected.Image, Platform: expected.Platform,
		Contracts: expected.Contracts, ConfigReference: expected.ConfigReference,
		ConfigChecksum: expected.ConfigChecksum, Health: types.TargetComponentHealthHealthy,
	}
	g.Expect(ValidateObservedState(expected, actual)).To(Succeed())

	actual.Platform = types.DeploymentTargetPlatformLinuxARM64
	g.Expect(ValidateObservedState(expected, actual)).To(MatchError(ContainSubstring("platform")))
}

func TestValidateCallbackSequenceBoundsHistory(t *testing.T) {
	g := NewWithT(t)

	g.Expect(ValidateCallbackSequence(MaxEventCount-1, types.ExternalExecutionStatusRunning)).To(Succeed())
	g.Expect(ValidateCallbackSequence(MaxEventCount, types.ExternalExecutionStatusSucceeded)).To(Succeed())
	g.Expect(ValidateCallbackSequence(MaxEventCount, types.ExternalExecutionStatusRunning)).
		To(MatchError(ContainSubstring("terminal")))
	g.Expect(ValidateCallbackSequence(MaxEventCount+1, types.ExternalExecutionStatusSucceeded)).
		To(MatchError(ContainSubstring("limit")))
}

func TestValidateProviderURLRejectsCredentialBearingURLs(t *testing.T) {
	g := NewWithT(t)

	g.Expect(ValidateProviderURL("https://jenkins.example/job/choice-tp/42")).To(Succeed())
	g.Expect(ValidateProviderURL("")).To(Succeed())
	g.Expect(ValidateProviderURL("https://user:password@jenkins.example/job/42")).
		To(MatchError(ContainSubstring("credentials")))
	g.Expect(ValidateProviderURL("https://jenkins.example/job/42?token=secret")).
		To(MatchError(ContainSubstring("query")))
}
