package svc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/distr-sh/distr/internal/campaignruntime"
	"github.com/distr-sh/distr/internal/campaigns"
	"github.com/distr-sh/distr/internal/cleanup"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/env"
	"github.com/distr-sh/distr/internal/executionworker"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/jobs"
	"github.com/distr-sh/distr/internal/notification"
	"github.com/distr-sh/distr/internal/observation"
	"github.com/distr-sh/distr/internal/registry/upstream"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

const (
	executionV2ReadyStepRecoveryInterval  = 5 * time.Second
	executionV2ReadyStepRecoveryTimeout   = 30 * time.Second
	executionV2ReadyStepRecoveryBatchSize = 100

	deploymentCampaignSchedulerInterval      = 5 * time.Second
	deploymentCampaignSchedulerTimeout       = 30 * time.Second
	deploymentCampaignSchedulerLeaseDuration = 30 * time.Second
	deploymentCampaignSchedulerBatchSize     = 25
)

var (
	errDesiredObservationDeadlineSweepUnconfigured = errors.New(
		"desired observation deadline sweep is unconfigured",
	)
	errExecutionV2ReadyStepRecoveryUnconfigured = errors.New(
		"execution v2 ready-step recovery is unconfigured",
	)
	errDeploymentCampaignSchedulerUnconfigured = errors.New("deployment campaign scheduler is unconfigured")
	errDeploymentCampaignSchedulerTick         = errors.New("deployment campaign scheduler tick failed")
	deploymentCampaignSchedulerWorkerID        = newDeploymentCampaignSchedulerWorkerID()
)

type durationJobRegistrar interface {
	RegisterDurationJob(time.Duration, jobs.Job) error
}

type campaignSchedulerTicker interface {
	Tick(context.Context, uuid.UUID, time.Time) (types.CampaignSchedulerResult, error)
}

type campaignSchedulerJobDependencies struct {
	WorkerID                   string
	ListRunnableCampaignRunIDs func(context.Context, string, int) ([]uuid.UUID, error)
	Scheduler                  campaignSchedulerTicker
	InjectRuntime              func(context.Context) context.Context
	Clock                      func() time.Time
}

type desiredObservationDeadlineSweepJobDependencies struct {
	InjectRuntime func(context.Context) context.Context
	Sweep         func(context.Context) ([]types.Task, error)
	Dispatch      func(context.Context, []types.Task) error
}

type executionV2ReadyStepRecoveryJobDependencies struct {
	InjectRuntime  func(context.Context) context.Context
	ListReadyTasks func(context.Context, int) ([]types.Task, error)
	Dispatch       func(context.Context, []types.Task) error
}

func (r *Registry) GetJobsScheduler() *jobs.Scheduler {
	return r.jobsScheduler
}

