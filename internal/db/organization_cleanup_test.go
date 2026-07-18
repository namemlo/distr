package db_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/gomega"
)

type organizationCleanupTopology struct {
	OrganizationID   uuid.UUID
	DeploymentPlanID uuid.UUID
	TaskID           uuid.UUID
	StepRunID        uuid.UUID
	ExecutionID      uuid.UUID
	EventID          uuid.UUID
}

func TestDeleteOrganizationsOlderThanPurgesModernExecutionTopology(t *testing.T) {
	database := newTask4TestDatabase(t, 138, "UTC")
	old := time.Now().UTC().Add(-2 * time.Hour)
	eligible := insertOrganizationCleanupTopology(
		t,
		database,
		"organization-cleanup-eligible",
		&old,
	)
	retained := insertOrganizationCleanupTopology(
		t,
		database,
		"organization-cleanup-retained",
		nil,
	)

	deletedCount, err := db.DeleteOrganizationsOlderThan(database.ctx, time.Hour)

	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(deletedCount).To(Equal(int64(1)))
	expectOrganizationCleanupTopologyCount(t, database, eligible, 0)
	expectOrganizationCleanupTopologyCount(t, database, retained, 1)

	var tombstoneRows, operationCount int64
	var operationIDText string
	err = database.pool.QueryRow(context.Background(), `
SELECT
  count(*),
  count(DISTINCT deletion_operation_id),
  min(deletion_operation_id::text)
FROM ExternalExecutionTimestampDeletionTombstone
WHERE source_row_id IN ($1, $2)`,
		eligible.ExecutionID,
		eligible.EventID,
	).Scan(&tombstoneRows, &operationCount, &operationIDText)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(tombstoneRows).To(Equal(int64(6)))
	g.Expect(operationCount).To(Equal(int64(1)))
	operationID, err := uuid.Parse(operationIDText)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(operationID).NotTo(Equal(uuid.Nil))
}

func TestDeleteOrganizationsOlderThanLocksEligibleOrganizationsBeforeChildDeletion(
	t *testing.T,
) {
	database := newTask4TestDatabase(t, 138, "UTC")
	old := time.Now().UTC().Add(-2 * time.Hour)
	eligible := insertOrganizationCleanupTopology(
		t,
		database,
		"organization-cleanup-lock-order",
		&old,
	)
	barrier := &organizationCleanupBarrierPool{
		Pool:              database.pool,
		taskDeleteStarted: make(chan struct{}),
		allowTaskDelete:   make(chan struct{}),
	}
	ctx := internalctx.WithDb(context.Background(), barrier)
	var releaseOnce sync.Once
	releaseTaskDelete := func() {
		releaseOnce.Do(func() { close(barrier.allowTaskDelete) })
	}
	defer releaseTaskDelete()

	type purgeResult struct {
		count int64
		err   error
	}
	purgeResults := make(chan purgeResult, 1)
	go func() {
		count, err := db.DeleteOrganizationsOlderThan(ctx, time.Hour)
		purgeResults <- purgeResult{count: count, err: err}
	}()

	select {
	case <-barrier.taskDeleteStarted:
	case result := <-purgeResults:
		t.Fatalf("purge completed before the Task deletion barrier: %v", result.err)
	case <-time.After(10 * time.Second):
		t.Fatal("purge did not reach the Task deletion barrier")
	}
	NewWithT(t).Expect(barrier.organizationLockQuerySeen.Load()).To(BeTrue())

	connection, err := database.pool.Acquire(context.Background())
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	defer connection.Release()
	var backendPID int32
	NewWithT(t).Expect(connection.QueryRow(
		context.Background(),
		`SELECT pg_backend_pid()`,
	).Scan(&backendPID)).To(Succeed())

	insertCtx, cancelInsert := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelInsert()
	insertResults := make(chan error, 1)
	go func() {
		_, insertErr := connection.Exec(insertCtx, `
INSERT INTO Application (id, name, type, organization_id)
VALUES ($1, 'organization-cleanup-concurrent-child', 'docker', $2)
/* organization_cleanup_concurrent_fk */`,
			uuid.New(),
			eligible.OrganizationID,
		)
		insertResults <- insertErr
	}()

	waitDeadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case insertErr := <-insertResults:
			t.Fatalf("concurrent child insert was not fenced by the organization lock: %v", insertErr)
		default:
		}
		var waitEventType string
		var markedQuery bool
		err = database.pool.QueryRow(context.Background(), `
SELECT
  COALESCE(wait_event_type, ''),
  query LIKE '%organization_cleanup_concurrent_fk%'
FROM pg_stat_activity
WHERE pid=$1`,
			backendPID,
		).Scan(&waitEventType, &markedQuery)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		if waitEventType == "Lock" && markedQuery {
			break
		}
		if time.Now().After(waitDeadline) {
			t.Fatalf(
				"concurrent child insert did not enter a lock wait; wait_event_type=%q marked_query=%t",
				waitEventType,
				markedQuery,
			)
		}
		time.Sleep(10 * time.Millisecond)
	}

	releaseTaskDelete()
	var purged purgeResult
	select {
	case purged = <-purgeResults:
	case <-time.After(10 * time.Second):
		t.Fatal("purge did not complete after releasing the Task deletion barrier")
	}
	g := NewWithT(t)
	g.Expect(purged.err).NotTo(HaveOccurred())
	g.Expect(purged.count).To(Equal(int64(1)))

	var insertErr error
	select {
	case insertErr = <-insertResults:
	case <-time.After(10 * time.Second):
		t.Fatal("concurrent child insert did not complete after the purge")
	}
	var postgresError *pgconn.PgError
	g.Expect(errors.As(insertErr, &postgresError)).To(BeTrue())
	g.Expect(postgresError.Code).To(Equal(pgerrcode.ForeignKeyViolation))
}

