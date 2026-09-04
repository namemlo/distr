package migrations

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	. "github.com/onsi/gomega"
)

type migration172Attempt struct {
	id                 uuid.UUID
	organizationID     uuid.UUID
	deploymentTargetID uuid.UUID
	executionID        uuid.UUID
	attemptNumber      int
	stepKey            string
}

func TestMigration172PreservesLegacyAndV3AttemptsAndEnforcesV4(t *testing.T) {
	g := NewWithT(t)
	database := newMigrationTestDatabase(t)
	database.migrateTo(t, 171)
	organizationID := prepareMigration172AttemptFixtures(t, database)

	legacy := insertMigration172Attempt(
		t, database, organizationID, "legacy-v2", nil, nil, nil,
	)
	v3 := insertMigration172Attempt(
		t, database, organizationID, "v3", nil, nil, nil,
	)
	database.migrateTo(t, 172)

	for _, attemptID := range []uuid.UUID{legacy.id, v3.id} {
		var runtimeManifest, desiredServiceConfig, expectedServiceConfig *string
		g.Expect(database.pool.QueryRow(context.Background(), `
SELECT runtime_manifest_checksum, desired_service_config_checksum,
  expected_current_service_config_checksum
FROM ExecutionAttempt
WHERE id = $1
`, attemptID).Scan(
			&runtimeManifest, &desiredServiceConfig, &expectedServiceConfig,
		)).To(Succeed())
		g.Expect(runtimeManifest).To(BeNil())
		g.Expect(desiredServiceConfig).To(BeNil())
		g.Expect(expectedServiceConfig).To(BeNil())
	}

	var defaultExpression string
	g.Expect(database.pool.QueryRow(context.Background(), `
SELECT column_default
FROM information_schema.columns
WHERE table_schema = current_schema()
  AND table_name = 'executionattempt'
  AND column_name = 'runtime_contract_version'
`).Scan(&defaultExpression)).To(Succeed())
	g.Expect(defaultExpression).To(Equal("'v4'::text"))

	incompleteErr := insertMigration172AttemptError(
		database, organizationID, "v4",
		migration172Checksum("a"), nil, migration172Checksum("c"),
	)
	expectMigration172CheckViolation(
		t, incompleteErr, "executionattempt_runtime_contract_shape_check",
	)

	v4 := insertMigration172Attempt(
		t, database, organizationID, "v4",
		migration172Checksum("a"), migration172Checksum("b"),
		migration172Checksum("c"),
	)
	_, err := database.pool.Exec(context.Background(), `
UPDATE ExecutionAttempt
SET runtime_manifest_checksum = $2
WHERE id = $1
`, v4.id, migration172Checksum("d"))
	expectMigration172CheckViolation(t, err, "")
	g.Expect(err.Error()).To(ContainSubstring(
		"execution attempt runtime trust contract is immutable",
	))
}

func TestMigration172VersionsPhysicalRuntimeEvidence(t *testing.T) {
	database := newMigrationTestDatabase(t)
	database.migrateTo(t, 172)
	organizationID := prepareMigration172AttemptFixtures(t, database)

	v1Attempt := insertMigration172Attempt(
		t, database, organizationID, "v3", nil, nil, nil,
	)
	insertMigration172Intent(t, database, v1Attempt)
	NewWithT(t).Expect(insertMigration172Evidence(
		database, v1Attempt, "distr.execution-runtime-evidence/v1", nil, nil,
	)).To(Succeed())

	v1PhysicalAttempt := insertMigration172Attempt(
		t, database, organizationID, "v3", nil, nil, nil,
	)
	insertMigration172Intent(t, database, v1PhysicalAttempt)
	err := insertMigration172Evidence(
		database,
		v1PhysicalAttempt,
		"distr.execution-runtime-evidence/v1",
		migration172Checksum("d"),
		migration172Checksum("e"),
	)
	expectMigration172CheckViolation(
		t, err, "executionruntimeevidence_service_config_shape_check",
	)

	v2IncompleteAttempt := insertMigration172Attempt(
		t, database, organizationID, "v4",
		migration172Checksum("a"), migration172Checksum("b"),
		migration172Checksum("c"),
	)
	insertMigration172Intent(t, database, v2IncompleteAttempt)
	err = insertMigration172Evidence(
		database,
		v2IncompleteAttempt,
		"distr.execution-runtime-evidence/v2",
		migration172Checksum("d"),
		nil,
	)
	expectMigration172CheckViolation(
		t, err, "executionruntimeevidence_service_config_shape_check",
	)

	v2Attempt := insertMigration172Attempt(
		t, database, organizationID, "v4",
		migration172Checksum("a"), migration172Checksum("b"),
		migration172Checksum("c"),
	)
	insertMigration172Intent(t, database, v2Attempt)
	NewWithT(t).Expect(insertMigration172Evidence(
		database,
		v2Attempt,
		"distr.execution-runtime-evidence/v2",
		migration172Checksum("d"),
		migration172Checksum("e"),
	)).To(Succeed())
}

