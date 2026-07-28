package types

import (
	"time"

	"github.com/google/uuid"
)

type SampleRetirementSubjectType string

const (
	SampleRetirementSubjectApplication      SampleRetirementSubjectType = "application"
	SampleRetirementSubjectDeploymentTarget SampleRetirementSubjectType = "deployment_target"
	SampleRetirementSubjectEnvironment      SampleRetirementSubjectType = "environment"
)

func (subjectType SampleRetirementSubjectType) IsValid() bool {
	switch subjectType {
	case SampleRetirementSubjectApplication,
		SampleRetirementSubjectDeploymentTarget,
		SampleRetirementSubjectEnvironment:
		return true
	default:
		return false
	}
}

type SampleRetirementJobState string

const (
	SampleRetirementJobPreviewed SampleRetirementJobState = "PREVIEWED"
	SampleRetirementJobApplying  SampleRetirementJobState = "APPLYING"
	SampleRetirementJobApplied   SampleRetirementJobState = "APPLIED"
	SampleRetirementJobVerified  SampleRetirementJobState = "VERIFIED"
	SampleRetirementJobFailed    SampleRetirementJobState = "FAILED"
)

type SampleRetirementItemState string

const (
	SampleRetirementItemPending SampleRetirementItemState = "PENDING"
	SampleRetirementItemApplied SampleRetirementItemState = "APPLIED"
	SampleRetirementItemSkipped SampleRetirementItemState = "SKIPPED"
	SampleRetirementItemFailed  SampleRetirementItemState = "FAILED"
)

type SampleRetirementSelector struct {
	Wildcard    string     `json:"wildcard,omitempty"`
	NamePattern string     `json:"namePattern,omitempty"`
	OlderThan   *time.Time `json:"olderThan,omitempty"`
}

type SampleRetirementSubject struct {
	SubjectType       SampleRetirementSubjectType `json:"subjectType"`
	SubjectID         uuid.UUID                   `json:"subjectId"`
	OwnershipMarker   string                      `json:"ownershipMarker"`
	OwnershipChecksum string                      `json:"ownershipChecksum"`
	ExpectedChecksum  string                      `json:"expectedChecksum"`
}

type SampleRetirementRequest struct {
	OrganizationID           uuid.UUID                 `json:"-"`
	RequestedByUserAccountID uuid.UUID                 `json:"-"`
	BackupReference          string                    `json:"backupReference"`
	BackupChecksum           string                    `json:"backupChecksum"`
	RestoreProofReference    string                    `json:"restoreProofReference"`
	RestoreProofChecksum     string                    `json:"restoreProofChecksum"`
	Items                    []SampleRetirementSubject `json:"items"`
	Selector                 SampleRetirementSelector  `json:"selector,omitempty"`
}

type SampleRetirementApplyRequest struct {
	OrganizationID     uuid.UUID `json:"-"`
	ActorUserAccountID uuid.UUID `json:"-"`
	JobID              uuid.UUID `json:"-"`
	PreviewChecksum    string    `json:"previewChecksum"`
	ApprovalID         string    `json:"approvalId"`
	ApprovalChecksum   string    `json:"approvalChecksum"`
}

type SampleRetirementCandidate struct {
	Subject                  SampleRetirementSubject `json:"subject"`
	OrganizationID           uuid.UUID               `json:"organizationId"`
	CurrentChecksum          string                  `json:"currentChecksum"`
	OwnershipEvidenceID      uuid.UUID               `json:"ownershipEvidenceId"`
	OwnershipMarker          string                  `json:"ownershipMarker"`
	OwnershipChecksum        string                  `json:"ownershipChecksum"`
	OwnershipSourceReference string                  `json:"ownershipSourceReference"`
	OwnershipSourceChecksum  string                  `json:"ownershipSourceChecksum"`
	BeforeCount              int                     `json:"beforeCount"`
	Immutable                bool                    `json:"immutable"`
}

type SampleRetirementOwnershipEvidence struct {
	ID                      uuid.UUID                   `db:"id" json:"id"`
	CreatedAt               time.Time                   `db:"created_at" json:"createdAt"`
	OrganizationID          uuid.UUID                   `db:"organization_id" json:"organizationId"`
	SubjectType             SampleRetirementSubjectType `db:"subject_type" json:"subjectType"`
	SubjectID               uuid.UUID                   `db:"subject_id" json:"subjectId"`
	OwnershipMarker         string                      `db:"ownership_marker" json:"ownershipMarker"`
	OwnershipChecksum       string                      `db:"ownership_checksum" json:"ownershipChecksum"`
	SourceReference         string                      `db:"source_reference" json:"sourceReference"`
	SourceChecksum          string                      `db:"source_checksum" json:"sourceChecksum"`
	RecordedByUserAccountID uuid.UUID                   `db:"recorded_by_useraccount_id" json:"recordedByUserAccountId"`
}

