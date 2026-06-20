package db_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/gomega"
)

func TestChannelRepositoryCRUDAndDefaultBehavior(t *testing.T) {
	ctx := channelDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, lifecycleID := createChannelDependencies(t, ctx)

	preview := types.Channel{
		OrganizationID: orgID,
		ApplicationID:  applicationID,
		LifecycleID:    lifecycleID,
		Name:           "Preview",
		SortOrder:      20,
	}
	g.Expect(db.CreateChannel(ctx, &preview)).To(Succeed())
	g.Expect(preview.IsDefault).To(BeTrue())

	hotfix := types.Channel{
		OrganizationID: orgID,
		ApplicationID:  applicationID,
		LifecycleID:    lifecycleID,
		Name:           "Hotfix",
		SortOrder:      30,
		IsDefault:      true,
	}
	g.Expect(db.CreateChannel(ctx, &hotfix)).To(Succeed())
	g.Expect(hotfix.IsDefault).To(BeTrue())

	updatedPreview, err := db.GetChannel(ctx, preview.ID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(updatedPreview.IsDefault).To(BeFalse())

	preview.IsDefault = true
	g.Expect(db.UpdateChannel(ctx, &preview)).To(Succeed())
	updatedHotfix, err := db.GetChannel(ctx, hotfix.ID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(updatedHotfix.IsDefault).To(BeFalse())

	g.Expect(db.DeleteChannelWithID(ctx, hotfix.ID, orgID)).To(Succeed())
	err = db.DeleteChannelWithID(ctx, preview.ID, orgID)
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
}

func TestChannelRepositoryRejectsInvalidAndCrossOrganizationReferences(t *testing.T) {
	ctx := channelDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, lifecycleID := createChannelDependencies(t, ctx)
	otherOrgID, otherApplicationID, otherLifecycleID := createChannelDependencies(t, ctx)

	tests := []struct {
		name           string
		organizationID uuid.UUID
		applicationID  uuid.UUID
		lifecycleID    uuid.UUID
	}{
		{
			name:           "missing application",
			organizationID: orgID,
			applicationID:  uuid.New(),
			lifecycleID:    lifecycleID,
		},
		{
			name:           "missing lifecycle",
			organizationID: orgID,
			applicationID:  applicationID,
			lifecycleID:    uuid.New(),
		},
		{
			name:           "cross-organization application",
			organizationID: orgID,
			applicationID:  otherApplicationID,
			lifecycleID:    lifecycleID,
		},
		{
			name:           "cross-organization lifecycle",
			organizationID: orgID,
			applicationID:  applicationID,
			lifecycleID:    otherLifecycleID,
		},
		{
			name:           "inverse cross-organization application",
			organizationID: otherOrgID,
			applicationID:  applicationID,
			lifecycleID:    otherLifecycleID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := types.Channel{
				OrganizationID: tt.organizationID,
				ApplicationID:  tt.applicationID,
				LifecycleID:    tt.lifecycleID,
				Name:           "Stable",
			}

			err := db.CreateChannel(ctx, &channel)

			g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
		})
	}
}

func TestChannelRepositoryRejectsDuplicateNamesWithinApplicationScope(t *testing.T) {
	ctx := channelDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, lifecycleID := createChannelDependencies(t, ctx)
	otherApplicationID, _ := createChannelDependenciesForOrganization(t, ctx, orgID)

	first := types.Channel{
		OrganizationID: orgID,
		ApplicationID:  applicationID,
		LifecycleID:    lifecycleID,
		Name:           "Stable",
	}
	g.Expect(db.CreateChannel(ctx, &first)).To(Succeed())

	duplicate := types.Channel{
		OrganizationID: orgID,
		ApplicationID:  applicationID,
		LifecycleID:    lifecycleID,
		Name:           "Stable",
	}
	err := db.CreateChannel(ctx, &duplicate)
	g.Expect(errors.Is(err, apierrors.ErrAlreadyExists)).To(BeTrue())

	sameNameOtherApplication := types.Channel{
		OrganizationID: orgID,
		ApplicationID:  otherApplicationID,
		LifecycleID:    lifecycleID,
		Name:           "Stable",
	}
	g.Expect(db.CreateChannel(ctx, &sameNameOtherApplication)).To(Succeed())
}

func TestEnsureDefaultChannelsIsIdempotent(t *testing.T) {
	ctx := channelDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, lifecycleID := createChannelDependencies(t, ctx)

	g.Expect(db.EnsureDefaultChannels(ctx, orgID)).To(Succeed())
	g.Expect(db.EnsureDefaultChannels(ctx, orgID)).To(Succeed())

	channels, err := db.GetChannelsByOrganizationID(ctx, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(channels).To(HaveLen(1))
	g.Expect(channels[0].ApplicationID).To(Equal(applicationID))
	g.Expect(channels[0].LifecycleID).To(Equal(lifecycleID))
	g.Expect(channels[0].Name).To(Equal(db.DefaultChannelName))
	g.Expect(channels[0].IsDefault).To(BeTrue())
}

func TestChannelMigrationDefinesScopedConstraints(t *testing.T) {
	g := NewWithT(t)

	up, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "110_channels.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	sql := string(up)

	g.Expect(sql).To(ContainSubstring("CREATE TABLE Channel"))
	g.Expect(sql).To(ContainSubstring("organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE"))
	g.Expect(sql).To(ContainSubstring("application_id UUID NOT NULL REFERENCES Application(id) ON DELETE CASCADE"))
	g.Expect(sql).To(ContainSubstring("lifecycle_id UUID NOT NULL REFERENCES Lifecycle(id) ON DELETE RESTRICT"))
	g.Expect(sql).To(ContainSubstring("channel_organization_application_name_unique"))
	g.Expect(sql).To(ContainSubstring("Channel_organization_application_default_unique"))
	g.Expect(sql).To(ContainSubstring("WHERE is_default"))

	down, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "110_channels.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(down)).To(ContainSubstring("DROP TABLE IF EXISTS Channel"))
}

func channelDBTestContext(t *testing.T) context.Context {
	t.Helper()
	databaseURL := os.Getenv("DISTR_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DISTR_TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	defer adminPool.Close()

	schema := "channel_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := adminPool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+quotedSchema+" CASCADE"); err != nil {
			t.Logf("drop test schema: %v", err)
		}
	})

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse test database url: %v", err)
	}
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+quotedSchema)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("connect to isolated test schema: %v", err)
	}
	t.Cleanup(pool.Close)

	runChannelTestMigrations(t, ctx, pool)
	return internalctx.WithDb(ctx, pool)
}

