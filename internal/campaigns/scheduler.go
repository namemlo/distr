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

type SchedulerStore interface {
	AcquireCampaignLease(
		context.Context,
		uuid.UUID,
		string,
		time.Time,
		time.Duration,
	) (types.CampaignLease, bool, error)
	LoadCampaignSchedule(context.Context, uuid.UUID, int64) (types.CampaignSchedule, error)
	RecordCampaignPrerequisiteEvaluation(
		context.Context,
		types.CampaignPrerequisiteEvaluation,
		int64,
	) error
	RecordCampaignThresholdEvaluation(
		context.Context,
		types.CampaignThresholdEvaluation,
		int64,
	) error
	AdmitCampaignMember(
		context.Context,
		types.CampaignMemberAdmission,
		int64,
	) (bool, error)
	PauseCampaignAdmission(context.Context, uuid.UUID, string, int64) error
}

type Scheduler struct {
	store         SchedulerStore
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
	if observations == nil {
		observations = UnwiredCampaignObservationVerifier{}
	}
	return &Scheduler{
		store:         store,
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
	if schedule.Run.State != types.CampaignRunStateRunning || schedule.Run.AdmissionsBlocked {
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
		if err := s.store.RecordCampaignThresholdEvaluation(
			ctx,
			evaluation,
			lease.FencingToken,
		); err != nil {
			return result, err
		}
	}
	if threshold.Breached {
		if err := s.store.PauseCampaignAdmission(
			ctx,
			runID,
			"campaign threshold breached",
			lease.FencingToken,
		); err != nil {
			return result, err
		}
		result.Paused = true
		return result, nil
	}

	candidates := slices.Clone(schedule.Candidates)
	slices.SortFunc(candidates, compareCampaignCandidates)
	if len(candidates) == 0 {
		return result, nil
	}
	candidate := candidates[0]

	for _, requirement := range candidate.Prerequisites {
		verifiedErr := s.observations.VerifyCampaignObservation(
			ctx,
			requirement.OrganizationID,
			requirement.ObservationID,
			requirement.ObservationChecksum,
		)
		matched := verifiedErr == nil &&
			requirement.ObservationID != uuid.Nil &&
			requirement.ExpectedChecksum != "" &&
			requirement.ObservationChecksum == requirement.ExpectedChecksum
		reason := ""
		if verifiedErr != nil {
			reason = verifiedErr.Error()
		} else if !matched {
			reason = "prerequisite observation checksum does not match frozen expectation"
		}
		actualObservationID := requirement.ObservationID
		actualChecksum := requirement.ObservationChecksum
		if verifiedErr != nil {
			actualObservationID = uuid.Nil
			actualChecksum = ""
		}
		evaluation := types.CampaignPrerequisiteEvaluation{
			ID:                  uuid.New(),
			CampaignRunID:       runID,
			MemberRunID:         candidate.MemberRunID,
			UpstreamPlanID:      requirement.UpstreamPlanID,
			StepKey:             requirement.StepKey,
			ExpectedChecksum:    requirement.ExpectedChecksum,
			ActualObservationID: actualObservationID,
			ActualChecksum:      actualChecksum,
			Matched:             matched,
			Reason:              reason,
			EvaluatedAt:         now,
			FencingToken:        lease.FencingToken,
		}
		if err := s.store.RecordCampaignPrerequisiteEvaluation(
			ctx,
			evaluation,
			lease.FencingToken,
		); err != nil {
			return result, err
		}
		if !matched {
			pauseReason := "campaign prerequisite mismatch"
			if errors.Is(verifiedErr, ErrCampaignObservationVerifierUnavailable) {
				pauseReason = "trusted observation unavailable"
			}
			if err := s.store.PauseCampaignAdmission(
				ctx,
				runID,
				pauseReason,
				lease.FencingToken,
			); err != nil {
				return result, err
			}
			result.Paused = true
			return result, nil
		}
	}

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
	admitted, err := s.store.AdmitCampaignMember(ctx, admission, lease.FencingToken)
	if err != nil {
		return result, err
	}
	result.Admitted = admitted
	if admitted {
		result.MemberRunID = candidate.MemberRunID
	}
	return result, nil
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
