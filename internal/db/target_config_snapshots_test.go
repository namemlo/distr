package db

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/targetconfig"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	. "github.com/onsi/gomega"
)

func TestMigration141DefinesImmutableTargetConfigContract(t *testing.T) {
	g := NewWithT(t)
	up, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "141_target_config_snapshots.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	down, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "141_target_config_snapshots.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())

	upSQL := string(up)
	for _, table := range []string{
		"TargetConfigSnapshot",
		"TargetConfigSnapshotObject",
		"TargetConfigSnapshotComponent",
		"TargetConfigSnapshotSecretReference",
		"TargetConfigSnapshotFeatureFlag",
	} {
		g.Expect(upSQL).To(ContainSubstring("CREATE TABLE " + table))
		g.Expect(upSQL).To(ContainSubstring(
			"CREATE TRIGGER " + table + "_immutable",
		))
	}
	g.Expect(upSQL).To(ContainSubstring("schema TEXT NOT NULL"))
	g.Expect(upSQL).To(ContainSubstring("canonical_payload BYTEA NOT NULL"))
	g.Expect(upSQL).To(ContainSubstring("canonical_checksum TEXT NOT NULL"))
	g.Expect(upSQL).To(ContainSubstring("created_by_user_account_id UUID NOT NULL"))
	g.Expect(upSQL).To(ContainSubstring("octet_length(version_id) <= 1024"))
	g.Expect(upSQL).To(ContainSubstring("version_id !~ '[[:cntrl:]]'"))
	g.Expect(upSQL).To(ContainSubstring(
		"version_id !~* '^(gh[pousr]_[A-Za-z0-9]{20,}|xox[baprs]-[A-Za-z0-9-]{20,}|(AKIA|ASIA)[A-Z0-9]{16})$'",
	))
	g.Expect(upSQL).To(ContainSubstring("octet_length(provider) BETWEEN 1 AND 128"))
	g.Expect(upSQL).To(ContainSubstring("provider ~ '^[a-z0-9]+([._-][a-z0-9]+)*$'"))
	g.Expect(upSQL).NotTo(ContainSubstring("object_body"))
	g.Expect(upSQL).NotTo(ContainSubstring("secret_value"))
	g.Expect(upSQL).To(MatchRegexp(
		`(?s)FOREIGN KEY \(\s*deployment_unit_id,\s*target_environment_assignment_id,\s*organization_id\s*\)`,
	))
	g.Expect(upSQL).To(MatchRegexp(
		`(?s)FOREIGN KEY \(\s*target_environment_assignment_id,\s*environment_id,\s*organization_id\s*\)`,
	))
	g.Expect(upSQL).To(MatchRegexp(
		`(?s)FOREIGN KEY \(\s*organization_id,\s*created_by_user_account_id\s*\)` +
			`\s*REFERENCES Organization_UserAccount\(\s*organization_id,\s*user_account_id\s*\)`,
	))
	g.Expect(string(down)).To(ContainSubstring(
		"downgrade crossing 141 is forbidden while target config snapshots exist",
	))
}

func TestTargetConfigComponentLockContractRejectsStalePhysicalName(t *testing.T) {
	g := NewWithT(t)
	componentID := uuid.New()
	unitID := uuid.New()
	draft := types.TargetConfigSnapshotDraft{
		DeploymentUnitID: unitID,
		Components: []types.TargetConfigSnapshotComponentDraft{{
			ComponentInstanceID: componentID,
			DeploymentUnitID:    unitID,
			PhysicalName:        "stale-api",
		}},
	}

	err := validateLockedTargetConfigComponents(draft, []targetConfigLockedComponent{{
		ID:               componentID,
		DeploymentUnitID: unitID,
		PhysicalName:     "api",
	}})

	g.Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())
	g.Expect(err).To(MatchError(ContainSubstring("physicalName does not match")))
	g.Expect(targetConfigComponentLockQuery).To(ContainSubstring("FOR SHARE"))
	g.Expect(targetConfigComponentLockQuery).To(ContainSubstring("organization_id = @organizationID"))
	g.Expect(targetConfigComponentLockQuery).To(ContainSubstring("deployment_unit_id = @deploymentUnitID"))
}

func TestTargetConfigComponentLockContractAcceptsExactIdentity(t *testing.T) {
	g := NewWithT(t)
	componentID := uuid.New()
	unitID := uuid.New()
	draft := types.TargetConfigSnapshotDraft{
		DeploymentUnitID: unitID,
		Components: []types.TargetConfigSnapshotComponentDraft{{
			ComponentInstanceID: componentID,
			DeploymentUnitID:    unitID,
			PhysicalName:        "api",
		}},
	}

	err := validateLockedTargetConfigComponents(draft, []targetConfigLockedComponent{{
		ID:               componentID,
		DeploymentUnitID: unitID,
		PhysicalName:     "api",
	}})

	g.Expect(err).NotTo(HaveOccurred())
}

