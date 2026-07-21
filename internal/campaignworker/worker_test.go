package campaignworker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/onsi/gomega"
)

type tickerFake struct {
	calls  []uuid.UUID
	errors map[uuid.UUID]error
}

func (t *tickerFake) Tick(
	_ context.Context,
	runID uuid.UUID,
	_ time.Time,
) (types.CampaignSchedulerResult, error) {
	t.calls = append(t.calls, runID)
	if t.errors != nil {
		if err := t.errors[runID]; err != nil {
			return types.CampaignSchedulerResult{}, err
		}
	}
	return types.CampaignSchedulerResult{LeaseAcquired: true}, nil
}

func TestWorkerContinuesAndAggregatesRunFailures(t *testing.T) {
	g := gomega.NewWithT(t)
	first := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	second := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	third := uuid.MustParse("00000000-0000-0000-0000-000000000003")
	ticker := &tickerFake{errors: map[uuid.UUID]error{
		first: errors.New("first failed"), third: errors.New("third failed"),
	}}
	err := New(ticker).RunOnce(context.Background(), []uuid.UUID{third, first, second}, time.Now())
	g.Expect(ticker.calls).To(gomega.Equal([]uuid.UUID{first, second, third}))
	g.Expect(err).To(gomega.MatchError(gomega.And(
		gomega.ContainSubstring("first failed"), gomega.ContainSubstring("third failed"),
	)))
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
