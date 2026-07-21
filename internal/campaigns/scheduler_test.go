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
	pendingTasks       []types.Task
	materializedTasks  []types.Task
	authorizer         types.AdmissionAuthorizer
	recoveryLoads      int
}

func (s *schedulerStoreFake) LoadPendingCampaignDispatchTasks(
	_ context.Context,
	_ uuid.UUID,
	_ int64,
) ([]types.Task, error) {
	s.recoveryLoads++
	return s.pendingTasks, nil
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

func (s *schedulerStoreFake) RecordThresholdAndMaybePause(
	_ context.Context,
	evaluation types.CampaignThresholdEvaluation,
	_ int64,
) (bool, error) {
	s.thresholds = append(s.thresholds, evaluation)
	if evaluation.Breached {
		s.pausedReason = "campaign threshold breached"
	}
	return evaluation.Breached, nil
}

func (s *schedulerStoreFake) RecordPrerequisitesAndAdmit(
	ctx context.Context,
	candidate types.CampaignMemberCandidate,
	admission types.CampaignMemberAdmission,
	resolver CampaignObservationResolver,
	verifier CampaignObservationVerifier,
	authorizer types.AdmissionAuthorizer,
	_ int64,
) ([]types.Task, bool, bool, error) {
	s.authorizer = authorizer
	for _, requirement := range candidate.Prerequisites {
		observationID := requirement.ObservationID
		runtimeChecksum := requirement.RuntimeStateChecksum
		var verifyErr error
		if observationID == uuid.Nil || runtimeChecksum == "" {
			observationID, runtimeChecksum, verifyErr = resolver.ResolveCampaignObservation(
				ctx, requirement.OrganizationID, requirement.ProviderComponentInstanceID,
				requirement.ExpectedRuntimeStateChecksum,
			)
		}
		if verifyErr == nil {
			verifyErr = verifier.VerifyCampaignObservation(
				ctx, requirement.OrganizationID, observationID, runtimeChecksum,
			)
		}
		matched := verifyErr == nil && observationID != uuid.Nil &&
			runtimeChecksum == requirement.ExpectedRuntimeStateChecksum
		evaluation := types.CampaignPrerequisiteEvaluation{
			CampaignRunID: admission.RunID, MemberRunID: candidate.MemberRunID,
			UpstreamPlanID: requirement.UpstreamPlanID, StepKey: requirement.StepKey,
			ExpectedRuntimeStateChecksum: requirement.ExpectedRuntimeStateChecksum,
			ActualObservationID:          observationID, ActualRuntimeStateChecksum: runtimeChecksum,
			Matched: matched,
		}
		if verifyErr != nil {
			evaluation.ActualObservationID = uuid.Nil
			evaluation.ActualRuntimeStateChecksum = ""
			evaluation.Reason = verifyErr.Error()
		}
		s.prerequisites = append(s.prerequisites, evaluation)
		if !matched {
			if errors.Is(verifyErr, ErrCampaignObservationVerifierUnavailable) ||
				errors.Is(verifyErr, ErrCampaignObservationResolverUnavailable) {
				s.pausedReason = "trusted observation unavailable"
			} else {
				s.pausedReason = "campaign prerequisite mismatch"
			}
			return nil, false, true, nil
		}
	}
	if s.loseLeaseOnAdmit {
		return nil, false, false, ErrCampaignLeaseLost
	}
	if s.duplicateAdmission {
		return nil, false, false, nil
	}
	s.admitted = append(s.admitted, admission)
	if s.materializedTasks == nil {
		s.materializedTasks = []types.Task{{
			ID: admission.MemberRunID, ExecutionOccurrenceID: admission.MemberRunID,
		}}
	}
	return s.materializedTasks, true, false, nil
}

type campaignTaskDispatcherFake struct {
	batches [][]types.Task
	err     error
}

func (d *campaignTaskDispatcherFake) DispatchCampaignTasks(
	_ context.Context,
	tasks []types.Task,
) error {
	d.batches = append(d.batches, append([]types.Task(nil), tasks...))
	return d.err
}

func newSchedulerForTest(
	store SchedulerStore,
	observations CampaignObservationVerifier,
	workerID string,
	leaseDuration time.Duration,
) *Scheduler {
	return NewSchedulerWithRuntime(
		store,
		UnwiredCampaignObservationResolver{},
		observations,
		nil,
		&campaignTaskDispatcherFake{},
		workerID,
		leaseDuration,
	)
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
	scheduler := newSchedulerForTest(store, UnwiredCampaignObservationVerifier{}, "worker-a", time.Minute)

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
					OrganizationID:               uuid.New(),
					UpstreamPlanID:               uuid.New(),
					StepKey:                      "verify-ledger",
					ObservationID:                uuid.New(),
					RuntimeStateChecksum:         "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					ExpectedRuntimeStateChecksum: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				}},
			}},
		},
	}

	result, err := newSchedulerForTest(
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
	g.Expect(store.prerequisites[0].ExpectedRuntimeStateChecksum).NotTo(gomega.BeEmpty())
	g.Expect(store.prerequisites[0].ActualObservationID).To(gomega.Equal(uuid.Nil))
	g.Expect(store.prerequisites[0].ActualRuntimeStateChecksum).To(gomega.BeEmpty())
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
					OrganizationID:               uuid.New(),
					UpstreamPlanID:               uuid.New(),
					StepKey:                      "health",
					ObservationID:                observationID,
					RuntimeStateChecksum:         actual,
					ExpectedRuntimeStateChecksum: expected,
				}},
			}},
		},
	}
	scheduler := newSchedulerForTest(store, &observationVerifierFake{}, "worker-a", time.Minute)

	result, err := scheduler.Tick(context.Background(), runID, time.Now())
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result.Admitted).To(gomega.BeFalse())
	g.Expect(store.prerequisites).To(gomega.HaveLen(1))
	g.Expect(store.prerequisites[0].ExpectedRuntimeStateChecksum).To(gomega.Equal(expected))
	g.Expect(store.prerequisites[0].ActualObservationID).To(gomega.Equal(observationID))
	g.Expect(store.prerequisites[0].ActualRuntimeStateChecksum).To(gomega.Equal(actual))
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
					OrganizationID:               organizationID,
					ProviderPlacementID:          placementID,
					ProviderComponentInstanceID:  componentInstanceID,
					ExpectedRuntimeStateChecksum: checksum,
				}},
			}},
		},
	}
	resolver := &observationResolverFake{
		observationID: observationID,
		actual:        checksum,
	}
	verifier := &capturingObservationVerifier{}

	result, err := NewSchedulerWithRuntime(
		store,
		resolver,
		verifier,
		nil,
		&campaignTaskDispatcherFake{},
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
		result, err := newSchedulerForTest(
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

	result, err := newSchedulerForTest(store, UnwiredCampaignObservationVerifier{}, "worker-a", time.Minute).
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
	_, err = newSchedulerForTest(store, UnwiredCampaignObservationVerifier{}, "worker-a", time.Minute).
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
	scheduler := newSchedulerForTest(store, UnwiredCampaignObservationVerifier{}, "worker-a", time.Minute)
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

	result, err := newSchedulerForTest(store, UnwiredCampaignObservationVerifier{}, "worker-a", time.Minute).
		Tick(context.Background(), runID, time.Now())
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result.Paused).To(gomega.BeTrue())
	g.Expect(store.finalizedPause).To(gomega.BeTrue())
	g.Expect(store.admitted).To(gomega.BeEmpty())
}

