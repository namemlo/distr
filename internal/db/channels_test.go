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

func TestChannelRepositoryMoveNonDefaultChannelToEmptyApplicationMakesItDefault(t *testing.T) {
	ctx := channelDBTestContext(t)
	g := NewWithT(t)
	orgID, sourceApplicationID, lifecycleID := createChannelDependencies(t, ctx)
	targetApplicationID := createChannelApplicationForOrganization(t, ctx, orgID)

	stable := types.Channel{
		OrganizationID: orgID,
		ApplicationID:  sourceApplicationID,
		LifecycleID:    lifecycleID,
		Name:           "Stable",
		IsDefault:      true,
	}
	g.Expect(db.CreateChannel(ctx, &stable)).To(Succeed())
	preview := types.Channel{
		OrganizationID: orgID,
		ApplicationID:  sourceApplicationID,
		LifecycleID:    lifecycleID,
		Name:           "Preview",
		SortOrder:      10,
	}
	g.Expect(db.CreateChannel(ctx, &preview)).To(Succeed())
	g.Expect(preview.IsDefault).To(BeFalse())

	preview.ApplicationID = targetApplicationID
	g.Expect(db.UpdateChannel(ctx, &preview)).To(Succeed())

	g.Expect(preview.IsDefault).To(BeTrue())
	assertChannelApplicationHasOneDefault(t, ctx, orgID, sourceApplicationID)
	assertChannelApplicationHasOneDefault(t, ctx, orgID, targetApplicationID)
}

