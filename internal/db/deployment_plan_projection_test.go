package db

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/gomega"
)

func TestDeploymentPlanOutputProjectionMatchesStrictStructScan(t *testing.T) {
	g := NewWithT(t)
	got := projectionColumns(deploymentPlanOutputExpr)
	want := []string{
		"id",
		"created_at",
		"sealed_at",
		"organization_id",
		"published_by_user_account_id",
		"application_id",
		"release_bundle_id",
		"channel_id",
		"environment_id",
		"deployment_unit_id",
		"effective_policy",
		"effective_policy_checksum",
		"subscriber_set_checksum",
		"process_snapshot_id",
		"variable_snapshot_id",
		"release_contract",
		"plan_schema",
		"draft_id",
		"target_config_snapshot_id",
		"protocol_version",
		"supersedes_deployment_plan_id",
		"supersede_reason",
		"previous_state_source_plan_id",
		"bootstrap",
		"status",
		"canonical_checksum",
		"canonical_payload",
	}

	g.Expect(got).To(Equal(want))
	g.Expect(got).To(ConsistOf(strictStructColumns(reflect.TypeFor[types.DeploymentPlan]())...))
}

func TestTargetConfigSnapshotOutputProjectionMatchesStrictStructScan(t *testing.T) {
	g := NewWithT(t)
	got := projectionColumns(targetConfigSnapshotOutputExpr)
	want := []string{
		"id",
		"created_at",
		"created_by_user_account_id",
		"organization_id",
		"deployment_unit_id",
		"target_environment_assignment_id",
		"environment_id",
		"source_repository",
		"source_commit",
		"source_adapter",
		"adapter_version",
		"target_platform",
		"runtime_constraints",
		"schema",
		"canonical_payload",
		"canonical_checksum",
	}

	g.Expect(got).To(Equal(want))
	g.Expect(got).To(ConsistOf(strictStructColumns(reflect.TypeFor[types.TargetConfigSnapshot]())...))
}

func TestTargetConfigSnapshotOutputProjectionScansStrictlyOnPostgres(t *testing.T) {
	databaseURL := os.Getenv("DISTR_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DISTR_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	id := uuid.New()
	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	rows, err := pool.Query(ctx, `
		SELECT `+targetConfigSnapshotOutputExpr+`
		FROM (
			SELECT
				@id::uuid AS id,
				@createdAt::timestamptz AS created_at,
				@actorID::uuid AS created_by_user_account_id,
				@organizationID::uuid AS organization_id,
				@deploymentUnitID::uuid AS deployment_unit_id,
				@assignmentID::uuid AS target_environment_assignment_id,
				@environmentID::uuid AS environment_id,
				'repository'::text AS source_repository,
				'commit'::text AS source_commit,
				'adapter'::text AS source_adapter,
				'1.0.0'::text AS adapter_version,
				'linux-amd64'::text AS target_platform,
				'{}'::jsonb AS runtime_constraints,
				'distr.target-config/v1'::text AS schema,
				convert_to('{}', 'UTF8') AS canonical_payload,
				@checksum::text AS canonical_checksum
		) AS snapshot`,
		pgx.NamedArgs{
			"id": id, "createdAt": createdAt, "actorID": uuid.New(),
			"organizationID": uuid.New(), "deploymentUnitID": uuid.New(),
			"assignmentID": uuid.New(), "environmentID": uuid.New(),
			"checksum": "sha256:" + strings.Repeat("a", 64),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.TargetConfigSnapshot],
	)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ID != id || !snapshot.CreatedAt.Equal(createdAt) {
		t.Fatalf("unexpected target config snapshot scan: %#v", snapshot)
	}
}

func projectionColumns(expression string) []string {
	lines := strings.Split(strings.TrimSpace(expression), "\n")
	columns := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), ","))
		if _, alias, found := strings.Cut(line, " AS "); found {
			columns = append(columns, strings.TrimSpace(alias))
			continue
		}
		columns = append(columns, strings.TrimPrefix(line, "dp."))
	}
	return columns
}

func strictStructColumns(structType reflect.Type) []any {
	columns := make([]any, 0, structType.NumField())
	for i := range structType.NumField() {
		name := structType.Field(i).Tag.Get("db")
		if name != "-" {
			columns = append(columns, name)
		}
	}
	return columns
}