func (r *Registry) createJobsScheduler() (*jobs.Scheduler, error) {
	scheduler, err := jobs.NewScheduler(r.GetLogger(), r.GetDbPool(), r.GetMailer(), r.GetTracers().Always(), r.s3Client)
	if err != nil {
		return nil, err
	}
	err = scheduler.RegisterCronJob(
		"* * * * *",
		jobs.NewJob(
			"DesiredObservationDeadlineSweep",
			desiredObservationDeadlineSweepJob(
				r.desiredObservationDeadlineSweepJobDependencies(),
			),
			50*time.Second,
		),
	)
	if err != nil {
		return nil, err
	}

	if cron := env.CleanupDeploymenRevisionStatusCron(); cron != nil {
		err = scheduler.RegisterCronJob(
			*cron,
			jobs.NewJob(
				"DeploymentRevisionStatusCleanup",
				cleanup.RunDeploymentRevisionStatusCleanup,
				env.CleanupDeploymenRevisionStatusTimeout(),
			),
		)
		if err != nil {
			return nil, err
		}
	}

	if cron := env.CleanupDeploymentTargetMetricsCron(); cron != nil {
		err = scheduler.RegisterCronJob(
			*cron,
			jobs.NewJob(
				"DeploymentTargetMetricsCleanup",
				cleanup.RunDeploymentTargetMetricsCleanup,
				env.CleanupDeploymentTargetMetricsTimeout(),
			),
		)
		if err != nil {
			return nil, err
		}
	}

	if cron := env.CleanupDeploymentTargetLogRecordCron(); cron != nil {
		err = scheduler.RegisterCronJob(
			*cron,
			jobs.NewJob(
				"DeploymentTargetLogRecordCleanup",
				cleanup.RunDeploymentTargetLogRecordCleanup,
				env.CleanupDeploymentTargetLogRecordTimeout(),
			),
		)
		if err != nil {
			return nil, err
		}
	}

	if cron := env.CleanupDeploymentLogRecordCron(); cron != nil {
		err = scheduler.RegisterCronJob(
			*cron,
			jobs.NewJob(
				"DeploymentLogRecordCleanup",
				cleanup.RunDeploymentLogRecordCleanup,
				env.CleanupDeploymentLogRecordTimeout(),
			),
		)
		if err != nil {
			return nil, err
		}
	}

	if cron := env.CleanupOIDCStateCron(); cron != nil {
		err = scheduler.RegisterCronJob(
			*cron,
			jobs.NewJob("OIDCStateCleanup", cleanup.RunOIDCStateCleanup, env.CleanupOIDCStateCronTimeout()),
		)
		if err != nil {
			return nil, err
		}
	}

	if cron := env.CleanupArtifactBlobCron(); cron != nil && r.s3Client != nil {
		err = scheduler.RegisterCronJob(
			*cron,
			jobs.NewJob("ArtifactBlobCleanup", cleanup.RunArtifactBlobCleanup, env.CleanupArtifactBlobTimeout()),
		)
		if err != nil {
			return nil, err
		}
	}

	if cron := env.CleanupOrganizationCron(); cron != nil {
		err = scheduler.RegisterCronJob(
			*cron,
			jobs.NewJob("OrganizationCleanup", cleanup.RunOrganizationCleanup, env.CleanupOrganizationTimeout()),
		)
		if err != nil {
			return nil, err
		}
	}

	if cron := env.DeploymentStatusNotificationCron(); cron != nil {
		err = scheduler.RegisterCronJob(
			*cron,
			jobs.NewJob(
				"DeploymentStatusNotification",
				notification.RunDeploymentStatusNotifications,
				env.DeploymentStatusNotificationTimeout(),
			),
		)
		if err != nil {
			return nil, err
		}
	}

	if cron := env.RegistryUpstreamSyncCron(); cron != nil {
		err = scheduler.RegisterCronJob(
			*cron,
			jobs.NewJob("RegistryUpstreamSync", func(ctx context.Context) error {
				return upstream.RunUpstreamSync(ctx, true)
			}, env.RegistryUpstreamSyncTimeout()),
		)
		if err != nil {
			return nil, err
		}
	}

	flags := featureflags.NewRegistry(env.ExperimentalFeatureFlags())
	if err := registerDeploymentCampaignScheduler(
		scheduler,
		flags,
		r.deploymentCampaignSchedulerJobDependencies(),
	); err != nil {
		return nil, err
	}
	if err := registerExecutionV2ReadyStepRecovery(
		scheduler,
		flags,
		r.executionV2ReadyStepRecoveryJobDependencies(),
	); err != nil {
		return nil, err
	}

	return scheduler, nil
}

func (r *Registry) desiredObservationDeadlineSweepJobDependencies() desiredObservationDeadlineSweepJobDependencies {
	return desiredObservationDeadlineSweepJobDependencies{
		InjectRuntime: r.executionRuntime.Inject,
		Sweep:         db.RunDesiredObservationDeadlineSweepWithTasks,
		Dispatch:      executionworker.DispatchCreatedTasks,
	}
}

func desiredObservationDeadlineSweepJob(
	dependencies desiredObservationDeadlineSweepJobDependencies,
) jobs.JobFunc {
	return func(ctx context.Context) error {
		if dependencies.InjectRuntime == nil || dependencies.Sweep == nil || dependencies.Dispatch == nil {
			return errDesiredObservationDeadlineSweepUnconfigured
		}
		ctx = dependencies.InjectRuntime(ctx)
		tasks, err := dependencies.Sweep(ctx)
		if err != nil {
			return fmt.Errorf("sweep desired observation deadlines: %w", err)
		}
		if err := dependencies.Dispatch(ctx, tasks); err != nil {
			return fmt.Errorf("dispatch desired observation continuations: %w", err)
		}
		return nil
	}
}

func (r *Registry) executionV2ReadyStepRecoveryJobDependencies() executionV2ReadyStepRecoveryJobDependencies {
	return executionV2ReadyStepRecoveryJobDependencies{
		InjectRuntime:  r.executionRuntime.Inject,
		ListReadyTasks: db.ListExecutionV2ReadyDispatchTasks,
		Dispatch:       executionworker.DispatchRecoveredTasks,
	}
}