func TestSchedulerDispatchesRecoveryBeforeMaterializedAdmissionTasks(t *testing.T) {
	g := gomega.NewWithT(t)
	runID := uuid.New()
	memberRunID := uuid.New()
	recovery := types.Task{ID: uuid.New(), ExecutionOccurrenceID: uuid.New()}
	created := types.Task{ID: uuid.New(), ExecutionOccurrenceID: memberRunID}
	authorizer := types.AdmissionAuthorizer(func(context.Context, types.AdmissionAuthorizationContext) error {
		return nil
	})
	store := &schedulerStoreFake{
		acquired:          true,
		lease:             types.CampaignLease{RunID: runID, FencingToken: 44},
		pendingTasks:      []types.Task{recovery},
		materializedTasks: []types.Task{created},
		schedule: types.CampaignSchedule{
			Run: types.CampaignRun{ID: runID, State: types.CampaignRunStateRunning},
			Candidates: []types.CampaignMemberCandidate{{
				MemberRunID: memberRunID,
				PlanID:      uuid.New(),
			}},
		},
	}
	dispatcher := &campaignTaskDispatcherFake{}

	result, err := NewSchedulerWithRuntime(
		store,
		UnwiredCampaignObservationResolver{},
		UnwiredCampaignObservationVerifier{},
		authorizer,
		dispatcher,
		"worker-a",
		time.Minute,
	).Tick(context.Background(), runID, time.Now())

	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result.Admitted).To(gomega.BeTrue())
	g.Expect(store.authorizer).NotTo(gomega.BeNil())
	g.Expect(dispatcher.batches).To(gomega.Equal([][]types.Task{{recovery}, {created}}))
}