type SampleRetirementOwnershipEvidenceRegistrationInput struct {
	OrganizationID          uuid.UUID                   `json:"-"`
	RecordedByUserAccountID uuid.UUID                   `json:"-"`
	SubjectType             SampleRetirementSubjectType `json:"subjectType"`
	SubjectID               uuid.UUID                   `json:"subjectId"`
	OwnershipMarker         string                      `json:"ownershipMarker"`
	OwnershipChecksum       string                      `json:"ownershipChecksum"`
	SourceReference         string                      `json:"sourceReference"`
	SourceChecksum          string                      `json:"sourceChecksum"`
}

type SampleRetirementRecoveryEvidenceKind string

const (
	SampleRetirementRecoveryEvidenceBackup       SampleRetirementRecoveryEvidenceKind = "backup"
	SampleRetirementRecoveryEvidenceRestoreProof SampleRetirementRecoveryEvidenceKind = "restore_proof"
)

func (kind SampleRetirementRecoveryEvidenceKind) IsValid() bool {
	return kind == SampleRetirementRecoveryEvidenceBackup ||
		kind == SampleRetirementRecoveryEvidenceRestoreProof
}

type SampleRetirementRecoveryEvidence struct {
	ID                      uuid.UUID                            `db:"id" json:"id"`
	CreatedAt               time.Time                            `db:"created_at" json:"createdAt"`
	OrganizationID          uuid.UUID                            `db:"organization_id" json:"organizationId"`
	EvidenceKind            SampleRetirementRecoveryEvidenceKind `db:"evidence_kind" json:"evidenceKind"`
	Reference               string                               `db:"reference" json:"reference"`
	Checksum                string                               `db:"checksum" json:"checksum"`
	SourceKind              string                               `db:"source_kind" json:"sourceKind"`
	SourceID                uuid.UUID                            `db:"source_id" json:"sourceId"`
	SourceChecksum          string                               `db:"source_checksum" json:"sourceChecksum"`
	VerifiedAt              time.Time                            `db:"verified_at" json:"verifiedAt"`
	VerifiedByUserAccountID uuid.UUID                            `db:"verified_by_useraccount_id" json:"verifiedByUserAccountId"`
}

type SampleRetirementRecoveryEvidenceRegistrationInput struct {
	OrganizationID          uuid.UUID                            `json:"-"`
	VerifiedByUserAccountID uuid.UUID                            `json:"-"`
	EvidenceKind            SampleRetirementRecoveryEvidenceKind `json:"evidenceKind"`
	Reference               string                               `json:"reference"`
	Checksum                string                               `json:"checksum"`
	SourceKind              string                               `json:"sourceKind"`
	SourceID                uuid.UUID                            `json:"sourceId"`
	SourceChecksum          string                               `json:"sourceChecksum"`
	VerifiedAt              time.Time                            `json:"verifiedAt"`
}

type RetirementReference struct {
	SourceType     string    `json:"sourceType"`
	SourceID       uuid.UUID `json:"sourceId"`
	Relationship   string    `json:"relationship"`
	OrganizationID uuid.UUID `json:"organizationId"`
	Protected      bool      `json:"protected"`
}

type ReferenceReport struct {
	Subject                         SampleRetirementSubject `json:"subject"`
	SubjectOrganizationID           uuid.UUID               `json:"subjectOrganizationId"`
	CurrentChecksum                 string                  `json:"currentChecksum"`
	BeforeCount                     int                     `json:"beforeCount"`
	Immutable                       bool                    `json:"immutable"`
	References                      []RetirementReference   `json:"references"`
	ProtectedReferenceCount         int                     `json:"protectedReferenceCount"`
	CrossOrganizationReferenceCount int                     `json:"crossOrganizationReferenceCount"`
	AuditEventCount                 int                     `json:"auditEventCount"`
	Retirable                       bool                    `json:"retirable"`
	BlockingReasons                 []string                `json:"blockingReasons"`
}

