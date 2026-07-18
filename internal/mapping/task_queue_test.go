package mapping

import (
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestTaskToAPI(t *testing.T) {
	g := NewWithT(t)
	queuedAt := time.Now().UTC()
	task := types.Task{
		ID:                     uuid.New(),
		CreatedAt:              queuedAt,
		UpdatedAt:              queuedAt,
		QueuedAt:               queuedAt,
		OrganizationID:         uuid.New(),
		DeploymentPlanID:       uuid.New(),
		DeploymentPlanTargetID: uuid.New(),
		DeploymentTargetID:     uuid.New(),
		ApplicationID:          uuid.New(),
		ReleaseBundleID:        uuid.New(),
		ChannelID:              uuid.New(),
		EnvironmentID:          uuid.New(),
		Status:                 types.TaskStatusQueued,
		ProtocolVersion:        types.ExecutionProtocolVersionV2,
		QueueOrder:             42,
		Locks: []types.TaskResourceLock{
			{
				ID:                uuid.New(),
				TaskID:            uuid.New(),
				ResourceType:      types.TaskLockResourceDeploymentTarget,
				ResourceKey:       uuid.NewString(),
				ConcurrencyPolicy: types.TaskConcurrencyPolicyQueue,
			},
		},
		StepRuns: []types.StepRun{
			{
				ID:                   uuid.New(),
				TaskID:               uuid.New(),
				DeploymentPlanStepID: uuid.New(),
				StepKey:              "deploy",
				Name:                 "Deploy",
				ActionType:           "distr.http.check",
				Status:               types.StepRunStatusPending,
				SortOrder:            10,
			},
		},
	}

	response := TaskToAPI(task)

	g.Expect(response.ID).To(Equal(task.ID))
	g.Expect(response.DeploymentPlanID).To(Equal(task.DeploymentPlanID))
	g.Expect(response.DeploymentTargetID).To(Equal(task.DeploymentTargetID))
	g.Expect(response.Status).To(Equal(types.TaskStatusQueued))
	g.Expect(response.ProtocolVersion).To(Equal(types.ExecutionProtocolVersionV2))
	g.Expect(response.QueueOrder).To(Equal(int64(42)))
	g.Expect(response.Locks).To(HaveLen(1))
	g.Expect(response.Locks[0].ResourceType).To(Equal(types.TaskLockResourceDeploymentTarget))
	g.Expect(response.Locks[0].ConcurrencyPolicy).To(Equal(types.TaskConcurrencyPolicyQueue))
	g.Expect(response.StepRuns).To(HaveLen(1))
	g.Expect(response.StepRuns[0].StepKey).To(Equal("deploy"))
	g.Expect(response.StepRuns[0].Status).To(Equal(types.StepRunStatusPending))
}
