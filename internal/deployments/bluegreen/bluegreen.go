package bluegreen

import (
	"fmt"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/traffic/provider"
	"github.com/google/uuid"
)

type SlotState string

const (
	SlotStateActive     SlotState = "ACTIVE"
	SlotStateInactive   SlotState = "INACTIVE"
	SlotStateDeploying  SlotState = "DEPLOYING"
	SlotStateReady      SlotState = "READY"
	SlotStateShifting   SlotState = "SHIFTING"
	SlotStateRetained   SlotState = "RETAINED"
	SlotStateScaledDown SlotState = "SCALED_DOWN"
	SlotStateDestroyed  SlotState = "DESTROYED"
	SlotStateRolledBack SlotState = "ROLLED_BACK"
)

type Phase string

const (
	PhaseCreate    Phase = "CREATE"
	PhaseDeploy    Phase = "DEPLOY"
	PhaseVerify    Phase = "VERIFY"
	PhaseShift     Phase = "SHIFT"
	PhaseObserve   Phase = "OBSERVE"
	PhasePromote   Phase = "PROMOTE"
	PhaseCleanup   Phase = "CLEANUP"
	PhaseRollback  Phase = "ROLLBACK"
	PhaseCompleted Phase = "COMPLETED"
)

type RetentionPolicy string

const (
	RetentionPolicyKeep      RetentionPolicy = "KEEP"
	RetentionPolicyScaleDown RetentionPolicy = "SCALE_DOWN"
	RetentionPolicyDestroy   RetentionPolicy = "DESTROY"
)

func (p RetentionPolicy) IsValid() bool {
	return p == RetentionPolicyKeep || p == RetentionPolicyScaleDown || p == RetentionPolicyDestroy
}

type HealthStatus string

const (
	HealthStatusPending HealthStatus = "PENDING"
	HealthStatusPassed  HealthStatus = "PASSED"
	HealthStatusFailed  HealthStatus = "FAILED"
)

func (s HealthStatus) IsValid() bool {
	return s == HealthStatusPending || s == HealthStatusPassed || s == HealthStatusFailed
}

type Slot struct {
	Name                   string            `json:"name"`
	State                  SlotState         `json:"state"`
	DeploymentPlanTargetID uuid.UUID         `json:"deploymentPlanTargetId,omitempty"`
	DeploymentTargetID     uuid.UUID         `json:"deploymentTargetId,omitempty"`
	DeploymentRevisionID   uuid.UUID         `json:"deploymentRevisionId,omitempty"`
	ApplicationVersionID   uuid.UUID         `json:"applicationVersionId,omitempty"`
	ProviderTargetID       string            `json:"providerTargetId,omitempty"`
	Metadata               map[string]string `json:"metadata,omitempty"`
}

type HealthCheck struct {
	Name      string       `json:"name"`
	Status    HealthStatus `json:"status"`
	Message   string       `json:"message,omitempty"`
	CheckedAt time.Time    `json:"checkedAt,omitempty"`
}

type Config struct {
	RolloutID        uuid.UUID
	DeploymentPlanID uuid.UUID
	Strategy         string
	ActiveSlot       Slot
	InactiveSlot     Slot
	RetentionPolicy  RetentionPolicy
}

type Lifecycle struct {
	RolloutID          uuid.UUID
	DeploymentPlanID   uuid.UUID
	Strategy           string
	Phase              Phase
	ActiveSlot         Slot
	InactiveSlot       Slot
	PreviousActiveSlot Slot
	RetentionPolicy    RetentionPolicy
	HealthChecks       []HealthCheck
	Observations       []HealthCheck
	RollbackReason     string
}