func runChannelTestMigrations(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "migrations", "sql", "*.up.sql"))
	if err != nil {
		t.Fatalf("list migration files: %v", err)
	}
	sort.Slice(files, func(i, j int) bool {
		return migrationVersion(t, files[i]) < migrationVersion(t, files[j])
	})
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read migration %s: %v", file, err)
		}
		if _, err := pool.Exec(ctx, string(data)); err != nil {
			t.Fatalf("run migration %s: %v", file, err)
		}
	}
}

func migrationVersion(t *testing.T, file string) int {
	t.Helper()
	base := filepath.Base(file)
	version, err := strconv.Atoi(strings.SplitN(base, "_", 2)[0])
	if err != nil {
		t.Fatalf("parse migration version %s: %v", file, err)
	}
	return version
}

func createChannelDependencies(t *testing.T, ctx context.Context) (uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	orgID := createChannelTestOrganization(t, ctx)
	applicationID, lifecycleID := createChannelDependenciesForOrganization(t, ctx, orgID)
	return orgID, applicationID, lifecycleID
}

func createChannelDependenciesForOrganization(
	t *testing.T,
	ctx context.Context,
	orgID uuid.UUID,
) (uuid.UUID, uuid.UUID) {
	t.Helper()
	application := types.Application{
		Name: "Application " + uuid.NewString(),
		Type: types.DeploymentTypeDocker,
	}
	if err := db.CreateApplication(ctx, &application, orgID); err != nil {
		t.Fatalf("create application: %v", err)
	}
	lifecycle := types.Lifecycle{
		OrganizationID: orgID,
		Name:           "Lifecycle " + uuid.NewString(),
	}
	if err := db.CreateLifecycle(ctx, &lifecycle); err != nil {
		t.Fatalf("create lifecycle: %v", err)
	}
	return application.ID, lifecycle.ID
}

func createChannelTestOrganization(t *testing.T, ctx context.Context) uuid.UUID {
	t.Helper()
	var orgID uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO Organization (name) VALUES (@name) RETURNING id`,
		pgx.NamedArgs{"name": "Organization " + uuid.NewString()},
	).Scan(&orgID); err != nil {
		t.Fatalf("create organization: %v", err)
	}
	return orgID
}
