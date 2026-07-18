package db

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
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

func TestMigration142DefinesImmutableV1ExtractionEvidence(t *testing.T) {
	g := NewWithT(t)
	up, err := os.ReadFile(filepath.Join(
		"..",
		"migrations",
		"sql",
		"142_release_contract_v1_extraction.up.sql",
	))
	g.Expect(err).NotTo(HaveOccurred())
	down, err := os.ReadFile(filepath.Join(
		"..",
		"migrations",
		"sql",
		"142_release_contract_v1_extraction.down.sql",
	))
	g.Expect(err).NotTo(HaveOccurred())

	upSQL := string(up)
	g.Expect(upSQL).To(ContainSubstring("CREATE TABLE BackfillCheckpoint"))
	g.Expect(upSQL).To(ContainSubstring("CREATE TABLE BackfillCheckpointSourceMembership"))
	g.Expect(upSQL).To(ContainSubstring("CREATE TABLE ReleaseContractV1ExtractionLineage"))
	g.Expect(upSQL).To(ContainSubstring("original_release_checksum"))
	g.Expect(upSQL).To(ContainSubstring("original_plan_checksum"))
	g.Expect(upSQL).To(ContainSubstring("derived_snapshot_checksum"))
	g.Expect(upSQL).To(ContainSubstring("predecessor_checkpoint_id UUID"))
	g.Expect(upSQL).To(ContainSubstring("source_membership_checkpoint_id UUID"))
	g.Expect(upSQL).To(ContainSubstring("source_membership_checksum TEXT NOT NULL"))
	g.Expect(upSQL).To(ContainSubstring("source_after_created_at TIMESTAMP"))
	g.Expect(upSQL).To(ContainSubstring("source_after_plan_id UUID"))
	g.Expect(upSQL).To(ContainSubstring("source_through_created_at TIMESTAMP"))
	g.Expect(upSQL).To(ContainSubstring("source_through_plan_id UUID"))
	g.Expect(upSQL).To(ContainSubstring("source_high_water_created_at TIMESTAMP"))
	g.Expect(upSQL).To(ContainSubstring("source_high_water_plan_id UUID"))
	g.Expect(upSQL).To(ContainSubstring("actor_user_account_id UUID NOT NULL"))
	g.Expect(upSQL).To(ContainSubstring("source_count <= batch_size"))
	g.Expect(upSQL).To(ContainSubstring(
		"UNIQUE (predecessor_checkpoint_id, organization_id)",
	))
	g.Expect(upSQL).To(MatchRegexp(
		`(?s)FOREIGN KEY \(\s*organization_id,\s*actor_user_account_id\s*\)` +
			`\s*REFERENCES Organization_UserAccount\(\s*organization_id,\s*user_account_id\s*\)`,
	))
	g.Expect(upSQL).To(MatchRegexp(
		`(?s)FOREIGN KEY \(\s*predecessor_checkpoint_id,\s*organization_id\s*\)` +
			`\s*REFERENCES BackfillCheckpoint\(id,\s*organization_id\)`,
	))
	g.Expect(upSQL).To(MatchRegexp(
		`(?s)FOREIGN KEY \(\s*original_release_bundle_id,\s*organization_id,\s*original_release_checksum\s*\)`,
	))
	g.Expect(upSQL).To(MatchRegexp(
		`(?s)FOREIGN KEY \(\s*original_plan_id,\s*organization_id,\s*original_plan_checksum\s*\)`,
	))
	g.Expect(upSQL).To(MatchRegexp(
		`(?s)FOREIGN KEY \(\s*derived_snapshot_id,\s*organization_id,\s*derived_snapshot_checksum\s*\)`,
	))
	g.Expect(upSQL).To(ContainSubstring("CREATE TRIGGER BackfillCheckpoint_immutable"))
	g.Expect(upSQL).To(ContainSubstring("CREATE TRIGGER BackfillCheckpointSourceMembership_immutable"))
	g.Expect(upSQL).To(ContainSubstring("CREATE TRIGGER ReleaseContractV1ExtractionLineage_immutable"))
	g.Expect(upSQL).NotTo(ContainSubstring("secret_value"))
	g.Expect(upSQL).NotTo(ContainSubstring("canonical_payload"))
	g.Expect(string(down)).To(ContainSubstring(
		"downgrade crossing 142 is forbidden while v1 extraction evidence exists",
	))
}