func TestSchedulerStopsBeforeAdmissionWhenRecoveryDispatchFails(t *testing.T) {
	g := gomega.NewWithT(t)
	runID := uuid.New()
	store := &schedulerStoreFake{
		acquired:     true,
		lease:        types.CampaignLease{RunID: runID, FencingToken: 45},
		pendingTasks: []types.Task{{ID: uuid.New()}},
		schedule: types.CampaignSchedule{
			Run: types.CampaignRun{ID: runID, State: types.CampaignRunStateRunning},
			Candidates: []types.CampaignMemberCandidate{{
				MemberRunID: uuid.New(), PlanID: uuid.New(),
			}},
		},
	}
	dispatcher := &campaignTaskDispatcherFake{err: errors.New("dispatcher unavailable")}

	result, err := NewSchedulerWithRuntime(
		store,
		UnwiredCampaignObservationResolver{},
		UnwiredCampaignObservationVerifier{},
		func(context.Context, types.AdmissionAuthorizationContext) error { return nil },
		dispatcher,
		"worker-a",
		time.Minute,
	).Tick(context.Background(), runID, time.Now())

	g.Expect(err).To(gomega.MatchError("dispatcher unavailable"))
	g.Expect(result.Admitted).To(gomega.BeFalse())
	g.Expect(store.admitted).To(gomega.BeEmpty())
}

func TestSchedulerNeverDispatchesWhilePaused(t *testing.T) {
	g := gomega.NewWithT(t)
	runID := uuid.New()
	store := &schedulerStoreFake{
		acquired:     true,
		lease:        types.CampaignLease{RunID: runID, FencingToken: 46},
		pendingTasks: []types.Task{{ID: uuid.New()}},
		schedule: types.CampaignSchedule{
			Run: types.CampaignRun{ID: runID, State: types.CampaignRunStatePaused},
		},
	}
	dispatcher := &campaignTaskDispatcherFake{}

	_, err := NewSchedulerWithRuntime(
		store,
		UnwiredCampaignObservationResolver{},
		UnwiredCampaignObservationVerifier{},
		func(context.Context, types.AdmissionAuthorizationContext) error { return nil },
		dispatcher,
		"worker-a",
		time.Minute,
	).Tick(context.Background(), runID, time.Now())

	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(dispatcher.batches).To(gomega.BeEmpty())
	g.Expect(store.recoveryLoads).To(gomega.Equal(0))
}

func TestSchedulerDoesNotCallDispatcherWhenNothingWasAdmittedOrRecovered(t *testing.T) {
	g := gomega.NewWithT(t)
	runID := uuid.New()
	store := &schedulerStoreFake{
		acquired:           true,
		duplicateAdmission: true,
		lease:              types.CampaignLease{RunID: runID, FencingToken: 47},
		schedule: types.CampaignSchedule{
			Run: types.CampaignRun{ID: runID, State: types.CampaignRunStateRunning},
			Candidates: []types.CampaignMemberCandidate{{
				MemberRunID: uuid.New(), PlanID: uuid.New(),
			}},
		},
	}
	dispatcher := &campaignTaskDispatcherFake{}

	result, err := NewSchedulerWithRuntime(
		store,
		UnwiredCampaignObservationResolver{},
		UnwiredCampaignObservationVerifier{},
		func(context.Context, types.AdmissionAuthorizationContext) error { return nil },
		dispatcher,
		"worker-a",
		time.Minute,
	).Tick(context.Background(), runID, time.Now())

	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(result.Admitted).To(gomega.BeFalse())
	g.Expect(dispatcher.batches).To(gomega.BeEmpty())
}

func TestNilCampaignTaskDispatcherFunctionFailsClosed(t *testing.T) {
	g := gomega.NewWithT(t)
	err := CampaignTaskDispatcherFunc(nil).DispatchCampaignTasks(
		context.Background(),
		[]types.Task{{ID: uuid.New()}},
	)
	g.Expect(err).To(gomega.MatchError(ErrCampaignTaskDispatcherUnavailable))
}
