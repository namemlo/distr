package agentmanifest

import (
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

func TestGetTemplateDataIncludesAgentCapabilitiesEndpoint(t *testing.T) {
	targetID := uuid.New()
	agentVersionID := uuid.New()
	appDomain := "https://hub.example.com/root"
	registryDomain := "registry.example.com"

	data, err := getTemplateData(
		types.DeploymentTargetFull{
			DeploymentTarget: types.DeploymentTarget{
				ID:   targetID,
				Type: types.DeploymentTypeDocker,
			},
			AgentVersion: types.AgentVersion{
				ID:                  agentVersionID,
				Name:                "snapshot",
				ComposeFileRevision: "v1",
			},
		},
		types.Organization{
			ID:             uuid.New(),
			AppDomain:      &appDomain,
			RegistryDomain: &registryDomain,
		},
		nil,
	)
	if err != nil {
		t.Fatalf("expected template data: %v", err)
	}
	if got, want := data["loginEndpoint"], "https://hub.example.com/root/api/v1/agent/login"; got != want {
		t.Fatalf("expected login endpoint %q, got %q", want, got)
	}
	if got, want := data["capabilitiesEndpoint"],
		"https://hub.example.com/root/api/v1/agents/"+targetID.String()+"/capabilities"; got != want {
		t.Fatalf("expected capabilities endpoint %q, got %q", want, got)
	}
}
