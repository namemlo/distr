package db_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/externalexecutiontimestamp"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/gomega"
)

func TestExternalExecutionInsertWritesTimestampPairs(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	execution := prepareExternalExecutionForTimestampWriteTest(t, ctx)

	assertExternalExecutionCreateTimestampPairs(t, ctx, execution)
}

func TestExternalExecutionTriggerWritesTimestampPairs(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	execution := prepareExternalExecutionForTimestampWriteTest(t, ctx)

	_, err := db.MarkExternalExecutionTriggered(ctx, types.MarkExternalExecutionTriggeredRequest{
		OrganizationID: execution.OrganizationID, ExternalExecutionID: execution.ID, TriggerAttempts: 1,
	})
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	assertExternalExecutionLifecyclePairs(t, ctx, execution.ID, true, false)
	stable := readExternalExecutionTimestampSnapshot(t, ctx, execution.ID)
	_, err = db.MarkExternalExecutionTriggered(ctx, types.MarkExternalExecutionTriggeredRequest{
		OrganizationID: execution.OrganizationID, ExternalExecutionID: execution.ID, TriggerAttempts: 2,
	})
	NewWithT(t).Expect(err).To(HaveOccurred())
	NewWithT(t).Expect(readExternalExecutionTimestampSnapshot(t, ctx, execution.ID)).To(Equal(stable))

	legacyStarted := time.Date(2024, time.November, 3, 1, 30, 0, 123456000, time.UTC)
	historicalCtx, historical := prepareHistoricalExternalExecutionForTimestampWriteTest(
		t, &legacyStarted, nil, time.Date(2024, time.November, 3, 2, 30, 0, 0, time.UTC),
	)
	_, err = db.MarkExternalExecutionTriggered(historicalCtx, types.MarkExternalExecutionTriggeredRequest{
		OrganizationID: historical.OrganizationID, ExternalExecutionID: historical.ID, TriggerAttempts: 1,
	})
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	assertExternalExecutionUnresolvedLifecyclePreserved(
		t, historicalCtx, historical.ID, &legacyStarted, nil,
	)
}

func TestExternalExecutionCallbackWritesTimestampPairs(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	execution := prepareExternalExecutionForTimestampWriteTest(t, ctx)

	running := types.RecordExternalExecutionCallbackRequest{
		OrganizationID: execution.OrganizationID, ExternalExecutionID: execution.ID,
		Sequence: 1, Status: types.ExternalExecutionStatusRunning, Message: "provider running",
	}
	_, err := db.RecordExternalExecutionCallback(ctx, running)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	assertExternalExecutionLifecyclePairs(t, ctx, execution.ID, true, false)
	assertLatestExternalExecutionEventClock(t, ctx, execution.ID, false)
	stable := readExternalExecutionTimestampSnapshot(t, ctx, execution.ID)
	_, err = db.RecordExternalExecutionCallback(ctx, running)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	NewWithT(t).Expect(readExternalExecutionTimestampSnapshot(t, ctx, execution.ID)).To(Equal(stable))

	_, err = db.RecordExternalExecutionCallback(ctx, types.RecordExternalExecutionCallbackRequest{
		OrganizationID: execution.OrganizationID, ExternalExecutionID: execution.ID,
		Sequence: 2, Status: types.ExternalExecutionStatusFailed, Message: "provider failed",
	})
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	assertExternalExecutionLifecyclePairs(t, ctx, execution.ID, true, true)
	assertLatestExternalExecutionEventClock(t, ctx, execution.ID, true)

	legacyStarted := time.Date(2024, time.November, 3, 1, 30, 0, 123456000, time.UTC)
	legacyCompleted := time.Date(2024, time.November, 3, 1, 45, 0, 654321000, time.UTC)
	historicalCtx, historical := prepareHistoricalExternalExecutionForTimestampWriteTest(
		t, &legacyStarted, &legacyCompleted, time.Now().UTC().Add(time.Hour),
	)
	_, err = db.RecordExternalExecutionCallback(historicalCtx, types.RecordExternalExecutionCallbackRequest{
		OrganizationID: historical.OrganizationID, ExternalExecutionID: historical.ID,
		Sequence: 1, Status: types.ExternalExecutionStatusFailed, Message: "historical provider failed",
	})
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	assertExternalExecutionUnresolvedLifecyclePreserved(
		t, historicalCtx, historical.ID, &legacyStarted, &legacyCompleted,
	)
	assertLatestExternalExecutionEventClock(t, historicalCtx, historical.ID, false)
}

