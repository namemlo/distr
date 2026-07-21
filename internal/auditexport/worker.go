package auditexport

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

const auditExportFailurePersistenceTimeout = 5 * time.Second

type Store interface {
	LoadAuditBatch(context.Context, uuid.UUID, int) ([]types.ControlPlaneAuditEvent, error)
	StartAuditExportAttempt(context.Context, uuid.UUID, []types.ControlPlaneAuditEvent) (uuid.UUID, error)
	CommitAuditExport(context.Context, uuid.UUID, uuid.UUID, int64, int) error
	RecordAuditExportFailure(context.Context, uuid.UUID, uuid.UUID, int64, error) error
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
	var previous int64
	for i, event := range events {
		if i > 0 && event.Sequence <= previous {
			return result, errors.New("audit export batch is not strictly ordered")
		}
		previous = event.Sequence
	}

	attemptID, err := w.store.StartAuditExportAttempt(ctx, sinkID, events)
	if err != nil {
		return result, fmt.Errorf("start audit export attempt: %w", err)
	}
	sink, err := w.resolveSink(sinkID)
	if err != nil {
		var persistenceErr error
		result.CheckpointLag, persistenceErr = w.persistAuditExportFailure(
			ctx, sinkID, attemptID, events[0].Sequence, err,
		)
		return result, errors.Join(
			fmt.Errorf("resolve audit export sink: %w", err),
			persistenceErr,
		)
	}
	for _, event := range events {
		if err := sink.Export(ctx, event); err != nil {
			var persistenceErr error
			result.CheckpointLag, persistenceErr = w.persistAuditExportFailure(
				ctx, sinkID, attemptID, event.Sequence, err,
			)
			return result, errors.Join(
				fmt.Errorf("export audit sequence %d: %w", event.Sequence, err),
				persistenceErr,
			)
		}
		result.Exported++
		result.LastSequence = event.Sequence
	}
	if err := w.store.CommitAuditExport(ctx, sinkID, attemptID, result.LastSequence, result.Exported); err != nil {
		var persistenceErr error
		result.CheckpointLag, persistenceErr = w.persistAuditExportFailure(
			ctx, sinkID, attemptID, result.LastSequence, err,
		)
		return result, errors.Join(
			fmt.Errorf("commit audit export checkpoint: %w", err),
			persistenceErr,
		)
	}
	result.CheckpointLag, err = w.store.AuditExportLag(ctx, sinkID)
	if err != nil {
		return result, fmt.Errorf("load audit export lag: %w", err)
	}
	return result, nil
}

func (w *Worker) persistAuditExportFailure(
	ctx context.Context,
	sinkID uuid.UUID,
	attemptID uuid.UUID,
	failedSequence int64,
	exportErr error,
) (int64, error) {
	persistenceCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		auditExportFailurePersistenceTimeout,
	)
	defer cancel()
	recordErr := w.store.RecordAuditExportFailure(
		persistenceCtx, sinkID, attemptID, failedSequence, exportErr,
	)
	lag, lagErr := w.store.AuditExportLag(persistenceCtx, sinkID)
	return lag, errors.Join(
		wrapOptionalError("record audit export failure", recordErr),
		wrapOptionalError("load audit export lag", lagErr),
	)
}

func wrapOptionalError(message string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", message, err)
}
