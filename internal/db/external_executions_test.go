package db_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/gomega"
)

func TestExternalExecutionRepositoryRecordsIdempotentCallbacksAndObservedState(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createExternalExecutionPlan(t, ctx)
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID: deps.orgID, DeploymentPlanID: deps.plan.ID, ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(tasks).To(HaveLen(1))
	g.Expect(tasks[0].StepRuns).To(HaveLen(1))
	lease, err := db.LeaseHubTask(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(lease).NotTo(BeNil())
	g.Expect(lease.TaskID).To(Equal(tasks[0].ID))

	execution, err := db.PrepareExternalExecution(ctx, types.PrepareExternalExecutionRequest{
		OrganizationID: deps.orgID, StepRunID: lease.Steps[0].StepRunID,
		Component: "api-image", CallbackTimeoutSeconds: 600,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(execution.Status).To(Equal(types.ExternalExecutionStatusQueued))
	g.Expect(execution.ExpectedImage).To(ContainSubstring("@sha256:"))
	g.Expect(execution.ExpectedConfigReference).To(ContainSubstring("s3://emlo-backend-configs/"))

	replayedPrepare, err := db.PrepareExternalExecution(ctx, types.PrepareExternalExecutionRequest{
		OrganizationID: deps.orgID, StepRunID: tasks[0].StepRuns[0].ID,
		Component: "api-image", CallbackTimeoutSeconds: 600,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(replayedPrepare.ID).To(Equal(execution.ID))

	execution, err = db.MarkExternalExecutionTriggered(ctx, types.MarkExternalExecutionTriggeredRequest{
		OrganizationID: deps.orgID, ExternalExecutionID: execution.ID, TriggerAttempts: 1,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(execution.Status).To(Equal(types.ExternalExecutionStatusRunning))
	_, err = db.MarkExternalExecutionTriggered(ctx, types.MarkExternalExecutionTriggeredRequest{
		OrganizationID: deps.orgID, ExternalExecutionID: execution.ID, TriggerAttempts: 1,
	})
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())

	running := types.RecordExternalExecutionCallbackRequest{
		OrganizationID: deps.orgID, ExternalExecutionID: execution.ID, Sequence: 1,
		Status: types.ExternalExecutionStatusRunning, ProviderReference: "jenkins-42",
		ProviderURL: "https://jenkins.example/job/42", Message: "deploying loyalty-api",
	}
	execution, err = db.RecordExternalExecutionCallback(ctx, running)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(execution.LastCallbackSequence).To(Equal(int64(1)))

	replayed, err := db.RecordExternalExecutionCallback(ctx, running)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(replayed.ID).To(Equal(execution.ID))
	conflictingReplay := running
	conflictingReplay.Message = "different payload"
	_, err = db.RecordExternalExecutionCallback(ctx, conflictingReplay)
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())

	observed := types.ExternalExecutionObservedState{
		Version: execution.ExpectedVersion, Image: execution.ExpectedImage, Platform: execution.ExpectedPlatform,
		Contracts: execution.ExpectedContracts, ConfigReference: execution.ExpectedConfigReference,
		ConfigChecksum: execution.ExpectedConfigChecksum, Health: types.TargetComponentHealthHealthy,
	}
	execution, err = db.RecordExternalExecutionCallback(ctx, types.RecordExternalExecutionCallbackRequest{
		OrganizationID: deps.orgID, ExternalExecutionID: execution.ID, Sequence: 2,
		Status: types.ExternalExecutionStatusSucceeded, ProviderReference: "jenkins-42",
		ProviderURL: "https://jenkins.example/job/42", Message: "loyalty-api is healthy",
		ObservedState: &observed,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(execution.Status).To(Equal(types.ExternalExecutionStatusSucceeded))
	g.Expect(execution.ObservedStateChecksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))

	state, err := db.GetTargetComponentState(
		ctx, deps.orgID, tasks[0].DeploymentTargetID, deps.applicationID, "api-image",
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(state.ReleaseBundleID).To(Equal(execution.ReleaseBundleID))
	g.Expect(state.ConfigReference).To(Equal(execution.ExpectedConfigReference))
	g.Expect(state.StateVersion).To(Equal(int64(1)))

	_, err = db.GetExternalExecution(ctx, execution.ID, uuid.New())
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
	events, err := db.GetExternalExecutionEvents(ctx, execution.ID, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(events).To(HaveLen(2))
	g.Expect(events[1].Sequence).To(Equal(int64(2)))
	g.Expect(events[1].Status).To(Equal(types.ExternalExecutionStatusSucceeded))
	_, err = internalctx.GetDb(ctx).Exec(ctx,
		`UPDATE ExternalExecution SET callback_deadline_at = now() - interval '1 second' WHERE id = @id`,
		pgx.NamedArgs{"id": execution.ID})
	g.Expect(err).NotTo(HaveOccurred())
	_, err = db.RecordExternalExecutionCallback(ctx, types.RecordExternalExecutionCallbackRequest{
		OrganizationID: deps.orgID, ExternalExecutionID: execution.ID, Sequence: 3,
		Status: types.ExternalExecutionStatusRunning, Message: "late callback",
	})
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
	terminal, err := db.GetExternalExecution(ctx, execution.ID, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(terminal.Status).To(Equal(types.ExternalExecutionStatusSucceeded))

	downSQL, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "136_external_execution.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	_, err = internalctx.GetDb(ctx).Exec(ctx, string(downSQL))
	g.Expect(err).NotTo(HaveOccurred())
	upSQL, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "136_external_execution.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	_, err = internalctx.GetDb(ctx).Exec(ctx, string(upSQL))
	g.Expect(err).NotTo(HaveOccurred())
}

func TestFailExternalExecutionAdvancesDurableEventSequence(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createExternalExecutionPlan(t, ctx)
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID: deps.orgID, DeploymentPlanID: deps.plan.ID, ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	lease, err := db.LeaseHubTask(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(lease).NotTo(BeNil())
	g.Expect(lease.TaskID).To(Equal(tasks[0].ID))
	execution, err := db.PrepareExternalExecution(ctx, types.PrepareExternalExecutionRequest{
		OrganizationID: deps.orgID, StepRunID: lease.Steps[0].StepRunID,
		Component: "api-image", CallbackTimeoutSeconds: 600,
	})
	g.Expect(err).NotTo(HaveOccurred())

	failed, err := db.FailExternalExecution(ctx, types.FailExternalExecutionRequest{
		OrganizationID: deps.orgID, ExternalExecutionID: execution.ID, Message: "trigger failed",
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(failed.Status).To(Equal(types.ExternalExecutionStatusFailed))
	g.Expect(failed.LastCallbackSequence).To(Equal(int64(1)))
	events, err := db.GetExternalExecutionEvents(ctx, execution.ID, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(events).To(HaveLen(1))
	g.Expect(events[0].Sequence).To(Equal(int64(1)))
	g.Expect(events[0].Status).To(Equal(types.ExternalExecutionStatusFailed))
}

func TestExternalExecutionRejectsLateSuccessAndPersistsTimeout(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createExternalExecutionPlan(t, ctx)
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID: deps.orgID, DeploymentPlanID: deps.plan.ID, ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	lease, err := db.LeaseHubTask(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	execution, err := db.PrepareExternalExecution(ctx, types.PrepareExternalExecutionRequest{
		OrganizationID: deps.orgID, StepRunID: lease.Steps[0].StepRunID,
		Component: "api-image", CallbackTimeoutSeconds: 600,
	})
	g.Expect(err).NotTo(HaveOccurred())
	_, err = internalctx.GetDb(ctx).Exec(ctx,
		`UPDATE ExternalExecution SET callback_deadline_at = now() - interval '1 second' WHERE id = @id`,
		pgx.NamedArgs{"id": execution.ID})
	g.Expect(err).NotTo(HaveOccurred())
	observed := types.ExternalExecutionObservedState{
		Version: execution.ExpectedVersion, Image: execution.ExpectedImage, Platform: execution.ExpectedPlatform,
		Contracts: execution.ExpectedContracts, ConfigReference: execution.ExpectedConfigReference,
		ConfigChecksum: execution.ExpectedConfigChecksum, Health: types.TargetComponentHealthHealthy,
	}

	_, err = db.RecordExternalExecutionCallback(ctx, types.RecordExternalExecutionCallbackRequest{
		OrganizationID: deps.orgID, ExternalExecutionID: execution.ID, Sequence: 1,
		Status: types.ExternalExecutionStatusSucceeded, ObservedState: &observed,
	})

	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
	timedOut, err := db.GetExternalExecution(ctx, execution.ID, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(timedOut.Status).To(Equal(types.ExternalExecutionStatusTimedOut))
	g.Expect(timedOut.LastCallbackSequence).To(Equal(int64(1)))
	_, err = db.GetTargetComponentState(ctx, deps.orgID, tasks[0].DeploymentTargetID, deps.applicationID, "api-image")
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
}

func TestTerminalTaskTransitionSerializesWithComponentProjection(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createExternalExecutionPlan(t, ctx)
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID: deps.orgID, DeploymentPlanID: deps.plan.ID, ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	lease, err := db.LeaseHubTask(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(lease).NotTo(BeNil())

	pool, ok := internalctx.GetDb(ctx).(*pgxpool.Pool)
	g.Expect(ok).To(BeTrue())
	tx, err := pool.Begin(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	resourceType := string(types.TaskLockResourceTargetComponent)
	resourceKey := tasks[0].DeploymentTargetID.String() + ":api-image"
	groupKey := fmt.Sprintf("%s:%d:%s:%d:%s", deps.orgID, len(resourceType), resourceType, len(resourceKey), resourceKey)
	_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1), hashtext($2))`,
		"distr-task-resource-lock", groupKey)
	g.Expect(err).NotTo(HaveOccurred())

	done := make(chan error, 1)
	go func() {
		_, transitionErr := db.TransitionTaskState(ctx, types.TransitionTaskStateRequest{
			OrganizationID: deps.orgID, TaskID: tasks[0].ID, Status: types.TaskStatusFailed,
		})
		done <- transitionErr
	}()
	select {
	case err := <-done:
		t.Fatalf("terminal transition bypassed component advisory lock: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	g.Expect(tx.Rollback(ctx)).To(Succeed())
	select {
	case err := <-done:
		g.Expect(err).NotTo(HaveOccurred())
	case <-time.After(3 * time.Second):
		t.Fatal("terminal transition remained blocked after component advisory lock was released")
	}
}

func TestExternalExecutionMigrationDefinesDurableCallbackState(t *testing.T) {
	g := NewWithT(t)
	up, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "136_external_execution.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	upSQL := string(up)
	g.Expect(upSQL).To(ContainSubstring("CREATE TABLE ExternalExecution"))
	g.Expect(upSQL).To(ContainSubstring("CREATE TABLE ExternalExecutionEvent"))
	g.Expect(upSQL).To(ContainSubstring("UNIQUE (step_run_id)"))
	g.Expect(upSQL).To(ContainSubstring("UNIQUE (external_execution_id, sequence)"))
	g.Expect(upSQL).To(ContainSubstring("'TIMED_OUT'"))
	g.Expect(upSQL).To(ContainSubstring("ADD COLUMN config_reference"))
	g.Expect(upSQL).To(ContainSubstring("external_execution_id UUID"))
	g.Expect(upSQL).To(ContainSubstring("releasebundle_id_organization_unique"))
	g.Expect(upSQL).To(ContainSubstring("sequence BETWEEN 1 AND 256"))
	stepEvents, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "125_step_events.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(stepEvents)).To(ContainSubstring("steprun_id_task_organization_unique"))

	down, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "136_external_execution.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	downSQL := string(down)
	g.Expect(downSQL).To(ContainSubstring("DROP TABLE IF EXISTS ExternalExecutionEvent"))
	g.Expect(downSQL).To(ContainSubstring("DROP TABLE IF EXISTS ExternalExecution"))
	g.Expect(downSQL).To(ContainSubstring("DROP COLUMN IF EXISTS config_reference"))
	g.Expect(downSQL).To(ContainSubstring("DROP CONSTRAINT IF EXISTS releasebundle_id_organization_unique"))
	g.Expect(downSQL).To(ContainSubstring("SET external_execution_id = NULL"))
}

func createExternalExecutionPlan(t *testing.T, ctx context.Context) taskQueuePlanDeps {
	t.Helper()
	g := NewWithT(t)
	deps := createReleaseBundleEligibilityDependencies(t, ctx)
	process := types.DeploymentProcess{
		OrganizationID: deps.orgID, ApplicationID: deps.applicationID,
		Name: "External execution " + uuid.NewString(),
	}
	g.Expect(db.CreateDeploymentProcess(ctx, &process)).To(Succeed())
	revision := types.DeploymentProcessRevision{
		OrganizationID: deps.orgID, DeploymentProcessID: process.ID, Description: "Callback execution",
		Steps: []types.DeploymentProcessStep{{
			Key: "deploy", Name: "Deploy with external executor", ActionType: "distr.webhook",
			ExecutionLocation: "hub", FailureMode: "fail", TimeoutSeconds: 60,
			RetryMaxAttempts: 1, RetryIntervalSeconds: 1, SortOrder: 10,
			InputBindings: map[string]any{
				"url": "https://hooks.example.com/deploy", "completionMode": "callback",
				"component": "api-image", "callbackTimeoutSeconds": 600,
				"signingSecret": "webhook_signing_key",
			},
		}},
	}
	g.Expect(db.CreateDeploymentProcessRevision(ctx, &revision)).To(Succeed())
	createDeploymentPlanVariableSet(t, ctx, deps.orgID, deps.applicationID)
	targetID := createReleaseBundleDockerTargetForOrganization(t, ctx, deps.orgID, "choice-tp-dev")
	actorID := createReleaseBundleTestUser(t, ctx, deps.orgID)
	_, err := internalctx.GetDb(ctx).Exec(
		ctx,
		`INSERT INTO Secret (organization_id, key, value, updated_by_useraccount_id)
		VALUES (@organizationId, @key, @value, @updatedBy)`,
		pgx.NamedArgs{
			"organizationId": deps.orgID, "key": "webhook_signing_key",
			"value": "test-only-signing-secret", "updatedBy": actorID,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	bundle := ociReleaseBundleFixture(deps.orgID, deps.applicationID, deps.channelID)
	bundle.DeploymentProcessRevisionID = &revision.ID
	bundle.ReleaseContract = releaseContractFixture(bundle.Components[0].Digest)
	bundle.ReleaseContract.Config.ImmutableObjects = []types.ReleaseContractConfigObject{{
		URI:       "s3://emlo-backend-configs/choice-tp_dev/1/rmt-loyalty-api/appsettings.Production.json",
		VersionID: "v42", Checksum: "sha256:" + strings.Repeat("b", 64),
	}}
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())
	published, result, err := db.PublishReleaseBundle(ctx, bundle.ID, deps.orgID, actorID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Valid).To(BeTrue())
	plan, err := db.CreateDeploymentPlan(ctx, types.CreateDeploymentPlanRequest{
		OrganizationID: deps.orgID, ReleaseBundleID: published.ID,
		EnvironmentID: deps.devEnvironmentID, TargetIDs: []uuid.UUID{targetID},
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(plan.Status).To(Equal(types.DeploymentPlanStatusReady))
	return taskQueuePlanDeps{
		orgID: deps.orgID, applicationID: deps.applicationID, channelID: deps.channelID,
		devEnvironmentID: deps.devEnvironmentID, actorID: actorID, plan: plan,
	}
}