func TestV1ExtractionCheckpointIsDeterministicAndStateBound(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	actorUserAccountID := uuid.New()
	predecessorCheckpointID := uuid.New()
	sourceMembershipCheckpointID := uuid.New()
	sourceMembershipChecksum := "sha256:" + strings.Repeat("e", 64)
	afterPlanID := uuid.New()
	afterCreatedAt := time.Date(2026, time.July, 18, 1, 2, 3, 0, time.UTC)
	highWaterPlanID := uuid.New()
	highWaterCreatedAt := afterCreatedAt.Add(2 * time.Hour)
	after := &v1ExtractionPlanCursor{
		CreatedAt: afterCreatedAt,
		PlanID:    afterPlanID,
	}
	highWater := &v1ExtractionPlanCursor{
		CreatedAt: highWaterCreatedAt,
		PlanID:    highWaterPlanID,
	}
	outcomes := []v1ExtractionOutcome{{
		ReleaseBundleID:  uuid.New(),
		ReleaseChecksum:  "sha256:" + strings.Repeat("a", 64),
		PlanCreatedAt:    afterCreatedAt.Add(time.Hour),
		PlanID:           uuid.New(),
		PlanChecksum:     "sha256:" + strings.Repeat("b", 64),
		Status:           types.V1ExtractionStatusCandidate,
		SnapshotChecksum: "sha256:" + strings.Repeat("c", 64),
	}}

	first, err := buildV1ExtractionCheckpoint(
		organizationID,
		actorUserAccountID,
		&predecessorCheckpointID,
		&sourceMembershipCheckpointID,
		sourceMembershipChecksum,
		after,
		highWater,
		false,
		100,
		outcomes,
	)
	g.Expect(err).NotTo(HaveOccurred())
	second, err := buildV1ExtractionCheckpoint(
		organizationID,
		actorUserAccountID,
		&predecessorCheckpointID,
		&sourceMembershipCheckpointID,
		sourceMembershipChecksum,
		after,
		highWater,
		false,
		25,
		outcomes,
	)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(second.ID).To(Equal(first.ID))
	g.Expect(second.DryRunChecksum).To(Equal(first.DryRunChecksum))
	g.Expect(second.SourceStateChecksum).To(Equal(first.SourceStateChecksum))
	g.Expect(second.SourceMembershipChecksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
	g.Expect(second.BatchSize).To(Equal(25))
	g.Expect(second.PredecessorCheckpointID).NotTo(BeNil())
	g.Expect(*second.PredecessorCheckpointID).To(Equal(predecessorCheckpointID))
	g.Expect(second.SourceMembershipCheckpointID).NotTo(BeNil())
	g.Expect(*second.SourceMembershipCheckpointID).To(Equal(sourceMembershipCheckpointID))
	g.Expect(second.SourceMembershipChecksum).To(Equal(sourceMembershipChecksum))
	g.Expect(second.SourceAfterCreatedAt).NotTo(BeNil())
	g.Expect(*second.SourceAfterCreatedAt).To(BeTemporally("==", afterCreatedAt))
	g.Expect(second.SourceAfterPlanID).NotTo(BeNil())
	g.Expect(*second.SourceAfterPlanID).To(Equal(afterPlanID))
	g.Expect(second.SourceThroughCreatedAt).NotTo(BeNil())
	g.Expect(*second.SourceThroughCreatedAt).To(BeTemporally("==", outcomes[0].PlanCreatedAt))
	g.Expect(second.SourceThroughPlanID).NotTo(BeNil())
	g.Expect(*second.SourceThroughPlanID).To(Equal(outcomes[0].PlanID))
	g.Expect(second.SourceHighWaterCreatedAt).NotTo(BeNil())
	g.Expect(*second.SourceHighWaterCreatedAt).To(BeTemporally("==", highWaterCreatedAt))
	g.Expect(second.SourceHighWaterPlanID).NotTo(BeNil())
	g.Expect(*second.SourceHighWaterPlanID).To(Equal(highWaterPlanID))
	g.Expect(second.HasMore).To(BeFalse())

	outcomes[0].SnapshotChecksum = "sha256:" + strings.Repeat("d", 64)
	changed, err := buildV1ExtractionCheckpoint(
		organizationID,
		actorUserAccountID,
		&predecessorCheckpointID,
		&sourceMembershipCheckpointID,
		sourceMembershipChecksum,
		after,
		highWater,
		false,
		100,
		outcomes,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(changed.ID).NotTo(Equal(first.ID))
	g.Expect(changed.DryRunChecksum).NotTo(Equal(first.DryRunChecksum))
	g.Expect(changed.SourceStateChecksum).To(Equal(first.SourceStateChecksum))

	hasMore, err := buildV1ExtractionCheckpoint(
		organizationID,
		actorUserAccountID,
		&predecessorCheckpointID,
		&sourceMembershipCheckpointID,
		sourceMembershipChecksum,
		after,
		highWater,
		true,
		100,
		outcomes,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(hasMore.DryRunChecksum).NotTo(Equal(first.DryRunChecksum))
	g.Expect(hasMore.HasMore).To(BeTrue())

	differentActor, err := buildV1ExtractionCheckpoint(
		organizationID,
		uuid.New(),
		&predecessorCheckpointID,
		&sourceMembershipCheckpointID,
		sourceMembershipChecksum,
		after,
		highWater,
		false,
		100,
		outcomes,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(differentActor.ID).NotTo(Equal(changed.ID))
	g.Expect(differentActor.DryRunChecksum).NotTo(Equal(changed.DryRunChecksum))
	g.Expect(differentActor.SourceStateChecksum).To(Equal(changed.SourceStateChecksum))

	outcomes[0].PlanCreatedAt = outcomes[0].PlanCreatedAt.Add(time.Nanosecond)
	movedSource, err := buildV1ExtractionCheckpoint(
		organizationID,
		actorUserAccountID,
		&predecessorCheckpointID,
		&sourceMembershipCheckpointID,
		sourceMembershipChecksum,
		after,
		highWater,
		false,
		100,
		outcomes,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(movedSource.SourceStateChecksum).NotTo(Equal(changed.SourceStateChecksum))
}

func TestV1ExtractionMembershipChecksumAndPagingUseCapturedRowsOnly(t *testing.T) {
	g := NewWithT(t)
	baseCreatedAt := time.Date(2026, time.July, 18, 1, 2, 3, 0, time.UTC)
	first := v1ExtractionPlanCursor{
		CreatedAt: baseCreatedAt,
		PlanID:    uuid.MustParse("10000000-0000-0000-0000-000000000000"),
	}
	last := v1ExtractionPlanCursor{
		CreatedAt: baseCreatedAt.Add(2 * time.Minute),
		PlanID:    uuid.MustParse("30000000-0000-0000-0000-000000000000"),
	}
	captured := []v1ExtractionPlanCursor{first, last}
	checksum, err := checksumV1ExtractionMembership(captured)
	g.Expect(err).NotTo(HaveOccurred())

	lateVisibleInsideBounds := v1ExtractionPlanCursor{
		CreatedAt: baseCreatedAt.Add(time.Minute),
		PlanID:    uuid.MustParse("20000000-0000-0000-0000-000000000000"),
	}
	changedChecksum, err := checksumV1ExtractionMembership([]v1ExtractionPlanCursor{
		first,
		lateVisibleInsideBounds,
		last,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(changedChecksum).NotTo(Equal(checksum))

	page, hasMore := pageV1ExtractionMembership(captured, &first, 100)
	g.Expect(hasMore).To(BeFalse())
	g.Expect(page).To(Equal([]v1ExtractionPlanCursor{last}))
	g.Expect(page).NotTo(ContainElement(lateVisibleInsideBounds))
}

func TestTargetConfigV1ExtractionRequiresCheckpointOrganizationMemberActor(t *testing.T) {
	ctx, pool := deploymentRegistryIsolatedPool(t, 142)
	g := NewWithT(t)
	first := createTargetConfigV1RepositoryFixture(
		t,
		ctx,
		"actor-first",
		types.ReleaseContractSchemaV1,
	)
	second := createTargetConfigV1RepositoryFixture(
		t,
		ctx,
		"actor-second",
		types.ReleaseContractSchemaV1,
	)

	_, err := CreateTargetConfigV1ExtractionDryRun(
		ctx,
		first.organizationID,
		uuid.Nil,
		nil,
		100,
		first.verifier,
	)
	g.Expect(err).To(MatchError(ContainSubstring("actorUserAccountId")))

	_, err = CreateTargetConfigV1ExtractionDryRun(
		ctx,
		first.organizationID,
		second.actorUserAccountID,
		nil,
		100,
		first.verifier,
	)
	g.Expect(err).To(MatchError(ContainSubstring(
		"actorUserAccountId must be a member of the organization",
	)))
	var checkpointCount int
	g.Expect(pool.QueryRow(ctx, `
		SELECT count(*)
		FROM BackfillCheckpoint
		WHERE organization_id = @organizationID`,
		pgx.NamedArgs{"organizationID": first.organizationID},
	).Scan(&checkpointCount)).To(Succeed())
	g.Expect(checkpointCount).To(BeZero())

	dryRun, err := CreateTargetConfigV1ExtractionDryRun(
		ctx,
		first.organizationID,
		first.actorUserAccountID,
		nil,
		100,
		first.verifier,
	)
	g.Expect(err).NotTo(HaveOccurred())
	otherMember := createTargetConfigSnapshotTestUser(t, ctx, first.organizationID)
	_, err = ApplyTargetConfigV1Extraction(
		ctx,
		first.organizationID,
		otherMember,
		dryRun.Checkpoint.ID,
		dryRun.Checkpoint.DryRunChecksum,
		100,
		first.verifier,
	)
	g.Expect(err).To(MatchError(ContainSubstring(
		"v1 extraction dry-run approval does not match",
	)))
	var snapshotCount int
	g.Expect(pool.QueryRow(ctx, `
		SELECT count(*)
		FROM TargetConfigSnapshot
		WHERE organization_id = @organizationID`,
		pgx.NamedArgs{"organizationID": first.organizationID},
	).Scan(&snapshotCount)).To(Succeed())
	g.Expect(snapshotCount).To(BeZero())
}

func TestTargetConfigV1ExtractionRepositoryDryRunApplyRestartAndNoOp(t *testing.T) {
	ctx, _ := deploymentRegistryIsolatedPool(t, 142)
	g := NewWithT(t)
	fixture := createTargetConfigV1RepositoryFixture(
		t,
		ctx,
		"repository-flow",
		types.ReleaseContractSchemaV1,
	)
	originalBundlePayload := append([]byte(nil), fixture.bundle.CanonicalPayload...)
	originalPlanPayload := append([]byte(nil), fixture.plan.CanonicalPayload...)

	dryRun, err := CreateTargetConfigV1ExtractionDryRun(
		ctx,
		fixture.organizationID,
		fixture.actorUserAccountID,
		nil,
		100,
		fixture.verifier,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(dryRun.Checkpoint.SourceCount).To(Equal(1))
	g.Expect(dryRun.Checkpoint.CandidateCount).To(Equal(1))
	g.Expect(dryRun.Pending).To(Equal(1))
	g.Expect(dryRun.Items).To(HaveLen(1))

	applied, err := ApplyTargetConfigV1Extraction(
		ctx,
		fixture.organizationID,
		fixture.actorUserAccountID,
		dryRun.Checkpoint.ID,
		dryRun.Checkpoint.DryRunChecksum,
		100,
		fixture.verifier,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(applied.Applied).To(Equal(1))
	g.Expect(applied.Pending).To(BeZero())

	restarted, err := ApplyTargetConfigV1Extraction(
		ctx,
		fixture.organizationID,
		fixture.actorUserAccountID,
		dryRun.Checkpoint.ID,
		dryRun.Checkpoint.DryRunChecksum,
		100,
		fixture.verifier,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(restarted.Applied).To(Equal(1))
	g.Expect(restarted.NoOp).To(Equal(1))

	var snapshotCount, appliedLineageCount int
	var createdByUserAccountID uuid.UUID
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT count(*), min(created_by_user_account_id::text)::uuid
		FROM TargetConfigSnapshot
		WHERE organization_id = @organizationID`,
		pgx.NamedArgs{"organizationID": fixture.organizationID},
	).Scan(&snapshotCount, &createdByUserAccountID)).To(Succeed())
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT count(*)
		FROM ReleaseContractV1ExtractionLineage
		WHERE organization_id = @organizationID
		  AND checkpoint_id = @checkpointID
		  AND status = 'applied'`,
		pgx.NamedArgs{
			"organizationID": fixture.organizationID,
			"checkpointID":   dryRun.Checkpoint.ID,
		},
	).Scan(&appliedLineageCount)).To(Succeed())
	g.Expect(snapshotCount).To(Equal(1))
	g.Expect(createdByUserAccountID).To(Equal(fixture.actorUserAccountID))
	g.Expect(appliedLineageCount).To(Equal(1))

	reloadedBundle, err := GetReleaseBundle(
		ctx,
		fixture.bundle.ID,
		fixture.organizationID,
	)
	g.Expect(err).NotTo(HaveOccurred())
	reloadedPlan, err := GetDeploymentPlan(
		ctx,
		fixture.plan.ID,
		fixture.organizationID,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(reloadedBundle.CanonicalPayload).To(Equal(originalBundlePayload))
	g.Expect(reloadedBundle.CanonicalChecksum).To(Equal(fixture.bundle.CanonicalChecksum))
	g.Expect(reloadedPlan.CanonicalPayload).To(Equal(originalPlanPayload))
	g.Expect(reloadedPlan.CanonicalChecksum).To(Equal(fixture.plan.CanonicalChecksum))
	g.Expect(reloadedPlan.ReleaseContract.Schema).To(Equal(types.ReleaseContractSchemaV1))
}

func TestTargetConfigV1ExtractionDistinctCheckpointsConcurrentlyReuseSnapshotChecksum(t *testing.T) {
	ctx, _ := deploymentRegistryIsolatedPool(t, 142)
	g := NewWithT(t)
	fixture := createTargetConfigV1RepositoryFixture(
		t,
		ctx,
		"concurrent-apply",
		types.ReleaseContractSchemaV1,
	)
	secondActorUserAccountID := createTargetConfigSnapshotTestUser(
		t,
		ctx,
		fixture.organizationID,
	)
	firstDryRun, err := CreateTargetConfigV1ExtractionDryRun(
		ctx,
		fixture.organizationID,
		fixture.actorUserAccountID,
		nil,
		100,
		fixture.verifier,
	)
	g.Expect(err).NotTo(HaveOccurred())
	secondDryRun, err := CreateTargetConfigV1ExtractionDryRun(
		ctx,
		fixture.organizationID,
		secondActorUserAccountID,
		nil,
		100,
		fixture.verifier,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(secondDryRun.Checkpoint.ID).NotTo(Equal(firstDryRun.Checkpoint.ID))
	g.Expect(secondDryRun.Items).To(HaveLen(1))
	g.Expect(firstDryRun.Items).To(HaveLen(1))
	g.Expect(secondDryRun.Items[0].DerivedSnapshotChecksum).To(Equal(
		firstDryRun.Items[0].DerivedSnapshotChecksum,
	))

	start := make(chan struct{})
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	checkpoints := []struct {
		actorUserAccountID uuid.UUID
		dryRun             *types.V1ExtractionReport
		verifier           *targetConfigV1BlockingObjectVerifier
	}{
		{
			actorUserAccountID: fixture.actorUserAccountID,
			dryRun:             firstDryRun,
			verifier: &targetConfigV1BlockingObjectVerifier{
				delegate: fixture.verifier,
				entered:  entered,
				release:  release,
			},
		},
		{
			actorUserAccountID: secondActorUserAccountID,
			dryRun:             secondDryRun,
			verifier: &targetConfigV1BlockingObjectVerifier{
				delegate: fixture.verifier,
				entered:  entered,
				release:  release,
			},
		},
	}
	for _, checkpoint := range checkpoints {
		workers.Add(1)
		go func(checkpoint struct {
			actorUserAccountID uuid.UUID
			dryRun             *types.V1ExtractionReport
			verifier           *targetConfigV1BlockingObjectVerifier
		}) {
			defer workers.Done()
			<-start
			_, applyErr := ApplyTargetConfigV1Extraction(
				ctx,
				fixture.organizationID,
				checkpoint.actorUserAccountID,
				checkpoint.dryRun.Checkpoint.ID,
				checkpoint.dryRun.Checkpoint.DryRunChecksum,
				100,
				checkpoint.verifier,
			)
			results <- applyErr
		}(checkpoint)
	}
	close(start)
	for range 2 {
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			t.Fatal("distinct checkpoint applies did not reach the shared insertion barrier")
		}
	}
	close(release)
	workers.Wait()
	close(results)
	for applyErr := range results {
		g.Expect(applyErr).NotTo(HaveOccurred())
	}

	var snapshotCount, appliedCount int
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT count(*)
		FROM TargetConfigSnapshot
		WHERE organization_id = @organizationID`,
		pgx.NamedArgs{"organizationID": fixture.organizationID},
	).Scan(&snapshotCount)).To(Succeed())
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT count(*)
		FROM ReleaseContractV1ExtractionLineage
		WHERE organization_id = @organizationID
		  AND status = 'applied'`,
		pgx.NamedArgs{
			"organizationID": fixture.organizationID,
		},
	).Scan(&appliedCount)).To(Succeed())
	g.Expect(snapshotCount).To(Equal(1))
	g.Expect(appliedCount).To(Equal(2))
}

func TestTargetConfigV1ExtractionRejectsRegistryChangeDuringApply(t *testing.T) {
	ctx, pool := deploymentRegistryIsolatedPool(t, 142)
	g := NewWithT(t)
	fixture := createTargetConfigV1RepositoryFixture(
		t,
		ctx,
		"registry-race",
		types.ReleaseContractSchemaV1,
	)
	dryRun, err := CreateTargetConfigV1ExtractionDryRun(
		ctx,
		fixture.organizationID,
		fixture.actorUserAccountID,
		nil,
		100,
		fixture.verifier,
	)
	g.Expect(err).NotTo(HaveOccurred())
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	blockingVerifier := &targetConfigV1BlockingObjectVerifier{
		delegate: fixture.verifier,
		entered:  entered,
		release:  release,
	}
	result := make(chan error, 1)
	go func() {
		_, applyErr := ApplyTargetConfigV1Extraction(
			ctx,
			fixture.organizationID,
			fixture.actorUserAccountID,
			dryRun.Checkpoint.ID,
			dryRun.Checkpoint.DryRunChecksum,
			100,
			blockingVerifier,
		)
		result <- applyErr
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("apply did not reach object evidence barrier")
	}
	_, err = pool.Exec(ctx, `
		UPDATE ComponentInstance
		SET physical_name = @physicalName,
		    updated_at = now()
		WHERE id = @id
		  AND organization_id = @organizationID`,
		pgx.NamedArgs{
			"physicalName":   "renamed-registry-race",
			"id":             fixture.placement.Instances[0].ID,
			"organizationID": fixture.organizationID,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	close(release)
	select {
	case applyErr := <-result:
		g.Expect(applyErr).To(HaveOccurred())
	case <-time.After(10 * time.Second):
		t.Fatal("apply did not finish after registry mutation")
	}

	var snapshotCount, appliedCount int
	g.Expect(pool.QueryRow(ctx, `
		SELECT count(*) FROM TargetConfigSnapshot
		WHERE organization_id = @organizationID`,
		pgx.NamedArgs{"organizationID": fixture.organizationID},
	).Scan(&snapshotCount)).To(Succeed())
	g.Expect(pool.QueryRow(ctx, `
		SELECT count(*)
		FROM ReleaseContractV1ExtractionLineage
		WHERE organization_id = @organizationID
		  AND checkpoint_id = @checkpointID
		  AND status = 'applied'`,
		pgx.NamedArgs{
			"organizationID": fixture.organizationID,
			"checkpointID":   dryRun.Checkpoint.ID,
		},
	).Scan(&appliedCount)).To(Succeed())
	g.Expect(snapshotCount).To(BeZero())
	g.Expect(appliedCount).To(BeZero())
}

func TestTargetConfigV1ExtractionApplyRollsBackSnapshotWhenLineageFails(t *testing.T) {
	ctx, pool := deploymentRegistryIsolatedPool(t, 142)
	g := NewWithT(t)
	fixture := createTargetConfigV1RepositoryFixture(
		t,
		ctx,
		"atomic-rollback",
		types.ReleaseContractSchemaV1,
	)
	dryRun, err := CreateTargetConfigV1ExtractionDryRun(
		ctx,
		fixture.organizationID,
		fixture.actorUserAccountID,
		nil,
		100,
		fixture.verifier,
	)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = pool.Exec(ctx, `
		CREATE FUNCTION target_config_v1_test_fail_applied_lineage()
		RETURNS trigger
		LANGUAGE plpgsql
		AS $$
		BEGIN
		  IF NEW.status = 'applied' THEN
		    RAISE EXCEPTION 'test interruption after snapshot';
		  END IF;
		  RETURN NEW;
		END;
		$$;
		CREATE TRIGGER TargetConfigV1_test_fail_applied_lineage
		BEFORE INSERT ON ReleaseContractV1ExtractionLineage
		FOR EACH ROW EXECUTE FUNCTION target_config_v1_test_fail_applied_lineage();`)
	g.Expect(err).NotTo(HaveOccurred())

	_, err = ApplyTargetConfigV1Extraction(
		ctx,
		fixture.organizationID,
		fixture.actorUserAccountID,
		dryRun.Checkpoint.ID,
		dryRun.Checkpoint.DryRunChecksum,
		100,
		fixture.verifier,
	)
	g.Expect(err).To(HaveOccurred())
	var snapshotCount int
	g.Expect(pool.QueryRow(ctx, `
		SELECT count(*) FROM TargetConfigSnapshot
		WHERE organization_id = @organizationID`,
		pgx.NamedArgs{"organizationID": fixture.organizationID},
	).Scan(&snapshotCount)).To(Succeed())
	g.Expect(snapshotCount).To(BeZero())

	_, err = pool.Exec(ctx, `
		DROP TRIGGER TargetConfigV1_test_fail_applied_lineage
		  ON ReleaseContractV1ExtractionLineage;
		DROP FUNCTION target_config_v1_test_fail_applied_lineage();`)
	g.Expect(err).NotTo(HaveOccurred())
	report, err := ApplyTargetConfigV1Extraction(
		ctx,
		fixture.organizationID,
		fixture.actorUserAccountID,
		dryRun.Checkpoint.ID,
		dryRun.Checkpoint.DryRunChecksum,
		100,
		fixture.verifier,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(report.Applied).To(Equal(1))
}

func TestTargetConfigV1ExtractionTenantForeignKeysAndDowngradeGuard(t *testing.T) {
	ctx, pool := deploymentRegistryIsolatedPool(t, 142)
	g := NewWithT(t)
	first := createTargetConfigV1RepositoryFixture(
		t,
		ctx,
		"tenant-a",
		types.ReleaseContractSchemaV1,
	)
	second := createTargetConfigV1RepositoryFixture(
		t,
		ctx,
		"tenant-b",
		types.ReleaseContractSchemaV1,
	)
	dryRun, err := CreateTargetConfigV1ExtractionDryRun(
		ctx,
		first.organizationID,
		first.actorUserAccountID,
		nil,
		100,
		first.verifier,
	)
	g.Expect(err).NotTo(HaveOccurred())

	_, err = pool.Exec(ctx, `
		INSERT INTO ReleaseContractV1ExtractionLineage (
			organization_id,
			checkpoint_id,
			original_release_bundle_id,
			original_release_checksum,
			original_plan_id,
			original_plan_checksum,
			derived_snapshot_checksum,
			extractor_version,
			status,
			blocked_reason_code
		) VALUES (
			@organizationID,
			@checkpointID,
			@releaseBundleID,
			@releaseChecksum,
			@foreignPlanID,
			@foreignPlanChecksum,
			@derivedChecksum,
			@extractorVersion,
			'candidate',
			''
		)`,
		pgx.NamedArgs{
			"organizationID":      first.organizationID,
			"checkpointID":        dryRun.Checkpoint.ID,
			"releaseBundleID":     first.bundle.ID,
			"releaseChecksum":     first.bundle.CanonicalChecksum,
			"foreignPlanID":       second.plan.ID,
			"foreignPlanChecksum": second.plan.CanonicalChecksum,
			"derivedChecksum":     "sha256:" + strings.Repeat("f", 64),
			"extractorVersion":    targetConfigV1ExtractorVersion,
		},
	)
	expectTargetConfigPostgreSQLCode(t, err, pgerrcode.ForeignKeyViolation)

	down, err := os.ReadFile(filepath.Join(
		"..",
		"migrations",
		"sql",
		"142_release_contract_v1_extraction.down.sql",
	))
	g.Expect(err).NotTo(HaveOccurred())
	_, err = pool.Exec(ctx, string(down))
	g.Expect(err).To(MatchError(ContainSubstring(
		"downgrade crossing 142 is forbidden while v1 extraction evidence exists",
	)))
	var checkpointCount int
	g.Expect(pool.QueryRow(ctx, `
		SELECT count(*) FROM BackfillCheckpoint
		WHERE id = @checkpointID`,
		pgx.NamedArgs{"checkpointID": dryRun.Checkpoint.ID},
	).Scan(&checkpointCount)).To(Succeed())
	g.Expect(checkpointCount).To(Equal(1))
}

func TestTargetConfigV1ExtractionIgnoresV2SourcesInMixedHistory(t *testing.T) {
	ctx, _ := deploymentRegistryIsolatedPool(t, 142)
	g := NewWithT(t)
	v1 := createTargetConfigV1RepositoryFixture(
		t,
		ctx,
		"mixed-v1",
		types.ReleaseContractSchemaV1,
	)
	_ = createTargetConfigV1RepositoryFixtureForOrganization(
		t,
		ctx,
		"mixed-v2",
		"distr.component-release/v2",
		v1.organizationID,
	)

	report, err := CreateTargetConfigV1ExtractionDryRun(
		ctx,
		v1.organizationID,
		v1.actorUserAccountID,
		nil,
		100,
		v1.verifier,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(report.Checkpoint.SourceCount).To(Equal(1))
	g.Expect(report.Items).To(HaveLen(1))
	g.Expect(report.Items[0].OriginalPlanID).To(Equal(v1.plan.ID))
}

func TestTargetConfigV1ExtractionChainsAppliedCreatedAtCursorWithinHighWater(t *testing.T) {
	ctx, _ := deploymentRegistryIsolatedPool(t, 142)
	g := NewWithT(t)
	fixtures := []targetConfigV1RepositoryFixture{createTargetConfigV1RepositoryFixture(
		t,
		ctx,
		"cursor-a",
		types.ReleaseContractSchemaV1,
	)}
	fixtures = append(
		fixtures,
		createTargetConfigV1RepositoryFixtureForOrganization(
			t,
			ctx,
			"cursor-b",
			types.ReleaseContractSchemaV1,
			fixtures[0].organizationID,
		),
		createTargetConfigV1RepositoryFixtureForOrganization(
			t,
			ctx,
			"cursor-c",
			types.ReleaseContractSchemaV1,
			fixtures[0].organizationID,
		),
	)
	sort.Slice(fixtures, func(left, right int) bool {
		return fixtures[left].plan.ID.String() < fixtures[right].plan.ID.String()
	})
	baseCreatedAt := time.Now().UTC().Truncate(time.Microsecond).Add(-10 * time.Minute)
	for index := range fixtures {
		createdAt := baseCreatedAt.Add(time.Duration(len(fixtures)-index) * time.Minute)
		_, err := internalctx.GetDb(ctx).Exec(ctx, `
			UPDATE DeploymentPlan
			SET created_at = @createdAt
			WHERE id = @planID
			  AND organization_id = @organizationID`,
			pgx.NamedArgs{
				"createdAt":      createdAt,
				"planID":         fixtures[index].plan.ID,
				"organizationID": fixtures[index].organizationID,
			},
		)
		g.Expect(err).NotTo(HaveOccurred())
		fixtures[index].plan.CreatedAt = createdAt
	}
	sort.Slice(fixtures, func(left, right int) bool {
		return compareV1ExtractionPlanPosition(
			fixtures[left].plan.CreatedAt,
			fixtures[left].plan.ID,
			fixtures[right].plan.CreatedAt,
			fixtures[right].plan.ID,
		) < 0
	})
	g.Expect(strings.Compare(
		fixtures[0].plan.ID.String(),
		fixtures[2].plan.ID.String(),
	)).To(Equal(1))

	first, err := CreateTargetConfigV1ExtractionDryRun(
		ctx,
		fixtures[0].organizationID,
		fixtures[0].actorUserAccountID,
		nil,
		1,
		fixtures[0].verifier,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(first.Checkpoint.SourceCount).To(Equal(1))
	g.Expect(first.Checkpoint.PredecessorCheckpointID).To(BeNil())
	g.Expect(first.Checkpoint.SourceAfterCreatedAt).To(BeNil())
	g.Expect(first.Checkpoint.SourceAfterPlanID).To(BeNil())
	g.Expect(first.Checkpoint.SourceThroughCreatedAt).NotTo(BeNil())
	g.Expect(*first.Checkpoint.SourceThroughCreatedAt).To(BeTemporally(
		"==",
		fixtures[0].plan.CreatedAt,
	))
	g.Expect(first.Checkpoint.SourceThroughPlanID).NotTo(BeNil())
	g.Expect(*first.Checkpoint.SourceThroughPlanID).To(Equal(fixtures[0].plan.ID))
	g.Expect(first.Checkpoint.SourceHighWaterCreatedAt).NotTo(BeNil())
	g.Expect(*first.Checkpoint.SourceHighWaterCreatedAt).To(BeTemporally(
		"==",
		fixtures[2].plan.CreatedAt,
	))
	g.Expect(first.Checkpoint.SourceHighWaterPlanID).NotTo(BeNil())
	g.Expect(*first.Checkpoint.SourceHighWaterPlanID).To(Equal(fixtures[2].plan.ID))
	g.Expect(first.Checkpoint.HasMore).To(BeTrue())
	g.Expect(first.Items).To(HaveLen(1))
	g.Expect(first.Items[0].OriginalPlanID).To(Equal(fixtures[0].plan.ID))

	lateFixture := createTargetConfigV1RepositoryFixtureForOrganization(
		t,
		ctx,
		"cursor-late-visible-inside-window",
		types.ReleaseContractSchemaV1,
		fixtures[0].organizationID,
	)
	lateCreatedAt := fixtures[1].plan.CreatedAt.Add(30 * time.Second)
	_, err = internalctx.GetDb(ctx).Exec(ctx, `
		UPDATE DeploymentPlan
		SET created_at = @createdAt
		WHERE id = @planID
		  AND organization_id = @organizationID`,
		pgx.NamedArgs{
			"createdAt":      lateCreatedAt,
			"planID":         lateFixture.plan.ID,
			"organizationID": lateFixture.organizationID,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = CreateTargetConfigV1ExtractionDryRun(
		ctx,
		fixtures[0].organizationID,
		fixtures[0].actorUserAccountID,
		&first.Checkpoint.ID,
		1,
		fixtures[0].verifier,
	)
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
	g.Expect(err).To(MatchError(ContainSubstring(
		"predecessor checkpoint must be fully applied",
	)))

	_, err = ApplyTargetConfigV1Extraction(
		ctx,
		fixtures[0].organizationID,
		fixtures[0].actorUserAccountID,
		first.Checkpoint.ID,
		first.Checkpoint.DryRunChecksum,
		100,
		fixtures[0].verifier,
	)
	g.Expect(err).NotTo(HaveOccurred())

	second, err := CreateTargetConfigV1ExtractionDryRun(
		ctx,
		fixtures[0].organizationID,
		fixtures[0].actorUserAccountID,
		&first.Checkpoint.ID,
		1,
		fixtures[0].verifier,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(second.Checkpoint.PredecessorCheckpointID).NotTo(BeNil())
	g.Expect(*second.Checkpoint.PredecessorCheckpointID).To(Equal(first.Checkpoint.ID))
	g.Expect(second.Checkpoint.SourceAfterCreatedAt).NotTo(BeNil())
	g.Expect(*second.Checkpoint.SourceAfterCreatedAt).To(BeTemporally(
		"==",
		fixtures[0].plan.CreatedAt,
	))
	g.Expect(second.Checkpoint.SourceAfterPlanID).NotTo(BeNil())
	g.Expect(*second.Checkpoint.SourceAfterPlanID).To(Equal(fixtures[0].plan.ID))
	g.Expect(second.Checkpoint.SourceThroughCreatedAt).NotTo(BeNil())
	g.Expect(*second.Checkpoint.SourceThroughCreatedAt).To(BeTemporally(
		"==",
		fixtures[1].plan.CreatedAt,
	))
	g.Expect(second.Checkpoint.SourceThroughPlanID).NotTo(BeNil())
	g.Expect(*second.Checkpoint.SourceThroughPlanID).To(Equal(fixtures[1].plan.ID))
	g.Expect(second.Checkpoint.SourceHighWaterCreatedAt).To(Equal(
		first.Checkpoint.SourceHighWaterCreatedAt,
	))
	g.Expect(second.Checkpoint.SourceHighWaterPlanID).To(Equal(
		first.Checkpoint.SourceHighWaterPlanID,
	))
	g.Expect(second.Checkpoint.HasMore).To(BeTrue())
	g.Expect(second.Items).To(HaveLen(1))
	g.Expect(second.Items[0].OriginalPlanID).To(Equal(fixtures[1].plan.ID))

	_, err = ApplyTargetConfigV1Extraction(
		ctx,
		fixtures[0].organizationID,
		fixtures[0].actorUserAccountID,
		second.Checkpoint.ID,
		second.Checkpoint.DryRunChecksum,
		100,
		fixtures[0].verifier,
	)
	g.Expect(err).NotTo(HaveOccurred())

	last, err := CreateTargetConfigV1ExtractionDryRun(
		ctx,
		fixtures[0].organizationID,
		fixtures[0].actorUserAccountID,
		&second.Checkpoint.ID,
		1,
		fixtures[0].verifier,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(last.Checkpoint.PredecessorCheckpointID).NotTo(BeNil())
	g.Expect(*last.Checkpoint.PredecessorCheckpointID).To(Equal(second.Checkpoint.ID))
	g.Expect(last.Checkpoint.SourceThroughPlanID).NotTo(BeNil())
	g.Expect(*last.Checkpoint.SourceThroughPlanID).To(Equal(fixtures[2].plan.ID))
	g.Expect(last.Checkpoint.SourceHighWaterCreatedAt).To(Equal(
		first.Checkpoint.SourceHighWaterCreatedAt,
	))
	g.Expect(last.Checkpoint.SourceHighWaterPlanID).To(Equal(
		first.Checkpoint.SourceHighWaterPlanID,
	))
	g.Expect(last.Checkpoint.HasMore).To(BeFalse())
	g.Expect(last.Items).To(HaveLen(1))
	g.Expect(last.Items[0].OriginalPlanID).To(Equal(fixtures[2].plan.ID))
}

type targetConfigV1RepositoryFixture struct {
	organizationID     uuid.UUID
	actorUserAccountID uuid.UUID
	placement          types.DeploymentRegistryPlacement
	bundle             types.ReleaseBundle
	plan               types.DeploymentPlan
	verifier           targetConfigV1StaticObjectVerifier
}

type targetConfigV1BlockingObjectVerifier struct {
	delegate targetConfigV1StaticObjectVerifier
	entered  chan<- struct{}
	release  <-chan struct{}
	once     sync.Once
}

func (verifier *targetConfigV1BlockingObjectVerifier) Verify(
	ctx context.Context,
	object types.TargetConfigSnapshotObject,
) (types.VerifiedTargetConfigObject, error) {
	verifier.once.Do(func() {
		verifier.entered <- struct{}{}
	})
	select {
	case <-ctx.Done():
		return types.VerifiedTargetConfigObject{}, ctx.Err()
	case <-verifier.release:
	}
	return verifier.delegate.Verify(ctx, object)
}

type targetConfigV1StaticObjectVerifier struct {
	evidence map[string]types.VerifiedTargetConfigObject
}

func (verifier targetConfigV1StaticObjectVerifier) Verify(
	_ context.Context,
	object types.TargetConfigSnapshotObject,
) (types.VerifiedTargetConfigObject, error) {
	evidence, exists := verifier.evidence[object.Reference]
	if !exists {
		return types.VerifiedTargetConfigObject{}, errors.New("test object does not exist")
	}
	return evidence, nil
}

func createTargetConfigV1RepositoryFixture(
	t *testing.T,
	ctx context.Context,
	suffix string,
	schema string,
) targetConfigV1RepositoryFixture {
	return createTargetConfigV1RepositoryFixtureForOrganization(
		t,
		ctx,
		suffix,
		schema,
		uuid.Nil,
	)
}

func createTargetConfigV1RepositoryFixtureForOrganization(
	t *testing.T,
	ctx context.Context,
	suffix string,
	schema string,
	organizationID uuid.UUID,
) targetConfigV1RepositoryFixture {
	t.Helper()
	g := NewWithT(t)
	var deps deploymentRegistryDependencies
	if organizationID == uuid.Nil {
		deps = createDeploymentRegistryDependencies(t, ctx)
	} else {
		deps = deploymentRegistryDependencies{
			organizationID:         organizationID,
			customerOrganizationID: createDeploymentRegistryCustomer(t, ctx, organizationID),
			environmentID:          createDeploymentRegistryEnvironment(t, ctx, organizationID),
			deploymentTargetID:     createDeploymentRegistryTarget(t, ctx, organizationID),
		}
	}
	actorUserAccountID := createTargetConfigSnapshotTestUser(
		t,
		ctx,
		deps.organizationID,
	)
	placement := createDeploymentRegistryPlacement(t, ctx, deps, suffix, time.Now().UTC())
	application := types.Application{
		Name: "V1 extraction " + suffix,
		Type: types.DeploymentTypeDocker,
	}
	g.Expect(CreateApplication(ctx, &application, deps.organizationID)).To(Succeed())
	var lifecycleID uuid.UUID
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
		INSERT INTO Lifecycle (organization_id, name)
		VALUES (@organizationID, @name)
		RETURNING id`,
		pgx.NamedArgs{
			"organizationID": deps.organizationID,
			"name":           "V1 extraction " + suffix,
		},
	).Scan(&lifecycleID)).To(Succeed())
	channel := types.Channel{
		OrganizationID: deps.organizationID,
		ApplicationID:  application.ID,
		LifecycleID:    lifecycleID,
		Name:           "Stable",
		IsDefault:      true,
	}
	g.Expect(CreateChannel(ctx, &channel)).To(Succeed())

	composeChecksum := "sha256:" + strings.Repeat("a", 64)
	serviceChecksum := "sha256:" + strings.Repeat("b", 64)
	componentDigest := "sha256:" + strings.Repeat("c", 64)
	composeReference := "s3://config-bucket/_immutable/sha256/" +
		strings.Repeat("a", 64) + "/config/docker-compose.yaml"
	serviceReference := "s3://config-bucket/config/service.json"
	componentName := placement.Definitions[0].Key
	contract := &types.ReleaseContract{
		Schema: schema,
		Source: types.ReleaseContractSource{
			Repository:   "https://git.example.invalid/emlo/config.git",
			Branch:       "main",
			SourceCommit: strings.Repeat("1", 40),
			BuiltCommit:  strings.Repeat("1", 40),
		},
		Components: []types.ReleaseContractComponent{{
			Name:     componentName,
			Version:  "1.2.3",
			Image:    "registry.example.invalid/emlo/api@" + componentDigest,
			Platform: string(types.DeploymentTargetPlatformLinuxAMD64),
		}},
		Compatibility: types.ReleaseContractCompatibility{
			AffectedComponents: []string{componentName},
		},
		Config: types.ReleaseContractConfig{
			RepositoryCommit:      strings.Repeat("2", 40),
			ComposePath:           "config/docker-compose.yaml",
			ServiceConfigPath:     "config/service.json",
			ComposeChecksum:       composeChecksum,
			ServiceConfigChecksum: serviceChecksum,
			ImmutableObjects: []types.ReleaseContractConfigObject{
				{URI: composeReference, Checksum: composeChecksum},
				{URI: serviceReference, VersionID: "version-7", Checksum: serviceChecksum},
			},
		},
	}
	bundle := types.ReleaseBundle{
		OrganizationID:  deps.organizationID,
		ApplicationID:   application.ID,
		ChannelID:       channel.ID,
		ReleaseNumber:   "1.2.3-" + suffix,
		ReleaseNotes:    "v1 extraction repository fixture",
		SourceRevision:  strings.Repeat("1", 40),
		ReleaseContract: contract,
		Components: []types.ReleaseBundleComponent{{
			Key:        componentName,
			Name:       componentName,
			Type:       types.ReleaseBundleComponentTypeExternalArtifact,
			Version:    "1.2.3",
			PackageRef: "registry.example.invalid/emlo/api@" + componentDigest,
			Digest:     componentDigest,
			Checksum:   componentDigest,
		}},
	}
	g.Expect(CreateReleaseBundle(ctx, &bundle)).To(Succeed())
	_, err := internalctx.GetDb(ctx).Exec(ctx, `
		UPDATE ReleaseBundle
		SET status = 'PUBLISHED'
		WHERE id = @id
		  AND organization_id = @organizationID`,
		pgx.NamedArgs{"id": bundle.ID, "organizationID": deps.organizationID},
	)
	g.Expect(err).NotTo(HaveOccurred())
	bundle.Status = types.ReleaseBundleStatusPublished

	planID := uuid.New()
	planTargetID := uuid.New()
	plan := types.DeploymentPlan{
		ID:              planID,
		OrganizationID:  deps.organizationID,
		ApplicationID:   application.ID,
		ReleaseBundleID: bundle.ID,
		ChannelID:       channel.ID,
		EnvironmentID:   deps.environmentID,
		ReleaseContract: contract,
		Status:          types.DeploymentPlanStatusReady,
		Targets: []types.DeploymentPlanTarget{{
			ID:                 planTargetID,
			DeploymentPlanID:   planID,
			OrganizationID:     deps.organizationID,
			DeploymentTargetID: deps.deploymentTargetID,
			Name:               "Target " + suffix,
			Type:               types.DeploymentTypeDocker,
			Platform:           types.DeploymentTargetPlatformLinuxAMD64,
		}},
		TargetComponents: []types.DeploymentPlanTargetComponent{{
			ID:                     uuid.New(),
			DeploymentPlanID:       planID,
			DeploymentPlanTargetID: planTargetID,
			OrganizationID:         deps.organizationID,
			DeploymentTargetID:     deps.deploymentTargetID,
			Component:              componentName,
			Version:                "1.2.3",
			Image:                  "registry.example.invalid/emlo/api@" + componentDigest,
			Platform:               types.DeploymentTargetPlatformLinuxAMD64,
			ConfigChecksum:         serviceChecksum,
		}},
		Variables: []types.DeploymentPlanVariable{},
	}
	g.Expect(setDeploymentPlanCanonicalFields(&plan)).To(Succeed())
	g.Expect(RunTx(ctx, func(txCtx context.Context) error {
		if err := insertDeploymentPlan(txCtx, &plan); err != nil {
			return err
		}
		if err := insertDeploymentPlanTargets(txCtx, plan); err != nil {
			return err
		}
		if err := insertDeploymentPlanTargetComponents(txCtx, plan); err != nil {
			return err
		}
		if err := insertDeploymentPlanVariables(txCtx, plan); err != nil {
			return err
		}
		return nil
	})).To(Succeed())
	loadedPlan, err := GetDeploymentPlan(ctx, plan.ID, deps.organizationID)
	g.Expect(err).NotTo(HaveOccurred())
	plan = *loadedPlan

	return targetConfigV1RepositoryFixture{
		organizationID:     deps.organizationID,
		actorUserAccountID: actorUserAccountID,
		placement:          placement,
		bundle:             bundle,
		plan:               plan,
		verifier: targetConfigV1StaticObjectVerifier{
			evidence: map[string]types.VerifiedTargetConfigObject{
				composeReference: {
					Reference: composeReference,
					MediaType: "application/vnd.emlo.compose+yaml",
					SizeBytes: 23,
					Checksum:  composeChecksum,
				},
				serviceReference: {
					Reference: serviceReference,
					VersionID: "version-7",
					MediaType: "application/vnd.emlo.service+json",
					SizeBytes: 31,
					Checksum:  serviceChecksum,
				},
			},
		},
	}
}
