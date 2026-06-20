package db_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestDeploymentProcessRepositoryCRUDAndRevisionPersistence(t *testing.T) {
	ctx := deploymentProcessDBTestContext(t)
	g := NewWithT(t)
	deps := createDeploymentProcessDependencies(t, ctx)

	process := types.DeploymentProcess{
		OrganizationID: deps.orgID,
		ApplicationID:  deps.applicationID,
		Name:           " Standard deploy ",
		Description:    "Deploys through the standard lifecycle",
		SortOrder:      20,
	}
	g.Expect(db.CreateDeploymentProcess(ctx, &process)).To(Succeed())
	g.Expect(process.ID).NotTo(Equal(uuid.Nil))
	g.Expect(process.Name).To(Equal("Standard deploy"))

	process.Description = "Updated description"
	process.SortOrder = 10
	g.Expect(db.UpdateDeploymentProcess(ctx, &process)).To(Succeed())

	loaded, err := db.GetDeploymentProcess(ctx, process.ID, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(loaded.Description).To(Equal("Updated description"))
	g.Expect(loaded.SortOrder).To(Equal(10))

	processes, err := db.GetDeploymentProcessesByOrganizationID(ctx, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(processes).To(HaveLen(1))
	g.Expect(processes[0].ID).To(Equal(process.ID))

	revision := deploymentProcessRevisionFixture(deps)
	revision.DeploymentProcessID = process.ID
	g.Expect(db.CreateDeploymentProcessRevision(ctx, &revision)).To(Succeed())
	g.Expect(revision.ID).NotTo(Equal(uuid.Nil))
	g.Expect(revision.RevisionNumber).To(Equal(1))
	g.Expect(revision.Steps).To(HaveLen(2))
	g.Expect(revision.Steps[0].Key).To(Equal("prepare"))
	g.Expect(revision.Steps[1].Key).To(Equal("deploy"))
	g.Expect(revision.Steps[1].Dependencies).To(Equal([]string{"prepare"}))
	g.Expect(revision.Steps[1].ChannelIDs).To(Equal([]uuid.UUID{deps.channelID}))
	g.Expect(revision.Steps[1].EnvironmentIDs).To(Equal([]uuid.UUID{deps.environmentID}))
	g.Expect(revision.Steps[1].InputBindings).To(HaveKeyWithValue("script", "make deploy"))

	secondRevision := deploymentProcessRevisionFixture(deps)
	secondRevision.DeploymentProcessID = process.ID
	secondRevision.Description = "second"
	g.Expect(db.CreateDeploymentProcessRevision(ctx, &secondRevision)).To(Succeed())
	g.Expect(secondRevision.RevisionNumber).To(Equal(2))

	revisions, err := db.GetDeploymentProcessRevisions(ctx, process.ID, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(revisions).To(HaveLen(2))
	g.Expect(revisions[0].RevisionNumber).To(Equal(1))
	g.Expect(revisions[1].RevisionNumber).To(Equal(2))

	loadedRevision, err := db.GetDeploymentProcessRevision(ctx, process.ID, secondRevision.ID, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(loadedRevision.ID).To(Equal(secondRevision.ID))
	g.Expect(loadedRevision.Steps).To(HaveLen(2))

	g.Expect(db.DeleteDeploymentProcessWithID(ctx, process.ID, deps.orgID)).To(Succeed())
	_, err = db.GetDeploymentProcess(ctx, process.ID, deps.orgID)
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
}

func TestDeploymentProcessRepositoryRejectsDuplicateNamesWithinApplicationScope(t *testing.T) {
	ctx := deploymentProcessDBTestContext(t)
	g := NewWithT(t)
	deps := createDeploymentProcessDependencies(t, ctx)
	otherApplicationID := createChannelApplicationForOrganization(t, ctx, deps.orgID)

	first := types.DeploymentProcess{
		OrganizationID: deps.orgID,
		ApplicationID:  deps.applicationID,
		Name:           "Standard deploy",
	}
	g.Expect(db.CreateDeploymentProcess(ctx, &first)).To(Succeed())

	duplicate := types.DeploymentProcess{
		OrganizationID: deps.orgID,
		ApplicationID:  deps.applicationID,
		Name:           "Standard deploy",
	}
	err := db.CreateDeploymentProcess(ctx, &duplicate)
	g.Expect(errors.Is(err, apierrors.ErrAlreadyExists)).To(BeTrue())

	sameNameOtherApplication := types.DeploymentProcess{
		OrganizationID: deps.orgID,
		ApplicationID:  otherApplicationID,
		Name:           "Standard deploy",
	}
	g.Expect(db.CreateDeploymentProcess(ctx, &sameNameOtherApplication)).To(Succeed())
}

func TestDeploymentProcessRepositoryRejectsInvalidAndCrossOrganizationReferences(t *testing.T) {
	ctx := deploymentProcessDBTestContext(t)
	g := NewWithT(t)
	deps := createDeploymentProcessDependencies(t, ctx)
	otherDeps := createDeploymentProcessDependencies(t, ctx)
	otherApplicationID := createChannelApplicationForOrganization(t, ctx, deps.orgID)
	otherApplicationChannel := createDeploymentProcessChannel(t, ctx, deps.orgID, otherApplicationID, deps.lifecycleID)

	process := types.DeploymentProcess{
		OrganizationID: deps.orgID,
		ApplicationID:  deps.applicationID,
		Name:           "Standard deploy",
	}
	g.Expect(db.CreateDeploymentProcess(ctx, &process)).To(Succeed())

	invalidProcess := types.DeploymentProcess{
		OrganizationID: deps.orgID,
		ApplicationID:  otherDeps.applicationID,
		Name:           "Cross org process",
	}
	err := db.CreateDeploymentProcess(ctx, &invalidProcess)
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())

	tests := []struct {
		name        string
		orgID       uuid.UUID
		processID   uuid.UUID
		environment uuid.UUID
		channel     uuid.UUID
	}{
		{
			name:        "cross organization process",
			orgID:       otherDeps.orgID,
			processID:   process.ID,
			environment: otherDeps.environmentID,
			channel:     otherDeps.channelID,
		},
		{
			name:        "cross organization environment",
			orgID:       deps.orgID,
			processID:   process.ID,
			environment: otherDeps.environmentID,
			channel:     deps.channelID,
		},
		{
			name:        "cross organization channel",
			orgID:       deps.orgID,
			processID:   process.ID,
			environment: deps.environmentID,
			channel:     otherDeps.channelID,
		},
		{
			name:        "channel for another application",
			orgID:       deps.orgID,
			processID:   process.ID,
			environment: deps.environmentID,
			channel:     otherApplicationChannel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			revision := deploymentProcessRevisionFixture(deps)
			revision.OrganizationID = tt.orgID
			revision.DeploymentProcessID = tt.processID
			revision.Steps[1].EnvironmentIDs = []uuid.UUID{tt.environment}
			revision.Steps[1].ChannelIDs = []uuid.UUID{tt.channel}

			err := db.CreateDeploymentProcessRevision(ctx, &revision)

			g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
		})
	}
}

func TestDeploymentProcessRepositoryRejectsInvalidRevisionGraph(t *testing.T) {
	ctx := deploymentProcessDBTestContext(t)
	g := NewWithT(t)
	deps := createDeploymentProcessDependencies(t, ctx)
	process := types.DeploymentProcess{
		OrganizationID: deps.orgID,
		ApplicationID:  deps.applicationID,
		Name:           "Standard deploy",
	}
	g.Expect(db.CreateDeploymentProcess(ctx, &process)).To(Succeed())

	tests := []struct {
		name   string
		mutate func(*types.DeploymentProcessRevision)
	}{
		{
			name: "duplicate step key",
			mutate: func(r *types.DeploymentProcessRevision) {
				r.Steps = append(r.Steps, deploymentProcessStepFixture("deploy", 30))
			},
		},
		{
			name: "duplicate sort order",
			mutate: func(r *types.DeploymentProcessRevision) {
				r.Steps = append(r.Steps, deploymentProcessStepFixture("verify", 20))
			},
		},
		{
			name: "missing dependency",
			mutate: func(r *types.DeploymentProcessRevision) {
				r.Steps[1].Dependencies = []string{"missing"}
			},
		},
		{
			name: "self dependency",
			mutate: func(r *types.DeploymentProcessRevision) {
				r.Steps[1].Dependencies = []string{"deploy"}
			},
		},
		{
			name: "dependency cycle",
			mutate: func(r *types.DeploymentProcessRevision) {
				r.Steps[0].Dependencies = []string{"deploy"}
				r.Steps[1].Dependencies = []string{"prepare"}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			revision := deploymentProcessRevisionFixture(deps)
			revision.DeploymentProcessID = process.ID
			tt.mutate(&revision)

			err := db.CreateDeploymentProcessRevision(ctx, &revision)

			g.Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())
		})
	}
}

func TestDeploymentProcessMigrationDefinesProcessSchema(t *testing.T) {
	g := NewWithT(t)

	up, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "115_deployment_processes.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	sql := string(up)
	g.Expect(sql).To(ContainSubstring("CREATE TABLE DeploymentProcess"))
	g.Expect(sql).To(ContainSubstring("CREATE TABLE DeploymentProcessRevision"))
	g.Expect(sql).To(ContainSubstring("CREATE TABLE DeploymentProcessStep"))
	g.Expect(sql).To(ContainSubstring("CREATE TABLE DeploymentProcessStepDependency"))
	g.Expect(sql).To(ContainSubstring("CREATE TABLE DeploymentProcessStepChannel"))
	g.Expect(sql).To(ContainSubstring("CREATE TABLE DeploymentProcessStepEnvironment"))
	g.Expect(sql).To(ContainSubstring("deploymentprocess_organization_application_name_unique"))
	g.Expect(sql).To(ContainSubstring("deploymentprocessstep_revision_key_unique"))

	down, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "115_deployment_processes.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(down)).To(ContainSubstring("DROP TABLE IF EXISTS DeploymentProcessStepEnvironment"))
	g.Expect(string(down)).To(ContainSubstring("DROP TABLE IF EXISTS DeploymentProcess"))
}

type deploymentProcessDependencies struct {
	orgID         uuid.UUID
	applicationID uuid.UUID
	lifecycleID   uuid.UUID
	channelID     uuid.UUID
	environmentID uuid.UUID
}

func deploymentProcessDBTestContext(t *testing.T) context.Context {
	t.Helper()
	return channelDBTestContext(t)
}

func createDeploymentProcessDependencies(t *testing.T, ctx context.Context) deploymentProcessDependencies {
	t.Helper()
	orgID, applicationID, lifecycleID := createChannelDependencies(t, ctx)
	channelID := createDeploymentProcessChannel(t, ctx, orgID, applicationID, lifecycleID)
	environmentID := createDeploymentProcessEnvironment(t, ctx, orgID)
	return deploymentProcessDependencies{
		orgID:         orgID,
		applicationID: applicationID,
		lifecycleID:   lifecycleID,
		channelID:     channelID,
		environmentID: environmentID,
	}
}

func createDeploymentProcessEnvironment(t *testing.T, ctx context.Context, orgID uuid.UUID) uuid.UUID {
	t.Helper()
	environment := types.Environment{
		OrganizationID: orgID,
		Name:           "Environment " + uuid.NewString(),
	}
	if err := db.CreateEnvironment(ctx, &environment); err != nil {
		t.Fatalf("create environment: %v", err)
	}
	return environment.ID
}

func createDeploymentProcessChannel(
	t *testing.T,
	ctx context.Context,
	orgID uuid.UUID,
	applicationID uuid.UUID,
	lifecycleID uuid.UUID,
) uuid.UUID {
	t.Helper()
	channel := types.Channel{
		OrganizationID: orgID,
		ApplicationID:  applicationID,
		LifecycleID:    lifecycleID,
		Name:           "Channel " + uuid.NewString(),
	}
	if err := db.CreateChannel(ctx, &channel); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	return channel.ID
}

func deploymentProcessRevisionFixture(deps deploymentProcessDependencies) types.DeploymentProcessRevision {
	return types.DeploymentProcessRevision{
		OrganizationID: deps.orgID,
		Description:    "Initial revision",
		Steps: []types.DeploymentProcessStep{
			deploymentProcessStepFixture("prepare", 10),
			{
				Key:                  "deploy",
				Name:                 "Deploy",
				ActionType:           "script",
				ExecutionLocation:    "hub",
				InputBindings:        map[string]any{"script": "make deploy"},
				Condition:            "channel == stable",
				ChannelIDs:           []uuid.UUID{deps.channelID},
				EnvironmentIDs:       []uuid.UUID{deps.environmentID},
				TargetTags:           []string{"linux"},
				FailureMode:          "fail",
				TimeoutSeconds:       120,
				RetryMaxAttempts:     3,
				RetryIntervalSeconds: 10,
				RequiredPermissions:  []string{"deploy:write"},
				SortOrder:            20,
				Dependencies:         []string{"prepare"},
			},
		},
	}
}

func deploymentProcessStepFixture(key string, sortOrder int) types.DeploymentProcessStep {
	return types.DeploymentProcessStep{
		Key:               key,
		Name:              key,
		ActionType:        "script",
		ExecutionLocation: "hub",
		FailureMode:       "fail",
		SortOrder:         sortOrder,
	}
}
