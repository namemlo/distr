package types

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	AdmissionRequiredPlanSchemaV2 = "distr.target-deployment-plan/v2"
	AdmissionRequiredProtocolV2   = "v2"
)

type AdmissionDecision string

const (
	AdmissionDecisionAdmit AdmissionDecision = "ADMIT"
	AdmissionDecisionWait  AdmissionDecision = "WAIT"
	AdmissionDecisionBlock AdmissionDecision = "BLOCK"
)

func (decision AdmissionDecision) IsValid() bool {
	switch decision {
	case AdmissionDecisionAdmit, AdmissionDecisionWait, AdmissionDecisionBlock:
		return true
	default:
		return false
	}
}

type AdmissionReasonCode string

const (
	AdmissionReasonAdmitted                AdmissionReasonCode = "admitted"
	AdmissionReasonMaintenanceWindowClosed AdmissionReasonCode = "maintenance_window_closed"
	AdmissionReasonDeploymentFreezeActive  AdmissionReasonCode = "deployment_freeze_active"
	AdmissionReasonApprovalMissing         AdmissionReasonCode = "approval_missing"
	AdmissionReasonApprovalInvalid         AdmissionReasonCode = "approval_invalid"
	AdmissionReasonMandatoryGateFailed     AdmissionReasonCode = "mandatory_gate_failed"
	AdmissionReasonEmergencyAcceleration   AdmissionReasonCode = "emergency_acceleration"
)

type AdmissionGateKey string

const (
	AdmissionGateApprovalWait    AdmissionGateKey = "approval-wait"
	AdmissionGateCampaignBake    AdmissionGateKey = "campaign-bake"
	AdmissionGateMaintenanceWait AdmissionGateKey = "maintenance-wait"
	AdmissionGateOptionalTest    AdmissionGateKey = "optional-test"

	AdmissionGateIntegrity        AdmissionGateKey = "integrity"
	AdmissionGateRequiredEvidence AdmissionGateKey = "evidence"
	AdmissionGateBackup           AdmissionGateKey = "backup"
	AdmissionGateProvenance       AdmissionGateKey = "provenance"
	AdmissionGateObservation      AdmissionGateKey = "observation"
	AdmissionGateMandatoryHealth  AdmissionGateKey = "mandatory-health"
)

type AdmissionPlanEvidence struct {
	ID              uuid.UUID `json:"id"`
	Revision        int64     `json:"revision"`
	Checksum        string    `json:"checksum"`
	Schema          string    `json:"schema"`
	ProtocolVersion string    `json:"protocolVersion"`
}

type AdmissionCampaignEvidence struct {
	ID       uuid.UUID `json:"id"`
	Revision int64     `json:"revision"`
	Checksum string    `json:"checksum"`
}

type AdmissionCalendarEvidence struct {
	VersionID            uuid.UUID          `json:"versionId"`
	Checksum             string             `json:"checksum"`
	Evaluation           CalendarEvaluation `json:"evaluation"`
	RemainingWaitSeconds int64              `json:"remainingWaitSeconds"`
}

type AdmissionFreezeEvidence struct {
	RevisionID           uuid.UUID        `json:"revisionId"`
	Checksum             string           `json:"checksum"`
	Evaluation           FreezeEvaluation `json:"evaluation"`
	RemainingWaitSeconds int64            `json:"remainingWaitSeconds"`
}

type AdmissionApprovalEvidence struct {
	RequestID               uuid.UUID          `json:"requestId"`
	RequestRevision         int64              `json:"requestRevision"`
	SubjectChecksum         string             `json:"subjectChecksum"`
	EffectivePolicyChecksum string             `json:"effectivePolicyChecksum"`
	SubscriberSetChecksum   string             `json:"subscriberSetChecksum"`
	Evaluation              ApprovalEvaluation `json:"evaluation"`
}

type AdmissionGateEvidence struct {
	Key       AdmissionGateKey `json:"key"`
	Mandatory bool             `json:"mandatory"`
	Satisfied bool             `json:"satisfied"`
	Checksum  string           `json:"checksum"`
}