type organizationCleanupBarrierPool struct {
	*pgxpool.Pool
	taskDeleteStarted         chan struct{}
	allowTaskDelete           chan struct{}
	taskDeleteStartedOnce     sync.Once
	organizationLockQuerySeen atomic.Bool
}

func (pool *organizationCleanupBarrierPool) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := pool.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &organizationCleanupBarrierTx{
		Tx:                        tx,
		taskDeleteStarted:         pool.taskDeleteStarted,
		allowTaskDelete:           pool.allowTaskDelete,
		taskDeleteStartedOnce:     &pool.taskDeleteStartedOnce,
		organizationLockQuerySeen: &pool.organizationLockQuerySeen,
	}, nil
}

type organizationCleanupBarrierTx struct {
	pgx.Tx
	taskDeleteStarted         chan struct{}
	allowTaskDelete           chan struct{}
	taskDeleteStartedOnce     *sync.Once
	organizationLockQuerySeen *atomic.Bool
}

func (tx *organizationCleanupBarrierTx) Query(
	ctx context.Context,
	sql string,
	args ...any,
) (pgx.Rows, error) {
	rows, err := tx.Tx.Query(ctx, sql, args...)
	normalized := strings.Join(strings.Fields(strings.ToUpper(sql)), " ")
	if err == nil &&
		strings.Contains(normalized, "SELECT ID FROM ORGANIZATION") &&
		strings.Contains(normalized, "ORDER BY ID FOR UPDATE") {
		tx.organizationLockQuerySeen.Store(true)
	}
	return rows, err
}

func (tx *organizationCleanupBarrierTx) Exec(
	ctx context.Context,
	sql string,
	arguments ...any,
) (pgconn.CommandTag, error) {
	normalized := strings.Join(strings.Fields(strings.ToUpper(sql)), " ")
	if strings.Contains(normalized, "DELETE FROM TASK") {
		if !tx.organizationLockQuerySeen.Load() {
			return pgconn.CommandTag{}, errors.New(
				"Task deletion started before eligible Organization rows were locked",
			)
		}
		tx.taskDeleteStartedOnce.Do(func() { close(tx.taskDeleteStarted) })
		select {
		case <-tx.allowTaskDelete:
		case <-ctx.Done():
			return pgconn.CommandTag{}, ctx.Err()
		}
	}
	return tx.Tx.Exec(ctx, sql, arguments...)
}

