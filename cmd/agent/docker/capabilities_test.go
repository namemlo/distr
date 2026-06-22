package main

import (
	"strings"
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestDockerCapabilityReportAdvertisesTypedActions(t *testing.T) {
	g := NewWithT(t)

	report := dockerCapabilityReport()

	g.Expect(report.Validate()).To(Succeed())
	g.Expect(report.SupportedActions).To(ContainElement(api.AgentActionCapabilityRequest{
		ActionType: composeDeployActionType,
		Versions:   []string{types.AgentActionVersionV1},
	}))
	g.Expect(report.SupportedActions).To(ContainElement(api.AgentActionCapabilityRequest{
		ActionType: ociJobActionType,
		Versions:   []string{types.AgentActionVersionV1},
	}))
	g.Expect(report.SupportedActions).To(ContainElement(api.AgentActionCapabilityRequest{
		ActionType: fileRenderActionType,
		Versions:   []string{types.AgentActionVersionV1},
	}))
	g.Expect(report.SupportedActions).To(ContainElement(api.AgentActionCapabilityRequest{
		ActionType: webhookActionType,
		Versions:   []string{types.AgentActionVersionV1},
	}))
}

func TestDockerCapabilityReportAdvertisesWebhookV1(t *testing.T) {
	g := NewWithT(t)

	report := dockerCapabilityReport()

	g.Expect(dockerActionCapabilityIDs(report.SupportedActions)).To(ContainElement("distr.webhook.v1"))
}

func dockerActionCapabilityIDs(actions []api.AgentActionCapabilityRequest) []string {
	values := []string{}
	for _, action := range actions {
		for _, version := range action.Versions {
			values = append(values, action.ActionType+".v"+strings.TrimPrefix(version, "v"))
		}
	}
	return values
}
