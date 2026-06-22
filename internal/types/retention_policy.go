package types

import (
	"time"

	"github.com/google/uuid"
)

type RetentionPolicy struct {
	ID                                uuid.UUID `db:"id" json:"id"`
	CreatedAt                         time.Time `db:"created_at" json:"createdAt"`
	UpdatedAt                         time.Time `db:"updated_at" json:"updatedAt"`
	OrganizationID                    uuid.UUID `db:"organization_id" json:"organizationId"`
	Name                              string    `db:"name" json:"name"`
	Description                       string    `db:"description" json:"description"`
	KeepLastSuccessfulReleases        int       `db:"keep_last_successful_releases" json:"keepLastSuccessfulReleases"`
	FailedTaskRetentionDays           int       `db:"failed_task_retention_days" json:"failedTaskRetentionDays"`
	ProductionFailedTaskRetentionDays int       `db:"production_failed_task_retention_days" json:"productionFailedTaskRetentionDays"`
	StepLogRetentionDays              int       `db:"step_log_retention_days" json:"stepLogRetentionDays"`
	ProtectCurrentlyDeployedReleases  bool      `db:"protect_currently_deployed_releases" json:"protectCurrentlyDeployedReleases"`
	ProtectRetentionProtectedReleases bool      `db:"protect_retention_protected_releases" json:"protectRetentionProtectedReleases"`
	MinimumAuditRetentionDays         int       `db:"minimum_audit_retention_days" json:"minimumAuditRetentionDays"`
}

type CreateRetentionPolicyRequest struct {
	OrganizationID                    uuid.UUID
	Name                              string
	Description                       string
	KeepLastSuccessfulReleases        int
	FailedTaskRetentionDays           int
	ProductionFailedTaskRetentionDays int
	StepLogRetentionDays              int
	ProtectCurrentlyDeployedReleases  bool
	ProtectRetentionProtectedReleases bool
	MinimumAuditRetentionDays         int
}

type UpdateRetentionPolicyRequest struct {
	CreateRetentionPolicyRequest
	ID uuid.UUID
}

type RetentionCleanupPreviewRequest struct {
	OrganizationID uuid.UUID
	PolicyID       uuid.UUID
	Now            time.Time
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
	ReleaseBundleID  uuid.UUID           `json:"releaseBundleId"`
	ApplicationID    uuid.UUID           `json:"applicationId"`
	ReleaseNumber    string              `json:"releaseNumber"`
	Status           ReleaseBundleStatus `json:"status"`
	PublishedAt      *time.Time          `json:"publishedAt,omitempty"`
	LastSuccessfulAt *time.Time          `json:"lastSuccessfulAt,omitempty"`
	SuccessfulRank   int                 `json:"successfulRank"`
	Reason           string              `json:"reason"`
}

type RetentionTaskCandidate struct {
	TaskID             uuid.UUID  `json:"taskId"`
	ReleaseBundleID    uuid.UUID  `json:"releaseBundleId"`
	ApplicationID      uuid.UUID  `json:"applicationId"`
	EnvironmentID      uuid.UUID  `json:"environmentId"`
	DeploymentTargetID uuid.UUID  `json:"deploymentTargetId"`
	Status             TaskStatus `json:"status"`
	CompletedAt        time.Time  `json:"completedAt"`
	RetentionDays      int        `json:"retentionDays"`
	Reason             string     `json:"reason"`
}

type RetentionStepLogCandidate struct {
	TaskID     uuid.UUID `json:"taskId"`
	ChunkCount int       `json:"chunkCount"`
	OldestAt   time.Time `json:"oldestAt"`
	NewestAt   time.Time `json:"newestAt"`
	Reason     string    `json:"reason"`
}

type RetentionSafetyReason string

const (
	RetentionSafetyCurrentlyDeployed RetentionSafetyReason = "currently_deployed"
	RetentionSafetyProtectedRelease  RetentionSafetyReason = "protected_release"
	RetentionSafetyAuditMinimum      RetentionSafetyReason = "audit_minimum"
	RetentionSafetyRecentSuccessRank RetentionSafetyReason = "recent_success_rank"
)

type RetentionSafetyBlock struct {
	ResourceType string                `json:"resourceType"`
	ResourceID   uuid.UUID             `json:"resourceId"`
	Reason       RetentionSafetyReason `json:"reason"`
	Message      string                `json:"message"`
}

type RetentionCleanupJobStatus string

const (
	RetentionCleanupJobStatusPreviewed RetentionCleanupJobStatus = "PREVIEWED"
	RetentionCleanupJobStatusRejected  RetentionCleanupJobStatus = "REJECTED"
	RetentionCleanupJobStatusCompleted RetentionCleanupJobStatus = "COMPLETED"
	RetentionCleanupJobStatusFailed    RetentionCleanupJobStatus = "FAILED"
)

type CreateRetentionCleanupJobRequest struct {
	OrganizationID uuid.UUID
	PolicyID       uuid.UUID
	ActorUserID    uuid.UUID
	DryRun         bool
	Now            time.Time
}

type RetentionCleanupJob struct {
	ID                       uuid.UUID                 `db:"id" json:"id"`
	CreatedAt                time.Time                 `db:"created_at" json:"createdAt"`
	UpdatedAt                time.Time                 `db:"updated_at" json:"updatedAt"`
	OrganizationID           uuid.UUID                 `db:"organization_id" json:"organizationId"`
	RetentionPolicyID        uuid.UUID                 `db:"retention_policy_id" json:"retentionPolicyId"`
	ActorUserAccountID       *uuid.UUID                `db:"actor_user_account_id" json:"actorUserAccountId,omitempty"`
	DryRun                   bool                      `db:"dry_run" json:"dryRun"`
	Status                   RetentionCleanupJobStatus `db:"status" json:"status"`
	ReleaseCandidateCount    int                       `db:"release_candidate_count" json:"releaseCandidateCount"`
	FailedTaskCandidateCount int                       `db:"failed_task_candidate_count" json:"failedTaskCandidateCount"`
	StepLogCandidateCount    int                       `db:"step_log_candidate_count" json:"stepLogCandidateCount"`
	SafetyBlockCount         int                       `db:"safety_block_count" json:"safetyBlockCount"`
	Plan                     RetentionCleanupPreview   `db:"-" json:"plan"`
	Message                  string                    `db:"message" json:"message"`
}