type SampleRetirementJob struct {
	ID                       uuid.UUID                `db:"id" json:"id"`
	CreatedAt                time.Time                `db:"created_at" json:"createdAt"`
	UpdatedAt                time.Time                `db:"updated_at" json:"updatedAt"`
	OrganizationID           uuid.UUID                `db:"organization_id" json:"organizationId"`
	RequestedByUserAccountID uuid.UUID                `db:"requested_by_useraccount_id" json:"requestedByUserAccountId"`
	State                    SampleRetirementJobState `db:"state" json:"state"`
	BackupEvidenceID         uuid.UUID                `db:"backup_evidence_id" json:"backupEvidenceId"`
	BackupReference          string                   `db:"backup_reference" json:"backupReference"`
	BackupChecksum           string                   `db:"backup_checksum" json:"backupChecksum"`
	RestoreProofEvidenceID   uuid.UUID                `db:"restore_proof_evidence_id" json:"restoreProofEvidenceId"`
	RestoreProofReference    string                   `db:"restore_proof_reference" json:"restoreProofReference"`
	RestoreProofChecksum     string                   `db:"restore_proof_checksum" json:"restoreProofChecksum"`
	ApprovalID               *string                  `db:"approval_id" json:"approvalId,omitempty"`
	ApprovalChecksum         *string                  `db:"approval_checksum" json:"approvalChecksum,omitempty"`
	AllowlistChecksum        string                   `db:"allowlist_checksum" json:"allowlistChecksum"`
	PreviewChecksum          string                   `db:"preview_checksum" json:"previewChecksum"`
	RequestedItemCount       int                      `db:"requested_item_count" json:"requestedItemCount"`
	PreviewedItemCount       int                      `db:"previewed_item_count" json:"previewedItemCount"`
	AppliedItemCount         int                      `db:"applied_item_count" json:"appliedItemCount"`
	SkippedItemCount         int                      `db:"skipped_item_count" json:"skippedItemCount"`
	TombstoneCount           int                      `db:"tombstone_count" json:"tombstoneCount"`
	FailedItemCount          int                      `db:"failed_item_count" json:"failedItemCount"`
	LastCheckpointSequence   int64                    `db:"last_checkpoint_sequence" json:"lastCheckpointSequence"`
	CompletedAt              *time.Time               `db:"completed_at" json:"completedAt,omitempty"`
	VerifiedAt               *time.Time               `db:"verified_at" json:"verifiedAt,omitempty"`
	Version                  int64                    `db:"version" json:"version"`
}

type SampleRetirementItem struct {
	ID                      uuid.UUID                   `db:"id" json:"id"`
	CreatedAt               time.Time                   `db:"created_at" json:"createdAt"`
	UpdatedAt               time.Time                   `db:"updated_at" json:"updatedAt"`
	OrganizationID          uuid.UUID                   `db:"organization_id" json:"organizationId"`
	RetirementJobID         uuid.UUID                   `db:"retirement_job_id" json:"retirementJobId"`
	Ordinal                 int                         `db:"ordinal" json:"ordinal"`
	SubjectType             SampleRetirementSubjectType `db:"subject_type" json:"subjectType"`
	SubjectID               uuid.UUID                   `db:"subject_id" json:"subjectId"`
	OwnershipEvidenceID     uuid.UUID                   `db:"ownership_evidence_id" json:"ownershipEvidenceId"`
	OwnershipMarker         string                      `db:"ownership_marker" json:"ownershipMarker"`
	OwnershipChecksum       string                      `db:"ownership_checksum" json:"ownershipChecksum"`
	ExpectedChecksum        string                      `db:"expected_checksum" json:"expectedChecksum"`
	BeforeCount             int                         `db:"before_count" json:"beforeCount"`
	ReferenceReportChecksum string                      `db:"reference_report_checksum" json:"referenceReportChecksum"`
	State                   SampleRetirementItemState   `db:"state" json:"state"`
	AppliedAt               *time.Time                  `db:"applied_at" json:"appliedAt,omitempty"`
	TombstoneID             *uuid.UUID                  `db:"tombstone_id" json:"tombstoneId,omitempty"`
	ErrorCode               string                      `db:"error_code" json:"errorCode,omitempty"`
	Version                 int64                       `db:"version" json:"version"`
}

type SampleRetirementCheckpoint struct {
	ID                   uuid.UUID `db:"id" json:"id"`
	CreatedAt            time.Time `db:"created_at" json:"createdAt"`
	OrganizationID       uuid.UUID `db:"organization_id" json:"organizationId"`
	RetirementJobID      uuid.UUID `db:"retirement_job_id" json:"retirementJobId"`
	Sequence             int64     `db:"sequence" json:"sequence"`
	LastCompletedOrdinal int       `db:"last_completed_ordinal" json:"lastCompletedOrdinal"`
	AppliedItemCount     int       `db:"applied_item_count" json:"appliedItemCount"`
	SkippedItemCount     int       `db:"skipped_item_count" json:"skippedItemCount"`
	TombstoneCount       int       `db:"tombstone_count" json:"tombstoneCount"`
	CheckpointChecksum   string    `db:"checkpoint_checksum" json:"checkpointChecksum"`
}

