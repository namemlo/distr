package api

import (
	"testing"

	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestTransitionTaskStateRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		request TransitionTaskStateRequest
		wantErr string
	}{
		{
			name:    "accepts running status",
			request: TransitionTaskStateRequest{Status: types.TaskStatusRunning},
		},
		{
			name:    "missing status",
			request: TransitionTaskStateRequest{},
			wantErr: "status is required",
		},
		{
			name:    "invalid status",
			request: TransitionTaskStateRequest{Status: types.TaskStatus("PAUSED")},
			wantErr: "status is invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			err := tt.request.Validate()

			if tt.wantErr == "" {
				g.Expect(err).NotTo(HaveOccurred())
			} else {
				g.Expect(err).To(MatchError(ContainSubstring(tt.wantErr)))
			}
		})
	}
}

func TestCreateTasksForDeploymentPlanRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		request CreateTasksForDeploymentPlanRequest
		wantErr string
	}{
		{
			name: "accepts empty request defaults",
		},
		{
			name: "accepts explicit policy and lock resources",
			request: CreateTasksForDeploymentPlanRequest{
				ConcurrencyPolicy: types.TaskConcurrencyPolicyRejectNew,
				LockResources: []TaskLockResourceRequest{
					{
						ResourceType:      types.TaskLockResourceCustom,
						ResourceKey:       " shared-db ",
						ConcurrencyPolicy: types.TaskConcurrencyPolicyQueue,
					},
				},
			},
		},
		{
			name: "invalid concurrency policy",
			request: CreateTasksForDeploymentPlanRequest{
				ConcurrencyPolicy: types.TaskConcurrencyPolicy("BOUNCE"),
			},
			wantErr: "concurrencyPolicy is invalid",
		},
		{
			name: "invalid lock resource type",
			request: CreateTasksForDeploymentPlanRequest{
				LockResources: []TaskLockResourceRequest{
					{
						ResourceType: types.TaskLockResourceType("planet"),
						ResourceKey:  "earth",
					},
				},
			},
			wantErr: "lockResources[0].resourceType is invalid",
		},
		{
			name: "empty lock resource key",
			request: CreateTasksForDeploymentPlanRequest{
				LockResources: []TaskLockResourceRequest{
					{
						ResourceType: types.TaskLockResourceCustom,
						ResourceKey:  "   ",
					},
				},
			},
			wantErr: "lockResources[0].resourceKey is required",
		},
		{
			name: "invalid lock resource policy",
			request: CreateTasksForDeploymentPlanRequest{
				LockResources: []TaskLockResourceRequest{
					{
						ResourceType:      types.TaskLockResourceCustom,
						ResourceKey:       "shared-db",
						ConcurrencyPolicy: types.TaskConcurrencyPolicy("MAYBE"),
					},
				},
			},
			wantErr: "lockResources[0].concurrencyPolicy is invalid",
		},
		{
			name: "duplicate trimmed lock resource",
			request: CreateTasksForDeploymentPlanRequest{
				LockResources: []TaskLockResourceRequest{
					{
						ResourceType: types.TaskLockResourceCustom,
						ResourceKey:  " shared-db",
					},
					{
						ResourceType: types.TaskLockResourceCustom,
						ResourceKey:  "shared-db ",
					},
				},
			},
			wantErr: "lockResources contains duplicate resource",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			err := tt.request.Validate()

			if tt.wantErr == "" {
				g.Expect(err).NotTo(HaveOccurred())
			} else {
				g.Expect(err).To(MatchError(ContainSubstring(tt.wantErr)))
			}
		})
	}
}
