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
	worker := NewWorker(store, func(uuid.UUID) (Sink, error) { return sink, nil })

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
	worker := NewWorker(store, func(uuid.UUID) (Sink, error) { return sink, nil })

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

type memoryExportStore struct {
	events     []types.ControlPlaneAuditEvent
	checkpoint int64
	failures   int
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
	lastSequence int64,
	_ int,
) error {
	m.checkpoint = lastSequence
	return nil
}

func (m *memoryExportStore) RecordAuditExportFailure(
	_ context.Context,
	_ uuid.UUID,
	_ int64,
	_ error,
) error {
	m.failures++
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

type recordingSink struct {
	sequences []int64
	err       error
}

func (s *recordingSink) Export(_ context.Context, event types.ControlPlaneAuditEvent) error {
	if s.err != nil {
		return s.err
	}
	s.sequences = append(s.sequences, event.Sequence)
	return nil
}