func NewLifecycle(config Config) (*Lifecycle, error) {
	if config.RolloutID == uuid.Nil {
		return nil, fmt.Errorf("rollout ID is required")
	}
	activeSlot, err := normalizeSlot(config.ActiveSlot)
	if err != nil {
		return nil, fmt.Errorf("active slot is invalid: %w", err)
	}
	inactiveSlot, err := normalizeSlot(config.InactiveSlot)
	if err != nil {
		return nil, fmt.Errorf("inactive slot is invalid: %w", err)
	}
	if strings.EqualFold(activeSlot.Name, inactiveSlot.Name) {
		return nil, fmt.Errorf("active and inactive slots must be different")
	}
	if !config.RetentionPolicy.IsValid() {
		return nil, fmt.Errorf("retention policy is invalid")
	}
	activeSlot.State = SlotStateActive
	inactiveSlot.State = SlotStateInactive
	strategy := strings.TrimSpace(config.Strategy)
	if strategy == "" {
		strategy = "blue-green"
	}
	return &Lifecycle{
		RolloutID:        config.RolloutID,
		DeploymentPlanID: config.DeploymentPlanID,
		Strategy:         strategy,
		Phase:            PhaseCreate,
		ActiveSlot:       activeSlot,
		InactiveSlot:     inactiveSlot,
		RetentionPolicy:  config.RetentionPolicy,
	}, nil
}

func (l *Lifecycle) StartDeployInactive() error {
	if l.Phase != PhaseCreate {
		return fmt.Errorf("inactive slot deployment can start only from CREATE phase")
	}
	l.Phase = PhaseDeploy
	l.InactiveSlot.State = SlotStateDeploying
	return nil
}

func (l *Lifecycle) MarkInactiveDeployed() error {
	if l.Phase != PhaseDeploy {
		return fmt.Errorf("inactive slot can be marked deployed only from DEPLOY phase")
	}
	l.Phase = PhaseVerify
	l.InactiveSlot.State = SlotStateReady
	return nil
}

func (l *Lifecycle) RecordHealthCheck(check HealthCheck) error {
	if l.Phase != PhaseVerify {
		return fmt.Errorf("health checks can be recorded only during VERIFY phase")
	}
	normalized, err := normalizeHealthCheck(check)
	if err != nil {
		return err
	}
	l.HealthChecks = upsertHealthCheck(l.HealthChecks, normalized)
	return nil
}

func (l *Lifecycle) ReadyForTrafficShift() bool {
	return allChecksPassed(l.HealthChecks)
}

func (l *Lifecycle) PlanTrafficShift() (provider.ShiftRequest, error) {
	if !l.ReadyForTrafficShift() {
		return provider.ShiftRequest{}, fmt.Errorf("health checks are not passing")
	}
	l.Phase = PhaseShift
	l.InactiveSlot.State = SlotStateShifting
	return provider.ShiftRequest{
		RolloutContext: l.rolloutContext("blue-green-shift-"),
		Targets:        l.providerTargets(),
		Parameters: map[string]any{
			"operation":    string(provider.OperationShift),
			"activeSlot":   l.ActiveSlot.Name,
			"inactiveSlot": l.InactiveSlot.Name,
		},
	}, nil
}

func (l *Lifecycle) MarkTrafficShifted() error {
	if l.Phase != PhaseShift {
		return fmt.Errorf("traffic can be marked shifted only from SHIFT phase")
	}
	l.Phase = PhaseObserve
	return nil
}

func (l *Lifecycle) RecordObservation(check HealthCheck) error {
	if l.Phase != PhaseObserve {
		return fmt.Errorf("observations can be recorded only during OBSERVE phase")
	}
	normalized, err := normalizeHealthCheck(check)
	if err != nil {
		return err
	}
	l.Observations = upsertHealthCheck(l.Observations, normalized)
	return nil
}

func (l *Lifecycle) Promote() error {
	if l.Phase != PhaseObserve {
		return fmt.Errorf("blue-green deployment can be promoted only from OBSERVE phase")
	}
	if !allChecksPassed(l.Observations) {
		return fmt.Errorf("observation checks are not passing")
	}
	l.Phase = PhasePromote
	previousActive := l.ActiveSlot
	previousActive.State = l.retainedSlotState()
	l.PreviousActiveSlot = previousActive
	l.ActiveSlot = l.InactiveSlot
	l.ActiveSlot.State = SlotStateActive
	l.InactiveSlot = Slot{}
	l.Phase = PhaseCompleted
	return nil
}

