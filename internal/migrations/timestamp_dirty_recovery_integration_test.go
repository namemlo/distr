package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	internalctx "github.com/distr-sh/distr/internal/context"
	internaldb "github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/externalexecutiontimestamp"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

func timestampRecoveryTestDocumentChecksum(t *testing.T, value any) string {
	t.Helper()
	checksum, err := computeTimestampDirtyRecoveryDocumentChecksum(value)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return checksum
}

func timestampDirtyRecoveryPlanOptions(
	expectedDirtyVersion uint,
) TimestampDirtyRecoveryPlanOptions {
	return TimestampDirtyRecoveryPlanOptions{
		ExpectedDirtyVersion:  expectedDirtyVersion,
		OperatorIdentity:      "release.operator@distr.example",
		Reason:                "Resume interrupted timestamp expansion",
		WriterFenceIdentifier: "timestamp-expand:recovery-01",
		LockTimeout:           5 * time.Second,
	}
}

func timestampDirtyRecoveryApplyOptions(
	t *testing.T,
	plan types.TimestampDirtyRecoveryPlan,
	manifest *types.ExternalExecutionTimestampManifest,
) TimestampDirtyRecoveryApplyOptions {
	t.Helper()
	options := TimestampDirtyRecoveryApplyOptions{
		PlanDocumentChecksum:  timestampRecoveryTestDocumentChecksum(t, plan),
		WriterFenceIdentifier: "timestamp-expand:recovery-01",
		LockTimeout:           5 * time.Second,
	}
	if manifest != nil {
		options.Manifest = manifest
		options.ManifestDocumentChecksum = timestampRecoveryTestDocumentChecksum(t, *manifest)
	}
	return options
}

