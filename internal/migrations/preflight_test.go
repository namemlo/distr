package migrations

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/externalexecutiontimestamp"
	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

func uintPointer(value uint) *uint { return &value }

func insertRunnerHistoricalFixture(
	t *testing.T,
	database *runnerTestDatabase,
) {
	t.Helper()
	insertHistoricalExecutionFixture(t, &migrationTestDatabase{pool: database.pool})
}

func approvedRunnerManifest(
	t *testing.T,
	database *runnerTestDatabase,
) types.ExternalExecutionTimestampManifest {
	t.Helper()
	g := NewWithT(t)
	ctx := internalctx.WithDb(context.Background(), database.pool)
	manifest, err := db.InspectExternalExecutionTimestamps(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	zero := int32(0)
	for index := range manifest.Cells {
		cell := &manifest.Cells[index]
		if cell.RawValue == nil {
			continue
		}
		cell.Decision = types.ExternalExecutionTimestampDecisionProven
		cell.SourceZone = "UTC"
		cell.SourceOffsetSeconds = &zero
		converted := *cell.RawValue + "Z"
		cell.ConvertedValue = &converted
		cell.EvidenceReference = "runner-test-evidence"
		cell.EvidenceChecksum = "sha256:" + strings.Repeat("e", 64)
		cell.ApprovingIdentity = "runner-test-approver"
	}
	sealed, err := externalexecutiontimestamp.SealManifest(
		*manifest,
		types.ExternalExecutionTimestampSealOptions{
			AuthorIdentity:          "runner-test-author",
			ReviewerIdentity:        "runner-test-reviewer",
			EvidenceBundleReference: "runner-test-bundle",
			EvidenceBundleChecksum:  "sha256:" + strings.Repeat("a", 64),
			TargetReleaseCommit:     strings.Repeat("b", 40),
			TargetImageDigest:       "sha256:" + strings.Repeat("c", 64),
		},
		time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
	)
	g.Expect(err).NotTo(HaveOccurred())
	return sealed
}

func insertRunnerManifest(
	t *testing.T,
	database *runnerTestDatabase,
	manifest types.ExternalExecutionTimestampManifest,
	state types.ExternalExecutionTimestampManifestState,
) {
	t.Helper()
	g := NewWithT(t)
	_, err := database.pool.Exec(context.Background(), `
INSERT INTO ExternalExecutionTimestampManifest (
  id, supersedes_manifest_id, database_identity_checksum,
  source_schema_version, snapshot_started_at, snapshot_ended_at,
  execution_count, event_count, raw_cell_count, populated_cell_count,
  raw_cell_checksum, evidence_bundle_reference, evidence_bundle_checksum,
  tool_version, conversion_expression_version,
  author_identity, reviewer_identity, approved_at,
  target_release_commit, target_image_digest,
  state, decision_content_checksum
) VALUES (
  $1, $2, $3, $4, $5, $6,
  $7, $8, $9, $10,
  $11, $12, $13,
  $14, $15,
  $16, $17, $18,
  $19, $20,
  'APPROVED', $21
)`,
		manifest.ID,
		manifest.SupersedesManifestID,
		manifest.DatabaseIdentityChecksum,
		manifest.SourceSchemaVersion,
		manifest.SnapshotStartedAt,
		manifest.SnapshotEndedAt,
		manifest.ExecutionCount,
		manifest.EventCount,
		manifest.RawCellCount,
		manifest.PopulatedCellCount,
		manifest.RawCellChecksum,
		manifest.EvidenceBundleReference,
		manifest.EvidenceBundleChecksum,
		manifest.ToolVersion,
		manifest.ConversionExpressionVersion,
		manifest.AuthorIdentity,
		manifest.ReviewerIdentity,
		manifest.ApprovedAt,
		manifest.TargetReleaseCommit,
		manifest.TargetImageDigest,
		manifest.DecisionContentChecksum,
	)
	g.Expect(err).NotTo(HaveOccurred())
	if state == types.ExternalExecutionTimestampManifestStateApproved {
		return
	}
	_, err = database.pool.Exec(context.Background(), `
UPDATE ExternalExecutionTimestampManifest
SET state='APPLIED', applied_at=clock_timestamp()
WHERE id=$1`, manifest.ID)
	g.Expect(err).NotTo(HaveOccurred())
	if state == types.ExternalExecutionTimestampManifestStateApplied {
		return
	}
	_, err = database.pool.Exec(context.Background(), `
UPDATE ExternalExecutionTimestampManifest
SET state='VERIFIED', verified_at=clock_timestamp()
WHERE id=$1`, manifest.ID)
	g.Expect(err).NotTo(HaveOccurred())
}

func TestExternalExecutionTimestampPreflight(t *testing.T) {
	type setupResult struct {
		manifest *types.ExternalExecutionTimestampManifest
	}
	tests := []struct {
		name      string
		target    uint
		setup     func(*testing.T, *runnerTestDatabase) setupResult
		wantError string
	}{
		{
			name: "clean install", target: 138,
			setup: func(*testing.T, *runnerTestDatabase) setupResult { return setupResult{} },
		},
		{
			name: "empty 137", target: 138,
			setup: func(t *testing.T, database *runnerTestDatabase) setupResult {
				database.migrateTo(t, 137)
				return setupResult{}
			},
		},
		{
			name: "data below 137", target: 138,
			setup: func(t *testing.T, database *runnerTestDatabase) setupResult {
				database.migrateTo(t, 136)
				insertRunnerHistoricalFixture(t, database)
				return setupResult{}
			},
			wantError: "migrate to exact schema 137",
		},
		{
			name: "nonempty no manifest", target: 138,
			setup: func(t *testing.T, database *runnerTestDatabase) setupResult {
				database.migrateTo(t, 137)
				insertRunnerHistoricalFixture(t, database)
				return setupResult{}
			},
			wantError: "approved manifest is required",
		},
		{
			name: "matching manifest", target: 138,
			setup: func(t *testing.T, database *runnerTestDatabase) setupResult {
				database.migrateTo(t, 137)
				insertRunnerHistoricalFixture(t, database)
				manifest := approvedRunnerManifest(t, database)
				return setupResult{manifest: &manifest}
			},
		},
		{
			name: "changed raw", target: 138,
			setup: func(t *testing.T, database *runnerTestDatabase) setupResult {
				database.migrateTo(t, 137)
				insertRunnerHistoricalFixture(t, database)
				manifest := approvedRunnerManifest(t, database)
				_, err := database.pool.Exec(context.Background(), `
UPDATE ExternalExecution SET updated_at=updated_at + interval '1 second'`)
				NewWithT(t).Expect(err).NotTo(HaveOccurred())
				return setupResult{manifest: &manifest}
			},
			wantError: "raw checksum",
		},
		{
			name: "manifest target later than 138", target: 139,
			setup: func(t *testing.T, database *runnerTestDatabase) setupResult {
				database.migrateTo(t, 137)
				insertRunnerHistoricalFixture(t, database)
				manifest := approvedRunnerManifest(t, database)
				return setupResult{manifest: &manifest}
			},
			wantError: "explicit target 138",
		},
		{
			name: "dirty", target: 138,
			setup: func(t *testing.T, database *runnerTestDatabase) setupResult {
				database.migrateTo(t, 137)
				_, err := database.pool.Exec(
					context.Background(), `UPDATE schema_migrations SET dirty=TRUE`,
				)
				NewWithT(t).Expect(err).NotTo(HaveOccurred())
				return setupResult{}
			},
			wantError: "schema version 137 is dirty",
		},
		{
			name: "absent version table with application schema", target: 138,
			setup: func(t *testing.T, database *runnerTestDatabase) setupResult {
				database.migrateTo(t, 137)
				_, err := database.pool.Exec(
					context.Background(), `DROP TABLE schema_migrations`,
				)
				NewWithT(t).Expect(err).NotTo(HaveOccurred())
				return setupResult{}
			},
			wantError: "schema_migrations is absent for an existing application schema",
		},
		{
			name: "partial execution schema", target: 138,
			setup: func(t *testing.T, database *runnerTestDatabase) setupResult {
				_, err := database.pool.Exec(
					context.Background(), `CREATE TABLE ExternalExecution(id UUID PRIMARY KEY)`,
				)
				NewWithT(t).Expect(err).NotTo(HaveOccurred())
				return setupResult{}
			},
			wantError: "partial external execution schema",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			database := newRunnerTestDatabase(t)
			result := test.setup(t, database)
			var constructed uint64
			database.runner.engineFactory = func(
				*sql.DB,
				*zap.Logger,
				string,
				time.Duration,
			) (migrationEngine, error) {
				constructed++
				return &fakeMigrationEngine{}, nil
			}
			err := database.runner.Run(context.Background(), RunOptions{
				Target:         uintPointer(test.target),
				CheckOnly:      true,
				ExpandManifest: result.manifest,
			})
			if test.wantError == "" {
				g.Expect(err).NotTo(HaveOccurred())
			} else {
				g.Expect(err).To(MatchError(ContainSubstring(test.wantError)))
			}
			g.Expect(constructed).To(Equal(uint64(0)))
		})
	}
}

