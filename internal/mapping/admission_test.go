package mapping

import (
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestAdmissionEvaluationToAPIExcludesTenantInternalFields(t *testing.T) {
	g := NewWithT(t)
	evaluation := types.AdmissionEvaluation{
		ID:                        uuid.New(),
		OrganizationID:            uuid.New(),
		DeploymentPlanID:          uuid.New(),
		PlanRevision:              3,
		PlanChecksum:              admissionMappingTestChecksum(),
		PlanSchema:                types.AdmissionRequiredPlanSchemaV2,
		ProtocolVersion:           types.AdmissionRequiredProtocolV2,
		EffectivePolicyChecksum:   admissionMappingTestChecksum(),
		Decision:                  types.AdmissionDecisionAdmit,
		ReasonCodes:               []types.AdmissionReasonCode{types.AdmissionReasonAdmitted},
		EvaluatedAt:               time.Date(2026, time.July, 18, 12, 0, 0, 0, time.UTC),
		MaterialChecksum:          admissionMappingTestChecksum(),
		DecisionChecksum:          admissionMappingTestChecksum(),
		SchedulerIdempotencyKey:   "scheduler:key",
		ActorUserAccountID:        uuid.New(),
		PolicyVersionIDs:          []uuid.UUID{uuid.New()},
		CalendarVersionIDs:        []uuid.UUID{},
		FreezeRevisionIDs:         []uuid.UUID{},
		GateEvidence:              []types.AdmissionGateEvidence{},
		TemporalEvidence:          types.AdmissionTemporalEvidence{},
		EmergencyOverrideChecksum: "",
	}

	response := AdmissionEvaluationToAPI(evaluation)

	g.Expect(response.ID).To(Equal(evaluation.ID))
	g.Expect(response.DeploymentPlanID).To(Equal(evaluation.DeploymentPlanID))
	g.Expect(response.Decision).To(Equal(types.AdmissionDecisionAdmit))
	g.Expect(response.DecisionChecksum).To(Equal(evaluation.DecisionChecksum))
}

func TestEmergencyOverrideToAPIPreservesChecksumBoundEvidence(t *testing.T) {
	g := NewWithT(t)
	override := types.EmergencyOverride{
		ID:                      uuid.New(),
		DeploymentPlanID:        uuid.New(),
		PlanRevision:            4,
		PlanChecksum:            admissionMappingTestChecksum(),
		EffectivePolicyChecksum: admissionMappingTestChecksum(),
		Accelerations: []types.EmergencyAcceleration{{
			GateKey:                types.AdmissionGateMaintenanceWait,
			MaxAccelerationSeconds: 600,
		}},
		ApprovalEvidence: []types.EmergencyOverrideApprovalEvidence{{
			RequestID:       uuid.New(),
			RequestRevision: 2,
			RequestChecksum: admissionMappingTestChecksum(),
			Eligible:        true,
		}},
		Checksum: admissionMappingTestChecksum(),
	}

	response := EmergencyOverrideToAPI(override)

	g.Expect(response.PlanChecksum).To(Equal(override.PlanChecksum))
	g.Expect(response.ApprovalEvidence).To(Equal(override.ApprovalEvidence))
	g.Expect(response.Checksum).To(Equal(override.Checksum))
}

func admissionMappingTestChecksum() string {
	return "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
}
