package campaignworker

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

type CampaignTicker interface {
	Tick(context.Context, uuid.UUID, time.Time) (types.CampaignSchedulerResult, error)
}

type Worker struct {
	ticker CampaignTicker
}

func New(ticker CampaignTicker) *Worker {
	return &Worker{ticker: ticker}
}

func (w *Worker) RunOnce(ctx context.Context, runIDs []uuid.UUID, now time.Time) error {
	ordered := slices.Clone(runIDs)
	slices.SortFunc(ordered, func(a, b uuid.UUID) int {
		return slices.Compare(a[:], b[:])
	})
	var runErrors []error
	for _, runID := range ordered {
		if _, err := w.ticker.Tick(ctx, runID, now); err != nil {
			runErrors = append(runErrors, fmt.Errorf("campaign run %s: %w", runID, err))
		}
	}
	return errors.Join(runErrors...)
}
