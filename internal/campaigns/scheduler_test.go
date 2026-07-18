package campaigns

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/onsi/gomega"
)

type schedulerStoreFake struct {
	lease              types.CampaignLease
	acquired           bool
	schedule           types.CampaignSchedule
	admitted           []types.CampaignMemberAdmission
	prerequisites      []types.CampaignPrerequisiteEvaluation
	thresholds         []types.CampaignThresholdEvaluation
	pausedReason       string
	loseLeaseOnAdmit   bool
	duplicateAdmission bool
	finalizedPause     bool
}

func (s *schedulerStoreFake) FinalizePendingCampaignPause(
	_ context.Context,
	_ uuid.UUID,
	_ int64,
) (bool, error) {
	s.finalizedPause = true
	return true, nil
}

func (s *schedulerStoreFake) AcquireCampaignLease(
	context.Context,
	uuid.UUID,
	string,
	time.Time,
	time.Duration,
) (types.CampaignLease, bool, error) {
	return s.lease, s.acquired, nil
}

func (s *schedulerStoreFake) LoadCampaignSchedule(
	context.Context,
	uuid.UUID,
	int64,
) (types.CampaignSchedule, error) {
	return s.schedule, nil
}

func (s *schedulerStoreFake) RecordCampaignPrerequisiteEvaluation(
	_ context.Context,
	evaluation types.CampaignPrerequisiteEvaluation,
	_ int64,
) error {
	s.prerequisites = append(s.prerequisites, evaluation)
	return nil
}

func (s *schedulerStoreFake) RecordCampaignThresholdEvaluation(
	_ context.Context,
	evaluation types.CampaignThresholdEvaluation,
	_ int64,
) error {
	s.thresholds = append(s.thresholds, evaluation)
	return nil
}

func (s *schedulerStoreFake) AdmitCampaignMember(
	_ context.Context,
	admission types.CampaignMemberAdmission,
	_ int64,
) (bool, error) {
	if s.loseLeaseOnAdmit {
		return false, ErrCampaignLeaseLost
	}
	if s.duplicateAdmission {
		return false, nil
	}
	s.admitted = append(s.admitted, admission)
	return true, nil
}

func (s *schedulerStoreFake) PauseCampaignAdmission(
	_ context.Context,
	_ uuid.UUID,
	reason string,
	_ int64,
) error {
	s.pausedReason = reason
	return nil
}

type observationVerifierFake struct {
	organizationID uuid.UUID
	observationID  uuid.UUID
	checksum       string
	err            error
}

func (v *observationVerifierFake) VerifyCampaignObservation(
	context.Context,
	uuid.UUID,
	uuid.UUID,
	string,
) error {
	return v.err
}

type observationResolverFake struct {
	organizationID      uuid.UUID
	componentInstanceID uuid.UUID
	expected            string
	observationID       uuid.UUID
	actual              string
	err                 error
}

func (r *observationResolverFake) ResolveCampaignObservation(
	_ context.Context,
	organizationID uuid.UUID,
	componentInstanceID uuid.UUID,
	expectedChecksum string,
) (uuid.UUID, string, error) {
	r.organizationID = organizationID
	r.componentInstanceID = componentInstanceID
	r.expected = expectedChecksum
	return r.observationID, r.actual, r.err
}

type capturingObservationVerifier struct {
	organizationID uuid.UUID
	observationID  uuid.UUID
	checksum       string
	err            error
}

func (v *capturingObservationVerifier) VerifyCampaignObservation(
	_ context.Context,
	organizationID uuid.UUID,
	observationID uuid.UUID,
	checksum string,
) error {
	v.organizationID = organizationID
	v.observationID = observationID
	v.checksum = checksum
	return v.err
}

