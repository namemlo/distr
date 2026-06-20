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
	"github.com/distr-sh/distr/internal/releasebundles"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
)

func TestReleaseBundleRepositoryDraftCRUDAndChecksum(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleDependencies(t, ctx)
	bundle := releaseBundleFixture(orgID, applicationID, channelID, versionID)

	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())
	g.Expect(bundle.ID).NotTo(Equal(uuid.Nil))
	g.Expect(bundle.Status).To(Equal(types.ReleaseBundleStatusDraft))
	g.Expect(bundle.CanonicalChecksum).To(HavePrefix("sha256:"))
	g.Expect(bundle.CanonicalPayload).NotTo(BeEmpty())
	g.Expect(bundle.Components).To(HaveLen(1))

	listed, err := db.GetReleaseBundlesByOrganizationID(ctx, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(listed).To(HaveLen(1))
	g.Expect(listed[0].ID).To(Equal(bundle.ID))
	g.Expect(listed[0].Components).To(HaveLen(1))

	createdChecksum := bundle.CanonicalChecksum
	bundle.ReleaseNotes = "Updated notes"
	bundle.Components[0].Version = "1.2.4"
	g.Expect(db.UpdateReleaseBundle(ctx, &bundle)).To(Succeed())
	g.Expect(bundle.CanonicalChecksum).NotTo(Equal(createdChecksum))

	fetched, err := db.GetReleaseBundle(ctx, bundle.ID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(fetched.Components[0].Version).To(Equal("1.2.4"))
	expectedPayload, expectedChecksum, err := releasebundles.Canonicalize(*fetched)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(fetched.CanonicalPayload).To(Equal(expectedPayload))
	g.Expect(fetched.CanonicalChecksum).To(Equal(expectedChecksum))

	g.Expect(db.DeleteReleaseBundleWithID(ctx, bundle.ID, orgID)).To(Succeed())
	_, err = db.GetReleaseBundle(ctx, bundle.ID, orgID)
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
}

func TestReleaseBundleRepositoryRejectsDuplicateReleaseNumbersWithinApplicationScope(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleDependencies(t, ctx)
	first := releaseBundleFixture(orgID, applicationID, channelID, versionID)
	g.Expect(db.CreateReleaseBundle(ctx, &first)).To(Succeed())

	duplicate := releaseBundleFixture(orgID, applicationID, channelID, versionID)
	err := db.CreateReleaseBundle(ctx, &duplicate)
	g.Expect(errors.Is(err, apierrors.ErrAlreadyExists)).To(BeTrue())

	otherApplicationID, otherChannelID, otherVersionID := createReleaseBundleDependenciesForOrganization(t, ctx, orgID)
	sameNumberOtherApplication := releaseBundleFixture(orgID, otherApplicationID, otherChannelID, otherVersionID)
	g.Expect(db.CreateReleaseBundle(ctx, &sameNumberOtherApplication)).To(Succeed())
}

func TestReleaseBundleRepositoryRejectsInvalidAndCrossOrganizationReferences(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleDependencies(t, ctx)
	otherOrgID, otherApplicationID, otherChannelID, otherVersionID := createReleaseBundleDependencies(t, ctx)

	tests := []struct {
		name           string
		organizationID uuid.UUID
		applicationID  uuid.UUID
		channelID      uuid.UUID
		versionID      uuid.UUID
	}{
		{
			name:           "missing application",
			organizationID: orgID,
			applicationID:  uuid.New(),
			channelID:      channelID,
			versionID:      versionID,
		},
		{
			name:           "missing channel",
			organizationID: orgID,
			applicationID:  applicationID,
			channelID:      uuid.New(),
			versionID:      versionID,
		},
		{
			name:           "missing application version",
			organizationID: orgID,
			applicationID:  applicationID,
			channelID:      channelID,
			versionID:      uuid.New(),
		},
		{
			name:           "cross-organization application",
			organizationID: orgID,
			applicationID:  otherApplicationID,
			channelID:      channelID,
			versionID:      versionID,
		},
		{
			name:           "cross-organization channel",
			organizationID: orgID,
			applicationID:  applicationID,
			channelID:      otherChannelID,
			versionID:      versionID,
		},
		{
			name:           "cross-organization application version",
			organizationID: orgID,
			applicationID:  applicationID,
			channelID:      channelID,
			versionID:      otherVersionID,
		},
		{
			name:           "inverse cross-organization application",
			organizationID: otherOrgID,
			applicationID:  applicationID,
			channelID:      otherChannelID,
			versionID:      otherVersionID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bundle := releaseBundleFixture(tt.organizationID, tt.applicationID, tt.channelID, tt.versionID)

			err := db.CreateReleaseBundle(ctx, &bundle)

			g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
		})
	}
}

