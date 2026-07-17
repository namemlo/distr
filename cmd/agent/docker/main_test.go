package main

import (
	"os"
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestInitializeAgentClientRejectsMissingTargetID(t *testing.T) {
	targetID, targetIDSet := os.LookupEnv("DISTR_TARGET_ID")
	if err := os.Unsetenv("DISTR_TARGET_ID"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if targetIDSet {
			if err := os.Setenv("DISTR_TARGET_ID", targetID); err != nil {
				t.Error(err)
			}
		} else if err := os.Unsetenv("DISTR_TARGET_ID"); err != nil {
			t.Error(err)
		}
	})

	g := NewWithT(t)
	g.Expect(initializeAgentClient()).To(MatchError("missing environment variable: DISTR_TARGET_ID"))
}

func TestShouldCleanupDeploymentSkipsTaskManagedDeployment(t *testing.T) {
	g := NewWithT(t)
	legacyID := uuid.New()
	taskID := uuid.New()

	resource := api.AgentResource{
		Deployments: []api.AgentDeployment{
			{
				ID:         legacyID,
				DockerType: ptr(types.DockerTypeCompose),
			},
		},
	}

	g.Expect(shouldCleanupDeployment(resource, AgentDeployment{
		ID:     legacyID,
		Source: AgentDeploymentSourceLegacy,
	})).To(BeFalse())
	g.Expect(shouldCleanupDeployment(resource, AgentDeployment{
		ID:     uuid.New(),
		Source: AgentDeploymentSourceLegacy,
	})).To(BeTrue())
	g.Expect(shouldCleanupDeployment(resource, AgentDeployment{
		ID:     taskID,
		Source: AgentDeploymentSourceTask,
	})).To(BeFalse())
}

func ptr[T any](value T) *T {
	return &value
}