func TestSchedulerUsesDeterministicAdmissionOrderAndDeduplicatesTick(t *testing.T) {
	g := gomega.NewWithT(t)
	runID := uuid.New()
	planA := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	planB := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	store := &schedulerStoreFake{
		acquired: true,
		lease:    types.CampaignLease{RunID: runID, FencingToken: 11},
		schedule: types.CampaignSchedule{
			Run: types.CampaignRun{ID: runID, State: types.CampaignRunStateRunning},
			Candidates: []types.CampaignMemberCandidate{
				{MemberRunID: uuid.New(), WaveOrder: 2, MemberOrder: 1, PlanID: planA},
				{MemberRunID: uuid.New(), WaveOrder: 1, MemberOrder: 2, PlanID: planB},
				{MemberRunID: uuid.New(), WaveOrder: 1, MemberOrder: 2, PlanID: planA},
			},
		},
	}
	scheduler := NewScheduler(store, UnwiredCampaignObservationVerifier{}, "worker-a", time.Minute)

	result, err := scheduler.Tick(context.Background(), runID, time.Now())
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result.Admitted).To(gomega.BeTrue())
	g.Expect(store.admitted).To(gomega.HaveLen(1))
	g.Expect(store.admitted[0].PlanID).To(gomega.Equal(planA))
	g.Expect(store.admitted[0].WaveOrder).To(gomega.Equal(1))

	store.duplicateAdmission = true
	result, err = scheduler.Tick(context.Background(), runID, time.Now())
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result.Admitted).To(gomega.BeFalse())
	g.Expect(store.admitted).To(gomega.HaveLen(1))
}

func TestSchedulerFailsClosedWhenObservationVerifierIsUnwired(t *testing.T) {
	g := gomega.NewWithT(t)
	runID := uuid.New()
	memberRunID := uuid.New()
	store := &schedulerStoreFake{
		acquired: true,
		lease:    types.CampaignLease{RunID: runID, FencingToken: 3},
		schedule: types.CampaignSchedule{
			Run: types.CampaignRun{ID: runID, State: types.CampaignRunStateRunning},
			Candidates: []types.CampaignMemberCandidate{{
				MemberRunID: memberRunID,
				WaveOrder:   1,
				MemberOrder: 1,
				PlanID:      uuid.New(),
				Prerequisites: []types.CampaignObservationRequirement{{
					OrganizationID:      uuid.New(),
					UpstreamPlanID:      uuid.New(),
					StepKey:             "verify-ledger",
					ObservationID:       uuid.New(),
					ObservationChecksum: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					ExpectedChecksum:    "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				}},
			}},
		},
	}

	result, err := NewScheduler(
		store,
		UnwiredCampaignObservationVerifier{},
		"worker-a",
		time.Minute,
	).Tick(context.Background(), runID, time.Now())
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result.Admitted).To(gomega.BeFalse())
	g.Expect(store.admitted).To(gomega.BeEmpty())
	g.Expect(store.pausedReason).To(gomega.ContainSubstring("trusted observation unavailable"))
	g.Expect(store.prerequisites).To(gomega.HaveLen(1))
	g.Expect(store.prerequisites[0].ExpectedChecksum).NotTo(gomega.BeEmpty())
	g.Expect(store.prerequisites[0].ActualObservationID).To(gomega.Equal(uuid.Nil))
	g.Expect(store.prerequisites[0].ActualChecksum).To(gomega.BeEmpty())
	g.Expect(store.prerequisites[0].Matched).To(gomega.BeFalse())
}

func TestSchedulerPersistsExactObservationBindingAndPausesOnMismatch(t *testing.T) {
	g := gomega.NewWithT(t)
	runID := uuid.New()
	observationID := uuid.New()
	expected := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	actual := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	store := &schedulerStoreFake{
		acquired: true,
		lease:    types.CampaignLease{RunID: runID, FencingToken: 7},
		schedule: types.CampaignSchedule{
			Run: types.CampaignRun{ID: runID, State: types.CampaignRunStateRunning},
			Candidates: []types.CampaignMemberCandidate{{
				MemberRunID: uuid.New(),
				PlanID:      uuid.New(),
				Prerequisites: []types.CampaignObservationRequirement{{
					OrganizationID:      uuid.New(),
					UpstreamPlanID:      uuid.New(),
					StepKey:             "health",
					ObservationID:       observationID,
					ObservationChecksum: actual,
					ExpectedChecksum:    expected,
				}},
			}},
		},
	}
	scheduler := NewScheduler(store, &observationVerifierFake{}, "worker-a", time.Minute)

	result, err := scheduler.Tick(context.Background(), runID, time.Now())
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result.Admitted).To(gomega.BeFalse())
	g.Expect(store.prerequisites).To(gomega.HaveLen(1))
	g.Expect(store.prerequisites[0].ExpectedChecksum).To(gomega.Equal(expected))
	g.Expect(store.prerequisites[0].ActualObservationID).To(gomega.Equal(observationID))
	g.Expect(store.prerequisites[0].ActualChecksum).To(gomega.Equal(actual))
	g.Expect(store.prerequisites[0].Matched).To(gomega.BeFalse())
	g.Expect(store.pausedReason).To(gomega.ContainSubstring("prerequisite mismatch"))
}