func TestExternalExecutionTimeoutWritesTimestampPairs(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	execution := prepareExternalExecutionForTimestampWriteTest(t, ctx)
	setExternalExecutionDeadline(t, ctx, execution.ID, time.Now().UTC().Add(-time.Minute))

	_, err := db.TimeoutExternalExecution(ctx, types.TimeoutExternalExecutionRequest{
		OrganizationID: execution.OrganizationID, ExternalExecutionID: execution.ID, Message: "timed out",
	})
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	assertExternalExecutionLifecyclePairs(t, ctx, execution.ID, false, true)
	assertLatestExternalExecutionEventClock(t, ctx, execution.ID, true)
	stable := readExternalExecutionTimestampSnapshot(t, ctx, execution.ID)
	_, err = db.TimeoutExternalExecution(ctx, types.TimeoutExternalExecutionRequest{
		OrganizationID: execution.OrganizationID, ExternalExecutionID: execution.ID, Message: "timed out again",
	})
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	NewWithT(t).Expect(readExternalExecutionTimestampSnapshot(t, ctx, execution.ID)).To(Equal(stable))

	legacyCompleted := time.Date(2024, time.November, 3, 1, 45, 0, 654321000, time.UTC)
	historicalCtx, historical := prepareHistoricalExternalExecutionForTimestampWriteTest(
		t, nil, &legacyCompleted, time.Now().UTC().Add(-time.Minute),
	)
	_, err = db.TimeoutExternalExecution(historicalCtx, types.TimeoutExternalExecutionRequest{
		OrganizationID: historical.OrganizationID, ExternalExecutionID: historical.ID,
		Message: "historical timeout",
	})
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	assertExternalExecutionUnresolvedLifecyclePreserved(
		t, historicalCtx, historical.ID, nil, &legacyCompleted,
	)
	assertLatestExternalExecutionEventClock(t, historicalCtx, historical.ID, false)
}

func TestExternalExecutionFailureWritesTimestampPairs(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	execution := prepareExternalExecutionForTimestampWriteTest(t, ctx)

	_, err := db.FailExternalExecution(ctx, types.FailExternalExecutionRequest{
		OrganizationID: execution.OrganizationID, ExternalExecutionID: execution.ID, Message: "trigger failed",
	})
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	assertExternalExecutionLifecyclePairs(t, ctx, execution.ID, false, true)
	assertLatestExternalExecutionEventClock(t, ctx, execution.ID, true)
	stable := readExternalExecutionTimestampSnapshot(t, ctx, execution.ID)
	_, err = db.FailExternalExecution(ctx, types.FailExternalExecutionRequest{
		OrganizationID: execution.OrganizationID, ExternalExecutionID: execution.ID, Message: "trigger failed again",
	})
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	NewWithT(t).Expect(readExternalExecutionTimestampSnapshot(t, ctx, execution.ID)).To(Equal(stable))

	legacyCompleted := time.Date(2024, time.November, 3, 1, 45, 0, 654321000, time.UTC)
	historicalCtx, historical := prepareHistoricalExternalExecutionForTimestampWriteTest(
		t, nil, &legacyCompleted, time.Date(2024, time.November, 3, 2, 30, 0, 0, time.UTC),
	)
	_, err = db.FailExternalExecution(historicalCtx, types.FailExternalExecutionRequest{
		OrganizationID: historical.OrganizationID, ExternalExecutionID: historical.ID,
		Message: "historical trigger failed",
	})
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	assertExternalExecutionUnresolvedLifecyclePreserved(
		t, historicalCtx, historical.ID, nil, &legacyCompleted,
	)
	assertLatestExternalExecutionEventClock(t, historicalCtx, historical.ID, false)
}

func TestExternalExecutionTimestampWritesIgnoreSessionAndHostTimezone(t *testing.T) {
	for _, databaseZone := range []string{
		"UTC", "Asia/Bangkok", "America/New_York",
	} {
		for _, applicationZone := range []*time.Location{
			time.UTC,
			time.FixedZone("host-plus-seven", 7*60*60),
			time.FixedZone("host-minus-five", -5*60*60),
		} {
			t.Run(databaseZone+"/"+applicationZone.String(), func(t *testing.T) {
				previous := time.Local
				time.Local = applicationZone
				t.Cleanup(func() { time.Local = previous })
				ctx := externalExecutionContextWithSessionZone(t, databaseZone)
				assertAllExternalTimestampWritePathsArePaired(t, ctx)
			})
		}
	}
}

