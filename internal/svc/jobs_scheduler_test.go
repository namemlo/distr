package svc

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestDesiredObservationDeadlineSweepIsAlwaysRegistered(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("jobs_scheduler.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)

	g.Expect(text).To(ContainSubstring("DesiredObservationDeadlineSweep"))
	g.Expect(text).To(ContainSubstring("desiredObservationDeadlineSweepJob"))
}

type executionJobInjectionTestKey struct{}

func TestDesiredObservationDeadlineSweepJobInjectsRuntimeAndDispatchesCommittedTasks(t *testing.T) {
	g := NewWithT(t)
	task := types.Task{ID: uuid.New(), ProtocolVersion: types.ExecutionProtocolVersionV2}
	swept := false
	dispatched := false
	job := desiredObservationDeadlineSweepJob(desiredObservationDeadlineSweepJobDependencies{
		InjectRuntime: func(ctx context.Context) context.Context {
			return context.WithValue(ctx, executionJobInjectionTestKey{}, true)
		},
		Sweep: func(ctx context.Context) ([]types.Task, error) {
			g.Expect(ctx.Value(executionJobInjectionTestKey{})).To(BeTrue())
			swept = true
			return []types.Task{task}, nil
		},
		Dispatch: func(ctx context.Context, tasks []types.Task) error {
			g.Expect(ctx.Value(executionJobInjectionTestKey{})).To(BeTrue())
			g.Expect(swept).To(BeTrue())
			g.Expect(tasks).To(Equal([]types.Task{task}))
			dispatched = true
			return nil
		},
	})

	g.Expect(job(context.Background())).To(Succeed())
	g.Expect(dispatched).To(BeTrue())
}

func TestDesiredObservationDeadlineSweepJobPropagatesSweepAndDispatchErrors(t *testing.T) {
	g := NewWithT(t)
	wantSweep := errors.New("sweep unavailable")
	job := desiredObservationDeadlineSweepJob(desiredObservationDeadlineSweepJobDependencies{
		InjectRuntime: func(ctx context.Context) context.Context { return ctx },
		Sweep:         func(context.Context) ([]types.Task, error) { return nil, wantSweep },
		Dispatch: func(context.Context, []types.Task) error {
			t.Fatal("dispatch must not run after a failed sweep")
			return nil
		},
	})
	g.Expect(job(context.Background())).To(MatchError(ContainSubstring("sweep unavailable")))

	wantDispatch := errors.New("dispatch unavailable")
	job = desiredObservationDeadlineSweepJob(desiredObservationDeadlineSweepJobDependencies{
		InjectRuntime: func(ctx context.Context) context.Context { return ctx },
		Sweep:         func(context.Context) ([]types.Task, error) { return []types.Task{{ID: uuid.New()}}, nil },
		Dispatch:      func(context.Context, []types.Task) error { return wantDispatch },
	})
	g.Expect(job(context.Background())).To(MatchError(ContainSubstring("dispatch unavailable")))
}

func TestExecutionV2ReadyStepRecoveryRegistrationRequiresBothControlPlaneFlags(t *testing.T) {
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
			err := registerExecutionV2ReadyStepRecovery(
				registrar,
				featureflags.NewRegistry(test.flags),
				executionV2ReadyStepRecoveryJobDependencies{},
			)
			if err != nil {
				t.Fatalf("register ready-step recovery: %v", err)
			}
			want := 0
			if test.registered {
				want = 1
			}
			if registrar.calls != want {
				t.Fatalf("registration calls = %d, want %d", registrar.calls, want)
			}
			if test.registered && registrar.duration != executionV2ReadyStepRecoveryInterval {
				t.Fatalf("duration = %s, want %s", registrar.duration, executionV2ReadyStepRecoveryInterval)
			}
		})
	}
}

func TestExecutionV2ReadyStepRecoveryInjectsRuntimeAndDispatchesBoundedBatch(t *testing.T) {
	g := NewWithT(t)
	tasks := []types.Task{{ID: uuid.New()}, {ID: uuid.New()}}
	registrar := &durationJobRegistrarFake{}
	err := registerExecutionV2ReadyStepRecovery(
		registrar,
		featureflags.NewRegistry([]featureflags.Key{
			featureflags.KeyOperatorControlPlaneV2,
			featureflags.KeyExecutorProtocolV2,
		}),
		executionV2ReadyStepRecoveryJobDependencies{
			InjectRuntime: func(ctx context.Context) context.Context {
				return context.WithValue(ctx, executionJobInjectionTestKey{}, true)
			},
			ListReadyTasks: func(ctx context.Context, limit int) ([]types.Task, error) {
				g.Expect(ctx.Value(executionJobInjectionTestKey{})).To(BeTrue())
				g.Expect(limit).To(Equal(executionV2ReadyStepRecoveryBatchSize))
				return tasks, nil
			},
			Dispatch: func(ctx context.Context, got []types.Task) error {
				g.Expect(ctx.Value(executionJobInjectionTestKey{})).To(BeTrue())
				g.Expect(got).To(Equal(tasks))
				return nil
			},
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(registrar.job.Run(context.Background())).To(Succeed())
}

func TestExecutionV2ReadyStepRecoveryPropagatesSelectionAndDispatchErrors(t *testing.T) {
	g := NewWithT(t)
	wantList := errors.New("selection unavailable")
	job := executionV2ReadyStepRecoveryJob(executionV2ReadyStepRecoveryJobDependencies{
		InjectRuntime: func(ctx context.Context) context.Context { return ctx },
		ListReadyTasks: func(context.Context, int) ([]types.Task, error) {
			return nil, wantList
		},
		Dispatch: func(context.Context, []types.Task) error { return nil },
	})
	g.Expect(job(context.Background())).To(MatchError(ContainSubstring("selection unavailable")))

	wantDispatch := errors.New("dispatch unavailable")
	job = executionV2ReadyStepRecoveryJob(executionV2ReadyStepRecoveryJobDependencies{
		InjectRuntime: func(ctx context.Context) context.Context { return ctx },
		ListReadyTasks: func(context.Context, int) ([]types.Task, error) {
			return []types.Task{{ID: uuid.New()}}, nil
		},
		Dispatch: func(context.Context, []types.Task) error { return wantDispatch },
	})
	g.Expect(job(context.Background())).To(MatchError(ContainSubstring("dispatch unavailable")))
}

func TestExecutionV2ReadyStepRecoveryFailsClosedWhenDependenciesAreMissing(t *testing.T) {
	g := NewWithT(t)
	job := executionV2ReadyStepRecoveryJob(executionV2ReadyStepRecoveryJobDependencies{})
	g.Expect(job(context.Background())).To(MatchError(errExecutionV2ReadyStepRecoveryUnconfigured))
}

func TestProductionCampaignSchedulerUsesTrustedObservationAdapters(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("jobs_scheduler.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)

	g.Expect(text).To(ContainSubstring("observation.CampaignResolver{Store: observationRepository}"))
	g.Expect(text).To(ContainSubstring("observation.CampaignVerifier{Store: observationRepository}"))
	g.Expect(text).NotTo(ContainSubstring("campaigns.UnwiredCampaignObservationResolver{}"))
	g.Expect(text).NotTo(ContainSubstring("campaigns.UnwiredCampaignObservationVerifier{}"))
}
