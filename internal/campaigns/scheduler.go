package campaigns

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

var (
	ErrCampaignLeaseLost                      = errors.New("campaign scheduler lease lost")
	ErrCampaignObservationVerifierUnavailable = errors.New("campaign observation verifier unavailable")
	ErrCampaignObservationResolverUnavailable = errors.New("campaign observation resolver unavailable")
)

// CampaignObservationVerifier is the PR-077 integration seam. Implementations
// must verify that the organization-scoped observation ID exists and is bound
// to the supplied checksum. The default implementation deliberately fails
// closed until the independent observation store is available.
type CampaignObservationVerifier interface {
	VerifyCampaignObservation(context.Context, uuid.UUID, uuid.UUID, string) error
}

type UnwiredCampaignObservationVerifier struct{}

func (UnwiredCampaignObservationVerifier) VerifyCampaignObservation(
	context.Context,
	uuid.UUID,
	uuid.UUID,
	string,
) error {
	return ErrCampaignObservationVerifierUnavailable
}

type CampaignObservationResolver interface {
	ResolveCampaignObservation(
		context.Context,
		uuid.UUID,
		uuid.UUID,
		string,
	) (uuid.UUID, string, error)
}

type UnwiredCampaignObservationResolver struct{}

func (UnwiredCampaignObservationResolver) ResolveCampaignObservation(
	context.Context,
	uuid.UUID,
	uuid.UUID,
	string,
) (uuid.UUID, string, error) {
	return uuid.Nil, "", ErrCampaignObservationResolverUnavailable
}

type SchedulerStore interface {
	AcquireCampaignLease(
		context.Context,
		uuid.UUID,
		string,
		time.Time,
		time.Duration,
	) (types.CampaignLease, bool, error)
	LoadCampaignSchedule(context.Context, uuid.UUID, int64) (types.CampaignSchedule, error)
	RecordPrerequisitesAndAdmit(
		context.Context,
		types.CampaignMemberCandidate,
		types.CampaignMemberAdmission,
		CampaignObservationResolver,
		CampaignObservationVerifier,
		int64,
	) (bool, bool, error)
	RecordThresholdAndMaybePause(
		context.Context,
		types.CampaignThresholdEvaluation,
		int64,
	) (bool, error)
	PauseCampaignAdmission(context.Context, uuid.UUID, string, int64) error
}

type PendingCampaignPauseStore interface {
	FinalizePendingCampaignPause(context.Context, uuid.UUID, int64) (bool, error)
}

type Scheduler struct {
	store         SchedulerStore
	resolver      CampaignObservationResolver
	observations  CampaignObservationVerifier
	workerID      string
	leaseDuration time.Duration
}

func NewScheduler(
	store SchedulerStore,
	observations CampaignObservationVerifier,
	workerID string,
	leaseDuration time.Duration,
) *Scheduler {
	return NewSchedulerWithObservationResolver(
		store,
		UnwiredCampaignObservationResolver{},
		observations,
		workerID,
		leaseDuration,
	)
}

func NewSchedulerWithObservationResolver(
	store SchedulerStore,
	resolver CampaignObservationResolver,
	observations CampaignObservationVerifier,
	workerID string,
	leaseDuration time.Duration,
) *Scheduler {
	if resolver == nil {
		resolver = UnwiredCampaignObservationResolver{}
	}
	if observations == nil {
		observations = UnwiredCampaignObservationVerifier{}
	}
	return &Scheduler{
		store:         store,
		resolver:      resolver,
		observations:  observations,
		workerID:      workerID,
		leaseDuration: leaseDuration,
	}
}

