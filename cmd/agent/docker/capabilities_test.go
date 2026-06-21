package main

import (
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestDockerCapabilityReportAdvertisesComposeDeployAction(t *testing.T) {
	g := NewWithT(t)

	report := dockerCapabilityReport()

	g.Expect(report.Validate()).To(Succeed())
	g.Expect(report.SupportedActions).To(ContainElement(api.AgentActionCapabilityRequest{
		ActionType: composeDeployActionType,
		Versions:   []string{types.AgentActionVersionV1},
	}))
}
