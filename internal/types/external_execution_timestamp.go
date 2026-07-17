package types

import "github.com/google/uuid"

type ExternalExecutionTimestampDecision string

const (
	ExternalExecutionTimestampDecisionProven     ExternalExecutionTimestampDecision = "PROVEN"
	ExternalExecutionTimestampDecisionAttested   ExternalExecutionTimestampDecision = "ATTESTED"
	ExternalExecutionTimestampDecisionUnresolved ExternalExecutionTimestampDecision = "UNRESOLVED"
	ExternalExecutionTimestampDecisionNull       ExternalExecutionTimestampDecision = "NULL_VALUE"
)

type ExternalExecutionTimestampManifestState string

const revokedBeforeApplyManifestState ExternalExecutionTimestampManifestState = "REVOKED_BEFORE_APPLY"

const ExternalExecutionTimestampManifestStateRevokedBeforeApply = revokedBeforeApplyManifestState

const (
	ExternalExecutionTimestampManifestStateDraft    ExternalExecutionTimestampManifestState = "DRAFT"
	ExternalExecutionTimestampManifestStateApproved ExternalExecutionTimestampManifestState = "APPROVED"
	ExternalExecutionTimestampManifestStateApplied  ExternalExecutionTimestampManifestState = "APPLIED"
	ExternalExecutionTimestampManifestStateVerified ExternalExecutionTimestampManifestState = "VERIFIED"
)

type ExternalExecutionTimestampRawCell struct {
	SourceTable     string    `json:"sourceTable"`
	SourceRowID     uuid.UUID `json:"sourceRowId"`
	SourceColumn    string    `json:"sourceColumn"`
	ColumnOrdinal   uint8     `json:"columnOrdinal"`
	RawValue        *string   `json:"rawValue"`
	RawCellChecksum string    `json:"rawCellChecksum"`
}

type ExternalExecutionTimestampCellDecision struct {
	ExternalExecutionTimestampRawCell
	Decision                    ExternalExecutionTimestampDecision `json:"decision"`
	SourceZone                  string                             `json:"sourceZone,omitempty"`
	SourceOffsetSeconds         *int32                             `json:"sourceOffsetSeconds,omitempty"`
	ConvertedValue              *string                            `json:"convertedValue,omitempty"`
	EvidenceReference           string                             `json:"evidenceReference,omitempty"`
	EvidenceChecksum            string                             `json:"evidenceChecksum,omitempty"`
	ApprovingIdentity           string                             `json:"approvingIdentity,omitempty"`
	ConversionExpressionVersion string                             `json:"conversionExpressionVersion"`
}

type ExternalExecutionTimestampManifest struct {
	ID                          uuid.UUID                                `json:"id"`
	SupersedesManifestID        *uuid.UUID                               `json:"supersedesManifestId,omitempty"`
	DatabaseIdentityChecksum    string                                   `json:"databaseIdentityChecksum"`
	SourceSchemaVersion         uint                                     `json:"sourceSchemaVersion"`
	SnapshotStartedAt           string                                   `json:"snapshotStartedAt"`
	SnapshotEndedAt             string                                   `json:"snapshotEndedAt"`
	ExecutionCount              uint64                                   `json:"executionCount"`
	EventCount                  uint64                                   `json:"eventCount"`
	RawCellCount                uint64                                   `json:"rawCellCount"`
	PopulatedCellCount          uint64                                   `json:"populatedCellCount"`
	RawCellChecksum             string                                   `json:"rawCellChecksum"`
	EvidenceBundleReference     string                                   `json:"evidenceBundleReference,omitempty"`
	EvidenceBundleChecksum      string                                   `json:"evidenceBundleChecksum,omitempty"`
	ToolVersion                 string                                   `json:"toolVersion"`
	ConversionExpressionVersion string                                   `json:"conversionExpressionVersion"`
	AuthorIdentity              string                                   `json:"authorIdentity,omitempty"`
	ReviewerIdentity            string                                   `json:"reviewerIdentity,omitempty"`
	ApprovedAt                  string                                   `json:"approvedAt,omitempty"`
	TargetReleaseCommit         string                                   `json:"targetReleaseCommit,omitempty"`
	TargetImageDigest           string                                   `json:"targetImageDigest,omitempty"`
	State                       ExternalExecutionTimestampManifestState  `json:"state"`
	DecisionContentChecksum     string                                   `json:"decisionContentChecksum"`
	Cells                       []ExternalExecutionTimestampCellDecision `json:"cells"`
}