func (s *Scheduler) Tick(
	ctx context.Context,
	runID uuid.UUID,
	now time.Time,
) (types.CampaignSchedulerResult, error) {
	lease, acquired, err := s.store.AcquireCampaignLease(
		ctx,
		runID,
		s.workerID,
		now,
		s.leaseDuration,
	)
	if err != nil || !acquired {
		return types.CampaignSchedulerResult{LeaseAcquired: acquired}, err
	}
	result := types.CampaignSchedulerResult{LeaseAcquired: true}

	schedule, err := s.store.LoadCampaignSchedule(ctx, runID, lease.FencingToken)
	if err != nil {
		return result, err
	}
	if schedule.Run.PauseRequested && schedule.AtSafePoint {
		if store, ok := s.store.(PendingCampaignPauseStore); ok {
			finalized, err := store.FinalizePendingCampaignPause(
				ctx,
				runID,
				lease.FencingToken,
			)
			if err != nil {
				return result, err
			}
			result.Paused = finalized
		}
		return result, nil
	}
	if schedule.Run.State != types.CampaignRunStateRunning || schedule.Run.AdmissionsBlocked {
		return result, nil
	}
	if campaignAdmissionBlocked(schedule, now) {
		return result, nil
	}

	threshold := EvaluateThreshold(schedule.ThresholdPolicy, schedule.ThresholdSnapshot)
	if schedule.ThresholdPolicy.MinimumSamples > 0 {
		evaluation := types.CampaignThresholdEvaluation{
			ID:                 uuid.New(),
			CampaignRunID:      runID,
			Samples:            threshold.Samples,
			Successful:         schedule.ThresholdSnapshot.Successful,
			Failed:             schedule.ThresholdSnapshot.Failed,
			FailureRate:        threshold.FailureRate,
			MaximumFailureRate: schedule.ThresholdPolicy.MaximumFailureRate,
			Breached:           threshold.Breached,
			EvaluatedAt:        now,
			FencingToken:       lease.FencingToken,
		}
		paused, err := s.store.RecordThresholdAndMaybePause(
			ctx,
			evaluation,
			lease.FencingToken,
		)
		if err != nil {
			return result, err
		}
		if paused {
			result.Paused = true
			return result, nil
		}
	}

	candidates := slices.Clone(schedule.Candidates)
	slices.SortFunc(candidates, compareCampaignCandidates)
	if len(candidates) == 0 {
		return result, nil
	}
	candidate := candidates[0]

	admission := types.CampaignMemberAdmission{
		RunID:        runID,
		WaveRunID:    candidate.WaveRunID,
		MemberRunID:  candidate.MemberRunID,
		PlanID:       candidate.PlanID,
		WaveOrder:    candidate.WaveOrder,
		MemberOrder:  candidate.MemberOrder,
		AdmittedAt:   now,
		FencingToken: lease.FencingToken,
	}
	admitted, paused, err := s.store.RecordPrerequisitesAndAdmit(
		ctx, candidate, admission, s.resolver, s.observations, lease.FencingToken,
	)
	if err != nil {
		return result, err
	}
	result.Paused = paused
	result.Admitted = admitted
	if admitted {
		result.MemberRunID = candidate.MemberRunID
	}
	return result, nil
}

func campaignAdmissionBlocked(schedule types.CampaignSchedule, now time.Time) bool {
	if schedule.BakeUntil != nil && now.Before(*schedule.BakeUntil) {
		return true
	}
	if schedule.WaveMaximumConcurrency > 0 &&
		schedule.WaveActive >= schedule.WaveMaximumConcurrency {
		return true
	}
	if schedule.CampaignMaximumConcurrency > 0 &&
		schedule.CampaignActive >= schedule.CampaignMaximumConcurrency {
		return true
	}
	samples := schedule.ThresholdSnapshot.Successful + schedule.ThresholdSnapshot.Failed
	if schedule.MinimumHealthyBasisPoints > 0 && samples > 0 {
		healthyBasisPoints := schedule.ThresholdSnapshot.Successful * 10000 / samples
		if healthyBasisPoints < schedule.MinimumHealthyBasisPoints {
			return true
		}
	}
	return false
}

func compareCampaignCandidates(a, b types.CampaignMemberCandidate) int {
	if a.WaveOrder != b.WaveOrder {
		return a.WaveOrder - b.WaveOrder
	}
	if a.MemberOrder != b.MemberOrder {
		return a.MemberOrder - b.MemberOrder
	}
	return slices.Compare(a.PlanID[:], b.PlanID[:])
}

func RequireCampaignLease(tagRows int64) error {
	if tagRows != 1 {
		return fmt.Errorf("%w: fenced write affected %d rows", ErrCampaignLeaseLost, tagRows)
	}
	return nil
}