func insertOrganizationCleanupTopology(
	t *testing.T,
	database *task4TestDatabase,
	name string,
	deletedAt *time.Time,
) organizationCleanupTopology {
	t.Helper()
	ids := struct {
		organization, application, target, environment, lifecycle  uuid.UUID
		channel, bundle, plan, planTarget, planStep, task, stepRun uuid.UUID
		execution, event                                           uuid.UUID
	}{
		organization: uuid.New(),
		application:  uuid.New(),
		target:       uuid.New(),
		environment:  uuid.New(),
		lifecycle:    uuid.New(),
		channel:      uuid.New(),
		bundle:       uuid.New(),
		plan:         uuid.New(),
		planTarget:   uuid.New(),
		planStep:     uuid.New(),
		task:         uuid.New(),
		stepRun:      uuid.New(),
		execution:    uuid.New(),
		event:        uuid.New(),
	}
	tx, err := database.pool.Begin(context.Background())
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	defer func() { _ = tx.Rollback(context.Background()) }()
	exec := func(statement string, arguments ...any) {
		t.Helper()
		_, execErr := tx.Exec(context.Background(), statement, arguments...)
		NewWithT(t).Expect(execErr).NotTo(HaveOccurred())
	}
	exec(
		`INSERT INTO Organization (id, name, deleted_at) VALUES ($1, $2, $3)`,
		ids.organization,
		name,
		deletedAt,
	)
	exec(`
INSERT INTO Application (id, name, type, organization_id)
VALUES ($1, $2, 'docker', $3)`,
		ids.application,
		name+"-app",
		ids.organization,
	)
	exec(`
INSERT INTO DeploymentTarget (
  id, name, type, organization_id, agent_version_id
) VALUES ($1, $2, 'docker', $3, (SELECT id FROM AgentVersion LIMIT 1))`,
		ids.target,
		name+"-target",
		ids.organization,
	)
	exec(`
INSERT INTO Environment (id, organization_id, name)
VALUES ($1, $2, $3)`,
		ids.environment,
		ids.organization,
		name+"-environment",
	)
	exec(`
INSERT INTO Lifecycle (id, organization_id, name)
VALUES ($1, $2, $3)`,
		ids.lifecycle,
		ids.organization,
		name+"-lifecycle",
	)
	exec(`
INSERT INTO Channel (
  id, organization_id, application_id, lifecycle_id, name, is_default
) VALUES ($1, $2, $3, $4, $5, TRUE)`,
		ids.channel,
		ids.organization,
		ids.application,
		ids.lifecycle,
		name+"-channel",
	)
	exec(`
INSERT INTO ReleaseBundle (
  id, organization_id, application_id, channel_id,
  release_number, status, canonical_checksum, canonical_payload
) VALUES (
  $1, $2, $3, $4, '1.0.0', 'PUBLISHED',
  'sha256:' || repeat('a', 64), convert_to('{}', 'UTF8')
)`,
		ids.bundle,
		ids.organization,
		ids.application,
		ids.channel,
	)
	exec(`
INSERT INTO DeploymentPlan (
  id, organization_id, release_bundle_id, application_id,
  channel_id, environment_id, status, canonical_checksum,
  canonical_payload
) VALUES (
  $1, $2, $3, $4, $5, $6, 'EXECUTED',
  'sha256:' || repeat('b', 64), convert_to('{}', 'UTF8')
)`,
		ids.plan,
		ids.organization,
		ids.bundle,
		ids.application,
		ids.channel,
		ids.environment,
	)
	exec(`
INSERT INTO DeploymentPlanTarget (
  id, deployment_plan_id, organization_id, deployment_target_id,
  name, type, sort_order
) VALUES ($1, $2, $3, $4, $5, 'docker', 0)`,
		ids.planTarget,
		ids.plan,
		ids.organization,
		ids.target,
		name+"-plan-target",
	)
	exec(`
INSERT INTO DeploymentPlanStep (
  id, deployment_plan_id, organization_id, step_key, name,
  action_type, action_name, execution_location, sort_order
) VALUES (
  $1, $2, $3, 'deploy', $4, 'external', 'deploy',
  'server', 0
)`,
		ids.planStep,
		ids.plan,
		ids.organization,
		name+"-plan-step",
	)
	exec(`
INSERT INTO Task (
  id, organization_id, deployment_plan_id, deployment_plan_target_id,
  deployment_target_id, application_id, release_bundle_id,
  channel_id, environment_id, status
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, 'SUCCEEDED'
)`,
		ids.task,
		ids.organization,
		ids.plan,
		ids.planTarget,
		ids.target,
		ids.application,
		ids.bundle,
		ids.channel,
		ids.environment,
	)
	exec(`
INSERT INTO StepRun (
  id, organization_id, task_id, deployment_plan_id,
  deployment_plan_step_id, step_key, name, action_type,
  status, sort_order
) VALUES (
  $1, $2, $3, $4, $5, 'deploy', $6, 'external',
  'SUCCEEDED', 0
)`,
		ids.stepRun,
		ids.organization,
		ids.task,
		ids.plan,
		ids.planStep,
		name+"-step-run",
	)
	created := time.Now().UTC().Truncate(time.Microsecond)
	updated := created.Add(time.Second)
	started := updated.Add(time.Second)
	completed := started.Add(time.Second)
	deadline := completed.Add(time.Second)
	eventCreated := deadline.Add(time.Second)
	exec(`
INSERT INTO ExternalExecution (
  id,
  created_at, created_at_instant,
  updated_at, updated_at_instant,
  started_at, started_at_instant,
  completed_at, completed_at_instant,
  callback_deadline_at, callback_deadline_at_instant,
  organization_id, step_run_id, task_id, deployment_plan_id,
  deployment_plan_target_id, deployment_target_id, application_id,
  release_bundle_id, component, plan_checksum, idempotency_key,
  expected_state_version, expected_version, expected_image, expected_platform,
  expected_config_reference, expected_config_checksum, status
) VALUES (
  $1,
  $2::timestamp, $2::timestamptz,
  $3::timestamp, $3::timestamptz,
  $4::timestamp, $4::timestamptz,
  $5::timestamp, $5::timestamptz,
  $6::timestamp, $6::timestamptz,
  $7, $8, $9, $10, $11, $12, $13, $14,
  'api-image', 'sha256:' || repeat('c', 64), $15,
  0, '1.0.0', 'repo/image@sha256:' || repeat('d', 64), 'linux/amd64',
  'config:organization-cleanup', 'sha256:' || repeat('e', 64), 'SUCCEEDED'
)`,
		ids.execution,
		created,
		updated,
		started,
		completed,
		deadline,
		ids.organization,
		ids.stepRun,
		ids.task,
		ids.plan,
		ids.planTarget,
		ids.target,
		ids.application,
		ids.bundle,
		"organization-cleanup-"+ids.execution.String(),
	)
	exec(`
INSERT INTO ExternalExecutionEvent (
  id, created_at, created_at_instant, organization_id,
  external_execution_id, sequence, status, payload_hash
) VALUES (
  $1, $2::timestamp, $2::timestamptz, $3,
  $4, 1, 'SUCCEEDED', 'sha256:' || repeat('f', 64)
)`,
		ids.event,
		eventCreated,
		ids.organization,
		ids.execution,
	)
	NewWithT(t).Expect(tx.Commit(context.Background())).To(Succeed())
	return organizationCleanupTopology{
		OrganizationID:   ids.organization,
		DeploymentPlanID: ids.plan,
		TaskID:           ids.task,
		StepRunID:        ids.stepRun,
		ExecutionID:      ids.execution,
		EventID:          ids.event,
	}
}