func TestExternalExecutionLegacyReadsIgnoreInstantShadows(t *testing.T) {
	g := NewWithT(t)
	legacyCreated := time.Date(2023, 7, 8, 9, 10, 11, 123456000, time.UTC)
	legacyUpdated := legacyCreated.Add(time.Minute)
	legacyStarted := legacyCreated.Add(2 * time.Minute)
	legacyCompleted := legacyCreated.Add(3 * time.Minute)
	legacyDeadline := legacyCreated.Add(4 * time.Minute)
	legacyEvent := legacyCreated.Add(5 * time.Minute)
	database := newTask4TestDatabase(t, 137, "UTC")
	task4DropFixtureForeignKeys(t, database.pool)
	executionID := uuid.New()
	task4InsertExecution(
		t,
		database.pool,
		executionID,
		formatLegacyTimestamp(legacyCreated),
		formatLegacyTimestamp(legacyUpdated),
		ptr(formatLegacyTimestamp(legacyStarted)),
		ptr(formatLegacyTimestamp(legacyCompleted)),
		formatLegacyTimestamp(legacyDeadline),
	)
	var organizationID uuid.UUID
	g.Expect(database.pool.QueryRow(context.Background(), `
SELECT organization_id FROM ExternalExecution WHERE id=$1`, executionID).Scan(&organizationID)).To(Succeed())
	_, err := database.pool.Exec(context.Background(), `
INSERT INTO ExternalExecutionEvent (
 id, created_at, organization_id, external_execution_id,
 sequence, status, payload_hash
) VALUES ($1, $2::timestamp, $3, $4, 1, 'FAILED', 'sha256:' || repeat('d', 64))`,
		uuid.New(), formatLegacyTimestamp(legacyEvent), organizationID, executionID)
	g.Expect(err).NotTo(HaveOccurred())
	draft, err := db.InspectExternalExecutionTimestamps(database.ctx)
	g.Expect(err).NotTo(HaveOccurred())
	manifest := task5ApproveTimestampManifest(t, *draft, int(draft.PopulatedCellCount))
	offset := int32(7 * 60 * 60)
	for index := range manifest.Cells {
		cell := &manifest.Cells[index]
		if cell.RawValue == nil {
			continue
		}
		converted, convertErr := externalexecutiontimestamp.ConvertWallClock(
			*cell.RawValue,
			offset,
		)
		g.Expect(convertErr).NotTo(HaveOccurred())
		convertedValue := externalexecutiontimestamp.FormatInstant(converted)
		cell.SourceZone = "Asia/Bangkok"
		cell.SourceOffsetSeconds = &offset
		cell.ConvertedValue = &convertedValue
	}
	task5RefreshDecisionChecksum(t, &manifest)
	database.migrateTo(t)
	_, err = db.ApplyExternalExecutionTimestampManifest(
		database.ctx,
		task5ApplyRequest(manifest),
	)
	g.Expect(err).NotTo(HaveOccurred())
	ctx := database.ctx

	var shadowCreated, shadowEvent time.Time
	g.Expect(database.pool.QueryRow(context.Background(), `
SELECT created_at_instant FROM ExternalExecution WHERE id=$1`, executionID).Scan(
		&shadowCreated,
	)).To(Succeed())
	g.Expect(database.pool.QueryRow(context.Background(), `
SELECT created_at_instant FROM ExternalExecutionEvent
WHERE external_execution_id=$1`, executionID).Scan(&shadowEvent)).To(Succeed())
	g.Expect(shadowCreated.UTC()).NotTo(Equal(legacyCreated))
	g.Expect(shadowEvent.UTC()).NotTo(Equal(legacyEvent))

	readExecution, err := db.GetExternalExecution(ctx, executionID, organizationID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(readExecution.CreatedAt).To(Equal(legacyCreated))
	g.Expect(readExecution.UpdatedAt).To(Equal(legacyUpdated))
	g.Expect(readExecution.StartedAt).NotTo(BeNil())
	g.Expect(*readExecution.StartedAt).To(Equal(legacyStarted))
	g.Expect(readExecution.CompletedAt).NotTo(BeNil())
	g.Expect(*readExecution.CompletedAt).To(Equal(legacyCompleted))
	g.Expect(readExecution.CallbackDeadlineAt).To(Equal(legacyDeadline))
	encoded, err := json.Marshal(readExecution)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(encoded)).NotTo(ContainSubstring("Instant"))

	events, err := db.GetExternalExecutionEvents(ctx, executionID, organizationID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(events).To(HaveLen(1))
	g.Expect(events[0].CreatedAt).To(Equal(legacyEvent))
	encoded, err = json.Marshal(events)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(encoded)).NotTo(ContainSubstring("Instant"))
}

