package mapping

import (
	"slices"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
)

func AdmissionEvaluationToAPI(evaluation types.AdmissionEvaluation) api.AdmissionEvaluation {
	return api.AdmissionEvaluation{
		ID:                        evaluation.ID,
		CreatedAt:                 evaluation.CreatedAt,
		DeploymentPlanID:          evaluation.DeploymentPlanID,
		PlanRevision:              evaluation.PlanRevision,
		PlanChecksum:              evaluation.PlanChecksum,
		PlanSchema:                evaluation.PlanSchema,
		ProtocolVersion:           evaluation.ProtocolVersion,
		CampaignID:                evaluation.CampaignID,
		CampaignRevision:          evaluation.CampaignRevision,
		CampaignChecksum:          evaluation.CampaignChecksum,
		EffectivePolicyChecksum:   evaluation.EffectivePolicyChecksum,
		PolicyVersionIDs:          slices.Clone(evaluation.PolicyVersionIDs),
		CalendarVersionIDs:        slices.Clone(evaluation.CalendarVersionIDs),
		FreezeRevisionIDs:         slices.Clone(evaluation.FreezeRevisionIDs),
		ApprovalRequestID:         evaluation.ApprovalRequestID,
		ApprovalRequestRevision:   evaluation.ApprovalRequestRevision,
		EmergencyOverrideID:       evaluation.EmergencyOverrideID,
		EmergencyOverrideChecksum: evaluation.EmergencyOverrideChecksum,
		Decision:                  evaluation.Decision,
		ReasonCodes:               slices.Clone(evaluation.ReasonCodes),
		EvaluatedAt:               evaluation.EvaluatedAt,
		TemporalEvidence:          evaluation.TemporalEvidence,
		GateEvidence:              slices.Clone(evaluation.GateEvidence),
		MaterialChecksum:          evaluation.MaterialChecksum,
		DecisionChecksum:          evaluation.DecisionChecksum,
		SchedulerIdempotencyKey:   evaluation.SchedulerIdempotencyKey,
	}
}

func EmergencyOverrideToAPI(override types.EmergencyOverride) api.EmergencyOverride {
	return api.EmergencyOverride{
		ID:                      override.ID,
		CreatedAt:               override.CreatedAt,
		DeploymentPlanID:        override.DeploymentPlanID,
		PlanRevision:            override.PlanRevision,
		PlanChecksum:            override.PlanChecksum,
		EffectivePolicyChecksum: override.EffectivePolicyChecksum,
		Accelerations:           slices.Clone(override.Accelerations),
		Reason:                  override.Reason,
		ActorUserAccountID:      override.ActorUserAccountID,
		ApprovalEvidence:        slices.Clone(override.ApprovalEvidence),
		ExpiresAt:               override.ExpiresAt,
		Checksum:                override.Checksum,
		IdempotencyKey:          override.IdempotencyKey,
	}
}
