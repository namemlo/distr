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

func TestRunbookRepositoryCreatesRevisionsAndPublishedSnapshots(t *testing.T) {
	ctx := runbookDBTestContext(t)
	g := NewWithT(t)
	deps := createDeploymentProcessDependencies(t, ctx)
	actorID := createReleaseBundleTestUser(t, ctx, deps.orgID)

	runbook := types.Runbook{
		OrganizationID: deps.orgID,
		ApplicationID:  deps.applicationID,
		Name:           " Rotate signing keys ",
		Description:    "Operational key rotation",
		SortOrder:      20,
	}
	g.Expect(db.CreateRunbook(ctx, &runbook)).To(Succeed())
	g.Expect(runbook.ID).NotTo(Equal(uuid.Nil))
	g.Expect(runbook.Name).To(Equal("Rotate signing keys"))

	revision := runbookRevisionFixture(deps)
	revision.RunbookID = runbook.ID
	g.Expect(db.CreateRunbookRevision(ctx, &revision)).To(Succeed())
	g.Expect(revision.RevisionNumber).To(Equal(1))
	g.Expect(revision.Steps).To(HaveLen(2))
	g.Expect(revision.Steps[1].Dependencies).To(Equal([]string{"prepare"}))

	snapshot, err := db.PublishRunbookRevision(ctx, runbook.ID, revision.ID, deps.orgID, actorID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(snapshot.RunbookRevisionID).To(Equal(revision.ID))
	g.Expect(snapshot.PublishedByUserAccountID).To(Equal(&actorID))
	g.Expect(snapshot.CanonicalChecksum).To(HavePrefix("sha256:"))
	g.Expect(snapshot.Revision.Steps).To(HaveLen(2))

	secondSnapshot, err := db.PublishRunbookRevision(ctx, runbook.ID, revision.ID, deps.orgID, actorID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(secondSnapshot.ID).To(Equal(snapshot.ID))
}

func TestRunbookRepositoryRejectsGitManagedPublish(t *testing.T) {
	ctx := runbookDBTestContext(t)
	g := NewWithT(t)
	deps := createDeploymentProcessDependencies(t, ctx)
	actorID := createReleaseBundleTestUser(t, ctx, deps.orgID)

	runbook := types.Runbook{
		OrganizationID: deps.orgID,
		ApplicationID:  deps.applicationID,
		Name:           "Rotate signing keys",
		Description:    "Operational key rotation",
		SortOrder:      20,
	}
	g.Expect(db.CreateRunbook(ctx, &runbook)).To(Succeed())

	revision := runbookRevisionFixture(deps)
	revision.RunbookID = runbook.ID
	g.Expect(db.CreateRunbookRevision(ctx, &revision)).To(Succeed())
	g.Expect(db.UpsertConfigAsCodeAuthority(ctx, &types.ConfigAsCodeAuthority{
		OrganizationID:   deps.orgID,
		ResourceKind:     types.ConfigAsCodeResourceKindRunbook,
		ResourceID:       runbook.ID,
		Authority:        types.ConfigAsCodeAuthorityGitManaged,
		RepositoryPath:   "runbooks/rotate-keys.yaml",
		SourceRevision:   "6dcb09f",
		DocumentChecksum: "sha256:1234",
		UpdatedByUserID:  &actorID,
	})).To(Succeed())

	_, err := db.PublishRunbookRevision(ctx, runbook.ID, revision.ID, deps.orgID, actorID)

	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
}

func TestRunbookRepositoryRejectsInvalidRevisionGraph(t *testing.T) {
	ctx := runbookDBTestContext(t)
	g := NewWithT(t)
	deps := createDeploymentProcessDependencies(t, ctx)
	runbook := types.Runbook{
		OrganizationID: deps.orgID,
		ApplicationID:  deps.applicationID,
		Name:           "Database maintenance",
	}
	g.Expect(db.CreateRunbook(ctx, &runbook)).To(Succeed())

	revision := runbookRevisionFixture(deps)
	revision.RunbookID = runbook.ID
	revision.Steps[1].Dependencies = []string{"missing"}

	err := db.CreateRunbookRevision(ctx, &revision)

	g.Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())
}

func TestRunbookMigrationDefinesRevisionSnapshotAndTaskTypeSchema(t *testing.T) {
	g := NewWithT(t)

	up, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "127_runbooks.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	sql := string(up)
	g.Expect(sql).To(ContainSubstring("CREATE TABLE Runbook"))
	g.Expect(sql).To(ContainSubstring("CREATE TABLE RunbookRevision"))
	g.Expect(sql).To(ContainSubstring("CREATE TABLE RunbookStep"))
	g.Expect(sql).To(ContainSubstring("CREATE TABLE RunbookSnapshot"))
	g.Expect(sql).To(ContainSubstring("ALTER TABLE Task"))
	g.Expect(sql).To(ContainSubstring("task_type"))
	g.Expect(sql).To(ContainSubstring("runbooksnapshot_revision_unique"))

	down, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "127_runbooks.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(down)).To(ContainSubstring("DROP TABLE IF EXISTS RunbookSnapshot"))
	g.Expect(string(down)).To(ContainSubstring("DROP TABLE IF EXISTS Runbook"))
}

func runbookDBTestContext(t *testing.T) context.Context {
	t.Helper()
	return deploymentProcessDBTestContext(t)
}

func runbookRevisionFixture(deps deploymentProcessDependencies) types.RunbookRevision {
	return types.RunbookRevision{
		OrganizationID: deps.orgID,
		Description:    "Initial runbook revision",
		Steps: []types.RunbookStep{
			runbookStepFixture("prepare", 10),
			{
				Key:                  "verify",
				Name:                 "Verify endpoint",
				ActionType:           "distr.http.check",
				ExecutionLocation:    "hub",
				InputBindings:        map[string]any{"url": "https://example.com/health"},
				Condition:            "always()",
				FailureMode:          "fail",
				TimeoutSeconds:       120,
				RetryMaxAttempts:     3,
				RetryIntervalSeconds: 10,
				RequiredPermissions:  []string{"runbook:execute"},
				SortOrder:            20,
				Dependencies:         []string{"prepare"},
			},
		},
	}
}

func runbookStepFixture(key string, sortOrder int) types.RunbookStep {
	return types.RunbookStep{
		Key:               key,
		Name:              key,
		ActionType:        "distr.preflight",
		InputBindings:     map[string]any{},
		ExecutionLocation: "hub",
		FailureMode:       "fail",
		SortOrder:         sortOrder,
	}
}