type EmergencyAcceleration struct {
	GateKey                AdmissionGateKey `json:"gateKey"`
	MaxAccelerationSeconds int64            `json:"maxAccelerationSeconds"`
}

type EmergencyOverrideApprovalEvidence struct {
	RequestID       uuid.UUID            `json:"requestId"`
	RequestRevision int64                `json:"requestRevision"`
	RequestChecksum string               `json:"requestChecksum"`
	Eligible        bool                 `json:"eligible"`
	State           ApprovalRequestState `json:"state"`
}

type EmergencyOverride struct {
	ID                      uuid.UUID                           `db:"id" json:"id"`
	CreatedAt               time.Time                           `db:"created_at" json:"createdAt"`
	OrganizationID          uuid.UUID                           `db:"organization_id" json:"organizationId"`
	DeploymentPlanID        uuid.UUID                           `db:"deployment_plan_id" json:"deploymentPlanId"`
	PlanRevision            int64                               `db:"plan_revision" json:"planRevision"`
	PlanChecksum            string                              `db:"plan_checksum" json:"planChecksum"`
	EffectivePolicyChecksum string                              `db:"effective_policy_checksum" json:"effectivePolicyChecksum"`
	Accelerations           []EmergencyAcceleration             `db:"accelerations" json:"accelerations"`
	Reason                  string                              `db:"reason" json:"reason"`
	ActorUserAccountID      uuid.UUID                           `db:"actor_useraccount_id" json:"actorUserAccountId"`
	ApprovalEvidence        []EmergencyOverrideApprovalEvidence `db:"approval_evidence" json:"approvalEvidence"`
	ExpiresAt               time.Time                           `db:"expires_at" json:"expiresAt"`
	Checksum                string                              `db:"checksum" json:"checksum"`
	IdempotencyKey          string                              `db:"idempotency_key" json:"idempotencyKey"`
}

type AdmissionRequest struct {
	OrganizationID    uuid.UUID                   `json:"organizationId"`
	Plan              AdmissionPlanEvidence       `json:"plan"`
	Campaign          *AdmissionCampaignEvidence  `json:"campaign,omitempty"`
	EffectivePolicy   EffectivePolicy             `json:"effectivePolicy"`
	CalendarEvidence  []AdmissionCalendarEvidence `json:"calendarEvidence"`
	FreezeEvidence    []AdmissionFreezeEvidence   `json:"freezeEvidence"`
	Approval          AdmissionApprovalEvidence   `json:"approval"`
	GateEvidence      []AdmissionGateEvidence     `json:"gateEvidence"`
	EmergencyOverride *EmergencyOverride          `json:"emergencyOverride,omitempty"`
	EvaluatedAt       time.Time                   `json:"evaluatedAt"`
}

type AdmissionTemporalEvidence struct {
	EvaluatedAt      time.Time                   `json:"evaluatedAt"`
	CalendarEvidence []AdmissionCalendarEvidence `json:"calendarEvidence"`
	FreezeEvidence   []AdmissionFreezeEvidence   `json:"freezeEvidence"`
}