type AuditSubjectTombstone struct {
	ID                     uuid.UUID                   `db:"id" json:"id"`
	CreatedAt              time.Time                   `db:"created_at" json:"createdAt"`
	RetiredAt              time.Time                   `db:"retired_at" json:"retiredAt"`
	OrganizationID         uuid.UUID                   `db:"organization_id" json:"organizationId"`
	RetirementJobID        uuid.UUID                   `db:"retirement_job_id" json:"retirementJobId"`
	RetirementItemID       uuid.UUID                   `db:"retirement_item_id" json:"retirementItemId"`
	SubjectType            SampleRetirementSubjectType `db:"subject_type" json:"subjectType"`
	SubjectID              uuid.UUID                   `db:"subject_id" json:"subjectId"`
	OwnershipMarker        string                      `db:"ownership_marker" json:"ownershipMarker"`
	OwnershipChecksum      string                      `db:"ownership_checksum" json:"ownershipChecksum"`
	SubjectChecksum        string                      `db:"subject_checksum" json:"subjectChecksum"`
	FirstAuditEventID      *uuid.UUID                  `db:"first_audit_event_id" json:"firstAuditEventId,omitempty"`
	AuditEventCount        int                         `db:"audit_event_count" json:"auditEventCount"`
	RetiredByUserAccountID uuid.UUID                   `db:"retired_by_useraccount_id" json:"retiredByUserAccountId"`
	LineageChecksum        string                      `db:"lineage_checksum" json:"lineageChecksum"`
}

type SampleRetirementPreview struct {
	Job              SampleRetirementJob    `json:"job"`
	Items            []SampleRetirementItem `json:"items"`
	ReferenceReports []ReferenceReport      `json:"referenceReports"`
	PreviewChecksum  string                 `json:"previewChecksum"`
	RequestedCount   int                    `json:"requestedCount"`
	RetirableCount   int                    `json:"retirableCount"`
	BlockedCount     int                    `json:"blockedCount"`
	AuditEventCount  int                    `json:"auditEventCount"`
	CreatedAt        time.Time              `json:"createdAt"`
}

type SampleRetirementDetail struct {
	Job         SampleRetirementJob          `json:"job"`
	Items       []SampleRetirementItem       `json:"items"`
	Checkpoints []SampleRetirementCheckpoint `json:"checkpoints"`
	Tombstones  []AuditSubjectTombstone      `json:"tombstones"`
}

type SampleRetirementApplyFacts struct {
	Job                     SampleRetirementJob         `json:"job"`
	Items                   []SampleRetirementItem      `json:"items"`
	LastCheckpoint          *SampleRetirementCheckpoint `json:"lastCheckpoint,omitempty"`
	CurrentReferenceReports []ReferenceReport           `json:"currentReferenceReports"`
	CurrentAuditEventCount  int                         `json:"currentAuditEventCount"`
}

type SampleRetirementResult struct {
	JobID              uuid.UUID                `json:"jobId"`
	PreviewChecksum    string                   `json:"previewChecksum"`
	State              SampleRetirementJobState `json:"state"`
	AppliedCount       int                      `json:"appliedCount"`
	SkippedCount       int                      `json:"skippedCount"`
	TombstoneCount     int                      `json:"tombstoneCount"`
	CheckpointSequence int64                    `json:"checkpointSequence"`
	NoOp               bool                     `json:"noOp"`
	CompletedAt        time.Time                `json:"completedAt"`
}

type SampleRetirementVerification struct {
	JobID                 uuid.UUID                `json:"jobId"`
	State                 SampleRetirementJobState `json:"state"`
	PreviewChecksum       string                   `json:"previewChecksum"`
	ExactCounts           bool                     `json:"exactCounts"`
	TombstoneLineageValid bool                     `json:"tombstoneLineageValid"`
	AuditEventsRetained   bool                     `json:"auditEventsRetained"`
	RemainingSubjectCount int                      `json:"remainingSubjectCount"`
	AuditEventCount       int                      `json:"auditEventCount"`
	AppliedCount          int                      `json:"appliedCount"`
	TombstoneCount        int                      `json:"tombstoneCount"`
	VerifiedAt            time.Time                `json:"verifiedAt"`
	Problems              []string                 `json:"problems"`
}
