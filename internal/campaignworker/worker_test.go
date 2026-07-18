package campaignworker

import (
	"context"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/onsi/gomega"
)

type tickerFake struct {
	calls []uuid.UUID
}

func (t *tickerFake) Tick(
	_ context.Context,
	runID uuid.UUID,
	_ time.Time,
) (types.CampaignSchedulerResult, error) {
	t.calls = append(t.calls, runID)
	return types.CampaignSchedulerResult{LeaseAcquired: true}, nil
}

func TestWorkerResumesRunsInStableOrderAfterRestart(t *testing.T) {
	g := gomega.NewWithT(t)
	first := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	second := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	ticker := &tickerFake{}
	worker := New(ticker)

	err := worker.RunOnce(context.Background(), []uuid.UUID{second, first}, time.Now())
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(ticker.calls).To(gomega.Equal([]uuid.UUID{first, second}))
}
