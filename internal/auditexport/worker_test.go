package auditexport

import (
	"context"
	"errors"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

func TestAuditExportBatchRetriesInOrderAndIsIdempotent(t *testing.T) {
	t.Parallel()

	sinkID := uuid.New()
	store := &memoryExportStore{
		events: []types.ControlPlaneAuditEvent{
			{ID: uuid.New(), Sequence: 1},
			{ID: uuid.New(), Sequence: 2},
		},
	}
	sink := &recordingSink{}
	worker := NewWorker(store, func(context.Context, uuid.UUID) (Sink, error) { return sink, nil })

	first, err := worker.ExportAuditBatch(context.Background(), sinkID, 10)
	if err != nil {
		t.Fatalf("ExportAuditBatch() error = %v", err)
	}
	second, err := worker.ExportAuditBatch(context.Background(), sinkID, 10)
	if err != nil {
		t.Fatalf("ExportAuditBatch() replay error = %v", err)
	}

	if first.Exported != 2 || second.Exported != 0 {
		t.Fatalf("unexpected export counts: first=%#v second=%#v", first, second)
	}
	if len(sink.sequences) != 2 || sink.sequences[0] != 1 || sink.sequences[1] != 2 {
		t.Fatalf("events exported out of order or duplicated: %v", sink.sequences)
	}
	if len(store.events) != 2 {
		t.Fatalf("primary events were deleted: %d", len(store.events))
	}
}

func TestAuditExportBatchRecordsFailureWithoutAdvancingCheckpoint(t *testing.T) {
	t.Parallel()

	sinkID := uuid.New()
	store := &memoryExportStore{events: []types.ControlPlaneAuditEvent{{ID: uuid.New(), Sequence: 1}}}
	sink := &recordingSink{err: errors.New("sink unavailable")}
	worker := NewWorker(store, func(context.Context, uuid.UUID) (Sink, error) { return sink, nil })

	result, err := worker.ExportAuditBatch(context.Background(), sinkID, 10)
	if err == nil {
		t.Fatal("ExportAuditBatch() expected sink failure")
	}
	if result.Exported != 0 || store.checkpoint != 0 || store.failures != 1 {
		t.Fatalf("failure changed durable order state: result=%#v checkpoint=%d failures=%d",
			result, store.checkpoint, store.failures)
	}
	if result.CheckpointLag != 1 {
		t.Fatalf("failure lag = %d, want 1", result.CheckpointLag)
	}
	if len(store.events) != 1 {
		t.Fatal("primary event was removed after export failure")
	}
}

func TestAuditExportBatchRecordsSinkResolutionFailureAsAttempt(t *testing.T) {
	t.Parallel()

	sinkID := uuid.New()
	store := &memoryExportStore{events: []types.ControlPlaneAuditEvent{{ID: uuid.New(), Sequence: 1}}}
	worker := NewWorker(store, func(context.Context, uuid.UUID) (Sink, error) {
		return nil, errors.New("resolver unavailable")
	})

	result, err := worker.ExportAuditBatch(context.Background(), sinkID, 10)
	if err == nil {
		t.Fatal("ExportAuditBatch() expected resolver failure")
	}
	if store.started != 1 || store.failures != 1 || store.checkpoint != 0 {
		t.Fatalf("resolver failure history = started:%d failures:%d checkpoint:%d",
			store.started, store.failures, store.checkpoint)
	}
	if result.CheckpointLag != 1 || len(store.events) != 1 {
		t.Fatalf("resolver failure lost lag or source event: result=%#v events=%d", result, len(store.events))
	}
}

func TestAuditExportBatchRetainsFailedAttemptWhenRetrySucceeds(t *testing.T) {
	t.Parallel()

	sinkID := uuid.New()
	store := &memoryExportStore{events: []types.ControlPlaneAuditEvent{{ID: uuid.New(), Sequence: 1}}}
	sink := &failOnceSink{}
	worker := NewWorker(store, func(context.Context, uuid.UUID) (Sink, error) { return sink, nil })

	if _, err := worker.ExportAuditBatch(context.Background(), sinkID, 10); err == nil {
		t.Fatal("first ExportAuditBatch() expected failure")
	}
	if _, err := worker.ExportAuditBatch(context.Background(), sinkID, 10); err != nil {
		t.Fatalf("retry ExportAuditBatch() error = %v", err)
	}
	statuses := store.attemptStatuses()
	if len(statuses) != 2 || statuses[0] != "FAILED" || statuses[1] != "SUCCEEDED" {
		t.Fatalf("retry history was overwritten: %v", statuses)
	}
}

func TestAuditExportBatchPersistsCancellationFailureWithDetachedContext(t *testing.T) {
	t.Parallel()

	sinkID := uuid.New()
	store := &memoryExportStore{
		events:            []types.ControlPlaneAuditEvent{{ID: uuid.New(), Sequence: 1}},
		honorCancellation: true,
	}
	worker := NewWorker(store, func(context.Context, uuid.UUID) (Sink, error) { return contextErrorSink{}, nil })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := worker.ExportAuditBatch(ctx, sinkID, 10); err == nil {
		t.Fatal("ExportAuditBatch() expected cancellation failure")
	}
	if store.failures != 1 || store.checkpoint != 0 {
		t.Fatalf("cancellation failure was not persisted: failures=%d checkpoint=%d", store.failures, store.checkpoint)
	}
	if statuses := store.attemptStatuses(); len(statuses) != 1 || statuses[0] != "FAILED" {
		t.Fatalf("cancelled attempt remained running: %v", statuses)
	}
}

func TestAuditExportBatchPersistsCommitFailure(t *testing.T) {
	t.Parallel()

	sinkID := uuid.New()
	store := &memoryExportStore{
		events:    []types.ControlPlaneAuditEvent{{ID: uuid.New(), Sequence: 1}},
		commitErr: errors.New("commit unavailable"),
	}
	worker := NewWorker(store, func(context.Context, uuid.UUID) (Sink, error) { return &recordingSink{}, nil })

	if _, err := worker.ExportAuditBatch(context.Background(), sinkID, 10); err == nil {
		t.Fatal("ExportAuditBatch() expected commit failure")
	}
	if store.failures != 1 || store.checkpoint != 0 {
		t.Fatalf("commit failure was not persisted: failures=%d checkpoint=%d", store.failures, store.checkpoint)
	}
	if statuses := store.attemptStatuses(); len(statuses) != 1 || statuses[0] != "FAILED" {
		t.Fatalf("commit-failed attempt remained running: %v", statuses)
	}
}

type memoryExportStore struct {
	events            []types.ControlPlaneAuditEvent
	checkpoint        int64
	started           int
	failures          int
	attempts          []memoryExportAttempt
	honorCancellation bool
	commitErr         error
}

type memoryExportAttempt struct {
	id     uuid.UUID
	status string
}

func (m *memoryExportStore) StartAuditExportAttempt(
	_ context.Context,
	_ uuid.UUID,
	_ []types.ControlPlaneAuditEvent,
) (uuid.UUID, error) {
	m.started++
	id := uuid.New()
	m.attempts = append(m.attempts, memoryExportAttempt{id: id, status: "RUNNING"})
	return id, nil
}

func (m *memoryExportStore) LoadAuditBatch(
	_ context.Context,
	_ uuid.UUID,
	limit int,
) ([]types.ControlPlaneAuditEvent, error) {
	var pending []types.ControlPlaneAuditEvent
	for _, event := range m.events {
		if event.Sequence > m.checkpoint {
			pending = append(pending, event)
		}
	}
	if len(pending) > limit {
		pending = pending[:limit]
	}
	return pending, nil
}

func (m *memoryExportStore) CommitAuditExport(
	_ context.Context,
	_ uuid.UUID,
	attemptID uuid.UUID,
	lastSequence int64,
	_ int,
) error {
	if m.commitErr != nil {
		return m.commitErr
	}
	m.checkpoint = lastSequence
	m.setAttemptStatus(attemptID, "SUCCEEDED")
	return nil
}

func (m *memoryExportStore) RecordAuditExportFailure(
	ctx context.Context,
	_ uuid.UUID,
	attemptID uuid.UUID,
	_ int64,
	_ error,
) error {
	if m.honorCancellation && ctx.Err() != nil {
		return ctx.Err()
	}
	m.failures++
	m.setAttemptStatus(attemptID, "FAILED")
	return nil
}

func (m *memoryExportStore) AuditExportLag(
	_ context.Context,
	_ uuid.UUID,
) (int64, error) {
	var latest int64
	for _, event := range m.events {
		if event.Sequence > latest {
			latest = event.Sequence
		}
	}
	return latest - m.checkpoint, nil
}

func (m *memoryExportStore) setAttemptStatus(id uuid.UUID, status string) {
	for i := range m.attempts {
		if m.attempts[i].id == id {
			m.attempts[i].status = status
			return
		}
	}
}

func (m *memoryExportStore) attemptStatuses() []string {
	result := make([]string, len(m.attempts))
	for i := range m.attempts {
		result[i] = m.attempts[i].status
	}
	return result
}

type recordingSink struct {
	sequences []int64
	err       error
}

type failOnceSink struct {
	calls int
}

type contextErrorSink struct{}

func (contextErrorSink) Export(ctx context.Context, _ types.ControlPlaneAuditEvent) error {
	return ctx.Err()
}

func (s *failOnceSink) Export(_ context.Context, _ types.ControlPlaneAuditEvent) error {
	s.calls++
	if s.calls == 1 {
		return errors.New("temporary sink failure")
	}
	return nil
}

func (s *recordingSink) Export(_ context.Context, event types.ControlPlaneAuditEvent) error {
	if s.err != nil {
		return s.err
	}
	s.sequences = append(s.sequences, event.Sequence)
	return nil
}