func TestReleaseBundleRepositoryRejectsMutatingNonDraftBundles(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleDependencies(t, ctx)
	bundle := releaseBundleFixture(orgID, applicationID, channelID, versionID)
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())
	markReleaseBundleStatusForTest(t, ctx, bundle.ID, types.ReleaseBundleStatusPublished)

	bundle.ReleaseNotes = "Cannot update"
	err := db.UpdateReleaseBundle(ctx, &bundle)
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())

	err = db.DeleteReleaseBundleWithID(ctx, bundle.ID, orgID)
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
}

func TestReleaseBundleRepositoryValidatePublishAndProtectPublishedBundle(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleDependencies(t, ctx)
	actorID := createReleaseBundleTestUser(t, ctx, orgID)
	bundle := releaseBundleFixture(orgID, applicationID, channelID, versionID)
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())

	result, err := db.ValidateReleaseBundle(ctx, bundle.ID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Valid).To(BeTrue())

	published, publishResult, err := db.PublishReleaseBundle(ctx, bundle.ID, orgID, actorID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(publishResult.Valid).To(BeTrue())
	g.Expect(published.Status).To(Equal(types.ReleaseBundleStatusPublished))
	g.Expect(published.PublishedByUserAccountID).To(Equal(&actorID))
	g.Expect(published.PublishedAt).NotTo(BeNil())
	g.Expect(published.CanonicalChecksum).To(Equal(bundle.CanonicalChecksum))

	published.ReleaseNotes = "Cannot edit after publish"
	err = db.UpdateReleaseBundle(ctx, published)
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
	err = db.DeleteReleaseBundleWithID(ctx, published.ID, orgID)
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())

	events, err := db.GetReleaseBundleAuditEvents(ctx, bundle.ID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(events).To(ContainElement(MatchFields(IgnoreExtras, Fields{
		"OrganizationID":     Equal(orgID),
		"ReleaseBundleID":    Equal(bundle.ID),
		"ActorUserAccountID": Equal(&actorID),
		"EventType":          Equal(types.ReleaseBundleAuditEventTypePublished),
		"FromStatus":         Equal(types.ReleaseBundleStatusDraft),
		"ToStatus":           Equal(releaseBundleStatusPtr(types.ReleaseBundleStatusPublished)),
		"Reason":             Equal(""),
	})))
}