func prepareExternalExecutionForTimestampWriteTest(
	t *testing.T,
	ctx context.Context,
) *types.ExternalExecution {
	t.Helper()
	g := NewWithT(t)
	deps := createExternalExecutionPlan(t, ctx)
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID: deps.orgID, DeploymentPlanID: deps.plan.ID, ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(tasks).To(HaveLen(1))
	lease, err := db.LeaseHubTask(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(lease).NotTo(BeNil())
	g.Expect(lease.TaskID).To(Equal(tasks[0].ID))
	execution, err := db.PrepareExternalExecution(ctx, types.PrepareExternalExecutionRequest{
		OrganizationID: deps.orgID, StepRunID: lease.Steps[0].StepRunID,
		Component: "api-image", CallbackTimeoutSeconds: 600,
	})
	g.Expect(err).NotTo(HaveOccurred())
	return execution
}

func assertExternalExecutionCreateTimestampPairs(
	t *testing.T,
	ctx context.Context,
	execution *types.ExternalExecution,
) {
	t.Helper()
	g := NewWithT(t)
	var createdPair, updatedPair, deadlinePair, oneClock, deadlineMatchesInput bool
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
SELECT
 created_at IS NOT DISTINCT FROM created_at_instant AT TIME ZONE 'UTC',
 updated_at IS NOT DISTINCT FROM updated_at_instant AT TIME ZONE 'UTC',
 callback_deadline_at IS NOT DISTINCT FROM
   callback_deadline_at_instant AT TIME ZONE 'UTC',
 created_at_instant = updated_at_instant,
 callback_deadline_at_instant = CAST(@deadline AS timestamptz)
FROM ExternalExecution WHERE id=@id`,
		pgx.NamedArgs{"id": execution.ID, "deadline": execution.CallbackDeadlineAt.UTC()},
	).Scan(&createdPair, &updatedPair, &deadlinePair, &oneClock, &deadlineMatchesInput)).To(Succeed())
	g.Expect([]bool{createdPair, updatedPair, deadlinePair, oneClock, deadlineMatchesInput}).
		To(Equal([]bool{true, true, true, true, true}))
}

func assertExternalExecutionLifecyclePairs(
	t *testing.T,
	ctx context.Context,
	executionID uuid.UUID,
	expectStarted bool,
	expectCompleted bool,
) {
	t.Helper()
	g := NewWithT(t)
	var updatedPair, startedPair, completedPair bool
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
SELECT
 updated_at IS NOT DISTINCT FROM updated_at_instant AT TIME ZONE 'UTC',
 CASE WHEN @expectStarted
   THEN started_at IS NOT NULL AND started_at_instant IS NOT NULL AND
        started_at = started_at_instant AT TIME ZONE 'UTC'
   ELSE started_at IS NULL AND started_at_instant IS NULL END,
 CASE WHEN @expectCompleted
   THEN completed_at IS NOT NULL AND completed_at_instant IS NOT NULL AND
        completed_at = completed_at_instant AT TIME ZONE 'UTC'
   ELSE completed_at IS NULL AND completed_at_instant IS NULL END
FROM ExternalExecution WHERE id=@id`, pgx.NamedArgs{
		"id": executionID, "expectStarted": expectStarted, "expectCompleted": expectCompleted,
	}).Scan(&updatedPair, &startedPair, &completedPair)).To(Succeed())
	g.Expect([]bool{updatedPair, startedPair, completedPair}).To(Equal([]bool{true, true, true}))
}

