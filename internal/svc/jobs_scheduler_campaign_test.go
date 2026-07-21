package svc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/jobs"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

type durationJobRegistrarFake struct {
	duration time.Duration
	job      jobs.Job
	calls    int
}

func (fake *durationJobRegistrarFake) RegisterDurationJob(duration time.Duration, job jobs.Job) error {
	fake.duration = duration
	fake.job = job
	fake.calls++
	return nil
}

type campaignSchedulerTickerFake struct {
	runIDs        []uuid.UUID
	injected      bool
	failureForRun uuid.UUID
}

func (fake *campaignSchedulerTickerFake) Tick(
	ctx context.Context,
	runID uuid.UUID,
	_ time.Time,
) (types.CampaignSchedulerResult, error) {
	value, _ := ctx.Value(campaignSchedulerInjectionTestKey{}).(bool)
	fake.injected = fake.injected || value
	fake.runIDs = append(fake.runIDs, runID)
	if runID == fake.failureForRun {
		return types.CampaignSchedulerResult{}, errors.New("tick failed")
	}
	return types.CampaignSchedulerResult{LeaseAcquired: true}, nil
}

type campaignSchedulerInjectionTestKey struct{}

func TestDeploymentCampaignSchedulerRegistrationRequiresBothControlPlaneFlags(t *testing.T) {
	tests := []struct {
		name       string
		flags      []featureflags.Key
		registered bool
	}{
		{name: "disabled"},
		{name: "operator only", flags: []featureflags.Key{featureflags.KeyOperatorControlPlaneV2}},
		{name: "executor only", flags: []featureflags.Key{featureflags.KeyExecutorProtocolV2}},
		{
			name: "both",
			flags: []featureflags.Key{
				featureflags.KeyOperatorControlPlaneV2,
				featureflags.KeyExecutorProtocolV2,
			},
			registered: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registrar := &durationJobRegistrarFake{}
			err := registerDeploymentCampaignScheduler(
				registrar,
				featureflags.NewRegistry(test.flags),
				campaignSchedulerJobDependencies{},
			)
			if err != nil {
				t.Fatalf("register campaign scheduler: %v", err)
			}
			wantCalls := 0
			if test.registered {
				wantCalls = 1
			}
			if registrar.calls != wantCalls {
				t.Fatalf("registration calls = %d, want %d", registrar.calls, wantCalls)
			}
			if test.registered && registrar.duration != deploymentCampaignSchedulerInterval {
				t.Fatalf("duration = %s, want %s", registrar.duration, deploymentCampaignSchedulerInterval)
			}
		})
	}
}

func TestDeploymentCampaignSchedulerJobInjectsRuntimeAndProcessesStableBoundedBatch(t *testing.T) {
	firstRunID := uuid.New()
	secondRunID := uuid.New()
	workerID := "campaign-worker-test"
	ticker := &campaignSchedulerTickerFake{failureForRun: firstRunID}
	registrar := &durationJobRegistrarFake{}
	listCalls := 0
	err := registerDeploymentCampaignScheduler(
		registrar,
		featureflags.NewRegistry([]featureflags.Key{
			featureflags.KeyOperatorControlPlaneV2,
			featureflags.KeyExecutorProtocolV2,
		}),
		campaignSchedulerJobDependencies{
			WorkerID: workerID,
			ListRunnableCampaignRunIDs: func(
				ctx context.Context,
				gotWorkerID string,
				limit int,
			) ([]uuid.UUID, error) {
				listCalls++
				if gotWorkerID != workerID || limit != deploymentCampaignSchedulerBatchSize {
					t.Fatalf("unexpected selection parameters: %q %d", gotWorkerID, limit)
				}
				if injected, _ := ctx.Value(campaignSchedulerInjectionTestKey{}).(bool); !injected {
					t.Fatal("execution runtime was not injected before repository selection")
				}
				return []uuid.UUID{firstRunID, secondRunID}, nil
			},
			Scheduler: ticker,
			InjectRuntime: func(ctx context.Context) context.Context {
				return context.WithValue(ctx, campaignSchedulerInjectionTestKey{}, true)
			},
			Clock: func() time.Time {
				return time.Date(2026, 7, 22, 2, 3, 4, 0, time.UTC)
			},
		},
	)
	if err != nil {
		t.Fatalf("register campaign scheduler: %v", err)
	}

	err = registrar.job.Run(context.Background())
	if err == nil || !errors.Is(err, errDeploymentCampaignSchedulerTick) {
		t.Fatalf("expected joined tick error, got %v", err)
	}
	if listCalls != 1 {
		t.Fatalf("list calls = %d, want 1", listCalls)
	}
	if !ticker.injected {
		t.Fatal("scheduler tick did not receive injected execution runtime")
	}
	if len(ticker.runIDs) != 2 || ticker.runIDs[0] != firstRunID || ticker.runIDs[1] != secondRunID {
		t.Fatalf("processed runs = %#v", ticker.runIDs)
	}
}

func TestDeploymentCampaignSchedulerJobFailsClosedWhenDependenciesAreMissing(t *testing.T) {
	registrar := &durationJobRegistrarFake{}
	err := registerDeploymentCampaignScheduler(
		registrar,
		featureflags.NewRegistry([]featureflags.Key{
			featureflags.KeyOperatorControlPlaneV2,
			featureflags.KeyExecutorProtocolV2,
		}),
		campaignSchedulerJobDependencies{},
	)
	if err != nil {
		t.Fatalf("register campaign scheduler: %v", err)
	}
	if err := registrar.job.Run(context.Background()); !errors.Is(err, errDeploymentCampaignSchedulerUnconfigured) {
		t.Fatalf("expected unconfigured error, got %v", err)
	}
}