func TestTargetConfigSnapshotRepositoryCreateReadListAndImmutability(t *testing.T) {
	ctx, pool := deploymentRegistryIsolatedPool(t, 141)
	g := NewWithT(t)
	deps := createDeploymentRegistryDependencies(t, ctx)
	placement := createDeploymentRegistryPlacement(t, ctx, deps, "target-config", time.Now().UTC())
	creatorID := createTargetConfigSnapshotTestUser(t, ctx, deps.organizationID)
	draft := targetConfigSnapshotRepositoryFixture(placement, deps.organizationID, creatorID)

	created, err := CreateTargetConfigSnapshot(ctx, &draft)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(created.ID).NotTo(Equal(uuid.Nil))
	g.Expect(created.CreatedByUserAccountID).To(Equal(creatorID))
	g.Expect(created.Objects).To(HaveLen(1))
	g.Expect(created.Components).To(HaveLen(1))
	g.Expect(created.SecretReferences).To(HaveLen(1))
	g.Expect(created.FeatureFlags).To(HaveLen(1))

	fetched, err := GetTargetConfigSnapshot(ctx, deps.organizationID, created.ID)
	g.Expect(err).NotTo(HaveOccurred())
	expectedPayload, expectedChecksum, err := targetconfig.Canonicalize(draft)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(fetched.CanonicalPayload).To(Equal(expectedPayload))
	g.Expect(fetched.CanonicalChecksum).To(Equal(expectedChecksum))
	g.Expect(fetched.CanonicalChecksum).To(Equal(created.CanonicalChecksum))

	page, err := ListTargetConfigSnapshots(ctx, types.TargetConfigListFilter{
		OrganizationID:   deps.organizationID,
		DeploymentUnitID: &placement.Unit.ID,
		Limit:            1,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(page.Items).To(HaveLen(1))
	g.Expect(page.Items[0].ID).To(Equal(created.ID))

	_, err = GetTargetConfigSnapshot(ctx, uuid.New(), created.ID)
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())

	_, err = pool.Exec(ctx, `
		UPDATE TargetConfigSnapshot
		SET adapter_version = 'changed'
		WHERE id = @id`, pgx.NamedArgs{"id": created.ID})
	expectTargetConfigPostgreSQLCode(t, err, pgerrcode.CheckViolation)
	_, err = pool.Exec(ctx, `
		DELETE FROM TargetConfigSnapshot
		WHERE id = @id`, pgx.NamedArgs{"id": created.ID})
	expectTargetConfigPostgreSQLCode(t, err, pgerrcode.CheckViolation)
}

func TestTargetConfigSnapshotOrganizationRetentionDeletesImmutableGraph(t *testing.T) {
	ctx, pool := deploymentRegistryIsolatedPool(t, 141)
	g := NewWithT(t)
	deps := createDeploymentRegistryDependencies(t, ctx)
	placement := createDeploymentRegistryPlacement(t, ctx, deps, "retention", time.Now().UTC())
	creatorID := createTargetConfigSnapshotTestUser(t, ctx, deps.organizationID)
	draft := targetConfigSnapshotRepositoryFixture(placement, deps.organizationID, creatorID)
	_, err := CreateTargetConfigSnapshot(ctx, &draft)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = pool.Exec(ctx, `
		UPDATE Organization
		SET deleted_at = now() - interval '2 days'
		WHERE id = @organizationID`,
		pgx.NamedArgs{"organizationID": deps.organizationID},
	)
	g.Expect(err).NotTo(HaveOccurred())

	deleted, err := DeleteOrganizationsOlderThan(ctx, 24*time.Hour)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(deleted).To(Equal(int64(1)))
	var count int
	g.Expect(pool.QueryRow(ctx, `
		SELECT count(*)
		FROM TargetConfigSnapshot
		WHERE organization_id = @organizationID`,
		pgx.NamedArgs{"organizationID": deps.organizationID},
	).Scan(&count)).To(Succeed())
	g.Expect(count).To(BeZero())
}

func TestTargetConfigSnapshotRepositoryRejectsDuplicateAndCrossPlacementIDs(t *testing.T) {
	ctx, _ := deploymentRegistryIsolatedPool(t, 141)
	g := NewWithT(t)
	deps := createDeploymentRegistryDependencies(t, ctx)
	placement := createDeploymentRegistryPlacement(t, ctx, deps, "primary", time.Now().UTC())
	creatorID := createTargetConfigSnapshotTestUser(t, ctx, deps.organizationID)
	draft := targetConfigSnapshotRepositoryFixture(placement, deps.organizationID, creatorID)
	_, err := CreateTargetConfigSnapshot(ctx, &draft)
	g.Expect(err).NotTo(HaveOccurred())

	_, err = CreateTargetConfigSnapshot(ctx, &draft)
	g.Expect(errors.Is(err, apierrors.ErrAlreadyExists)).To(BeTrue())

	otherDeps := createDeploymentRegistryDependencies(t, ctx)
	otherPlacement := createDeploymentRegistryPlacement(t, ctx, otherDeps, "other", time.Now().UTC())
	crossScope := targetConfigSnapshotRepositoryFixture(placement, deps.organizationID, creatorID)
	crossScope.TargetEnvironmentAssignmentID = otherPlacement.Assignment.ID
	_, err = CreateTargetConfigSnapshot(ctx, &crossScope)
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
}

func targetConfigSnapshotRepositoryFixture(
	placement types.DeploymentRegistryPlacement,
	organizationID,
	createdByUserAccountID uuid.UUID,
) types.TargetConfigSnapshotDraft {
	checksum := "sha256:" + strings.Repeat("a", 64)
	return types.TargetConfigSnapshotDraft{
		OrganizationID:                organizationID,
		CreatedByUserAccountID:        createdByUserAccountID,
		DeploymentUnitID:              placement.Unit.ID,
		TargetEnvironmentAssignmentID: placement.Assignment.ID,
		EnvironmentID:                 placement.Assignment.EnvironmentID,
		SourceRepository:              "https://git.example.invalid/config",
		SourceCommit:                  strings.Repeat("b", 40),
		SourceAdapter:                 "git",
		AdapterVersion:                "1.0.0",
		TargetPlatform:                "linux/amd64",
		RuntimeConstraints:            map[string]string{"runtime": "compose"},
		Objects: []types.TargetConfigSnapshotObjectDraft{{
			Key: "compose", Kind: types.TargetConfigObjectKindDeploymentDescriptor,
			Reference: "s3://bucket/_immutable/sha256/" + strings.Repeat("a", 64) + "/compose.yaml",
			MediaType: "application/yaml", SizeBytes: 10, Checksum: checksum,
		}},
		Components: []types.TargetConfigSnapshotComponentDraft{{
			PhysicalName:        placement.Instances[0].PhysicalName,
			ComponentInstanceID: placement.Instances[0].ID,
			DeploymentUnitID:    placement.Unit.ID,
		}},
		SecretReferences: []types.TargetConfigSnapshotSecretReferenceDraft{{
			Key: "database", Provider: "vault", Reference: "kv:database",
			VersionFingerprint: "sha256:" + strings.Repeat("c", 64),
		}},
		FeatureFlags: []types.TargetConfigSnapshotFeatureFlagDraft{{Key: "audit", Enabled: true}},
	}
}

func createTargetConfigSnapshotTestUser(
	t *testing.T,
	ctx context.Context,
	organizationID uuid.UUID,
) uuid.UUID {
	t.Helper()
	g := NewWithT(t)
	var userID uuid.UUID
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
		INSERT INTO UserAccount (email)
		VALUES (@email)
		RETURNING id`,
		pgx.NamedArgs{"email": uuid.NewString() + "@target-config.invalid"},
	).Scan(&userID)).To(Succeed())
	_, err := internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO Organization_UserAccount (
			organization_id,
			user_account_id,
			user_role
		) VALUES (
			@organizationID,
			@userID,
			'admin'
		)`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"userID":         userID,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	return userID
}

func expectTargetConfigPostgreSQLCode(t *testing.T, err error, code string) {
	t.Helper()
	g := NewWithT(t)
	g.Expect(err).To(HaveOccurred())
	var postgresError *pgconn.PgError
	g.Expect(errors.As(err, &postgresError)).To(BeTrue())
	g.Expect(postgresError.Code).To(Equal(code))
}

func TestTargetConfigSnapshotMigration141DowngradeGuard(t *testing.T) {
	ctx, pool := deploymentRegistryIsolatedPool(t, 141)
	g := NewWithT(t)
	deps := createDeploymentRegistryDependencies(t, ctx)
	placement := createDeploymentRegistryPlacement(t, ctx, deps, "downgrade", time.Now().UTC())
	creatorID := createTargetConfigSnapshotTestUser(t, ctx, deps.organizationID)
	draft := targetConfigSnapshotRepositoryFixture(placement, deps.organizationID, creatorID)
	_, err := CreateTargetConfigSnapshot(ctx, &draft)
	g.Expect(err).NotTo(HaveOccurred())

	down, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "141_target_config_snapshots.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	_, err = pool.Exec(ctx, string(down))
	g.Expect(err).To(MatchError(ContainSubstring(
		"downgrade crossing 141 is forbidden while target config snapshots exist",
	)))

	var count int
	g.Expect(internalctx.GetDb(ctx).QueryRow(
		ctx,
		`SELECT count(*) FROM TargetConfigSnapshot`,
	).Scan(&count)).To(Succeed())
	g.Expect(count).To(Equal(1))
}
