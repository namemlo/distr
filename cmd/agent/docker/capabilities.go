package main

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/agentclient"
	"github.com/distr-sh/distr/internal/types"
)

const composeDeployActionType = "distr.compose.deploy"
const ociJobActionType = "distr.oci.job"
const fileRenderActionType = "distr.file.render"

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
		{
			ActionType: ociJobActionType,
			Versions:   []string{types.AgentActionVersionV1},
		},
		{
			ActionType: fileRenderActionType,
			Versions:   []string{types.AgentActionVersionV1},
		},
	}
	return report
}
