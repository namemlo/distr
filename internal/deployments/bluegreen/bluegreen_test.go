package bluegreen

import (
	"testing"

	"github.com/distr-sh/distr/internal/traffic/provider"
	. "github.com/onsi/gomega"

	"github.com/google/uuid"
)

func TestNewLifecycleInitializesActiveAndInactiveSlots(t *testing.T) {
	g := NewWithT(t)
	activeTargetID := uuid.New()
	inactiveTargetID := uuid.New()

	lifecycle, err := NewLifecycle(Config{
		RolloutID:        uuid.New(),
		DeploymentPlanID: uuid.New(),
		Strategy:         "blue-green",
		ActiveSlot:       Slot{Name: "blue", DeploymentTargetID: activeTargetID, ProviderTargetID: "blue-slot"},
		InactiveSlot:     Slot{Name: "green", DeploymentTargetID: inactiveTargetID, ProviderTargetID: "green-slot"},
		RetentionPolicy:  RetentionPolicyKeep,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(lifecycle.Phase).To(Equal(PhaseCreate))
	g.Expect(lifecycle.ActiveSlot.State).To(Equal(SlotStateActive))
	g.Expect(lifecycle.InactiveSlot.State).To(Equal(SlotStateInactive))
	g.Expect(lifecycle.ActiveSlot.DeploymentTargetID).To(Equal(activeTargetID))
	g.Expect(lifecycle.InactiveSlot.DeploymentTargetID).To(Equal(inactiveTargetID))
}

func TestLifecycleRequiresPassingHealthChecksBeforeTrafficShift(t *testing.T) {
	g := NewWithT(t)
	lifecycle := mustLifecycle(t, RetentionPolicyKeep)
	g.Expect(lifecycle.StartDeployInactive()).To(Succeed())
	g.Expect(lifecycle.MarkInactiveDeployed()).To(Succeed())
	g.Expect(lifecycle.RecordHealthCheck(HealthCheck{Name: "readiness", Status: HealthStatusPassed})).To(Succeed())
	g.Expect(lifecycle.RecordHealthCheck(HealthCheck{Name: "smoke", Status: HealthStatusFailed, Message: "500"})).To(Succeed())

	_, err := lifecycle.PlanTrafficShift()

	g.Expect(err).To(MatchError(ContainSubstring("health checks are not passing")))
	g.Expect(lifecycle.ReadyForTrafficShift()).To(BeFalse())

	g.Expect(lifecycle.RecordHealthCheck(HealthCheck{Name: "smoke", Status: HealthStatusPassed})).To(Succeed())
	shift, err := lifecycle.PlanTrafficShift()

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(lifecycle.ReadyForTrafficShift()).To(BeTrue())
	g.Expect(lifecycle.Phase).To(Equal(PhaseShift))
	g.Expect(shift.RolloutContext.Strategy).To(Equal("blue-green"))
	g.Expect(shift.RolloutContext.IdempotencyKey).To(Equal("blue-green-shift-" + lifecycle.RolloutID.String()))
	g.Expect(shift.Targets.Targets).To(HaveLen(2))
	g.Expect(shift.Parameters).To(HaveKeyWithValue("operation", string(provider.OperationShift)))
}

func TestLifecyclePromotesInactiveSlotAndAppliesRetentionPolicy(t *testing.T) {
	g := NewWithT(t)
	lifecycle := mustLifecycle(t, RetentionPolicyDestroy)
	g.Expect(lifecycle.StartDeployInactive()).To(Succeed())
	g.Expect(lifecycle.MarkInactiveDeployed()).To(Succeed())
	g.Expect(lifecycle.RecordHealthCheck(HealthCheck{Name: "readiness", Status: HealthStatusPassed})).To(Succeed())
	_, err := lifecycle.PlanTrafficShift()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(lifecycle.MarkTrafficShifted()).To(Succeed())
	g.Expect(lifecycle.RecordObservation(HealthCheck{Name: "post-shift", Status: HealthStatusPassed})).To(Succeed())

	g.Expect(lifecycle.Promote()).To(Succeed())

	g.Expect(lifecycle.Phase).To(Equal(PhaseCompleted))
	g.Expect(lifecycle.ActiveSlot.Name).To(Equal("green"))
	g.Expect(lifecycle.ActiveSlot.State).To(Equal(SlotStateActive))
	g.Expect(lifecycle.PreviousActiveSlot.Name).To(Equal("blue"))
	g.Expect(lifecycle.PreviousActiveSlot.State).To(Equal(SlotStateDestroyed))
}

func TestLifecyclePlansRollbackFromObservationPhase(t *testing.T) {
	g := NewWithT(t)
	lifecycle := mustLifecycle(t, RetentionPolicyScaleDown)
	g.Expect(lifecycle.StartDeployInactive()).To(Succeed())
	g.Expect(lifecycle.MarkInactiveDeployed()).To(Succeed())
	g.Expect(lifecycle.RecordHealthCheck(HealthCheck{Name: "readiness", Status: HealthStatusPassed})).To(Succeed())
	_, err := lifecycle.PlanTrafficShift()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(lifecycle.MarkTrafficShifted()).To(Succeed())

	rollback, err := lifecycle.PlanRollback("post-shift health degraded")

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(lifecycle.Phase).To(Equal(PhaseRollback))
	g.Expect(lifecycle.InactiveSlot.State).To(Equal(SlotStateRolledBack))
	g.Expect(lifecycle.ActiveSlot.State).To(Equal(SlotStateActive))
	g.Expect(lifecycle.RollbackReason).To(Equal("post-shift health degraded"))
	g.Expect(rollback.RolloutContext.IdempotencyKey).To(Equal("blue-green-rollback-" + lifecycle.RolloutID.String()))
	g.Expect(rollback.Parameters).To(HaveKeyWithValue("reason", "post-shift health degraded"))
}

func TestNewLifecycleRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{name: "missing rollout", config: Config{ActiveSlot: slot("blue"), InactiveSlot: slot("green"), RetentionPolicy: RetentionPolicyKeep}},
		{name: "missing active slot", config: Config{RolloutID: uuid.New(), InactiveSlot: slot("green"), RetentionPolicy: RetentionPolicyKeep}},
		{name: "missing inactive slot", config: Config{RolloutID: uuid.New(), ActiveSlot: slot("blue"), RetentionPolicy: RetentionPolicyKeep}},
		{name: "same slot names", config: Config{RolloutID: uuid.New(), ActiveSlot: slot("blue"), InactiveSlot: slot(" blue "), RetentionPolicy: RetentionPolicyKeep}},
		{name: "invalid retention", config: Config{RolloutID: uuid.New(), ActiveSlot: slot("blue"), InactiveSlot: slot("green"), RetentionPolicy: "archive"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			_, err := NewLifecycle(tt.config)

			g.Expect(err).To(HaveOccurred())
		})
	}
}

func mustLifecycle(t *testing.T, retentionPolicy RetentionPolicy) *Lifecycle {
	t.Helper()
	lifecycle, err := NewLifecycle(Config{
		RolloutID:        uuid.New(),
		DeploymentPlanID: uuid.New(),
		Strategy:         "blue-green",
		ActiveSlot:       slot("blue"),
		InactiveSlot:     slot("green"),
		RetentionPolicy:  retentionPolicy,
	})
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return lifecycle
}

func slot(name string) Slot {
	return Slot{
		Name:                   name,
		DeploymentPlanTargetID: uuid.New(),
		DeploymentTargetID:     uuid.New(),
		ProviderTargetID:       name + "-slot",
		DeploymentRevisionID:   uuid.New(),
		ApplicationVersionID:   uuid.New(),
	}
}
