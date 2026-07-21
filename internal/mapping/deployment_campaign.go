package mapping

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

func CampaignDraftToAPI(draft types.CampaignDraft) api.DeploymentCampaignDraft {
	return api.DeploymentCampaignDraft{
		ID:             draft.ID,
		CreatedAt:      draft.CreatedAt,
		UpdatedAt:      draft.UpdatedAt,
		OrganizationID: draft.OrganizationID,
		Name:           draft.Name,
		Description:    draft.Description,
		Revision:       draft.Revision,
		Membership: api.CampaignMembershipRequest{
			PlanIDs:  append([]uuid.UUID(nil), draft.Membership.PlanIDs...),
			TagQuery: draft.Membership.TagQuery,
		},
		Waves: List(draft.Waves, campaignWaveDraftToAPI),
		Prerequisites: List(
			draft.Prerequisites,
			campaignPrerequisiteDraftToAPI,
		),
		RiskPolicy:              campaignRiskPolicyToAPI(draft.RiskPolicy),
		LastPublishedRevisionID: draft.LastPublishedRevisionID,
	}
}

func CampaignRevisionToAPI(
	revision types.CampaignRevision,
) api.DeploymentCampaignRevision {
	return api.DeploymentCampaignRevision{
		ID:                       revision.ID,
		PublishedAt:              revision.PublishedAt,
		OrganizationID:           revision.OrganizationID,
		CampaignDraftID:          revision.CampaignDraftID,
		RevisionNumber:           revision.RevisionNumber,
		SourceDraftRevision:      revision.SourceDraftRevision,
		Name:                     revision.Name,
		Description:              revision.Description,
		MembershipTagQuery:       revision.MembershipTagQuery,
		RiskPolicy:               campaignRiskPolicyToAPI(revision.RiskPolicy),
		CanonicalChecksum:        revision.CanonicalChecksum,
		PublishedByUserAccountID: revision.PublishedByUserAccountID,
		Waves:                    List(revision.Waves, campaignWaveToAPI),
		Members:                  List(revision.Members, campaignMemberToAPI),
		Prerequisites: List(
			revision.Prerequisites,
			campaignPrerequisiteToAPI,
		),
	}
}

func campaignRiskPolicyToAPI(
	policy types.CampaignRiskPolicy,
) api.CampaignRiskPolicy {
	return api.CampaignRiskPolicy{
		MaximumConcurrency:          policy.MaximumConcurrency,
		FailureToleranceBasisPoints: policy.FailureToleranceBasisPoints,
		MinimumHealthyBasisPoints:   policy.MinimumHealthyBasisPoints,
	}
}

func campaignWaveDraftToAPI(wave types.CampaignWaveDraft) api.CampaignWaveRequest {
	return api.CampaignWaveRequest{
		Order:              wave.Order,
		Name:               wave.Name,
		PlanIDs:            append([]uuid.UUID(nil), wave.PlanIDs...),
		BakeSeconds:        wave.BakeSeconds,
		MaximumConcurrency: wave.MaximumConcurrency,
	}
}

func campaignPrerequisiteDraftToAPI(
	prerequisite types.CampaignPrerequisiteDraft,
) api.CampaignPrerequisiteRequest {
	return api.CampaignPrerequisiteRequest{
		DownstreamPlanID:             prerequisite.DownstreamPlanID,
		UpstreamPlanID:               prerequisite.UpstreamPlanID,
		UpstreamStepKey:              prerequisite.UpstreamStepKey,
		ProviderPlacementID:          prerequisite.ProviderPlacementID,
		ExpectedRuntimeStateChecksum: prerequisite.ExpectedRuntimeStateChecksum,
	}
}

func campaignWaveToAPI(wave types.CampaignWave) api.CampaignWave {
	return api.CampaignWave{
		Order:              wave.Order,
		Name:               wave.Name,
		BakeSeconds:        wave.BakeSeconds,
		MaximumConcurrency: wave.MaximumConcurrency,
	}
}

func campaignMemberToAPI(member types.CampaignMember) api.CampaignMember {
	return api.CampaignMember{
		PlanID:                  member.PlanID,
		DeploymentUnitID:        member.DeploymentUnitID,
		PlanChecksum:            member.PlanChecksum,
		EffectivePolicyChecksum: member.EffectivePolicyChecksum,
		ApprovalRequestID:       member.ApprovalRequestID,
		ApprovalRequestRevision: member.ApprovalRequestRevision,
		ApprovalChecksum:        member.ApprovalChecksum,
		CalendarVersionIDs: append(
			[]uuid.UUID(nil),
			member.CalendarVersionIDs...,
		),
		CalendarChecksums:     append([]string(nil), member.CalendarChecksums...),
		AdmissionEvaluationID: member.AdmissionEvaluationID,
		AdmissionChecksum:     member.AdmissionChecksum,
		WaveOrder:             member.WaveOrder,
		MemberOrder:           member.MemberOrder,
	}
}

func campaignPrerequisiteToAPI(
	prerequisite types.CampaignPrerequisite,
) api.CampaignPrerequisite {
	return api.CampaignPrerequisite{
		DownstreamPlanID:             prerequisite.DownstreamPlanID,
		UpstreamPlanID:               prerequisite.UpstreamPlanID,
		UpstreamStepKey:              prerequisite.UpstreamStepKey,
		ProviderPlacementID:          prerequisite.ProviderPlacementID,
		ProviderDeploymentUnitID:     prerequisite.ProviderDeploymentUnitID,
		ProviderComponentInstanceID:  prerequisite.ProviderComponentInstanceID,
		ExpectedRuntimeStateChecksum: prerequisite.ExpectedRuntimeStateChecksum,
	}
}

func DeploymentCampaignRunToAPI(run types.CampaignRun) api.DeploymentCampaignRun {
	return api.DeploymentCampaignRun{
		ID:                     run.ID,
		CreatedAt:              run.CreatedAt,
		UpdatedAt:              run.UpdatedAt,
		CampaignRevisionID:     run.CampaignRevisionID,
		State:                  run.State,
		Version:                run.Version,
		CurrentWaveOrder:       run.CurrentWaveOrder,
		CurrentMemberOrder:     run.CurrentMemberOrder,
		AdmissionsBlocked:      run.AdmissionsBlocked,
		ResumeState:            run.ResumeState,
		PauseRequested:         run.PauseRequested,
		ReconciliationRequired: run.ReconciliationRequired,
		FencingToken:           run.FencingToken,
		LeaseExpiresAt:         run.LeaseExpiresAt,
	}
}

func DeploymentCampaignControlResultToAPI(
	result types.CampaignControlResult,
) api.DeploymentCampaignControlResult {
	return api.DeploymentCampaignControlResult{
		RequestID:              result.RequestID,
		Status:                 result.Status,
		Run:                    DeploymentCampaignRunToAPI(result.Run),
		PausePending:           result.PausePending,
		ReconciliationRequired: result.ReconciliationRequired,
		Duplicate:              result.Duplicate,
	}
}

func DeploymentCampaignExclusionToAPI(
	exclusion types.CampaignExclusion,
) api.DeploymentCampaignExclusion {
	return api.DeploymentCampaignExclusion{
		ID:                exclusion.ID,
		CampaignRunID:     exclusion.CampaignRunID,
		MemberRunID:       exclusion.MemberRunID,
		Reason:            exclusion.Reason,
		VisibleIncomplete: exclusion.VisibleIncomplete,
		DriftReason:       exclusion.DriftReason,
		ExcludedAt:        exclusion.ExcludedAt,
	}
}