func TestExternalExecutionTimestampPreflightDownStates(t *testing.T) {
	tests := []struct {
		name      string
		state     types.ExternalExecutionTimestampManifestState
		wantError string
	}{
		{"preapply down", types.ExternalExecutionTimestampManifestStateApproved, ""},
		{"applied down", types.ExternalExecutionTimestampManifestStateApplied, "downgrade crossing 138"},
		{"verified down", types.ExternalExecutionTimestampManifestStateVerified, "downgrade crossing 138"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			database := newRunnerTestDatabase(t)
			database.migrateTo(t, 137)
			manifest := approvedRunnerManifest(t, database)
			database.migrateTo(t, 138)
			insertRunnerManifest(t, database, manifest, test.state)
			var constructed uint64
			database.runner.engineFactory = func(
				*sql.DB,
				*zap.Logger,
				string,
				time.Duration,
			) (migrationEngine, error) {
				constructed++
				return &fakeMigrationEngine{}, nil
			}
			err := database.runner.Run(context.Background(), RunOptions{
				Target:    uintPointer(137),
				CheckOnly: true,
			})
			if test.wantError == "" {
				g.Expect(err).NotTo(HaveOccurred())
			} else {
				g.Expect(err).To(MatchError(ContainSubstring(test.wantError)))
			}
			g.Expect(constructed).To(Equal(uint64(0)))
		})
	}
}