func TestSchedulerResolvesFrozenPrerequisiteByCanonicalProviderIdentity(t *testing.T) {
	g := gomega.NewWithT(t)
	runID := uuid.New()
	organizationID := uuid.New()
	placementID := uuid.New()
	componentInstanceID := uuid.New()
	observationID := uuid.New()
	checksum := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	store := &schedulerStoreFake{
		acquired: true,
		lease:    types.CampaignLease{RunID: runID, FencingToken: 31},
		schedule: types.CampaignSchedule{
			Run: types.CampaignRun{ID: runID, State: types.CampaignRunStateRunning},
			Candidates: []types.CampaignMemberCandidate{{
				MemberRunID: uuid.New(),
				PlanID:      uuid.New(),
				Prerequisites: []types.CampaignObservationRequirement{{
					OrganizationID:              organizationID,
					ProviderPlacementID:         placementID,
					ProviderComponentInstanceID: componentInstanceID,
					ExpectedChecksum:            checksum,
				}},
			}},
		},
	}
	resolver := &observationResolverFake{
		observationID: observationID,
		actual:        checksum,
	}
	verifier := &capturingObservationVerifier{}

	result, err := NewSchedulerWithObservationResolver(
		store,
		resolver,
		verifier,
		"worker-a",
		time.Minute,
	).Tick(context.Background(), runID, time.Now())
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result.Admitted).To(gomega.BeTrue())
	g.Expect(resolver.organizationID).To(gomega.Equal(organizationID))
	g.Expect(resolver.componentInstanceID).To(gomega.Equal(componentInstanceID))
	g.Expect(resolver.expected).To(gomega.Equal(checksum))
	g.Expect(verifier.organizationID).To(gomega.Equal(organizationID))
	g.Expect(verifier.observationID).To(gomega.Equal(observationID))
	g.Expect(verifier.checksum).To(gomega.Equal(checksum))
	g.Expect(store.prerequisites).To(gomega.HaveLen(1))
	g.Expect(store.prerequisites[0].ActualObservationID).To(gomega.Equal(observationID))
}

func TestSchedulerBlocksForBakeConcurrencyRiskAndHealth(t *testing.T) {
	g := gomega.NewWithT(t)
	runID := uuid.New()
	now := time.Now()
	for _, tc := range []struct {
		name     string
		schedule types.CampaignSchedule
	}{
		{
			name: "bake",
			schedule: types.CampaignSchedule{
				BakeUntil: new(now.Add(time.Minute)),
			},
		},
		{
			name: "wave concurrency",
			schedule: types.CampaignSchedule{
				WaveMaximumConcurrency: 1,
				WaveActive:             1,
			},
		},
		{
			name: "campaign concurrency",
			schedule: types.CampaignSchedule{
				CampaignMaximumConcurrency: 1,
				CampaignActive:             1,
			},
		},
		{
			name: "health",
			schedule: types.CampaignSchedule{
				MinimumHealthyBasisPoints: 9000,
				ThresholdSnapshot: types.CampaignThresholdSnapshot{
					Successful: 8,
					Failed:     2,
				},
			},
		},
	} {
		tc.schedule.Run = types.CampaignRun{ID: runID, State: types.CampaignRunStateRunning}
		tc.schedule.Candidates = []types.CampaignMemberCandidate{{
			MemberRunID: uuid.New(),
			PlanID:      uuid.New(),
		}}
		store := &schedulerStoreFake{
			acquired: true,
			lease:    types.CampaignLease{RunID: runID, FencingToken: 41},
			schedule: tc.schedule,
		}
		result, err := NewScheduler(
			store,
			UnwiredCampaignObservationVerifier{},
			"worker-a",
			time.Minute,
		).Tick(context.Background(), runID, now)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(result.Admitted).To(gomega.BeFalse(), tc.name)
		g.Expect(store.admitted).To(gomega.BeEmpty())
	}
}

