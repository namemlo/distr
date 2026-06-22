package deployments

import (
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

type RollingTargetState string

const (
	RollingTargetPending    RollingTargetState = "PENDING"
	RollingTargetInProgress RollingTargetState = "IN_PROGRESS"
	RollingTargetSucceeded  RollingTargetState = "SUCCEEDED"
	RollingTargetFailed     RollingTargetState = "FAILED"
	RollingTargetSkipped    RollingTargetState = "SKIPPED"
)

func (s RollingTargetState) IsValid() bool {
	switch s {
	case RollingTargetPending,
		RollingTargetInProgress,
		RollingTargetSucceeded,
		RollingTargetFailed,
		RollingTargetSkipped:
		return true
	default:
		return false
	}
}

func (s RollingTargetState) IsTerminal() bool {
	return s == RollingTargetSucceeded || s == RollingTargetFailed || s == RollingTargetSkipped
}

type RollingPhase string

const (
	RollingPhasePending    RollingPhase = "PENDING"
	RollingPhaseInProgress RollingPhase = "IN_PROGRESS"
	RollingPhasePaused     RollingPhase = "PAUSED"
	RollingPhaseAborted    RollingPhase = "ABORTED"
	RollingPhaseCompleted  RollingPhase = "COMPLETED"
)

type RollingFailureAction string

const (
	RollingFailureActionPause RollingFailureAction = "PAUSE"
	RollingFailureActionAbort RollingFailureAction = "ABORT"
)

type RollingFailureThreshold struct {
	MaxFailedTargets     int
	MaxFailurePercentage int
	Action               RollingFailureAction
}

type RollingConfig struct {
	WindowSize          int
	MaximumUnavailable  int
	PauseBetweenWindows time.Duration
	FailureThreshold    RollingFailureThreshold
}

func (c RollingConfig) Validate() error {
	if c.WindowSize <= 0 {
		return fmt.Errorf("window size must be greater than zero")
	}
	if c.MaximumUnavailable < 0 {
		return fmt.Errorf("maximum unavailable must not be negative")
	}
	if c.PauseBetweenWindows < 0 {
		return fmt.Errorf("pause between windows must not be negative")
	}
	return c.FailureThreshold.Validate()
}

func (t RollingFailureThreshold) Validate() error {
	if t.MaxFailedTargets < 0 {
		return fmt.Errorf("max failed targets must not be negative")
	}
	if t.MaxFailurePercentage < 0 || t.MaxFailurePercentage > 100 {
		return fmt.Errorf("max failure percentage must be between 0 and 100")
	}
	if t.enabled() && !t.action().IsValid() {
		return fmt.Errorf("failure threshold action must be PAUSE or ABORT")
	}
	return nil
}

func (t RollingFailureThreshold) enabled() bool {
	return t.MaxFailedTargets > 0 || t.MaxFailurePercentage > 0
}

func (t RollingFailureThreshold) action() RollingFailureAction {
	if t.Action == "" {
		return RollingFailureActionPause
	}
	return t.Action
}

func (a RollingFailureAction) IsValid() bool {
	return a == RollingFailureActionPause || a == RollingFailureActionAbort
}

func (a RollingFailureAction) phase() RollingPhase {
	if a == RollingFailureActionAbort {
		return RollingPhaseAborted
	}
	return RollingPhasePaused
}

type RollingTarget struct {
	DeploymentPlanTargetID uuid.UUID
	DeploymentTargetID     uuid.UUID
	SortOrder              int
	State                  RollingTargetState
	WindowNumber           int
	FailureReason          string
}

type RollingState struct {
	Config        RollingConfig
	Phase         RollingPhase
	CurrentWindow int
	Targets       []RollingTarget
}

func NewRollingState(config RollingConfig, targets []RollingTarget) (*RollingState, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("at least one rolling target is required")
	}
	normalized := make([]RollingTarget, len(targets))
	seenPlanTargets := map[uuid.UUID]struct{}{}
	for i, target := range targets {
		if target.DeploymentPlanTargetID == uuid.Nil {
			return nil, fmt.Errorf("deployment plan target ID is required")
		}
		if target.DeploymentTargetID == uuid.Nil {
			return nil, fmt.Errorf("deployment target ID is required")
		}
		if _, ok := seenPlanTargets[target.DeploymentPlanTargetID]; ok {
			return nil, fmt.Errorf("deployment plan target IDs must be unique")
		}
		seenPlanTargets[target.DeploymentPlanTargetID] = struct{}{}
		target.State = RollingTargetPending
		target.WindowNumber = 0
		target.FailureReason = ""
		normalized[i] = target
	}
	sort.SliceStable(normalized, func(i, j int) bool {
		if normalized[i].SortOrder != normalized[j].SortOrder {
			return normalized[i].SortOrder < normalized[j].SortOrder
		}
		return normalized[i].DeploymentPlanTargetID.String() < normalized[j].DeploymentPlanTargetID.String()
	})
	return &RollingState{
		Config:  config,
		Phase:   RollingPhasePending,
		Targets: normalized,
	}, nil
}

