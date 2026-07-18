package types

import (
	"time"

	"github.com/google/uuid"
)

type CampaignRunState string

const (
	CampaignRunStateDraft            CampaignRunState = "DRAFT"
	CampaignRunStateValidated        CampaignRunState = "VALIDATED"
	CampaignRunStateAwaitingApproval CampaignRunState = "AWAITING_APPROVAL"
	CampaignRunStateScheduled        CampaignRunState = "SCHEDULED"
	CampaignRunStateRunning          CampaignRunState = "RUNNING"
	CampaignRunStatePaused           CampaignRunState = "PAUSED"
	CampaignRunStateFailed           CampaignRunState = "FAILED"
	CampaignRunStateCompleted        CampaignRunState = "COMPLETED"
	CampaignRunStateCanceled         CampaignRunState = "CANCELED"
)

type CampaignRun struct {
	ID                 uuid.UUID        `db:"id" json:"id"`
	CreatedAt          time.Time        `db:"created_at" json:"createdAt"`
	UpdatedAt          time.Time        `db:"updated_at" json:"updatedAt"`
	OrganizationID     uuid.UUID        `db:"organization_id" json:"organizationId"`
	CampaignRevisionID uuid.UUID        `db:"campaign_revision_id" json:"campaignRevisionId"`
	State              CampaignRunState `db:"state" json:"state"`
	Version            int64            `db:"version" json:"version"`
	CurrentWaveOrder   int              `db:"current_wave_order" json:"currentWaveOrder"`
	CurrentMemberOrder int              `db:"current_member_order" json:"currentMemberOrder"`
	AdmissionsBlocked  bool             `db:"admissions_blocked" json:"admissionsBlocked"`
	FencingToken       int64            `db:"fencing_token" json:"fencingToken"`
	LeaseHolder        string           `db:"lease_holder" json:"leaseHolder,omitempty"`
	LeaseExpiresAt     *time.Time       `db:"lease_expires_at" json:"leaseExpiresAt,omitempty"`
}

type CampaignTransition struct {
	RunID           uuid.UUID
	OrganizationID  uuid.UUID
	ExpectedVersion int64
	To              CampaignRunState
	Reason          string
	ActorID         *uuid.UUID
	At              time.Time
}

type CampaignLease struct {
	RunID          uuid.UUID
	Holder         string
	FencingToken   int64
	LeaseExpiresAt time.Time
}

type CampaignThresholdPolicy struct {
	MinimumSamples     int
	MaximumFailureRate float64
}

type CampaignThresholdSnapshot struct {
	Successful int
	Failed     int
}

type CampaignThresholdDecision struct {
	Samples          int
	FailureRate      float64
	Breached         bool
	AdmissionAllowed bool
}

type CampaignObservationRequirement struct {
	OrganizationID      uuid.UUID
	UpstreamPlanID      uuid.UUID
	StepKey             string
	ObservationID       uuid.UUID
	ObservationChecksum string
	ExpectedChecksum    string
}

type CampaignMemberCandidate struct {
	MemberRunID   uuid.UUID
	WaveRunID     uuid.UUID
	WaveOrder     int
	MemberOrder   int
	PlanID        uuid.UUID
	Prerequisites []CampaignObservationRequirement
}

type CampaignSchedule struct {
	Run               CampaignRun
	Candidates        []CampaignMemberCandidate
	ThresholdPolicy   CampaignThresholdPolicy
	ThresholdSnapshot CampaignThresholdSnapshot
}

type CampaignMemberAdmission struct {
	RunID        uuid.UUID
	WaveRunID    uuid.UUID
	MemberRunID  uuid.UUID
	PlanID       uuid.UUID
	WaveOrder    int
	MemberOrder  int
	AdmittedAt   time.Time
	FencingToken int64
}

type CampaignPrerequisiteEvaluation struct {
	ID                  uuid.UUID
	CampaignRunID       uuid.UUID
	MemberRunID         uuid.UUID
	UpstreamPlanID      uuid.UUID
	StepKey             string
	ExpectedChecksum    string
	ActualObservationID uuid.UUID
	ActualChecksum      string
	Matched             bool
	Reason              string
	EvaluatedAt         time.Time
	FencingToken        int64
}

type CampaignThresholdEvaluation struct {
	ID                 uuid.UUID
	CampaignRunID      uuid.UUID
	Samples            int
	Successful         int
	Failed             int
	FailureRate        float64
	MaximumFailureRate float64
	Breached           bool
	EvaluatedAt        time.Time
	FencingToken       int64
}

type CampaignSchedulerResult struct {
	LeaseAcquired bool
	Admitted      bool
	Paused        bool
	MemberRunID   uuid.UUID
}

type WaveAdmission struct {
	CampaignRunID uuid.UUID
	Candidate     *CampaignMemberCandidate
	Allowed       bool
	Reason        string
}
