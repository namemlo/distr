package main

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/agentclient"
	"github.com/distr-sh/distr/internal/types"
)

const composeDeployActionType = "distr.compose.deploy"

func dockerCapabilityReport() api.AgentCapabilitiesRequest {
	report := agentclient.DefaultCapabilityReport(
		"docker",
		[]string{"docker", "docker-compose"},
		[]string{"docker-compose"},
	)
	report.SupportedActions = []api.AgentActionCapabilityRequest{
		{
			ActionType: composeDeployActionType,
			Versions:   []string{types.AgentActionVersionV1},
		},
	}
	return report
}
