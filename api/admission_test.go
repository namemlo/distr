package api

import (
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestAdmissionRequestValidate(t *testing.T) {
	g := NewWithT(t)
	request := AdmitDeploymentPlanRequest{
		SchedulerIdempotencyKey: "scheduler:plan:revision-1",
	}

	g.Expect(request.Validate()).To(Succeed())

	request.SchedulerIdempotencyKey = ""
	g.Expect(request.Validate()).To(MatchError(ContainSubstring("schedulerIdempotencyKey")))
}

func TestCreateEmergencyOverrideRequestRejectsProtectedAndUnapprovedAccelerations(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, time.July, 18, 12, 0, 0, 0, time.UTC)
	request := CreateEmergencyOverrideRequest{
		Accelerations: []types.EmergencyAcceleration{{
			GateKey:                types.AdmissionGateIntegrity,
			MaxAccelerationSeconds: 300,
		}},
		Reason:             "restore a critical customer environment",
		ApprovalRequestIDs: []uuid.UUID{uuid.New()},
		ExpiresAt:          now.Add(time.Hour),
		IdempotencyKey:     "override:incident-42",
	}

	g.Expect(request.Validate(now)).To(MatchError(ContainSubstring("protected")))

	request.Accelerations[0].GateKey = types.AdmissionGateMaintenanceWait
	g.Expect(request.Validate(now)).To(Succeed())

	request.ApprovalRequestIDs = nil
	g.Expect(request.Validate(now)).To(MatchError(ContainSubstring("approvalRequestIds")))
}

func admissionAPITestChecksum() string {
	return "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
}