func (l *Lifecycle) PlanRollback(reason string) (provider.RollbackRequest, error) {
	if l.Phase != PhaseShift && l.Phase != PhaseObserve {
		return provider.RollbackRequest{}, fmt.Errorf("rollback can be planned only from SHIFT or OBSERVE phase")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "rollback requested"
	}
	l.Phase = PhaseRollback
	l.RollbackReason = reason
	l.ActiveSlot.State = SlotStateActive
	l.InactiveSlot.State = SlotStateRolledBack
	return provider.RollbackRequest{
		RolloutContext: l.rolloutContext("blue-green-rollback-"),
		Targets:        l.providerTargets(),
		Parameters: map[string]any{
			"operation":    string(provider.OperationRollback),
			"activeSlot":   l.ActiveSlot.Name,
			"inactiveSlot": l.InactiveSlot.Name,
			"reason":       reason,
		},
	}, nil
}

func (l *Lifecycle) retainedSlotState() SlotState {
	switch l.RetentionPolicy {
	case RetentionPolicyScaleDown:
		return SlotStateScaledDown
	case RetentionPolicyDestroy:
		return SlotStateDestroyed
	default:
		return SlotStateRetained
	}
}

func (l *Lifecycle) rolloutContext(prefix string) provider.RolloutContext {
	return provider.RolloutContext{
		DeploymentPlanID: l.DeploymentPlanID,
		RolloutID:        l.RolloutID,
		Strategy:         l.Strategy,
		IdempotencyKey:   prefix + l.RolloutID.String(),
		ProviderMetadata: map[string]string{
			"activeSlot":   l.ActiveSlot.Name,
			"inactiveSlot": l.InactiveSlot.Name,
		},
	}
}

func (l *Lifecycle) providerTargets() provider.TargetSet {
	return provider.TargetSet{Targets: []provider.Target{
		slotProviderTarget(l.ActiveSlot),
		slotProviderTarget(l.InactiveSlot),
	}}
}

func slotProviderTarget(slot Slot) provider.Target {
	return provider.Target{
		DeploymentPlanTargetID: slot.DeploymentPlanTargetID,
		DeploymentTargetID:     slot.DeploymentTargetID,
		Name:                   slot.Name,
		State:                  string(slot.State),
		Metadata: map[string]string{
			"providerTargetId": slot.ProviderTargetID,
			"slot":             slot.Name,
		},
	}
}

func normalizeSlot(slot Slot) (Slot, error) {
	slot.Name = strings.TrimSpace(slot.Name)
	if slot.Name == "" {
		return Slot{}, fmt.Errorf("name is required")
	}
	if slot.DeploymentTargetID == uuid.Nil {
		return Slot{}, fmt.Errorf("deployment target ID is required")
	}
	if slot.ProviderTargetID == "" {
		slot.ProviderTargetID = slot.Name
	}
	return slot, nil
}

func normalizeHealthCheck(check HealthCheck) (HealthCheck, error) {
	check.Name = strings.TrimSpace(check.Name)
	if check.Name == "" {
		return HealthCheck{}, fmt.Errorf("health check name is required")
	}
	if !check.Status.IsValid() {
		return HealthCheck{}, fmt.Errorf("health check status is invalid")
	}
	return check, nil
}

func upsertHealthCheck(checks []HealthCheck, check HealthCheck) []HealthCheck {
	for i, existing := range checks {
		if strings.EqualFold(existing.Name, check.Name) {
			checks[i] = check
			return checks
		}
	}
	return append(checks, check)
}

func allChecksPassed(checks []HealthCheck) bool {
	if len(checks) == 0 {
		return false
	}
	for _, check := range checks {
		if check.Status != HealthStatusPassed {
			return false
		}
	}
	return true
}
