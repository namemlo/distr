package api

import (
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

type CreateUpdateRetentionPolicyRequest struct {
	Name                              string `json:"name"`
	Description                       string `json:"description"`
	KeepLastSuccessfulReleases        int    `json:"keepLastSuccessfulReleases"`
	FailedTaskRetentionDays           int    `json:"failedTaskRetentionDays"`
	ProductionFailedTaskRetentionDays int    `json:"productionFailedTaskRetentionDays"`
	StepLogRetentionDays              int    `json:"stepLogRetentionDays"`
	ProtectCurrentlyDeployedReleases  bool   `json:"protectCurrentlyDeployedReleases"`
	ProtectRetentionProtectedReleases bool   `json:"protectRetentionProtectedReleases"`
	MinimumAuditRetentionDays         int    `json:"minimumAuditRetentionDays"`
}

func (r *CreateUpdateRetentionPolicyRequest) Validate() error {
	r.Name = strings.TrimSpace(r.Name)
	r.Description = strings.TrimSpace(r.Description)
	if r.Name == "" {
		return validation.NewValidationFailedError("name is required")
	}
	if r.KeepLastSuccessfulReleases < 0 {
		return validation.NewValidationFailedError("keepLastSuccessfulReleases must be non-negative")
	}
	if r.FailedTaskRetentionDays < 0 {
		return validation.NewValidationFailedError("failedTaskRetentionDays must be non-negative")
	}
	if r.ProductionFailedTaskRetentionDays < 0 {
		return validation.NewValidationFailedError("productionFailedTaskRetentionDays must be non-negative")
	}
	if r.StepLogRetentionDays < 0 {
		return validation.NewValidationFailedError("stepLogRetentionDays must be non-negative")
	}
	if r.MinimumAuditRetentionDays < 0 {
		return validation.NewValidationFailedError("minimumAuditRetentionDays must be non-negative")
	}
	return nil
}

type CreateRetentionCleanupJobRequest struct {
	DryRun bool `json:"dryRun"`
}

func (r CreateRetentionCleanupJobRequest) Validate() error {
	if !r.DryRun {
		return validation.NewValidationFailedError("dryRun must be true")
	}
	return nil
}

type RetentionPolicy struct {
	ID                                uuid.UUID `json:"id"`
	CreatedAt                         time.Time `json:"createdAt"`
	UpdatedAt                         time.Time `json:"updatedAt"`
	Name                              string    `json:"name"`
	Description                       string    `json:"description"`
	KeepLastSuccessfulReleases        int       `json:"keepLastSuccessfulReleases"`
	FailedTaskRetentionDays           int       `json:"failedTaskRetentionDays"`
	ProductionFailedTaskRetentionDays int       `json:"productionFailedTaskRetentionDays"`
	StepLogRetentionDays              int       `json:"stepLogRetentionDays"`
	ProtectCurrentlyDeployedReleases  bool      `json:"protectCurrentlyDeployedReleases"`
	ProtectRetentionProtectedReleases bool      `json:"protectRetentionProtectedReleases"`
	MinimumAuditRetentionDays         int       `json:"minimumAuditRetentionDays"`
}

type RetentionCleanupPreview struct {
	Policy               RetentionPolicy             `json:"policy"`
	GeneratedAt          time.Time                   `json:"generatedAt"`
	ReleaseCandidates    []RetentionReleaseCandidate `json:"releaseCandidates"`
	FailedTaskCandidates []RetentionTaskCandidate    `json:"failedTaskCandidates"`
	StepLogCandidates    []RetentionStepLogCandidate `json:"stepLogCandidates"`
	SafetyBlocks         []RetentionSafetyBlock      `json:"safetyBlocks"`
}

type RetentionReleaseCandidate struct {
	ReleaseBundleID  uuid.UUID                 `json:"releaseBundleId"`
	ApplicationID    uuid.UUID                 `json:"applicationId"`
	ReleaseNumber    string                    `json:"releaseNumber"`
	Status           types.ReleaseBundleStatus `json:"status"`
	PublishedAt      *time.Time                `json:"publishedAt,omitempty"`
	LastSuccessfulAt *time.Time                `json:"lastSuccessfulAt,omitempty"`
	SuccessfulRank   int                       `json:"successfulRank"`
	Reason           string                    `json:"reason"`
}

type RetentionTaskCandidate struct {
	TaskID             uuid.UUID        `json:"taskId"`
	ReleaseBundleID    uuid.UUID        `json:"releaseBundleId"`
	ApplicationID      uuid.UUID        `json:"applicationId"`
	EnvironmentID      uuid.UUID        `json:"environmentId"`
	DeploymentTargetID uuid.UUID        `json:"deploymentTargetId"`
	Status             types.TaskStatus `json:"status"`
	CompletedAt        time.Time        `json:"completedAt"`
	RetentionDays      int              `json:"retentionDays"`
	Reason             string           `json:"reason"`
}

type RetentionStepLogCandidate struct {
	TaskID     uuid.UUID `json:"taskId"`
	ChunkCount int       `json:"chunkCount"`
	OldestAt   time.Time `json:"oldestAt"`
	NewestAt   time.Time `json:"newestAt"`
	Reason     string    `json:"reason"`
}

type RetentionSafetyBlock struct {
	ResourceType string                      `json:"resourceType"`
	ResourceID   uuid.UUID                   `json:"resourceId"`
	Reason       types.RetentionSafetyReason `json:"reason"`
	Message      string                      `json:"message"`
}

type RetentionCleanupJob struct {
	ID                       uuid.UUID                       `json:"id"`
	CreatedAt                time.Time                       `json:"createdAt"`
	UpdatedAt                time.Time                       `json:"updatedAt"`
	RetentionPolicyID        uuid.UUID                       `json:"retentionPolicyId"`
	ActorUserAccountID       *uuid.UUID                      `json:"actorUserAccountId,omitempty"`
	DryRun                   bool                            `json:"dryRun"`
	Status                   types.RetentionCleanupJobStatus `json:"status"`
	ReleaseCandidateCount    int                             `json:"releaseCandidateCount"`
	FailedTaskCandidateCount int                             `json:"failedTaskCandidateCount"`
	StepLogCandidateCount    int                             `json:"stepLogCandidateCount"`
	SafetyBlockCount         int                             `json:"safetyBlockCount"`
	Plan                     RetentionCleanupPreview         `json:"plan"`
	Message                  string                          `json:"message"`
}