type AdmissionEvaluation struct {
	ID                        uuid.UUID                 `db:"id" json:"id"`
	CreatedAt                 time.Time                 `db:"created_at" json:"createdAt"`
	OrganizationID            uuid.UUID                 `db:"organization_id" json:"organizationId"`
	DeploymentPlanID          uuid.UUID                 `db:"deployment_plan_id" json:"deploymentPlanId"`
	PlanRevision              int64                     `db:"plan_revision" json:"planRevision"`
	PlanChecksum              string                    `db:"plan_checksum" json:"planChecksum"`
	PlanSchema                string                    `db:"plan_schema" json:"planSchema"`
	ProtocolVersion           string                    `db:"protocol_version" json:"protocolVersion"`
	CampaignID                *uuid.UUID                `db:"campaign_id" json:"campaignId,omitempty"`
	CampaignRevision          *int64                    `db:"campaign_revision" json:"campaignRevision,omitempty"`
	CampaignChecksum          string                    `db:"campaign_checksum" json:"campaignChecksum,omitempty"`
	EffectivePolicyChecksum   string                    `db:"effective_policy_checksum" json:"effectivePolicyChecksum"`
	PolicyVersionIDs          []uuid.UUID               `db:"policy_version_ids" json:"policyVersionIds"`
	CalendarVersionIDs        []uuid.UUID               `db:"calendar_version_ids" json:"calendarVersionIds"`
	FreezeRevisionIDs         []uuid.UUID               `db:"freeze_revision_ids" json:"freezeRevisionIds"`
	ApprovalRequestID         *uuid.UUID                `db:"approval_request_id" json:"approvalRequestId,omitempty"`
	ApprovalRequestRevision   *int64                    `db:"approval_request_revision" json:"approvalRequestRevision,omitempty"`
	EmergencyOverrideID       *uuid.UUID                `db:"emergency_override_id" json:"emergencyOverrideId,omitempty"`
	EmergencyOverrideChecksum string                    `db:"emergency_override_checksum" json:"emergencyOverrideChecksum,omitempty"`
	Decision                  AdmissionDecision         `db:"decision" json:"decision"`
	ReasonCodes               []AdmissionReasonCode     `db:"reason_codes" json:"reasonCodes"`
	EvaluatedAt               time.Time                 `db:"evaluated_at" json:"evaluatedAt"`
	TemporalEvidence          AdmissionTemporalEvidence `db:"temporal_evidence" json:"temporalEvidence"`
	GateEvidence              []AdmissionGateEvidence   `db:"gate_evidence" json:"gateEvidence"`
	MaterialChecksum          string                    `db:"material_checksum" json:"materialChecksum"`
	DecisionChecksum          string                    `db:"decision_checksum" json:"decisionChecksum"`
	SchedulerIdempotencyKey   string                    `db:"scheduler_idempotency_key" json:"schedulerIdempotencyKey"`
	ActorUserAccountID        uuid.UUID                 `db:"actor_useraccount_id" json:"actorUserAccountId"`
}

type AdmissionAuthorizationContext struct {
	OrganizationID     uuid.UUID
	ActorUserAccountID uuid.UUID
	DeploymentPlanID   uuid.UUID
	EnvironmentID      uuid.UUID
	DeploymentUnitID   *uuid.UUID
	Action             string
	DecisionAt         time.Time
}

type AdmissionAuthorizer func(context.Context, AdmissionAuthorizationContext) error

type AdmitDeploymentPlanRequest struct {
	OrganizationID          uuid.UUID
	DeploymentPlanID        uuid.UUID
	ActorUserAccountID      uuid.UUID
	SchedulerIdempotencyKey string
	Campaign                *AdmissionCampaignEvidence
	Authorize               AdmissionAuthorizer
}

type CreateEmergencyOverrideRequest struct {
	OrganizationID     uuid.UUID
	DeploymentPlanID   uuid.UUID
	ActorUserAccountID uuid.UUID
	Accelerations      []EmergencyAcceleration
	Reason             string
	ApprovalRequestIDs []uuid.UUID
	ExpiresAt          time.Time
	IdempotencyKey     string
	Authorize          AdmissionAuthorizer
}

type CreateTasksForAdmittedV2PlanRequest struct {
	OrganizationID          uuid.UUID
	DeploymentPlanID        uuid.UUID
	ExecutionOccurrenceID   uuid.UUID
	ActorUserAccountID      uuid.UUID
	SchedulerIdempotencyKey string
	ConcurrencyPolicy       TaskConcurrencyPolicy
	AdditionalResources     []TaskLockResourceRequest
	Campaign                *AdmissionCampaignEvidence
	Authorize               AdmissionAuthorizer
}

type AdmissionPlanSnapshot struct {
	Plan             DeploymentPlan
	PlanRevision     int64
	PlanSchema       string
	ProtocolVersion  string
	EnvironmentID    uuid.UUID
	DeploymentUnitID *uuid.UUID
}

type EmergencyOverrideRecord struct {
	EmergencyOverride
	ApprovalEvidenceJSON json.RawMessage `db:"approval_evidence"`
	AccelerationsJSON    json.RawMessage `db:"accelerations"`
}