func expectOrganizationCleanupTopologyCount(
	t *testing.T,
	database *task4TestDatabase,
	fixture organizationCleanupTopology,
	expected int64,
) {
	t.Helper()
	var organizationCount, planCount, taskCount int64
	var stepRunCount, executionCount, eventCount int64
	err := database.pool.QueryRow(context.Background(), `
SELECT
  (SELECT count(*) FROM Organization WHERE id=$1),
  (SELECT count(*) FROM DeploymentPlan WHERE id=$2),
  (SELECT count(*) FROM Task WHERE id=$3),
  (SELECT count(*) FROM StepRun WHERE id=$4),
  (SELECT count(*) FROM ExternalExecution WHERE id=$5),
  (SELECT count(*) FROM ExternalExecutionEvent WHERE id=$6)`,
		fixture.OrganizationID,
		fixture.DeploymentPlanID,
		fixture.TaskID,
		fixture.StepRunID,
		fixture.ExecutionID,
		fixture.EventID,
	).Scan(
		&organizationCount,
		&planCount,
		&taskCount,
		&stepRunCount,
		&executionCount,
		&eventCount,
	)
	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect([]int64{
		organizationCount,
		planCount,
		taskCount,
		stepRunCount,
		executionCount,
		eventCount,
	}).To(Equal([]int64{expected, expected, expected, expected, expected, expected}))
}