func setTimestampRecoverySchemaStatus(
	t *testing.T,
	database *runnerTestDatabase,
	version uint,
) {
	t.Helper()
	_, err := database.pool.Exec(
		context.Background(),
		`UPDATE schema_migrations SET version=$1, dirty=TRUE`,
		version,
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
}

func approvedTimestampRecoveryManifest(
	t *testing.T,
	database *runnerTestDatabase,
) types.ExternalExecutionTimestampManifest {
	t.Helper()
	g := NewWithT(t)
	ctx := internalctx.WithDb(context.Background(), database.pool)
	draft, err := internaldb.InspectExternalExecutionTimestamps(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	approved, err := externalexecutiontimestamp.SealManifest(
		*draft,
		types.ExternalExecutionTimestampSealOptions{
			AuthorIdentity:          "timestamp-author@example.test",
			ReviewerIdentity:        "timestamp-reviewer@example.test",
			EvidenceBundleReference: "evidence:recovery-fixture",
			EvidenceBundleChecksum:  "sha256:" + strings.Repeat("b", 64),
			TargetReleaseCommit:     strings.Repeat("c", 40),
			TargetImageDigest:       "sha256:" + strings.Repeat("d", 64),
		},
		time.Now().UTC().Add(-time.Minute),
	)
	g.Expect(err).NotTo(HaveOccurred())
	return approved
}

func applyTimestampRecoveryManifest(
	t *testing.T,
	database *runnerTestDatabase,
	manifest types.ExternalExecutionTimestampManifest,
) {
	t.Helper()
	_, err := internaldb.ApplyExternalExecutionTimestampManifest(
		internalctx.WithDb(context.Background(), database.pool),
		types.ExternalExecutionTimestampApplyRequest{
			Manifest:                     manifest,
			Apply:                        true,
			WriterFenceIdentifier:        "timestamp-expand:recovery-fixture",
			BackupReference:              "backup:recovery-fixture",
			BackupChecksum:               "sha256:" + strings.Repeat("e", 64),
			RestoreVerificationReference: "restore:recovery-fixture",
			RestoreVerificationChecksum:  "sha256:" + strings.Repeat("f", 64),
		},
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
}

func supersedingTimestampRecoveryManifest(
	t *testing.T,
	previous types.ExternalExecutionTimestampManifest,
) types.ExternalExecutionTimestampManifest {
	t.Helper()
	parentID := previous.ID
	draft := previous
	draft.ID = uuid.New()
	draft.SupersedesManifestID = &parentID
	draft.EvidenceBundleReference = ""
	draft.EvidenceBundleChecksum = ""
	draft.AuthorIdentity = ""
	draft.ReviewerIdentity = ""
	draft.ApprovedAt = ""
	draft.TargetReleaseCommit = ""
	draft.TargetImageDigest = ""
	draft.State = types.ExternalExecutionTimestampManifestStateDraft
	sealed, err := externalexecutiontimestamp.SealManifest(
		draft,
		types.ExternalExecutionTimestampSealOptions{
			AuthorIdentity:          "timestamp-author-v2@example.test",
			ReviewerIdentity:        "timestamp-reviewer-v2@example.test",
			EvidenceBundleReference: "evidence:recovery-fixture-v2",
			EvidenceBundleChecksum:  "sha256:" + strings.Repeat("4", 64),
			TargetReleaseCommit:     strings.Repeat("5", 40),
			TargetImageDigest:       "sha256:" + strings.Repeat("6", 64),
		},
		time.Now().UTC().Add(-time.Millisecond),
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return sealed
}

func insertTimestampRecoveryHistoricalFixture(
	t *testing.T,
	database *runnerTestDatabase,
) uuid.UUID {
	t.Helper()
	g := NewWithT(t)
	ctx := context.Background()
	tx, err := database.pool.Begin(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	defer func() { _ = tx.Rollback(ctx) }()

	// The recovery catalog fingerprints the source-table FK topology. Suppress
	// FK triggers only for this transaction instead of dropping catalog objects.
	_, err = tx.Exec(ctx, `SET LOCAL session_replication_role = 'replica'`)
	g.Expect(err).NotTo(HaveOccurred())
	executionID := uuid.New()
	eventID := uuid.New()
	organizationID := uuid.New()
	_, err = tx.Exec(ctx, `
INSERT INTO ExternalExecution (
  id, created_at, updated_at, started_at, completed_at, callback_deadline_at,
  organization_id, step_run_id, task_id, deployment_plan_id,
  deployment_plan_target_id, deployment_target_id, application_id,
  release_bundle_id, component, plan_checksum, idempotency_key,
  expected_state_version, expected_version, expected_image, expected_platform,
  expected_config_reference, expected_config_checksum
) VALUES (
  $1::uuid,
  TIMESTAMP '2026-07-15 10:00:00.000001',
  TIMESTAMP '2026-07-15 10:01:00.000002',
  TIMESTAMP '2026-07-15 10:02:00.000003',
  TIMESTAMP '2026-07-15 10:03:00.000004',
  TIMESTAMP '2026-07-15 10:04:00.000005',
  $2, $3, $4, $5, $6, $7, $8, $9,
  'api-image', 'sha256:' || repeat('a', 64),
  'fixture-' || $1::uuid::text,
  0, '1.0.0', 'repo/image@sha256:' || repeat('b', 64), 'linux/amd64',
  'config:fixture', 'sha256:' || repeat('c', 64)
)`,
		executionID, organizationID, uuid.New(), uuid.New(), uuid.New(),
		uuid.New(), uuid.New(), uuid.New(), uuid.New(),
	)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = tx.Exec(ctx, `
INSERT INTO ExternalExecutionEvent (
  id, created_at, organization_id, external_execution_id,
  sequence, status, payload_hash
) VALUES (
  $1, TIMESTAMP '2026-07-15 10:05:00.000006',
  $2, $3, 1, 'SUCCEEDED', 'sha256:' || repeat('d', 64)
)`, eventID, organizationID, executionID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(tx.Commit(ctx)).To(Succeed())
	return executionID
}

func TestPlanTimestampDirtyRecoveryEmptyEvidenceModes(t *testing.T) {
	for _, test := range []struct {
		name                 string
		catalogVersion       uint
		expectedDirtyVersion uint
		expectedForceVersion uint
		expectedShape        types.TimestampRecoveryCatalogShape
	}{
		{
			name:                 "predecessor empty without manifest",
			catalogVersion:       137,
			expectedDirtyVersion: 138,
			expectedForceVersion: 137,
			expectedShape:        types.TimestampRecoveryCatalogShapePredecessor137,
		},
		{
			name:                 "expand zero history readiness",
			catalogVersion:       138,
			expectedDirtyVersion: 138,
			expectedForceVersion: 138,
			expectedShape:        types.TimestampRecoveryCatalogShapeExpand138,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			database := newRunnerTestDatabase(t)
			database.migrateTo(t, test.catalogVersion)
			setTimestampRecoverySchemaStatus(
				t,
				database,
				test.expectedDirtyVersion,
			)

			plan, err := database.runner.PlanTimestampDirtyRecovery(
				context.Background(),
				timestampDirtyRecoveryPlanOptions(test.expectedDirtyVersion),
			)

			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(plan.Validate()).To(Succeed())
			g.Expect(plan.ExpectedDirtyVersion).To(Equal(test.expectedDirtyVersion))
			g.Expect(plan.ForceVersion).To(Equal(test.expectedForceVersion))
			g.Expect(plan.CatalogShape).To(Equal(test.expectedShape))
			g.Expect(plan.Manifest).To(BeNil())
			g.Expect(plan.CatalogChecksum).To(HavePrefix("sha256:"))
			g.Expect(plan.CatalogChecksum).To(HaveLen(len("sha256:") + 64))
			g.Expect(plan.CreatedAt.Location()).To(Equal(time.UTC))
			g.Expect(plan.Reason).NotTo(ContainSubstring("postgres://"))
			g.Expect(plan.WriterFenceIdentifier).NotTo(ContainSubstring(`\`))
		})
	}
}

func TestPlanTimestampDirtyRecoveryManifestEvidenceModes(t *testing.T) {
	for _, test := range []struct {
		name                 string
		prepare              func(*testing.T, *runnerTestDatabase) types.ExternalExecutionTimestampManifest
		expectedDirtyVersion uint
		expectedForceVersion uint
		expectedShape        types.TimestampRecoveryCatalogShape
	}{
		{
			name: "predecessor nonempty approved root",
			prepare: func(t *testing.T, database *runnerTestDatabase,
			) types.ExternalExecutionTimestampManifest {
				database.migrateTo(t, 137)
				insertTimestampRecoveryHistoricalFixture(t, database)
				return approvedTimestampRecoveryManifest(t, database)
			},
			expectedDirtyVersion: 138,
			expectedForceVersion: 137,
			expectedShape:        types.TimestampRecoveryCatalogShapePredecessor137,
		},
		{
			name: "expand manifest required before apply",
			prepare: func(t *testing.T, database *runnerTestDatabase,
			) types.ExternalExecutionTimestampManifest {
				database.migrateTo(t, 137)
				insertTimestampRecoveryHistoricalFixture(t, database)
				manifest := approvedTimestampRecoveryManifest(t, database)
				database.migrateTo(t, 138)
				return manifest
			},
			expectedDirtyVersion: 138,
			expectedForceVersion: 138,
			expectedShape:        types.TimestampRecoveryCatalogShapeExpand138,
		},
		{
			name: "expand verified tip matches approved document",
			prepare: func(t *testing.T, database *runnerTestDatabase,
			) types.ExternalExecutionTimestampManifest {
				database.migrateTo(t, 137)
				insertTimestampRecoveryHistoricalFixture(t, database)
				manifest := approvedTimestampRecoveryManifest(t, database)
				database.migrateTo(t, 138)
				applyTimestampRecoveryManifest(t, database, manifest)
				return manifest
			},
			expectedDirtyVersion: 137,
			expectedForceVersion: 138,
			expectedShape:        types.TimestampRecoveryCatalogShapeExpand138,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			database := newRunnerTestDatabase(t)
			manifest := test.prepare(t, database)
			setTimestampRecoverySchemaStatus(
				t,
				database,
				test.expectedDirtyVersion,
			)
			options := timestampDirtyRecoveryPlanOptions(
				test.expectedDirtyVersion,
			)
			options.Manifest = &manifest
			options.ManifestDocumentChecksum = timestampRecoveryTestDocumentChecksum(t, manifest)

			plan, err := database.runner.PlanTimestampDirtyRecovery(
				context.Background(),
				options,
			)

			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(plan.Validate()).To(Succeed())
			g.Expect(plan.ForceVersion).To(Equal(test.expectedForceVersion))
			g.Expect(plan.CatalogShape).To(Equal(test.expectedShape))
			g.Expect(plan.Manifest).NotTo(BeNil())
			g.Expect(plan.Manifest.ID).To(Equal(manifest.ID))
			g.Expect(plan.Manifest.DocumentChecksum).To(Equal(
				timestampRecoveryTestDocumentChecksum(t, manifest),
			))
			g.Expect(plan.Manifest.DecisionContentChecksum).To(Equal(
				manifest.DecisionContentChecksum,
			))
			g.Expect(plan.Manifest.RawSetChecksum).To(Equal(
				manifest.RawCellChecksum,
			))
			g.Expect(plan.Manifest.DatabaseIdentityChecksum).To(Equal(
				manifest.DatabaseIdentityChecksum,
			))
		})
	}
}

func TestPlanTimestampDirtyRecoveryAcceptsVerifiedSupersedingTip(t *testing.T) {
	g := NewWithT(t)
	database := newRunnerTestDatabase(t)
	database.migrateTo(t, 137)
	insertTimestampRecoveryHistoricalFixture(t, database)
	root := approvedTimestampRecoveryManifest(t, database)
	database.migrateTo(t, 138)
	applyTimestampRecoveryManifest(t, database, root)
	child := supersedingTimestampRecoveryManifest(t, root)
	applyTimestampRecoveryManifest(t, database, child)
	setTimestampRecoverySchemaStatus(t, database, 137)
	options := timestampDirtyRecoveryPlanOptions(137)
	options.Manifest = &child
	options.ManifestDocumentChecksum = timestampRecoveryTestDocumentChecksum(t, child)

	plan, err := database.runner.PlanTimestampDirtyRecovery(
		context.Background(),
		options,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(plan.Validate()).To(Succeed())
	g.Expect(plan.Manifest).NotTo(BeNil())
	g.Expect(plan.Manifest.ID).To(Equal(child.ID))
	g.Expect(plan.Manifest.DecisionContentChecksum).To(Equal(
		child.DecisionContentChecksum,
	))
	g.Expect(child.SupersedesManifestID).NotTo(BeNil())
	g.Expect(*child.SupersedesManifestID).To(Equal(root.ID))

	wrongTipResult, err := database.runner.ApplyTimestampDirtyRecovery(
		context.Background(),
		plan,
		timestampDirtyRecoveryApplyOptions(t, plan, &root),
	)
	g.Expect(err).To(MatchError(ContainSubstring(
		"manifest does not match the plan",
	)))
	g.Expect(wrongTipResult).To(Equal(types.TimestampDirtyRecoveryResult{}))

	ancestorOptions := timestampDirtyRecoveryPlanOptions(137)
	ancestorOptions.Manifest = &root
	ancestorOptions.ManifestDocumentChecksum = timestampRecoveryTestDocumentChecksum(t, root)
	ancestorPlan, err := database.runner.PlanTimestampDirtyRecovery(
		context.Background(),
		ancestorOptions,
	)
	g.Expect(err).To(MatchError(ContainSubstring(
		"tip does not match the supplied approved manifest",
	)))
	g.Expect(ancestorPlan).To(Equal(types.TimestampDirtyRecoveryPlan{}))

	tampered := child
	approvedAt, err := externalexecutiontimestamp.ParseInstant(
		tampered.ApprovedAt,
	)
	g.Expect(err).NotTo(HaveOccurred())
	tampered.ApprovedAt = externalexecutiontimestamp.FormatInstant(
		approvedAt.Add(time.Second),
	)
	tamperedOptions := timestampDirtyRecoveryPlanOptions(137)
	tamperedOptions.Manifest = &tampered
	tamperedOptions.ManifestDocumentChecksum = timestampRecoveryTestDocumentChecksum(t, tampered)
	tamperedPlan, err := database.runner.PlanTimestampDirtyRecovery(
		context.Background(),
		tamperedOptions,
	)
	g.Expect(err).To(MatchError(ContainSubstring(
		"does not match the supplied approved manifest",
	)))
	g.Expect(tamperedPlan).To(Equal(types.TimestampDirtyRecoveryPlan{}))
}

func TestApplyTimestampDirtyRecoveryForceMatrixAndCleanRetry(t *testing.T) {
	for _, test := range []struct {
		name                 string
		catalogVersion       uint
		expectedDirtyVersion uint
		expectedForceVersion uint
	}{
		{
			name:                 "dirty 138 predecessor forces 137",
			catalogVersion:       137,
			expectedDirtyVersion: 138,
			expectedForceVersion: 137,
		},
		{
			name:                 "dirty 138 expand forces 138",
			catalogVersion:       138,
			expectedDirtyVersion: 138,
			expectedForceVersion: 138,
		},
		{
			name:                 "dirty 137 expand forces 138",
			catalogVersion:       138,
			expectedDirtyVersion: 137,
			expectedForceVersion: 138,
		},
		{
			name:                 "dirty 137 predecessor forces 137",
			catalogVersion:       137,
			expectedDirtyVersion: 137,
			expectedForceVersion: 137,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			database := newRunnerTestDatabase(t)
			database.migrateTo(t, test.catalogVersion)
			setTimestampRecoverySchemaStatus(
				t,
				database,
				test.expectedDirtyVersion,
			)
			plan, err := database.runner.PlanTimestampDirtyRecovery(
				context.Background(),
				timestampDirtyRecoveryPlanOptions(test.expectedDirtyVersion),
			)
			g.Expect(err).NotTo(HaveOccurred())
			originalFactory := database.runner.recoveryForceEngineFactory
			var engineFactoryCalls uint64
			database.runner.recoveryForceEngineFactory = func(
				ctx context.Context,
				sqlDB *sql.DB,
				logger *zap.Logger,
				schema string,
				lockTimeout time.Duration,
			) (timestampDirtyRecoveryForceEngine, error) {
				engineFactoryCalls++
				return originalFactory(
					ctx,
					sqlDB,
					logger,
					schema,
					lockTimeout,
				)
			}

			result, err := database.runner.ApplyTimestampDirtyRecovery(
				context.Background(),
				plan,
				timestampDirtyRecoveryApplyOptions(t, plan, nil),
			)

			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(result.Validate()).To(Succeed())
			g.Expect(result.Action).To(Equal(
				types.TimestampDirtyRecoveryActionForced,
			))
			g.Expect(result.ForcedVersion).To(Equal(
				test.expectedForceVersion,
			))
			g.Expect(result.PostStatus).To(Equal(
				types.TimestampDirtyRecoverySchemaStatus{
					Version: test.expectedForceVersion,
					Dirty:   false,
				},
			))
			g.Expect(engineFactoryCalls).To(Equal(uint64(1)))

			retry, err := database.runner.ApplyTimestampDirtyRecovery(
				context.Background(),
				plan,
				timestampDirtyRecoveryApplyOptions(t, plan, nil),
			)

			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(retry.Validate()).To(Succeed())
			g.Expect(retry.Action).To(Equal(
				types.TimestampDirtyRecoveryActionObservedAlreadyClean,
			))
			g.Expect(engineFactoryCalls).To(Equal(uint64(1)))
		})
	}
}

func TestApplyTimestampDirtyRecoveryManifestBoundModes(t *testing.T) {
	for _, test := range []struct {
		name                 string
		prepare              func(*testing.T, *runnerTestDatabase) types.ExternalExecutionTimestampManifest
		expectedDirtyVersion uint
		expectedForceVersion uint
	}{
		{
			name: "predecessor nonempty",
			prepare: func(t *testing.T, database *runnerTestDatabase,
			) types.ExternalExecutionTimestampManifest {
				database.migrateTo(t, 137)
				insertTimestampRecoveryHistoricalFixture(t, database)
				return approvedTimestampRecoveryManifest(t, database)
			},
			expectedDirtyVersion: 138,
			expectedForceVersion: 137,
		},
		{
			name: "expand manifest required preapply",
			prepare: func(t *testing.T, database *runnerTestDatabase,
			) types.ExternalExecutionTimestampManifest {
				database.migrateTo(t, 137)
				insertTimestampRecoveryHistoricalFixture(t, database)
				manifest := approvedTimestampRecoveryManifest(t, database)
				database.migrateTo(t, 138)
				return manifest
			},
			expectedDirtyVersion: 138,
			expectedForceVersion: 138,
		},
		{
			name: "expand verified superseding tip",
			prepare: func(t *testing.T, database *runnerTestDatabase,
			) types.ExternalExecutionTimestampManifest {
				database.migrateTo(t, 137)
				insertTimestampRecoveryHistoricalFixture(t, database)
				root := approvedTimestampRecoveryManifest(t, database)
				database.migrateTo(t, 138)
				applyTimestampRecoveryManifest(t, database, root)
				child := supersedingTimestampRecoveryManifest(t, root)
				applyTimestampRecoveryManifest(t, database, child)
				return child
			},
			expectedDirtyVersion: 137,
			expectedForceVersion: 138,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			database := newRunnerTestDatabase(t)
			manifest := test.prepare(t, database)
			setTimestampRecoverySchemaStatus(
				t,
				database,
				test.expectedDirtyVersion,
			)
			planOptions := timestampDirtyRecoveryPlanOptions(
				test.expectedDirtyVersion,
			)
			planOptions.Manifest = &manifest
			planOptions.ManifestDocumentChecksum = timestampRecoveryTestDocumentChecksum(t, manifest)
			plan, err := database.runner.PlanTimestampDirtyRecovery(
				context.Background(),
				planOptions,
			)
			g.Expect(err).NotTo(HaveOccurred())

			result, err := database.runner.ApplyTimestampDirtyRecovery(
				context.Background(),
				plan,
				timestampDirtyRecoveryApplyOptions(t, plan, &manifest),
			)

			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(result.Validate()).To(Succeed())
			g.Expect(result.Action).To(Equal(
				types.TimestampDirtyRecoveryActionForced,
			))
			g.Expect(result.ForcedVersion).To(Equal(
				test.expectedForceVersion,
			))
		})
	}
}

func TestApplyTimestampDirtyRecoveryRejectsPlanAndFenceTampering(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(
			*types.TimestampDirtyRecoveryPlan,
			*TimestampDirtyRecoveryApplyOptions,
		)
		want string
	}{
		{
			name: "plan field with original checksum",
			mutate: func(
				plan *types.TimestampDirtyRecoveryPlan,
				_ *TimestampDirtyRecoveryApplyOptions,
			) {
				plan.Reason = "Tampered recovery reason"
			},
			want: "plan document checksum does not match",
		},
		{
			name: "plan fence with original checksum",
			mutate: func(
				plan *types.TimestampDirtyRecoveryPlan,
				_ *TimestampDirtyRecoveryApplyOptions,
			) {
				plan.WriterFenceIdentifier = "timestamp-expand:tampered-plan"
			},
			want: "plan document checksum does not match",
		},
		{
			name: "apply fence differs from signed plan",
			mutate: func(
				_ *types.TimestampDirtyRecoveryPlan,
				options *TimestampDirtyRecoveryApplyOptions,
			) {
				options.WriterFenceIdentifier = "timestamp-expand:tampered-apply"
			},
			want: "writer fence does not match",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			database := newRunnerTestDatabase(t)
			database.migrateTo(t, 137)
			setTimestampRecoverySchemaStatus(t, database, 137)
			plan, err := database.runner.PlanTimestampDirtyRecovery(
				context.Background(),
				timestampDirtyRecoveryPlanOptions(137),
			)
			g.Expect(err).NotTo(HaveOccurred())
			options := timestampDirtyRecoveryApplyOptions(t, plan, nil)
			test.mutate(&plan, &options)

			result, err := database.runner.ApplyTimestampDirtyRecovery(
				context.Background(),
				plan,
				options,
			)

			g.Expect(err).To(MatchError(ContainSubstring(test.want)))
			g.Expect(result).To(Equal(types.TimestampDirtyRecoveryResult{}))
			var dirty bool
			g.Expect(database.pool.QueryRow(
				context.Background(),
				`SELECT dirty FROM schema_migrations`,
			).Scan(&dirty)).To(Succeed())
			g.Expect(dirty).To(BeTrue())
		})
	}
}

func installUnexpectedTimestampRecoveryForceFactory(
	t *testing.T,
	database *runnerTestDatabase,
) *uint64 {
	t.Helper()
	calls := new(uint64)
	database.runner.recoveryForceEngineFactory = func(
		context.Context,
		*sql.DB,
		*zap.Logger,
		string,
		time.Duration,
	) (timestampDirtyRecoveryForceEngine, error) {
		(*calls)++
		return nil, errors.New("unexpected timestamp recovery Force construction")
	}
	return calls
}

func TestApplyTimestampDirtyRecoveryRejectsDriftBeforeForce(t *testing.T) {
	t.Run("catalog drift", func(t *testing.T) {
		g := NewWithT(t)
		database := newRunnerTestDatabase(t)
		database.migrateTo(t, 137)
		setTimestampRecoverySchemaStatus(t, database, 137)
		plan, err := database.runner.PlanTimestampDirtyRecovery(
			context.Background(),
			timestampDirtyRecoveryPlanOptions(137),
		)
		g.Expect(err).NotTo(HaveOccurred())
		_, err = database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecution
ADD COLUMN recovery_drift_instant TIMESTAMPTZ`)
		g.Expect(err).NotTo(HaveOccurred())
		forceCalls := installUnexpectedTimestampRecoveryForceFactory(t, database)

		result, err := database.runner.ApplyTimestampDirtyRecovery(
			context.Background(),
			plan,
			timestampDirtyRecoveryApplyOptions(t, plan, nil),
		)

		g.Expect(err).To(MatchError(ContainSubstring(
			"partial, mixed, extra, or mutated",
		)))
		g.Expect(result).To(Equal(types.TimestampDirtyRecoveryResult{}))
		g.Expect(*forceCalls).To(BeZero())
	})

	t.Run("evidence drift", func(t *testing.T) {
		g := NewWithT(t)
		database := newRunnerTestDatabase(t)
		database.migrateTo(t, 137)
		setTimestampRecoverySchemaStatus(t, database, 137)
		plan, err := database.runner.PlanTimestampDirtyRecovery(
			context.Background(),
			timestampDirtyRecoveryPlanOptions(137),
		)
		g.Expect(err).NotTo(HaveOccurred())
		insertTimestampRecoveryHistoricalFixture(t, database)
		forceCalls := installUnexpectedTimestampRecoveryForceFactory(t, database)

		result, err := database.runner.ApplyTimestampDirtyRecovery(
			context.Background(),
			plan,
			timestampDirtyRecoveryApplyOptions(t, plan, nil),
		)

		g.Expect(err).To(MatchError(ContainSubstring(
			"requires an APPROVED root manifest",
		)))
		g.Expect(result).To(Equal(types.TimestampDirtyRecoveryResult{}))
		g.Expect(*forceCalls).To(BeZero())
	})

	t.Run("manifest checksum and approved at drift", func(t *testing.T) {
		g := NewWithT(t)
		database := newRunnerTestDatabase(t)
		database.migrateTo(t, 137)
		insertTimestampRecoveryHistoricalFixture(t, database)
		manifest := approvedTimestampRecoveryManifest(t, database)
		setTimestampRecoverySchemaStatus(t, database, 138)
		planOptions := timestampDirtyRecoveryPlanOptions(138)
		planOptions.Manifest = &manifest
		planOptions.ManifestDocumentChecksum = timestampRecoveryTestDocumentChecksum(t, manifest)
		plan, err := database.runner.PlanTimestampDirtyRecovery(
			context.Background(),
			planOptions,
		)
		g.Expect(err).NotTo(HaveOccurred())
		forceCalls := installUnexpectedTimestampRecoveryForceFactory(t, database)
		badChecksumOptions := timestampDirtyRecoveryApplyOptions(
			t,
			plan,
			&manifest,
		)
		badChecksumOptions.ManifestDocumentChecksum = "sha256:" + strings.Repeat("0", 64)
		result, err := database.runner.ApplyTimestampDirtyRecovery(
			context.Background(),
			plan,
			badChecksumOptions,
		)
		g.Expect(err).To(MatchError(ContainSubstring(
			"manifest document checksum does not match",
		)))
		g.Expect(result).To(Equal(types.TimestampDirtyRecoveryResult{}))

		tampered := manifest
		approvedAt, err := externalexecutiontimestamp.ParseInstant(
			tampered.ApprovedAt,
		)
		g.Expect(err).NotTo(HaveOccurred())
		tampered.ApprovedAt = externalexecutiontimestamp.FormatInstant(
			approvedAt.Add(time.Second),
		)
		tamperedOptions := timestampDirtyRecoveryApplyOptions(
			t,
			plan,
			&tampered,
		)
		result, err = database.runner.ApplyTimestampDirtyRecovery(
			context.Background(),
			plan,
			tamperedOptions,
		)
		g.Expect(err).To(MatchError(ContainSubstring(
			"manifest does not match the plan",
		)))
		g.Expect(result).To(Equal(types.TimestampDirtyRecoveryResult{}))
		g.Expect(*forceCalls).To(BeZero())
	})
}

func TestPlanTimestampDirtyRecoveryRejectsMalformedMarkerCatalog(t *testing.T) {
	for _, test := range []struct {
		name      string
		statement string
	}{
		{
			name:      "unlogged marker",
			statement: `ALTER TABLE schema_migrations SET UNLOGGED`,
		},
		{
			name: "column default",
			statement: `
ALTER TABLE schema_migrations
ALTER COLUMN dirty SET DEFAULT FALSE`,
		},
		{
			name: "extra constraint",
			statement: `
ALTER TABLE schema_migrations
ADD CONSTRAINT schema_migrations_recovery_probe CHECK (version >= 0)`,
		},
		{
			name:      "row security",
			statement: `ALTER TABLE schema_migrations ENABLE ROW LEVEL SECURITY`,
		},
		{
			name: "truncate trigger",
			statement: `
CREATE FUNCTION schema_migrations_recovery_probe()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
  RETURN NULL;
END;
$$;
CREATE TRIGGER schema_migrations_recovery_probe
BEFORE TRUNCATE ON schema_migrations
FOR EACH STATEMENT
EXECUTE FUNCTION schema_migrations_recovery_probe()`,
		},
		{
			name: "insert trigger",
			statement: `
CREATE FUNCTION schema_migrations_recovery_probe()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
  RETURN NEW;
END;
$$;
CREATE TRIGGER schema_migrations_recovery_probe
BEFORE INSERT ON schema_migrations
FOR EACH ROW
EXECUTE FUNCTION schema_migrations_recovery_probe()`,
		},
		{
			name: "inheritance child",
			statement: `
CREATE TABLE schema_migrations_recovery_child ()
INHERITS (schema_migrations)`,
		},
		{
			name: "inbound foreign key",
			statement: `
CREATE TABLE schema_migrations_recovery_reference (
  version BIGINT NOT NULL
    REFERENCES schema_migrations(version)
)`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			database := newRunnerTestDatabase(t)
			database.migrateTo(t, 137)
			setTimestampRecoverySchemaStatus(t, database, 137)
			_, err := database.pool.Exec(
				context.Background(),
				test.statement,
			)
			g.Expect(err).NotTo(HaveOccurred())

			plan, err := database.runner.PlanTimestampDirtyRecovery(
				context.Background(),
				timestampDirtyRecoveryPlanOptions(137),
			)

			g.Expect(err).To(MatchError(ContainSubstring(
				"schema_migrations catalog is malformed",
			)))
			g.Expect(plan).To(Equal(types.TimestampDirtyRecoveryPlan{}))
		})
	}
}

func TestPlanTimestampDirtyRecoveryRejectsPG18NoInheritMarkerNotNull(
	t *testing.T,
) {
	g := NewWithT(t)
	database := newRunnerTestDatabase(t)
	var serverVersion int
	g.Expect(database.pool.QueryRow(
		context.Background(),
		`SELECT current_setting('server_version_num')::integer`,
	).Scan(&serverVersion)).To(Succeed())
	if serverVersion < 180000 {
		t.Skip("requires the hosted PostgreSQL 18 compatibility gate")
	}
	database.migrateTo(t, 137)
	setTimestampRecoverySchemaStatus(t, database, 137)
	var constraintName string
	g.Expect(database.pool.QueryRow(context.Background(), `
SELECT constraint_row.conname
FROM pg_catalog.pg_constraint constraint_row
JOIN pg_catalog.pg_attribute attribute_row
  ON attribute_row.attrelid = constraint_row.conrelid
 AND constraint_row.conkey = ARRAY[attribute_row.attnum]::smallint[]
WHERE constraint_row.conrelid = 'schema_migrations'::regclass
  AND constraint_row.contype = 'n'
  AND attribute_row.attname = 'dirty'`).Scan(&constraintName)).To(Succeed())
	_, err := database.pool.Exec(
		context.Background(),
		"ALTER TABLE schema_migrations ALTER CONSTRAINT "+
			pgx.Identifier{constraintName}.Sanitize()+" NO INHERIT",
	)
	g.Expect(err).NotTo(HaveOccurred())
	var noInherit bool
	g.Expect(database.pool.QueryRow(
		context.Background(),
		`SELECT connoinherit
FROM pg_catalog.pg_constraint
WHERE conrelid = 'schema_migrations'::regclass
  AND conname = $1`,
		constraintName,
	).Scan(&noInherit)).To(Succeed())
	g.Expect(noInherit).To(BeTrue())

	plan, err := database.runner.PlanTimestampDirtyRecovery(
		context.Background(),
		timestampDirtyRecoveryPlanOptions(137),
	)

	g.Expect(err).To(MatchError(ContainSubstring(
		"schema_migrations catalog is malformed",
	)))
	g.Expect(plan).To(Equal(types.TimestampDirtyRecoveryPlan{}))
}

func TestApplyTimestampDirtyRecoveryForceLockWaitIsBounded(t *testing.T) {
	g := NewWithT(t)
	database := newRunnerTestDatabase(t)
	database.migrateTo(t, 137)
	setTimestampRecoverySchemaStatus(t, database, 137)
	plan, err := database.runner.PlanTimestampDirtyRecovery(
		context.Background(),
		timestampDirtyRecoveryPlanOptions(137),
	)
	g.Expect(err).NotTo(HaveOccurred())
	blocker, err := database.pool.Begin(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	defer func() { _ = blocker.Rollback(context.Background()) }()
	_, err = blocker.Exec(
		context.Background(),
		`LOCK TABLE schema_migrations IN ACCESS SHARE MODE`,
	)
	g.Expect(err).NotTo(HaveOccurred())
	options := timestampDirtyRecoveryApplyOptions(t, plan, nil)
	options.LockTimeout = 250 * time.Millisecond
	started := time.Now()

	result, err := database.runner.ApplyTimestampDirtyRecovery(
		context.Background(),
		plan,
		options,
	)

	g.Expect(err).To(MatchError(ContainSubstring("outcome is uncertain")))
	g.Expect(result).To(Equal(types.TimestampDirtyRecoveryResult{}))
	g.Expect(time.Since(started)).To(BeNumerically("<", 5*time.Second))
	var version uint
	var dirty bool
	g.Expect(database.pool.QueryRow(
		context.Background(),
		`SELECT version, dirty FROM schema_migrations`,
	).Scan(&version, &dirty)).To(Succeed())
	g.Expect(version).To(Equal(uint(137)))
	g.Expect(dirty).To(BeTrue())
	g.Expect(blocker.Rollback(context.Background())).To(Succeed())

	retry, err := database.runner.ApplyTimestampDirtyRecovery(
		context.Background(),
		plan,
		timestampDirtyRecoveryApplyOptions(t, plan, nil),
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(retry.Action).To(Equal(
		types.TimestampDirtyRecoveryActionForced,
	))
}

func TestApplyTimestampDirtyRecoveryPreStatusLockWaitIsBounded(t *testing.T) {
	g := NewWithT(t)
	database := newRunnerTestDatabase(t)
	database.migrateTo(t, 137)
	setTimestampRecoverySchemaStatus(t, database, 137)
	plan, err := database.runner.PlanTimestampDirtyRecovery(
		context.Background(),
		timestampDirtyRecoveryPlanOptions(137),
	)
	g.Expect(err).NotTo(HaveOccurred())
	blocker, err := database.pool.Begin(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	defer func() { _ = blocker.Rollback(context.Background()) }()
	_, err = blocker.Exec(
		context.Background(),
		`LOCK TABLE schema_migrations IN ACCESS EXCLUSIVE MODE`,
	)
	g.Expect(err).NotTo(HaveOccurred())
	forceCalls := installUnexpectedTimestampRecoveryForceFactory(t, database)
	options := timestampDirtyRecoveryApplyOptions(t, plan, nil)
	options.LockTimeout = 250 * time.Millisecond
	started := time.Now()

	result, err := database.runner.ApplyTimestampDirtyRecovery(
		context.Background(),
		plan,
		options,
	)

	g.Expect(err).To(HaveOccurred())
	g.Expect(result).To(Equal(types.TimestampDirtyRecoveryResult{}))
	g.Expect(time.Since(started)).To(BeNumerically("<", 5*time.Second))
	g.Expect(*forceCalls).To(BeZero())
	g.Expect(blocker.Rollback(context.Background())).To(Succeed())
	var version uint
	var dirty bool
	g.Expect(database.pool.QueryRow(
		context.Background(),
		`SELECT version, dirty FROM schema_migrations`,
	).Scan(&version, &dirty)).To(Succeed())
	g.Expect(version).To(Equal(uint(137)))
	g.Expect(dirty).To(BeTrue())
}

func TestApplyTimestampDirtyRecoveryForceHonorsContextDeadline(t *testing.T) {
	g := NewWithT(t)
	database := newRunnerTestDatabase(t)
	database.migrateTo(t, 137)
	setTimestampRecoverySchemaStatus(t, database, 137)
	plan, err := database.runner.PlanTimestampDirtyRecovery(
		context.Background(),
		timestampDirtyRecoveryPlanOptions(137),
	)
	g.Expect(err).NotTo(HaveOccurred())
	blocker, err := database.pool.Begin(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	defer func() { _ = blocker.Rollback(context.Background()) }()
	_, err = blocker.Exec(
		context.Background(),
		`LOCK TABLE schema_migrations IN ACCESS SHARE MODE`,
	)
	g.Expect(err).NotTo(HaveOccurred())
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	options := timestampDirtyRecoveryApplyOptions(t, plan, nil)
	options.LockTimeout = 5 * time.Second
	started := time.Now()

	result, err := database.runner.ApplyTimestampDirtyRecovery(
		ctx,
		plan,
		options,
	)

	g.Expect(err).To(MatchError(ContainSubstring("uncertain")))
	g.Expect(result).To(Equal(types.TimestampDirtyRecoveryResult{}))
	g.Expect(time.Since(started)).To(BeNumerically("<", 2*time.Second))
	g.Expect(blocker.Rollback(context.Background())).To(Succeed())
	var dirty bool
	g.Expect(database.pool.QueryRow(
		context.Background(),
		`SELECT dirty FROM schema_migrations`,
	).Scan(&dirty)).To(Succeed())
	g.Expect(dirty).To(BeTrue())

	retry, err := database.runner.ApplyTimestampDirtyRecovery(
		context.Background(),
		plan,
		timestampDirtyRecoveryApplyOptions(t, plan, nil),
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(retry.Action).To(Equal(
		types.TimestampDirtyRecoveryActionForced,
	))
}

func TestPlanTimestampDirtyRecoveryPreservesMarkerDependencySentinels(
	t *testing.T,
) {
	for _, test := range []struct {
		name     string
		setupSQL string
		querySQL string
		expected uint
	}{
		{
			name: "inheritance child",
			setupSQL: `
CREATE TABLE schema_migrations_recovery_child ()
INHERITS (schema_migrations);
INSERT INTO schema_migrations_recovery_child(version, dirty)
VALUES (999, TRUE)`,
			querySQL: `SELECT version FROM ONLY schema_migrations_recovery_child`,
			expected: 999,
		},
		{
			name: "inbound foreign key",
			setupSQL: `
CREATE TABLE schema_migrations_recovery_reference (
  version BIGINT NOT NULL
    REFERENCES schema_migrations(version)
);
INSERT INTO schema_migrations_recovery_reference(version) VALUES (137)`,
			querySQL: `SELECT version FROM schema_migrations_recovery_reference`,
			expected: 137,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			database := newRunnerTestDatabase(t)
			database.migrateTo(t, 137)
			setTimestampRecoverySchemaStatus(t, database, 137)
			_, err := database.pool.Exec(context.Background(), test.setupSQL)
			g.Expect(err).NotTo(HaveOccurred())

			plan, err := database.runner.PlanTimestampDirtyRecovery(
				context.Background(),
				timestampDirtyRecoveryPlanOptions(137),
			)

			g.Expect(err).To(HaveOccurred())
			g.Expect(plan).To(Equal(types.TimestampDirtyRecoveryPlan{}))
			var sentinel uint
			g.Expect(database.pool.QueryRow(
				context.Background(),
				test.querySQL,
			).Scan(&sentinel)).To(Succeed())
			g.Expect(sentinel).To(Equal(test.expected))
		})
	}
}

func TestPlanTimestampDirtyRecoveryRejectsLateFailingMarkerIndex(t *testing.T) {
	g := NewWithT(t)
	database := newRunnerTestDatabase(t)
	database.migrateTo(t, 137)
	setTimestampRecoverySchemaStatus(t, database, 138)
	_, err := database.pool.Exec(context.Background(), `
CREATE FUNCTION schema_migrations_recovery_index_probe(value BIGINT)
RETURNS BIGINT
IMMUTABLE
LANGUAGE plpgsql
AS $$
BEGIN
  IF value = 137 THEN
    RAISE EXCEPTION 'target marker rejected';
  END IF;
  RETURN value;
END;
$$;
CREATE INDEX schema_migrations_recovery_index_probe
ON schema_migrations (
  schema_migrations_recovery_index_probe(version)
)`)
	g.Expect(err).NotTo(HaveOccurred())

	plan, err := database.runner.PlanTimestampDirtyRecovery(
		context.Background(),
		timestampDirtyRecoveryPlanOptions(138),
	)

	g.Expect(err).To(MatchError(ContainSubstring(
		"schema_migrations catalog is malformed",
	)))
	g.Expect(plan).To(Equal(types.TimestampDirtyRecoveryPlan{}))
	var version uint
	var dirty bool
	g.Expect(database.pool.QueryRow(
		context.Background(),
		`SELECT version, dirty FROM schema_migrations`,
	).Scan(&version, &dirty)).To(Succeed())
	g.Expect(version).To(Equal(uint(138)))
	g.Expect(dirty).To(BeTrue())
}

type committedUncertainTimestampRecoveryForceEngine struct {
	database *sql.DB
	calls    *uint64
}

func (engine *committedUncertainTimestampRecoveryForceEngine) Force(
	version int,
) error {
	*engine.calls++
	if _, err := engine.database.Exec(
		`UPDATE schema_migrations SET version=$1, dirty=FALSE`,
		version,
	); err != nil {
		return err
	}
	return errors.New("injected uncertain response after committed marker")
}

func (*committedUncertainTimestampRecoveryForceEngine) Close() error {
	return nil
}

func (*committedUncertainTimestampRecoveryForceEngine) Discard() error {
	return nil
}

type postForceMarkerDriftEngine struct {
	database *sql.DB
	calls    *uint64
}

func (engine *postForceMarkerDriftEngine) Force(version int) error {
	*engine.calls++
	if _, err := engine.database.Exec(
		`UPDATE schema_migrations SET version=$1, dirty=FALSE`,
		version,
	); err != nil {
		return err
	}
	_, err := engine.database.Exec(`
CREATE INDEX schema_migrations_post_force_probe
ON schema_migrations (dirty)`)
	return err
}

func (*postForceMarkerDriftEngine) Close() error   { return nil }
func (*postForceMarkerDriftEngine) Discard() error { return nil }

func TestApplyTimestampDirtyRecoveryUncertainCommitHasNoResultAndCleanRetry(
	t *testing.T,
) {
	g := NewWithT(t)
	database := newRunnerTestDatabase(t)
	database.migrateTo(t, 137)
	setTimestampRecoverySchemaStatus(t, database, 138)
	plan, err := database.runner.PlanTimestampDirtyRecovery(
		context.Background(),
		timestampDirtyRecoveryPlanOptions(138),
	)
	g.Expect(err).NotTo(HaveOccurred())
	var forceCalls uint64
	database.runner.recoveryForceEngineFactory = func(
		_ context.Context,
		sqlDB *sql.DB,
		_ *zap.Logger,
		_ string,
		_ time.Duration,
	) (timestampDirtyRecoveryForceEngine, error) {
		return &committedUncertainTimestampRecoveryForceEngine{
			database: sqlDB,
			calls:    &forceCalls,
		}, nil
	}

	result, err := database.runner.ApplyTimestampDirtyRecovery(
		context.Background(),
		plan,
		timestampDirtyRecoveryApplyOptions(t, plan, nil),
	)

	g.Expect(err).To(MatchError(ContainSubstring("outcome is uncertain")))
	g.Expect(result).To(Equal(types.TimestampDirtyRecoveryResult{}))
	g.Expect(forceCalls).To(Equal(uint64(1)))

	retry, err := database.runner.ApplyTimestampDirtyRecovery(
		context.Background(),
		plan,
		timestampDirtyRecoveryApplyOptions(t, plan, nil),
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(retry.Action).To(Equal(
		types.TimestampDirtyRecoveryActionObservedAlreadyClean,
	))
	g.Expect(forceCalls).To(Equal(uint64(1)))
}

func TestApplyTimestampDirtyRecoveryPostForceFailureIsUncertainAndRetryable(
	t *testing.T,
) {
	g := NewWithT(t)
	database := newRunnerTestDatabase(t)
	database.migrateTo(t, 137)
	setTimestampRecoverySchemaStatus(t, database, 138)
	plan, err := database.runner.PlanTimestampDirtyRecovery(
		context.Background(),
		timestampDirtyRecoveryPlanOptions(138),
	)
	g.Expect(err).NotTo(HaveOccurred())
	var forceCalls uint64
	database.runner.recoveryForceEngineFactory = func(
		_ context.Context,
		sqlDB *sql.DB,
		_ *zap.Logger,
		_ string,
		_ time.Duration,
	) (timestampDirtyRecoveryForceEngine, error) {
		return &postForceMarkerDriftEngine{
			database: sqlDB,
			calls:    &forceCalls,
		}, nil
	}

	result, err := database.runner.ApplyTimestampDirtyRecovery(
		context.Background(),
		plan,
		timestampDirtyRecoveryApplyOptions(t, plan, nil),
	)

	g.Expect(err).To(MatchError(ContainSubstring(
		"uncertain after successful force",
	)))
	g.Expect(result).To(Equal(types.TimestampDirtyRecoveryResult{}))
	g.Expect(forceCalls).To(Equal(uint64(1)))
	_, err = database.pool.Exec(
		context.Background(),
		`DROP INDEX schema_migrations_post_force_probe`,
	)
	g.Expect(err).NotTo(HaveOccurred())

	retry, err := database.runner.ApplyTimestampDirtyRecovery(
		context.Background(),
		plan,
		timestampDirtyRecoveryApplyOptions(t, plan, nil),
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(retry.Action).To(Equal(
		types.TimestampDirtyRecoveryActionObservedAlreadyClean,
	))
	g.Expect(forceCalls).To(Equal(uint64(1)))
}

func TestApplyTimestampDirtyRecoveryDiscardsBoundedForceSession(t *testing.T) {
	g := NewWithT(t)
	database := newRunnerTestDatabase(t)
	database.migrateTo(t, 137)
	setTimestampRecoverySchemaStatus(t, database, 137)
	plan, err := database.runner.PlanTimestampDirtyRecovery(
		context.Background(),
		timestampDirtyRecoveryPlanOptions(137),
	)
	g.Expect(err).NotTo(HaveOccurred())
	originalFactory := database.runner.recoveryForceEngineFactory
	var forcePID int32
	var constructed timestampDirtyRecoveryForceEngine
	database.runner.recoveryForceEngineFactory = func(
		ctx context.Context,
		sqlDB *sql.DB,
		logger *zap.Logger,
		schema string,
		timeout time.Duration,
	) (timestampDirtyRecoveryForceEngine, error) {
		engine, err := originalFactory(
			ctx,
			sqlDB,
			logger,
			schema,
			timeout,
		)
		if err != nil {
			return nil, err
		}
		constructed = engine
		concrete := engine.(*golangTimestampDirtyRecoveryForceEngine)
		if err := concrete.connection.QueryRowContext(
			ctx,
			`SELECT pg_backend_pid()`,
		).Scan(&forcePID); err != nil {
			return nil, err
		}
		return engine, nil
	}

	result, err := database.runner.ApplyTimestampDirtyRecovery(
		context.Background(),
		plan,
		timestampDirtyRecoveryApplyOptions(t, plan, nil),
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Action).To(Equal(
		types.TimestampDirtyRecoveryActionForced,
	))
	g.Expect(forcePID).NotTo(BeZero())
	g.Expect(constructed.Discard()).To(Succeed())
	var forceBackendRows uint64
	g.Expect(database.pool.QueryRow(
		context.Background(),
		`SELECT count(*) FROM pg_stat_activity WHERE pid=$1`,
		forcePID,
	).Scan(&forceBackendRows)).To(Succeed())
	g.Expect(forceBackendRows).To(BeZero())
	var lockTimeout, statementTimeout string
	g.Expect(database.runner.db.QueryRowContext(
		context.Background(),
		`SHOW lock_timeout`,
	).Scan(&lockTimeout)).To(Succeed())
	g.Expect(database.runner.db.QueryRowContext(
		context.Background(),
		`SHOW statement_timeout`,
	).Scan(&statementTimeout)).To(Succeed())
	g.Expect(lockTimeout).To(Equal("0"))
	g.Expect(statementTimeout).To(Equal("0"))
}

func TestTimestampDirtyRecoveryCatalogClassifierMatrix(t *testing.T) {
	for _, test := range []struct {
		name    string
		version uint
		shape   types.TimestampRecoveryCatalogShape
	}{
		{
			name:    "exact predecessor",
			version: 137,
			shape:   types.TimestampRecoveryCatalogShapePredecessor137,
		},
		{
			name:    "exact expand",
			version: 138,
			shape:   types.TimestampRecoveryCatalogShapeExpand138,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			database := newRunnerTestDatabase(t)
			database.migrateTo(t, test.version)
			tx, err := database.pool.BeginTx(
				context.Background(),
				pgx.TxOptions{
					IsoLevel:   pgx.RepeatableRead,
					AccessMode: pgx.ReadOnly,
				},
			)
			g.Expect(err).NotTo(HaveOccurred())
			defer func() { _ = tx.Rollback(context.Background()) }()

			catalog, err := classifyTimestampDirtyRecoveryCatalog(
				context.Background(),
				tx,
			)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(catalog.Shape).To(Equal(test.shape))
			g.Expect(catalog.Checksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
			g.Expect(catalog.Records).NotTo(BeEmpty())
		})
	}
}

func TestTimestampDirtyRecoveryCatalogRejectsPG18NotValidNotNullConstraint(
	t *testing.T,
) {
	g := NewWithT(t)
	database := newRunnerTestDatabase(t)
	var serverVersion int
	g.Expect(database.pool.QueryRow(
		context.Background(),
		`SELECT current_setting('server_version_num')::integer`,
	).Scan(&serverVersion)).To(Succeed())
	if serverVersion < 180000 {
		t.Skip("requires the hosted PostgreSQL 18 compatibility gate")
	}
	database.migrateTo(t, 137)
	executionID := insertTimestampRecoveryHistoricalFixture(t, database)
	ctx := context.Background()
	tx, err := database.pool.Begin(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `
ALTER TABLE ExternalExecution
	ALTER COLUMN created_at DROP NOT NULL`)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = tx.Exec(ctx, `
UPDATE ExternalExecution SET created_at = NULL WHERE id = $1`, executionID)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = tx.Exec(ctx, `
ALTER TABLE ExternalExecution
  ADD CONSTRAINT recovery_created_at_not_null
  NOT NULL created_at NOT VALID`)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(tx.Commit(ctx)).To(Succeed())
	var attributeNotNull, constraintValidated bool
	g.Expect(database.pool.QueryRow(
		context.Background(),
		`
SELECT attribute_row.attnotnull, constraint_row.convalidated
FROM pg_catalog.pg_attribute attribute_row
JOIN pg_catalog.pg_constraint constraint_row
  ON constraint_row.conrelid = attribute_row.attrelid
 AND constraint_row.contype = 'n'
 AND constraint_row.conkey = ARRAY[attribute_row.attnum]::smallint[]
WHERE attribute_row.attrelid = 'externalexecution'::regclass
  AND attribute_row.attname = 'created_at'
  AND constraint_row.conname = 'recovery_created_at_not_null'`,
	).Scan(&attributeNotNull, &constraintValidated)).To(Succeed())
	g.Expect(attributeNotNull).To(BeTrue())
	g.Expect(constraintValidated).To(BeFalse())
	manifest := approvedTimestampRecoveryManifest(t, database)
	setTimestampRecoverySchemaStatus(t, database, 137)
	options := timestampDirtyRecoveryPlanOptions(137)
	options.Manifest = &manifest
	options.ManifestDocumentChecksum = timestampRecoveryTestDocumentChecksum(t, manifest)

	plan, err := database.runner.PlanTimestampDirtyRecovery(
		context.Background(),
		options,
	)

	g.Expect(err).To(MatchError(ContainSubstring(
		"partial, mixed, extra, or mutated",
	)))
	g.Expect(plan).To(Equal(types.TimestampDirtyRecoveryPlan{}))
}

func TestTimestampDirtyRecoveryCatalogRejectsEveryOwnedObjectCategoryMutation(
	t *testing.T,
) {
	for _, test := range []struct {
		name      string
		statement string
	}{
		{
			name:      "source relation",
			statement: `ALTER TABLE ExternalExecution ENABLE ROW LEVEL SECURITY`,
		},
		{
			name: "source inheritance child",
			statement: `
CREATE TABLE recovery_source_inheritance_probe ()
INHERITS (ExternalExecution)`,
		},
		{
			name: "source rewrite rule",
			statement: `
CREATE RULE recovery_source_update_probe
AS ON UPDATE TO ExternalExecution
DO INSTEAD NOTHING`,
		},
		{
			name: "source timestamp column",
			statement: `
ALTER TABLE ExternalExecution
ADD COLUMN recovery_probe_instant TIMESTAMPTZ`,
		},
		{
			name: "owned constraint",
			statement: `
ALTER TABLE ExternalExecutionTimestampManifest
ADD CONSTRAINT recovery_probe_constraint CHECK (id IS NOT NULL)`,
		},
		{
			name: "source constraint",
			statement: `
ALTER TABLE ExternalExecution
ADD CONSTRAINT recovery_source_probe_constraint CHECK (id IS NOT NULL)`,
		},
		{
			name: "timestamp source index",
			statement: `
CREATE INDEX externalexecution_timestamp_recovery_probe
ON ExternalExecution (id)`,
		},
		{
			name: "source index include mutation",
			statement: `
DROP INDEX ExternalExecution_task;
CREATE INDEX ExternalExecution_task
ON ExternalExecution (task_id, created_at, id)
INCLUDE (status)`,
		},
		{
			name: "source trigger",
			statement: `
CREATE TRIGGER ExternalExecution_timestamp_recovery_probe
BEFORE INSERT ON ExternalExecution
FOR EACH ROW
EXECUTE FUNCTION external_execution_timestamp_pair_guard()`,
		},
		{
			name: "owned function",
			statement: `
CREATE FUNCTION external_execution_timestamp_recovery_probe()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
  RETURN NEW;
END;
$$`,
		},
		{
			name: "weakened active parent null uniqueness",
			statement: `
DROP INDEX externalexecutiontimestampmanifest_active_parent_unique;
CREATE UNIQUE INDEX externalexecutiontimestampmanifest_active_parent_unique
ON ExternalExecutionTimestampManifest (supersedes_manifest_id)
WHERE state <> 'REVOKED_BEFORE_APPLY'`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			database := newRunnerTestDatabase(t)
			database.migrateTo(t, 138)
			_, err := database.pool.Exec(
				context.Background(),
				test.statement,
			)
			g.Expect(err).NotTo(HaveOccurred())
			tx, err := database.pool.BeginTx(
				context.Background(),
				pgx.TxOptions{
					IsoLevel:   pgx.RepeatableRead,
					AccessMode: pgx.ReadOnly,
				},
			)
			g.Expect(err).NotTo(HaveOccurred())
			defer func() { _ = tx.Rollback(context.Background()) }()

			catalog, err := classifyTimestampDirtyRecoveryCatalog(
				context.Background(),
				tx,
			)
			g.Expect(err).To(MatchError(ContainSubstring(
				"partial, mixed, extra, or mutated",
			)))
			g.Expect(catalog).To(Equal(timestampDirtyRecoveryCatalog{}))
		})
	}
}

func TestTimestampDirtyRecoveryCatalogRejectsCrossSchemaTriggerFunctionSwap(
	t *testing.T,
) {
	g := NewWithT(t)
	database := newRunnerTestDatabase(t)
	database.migrateTo(t, 138)
	evilSchema := database.schema + "_evil"
	quotedEvilSchema := pgx.Identifier{evilSchema}.Sanitize()
	defer func() {
		_, _ = database.pool.Exec(
			context.Background(),
			"DROP SCHEMA IF EXISTS "+quotedEvilSchema+" CASCADE",
		)
	}()
	_, err := database.pool.Exec(
		context.Background(),
		"CREATE SCHEMA "+quotedEvilSchema,
	)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = database.pool.Exec(context.Background(), fmt.Sprintf(`
CREATE FUNCTION %s.external_execution_timestamp_provenance_append_only()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
  RETURN NEW;
END;
$$;
DROP TRIGGER ExternalExecutionTimestampCellProvenance_append_only
  ON ExternalExecutionTimestampCellProvenance;
CREATE TRIGGER ExternalExecutionTimestampCellProvenance_append_only
BEFORE UPDATE OR DELETE ON ExternalExecutionTimestampCellProvenance
FOR EACH ROW
EXECUTE FUNCTION %s.external_execution_timestamp_provenance_append_only()`,
		quotedEvilSchema,
		quotedEvilSchema,
	))
	g.Expect(err).NotTo(HaveOccurred())
	tx, err := database.pool.BeginTx(
		context.Background(),
		pgx.TxOptions{
			IsoLevel:   pgx.RepeatableRead,
			AccessMode: pgx.ReadOnly,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	defer func() { _ = tx.Rollback(context.Background()) }()

	catalog, err := classifyTimestampDirtyRecoveryCatalog(
		context.Background(),
		tx,
	)

	g.Expect(err).To(MatchError(ContainSubstring(
		"partial, mixed, extra, or mutated",
	)))
	g.Expect(catalog).To(Equal(timestampDirtyRecoveryCatalog{}))
}
