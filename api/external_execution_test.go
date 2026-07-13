package api

import (
	"testing"

	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestExternalExecutionCallbackRequestValidation(t *testing.T) {
	g := NewWithT(t)
	request := ExternalExecutionCallbackRequest{
		Sequence:          1,
		Status:            types.ExternalExecutionStatusSucceeded,
		ProviderReference: "jenkins-choice-tp-42",
		ProviderURL:       "https://jenkins.example/job/choice-tp/42",
		Message:           "loyalty-api is healthy",
		ObservedState: &ExternalExecutionObservedState{
			Version:         "1.4.2",
			Image:           "821392278328.dkr.ecr.ap-southeast-1.amazonaws.com/loyalty-api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Platform:        types.DeploymentTargetPlatformLinuxAMD64,
			Contracts:       []string{"loyalty.v1"},
			ConfigReference: "s3://emlo-backend-configs/choice-tp_dev/1/rmt-loyalty-api/appsettings.Production.json?versionId=v42",
			ConfigChecksum:  "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			Health:          types.TargetComponentHealthHealthy,
		},
	}

	g.Expect(request.Validate()).To(Succeed())

	invalidSequence := request
	invalidSequence.Sequence = 0
	g.Expect(invalidSequence.Validate()).To(MatchError(ContainSubstring("sequence must be greater than 0")))

	invalidStatus := request
	invalidStatus.Status = types.ExternalExecutionStatusTimedOut
	g.Expect(invalidStatus.Validate()).To(MatchError(ContainSubstring("status must be a callback state")))

	missingObservedState := request
	missingObservedState.ObservedState = nil
	g.Expect(missingObservedState.Validate()).To(MatchError(ContainSubstring("observedState is required")))

	unhealthy := request
	unhealthy.ObservedState = &ExternalExecutionObservedState{
		Version: "1.4.2", Image: request.ObservedState.Image,
		Platform: types.DeploymentTargetPlatformLinuxAMD64, ConfigReference: request.ObservedState.ConfigReference,
		ConfigChecksum: request.ObservedState.ConfigChecksum, Health: types.TargetComponentHealthUnhealthy,
	}
	g.Expect(unhealthy.Validate()).To(MatchError(ContainSubstring("health must be HEALTHY")))
}

func TestExternalExecutionFailureCallbackRequiresBoundedMessage(t *testing.T) {
	g := NewWithT(t)
	request := ExternalExecutionCallbackRequest{
		Sequence: 1,
		Status:   types.ExternalExecutionStatusFailed,
	}
	g.Expect(request.Validate()).To(MatchError(ContainSubstring("message is required")))

	request.Message = "deployment command failed"
	g.Expect(request.Validate()).To(Succeed())
}

func TestExternalExecutionCallbackRejectsCredentialBearingProviderURL(t *testing.T) {
	g := NewWithT(t)
	for _, providerURL := range []string{
		"https://user:password@jenkins.example/job/42",
		"https://jenkins.example/job/42?token=secret",
	} {
		request := ExternalExecutionCallbackRequest{
			Sequence: 1, Status: types.ExternalExecutionStatusRunning, ProviderURL: providerURL,
		}
		g.Expect(request.Validate()).To(HaveOccurred())
	}
}