func TestSchedulerStopsForThresholdPauseAndLeaseLoss(t *testing.T) {
	g := gomega.NewWithT(t)
	runID := uuid.New()
	store := &schedulerStoreFake{
		acquired: true,
		lease:    types.CampaignLease{RunID: runID, FencingToken: 9},
		schedule: types.CampaignSchedule{
			Run:               types.CampaignRun{ID: runID, State: types.CampaignRunStateRunning},
			ThresholdPolicy:   types.CampaignThresholdPolicy{MinimumSamples: 1, MaximumFailureRate: 0},
			ThresholdSnapshot: types.CampaignThresholdSnapshot{Failed: 1},
			Candidates:        []types.CampaignMemberCandidate{{MemberRunID: uuid.New(), PlanID: uuid.New()}},
		},
	}

	result, err := NewScheduler(store, UnwiredCampaignObservationVerifier{}, "worker-a", time.Minute).
		Tick(context.Background(), runID, time.Now())
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result.Admitted).To(gomega.BeFalse())
	g.Expect(store.thresholds).To(gomega.HaveLen(1))
	g.Expect(store.thresholds[0].Breached).To(gomega.BeTrue())
	g.Expect(store.pausedReason).To(gomega.ContainSubstring("threshold breached"))

	store.schedule.ThresholdPolicy = types.CampaignThresholdPolicy{}
	store.schedule.ThresholdSnapshot = types.CampaignThresholdSnapshot{}
	store.pausedReason = ""
	store.loseLeaseOnAdmit = true
	_, err = NewScheduler(store, UnwiredCampaignObservationVerifier{}, "worker-a", time.Minute).
		Tick(context.Background(), runID, time.Now())
	g.Expect(errors.Is(err, ErrCampaignLeaseLost)).To(gomega.BeTrue())
	g.Expect(store.admitted).To(gomega.BeEmpty())
}

func TestSchedulerDoesNotExposeWhilePausedOrLeaseUnavailable(t *testing.T) {
	g := gomega.NewWithT(t)
	runID := uuid.New()
	store := &schedulerStoreFake{
		acquired: false,
		lease:    types.CampaignLease{RunID: runID},
		schedule: types.CampaignSchedule{
			Run:        types.CampaignRun{ID: runID, State: types.CampaignRunStatePaused},
			Candidates: []types.CampaignMemberCandidate{{MemberRunID: uuid.New(), PlanID: uuid.New()}},
		},
	}
	scheduler := NewScheduler(store, UnwiredCampaignObservationVerifier{}, "worker-a", time.Minute)
	result, err := scheduler.Tick(context.Background(), runID, time.Now())
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result.LeaseAcquired).To(gomega.BeFalse())

	store.acquired = true
	result, err = scheduler.Tick(context.Background(), runID, time.Now())
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result.Admitted).To(gomega.BeFalse())
	g.Expect(store.admitted).To(gomega.BeEmpty())
}

func TestPauseCampaignCompletesAtSafePointAfterRestart(t *testing.T) {
	g := gomega.NewWithT(t)
	runID := uuid.New()
	store := &schedulerStoreFake{
		acquired: true,
		lease:    types.CampaignLease{RunID: runID, FencingToken: 21},
		schedule: types.CampaignSchedule{
			Run: types.CampaignRun{
				ID:                runID,
				State:             types.CampaignRunStateRunning,
				AdmissionsBlocked: true,
				PauseRequested:    true,
			},
			AtSafePoint: true,
		},
	}

	result, err := NewScheduler(store, UnwiredCampaignObservationVerifier{}, "worker-a", time.Minute).
		Tick(context.Background(), runID, time.Now())
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result.Paused).To(gomega.BeTrue())
	g.Expect(store.finalizedPause).To(gomega.BeTrue())
	g.Expect(store.admitted).To(gomega.BeEmpty())
}
