package auditexport

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

type Store interface {
	LoadAuditBatch(context.Context, uuid.UUID, int) ([]types.ControlPlaneAuditEvent, error)
	CommitAuditExport(context.Context, uuid.UUID, int64, int) error
	RecordAuditExportFailure(context.Context, uuid.UUID, int64, error) error
	AuditExportLag(context.Context, uuid.UUID) (int64, error)
}

type Sink interface {
	Export(context.Context, types.ControlPlaneAuditEvent) error
}

type SinkResolver func(uuid.UUID) (Sink, error)

type Worker struct {
	store       Store
	resolveSink SinkResolver
}

func NewWorker(store Store, resolveSink SinkResolver) *Worker {
	return &Worker{store: store, resolveSink: resolveSink}
}

func (w *Worker) ExportAuditBatch(
	ctx context.Context,
	sinkID uuid.UUID,
	limit int,
) (types.ExportBatchResult, error) {
	result := types.ExportBatchResult{SinkID: sinkID}
	if w == nil || w.store == nil || w.resolveSink == nil {
		return result, errors.New("audit export worker is not configured")
	}
	if sinkID == uuid.Nil {
		return result, errors.New("audit export sink is required")
	}
	if limit < 1 || limit > 1000 {
		return result, errors.New("audit export batch size must be between 1 and 1000")
	}

	events, err := w.store.LoadAuditBatch(ctx, sinkID, limit)
	if err != nil {
		return result, fmt.Errorf("load audit export batch: %w", err)
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].Sequence != events[j].Sequence {
			return events[i].Sequence < events[j].Sequence
		}
		return events[i].ID.String() < events[j].ID.String()
	})
	if len(events) == 0 {
		result.CheckpointLag, err = w.store.AuditExportLag(ctx, sinkID)
		if err != nil {
			return result, fmt.Errorf("load audit export lag: %w", err)
		}
		return result, nil
	}

	sink, err := w.resolveSink(sinkID)
	if err != nil {
		return result, fmt.Errorf("resolve audit export sink: %w", err)
	}
	var previous int64
	for i, event := range events {
		if i > 0 && event.Sequence <= previous {
			return result, errors.New("audit export batch is not strictly ordered")
		}
		if err := sink.Export(ctx, event); err != nil {
			recordErr := w.store.RecordAuditExportFailure(ctx, sinkID, event.Sequence, err)
			var lagErr error
			result.CheckpointLag, lagErr = w.store.AuditExportLag(ctx, sinkID)
			if recordErr != nil {
				err = errors.Join(
					fmt.Errorf("export audit sequence %d: %w", event.Sequence, err),
					fmt.Errorf("record audit export failure: %w", recordErr),
				)
			} else {
				err = fmt.Errorf("export audit sequence %d: %w", event.Sequence, err)
			}
			if lagErr != nil {
				err = errors.Join(err, fmt.Errorf("load audit export lag: %w", lagErr))
			}
			return result, err
		}
		previous = event.Sequence
		result.Exported++
		result.LastSequence = event.Sequence
	}
	if err := w.store.CommitAuditExport(ctx, sinkID, result.LastSequence, result.Exported); err != nil {
		return result, fmt.Errorf("commit audit export checkpoint: %w", err)
	}
	result.CheckpointLag, err = w.store.AuditExportLag(ctx, sinkID)
	if err != nil {
		return result, fmt.Errorf("load audit export lag: %w", err)
	}
	return result, nil
}