func registerExecutionV2ReadyStepRecovery(
	registrar durationJobRegistrar,
	flags featureflags.Registry,
	dependencies executionV2ReadyStepRecoveryJobDependencies,
) error {
	if !flags.IsEnabled(featureflags.KeyOperatorControlPlaneV2) ||
		!flags.IsEnabled(featureflags.KeyExecutorProtocolV2) {
		return nil
	}
	return registrar.RegisterDurationJob(
		executionV2ReadyStepRecoveryInterval,
		jobs.NewJob(
			"ExecutionV2ReadyStepRecovery",
			executionV2ReadyStepRecoveryJob(dependencies),
			executionV2ReadyStepRecoveryTimeout,
		),
	)
}

func executionV2ReadyStepRecoveryJob(
	dependencies executionV2ReadyStepRecoveryJobDependencies,
) jobs.JobFunc {
	return func(ctx context.Context) error {
		if dependencies.InjectRuntime == nil ||
			dependencies.ListReadyTasks == nil || dependencies.Dispatch == nil {
			return errExecutionV2ReadyStepRecoveryUnconfigured
		}
		ctx = dependencies.InjectRuntime(ctx)
		tasks, err := dependencies.ListReadyTasks(ctx, executionV2ReadyStepRecoveryBatchSize)
		if err != nil {
			return fmt.Errorf("list execution v2 ready-step recovery tasks: %w", err)
		}
		if err := dependencies.Dispatch(ctx, tasks); err != nil {
			return fmt.Errorf("dispatch execution v2 ready-step recovery tasks: %w", err)
		}
		return nil
	}
}

func (r *Registry) deploymentCampaignSchedulerJobDependencies() campaignSchedulerJobDependencies {
	repository := db.CampaignRepository{}
	observationRepository := db.CampaignObservationRepository{}
	scheduler := campaigns.NewSchedulerWithRuntime(
		repository,
		observation.CampaignResolver{Store: observationRepository},
		observation.CampaignVerifier{Store: observationRepository},
		campaignruntime.NewDatabaseBackgroundAdmissionAuthorizer(),
		campaigns.CampaignTaskDispatcherFunc(executionworker.DispatchCreatedTasks),
		deploymentCampaignSchedulerWorkerID,
		deploymentCampaignSchedulerLeaseDuration,
	)
	return campaignSchedulerJobDependencies{
		WorkerID:                   deploymentCampaignSchedulerWorkerID,
		ListRunnableCampaignRunIDs: repository.ListRunnableCampaignRunIDs,
		Scheduler:                  scheduler,
		InjectRuntime: func(ctx context.Context) context.Context {
			return r.executionRuntime.Inject(ctx)
		},
		Clock: func() time.Time { return time.Now().UTC() },
	}
}

func registerDeploymentCampaignScheduler(
	registrar durationJobRegistrar,
	flags featureflags.Registry,
	dependencies campaignSchedulerJobDependencies,
) error {
	if !flags.IsEnabled(featureflags.KeyOperatorControlPlaneV2) ||
		!flags.IsEnabled(featureflags.KeyExecutorProtocolV2) {
		return nil
	}
	return registrar.RegisterDurationJob(
		deploymentCampaignSchedulerInterval,
		jobs.NewJob(
			"DeploymentCampaignScheduler",
			deploymentCampaignSchedulerJob(dependencies),
			deploymentCampaignSchedulerTimeout,
		),
	)
}

func deploymentCampaignSchedulerJob(
	dependencies campaignSchedulerJobDependencies,
) jobs.JobFunc {
	return func(ctx context.Context) error {
		if dependencies.WorkerID == "" ||
			dependencies.ListRunnableCampaignRunIDs == nil ||
			dependencies.Scheduler == nil ||
			dependencies.InjectRuntime == nil ||
			dependencies.Clock == nil {
			return errDeploymentCampaignSchedulerUnconfigured
		}

		ctx = dependencies.InjectRuntime(ctx)
		runIDs, err := dependencies.ListRunnableCampaignRunIDs(
			ctx,
			dependencies.WorkerID,
			deploymentCampaignSchedulerBatchSize,
		)
		if err != nil {
			return fmt.Errorf("list runnable deployment campaigns: %w", err)
		}

		var tickErrors []error
		for _, runID := range runIDs {
			if _, err := dependencies.Scheduler.Tick(ctx, runID, dependencies.Clock().UTC()); err != nil {
				tickErrors = append(tickErrors, fmt.Errorf(
					"%w for run %s: %w",
					errDeploymentCampaignSchedulerTick,
					runID,
					err,
				))
			}
		}
		return errors.Join(tickErrors...)
	}
}

func newDeploymentCampaignSchedulerWorkerID() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown-host"
	}
	return fmt.Sprintf("campaign-scheduler:%s:%d:%s", hostname, os.Getpid(), uuid.NewString())
}