func (s *RollingState) StartNextWindow() ([]RollingTarget, error) {
	if s.Phase == RollingPhasePaused || s.Phase == RollingPhaseAborted || s.Phase == RollingPhaseCompleted {
		return []RollingTarget{}, nil
	}
	if !s.currentWindowTerminal() {
		return []RollingTarget{}, nil
	}
	pending := s.pendingTargetIndexes()
	if len(pending) == 0 {
		s.Phase = RollingPhaseCompleted
		return []RollingTarget{}, nil
	}
	limit := s.effectiveWindowSize()
	if limit > len(pending) {
		limit = len(pending)
	}
	s.CurrentWindow++
	started := make([]RollingTarget, 0, limit)
	for _, index := range pending[:limit] {
		s.Targets[index].State = RollingTargetInProgress
		s.Targets[index].WindowNumber = s.CurrentWindow
		started = append(started, s.Targets[index])
	}
	s.Phase = RollingPhaseInProgress
	return started, nil
}

func (s *RollingState) TransitionTarget(
	deploymentPlanTargetID uuid.UUID,
	state RollingTargetState,
	failureReason string,
) error {
	if !state.IsValid() {
		return fmt.Errorf("invalid rolling target state %q", state)
	}
	index := s.targetIndex(deploymentPlanTargetID)
	if index < 0 {
		return fmt.Errorf("rolling target %s not found", deploymentPlanTargetID)
	}
	s.Targets[index].State = state
	if state == RollingTargetFailed {
		s.Targets[index].FailureReason = failureReason
	} else {
		s.Targets[index].FailureReason = ""
	}
	s.updatePhase()
	return nil
}

func (s *RollingState) effectiveWindowSize() int {
	if s.Config.MaximumUnavailable > 0 && s.Config.MaximumUnavailable < s.Config.WindowSize {
		return s.Config.MaximumUnavailable
	}
	return s.Config.WindowSize
}

func (s *RollingState) currentWindowTerminal() bool {
	if s.CurrentWindow == 0 {
		return true
	}
	for _, target := range s.Targets {
		if target.WindowNumber == s.CurrentWindow && !target.State.IsTerminal() {
			return false
		}
	}
	return true
}

func (s *RollingState) pendingTargetIndexes() []int {
	indexes := []int{}
	for i, target := range s.Targets {
		if target.State == RollingTargetPending {
			indexes = append(indexes, i)
		}
	}
	return indexes
}

func (s *RollingState) targetIndex(deploymentPlanTargetID uuid.UUID) int {
	for i, target := range s.Targets {
		if target.DeploymentPlanTargetID == deploymentPlanTargetID {
			return i
		}
	}
	return -1
}

func (s *RollingState) updatePhase() {
	if thresholdPhase, ok := s.failureThresholdPhase(); ok {
		s.Phase = thresholdPhase
		return
	}
	if s.allTargetsTerminal() {
		s.Phase = RollingPhaseCompleted
		return
	}
	if s.currentWindowTerminal() {
		s.Phase = RollingPhasePending
		return
	}
	s.Phase = RollingPhaseInProgress
}

func (s *RollingState) failureThresholdPhase() (RollingPhase, bool) {
	threshold := s.Config.FailureThreshold
	if !threshold.enabled() {
		return "", false
	}
	failed := 0
	for _, target := range s.Targets {
		if target.State == RollingTargetFailed {
			failed++
		}
	}
	if threshold.MaxFailedTargets > 0 && failed >= threshold.MaxFailedTargets {
		return threshold.action().phase(), true
	}
	if threshold.MaxFailurePercentage > 0 && failed*100 >= threshold.MaxFailurePercentage*len(s.Targets) {
		return threshold.action().phase(), true
	}
	return "", false
}

func (s *RollingState) allTargetsTerminal() bool {
	for _, target := range s.Targets {
		if !target.State.IsTerminal() {
			return false
		}
	}
	return true
}