func TestReleaseBundleRepositoryRejectsPublishWhenValidationFails(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID := createReleaseBundleTestOrganization(t, ctx)
	applicationID, channelID, versionID := createReleaseBundleDependenciesForOrganizationWithRules(
		t, ctx, orgID, []string{">=2.0.0 <3.0.0"}, nil, nil, nil,
	)
	actorID := createReleaseBundleTestUser(t, ctx, orgID)
	bundle := releaseBundleFixture(orgID, applicationID, channelID, versionID)
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())

	published, result, err := db.PublishReleaseBundle(ctx, bundle.ID, orgID, actorID)

	g.Expect(published).To(BeNil())
	g.Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())
	g.Expect(result.Valid).To(BeFalse())
	g.Expect(result.Errors).To(ContainElement(releasebundles.ValidationIssue{
		Field:   "components.api.version",
		Rule:    ">=2.0.0 <3.0.0",
		Message: "version does not match an allowed range",
	}))
	fetched, err := db.GetReleaseBundle(ctx, bundle.ID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(fetched.Status).To(Equal(types.ReleaseBundleStatusDraft))
	events, err := db.GetReleaseBundleAuditEvents(ctx, bundle.ID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(events).To(ContainElement(MatchFields(IgnoreExtras, Fields{
		"OrganizationID":     Equal(orgID),
		"ReleaseBundleID":    Equal(bundle.ID),
		"ActorUserAccountID": Equal(&actorID),
		"EventType":          Equal(types.ReleaseBundleAuditEventTypeStateTransitionRejected),
		"FromStatus":         Equal(types.ReleaseBundleStatusDraft),
		"ToStatus":           Equal(releaseBundleStatusPtr(types.ReleaseBundleStatusPublished)),
		"Reason":             Equal("validation failed"),
	})))
}

func TestReleaseBundleRepositoryBlockArchiveAndRejectedTransitionAudits(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleDependencies(t, ctx)
	actorID := createReleaseBundleTestUser(t, ctx, orgID)
	bundle := releaseBundleFixture(orgID, applicationID, channelID, versionID)
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())

	_, _, err := db.PublishReleaseBundle(ctx, bundle.ID, orgID, actorID)
	g.Expect(err).NotTo(HaveOccurred())
	blocked, err := db.BlockReleaseBundle(ctx, bundle.ID, orgID, actorID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(blocked.Status).To(Equal(types.ReleaseBundleStatusBlocked))
	_, err = db.BlockReleaseBundle(ctx, bundle.ID, orgID, actorID)
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
	archived, err := db.ArchiveReleaseBundle(ctx, bundle.ID, orgID, actorID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(archived.Status).To(Equal(types.ReleaseBundleStatusArchived))

	events, err := db.GetReleaseBundleAuditEvents(ctx, bundle.ID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(events).To(ContainElements(
		MatchFields(IgnoreExtras, Fields{
			"OrganizationID":     Equal(orgID),
			"ReleaseBundleID":    Equal(bundle.ID),
			"ActorUserAccountID": Equal(&actorID),
			"EventType":          Equal(types.ReleaseBundleAuditEventTypeBlocked),
			"FromStatus":         Equal(types.ReleaseBundleStatusPublished),
			"ToStatus":           Equal(releaseBundleStatusPtr(types.ReleaseBundleStatusBlocked)),
			"Reason":             Equal(""),
		}),
		MatchFields(IgnoreExtras, Fields{
			"OrganizationID":     Equal(orgID),
			"ReleaseBundleID":    Equal(bundle.ID),
			"ActorUserAccountID": Equal(&actorID),
			"EventType":          Equal(types.ReleaseBundleAuditEventTypeStateTransitionRejected),
			"FromStatus":         Equal(types.ReleaseBundleStatusBlocked),
			"ToStatus":           Equal(releaseBundleStatusPtr(types.ReleaseBundleStatusBlocked)),
			"Reason":             Equal("release bundle cannot transition from BLOCKED to BLOCKED"),
		}),
		MatchFields(IgnoreExtras, Fields{
			"OrganizationID":     Equal(orgID),
			"ReleaseBundleID":    Equal(bundle.ID),
			"ActorUserAccountID": Equal(&actorID),
			"EventType":          Equal(types.ReleaseBundleAuditEventTypeArchived),
			"FromStatus":         Equal(types.ReleaseBundleStatusBlocked),
			"ToStatus":           Equal(releaseBundleStatusPtr(types.ReleaseBundleStatusArchived)),
			"Reason":             Equal(""),
		}),
	))
}

func TestReleaseBundleRepositoryPreventsMovingReferencedChannelAcrossApplications(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, sourceApplicationID, sourceChannelID, versionID := createReleaseBundleDependencies(t, ctx)
	targetApplicationID, _, _ := createReleaseBundleDependenciesForOrganization(t, ctx, orgID)

	sourceChannel, err := db.GetChannel(ctx, sourceChannelID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	preview := types.Channel{
		OrganizationID: orgID,
		ApplicationID:  sourceApplicationID,
		LifecycleID:    sourceChannel.LifecycleID,
		Name:           "Preview",
		SortOrder:      10,
	}
	g.Expect(db.CreateChannel(ctx, &preview)).To(Succeed())
	g.Expect(preview.IsDefault).To(BeFalse())

	bundle := releaseBundleFixture(orgID, sourceApplicationID, preview.ID, versionID)
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())

	preview.ApplicationID = targetApplicationID
	err = db.UpdateChannel(ctx, &preview)

	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
	unchanged, err := db.GetChannel(ctx, preview.ID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(unchanged.ApplicationID).To(Equal(sourceApplicationID))
	fetched, err := db.GetReleaseBundle(ctx, bundle.ID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(fetched.ApplicationID).To(Equal(sourceApplicationID))
	g.Expect(fetched.ChannelID).To(Equal(preview.ID))
}

func TestReleaseBundleMigrationDefinesDraftBundleSchema(t *testing.T) {
	g := NewWithT(t)

	up, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "112_release_bundles.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	sql := string(up)

	g.Expect(sql).To(ContainSubstring("CREATE TABLE ReleaseBundle"))
	g.Expect(sql).To(ContainSubstring("organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE"))
	g.Expect(sql).To(ContainSubstring("application_id UUID NOT NULL REFERENCES Application(id) ON DELETE RESTRICT"))
	g.Expect(sql).To(ContainSubstring("channel_id UUID NOT NULL"))
	g.Expect(sql).To(ContainSubstring("releasebundle_organization_application_number_unique"))
	g.Expect(sql).To(ContainSubstring("canonical_checksum TEXT NOT NULL"))
	g.Expect(sql).To(ContainSubstring("canonical_payload BYTEA NOT NULL"))
	g.Expect(sql).To(ContainSubstring("channel_id_application_organization_unique"))
	g.Expect(sql).To(ContainSubstring("releasebundle_channel_application_organization_fk"))
	g.Expect(sql).To(ContainSubstring("FOREIGN KEY (channel_id, application_id, organization_id)"))
	g.Expect(sql).To(ContainSubstring("CREATE TABLE ReleaseBundleComponent"))
	g.Expect(sql).To(ContainSubstring("releasebundlecomponent_bundle_key_unique"))

	up113, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "113_release_bundle_publication.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	publicationSQL := string(up113)
	g.Expect(publicationSQL).To(ContainSubstring(
		"published_by_user_account_id UUID REFERENCES UserAccount(id) ON DELETE SET NULL",
	))
	g.Expect(publicationSQL).To(ContainSubstring("published_at TIMESTAMP"))
	g.Expect(publicationSQL).To(ContainSubstring("CREATE TABLE ReleaseBundleAuditEvent"))
	g.Expect(publicationSQL).To(ContainSubstring("releasebundleauditevent_type_check"))

	down, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "112_release_bundles.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(down)).To(ContainSubstring("DROP TABLE IF EXISTS ReleaseBundleComponent"))
	g.Expect(string(down)).To(ContainSubstring("DROP TABLE IF EXISTS ReleaseBundle"))
	g.Expect(string(down)).To(ContainSubstring("DROP CONSTRAINT IF EXISTS channel_id_application_organization_unique"))

	down113, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "113_release_bundle_publication.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(down113)).To(ContainSubstring("DROP TABLE IF EXISTS ReleaseBundleAuditEvent"))
	g.Expect(string(down113)).To(ContainSubstring("DROP COLUMN published_by_user_account_id"))
}

//nolint:dupl
func releaseBundleDBTestContext(t *testing.T) context.Context {
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

	schema := "release_bundle_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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

	runReleaseBundleTestMigrations(t, ctx, pool)
	return internalctx.WithDb(ctx, pool)
}

func runReleaseBundleTestMigrations(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "migrations", "sql", "*.up.sql"))
	if err != nil {
		t.Fatalf("list migration files: %v", err)
	}
	sort.Slice(files, func(i, j int) bool {
		return releaseBundleMigrationVersion(t, files[i]) < releaseBundleMigrationVersion(t, files[j])
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

func releaseBundleMigrationVersion(t *testing.T, file string) int {
	t.Helper()
	base := filepath.Base(file)
	version, err := strconv.Atoi(strings.SplitN(base, "_", 2)[0])
	if err != nil {
		t.Fatalf("parse migration version %s: %v", file, err)
	}
	return version
}

func createReleaseBundleDependencies(t *testing.T, ctx context.Context) (uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	orgID := createReleaseBundleTestOrganization(t, ctx)
	applicationID, channelID, versionID := createReleaseBundleDependenciesForOrganization(t, ctx, orgID)
	return orgID, applicationID, channelID, versionID
}

func createReleaseBundleDependenciesForOrganization(
	t *testing.T,
	ctx context.Context,
	orgID uuid.UUID,
) (uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	return createReleaseBundleDependenciesForOrganizationWithRules(t, ctx, orgID, nil, nil, nil, nil)
}

func createReleaseBundleDependenciesForOrganizationWithRules(
	t *testing.T,
	ctx context.Context,
	orgID uuid.UUID,
	ranges []string,
	prereleasePatterns []string,
	sourceBranchPatterns []string,
	sourceTagPatterns []string,
) (uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	application := types.Application{
		Name: "Application " + uuid.NewString(),
		Type: types.DeploymentTypeDocker,
	}
	if err := db.CreateApplication(ctx, &application, orgID); err != nil {
		t.Fatalf("create application: %v", err)
	}
	version := types.ApplicationVersion{
		Name:            "1.2.3",
		ApplicationID:   application.ID,
		LinkTemplate:    "https://example.com/{{.version}}",
		ComposeFileData: []byte("services: {}\n"),
	}
	if err := db.CreateApplicationVersion(ctx, &version); err != nil {
		t.Fatalf("create application version: %v", err)
	}
	var lifecycleID uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO Lifecycle (organization_id, name) VALUES (@organizationId, @name) RETURNING id`,
		pgx.NamedArgs{"organizationId": orgID, "name": "Lifecycle " + uuid.NewString()},
	).Scan(&lifecycleID); err != nil {
		t.Fatalf("create lifecycle: %v", err)
	}
	channel := types.Channel{
		OrganizationID:              orgID,
		ApplicationID:               application.ID,
		LifecycleID:                 lifecycleID,
		Name:                        "Stable",
		IsDefault:                   true,
		AllowedVersionRanges:        ranges,
		AllowedPrereleasePatterns:   prereleasePatterns,
		AllowedSourceBranchPatterns: sourceBranchPatterns,
		AllowedSourceTagPatterns:    sourceTagPatterns,
	}
	if err := db.CreateChannel(ctx, &channel); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	return application.ID, channel.ID, version.ID
}

func createReleaseBundleTestUser(t *testing.T, ctx context.Context, orgID uuid.UUID) uuid.UUID {
	t.Helper()
	var userID uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO UserAccount (email) VALUES (@email) RETURNING id`,
		pgx.NamedArgs{"email": "release-bundle-" + uuid.NewString() + "@example.com"},
	).Scan(&userID); err != nil {
		t.Fatalf("create user account: %v", err)
	}
	if _, err := internalctx.GetDb(ctx).Exec(
		ctx,
		`INSERT INTO Organization_UserAccount (organization_id, user_account_id, user_role)
		VALUES (@organizationId, @userId, 'admin')`,
		pgx.NamedArgs{"organizationId": orgID, "userId": userID},
	); err != nil {
		t.Fatalf("create organization user account: %v", err)
	}
	return userID
}

func createReleaseBundleTestOrganization(t *testing.T, ctx context.Context) uuid.UUID {
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

func releaseBundleFixture(
	orgID uuid.UUID,
	applicationID uuid.UUID,
	channelID uuid.UUID,
	versionID uuid.UUID,
) types.ReleaseBundle {
	return types.ReleaseBundle{
		OrganizationID: orgID,
		ApplicationID:  applicationID,
		ChannelID:      channelID,
		ReleaseNumber:  "2026.06.20",
		ReleaseNotes:   "Initial release",
		SourceRevision: "abc123",
		Components: []types.ReleaseBundleComponent{
			{
				Key:                  "api",
				Name:                 "API",
				Type:                 types.ReleaseBundleComponentTypeApplicationVersion,
				Version:              "1.2.3",
				ApplicationVersionID: &versionID,
			},
		},
	}
}

func markReleaseBundleStatusForTest(
	t *testing.T,
	ctx context.Context,
	id uuid.UUID,
	status types.ReleaseBundleStatus,
) {
	t.Helper()
	if _, err := internalctx.GetDb(ctx).Exec(
		ctx,
		`UPDATE ReleaseBundle SET status = @status WHERE id = @id`,
		pgx.NamedArgs{"id": id, "status": status},
	); err != nil {
		t.Fatalf("mark release bundle status: %v", err)
	}
}

func releaseBundleStatusPtr(status types.ReleaseBundleStatus) *types.ReleaseBundleStatus {
	return &status
}
