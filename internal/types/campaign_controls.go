package types

import (
	"time"

	"github.com/google/uuid"
)

type CampaignControlKind string

const (
	CampaignControlKindPause   CampaignControlKind = "PAUSE"
	CampaignControlKindResume  CampaignControlKind = "RESUME"
	CampaignControlKindRetry   CampaignControlKind = "RETRY"
	CampaignControlKindExclude CampaignControlKind = "EXCLUDE"
	CampaignControlKindCancel  CampaignControlKind = "CANCEL"
)

type CampaignControlStatus string

const (
	CampaignControlStatusApplied               CampaignControlStatus = "APPLIED"
	CampaignControlStatusPendingSafePoint      CampaignControlStatus = "PENDING_SAFE_POINT"
	CampaignControlStatusPendingReconciliation CampaignControlStatus = "PENDING_RECONCILIATION"
	CampaignControlStatusRejected              CampaignControlStatus = "REJECTED"
)

type CampaignControlInput struct {
	RequestID       uuid.UUID
	OrganizationID  uuid.UUID
	RunID           uuid.UUID
	ActorID         uuid.UUID
	ExpectedVersion int64
	Kind            CampaignControlKind
	Reason          string
	RequestedAt     time.Time
}

type CampaignMemberControlInput struct {
	CampaignControlInput
	MemberRunID     uuid.UUID
	ProtocolVersion string
}

type CampaignControlFacts struct {
	AtSafePoint               bool
	HasUncertainSteps         bool
	AllActiveStepsCancellable bool
}

type CampaignControlDecision struct {
	Run                    CampaignRun
	Status                 CampaignControlStatus
	PausePending           bool
	ReconciliationRequired bool
}

type CampaignControlResult struct {
	RequestID              uuid.UUID             `json:"requestId"`
	Status                 CampaignControlStatus `json:"status"`
	Run                    CampaignRun           `json:"run"`
	PausePending           bool                  `json:"pausePending"`
	ReconciliationRequired bool                  `json:"reconciliationRequired"`
	Duplicate              bool                  `json:"duplicate"`
}

type CampaignExclusionFacts struct {
	Authorized  bool
	WasAdmitted bool
}

type CampaignExclusion struct {
	ID                uuid.UUID `json:"id"`
	OrganizationID    uuid.UUID `json:"organizationId"`
	CampaignRunID     uuid.UUID `json:"campaignRunId"`
	MemberRunID       uuid.UUID `json:"memberRunId"`
	ControlRequestID  uuid.UUID `json:"controlRequestId"`
	Reason            string    `json:"reason"`
	VisibleIncomplete bool      `json:"visibleIncomplete"`
	DriftReason       string    `json:"driftReason"`
	ExcludedAt        time.Time `json:"excludedAt"`
	ExcludedByActorID uuid.UUID `json:"excludedByActorId"`
}
