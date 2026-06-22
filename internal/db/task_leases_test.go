package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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

func TestTaskLeaseRepositoryClaimsQueuedTaskForAgent(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskLease(t, ctx)
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(lease).NotTo(BeNil())
	g.Expect(lease.TaskID).To(Equal(tasks[0].ID))
	g.Expect(lease.AgentID).To(Equal(tasks[0].DeploymentTargetID))
	g.Expect(lease.LeaseToken).NotTo(BeEmpty())
	g.Expect(lease.Attempt).To(Equal(1))
	g.Expect(lease.ExpiresAt).To(BeTemporally(">", lease.HeartbeatAt))
	g.Expect(lease.Task.Status).To(Equal(types.TaskStatusRunning))
	g.Expect(lease.Task.Locks).To(ContainElement(WithTransform(
		func(lock types.TaskResourceLock) bool { return lock.AcquiredAt != nil && lock.ReleasedAt == nil },
		BeTrue(),
	)))
	g.Expect(lease.Steps).To(HaveLen(1))
	g.Expect(lease.Steps[0].StepRunID).To(Equal(tasks[0].StepRuns[0].ID))
	g.Expect(lease.Steps[0].StepKey).To(Equal("deploy"))
	g.Expect(lease.Steps[0].ActionType).To(Equal("distr.http.check"))
	g.Expect(lease.Steps[0].ActionVersion).To(Equal(types.AgentActionVersionV1))
	g.Expect(lease.Steps[0].InputBindings).To(HaveKey("url"))
	g.Expect(lease.Steps[0].IdempotencyKey).To(HavePrefix("sha256:"))

	fetched, err := db.GetTask(ctx, tasks[0].ID, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(fetched.Status).To(Equal(types.TaskStatusRunning))
}

func TestTaskLeaseRepositoryResolvesComposeRegistryAuthSecretOnlyForAgentLease(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	compose := taskLeaseComposeDeployStep("compose", "Compose deploy", 10)
	compose.InputBindings = map[string]any{
		"applicationVersion": map[string]any{
			"composeFile": "services:\n  web:\n    image: registry.example.com/app:latest\n",
			"registryAuth": map[string]any{
				"registry.example.com": map[string]any{
					"username":          "deploy-user",
					"passwordSecretRef": "docker_password",
				},
			},
		},
		"projectName": "task-lease-registry-secret",
	}
	deps := createReadyDeploymentPlanForTaskLeaseWithSteps(t, ctx, []types.DeploymentProcessStep{compose})
	_, err := internalctx.GetDb(ctx).Exec(
		ctx,
		`INSERT INTO Secret (organization_id, key, value, updated_by_useraccount_id)
		VALUES (@organizationId, @key, @value, @updatedBy)`,
		pgx.NamedArgs{
			"organizationId": deps.orgID,
			"key":            "docker_password",
			"value":          "super-secret-password",
			"updatedBy":      deps.actorID,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	planPayload, err := json.Marshal(deps.plan.Steps[0].InputBindings)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(planPayload)).NotTo(ContainSubstring("super-secret-password"))
	g.Expect(string(planPayload)).To(ContainSubstring("passwordSecretRef"))
	g.Expect(deps.plan.ProcessSnapshotID).NotTo(BeNil())
	snapshot, err := db.GetProcessSnapshot(ctx, *deps.plan.ProcessSnapshotID, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	snapshotPayload, err := json.Marshal(snapshot.Revision.Steps[0].InputBindings)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(snapshotPayload)).NotTo(ContainSubstring("super-secret-password"))
	g.Expect(string(snapshotPayload)).To(ContainSubstring("passwordSecretRef"))
	fetchedPlan, err := db.GetDeploymentPlan(ctx, deps.plan.ID, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	fetchedPayload, err := json.Marshal(fetchedPlan.Steps[0].InputBindings)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(fetchedPayload)).NotTo(ContainSubstring("super-secret-password"))
	g.Expect(string(fetchedPayload)).To(ContainSubstring("passwordSecretRef"))
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(lease).NotTo(BeNil())
	g.Expect(lease.Steps).To(HaveLen(1))
	g.Expect(lease.Steps[0].SecretReferences).To(ContainElement("secret:docker_password"))
	auth := lease.Steps[0].InputBindings["applicationVersion"].(map[string]any)["registryAuth"].(map[string]any)["registry.example.com"].(map[string]any)
	g.Expect(auth).To(HaveKeyWithValue("username", "deploy-user"))
	g.Expect(auth).To(HaveKeyWithValue("password", "super-secret-password"))
	g.Expect(auth).NotTo(HaveKey("passwordSecretRef"))
}

func TestTaskLeaseRepositoryResolvesOCIJobSecretEnvironmentOnlyForAgentLease(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	job := taskLeaseOCIJobStep("job", "OCI job", 10)
	deps := createReadyDeploymentPlanForTaskLeaseWithSteps(t, ctx, []types.DeploymentProcessStep{job})
	_, err := internalctx.GetDb(ctx).Exec(
		ctx,
		`INSERT INTO Secret (organization_id, key, value, updated_by_useraccount_id)
		VALUES (@organizationId, @key, @value, @updatedBy)`,
		pgx.NamedArgs{
			"organizationId": deps.orgID,
			"key":            "job_api_token",
			"value":          "super-secret-job-token",
			"updatedBy":      deps.actorID,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	planPayload, err := json.Marshal(deps.plan.Steps[0].InputBindings)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(planPayload)).NotTo(ContainSubstring("super-secret-job-token"))
	g.Expect(string(planPayload)).To(ContainSubstring("secretEnvironment"))
	g.Expect(string(planPayload)).To(ContainSubstring("job_api_token"))
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(lease).NotTo(BeNil())
	g.Expect(lease.Steps).To(HaveLen(1))
	g.Expect(lease.Steps[0].SecretReferences).To(ContainElement("secret:job_api_token"))
	environment := lease.Steps[0].InputBindings["environment"].(map[string]any)
	g.Expect(environment).To(HaveKeyWithValue("MODE", "once"))
	g.Expect(environment).NotTo(HaveKey("API_TOKEN"))
	secretEnvironment := lease.Steps[0].InputBindings["secretEnvironment"].(map[string]any)
	g.Expect(secretEnvironment).To(HaveKeyWithValue("API_TOKEN", "super-secret-job-token"))
}

func TestTaskLeaseRepositoryResolvesFileRenderSecretVariablesOnlyForAgentLease(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	render := taskLeaseFileRenderStep("render", "Render config", 10)
	deps := createReadyDeploymentPlanForTaskLeaseWithSteps(t, ctx, []types.DeploymentProcessStep{render})
	_, err := internalctx.GetDb(ctx).Exec(
		ctx,
		`INSERT INTO Secret (organization_id, key, value, updated_by_useraccount_id)
		VALUES (@organizationId, @key, @value, @updatedBy)`,
		pgx.NamedArgs{
			"organizationId": deps.orgID,
			"key":            "render_api_token",
			"value":          "super-secret-render-token",
			"updatedBy":      deps.actorID,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	planPayload, err := json.Marshal(deps.plan.Steps[0].InputBindings)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(planPayload)).NotTo(ContainSubstring("super-secret-render-token"))
	g.Expect(string(planPayload)).To(ContainSubstring("secretVariables"))
	g.Expect(string(planPayload)).To(ContainSubstring("render_api_token"))
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(lease).NotTo(BeNil())
	g.Expect(lease.Steps).To(HaveLen(1))
	g.Expect(lease.Steps[0].SecretReferences).To(ContainElement("secret:render_api_token"))
	variables := lease.Steps[0].InputBindings["variables"].(map[string]any)
	g.Expect(variables).To(HaveKeyWithValue("apiUrl", "https://api.example.com"))
	g.Expect(variables).NotTo(HaveKey("apiToken"))
	secretVariables := lease.Steps[0].InputBindings["secretVariables"].(map[string]any)
	g.Expect(secretVariables).To(HaveKeyWithValue("apiToken", "super-secret-render-token"))
}

func TestTaskLeaseRepositoryResolvesWebhookSecretsOnlyForAgentLease(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	webhook := taskLeaseWebhookStep("webhook", "Notify webhook", 10)
	deps := createReadyDeploymentPlanForTaskLeaseWithSteps(t, ctx, []types.DeploymentProcessStep{webhook})
	_, err := internalctx.GetDb(ctx).Exec(
		ctx,
		`INSERT INTO Secret (organization_id, key, value, updated_by_useraccount_id)
		VALUES
			(@organizationId, @authKey, @authValue, @updatedBy),
			(@organizationId, @signingKey, @signingValue, @updatedBy)`,
		pgx.NamedArgs{
			"organizationId": deps.orgID,
			"authKey":        "webhook_auth_token",
			"authValue":      "Bearer super-secret-webhook-token",
			"signingKey":     "webhook_signing_key",
			"signingValue":   "super-secret-signing-key",
			"updatedBy":      deps.actorID,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	planPayload, err := json.Marshal(deps.plan.Steps[0].InputBindings)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(planPayload)).NotTo(ContainSubstring("super-secret-webhook-token"))
	g.Expect(string(planPayload)).NotTo(ContainSubstring("super-secret-signing-key"))
	g.Expect(string(planPayload)).To(ContainSubstring("webhook_auth_token"))
	g.Expect(string(planPayload)).To(ContainSubstring("webhook_signing_key"))
	g.Expect(deps.plan.ProcessSnapshotID).NotTo(BeNil())
	snapshot, err := db.GetProcessSnapshot(ctx, *deps.plan.ProcessSnapshotID, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	snapshotPayload, err := json.Marshal(snapshot.Revision.Steps[0].InputBindings)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(snapshotPayload)).NotTo(ContainSubstring("super-secret-webhook-token"))
	g.Expect(string(snapshotPayload)).NotTo(ContainSubstring("super-secret-signing-key"))
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(lease).NotTo(BeNil())
	g.Expect(lease.Steps).To(HaveLen(1))
	g.Expect(lease.Steps[0].SecretReferences).To(ContainElement("secret:webhook_auth_token"))
	g.Expect(lease.Steps[0].SecretReferences).To(ContainElement("secret:webhook_signing_key"))
	headers := lease.Steps[0].InputBindings["headers"].(map[string]any)
	g.Expect(headers).To(HaveKeyWithValue("X-Deployment", "demo"))
	g.Expect(headers).NotTo(HaveKey("Authorization"))
	secretHeaders := lease.Steps[0].InputBindings["secretHeaders"].(map[string]any)
	g.Expect(secretHeaders).To(HaveKeyWithValue("Authorization", "Bearer super-secret-webhook-token"))
	g.Expect(lease.Steps[0].InputBindings).To(HaveKeyWithValue("signingSecret", "super-secret-signing-key"))
}

func TestTaskLeaseRepositoryReturnsNilWhenNoQueuedTaskForAgent(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	orgID := createReleaseBundleTestOrganization(t, ctx)
	targetID := createReleaseBundleDockerTargetForOrganization(t, ctx, orgID, "cluster-a")

	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: orgID,
		AgentID:        targetID,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(lease).To(BeNil())
}

func TestTaskLeaseRepositorySkipsHubOnlyTask(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "cluster-a")
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(lease).To(BeNil())
}

func TestTaskLeaseRepositoryDoesNotClaimTaskForAnotherOrganization(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskLease(t, ctx)
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	otherOrgID := createReleaseBundleTestOrganization(t, ctx)

	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: otherOrgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(lease).To(BeNil())
}

func TestTaskLeaseRepositoryHeartbeatsActiveLease(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskLease(t, ctx)
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	time.Sleep(10 * time.Millisecond)

	heartbeat, err := db.HeartbeatAgentTaskLease(ctx, types.HeartbeatAgentTaskLeaseRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
		TaskID:         tasks[0].ID,
		LeaseToken:     lease.LeaseToken,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(heartbeat.ID).To(Equal(lease.ID))
	g.Expect(heartbeat.HeartbeatAt).To(BeTemporally(">", lease.HeartbeatAt))
	g.Expect(heartbeat.ExpiresAt).To(BeTemporally(">", lease.ExpiresAt))
}

func TestTaskLeaseRepositoryRejectsExpiredHeartbeat(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskLease(t, ctx)
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	expireTaskLeaseForTest(t, ctx, lease.ID)

	_, err = db.HeartbeatAgentTaskLease(ctx, types.HeartbeatAgentTaskLeaseRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
		TaskID:         tasks[0].ID,
		LeaseToken:     lease.LeaseToken,
	})

	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
}

func TestTaskLeaseRepositoryReclaimsExpiredLeaseWithNewAttempt(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskLease(t, ctx)
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	first, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	expireTaskLeaseForTest(t, ctx, first.ID)

	second, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(second).NotTo(BeNil())
	g.Expect(second.TaskID).To(Equal(first.TaskID))
	g.Expect(second.ID).NotTo(Equal(first.ID))
	g.Expect(second.LeaseToken).NotTo(Equal(first.LeaseToken))
	g.Expect(second.Attempt).To(Equal(2))
	g.Expect(countActiveTaskLeasesForTest(t, ctx, tasks[0].ID)).To(Equal(1))
	g.Expect(second.Task.Status).To(Equal(types.TaskStatusRunning))
}

func TestTaskLeaseRepositoryReclaimSkipsSucceededStepRuns(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskLeaseWithSteps(t, ctx, []types.DeploymentProcessStep{
		taskLeaseHTTPCheckStep("prepare", "Prepare", 10),
		taskLeaseHTTPCheckStep("deploy", "Deploy", 20),
	})
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	first, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(first.Steps).To(HaveLen(2))
	recordTaskLeaseStepEventForTest(
		t,
		ctx,
		deps.orgID,
		tasks[0].DeploymentTargetID,
		first.Steps[0].StepRunID,
		first.LeaseToken,
		1,
		types.StepRunEventTypeStarted,
	)
	recordTaskLeaseStepEventForTest(
		t,
		ctx,
		deps.orgID,
		tasks[0].DeploymentTargetID,
		first.Steps[0].StepRunID,
		first.LeaseToken,
		2,
		types.StepRunEventTypeSucceeded,
	)
	expireTaskLeaseForTest(t, ctx, first.ID)

	second, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(second).NotTo(BeNil())
	g.Expect(second.Attempt).To(Equal(2))
	g.Expect(second.Steps).To(HaveLen(1))
	g.Expect(second.Steps[0].StepKey).To(Equal("deploy"))
	g.Expect(second.Steps[0].StepRunID).To(Equal(first.Steps[1].StepRunID))
	recordTaskLeaseStepEventForTest(
		t,
		ctx,
		deps.orgID,
		tasks[0].DeploymentTargetID,
		second.Steps[0].StepRunID,
		second.LeaseToken,
		1,
		types.StepRunEventTypeStarted,
	)
}

func TestTaskLeaseRepositoryReclaimResetsInterruptedRunningStepRunForRetry(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskLeaseWithSteps(t, ctx, []types.DeploymentProcessStep{
		taskLeaseComposeDeployStep("compose", "Compose deploy", 10),
	})
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	first, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(first.Steps).To(HaveLen(1))
	recordTaskLeaseStepEventForTest(
		t,
		ctx,
		deps.orgID,
		tasks[0].DeploymentTargetID,
		first.Steps[0].StepRunID,
		first.LeaseToken,
		1,
		types.StepRunEventTypeStarted,
	)
	expireTaskLeaseForTest(t, ctx, first.ID)

	second, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(second).NotTo(BeNil())
	g.Expect(second.Attempt).To(Equal(2))
	g.Expect(second.Steps).To(HaveLen(1))
	g.Expect(second.Steps[0].StepRunID).To(Equal(first.Steps[0].StepRunID))

	recordTaskLeaseStepEventForTest(
		t,
		ctx,
		deps.orgID,
		tasks[0].DeploymentTargetID,
		second.Steps[0].StepRunID,
		second.LeaseToken,
		1,
		types.StepRunEventTypeStarted,
	)
}

func TestTaskLeaseRepositoryReclaimDoesNotResetRunningHubStepRun(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	hubStep := taskLeaseHTTPCheckStep("hub-prepare", "Hub prepare", 10)
	hubStep.ExecutionLocation = "hub"
	deps := createReadyDeploymentPlanForTaskLeaseWithSteps(t, ctx, []types.DeploymentProcessStep{
		hubStep,
		taskLeaseComposeDeployStep("compose", "Compose deploy", 20),
	})
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	hubRun := taskLeaseStepRunByKeyForTest(t, tasks[0], "hub-prepare")
	composeRun := taskLeaseStepRunByKeyForTest(t, tasks[0], "compose")
	first, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(first.Steps).To(HaveLen(1))
	g.Expect(first.Steps[0].StepRunID).To(Equal(composeRun.ID))
	_, err = db.TransitionStepRunState(ctx, types.TransitionStepRunStateRequest{
		OrganizationID: deps.orgID,
		StepRunID:      hubRun.ID,
		Status:         types.StepRunStatusRunning,
	})
	g.Expect(err).NotTo(HaveOccurred())
	recordTaskLeaseStepEventForTest(
		t,
		ctx,
		deps.orgID,
		tasks[0].DeploymentTargetID,
		composeRun.ID,
		first.LeaseToken,
		1,
		types.StepRunEventTypeStarted,
	)
	expireTaskLeaseForTest(t, ctx, first.ID)

	second, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(second).NotTo(BeNil())
	fetched, err := db.GetTask(ctx, tasks[0].ID, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(taskLeaseStepRunByKeyForTest(t, *fetched, "hub-prepare").Status).To(Equal(types.StepRunStatusRunning))
	g.Expect(taskLeaseStepRunByKeyForTest(t, *fetched, "compose").Status).To(Equal(types.StepRunStatusPending))
	recordTaskLeaseStepEventForTest(
		t,
		ctx,
		deps.orgID,
		tasks[0].DeploymentTargetID,
		second.Steps[0].StepRunID,
		second.LeaseToken,
		1,
		types.StepRunEventTypeStarted,
	)
}

func TestTaskLeaseRepositoryOrdersTargetStepsByDependencies(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	compose := taskLeaseComposeDeployStep("compose", "Compose deploy", 10)
	compose.Dependencies = []string{"prepare"}
	deps := createReadyDeploymentPlanForTaskLeaseWithSteps(t, ctx, []types.DeploymentProcessStep{
		compose,
		taskLeaseHTTPCheckStep("prepare", "Prepare", 20),
	})
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(lease).NotTo(BeNil())
	g.Expect(lease.Steps).To(HaveLen(2))
	g.Expect(lease.Steps[0].StepKey).To(Equal("prepare"))
	g.Expect(lease.Steps[1].StepKey).To(Equal("compose"))
}

func TestTaskLeaseRepositoryWaitsForHubDependencyBeforeLeasingTargetStep(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	hubStep := taskLeaseHTTPCheckStep("hub-prepare", "Hub prepare", 10)
	hubStep.ExecutionLocation = "hub"
	compose := taskLeaseComposeDeployStep("compose", "Compose deploy", 20)
	compose.Dependencies = []string{"hub-prepare"}
	deps := createReadyDeploymentPlanForTaskLeaseWithSteps(t, ctx, []types.DeploymentProcessStep{
		hubStep,
		compose,
	})
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	hubRun := taskLeaseStepRunByKeyForTest(t, tasks[0], "hub-prepare")

	blocked, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(blocked).To(BeNil())
	g.Expect(countActiveTaskLeasesForTest(t, ctx, tasks[0].ID)).To(Equal(0))
	fetched, err := db.GetTask(ctx, tasks[0].ID, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(fetched.Status).To(Equal(types.TaskStatusQueued))

	_, err = db.TransitionStepRunState(ctx, types.TransitionStepRunStateRequest{
		OrganizationID: deps.orgID,
		StepRunID:      hubRun.ID,
		Status:         types.StepRunStatusRunning,
	})
	g.Expect(err).NotTo(HaveOccurred())
	_, err = db.TransitionStepRunState(ctx, types.TransitionStepRunStateRequest{
		OrganizationID: deps.orgID,
		StepRunID:      hubRun.ID,
		Status:         types.StepRunStatusSucceeded,
	})
	g.Expect(err).NotTo(HaveOccurred())

	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(lease).NotTo(BeNil())
	g.Expect(lease.Steps).To(HaveLen(1))
	g.Expect(lease.Steps[0].StepKey).To(Equal("compose"))
}

func TestTaskLeaseRepositoryDoesNotClaimWhenExclusiveLockIsHeld(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskLease(t, ctx)
	secondPlan := createReadyDeploymentPlanForTaskQueueWithTargets(
		t,
		ctx,
		deps,
		"Task lease queued behind running",
		deps.plan.Targets[0].DeploymentTargetID,
	)
	firstTasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	_, err = db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   secondPlan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	firstLease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        firstTasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	secondLease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        firstLease.AgentID,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(secondLease).To(BeNil())
}

func TestTaskLeaseRepositoryDoesNotClaimWhenReleaseBundleIsBlocked(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskLease(t, ctx)
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	blocked, err := db.BlockReleaseBundle(ctx, deps.plan.ReleaseBundleID, deps.orgID, deps.actorID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(blocked.Status).To(Equal(types.ReleaseBundleStatusBlocked))

	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(lease).To(BeNil())
	fetched, err := db.GetTask(ctx, tasks[0].ID, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(fetched.Status).To(Equal(types.TaskStatusQueued))
}

func TestTaskLeaseRepositoryDoesNotReclaimExpiredLeaseWhenReleaseBundleIsBlocked(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskLease(t, ctx)
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	first, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	expireTaskLeaseForTest(t, ctx, first.ID)
	blocked, err := db.BlockReleaseBundle(ctx, deps.plan.ReleaseBundleID, deps.orgID, deps.actorID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(blocked.Status).To(Equal(types.ReleaseBundleStatusBlocked))

	second, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(second).To(BeNil())
	g.Expect(countActiveTaskLeasesForTest(t, ctx, tasks[0].ID)).To(Equal(1))
}

func TestTaskLeaseRepositoryWaitsForConcurrentReleaseBundleBlockBeforeClaim(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskLease(t, ctx)
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	tx := lockAndBlockReleaseBundleForTest(t, ctx, deps.plan.ReleaseBundleID)
	resultCh := make(chan taskLeaseAsyncResult, 1)

	go func() {
		lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
			OrganizationID: deps.orgID,
			AgentID:        tasks[0].DeploymentTargetID,
		})
		resultCh <- taskLeaseAsyncResult{lease: lease, err: err}
	}()

	assertTaskLeaseOperationIsWaiting(t, resultCh)
	g.Expect(tx.Commit(ctx)).To(Succeed())
	result := awaitTaskLeaseOperation(t, resultCh)
	g.Expect(result.err).NotTo(HaveOccurred())
	g.Expect(result.lease).To(BeNil())
	fetched, err := db.GetTask(ctx, tasks[0].ID, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(fetched.Status).To(Equal(types.TaskStatusQueued))
}

func TestTaskLeaseRepositoryTerminalTaskReleasesLeaseAndRejectsHeartbeat(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskLease(t, ctx)
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	terminal, err := db.TransitionTaskState(ctx, types.TransitionTaskStateRequest{
		OrganizationID: deps.orgID,
		TaskID:         tasks[0].ID,
		Status:         types.TaskStatusSucceeded,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(terminal.Status).To(Equal(types.TaskStatusSucceeded))
	g.Expect(countActiveTaskLeasesForTest(t, ctx, tasks[0].ID)).To(Equal(0))

	_, err = db.HeartbeatAgentTaskLease(ctx, types.HeartbeatAgentTaskLeaseRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
		TaskID:         tasks[0].ID,
		LeaseToken:     lease.LeaseToken,
	})

	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
}

func TestTaskLeaseRepositoryHeartbeatWaitsForConcurrentTerminalTask(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskLease(t, ctx)
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	tx := lockAndCompleteTaskForTest(t, ctx, tasks[0].ID, deps.orgID)
	resultCh := make(chan taskLeaseAsyncResult, 1)

	go func() {
		heartbeat, err := db.HeartbeatAgentTaskLease(ctx, types.HeartbeatAgentTaskLeaseRequest{
			OrganizationID: deps.orgID,
			AgentID:        tasks[0].DeploymentTargetID,
			TaskID:         tasks[0].ID,
			LeaseToken:     lease.LeaseToken,
		})
		resultCh <- taskLeaseAsyncResult{lease: heartbeat, err: err}
	}()

	assertTaskLeaseOperationIsWaiting(t, resultCh)
	g.Expect(tx.Commit(ctx)).To(Succeed())
	result := awaitTaskLeaseOperation(t, resultCh)
	g.Expect(errors.Is(result.err, apierrors.ErrNotFound)).To(BeTrue())
	g.Expect(result.lease).To(BeNil())
	g.Expect(countActiveTaskLeasesForTest(t, ctx, tasks[0].ID)).To(Equal(0))
}

func TestTaskLeaseMigrationDefinesLeaseTables(t *testing.T) {
	g := NewWithT(t)
	sql, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "124_task_leases.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	upSQL := string(sql)
	g.Expect(upSQL).To(ContainSubstring("CREATE TABLE TaskLease"))
	g.Expect(upSQL).To(ContainSubstring("lease_token_hash"))
	g.Expect(upSQL).To(ContainSubstring("FOREIGN KEY (task_id, organization_id)"))
	g.Expect(upSQL).To(ContainSubstring("FOREIGN KEY (agent_id, organization_id)"))
	g.Expect(upSQL).To(ContainSubstring("UNIQUE INDEX TaskLease_active_task"))

	down, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "124_task_leases.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	downSQL := string(down)
	g.Expect(downSQL).To(ContainSubstring("DROP TABLE IF EXISTS TaskLease"))
}

func taskLeaseDBTestContext(t *testing.T) context.Context {
	t.Helper()
	return taskQueueDBTestContext(t)
}

func createReadyDeploymentPlanForTaskLease(t *testing.T, ctx context.Context) taskQueuePlanDeps {
	t.Helper()
	return createReadyDeploymentPlanForTaskLeaseWithSteps(t, ctx, []types.DeploymentProcessStep{
		taskLeaseHTTPCheckStep("deploy", "Deploy", 10),
	})
}

func createReadyDeploymentPlanForTaskLeaseWithSteps(
	t *testing.T,
	ctx context.Context,
	steps []types.DeploymentProcessStep,
) taskQueuePlanDeps {
	t.Helper()
	g := NewWithT(t)
	deps := createReleaseBundleEligibilityDependencies(t, ctx)
	process := types.DeploymentProcess{
		OrganizationID: deps.orgID,
		ApplicationID:  deps.applicationID,
		Name:           "Task lease deploy " + uuid.NewString(),
	}
	g.Expect(db.CreateDeploymentProcess(ctx, &process)).To(Succeed())
	revision := types.DeploymentProcessRevision{
		OrganizationID:      deps.orgID,
		DeploymentProcessID: process.ID,
		Description:         "Initial revision",
		Steps:               steps,
	}
	g.Expect(db.CreateDeploymentProcessRevision(ctx, &revision)).To(Succeed())
	createDeploymentPlanVariableSet(t, ctx, deps.orgID, deps.applicationID)
	targetID := createReleaseBundleDockerTargetForOrganization(t, ctx, deps.orgID, "cluster-a")
	actorID := createReleaseBundleTestUser(t, ctx, deps.orgID)
	bundle := releaseBundleFixture(deps.orgID, deps.applicationID, deps.channelID, deps.versionID)
	bundle.DeploymentProcessRevisionID = &revision.ID
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())
	published, result, err := db.PublishReleaseBundle(ctx, bundle.ID, deps.orgID, actorID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Valid).To(BeTrue())
	plan, err := db.CreateDeploymentPlan(ctx, types.CreateDeploymentPlanRequest{
		OrganizationID:  deps.orgID,
		ReleaseBundleID: published.ID,
		EnvironmentID:   deps.devEnvironmentID,
		TargetIDs:       []uuid.UUID{targetID},
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(plan.Status).To(Equal(types.DeploymentPlanStatusReady))
	return taskQueuePlanDeps{
		orgID:            deps.orgID,
		applicationID:    deps.applicationID,
		channelID:        deps.channelID,
		versionID:        deps.versionID,
		devEnvironmentID: deps.devEnvironmentID,
		actorID:          actorID,
		plan:             plan,
	}
}

func taskLeaseHTTPCheckStep(key, name string, sortOrder int) types.DeploymentProcessStep {
	return types.DeploymentProcessStep{
		Key:                  key,
		Name:                 name,
		ActionType:           "distr.http.check",
		ExecutionLocation:    "target",
		InputBindings:        map[string]any{"url": "https://example.com/health"},
		FailureMode:          "fail",
		TimeoutSeconds:       120,
		RetryMaxAttempts:     3,
		RetryIntervalSeconds: 10,
		RequiredPermissions:  []string{"deploy:write"},
		SortOrder:            sortOrder,
	}
}

func taskLeaseComposeDeployStep(key, name string, sortOrder int) types.DeploymentProcessStep {
	return types.DeploymentProcessStep{
		Key:               key,
		Name:              name,
		ActionType:        "distr.compose.deploy",
		ExecutionLocation: "target",
		InputBindings: map[string]any{
			"applicationVersion": map[string]any{
				"composeFile": "services:\n  web:\n    image: nginx:alpine\n",
			},
			"projectName": "task-lease-retry",
		},
		FailureMode:          "fail",
		TimeoutSeconds:       120,
		RetryMaxAttempts:     3,
		RetryIntervalSeconds: 10,
		RequiredPermissions:  []string{"deploy:write"},
		SortOrder:            sortOrder,
	}
}

func taskLeaseOCIJobStep(key, name string, sortOrder int) types.DeploymentProcessStep {
	return types.DeploymentProcessStep{
		Key:               key,
		Name:              name,
		ActionType:        "distr.oci.job",
		ExecutionLocation: "target",
		InputBindings: map[string]any{
			"imageDigest": "registry.example.com/jobs/cleanup@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"command":     []any{"/bin/cleanup"},
			"arguments":   []any{"--tenant", "demo"},
			"environment": map[string]any{
				"MODE": "once",
			},
			"secretEnvironment": map[string]any{
				"API_TOKEN": "job_api_token",
			},
			"network":           "none",
			"timeoutSeconds":    60,
			"expectedExitCodes": []any{0},
		},
		FailureMode:          "fail",
		TimeoutSeconds:       120,
		RetryMaxAttempts:     3,
		RetryIntervalSeconds: 10,
		RequiredPermissions:  []string{"deploy:write"},
		SortOrder:            sortOrder,
	}
}

func taskLeaseFileRenderStep(key, name string, sortOrder int) types.DeploymentProcessStep {
	return types.DeploymentProcessStep{
		Key:               key,
		Name:              name,
		ActionType:        "distr.file.render",
		ExecutionLocation: "target",
		InputBindings: map[string]any{
			"destinationPath": "app/config/runtime.env",
			"template":        "API_URL=${apiUrl}\nAPI_TOKEN=${secrets.apiToken}\n",
			"variables": map[string]any{
				"apiUrl": "https://api.example.com",
			},
			"secretVariables": map[string]any{
				"apiToken": "render_api_token",
			},
			"mode":   "0640",
			"backup": true,
		},
		FailureMode:          "fail",
		TimeoutSeconds:       120,
		RetryMaxAttempts:     3,
		RetryIntervalSeconds: 10,
		RequiredPermissions:  []string{"deploy:write"},
		SortOrder:            sortOrder,
	}
}

func taskLeaseWebhookStep(key, name string, sortOrder int) types.DeploymentProcessStep {
	return types.DeploymentProcessStep{
		Key:               key,
		Name:              name,
		ActionType:        "distr.webhook",
		ExecutionLocation: "target",
		InputBindings: map[string]any{
			"url":     "https://hooks.example.com/deployments",
			"method":  "POST",
			"headers": map[string]any{"X-Deployment": "demo"},
			"secretHeaders": map[string]any{
				"Authorization": "webhook_auth_token",
			},
			"body": map[string]any{
				"deploymentId": "dep-123",
			},
			"signingSecret":       "webhook_signing_key",
			"timeoutSeconds":      30,
			"expectedStatusCodes": []any{200, 202},
			"outputs": []any{
				map[string]any{"name": "remoteId", "pointer": "/id", "type": "string", "required": true},
			},
		},
		FailureMode:          "fail",
		TimeoutSeconds:       120,
		RetryMaxAttempts:     3,
		RetryIntervalSeconds: 10,
		RequiredPermissions:  []string{"deploy:write"},
		SortOrder:            sortOrder,
	}
}

func recordTaskLeaseStepEventForTest(
	t *testing.T,
	ctx context.Context,
	orgID uuid.UUID,
	agentID uuid.UUID,
	stepRunID uuid.UUID,
	leaseToken string,
	sequence int64,
	eventType types.StepRunEventType,
) {
	t.Helper()
	g := NewWithT(t)
	_, err := db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: orgID,
		AgentID:        agentID,
		StepRunID:      stepRunID,
		LeaseToken:     leaseToken,
		Sequence:       sequence,
		Type:           eventType,
		Message:        string(eventType),
	})
	g.Expect(err).NotTo(HaveOccurred())
}

func taskLeaseStepRunByKeyForTest(t *testing.T, task types.Task, key string) types.StepRun {
	t.Helper()
	for _, stepRun := range task.StepRuns {
		if stepRun.StepKey == key {
			return stepRun
		}
	}
	t.Fatalf("step run %q not found", key)
	return types.StepRun{}
}

func expireTaskLeaseForTest(t *testing.T, ctx context.Context, leaseID uuid.UUID) {
	t.Helper()
	_, err := internalctx.GetDb(ctx).Exec(
		ctx,
		`UPDATE TaskLease
		SET expires_at = now() - interval '1 minute', heartbeat_at = now() - interval '2 minutes'
		WHERE id = @leaseId`,
		pgx.NamedArgs{"leaseId": leaseID},
	)
	if err != nil {
		t.Fatalf("expire task lease: %v", err)
	}
}

func countActiveTaskLeasesForTest(t *testing.T, ctx context.Context, taskID uuid.UUID) int {
	t.Helper()
	var count int
	err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`SELECT count(*) FROM TaskLease WHERE task_id = @taskId AND released_at IS NULL`,
		pgx.NamedArgs{"taskId": taskID},
	).Scan(&count)
	if err != nil {
		t.Fatalf("count active task leases: %v", err)
	}
	return count
}

type taskLeaseAsyncResult struct {
	lease *types.TaskLease
	err   error
}

func assertTaskLeaseOperationIsWaiting(t *testing.T, resultCh <-chan taskLeaseAsyncResult) {
	t.Helper()
	select {
	case result := <-resultCh:
		t.Fatalf("task lease operation completed before lock was released: lease=%v err=%v", result.lease, result.err)
	case <-time.After(200 * time.Millisecond):
	}
}

func awaitTaskLeaseOperation(t *testing.T, resultCh <-chan taskLeaseAsyncResult) taskLeaseAsyncResult {
	t.Helper()
	select {
	case result := <-resultCh:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("task lease operation did not finish after lock was released")
		return taskLeaseAsyncResult{}
	}
}

func lockAndCompleteTaskForTest(t *testing.T, ctx context.Context, taskID, orgID uuid.UUID) pgx.Tx {
	t.Helper()
	pool, ok := internalctx.GetDb(ctx).(*pgxpool.Pool)
	if !ok {
		t.Fatalf("test context db is %T, expected *pgxpool.Pool", internalctx.GetDb(ctx))
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	t.Cleanup(func() {
		_ = tx.Rollback(ctx)
	})
	if _, err := tx.Exec(
		ctx,
		`SELECT id FROM Task
		WHERE id = @taskId AND organization_id = @organizationId
		FOR UPDATE`,
		pgx.NamedArgs{"taskId": taskID, "organizationId": orgID},
	); err != nil {
		t.Fatalf("lock task row: %v", err)
	}
	if _, err := tx.Exec(
		ctx,
		`UPDATE Task
		SET status = @status, updated_at = now(), completed_at = now()
		WHERE id = @taskId AND organization_id = @organizationId`,
		pgx.NamedArgs{
			"taskId":         taskID,
			"organizationId": orgID,
			"status":         types.TaskStatusSucceeded,
		},
	); err != nil {
		t.Fatalf("complete task in transaction: %v", err)
	}
	if _, err := tx.Exec(
		ctx,
		`UPDATE TaskLease
		SET released_at = COALESCE(released_at, now()), updated_at = now()
		WHERE task_id = @taskId
			AND organization_id = @organizationId
			AND released_at IS NULL`,
		pgx.NamedArgs{"taskId": taskID, "organizationId": orgID},
	); err != nil {
		t.Fatalf("release task lease in transaction: %v", err)
	}
	return tx
}
