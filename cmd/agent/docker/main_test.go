package main

import (
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

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