func TestChannelRepositoryMoveNonDefaultChannelToApplicationWithDefaultPreservesTargetDefault(t *testing.T) {
	ctx := channelDBTestContext(t)
	g := NewWithT(t)
	orgID, sourceApplicationID, lifecycleID := createChannelDependencies(t, ctx)
	targetApplicationID := createChannelApplicationForOrganization(t, ctx, orgID)

	sourceDefault := types.Channel{
		OrganizationID: orgID,
		ApplicationID:  sourceApplicationID,
		LifecycleID:    lifecycleID,
		Name:           "Source Stable",
		IsDefault:      true,
	}
	g.Expect(db.CreateChannel(ctx, &sourceDefault)).To(Succeed())
	preview := types.Channel{
		OrganizationID: orgID,
		ApplicationID:  sourceApplicationID,
		LifecycleID:    lifecycleID,
		Name:           "Preview",
		SortOrder:      10,
	}
	g.Expect(db.CreateChannel(ctx, &preview)).To(Succeed())
	g.Expect(preview.IsDefault).To(BeFalse())
	targetDefault := types.Channel{
		OrganizationID: orgID,
		ApplicationID:  targetApplicationID,
		LifecycleID:    lifecycleID,
		Name:           "Target Stable",
		IsDefault:      true,
	}
	g.Expect(db.CreateChannel(ctx, &targetDefault)).To(Succeed())

	preview.ApplicationID = targetApplicationID
	g.Expect(db.UpdateChannel(ctx, &preview)).To(Succeed())

	g.Expect(preview.IsDefault).To(BeFalse())
	updatedTargetDefault, err := db.GetChannel(ctx, targetDefault.ID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(updatedTargetDefault.IsDefault).To(BeTrue())
	assertChannelApplicationHasOneDefault(t, ctx, orgID, sourceApplicationID)
	assertChannelApplicationHasOneDefault(t, ctx, orgID, targetApplicationID)
}

func TestChannelRepositoryRejectsMovingExistingDefaultChannel(t *testing.T) {
	ctx := channelDBTestContext(t)
	g := NewWithT(t)
	orgID, sourceApplicationID, lifecycleID := createChannelDependencies(t, ctx)
	targetApplicationID := createChannelApplicationForOrganization(t, ctx, orgID)
	stable := types.Channel{
		OrganizationID: orgID,
		ApplicationID:  sourceApplicationID,
		LifecycleID:    lifecycleID,
		Name:           "Stable",
		IsDefault:      true,
	}
	g.Expect(db.CreateChannel(ctx, &stable)).To(Succeed())

	stable.ApplicationID = targetApplicationID
	err := db.UpdateChannel(ctx, &stable)

	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
	unchanged, err := db.GetChannel(ctx, stable.ID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(unchanged.ApplicationID).To(Equal(sourceApplicationID))
	g.Expect(unchanged.IsDefault).To(BeTrue())
	assertChannelApplicationHasOneDefault(t, ctx, orgID, sourceApplicationID)
}

func TestChannelRepositoryDeleteDefaultAndNonDefaultChannels(t *testing.T) {
	ctx := channelDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, lifecycleID := createChannelDependencies(t, ctx)
	stable := types.Channel{
		OrganizationID: orgID,
		ApplicationID:  applicationID,
		LifecycleID:    lifecycleID,
		Name:           "Stable",
		IsDefault:      true,
	}
	g.Expect(db.CreateChannel(ctx, &stable)).To(Succeed())
	preview := types.Channel{
		OrganizationID: orgID,
		ApplicationID:  applicationID,
		LifecycleID:    lifecycleID,
		Name:           "Preview",
		SortOrder:      10,
	}
	g.Expect(db.CreateChannel(ctx, &preview)).To(Succeed())
	g.Expect(preview.IsDefault).To(BeFalse())

	err := db.DeleteChannelWithID(ctx, stable.ID, orgID)
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
	g.Expect(db.DeleteChannelWithID(ctx, preview.ID, orgID)).To(Succeed())

	_, err = db.GetChannel(ctx, preview.ID, orgID)
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
	assertChannelApplicationHasOneDefault(t, ctx, orgID, applicationID)
}

func TestChannelRepositoryRejectsMovingChannelPromotedToDefaultConcurrently(t *testing.T) {
	ctx := channelDBTestContext(t)
	g := NewWithT(t)
	orgID, sourceApplicationID, lifecycleID := createChannelDependencies(t, ctx)
	targetApplicationID := createChannelApplicationForOrganization(t, ctx, orgID)

	stable := types.Channel{
		OrganizationID: orgID,
		ApplicationID:  sourceApplicationID,
		LifecycleID:    lifecycleID,
		Name:           "Stable",
		IsDefault:      true,
	}
	g.Expect(db.CreateChannel(ctx, &stable)).To(Succeed())
	preview := types.Channel{
		OrganizationID: orgID,
		ApplicationID:  sourceApplicationID,
		LifecycleID:    lifecycleID,
		Name:           "Preview",
		SortOrder:      10,
	}
	g.Expect(db.CreateChannel(ctx, &preview)).To(Succeed())
	targetDefault := types.Channel{
		OrganizationID: orgID,
		ApplicationID:  targetApplicationID,
		LifecycleID:    lifecycleID,
		Name:           "Target Stable",
		IsDefault:      true,
	}
	g.Expect(db.CreateChannel(ctx, &targetDefault)).To(Succeed())

	tx := lockChannelRowForTest(t, ctx, preview.ID)
	promoteChannelToDefaultInTx(t, ctx, tx, orgID, sourceApplicationID, stable.ID, preview.ID)

	preview.ApplicationID = targetApplicationID
	errCh := make(chan error, 1)
	go func() {
		errCh <- db.UpdateChannel(ctx, &preview)
	}()
	assertChannelOperationIsWaiting(t, errCh)
	g.Expect(tx.Commit(ctx)).To(Succeed())

	err := awaitChannelOperation(t, errCh)
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
	unchanged, err := db.GetChannel(ctx, preview.ID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(unchanged.ApplicationID).To(Equal(sourceApplicationID))
	g.Expect(unchanged.IsDefault).To(BeTrue())
	assertChannelApplicationHasOneDefault(t, ctx, orgID, sourceApplicationID)
	assertChannelApplicationHasOneDefault(t, ctx, orgID, targetApplicationID)
}

func TestChannelRepositoryRejectsDeletingChannelPromotedToDefaultConcurrently(t *testing.T) {
	ctx := channelDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, lifecycleID := createChannelDependencies(t, ctx)

	stable := types.Channel{
		OrganizationID: orgID,
		ApplicationID:  applicationID,
		LifecycleID:    lifecycleID,
		Name:           "Stable",
		IsDefault:      true,
	}
	g.Expect(db.CreateChannel(ctx, &stable)).To(Succeed())
	preview := types.Channel{
		OrganizationID: orgID,
		ApplicationID:  applicationID,
		LifecycleID:    lifecycleID,
		Name:           "Preview",
		SortOrder:      10,
	}
	g.Expect(db.CreateChannel(ctx, &preview)).To(Succeed())

	tx := lockChannelRowForTest(t, ctx, preview.ID)
	promoteChannelToDefaultInTx(t, ctx, tx, orgID, applicationID, stable.ID, preview.ID)

	errCh := make(chan error, 1)
	go func() {
		errCh <- db.DeleteChannelWithID(ctx, preview.ID, orgID)
	}()
	assertChannelOperationIsWaiting(t, errCh)
	g.Expect(tx.Commit(ctx)).To(Succeed())

	err := awaitChannelOperation(t, errCh)
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
	unchanged, err := db.GetChannel(ctx, preview.ID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(unchanged.IsDefault).To(BeTrue())
	assertChannelApplicationHasOneDefault(t, ctx, orgID, applicationID)
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
	t.Cleanup(adminPool.Close)

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
	applicationID := createChannelApplicationForOrganization(t, ctx, orgID)
	lifecycle := types.Lifecycle{
		OrganizationID: orgID,
		Name:           "Lifecycle " + uuid.NewString(),
	}
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO Lifecycle (organization_id, name) VALUES (@organizationId, @name) RETURNING id`,
		pgx.NamedArgs{
			"organizationId": lifecycle.OrganizationID,
			"name":           lifecycle.Name,
		},
	).Scan(&lifecycle.ID); err != nil {
		t.Fatalf("create lifecycle: %v", err)
	}
	return applicationID, lifecycle.ID
}

func createChannelApplicationForOrganization(t *testing.T, ctx context.Context, orgID uuid.UUID) uuid.UUID {
	t.Helper()
	application := types.Application{
		Name: "Application " + uuid.NewString(),
		Type: types.DeploymentTypeDocker,
	}
	if err := db.CreateApplication(ctx, &application, orgID); err != nil {
		t.Fatalf("create application: %v", err)
	}
	return application.ID
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

func lockChannelRowForTest(t *testing.T, ctx context.Context, channelID uuid.UUID) pgx.Tx {
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
		`SELECT id FROM Channel WHERE id = @id FOR UPDATE`,
		pgx.NamedArgs{"id": channelID},
	); err != nil {
		t.Fatalf("lock channel row: %v", err)
	}
	return tx
}

func promoteChannelToDefaultInTx(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
	orgID uuid.UUID,
	applicationID uuid.UUID,
	currentDefaultID uuid.UUID,
	newDefaultID uuid.UUID,
) {
	t.Helper()
	if _, err := tx.Exec(
		ctx,
		`UPDATE Channel
		SET is_default = false
		WHERE id = @id AND organization_id = @organizationId AND application_id = @applicationId`,
		pgx.NamedArgs{
			"id":             currentDefaultID,
			"organizationId": orgID,
			"applicationId":  applicationID,
		},
	); err != nil {
		t.Fatalf("clear current default channel: %v", err)
	}
	if _, err := tx.Exec(
		ctx,
		`UPDATE Channel
		SET is_default = true
		WHERE id = @id AND organization_id = @organizationId AND application_id = @applicationId`,
		pgx.NamedArgs{
			"id":             newDefaultID,
			"organizationId": orgID,
			"applicationId":  applicationID,
		},
	); err != nil {
		t.Fatalf("promote channel to default: %v", err)
	}
}

func assertChannelOperationIsWaiting(t *testing.T, errCh <-chan error) {
	t.Helper()
	select {
	case err := <-errCh:
		t.Fatalf("channel operation completed before row lock was released: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
}

func awaitChannelOperation(t *testing.T, errCh <-chan error) error {
	t.Helper()
	select {
	case err := <-errCh:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("channel operation did not finish after row lock was released")
		return nil
	}
}

func assertChannelApplicationHasOneDefault(
	t *testing.T,
	ctx context.Context,
	orgID uuid.UUID,
	applicationID uuid.UUID,
) {
	t.Helper()
	var count int
	err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`SELECT count(*)
		FROM Channel
		WHERE organization_id = @organizationId
			AND application_id = @applicationId
			AND is_default`,
		pgx.NamedArgs{
			"organizationId": orgID,
			"applicationId":  applicationID,
		},
	).Scan(&count)
	if err != nil {
		t.Fatalf("count default channels: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 default channel for application %s, got %d", applicationID, count)
	}
}
