package scheduling

import (
	"context"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestEvaluateAdmissionWaitsForClosedWindowAndActiveFreeze(t *testing.T) {
	g := NewWithT(t)
	request := validAdmissionRequest()
	request.CalendarEvidence[0].Evaluation.Allowed = false
	request.CalendarEvidence[0].Evaluation.ReasonCode = types.CalendarReasonWindowClosed

	evaluation, err := EvaluateAdmission(context.Background(), request)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(evaluation.Decision).To(Equal(types.AdmissionDecisionWait))
	g.Expect(evaluation.ReasonCodes).To(ContainElement(types.AdmissionReasonMaintenanceWindowClosed))

	request = validAdmissionRequest()
	request.FreezeEvidence[0].Evaluation.Allowed = false
	request.FreezeEvidence[0].Evaluation.Blocked = true
	request.FreezeEvidence[0].Evaluation.ReasonCode = types.CalendarReasonFreezeActive
	request.FreezeEvidence[0].Evaluation.ActiveRevisionIDs = []uuid.UUID{
		request.FreezeEvidence[0].RevisionID,
	}

	evaluation, err = EvaluateAdmission(context.Background(), request)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(evaluation.Decision).To(Equal(types.AdmissionDecisionWait))
	g.Expect(evaluation.ReasonCodes).To(ContainElement(types.AdmissionReasonDeploymentFreezeActive))
}

func TestEvaluateAdmissionBlocksMissingApproval(t *testing.T) {
	g := NewWithT(t)
	request := validAdmissionRequest()
	request.Approval.Evaluation.Eligible = false
	request.Approval.Evaluation.State = types.ApprovalRequestStatePending

	evaluation, err := EvaluateAdmission(context.Background(), request)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(evaluation.Decision).To(Equal(types.AdmissionDecisionBlock))
	g.Expect(evaluation.ReasonCodes).To(ContainElement(types.AdmissionReasonApprovalMissing))
}

func TestEvaluateAdmissionMaterialChecksumIgnoresClockButPinsMaterialFacts(t *testing.T) {
	g := NewWithT(t)
	first := validAdmissionRequest()
	second := first
	second.CalendarEvidence = append([]types.AdmissionCalendarEvidence(nil), first.CalendarEvidence...)
	second.FreezeEvidence = append([]types.AdmissionFreezeEvidence(nil), first.FreezeEvidence...)
	second.EvaluatedAt = first.EvaluatedAt.Add(30 * time.Minute)
	second.CalendarEvidence[0].Evaluation.UTCInstant = second.EvaluatedAt
	second.FreezeEvidence[0].Evaluation.UTCInstant = second.EvaluatedAt

	firstEvaluation, err := EvaluateAdmission(context.Background(), first)
	g.Expect(err).NotTo(HaveOccurred())
	secondEvaluation, err := EvaluateAdmission(context.Background(), second)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(secondEvaluation.MaterialChecksum).To(Equal(firstEvaluation.MaterialChecksum))
	g.Expect(secondEvaluation.DecisionChecksum).NotTo(Equal(firstEvaluation.DecisionChecksum))

	second.EffectivePolicy.Checksum = testChecksum("changed-policy")
	secondEvaluation, err = EvaluateAdmission(context.Background(), second)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(secondEvaluation.MaterialChecksum).NotTo(Equal(firstEvaluation.MaterialChecksum))

	second = validAdmissionRequest()
	second.CalendarEvidence[0].Checksum = testChecksum("changed-calendar")
	secondEvaluation, err = EvaluateAdmission(context.Background(), second)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(secondEvaluation.MaterialChecksum).NotTo(Equal(firstEvaluation.MaterialChecksum))
}

func TestEmergencyOverrideAcceleratesOnlyWhitelistedWaits(t *testing.T) {
	g := NewWithT(t)
	request := validAdmissionRequest()
	request.CalendarEvidence[0].Evaluation.Allowed = false
	request.CalendarEvidence[0].Evaluation.ReasonCode = types.CalendarReasonWindowClosed
	override := validEmergencyOverride(request)
	override.Accelerations = []types.EmergencyAcceleration{{
		GateKey:                types.AdmissionGateMaintenanceWait,
		MaxAccelerationSeconds: 900,
	}}
	override.Checksum = EmergencyOverrideChecksum(override)
	request.EmergencyOverride = &override

	evaluation, err := EvaluateAdmission(context.Background(), request)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(evaluation.Decision).To(Equal(types.AdmissionDecisionAdmit))
	g.Expect(evaluation.ReasonCodes).To(ContainElement(types.AdmissionReasonEmergencyAcceleration))

	override.Accelerations = []types.EmergencyAcceleration{{
		GateKey:                types.AdmissionGateIntegrity,
		MaxAccelerationSeconds: 900,
	}}
	override.Checksum = EmergencyOverrideChecksum(override)
	request.EmergencyOverride = &override
	_, err = EvaluateAdmission(context.Background(), request)
	g.Expect(err).To(MatchError(ContainSubstring("protected admission gate")))
}

func TestEvaluateAdmissionNeverOverridesMandatoryGateEvidence(t *testing.T) {
	g := NewWithT(t)
	request := validAdmissionRequest()
	request.GateEvidence = append(request.GateEvidence, types.AdmissionGateEvidence{
		Key:       types.AdmissionGateMandatoryHealth,
		Mandatory: true,
		Satisfied: false,
		Checksum:  testChecksum("health-failed"),
	})
	override := validEmergencyOverride(request)
	override.Accelerations = []types.EmergencyAcceleration{{
		GateKey:                types.AdmissionGateMaintenanceWait,
		MaxAccelerationSeconds: 60,
	}}
	override.Checksum = EmergencyOverrideChecksum(override)
	request.EmergencyOverride = &override

	evaluation, err := EvaluateAdmission(context.Background(), request)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(evaluation.Decision).To(Equal(types.AdmissionDecisionBlock))
	g.Expect(evaluation.ReasonCodes).To(ContainElement(types.AdmissionReasonMandatoryGateFailed))
}

func TestEvaluateAdmissionRejectsOverrideOutsideItsExactLifetime(t *testing.T) {
	g := NewWithT(t)
	request := validAdmissionRequest()
	override := validEmergencyOverride(request)
	override.Accelerations = []types.EmergencyAcceleration{{
		GateKey:                types.AdmissionGateMaintenanceWait,
		MaxAccelerationSeconds: 60,
	}}
	override.CreatedAt = request.EvaluatedAt.Add(time.Minute)
	override.ExpiresAt = override.CreatedAt.Add(time.Hour)
	override.Checksum = EmergencyOverrideChecksum(override)
	request.EmergencyOverride = &override

	_, err := EvaluateAdmission(context.Background(), request)

	g.Expect(err).To(MatchError(ContainSubstring("expired or exceeds its maximum lifetime")))
}

func validAdmissionRequest() types.AdmissionRequest {
	evaluatedAt := time.Date(2026, time.July, 18, 12, 0, 0, 0, time.UTC)
	planID := uuid.New()
	policyVersionID := uuid.New()
	calendarVersionID := uuid.New()
	freezeRevisionID := uuid.New()
	approvalRequestID := uuid.New()
	return types.AdmissionRequest{
		OrganizationID: uuid.New(),
		Plan: types.AdmissionPlanEvidence{
			ID:              planID,
			Revision:        1,
			Checksum:        testChecksum("plan"),
			Schema:          types.AdmissionRequiredPlanSchemaV2,
			ProtocolVersion: types.AdmissionRequiredProtocolV2,
		},
		EffectivePolicy: types.EffectivePolicy{
			VersionIDs:            []uuid.UUID{policyVersionID},
			Checksum:              testChecksum("policy"),
			SubscriberSetChecksum: testChecksum("subscribers"),
			AdmissionRules: types.AdmissionRules{
				MaximumWaitSeconds:          3600,
				MaintenanceWindowVersionIDs: []uuid.UUID{calendarVersionID},
				FreezeRuleVersionIDs:        []uuid.UUID{freezeRevisionID},
			},
			OverrideRules: []types.OverrideRules{{
				Allowed:             true,
				AuthorityGroupID:    uuidPtr(uuid.New()),
				ShortenableGateKeys: []string{string(types.AdmissionGateMaintenanceWait)},
				MinimumReasonLength: 12,
				PolicyVersionID:     policyVersionID,
				AuthorityKind:       types.PolicyAuthorityOwner,
				AuthorityID:         uuid.New(),
			}},
			RequiredEvidence: []string{
				string(types.AdmissionGateIntegrity),
				string(types.AdmissionGateProvenance),
			},
		},
		CalendarEvidence: []types.AdmissionCalendarEvidence{{
			VersionID: calendarVersionID,
			Checksum:  testChecksum("calendar"),
			Evaluation: types.CalendarEvaluation{
				Allowed:            true,
				UTCInstant:         evaluatedAt,
				LocalTime:          evaluatedAt,
				IANAZone:           "UTC",
				RuleVersion:        "system:test",
				CalendarVersionID:  &calendarVersionID,
				ReasonCode:         types.CalendarReasonWindowOpen,
				EvaluationIdentity: testChecksum("calendar-evaluation"),
			},
		}},
		FreezeEvidence: []types.AdmissionFreezeEvidence{{
			RevisionID: freezeRevisionID,
			Checksum:   testChecksum("freeze"),
			Evaluation: types.FreezeEvaluation{
				Allowed:            true,
				UTCInstant:         evaluatedAt,
				LocalTime:          evaluatedAt,
				IANAZone:           "UTC",
				RuleVersion:        "system:test",
				ActiveRevisionIDs:  []uuid.UUID{},
				ReasonCode:         types.CalendarReasonNoActiveFreeze,
				EvaluationIdentity: testChecksum("freeze-evaluation"),
			},
		}},
		Approval: types.AdmissionApprovalEvidence{
			RequestID:               approvalRequestID,
			RequestRevision:         2,
			SubjectChecksum:         testChecksum("plan"),
			EffectivePolicyChecksum: testChecksum("policy"),
			SubscriberSetChecksum:   testChecksum("subscribers"),
			Evaluation: types.ApprovalEvaluation{
				RequestID: approvalRequestID,
				State:     types.ApprovalRequestStateApproved,
				Eligible:  true,
			},
		},
		GateEvidence: []types.AdmissionGateEvidence{
			{
				Key:       types.AdmissionGateIntegrity,
				Mandatory: true,
				Satisfied: true,
				Checksum:  testChecksum("integrity"),
			},
			{
				Key:       types.AdmissionGateProvenance,
				Mandatory: true,
				Satisfied: true,
				Checksum:  testChecksum("provenance"),
			},
		},
		EvaluatedAt: evaluatedAt,
	}
}

func validEmergencyOverride(request types.AdmissionRequest) types.EmergencyOverride {
	return types.EmergencyOverride{
		ID:                      uuid.New(),
		CreatedAt:               request.EvaluatedAt.Add(-time.Minute),
		OrganizationID:          request.OrganizationID,
		DeploymentPlanID:        request.Plan.ID,
		PlanRevision:            request.Plan.Revision,
		PlanChecksum:            request.Plan.Checksum,
		EffectivePolicyChecksum: request.EffectivePolicy.Checksum,
		Reason:                  "urgent customer recovery",
		ActorUserAccountID:      uuid.New(),
		ApprovalEvidence: []types.EmergencyOverrideApprovalEvidence{{
			RequestID:       request.Approval.RequestID,
			RequestRevision: request.Approval.RequestRevision,
			RequestChecksum: testChecksum("override-approval"),
			Eligible:        true,
		}},
		ExpiresAt: request.EvaluatedAt.Add(time.Hour),
	}
}

func testChecksum(seed string) string {
	const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	switch seed {
	case "changed-policy":
		return "sha256:1123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	case "changed-calendar":
		return "sha256:2123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	default:
		return "sha256:" + digest
	}
}

func uuidPtr(value uuid.UUID) *uuid.UUID {
	return &value
}