func assertLatestExternalExecutionEventClock(
	t *testing.T,
	ctx context.Context,
	executionID uuid.UUID,
	expectTerminalMatch bool,
) {
	t.Helper()
	g := NewWithT(t)
	var eventPair, eventMatchesUpdated, terminalMatchesCompleted bool
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
SELECT
 event.created_at = event.created_at_instant AT TIME ZONE 'UTC',
 event.created_at_instant = execution.updated_at_instant,
 CASE WHEN @expectTerminalMatch
   THEN event.created_at_instant = execution.completed_at_instant
   ELSE execution.completed_at_instant IS NULL END
FROM ExternalExecution execution
JOIN ExternalExecutionEvent event
 ON event.external_execution_id=execution.id
WHERE execution.id=@id
ORDER BY event.sequence DESC LIMIT 1`, pgx.NamedArgs{
		"id": executionID, "expectTerminalMatch": expectTerminalMatch,
	}).Scan(&eventPair, &eventMatchesUpdated, &terminalMatchesCompleted)).To(Succeed())
	g.Expect([]bool{eventPair, eventMatchesUpdated, terminalMatchesCompleted}).
		To(Equal([]bool{true, true, true}))
}

func prepareHistoricalExternalExecutionForTimestampWriteTest(
	t *testing.T,
	startedAt *time.Time,
	completedAt *time.Time,
	deadline time.Time,
) (context.Context, *types.ExternalExecution) {
	t.Helper()
	database := newTask4TestDatabase(t, 137, "UTC")
	task4DropFixtureForeignKeys(t, database.pool)
	executionID := uuid.New()
	createdAt := time.Date(2024, time.November, 3, 1, 0, 0, 111111000, time.UTC)
	updatedAt := createdAt.Add(time.Minute)
	task4InsertExecution(
		t,
		database.pool,
		executionID,
		formatLegacyTimestamp(createdAt),
		formatLegacyTimestamp(updatedAt),
		formatOptionalLegacyTimestamp(startedAt),
		formatOptionalLegacyTimestamp(completedAt),
		formatLegacyTimestamp(deadline),
	)
	database.migrateTo(t)
	var organizationID uuid.UUID
	g := NewWithT(t)
	g.Expect(database.pool.QueryRow(context.Background(), `
SELECT organization_id FROM ExternalExecution WHERE id=$1`, executionID).Scan(&organizationID)).To(Succeed())
	execution, err := db.GetExternalExecution(database.ctx, executionID, organizationID)
	g.Expect(err).NotTo(HaveOccurred())
	return database.ctx, execution
}

func formatLegacyTimestamp(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000")
}

func formatOptionalLegacyTimestamp(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := formatLegacyTimestamp(*value)
	return &formatted
}

type externalExecutionTimestampSnapshot struct {
	CreatedAt                 time.Time
	CreatedAtInstant          *time.Time
	UpdatedAt                 time.Time
	UpdatedAtInstant          *time.Time
	StartedAt                 *time.Time
	StartedAtInstant          *time.Time
	CompletedAt               *time.Time
	CompletedAtInstant        *time.Time
	CallbackDeadlineAt        time.Time
	CallbackDeadlineAtInstant *time.Time
	EventCount                int64
}

func readExternalExecutionTimestampSnapshot(
	t *testing.T,
	ctx context.Context,
	executionID uuid.UUID,
) externalExecutionTimestampSnapshot {
	t.Helper()
	var snapshot externalExecutionTimestampSnapshot
	NewWithT(t).Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
SELECT
 execution.created_at,
 execution.created_at_instant,
 execution.updated_at,
 execution.updated_at_instant,
 execution.started_at,
 execution.started_at_instant,
 execution.completed_at,
 execution.completed_at_instant,
 execution.callback_deadline_at,
 execution.callback_deadline_at_instant,
 (SELECT count(*) FROM ExternalExecutionEvent event
   WHERE event.external_execution_id=execution.id)
FROM ExternalExecution execution
WHERE execution.id=@id`, pgx.NamedArgs{"id": executionID}).Scan(
		&snapshot.CreatedAt,
		&snapshot.CreatedAtInstant,
		&snapshot.UpdatedAt,
		&snapshot.UpdatedAtInstant,
		&snapshot.StartedAt,
		&snapshot.StartedAtInstant,
		&snapshot.CompletedAt,
		&snapshot.CompletedAtInstant,
		&snapshot.CallbackDeadlineAt,
		&snapshot.CallbackDeadlineAtInstant,
		&snapshot.EventCount,
	)).To(Succeed())
	return snapshot
}