func TestMigration172CleanDowngradePreservesV3AndV1Evidence(t *testing.T) {
	g := NewWithT(t)
	database := newMigrationTestDatabase(t)
	database.migrateTo(t, 171)
	organizationID := prepareMigration172AttemptFixtures(t, database)
	v3 := insertMigration172Attempt(
		t, database, organizationID, "v3", nil, nil, nil,
	)
	database.migrateTo(t, 172)
	insertMigration172Intent(t, database, v3)
	g.Expect(insertMigration172Evidence(
		database, v3, "distr.execution-runtime-evidence/v1", nil, nil,
	)).To(Succeed())

	database.migrateTo(t, 171)

	var runtimeVersion string
	g.Expect(database.pool.QueryRow(context.Background(), `
SELECT runtime_contract_version FROM ExecutionAttempt WHERE id = $1
`, v3.id).Scan(&runtimeVersion)).To(Succeed())
	g.Expect(runtimeVersion).To(Equal("v3"))

	var evidenceCount int
	g.Expect(database.pool.QueryRow(context.Background(), `
SELECT count(*) FROM ExecutionRuntimeEvidence WHERE execution_attempt_id = $1
`, v3.id).Scan(&evidenceCount)).To(Succeed())
	g.Expect(evidenceCount).To(Equal(1))

	var addedColumns int
	g.Expect(database.pool.QueryRow(context.Background(), `
SELECT count(*)
FROM information_schema.columns
WHERE table_schema = current_schema()
  AND (
    (table_name = 'executionattempt' AND column_name IN (
      'runtime_manifest_checksum',
      'desired_service_config_checksum',
      'expected_current_service_config_checksum'
    ))
    OR
    (table_name = 'executionruntimeevidence' AND column_name IN (
      'pre_execution_service_config_checksum',
      'result_service_config_checksum'
    ))
  )
`).Scan(&addedColumns)).To(Succeed())
	g.Expect(addedColumns).To(Equal(0))
}

func TestMigration172DowngradeRefusesV4AttemptsAndV2Evidence(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		setup func(*testing.T, *migrationTestDatabase, uuid.UUID)
	}{
		{
			name: "v4 attempt",
			setup: func(t *testing.T, database *migrationTestDatabase, organizationID uuid.UUID) {
				insertMigration172Attempt(
					t, database, organizationID, "v4",
					migration172Checksum("a"), migration172Checksum("b"),
					migration172Checksum("c"),
				)
			},
		},
		{
			name: "v2 evidence",
			setup: func(t *testing.T, database *migrationTestDatabase, organizationID uuid.UUID) {
				attempt := insertMigration172Attempt(
					t, database, organizationID, "v3", nil, nil, nil,
				)
				insertMigration172Intent(t, database, attempt)
				NewWithT(t).Expect(insertMigration172Evidence(
					database,
					attempt,
					"distr.execution-runtime-evidence/v2",
					migration172Checksum("d"),
					migration172Checksum("e"),
				)).To(Succeed())
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			g := NewWithT(t)
			database := newMigrationTestDatabase(t)
			database.migrateTo(t, 172)
			organizationID := prepareMigration172AttemptFixtures(t, database)
			testCase.setup(t, database, organizationID)

			err := database.runner.Migrate(171)
			g.Expect(err).To(MatchError(ContainSubstring(
				"refusing migration 172 rollback while v4 runtime checksum contracts or evidence exist",
			)))

			var addedColumns int
			g.Expect(database.pool.QueryRow(context.Background(), `
SELECT count(*)
FROM information_schema.columns
WHERE table_schema = current_schema()
  AND table_name = 'executionattempt'
  AND column_name = 'runtime_manifest_checksum'
`).Scan(&addedColumns)).To(Succeed())
			g.Expect(addedColumns).To(Equal(1))
		})
	}
}

func prepareMigration172AttemptFixtures(
	t *testing.T,
	database *migrationTestDatabase,
) uuid.UUID {
	t.Helper()
	g := NewWithT(t)
	organizationID := uuid.New()
	_, err := database.pool.Exec(context.Background(), `
INSERT INTO Organization (id, name) VALUES ($1, $2)
`, organizationID, "Migration 172 "+uuid.NewString())
	g.Expect(err).NotTo(HaveOccurred())
	_, err = database.pool.Exec(context.Background(), `
ALTER TABLE ExecutionAttempt
  DROP CONSTRAINT executionattempt_task_fk,
  DROP CONSTRAINT executionattempt_step_run_fk
`)
	g.Expect(err).NotTo(HaveOccurred())
	return organizationID
}

