package types

import "github.com/google/uuid"

type MigrationPhase string

const (
	MigrationPhaseExpand   MigrationPhase = "expand"
	MigrationPhaseData     MigrationPhase = "data"
	MigrationPhaseSwitch   MigrationPhase = "switch"
	MigrationPhaseContract MigrationPhase = "contract"
)

type MigrationRetryClass string

const (
	MigrationRetryNone    MigrationRetryClass = "none"
	MigrationRetryBounded MigrationRetryClass = "bounded"
	MigrationRetrySafe    MigrationRetryClass = "safe"
)

type MigrationReversibility string

const (
	MigrationReversibilityReversible  MigrationReversibility = "reversible"
	MigrationReversibilityManual      MigrationReversibility = "manual"
	MigrationReversibilityForwardOnly MigrationReversibility = "forward_only"
)

type MigrationProbe struct {
	Name             string `json:"name"`
	Reference        string `json:"reference"`
	ExpectedChecksum string `json:"expectedChecksum"`
}

type MigrationContract struct {
	ID                               string                 `json:"id"`
	Checksum                         string                 `json:"checksum"`
	ComponentKey                     string                 `json:"componentKey"`
	DatabaseResourceKey              string                 `json:"databaseResourceKey"`
	ExpectedSourceVersion            string                 `json:"expectedSourceVersion"`
	ExpectedSourceChecksum           string                 `json:"expectedSourceChecksum"`
	ResultingVersion                 string                 `json:"resultingVersion"`
	Phase                            MigrationPhase         `json:"phase"`
	DependsOn                        []string               `json:"dependsOn,omitempty"`
	LockType                         string                 `json:"lockType"`
	LockTimeoutSeconds               int                    `json:"lockTimeoutSeconds"`
	OperationalImpact                string                 `json:"operationalImpact"`
	BackupRequired                   bool                   `json:"backupRequired"`
	BackupVerifier                   string                 `json:"backupVerifier,omitempty"`
	PreconditionProbes               []MigrationProbe       `json:"preconditionProbes"`
	PostconditionProbes              []MigrationProbe       `json:"postconditionProbes"`
	RetryClass                       MigrationRetryClass    `json:"retryClass"`
	IdempotencyKey                   string                 `json:"idempotencyKey,omitempty"`
	Reversibility                    MigrationReversibility `json:"reversibility"`
	PreviousApplicationCompatibility string                 `json:"previousApplicationCompatibility"`
	RecoveryProcedureReference       string                 `json:"recoveryProcedureReference"`
	RequiresForwardFix               bool                   `json:"requiresForwardFix"`
	AdapterType                      string                 `json:"adapterType,omitempty"`
	ArtifactDigest                   string                 `json:"artifactDigest,omitempty"`
	EvidenceRetentionDays            int                    `json:"evidenceRetentionDays"`
}

type SchemaState struct {
	ComponentKey        string `json:"componentKey"`
	DatabaseResourceKey string `json:"databaseResourceKey"`
	Version             string `json:"version"`
	Checksum            string `json:"checksum"`
}

type BackupEvidence struct {
	ID       string `json:"id"`
	Checksum string `json:"checksum"`
	Verified bool   `json:"verified"`
}

type MigrationPreflight struct {
	Contract                 MigrationContract `json:"contract"`
	Backup                   *BackupEvidence   `json:"backup,omitempty"`
	CurrentSchema            SchemaState       `json:"currentSchema"`
	TargetLockAvailable      bool              `json:"targetLockAvailable"`
	DatabaseLockAvailable    bool              `json:"databaseLockAvailable"`
	AdapterAvailable         bool              `json:"adapterAvailable"`
	PreconditionProbesPassed bool              `json:"preconditionProbesPassed"`
}

type RecoveryMode string

const (
	RecoveryModeReverse    RecoveryMode = "reverse"
	RecoveryModeForwardFix RecoveryMode = "forward_fix"
	RecoveryModeManual     RecoveryMode = "manual"
	RecoveryModeRestore    RecoveryMode = "restore"
)

type FailedPlan struct {
	PlanID                uuid.UUID           `json:"planId"`
	Draft                 PlanDraft           `json:"draft"`
	Graph                 TargetPlanGraph     `json:"graph"`
	FailedStepKey         string              `json:"failedStepKey"`
	Contracts             []MigrationContract `json:"contracts"`
	CompletedMigrationIDs []string            `json:"completedMigrationIds"`
}

type RecoveryRequest struct {
	Mode                     RecoveryMode     `json:"mode"`
	Reason                   string           `json:"reason"`
	SeparateApprovalID       string           `json:"separateApprovalId,omitempty"`
	BackupID                 string           `json:"backupId,omitempty"`
	BackupChecksum           string           `json:"backupChecksum,omitempty"`
	DatabaseResourceKey      string           `json:"databaseResourceKey,omitempty"`
	ExpectedDataLossBoundary string           `json:"expectedDataLossBoundary,omitempty"`
	ProcedureVersion         string           `json:"procedureVersion,omitempty"`
	RequiredApproverGroups   []string         `json:"requiredApproverGroups,omitempty"`
	OperatorScope            string           `json:"operatorScope,omitempty"`
	ValidationProbes         []MigrationProbe `json:"validationProbes,omitempty"`
}

type RecoveryPlan struct {
	Mode                      RecoveryMode    `json:"mode"`
	SourcePlanID              uuid.UUID       `json:"sourcePlanId"`
	Graph                     TargetPlanGraph `json:"graph"`
	EvidenceRetentionRequired bool            `json:"evidenceRetentionRequired"`
	Reason                    string          `json:"reason"`
}