type ExternalExecutionTimestampValidationReport struct {
	ManifestID               uuid.UUID `json:"manifestId"`
	SchemaVersion            uint      `json:"schemaVersion"`
	ExecutionCount           uint64    `json:"executionCount"`
	EventCount               uint64    `json:"eventCount"`
	RawCellCount             uint64    `json:"rawCellCount"`
	PopulatedCellCount       uint64    `json:"populatedCellCount"`
	UnresolvedCellCount      uint64    `json:"unresolvedCellCount"`
	RawSetChecksum           string    `json:"rawSetChecksum"`
	DatabaseIdentityChecksum string    `json:"databaseIdentityChecksum"`
	DecisionContentChecksum  string    `json:"decisionContentChecksum"`
}

type ExternalExecutionTimestampSealOptions struct {
	AuthorIdentity          string
	ReviewerIdentity        string
	EvidenceBundleReference string
	EvidenceBundleChecksum  string
	TargetReleaseCommit     string
	TargetImageDigest       string
}

type ExternalExecutionTimestampApplyRequest struct {
	Manifest                     ExternalExecutionTimestampManifest
	Apply                        bool
	WriterFenceIdentifier        string
	BackupReference              string
	BackupChecksum               string
	RestoreVerificationReference string
	RestoreVerificationChecksum  string
}

type ExternalExecutionTimestampApplyReport struct {
	ManifestID               uuid.UUID `json:"manifestId"`
	DryRun                   bool      `json:"dryRun"`
	Idempotent               bool      `json:"idempotent"`
	ProvenCount              uint64    `json:"provenCount"`
	AttestedCount            uint64    `json:"attestedCount"`
	UnresolvedCount          uint64    `json:"unresolvedCount"`
	NullCount                uint64    `json:"nullCount"`
	ProvenanceRows           uint64    `json:"provenanceRows"`
	WouldPopulateCount       uint64    `json:"wouldPopulateCount"`
	PopulatedShadowCount     uint64    `json:"populatedShadowCount"`
	RawSetChecksum           string    `json:"rawSetChecksum"`
	DatabaseIdentityChecksum string    `json:"databaseIdentityChecksum"`
}

type ExternalExecutionTimestampVerificationReport struct {
	ManifestID              uuid.UUID `json:"manifestId"`
	SchemaVersion           uint      `json:"schemaVersion"`
	SourceExecutionCount    uint64    `json:"sourceExecutionCount"`
	SourceEventCount        uint64    `json:"sourceEventCount"`
	CurrentExecutionCount   uint64    `json:"currentExecutionCount"`
	CurrentEventCount       uint64    `json:"currentEventCount"`
	ProvenanceRows          uint64    `json:"provenanceRows"`
	ResolvedShadowCount     uint64    `json:"resolvedShadowCount"`
	UnresolvedShadowCount   uint64    `json:"unresolvedShadowCount"`
	PostManifestPairedCount uint64    `json:"postManifestPairedCount"`
	RawSetChecksum          string    `json:"rawSetChecksum"`
	DecisionContentChecksum string    `json:"decisionContentChecksum"`
}

type ExternalExecutionTimestampReadiness struct {
	SchemaVersion           uint       `json:"schemaVersion"`
	TransitionKind          string     `json:"transitionKind"`
	ManifestID              *uuid.UUID `json:"manifestId,omitempty"`
	ExecutionCount          uint64     `json:"executionCount"`
	EventCount              uint64     `json:"eventCount"`
	ProvenanceRows          uint64     `json:"provenanceRows"`
	PostTransitionPairCount uint64     `json:"postTransitionPairCount"`
}