func insertMigration172Attempt(
	t *testing.T,
	database *migrationTestDatabase,
	organizationID uuid.UUID,
	runtimeVersion string,
	runtimeManifestChecksum, desiredServiceConfigChecksum,
	expectedCurrentServiceConfigChecksum any,
) migration172Attempt {
	t.Helper()
	attempt, err := insertMigration172AttemptWithError(
		database,
		organizationID,
		runtimeVersion,
		runtimeManifestChecksum,
		desiredServiceConfigChecksum,
		expectedCurrentServiceConfigChecksum,
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return attempt
}

func insertMigration172AttemptError(
	database *migrationTestDatabase,
	organizationID uuid.UUID,
	runtimeVersion string,
	runtimeManifestChecksum, desiredServiceConfigChecksum,
	expectedCurrentServiceConfigChecksum any,
) error {
	_, err := insertMigration172AttemptWithError(
		database,
		organizationID,
		runtimeVersion,
		runtimeManifestChecksum,
		desiredServiceConfigChecksum,
		expectedCurrentServiceConfigChecksum,
	)
	return err
}

func insertMigration172AttemptWithError(
	database *migrationTestDatabase,
	organizationID uuid.UUID,
	runtimeVersion string,
	runtimeManifestChecksum, desiredServiceConfigChecksum,
	expectedCurrentServiceConfigChecksum any,
) (migration172Attempt, error) {
	attempt := migration172Attempt{
		id:                 uuid.New(),
		organizationID:     organizationID,
		deploymentTargetID: uuid.New(),
		executionID:        uuid.New(),
		attemptNumber:      1,
		stepKey:            "migration-172-" + uuid.NewString(),
	}
	now := time.Now().UTC()
	var revision any
	var expectedObservedChecksum, expectedImage, expectedConfig any
	var expectedPlatform, caller, audience any
	if runtimeVersion != "legacy-v2" {
		revision = int64(1)
		expectedObservedChecksum = migration172Checksum("4")
		expectedImage = migration172Checksum("5")
		expectedConfig = migration172Checksum("6")
		expectedPlatform = "linux/amd64"
		caller = "urn:distr:caller:migration-172"
		audience = "urn:distr:audience:migration-172"
	}
	var hasRuntimeChecksumColumns bool
	err := database.pool.QueryRow(context.Background(), `
SELECT EXISTS (
  SELECT 1
  FROM information_schema.columns
  WHERE table_schema = current_schema()
    AND table_name = 'executionattempt'
    AND column_name = 'runtime_manifest_checksum'
)
`).Scan(&hasRuntimeChecksumColumns)
	if err != nil {
		return attempt, err
	}
	if !hasRuntimeChecksumColumns {
		_, err = database.pool.Exec(context.Background(), `
INSERT INTO ExecutionAttempt (
  id, organization_id, deployment_target_id, task_id, step_run_id,
  execution_id, attempt_number, step_key, status,
  plan_checksum, artifact_digest, config_checksum, adapter_revision,
  runtime_contract_version, expected_observed_state_revision,
  expected_observed_state_checksum, expected_current_image_digest,
  expected_current_config_checksum, expected_platform,
  intent_caller, intent_audience,
  intent_issued_at, intent_expires_at, cancellable, retry_safe
) VALUES (
  $1, $2, $3, $4, $5,
  $6, $7, $8, 'PENDING',
  $9, $10, $11, 'adapter.compose@2',
  $12, $13, $14, $15, $16, $17, $18, $19,
  $20, $21, TRUE, TRUE
)
`,
			attempt.id, attempt.organizationID, attempt.deploymentTargetID,
			uuid.New(), uuid.New(), attempt.executionID, attempt.attemptNumber,
			attempt.stepKey,
			migration172Checksum("1"), migration172Checksum("2"),
			migration172Checksum("3"), runtimeVersion, revision,
			expectedObservedChecksum, expectedImage, expectedConfig,
			expectedPlatform, caller, audience,
			now, now.Add(10*time.Minute),
		)
		return attempt, err
	}

	_, err = database.pool.Exec(context.Background(), `
INSERT INTO ExecutionAttempt (
  id, organization_id, deployment_target_id, task_id, step_run_id,
  execution_id, attempt_number, step_key, status,
  plan_checksum, artifact_digest, config_checksum, adapter_revision,
  runtime_contract_version, expected_observed_state_revision,
  expected_observed_state_checksum, expected_current_image_digest,
  expected_current_config_checksum, expected_platform,
  intent_caller, intent_audience,
  runtime_manifest_checksum, desired_service_config_checksum,
  expected_current_service_config_checksum,
  intent_issued_at, intent_expires_at, cancellable, retry_safe
) VALUES (
  $1, $2, $3, $4, $5,
  $6, $7, $8, 'PENDING',
  $9, $10, $11, 'adapter.compose@2',
  $12, $13, $14, $15, $16, $17, $18, $19,
  $20, $21, $22,
  $23, $24, TRUE, TRUE
)
`,
		attempt.id, attempt.organizationID, attempt.deploymentTargetID,
		uuid.New(), uuid.New(), attempt.executionID, attempt.attemptNumber,
		attempt.stepKey,
		migration172Checksum("1"), migration172Checksum("2"),
		migration172Checksum("3"), runtimeVersion, revision,
		expectedObservedChecksum, expectedImage, expectedConfig,
		expectedPlatform, caller, audience,
		runtimeManifestChecksum, desiredServiceConfigChecksum,
		expectedCurrentServiceConfigChecksum,
		now, now.Add(10*time.Minute),
	)
	return attempt, err
}

func insertMigration172Intent(
	t *testing.T,
	database *migrationTestDatabase,
	attempt migration172Attempt,
) string {
	t.Helper()
	payload := []byte(`{}`)
	checksum := fmt.Sprintf("sha256:%x", sha256.Sum256(payload))
	_, err := database.pool.Exec(context.Background(), `
INSERT INTO ExecutionIntent (
  organization_id, execution_attempt_id, payload, checksum, key_id, signature
) VALUES ($1, $2, $3, $4, $5, $6)
`,
		attempt.organizationID,
		attempt.id,
		payload,
		checksum,
		migration172Checksum("7"),
		strings.Repeat("A", 86),
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return checksum
}

func insertMigration172Evidence(
	database *migrationTestDatabase,
	attempt migration172Attempt,
	schemaVersion string,
	preExecutionServiceConfigChecksum, resultServiceConfigChecksum any,
) error {
	payload := []byte(`{}`)
	intentChecksum := fmt.Sprintf("sha256:%x", sha256.Sum256(payload))
	_, err := database.pool.Exec(context.Background(), `
INSERT INTO ExecutionRuntimeEvidence (
  organization_id, deployment_target_id, execution_attempt_id,
  execution_id, attempt_number, step_key, event_identity, schema_version,
  intent_checksum, executor_id, caller_identity, audience,
  fence_generation, expected_observed_state_revision,
  expected_observed_state_checksum, pre_execution_image_digest,
  pre_execution_config_checksum, result_image_digest, result_config_checksum,
  pre_execution_service_config_checksum, result_service_config_checksum,
  platform, health_status, result_checksum, evidence_reference,
  evidence_checksum, canonical_checksum, captured_at
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8,
  $9, 'migration-172-executor', 'urn:distr:caller:migration-172',
  'urn:distr:audience:migration-172',
  1, 1, $10, $11, $12, $13, $14, $15, $16,
  'linux/amd64', 'HEALTHY', $17, $18, $19, $20, $21
)
`,
		attempt.organizationID,
		attempt.deploymentTargetID,
		attempt.id,
		attempt.executionID,
		attempt.attemptNumber,
		attempt.stepKey,
		uuid.New(),
		schemaVersion,
		intentChecksum,
		migration172Checksum("4"),
		migration172Checksum("5"),
		migration172Checksum("6"),
		migration172Checksum("2"),
		migration172Checksum("3"),
		preExecutionServiceConfigChecksum,
		resultServiceConfigChecksum,
		migration172Checksum("8"),
		"fixture://migration-172/"+attempt.id.String(),
		migration172Checksum("9"),
		migration172Checksum("a"),
		time.Now().UTC(),
	)
	return err
}

func expectMigration172CheckViolation(
	t *testing.T,
	err error,
	constraintName string,
) {
	t.Helper()
	g := NewWithT(t)
	g.Expect(err).To(HaveOccurred())
	var pgErr *pgconn.PgError
	g.Expect(errors.As(err, &pgErr)).To(BeTrue())
	g.Expect(pgErr.Code).To(Equal(pgerrcode.CheckViolation))
	if constraintName != "" {
		g.Expect(pgErr.ConstraintName).To(Equal(constraintName))
	}
}

func migration172Checksum(hexDigit string) string {
	return "sha256:" + strings.Repeat(hexDigit, 64)
}
