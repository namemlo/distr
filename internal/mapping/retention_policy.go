package mapping

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
)

func RetentionPolicyToAPI(policy types.RetentionPolicy) api.RetentionPolicy {
	return api.RetentionPolicy{
		ID:                                policy.ID,
		CreatedAt:                         policy.CreatedAt,
		UpdatedAt:                         policy.UpdatedAt,
		Name:                              policy.Name,
		Description:                       policy.Description,
		KeepLastSuccessfulReleases:        policy.KeepLastSuccessfulReleases,
		FailedTaskRetentionDays:           policy.FailedTaskRetentionDays,
		ProductionFailedTaskRetentionDays: policy.ProductionFailedTaskRetentionDays,
		StepLogRetentionDays:              policy.StepLogRetentionDays,
		ProtectCurrentlyDeployedReleases:  policy.ProtectCurrentlyDeployedReleases,
		ProtectRetentionProtectedReleases: policy.ProtectRetentionProtectedReleases,
		MinimumAuditRetentionDays:         policy.MinimumAuditRetentionDays,
	}
}

func RetentionCleanupPreviewToAPI(preview types.RetentionCleanupPreview) api.RetentionCleanupPreview {
	return api.RetentionCleanupPreview{
		Policy:               RetentionPolicyToAPI(preview.Policy),
		GeneratedAt:          preview.GeneratedAt,
		ReleaseCandidates:    List(preview.ReleaseCandidates, RetentionReleaseCandidateToAPI),
		FailedTaskCandidates: List(preview.FailedTaskCandidates, RetentionTaskCandidateToAPI),
		StepLogCandidates:    List(preview.StepLogCandidates, RetentionStepLogCandidateToAPI),
		SafetyBlocks:         List(preview.SafetyBlocks, RetentionSafetyBlockToAPI),
	}
}

func RetentionReleaseCandidateToAPI(candidate types.RetentionReleaseCandidate) api.RetentionReleaseCandidate {
	return api.RetentionReleaseCandidate{
		ReleaseBundleID:  candidate.ReleaseBundleID,
		ApplicationID:    candidate.ApplicationID,
		ReleaseNumber:    candidate.ReleaseNumber,
		Status:           candidate.Status,
		PublishedAt:      candidate.PublishedAt,
		LastSuccessfulAt: candidate.LastSuccessfulAt,
		SuccessfulRank:   candidate.SuccessfulRank,
		Reason:           candidate.Reason,
	}
}

func RetentionTaskCandidateToAPI(candidate types.RetentionTaskCandidate) api.RetentionTaskCandidate {
	return api.RetentionTaskCandidate{
		TaskID:             candidate.TaskID,
		ReleaseBundleID:    candidate.ReleaseBundleID,
		ApplicationID:      candidate.ApplicationID,
		EnvironmentID:      candidate.EnvironmentID,
		DeploymentTargetID: candidate.DeploymentTargetID,
		Status:             candidate.Status,
		CompletedAt:        candidate.CompletedAt,
		RetentionDays:      candidate.RetentionDays,
		Reason:             candidate.Reason,
	}
}

func RetentionStepLogCandidateToAPI(candidate types.RetentionStepLogCandidate) api.RetentionStepLogCandidate {
	return api.RetentionStepLogCandidate{
		TaskID:     candidate.TaskID,
		ChunkCount: candidate.ChunkCount,
		OldestAt:   candidate.OldestAt,
		NewestAt:   candidate.NewestAt,
		Reason:     candidate.Reason,
	}
}

func RetentionSafetyBlockToAPI(block types.RetentionSafetyBlock) api.RetentionSafetyBlock {
	return api.RetentionSafetyBlock{
		ResourceType: block.ResourceType,
		ResourceID:   block.ResourceID,
		Reason:       block.Reason,
		Message:      block.Message,
	}
}

func RetentionCleanupJobToAPI(job types.RetentionCleanupJob) api.RetentionCleanupJob {
	return api.RetentionCleanupJob{
		ID:                       job.ID,
		CreatedAt:                job.CreatedAt,
		UpdatedAt:                job.UpdatedAt,
		RetentionPolicyID:        job.RetentionPolicyID,
		ActorUserAccountID:       job.ActorUserAccountID,
		DryRun:                   job.DryRun,
		Status:                   job.Status,
		ReleaseCandidateCount:    job.ReleaseCandidateCount,
		FailedTaskCandidateCount: job.FailedTaskCandidateCount,
		StepLogCandidateCount:    job.StepLogCandidateCount,
		SafetyBlockCount:         job.SafetyBlockCount,
		Plan:                     RetentionCleanupPreviewToAPI(job.Plan),
		Message:                  job.Message,
	}
}