func assertExternalExecutionUnresolvedLifecyclePreserved(
	t *testing.T,
	ctx context.Context,
	executionID uuid.UUID,
	startedAt *time.Time,
	completedAt *time.Time,
) {
	t.Helper()
	g := NewWithT(t)
	var startedPreserved, completedPreserved, updatedPair bool
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
SELECT
 CASE WHEN @startedAt::timestamp IS NULL
   THEN started_at IS NULL AND started_at_instant IS NULL
   ELSE started_at = @startedAt::timestamp AND started_at_instant IS NULL END,
 CASE WHEN @completedAt::timestamp IS NULL
   THEN completed_at IS NULL AND completed_at_instant IS NULL
   ELSE completed_at = @completedAt::timestamp AND completed_at_instant IS NULL END,
 updated_at = updated_at_instant AT TIME ZONE 'UTC'
FROM ExternalExecution WHERE id=@id`, pgx.NamedArgs{
		"id": executionID, "startedAt": startedAt, "completedAt": completedAt,
	}).Scan(&startedPreserved, &completedPreserved, &updatedPair)).To(Succeed())
	g.Expect([]bool{startedPreserved, completedPreserved, updatedPair}).To(Equal([]bool{true, true, true}))
}

func externalExecutionContextWithSessionZone(t *testing.T, databaseZone string) context.Context {
	t.Helper()
	ctx := taskQueueDBTestContext(t)
	pool, ok := internalctx.GetDb(ctx).(*pgxpool.Pool)
	NewWithT(t).Expect(ok).To(BeTrue())
	connection, err := pool.Acquire(ctx)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	t.Cleanup(connection.Release)
	_, err = connection.Exec(ctx, `SELECT set_config('TimeZone', $1, false)`, databaseZone)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return internalctx.WithDb(ctx, connection)
}

func assertAllExternalTimestampWritePathsArePaired(t *testing.T, ctx context.Context) {
	t.Helper()

	triggered := prepareExternalExecutionForTimestampWriteTest(t, ctx)
	assertExternalExecutionCreateTimestampPairs(t, ctx, triggered)
	_, err := db.MarkExternalExecutionTriggered(ctx, types.MarkExternalExecutionTriggeredRequest{
		OrganizationID: triggered.OrganizationID, ExternalExecutionID: triggered.ID, TriggerAttempts: 1,
	})
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	assertExternalExecutionLifecyclePairs(t, ctx, triggered.ID, true, false)

	callback := prepareExternalExecutionForTimestampWriteTest(t, ctx)
	_, err = db.RecordExternalExecutionCallback(ctx, types.RecordExternalExecutionCallbackRequest{
		OrganizationID: callback.OrganizationID, ExternalExecutionID: callback.ID,
		Sequence: 1, Status: types.ExternalExecutionStatusFailed, Message: "matrix callback",
	})
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	assertExternalExecutionLifecyclePairs(t, ctx, callback.ID, true, true)
	assertLatestExternalExecutionEventClock(t, ctx, callback.ID, true)

	timedOut := prepareExternalExecutionForTimestampWriteTest(t, ctx)
	setExternalExecutionDeadline(t, ctx, timedOut.ID, time.Now().UTC().Add(-time.Minute))
	_, err = db.TimeoutExternalExecution(ctx, types.TimeoutExternalExecutionRequest{
		OrganizationID: timedOut.OrganizationID, ExternalExecutionID: timedOut.ID, Message: "matrix timeout",
	})
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	assertExternalExecutionLifecyclePairs(t, ctx, timedOut.ID, false, true)
	assertLatestExternalExecutionEventClock(t, ctx, timedOut.ID, true)

	failed := prepareExternalExecutionForTimestampWriteTest(t, ctx)
	_, err = db.FailExternalExecution(ctx, types.FailExternalExecutionRequest{
		OrganizationID: failed.OrganizationID, ExternalExecutionID: failed.ID, Message: "matrix failure",
	})
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	assertExternalExecutionLifecyclePairs(t, ctx, failed.ID, false, true)
	assertLatestExternalExecutionEventClock(t, ctx, failed.ID, true)
}
