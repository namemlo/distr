package jobs

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-co-op/gocron/v2"
	. "github.com/onsi/gomega"
	"go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/zap"
)

func TestRegisterDurationJobRegistersNamedDurationSchedule(t *testing.T) {
	g := NewWithT(t)
	scheduler := newTestScheduler(t)

	err := scheduler.RegisterDurationJob(15*time.Second, NewJob("duration-job", func(context.Context) error {
		return nil
	}, 0))

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(scheduler.scheduler.Jobs()).To(HaveLen(1))
	registered := scheduler.scheduler.Jobs()[0]
	g.Expect(registered.Name()).To(Equal("duration-job"))
	g.Expect(registered.Schedule()).To(Equal(gocron.DurationJobSchedule{Duration: 15 * time.Second}))
}

func TestRegisterDurationJobRejectsNonPositiveDuration(t *testing.T) {
	for _, test := range []struct {
		name     string
		duration time.Duration
		err      error
	}{
		{name: "zero", duration: 0, err: gocron.ErrDurationJobIntervalZero},
		{name: "negative", duration: -time.Second, err: gocron.ErrDurationJobIntervalNegative},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			scheduler := newTestScheduler(t)

			err := scheduler.RegisterDurationJob(test.duration, NewJob("invalid-duration", func(context.Context) error {
				return nil
			}, 0))

			g.Expect(err).To(MatchError(test.err))
			g.Expect(scheduler.scheduler.Jobs()).To(BeEmpty())
		})
	}
}

func TestRegisterDurationJobRunsThroughRunnerWithoutOverlap(t *testing.T) {
	g := NewWithT(t)
	scheduler := newTestScheduler(t)
	started := make(chan struct{})
	release := make(chan struct{})
	overlap := make(chan struct{})
	var runs atomic.Int32
	var closeStarted sync.Once
	var closeOverlap sync.Once

	err := scheduler.RegisterDurationJob(5*time.Millisecond, NewJob("singleton-duration", func(context.Context) error {
		if runs.Add(1) > 1 {
			closeOverlap.Do(func() { close(overlap) })
			return nil
		}
		closeStarted.Do(func() { close(started) })
		<-release
		return nil
	}, 0))
	g.Expect(err).NotTo(HaveOccurred())

	scheduler.Start()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("duration job did not run through the scheduler runner")
	}

	select {
	case <-overlap:
		t.Fatal("duration job overlapped while its previous run was active")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	g.Expect(scheduler.Shutdown()).To(Succeed())
}

func newTestScheduler(t *testing.T) *Scheduler {
	t.Helper()
	g := NewWithT(t)
	inner, err := gocron.NewScheduler()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() {
		_ = inner.Shutdown()
	})
	logger := zap.NewNop()
	return &Scheduler{
		scheduler: inner,
		logger:    logger,
		runner:    NewRunner(logger, nil, nil, noop.NewTracerProvider(), nil),
	}
}
