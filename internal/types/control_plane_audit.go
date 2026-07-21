package types

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var auditExportReferencePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,511}$`)

type ControlPlaneAuditEventInput struct {
	OrganizationID                   uuid.UUID
	EventType                        string
	ActorID                          *uuid.UUID
	Outcome                          string
	ReleaseID                        *uuid.UUID
	ComponentReleaseID               *uuid.UUID
	ProductReleaseID                 *uuid.UUID
	TargetConfigID                   *uuid.UUID
	DeploymentPlanID                 *uuid.UUID
	DeploymentPolicyID               *uuid.UUID
	DeploymentPolicyVersionID        *uuid.UUID
	ApprovalID                       *uuid.UUID
	MaintenanceCalendarID            *uuid.UUID
	DeploymentFreezeID               *uuid.UUID
	AdmissionDecisionID              *uuid.UUID
	EmergencyOverrideID              *uuid.UUID
	CampaignDraftID                  *uuid.UUID
	CampaignRevisionID               *uuid.UUID
	CampaignRunID                    *uuid.UUID
	CampaignWaveDefinitionID         *uuid.UUID
	CampaignWaveRunID                *uuid.UUID
	CampaignMemberID                 *uuid.UUID
	CampaignMemberRunID              *uuid.UUID
	CampaignControlRequestID         *uuid.UUID
	CampaignExclusionID              *uuid.UUID
	CampaignPrerequisiteEvaluationID *uuid.UUID
	CampaignThresholdEvaluationID    *uuid.UUID
	ExecutionID                      *uuid.UUID
	ExecutionAttemptID               *uuid.UUID
	AdapterRevisionID                *uuid.UUID
	DesiredStateID                   *uuid.UUID
	ObservationID                    *uuid.UUID
	DriftCaseID                      *uuid.UUID
	ReconciliationID                 *uuid.UUID
	DeploymentTargetID               *uuid.UUID
	EnvironmentID                    *uuid.UUID
	CustomerOrganizationID           *uuid.UUID
	DeploymentUnitID                 *uuid.UUID
	ComponentID                      *uuid.UUID
	TaskID                           *uuid.UUID
	StepRunID                        *uuid.UUID
	AuditExportSinkID                *uuid.UUID
	AuditExportAttemptID             *uuid.UUID
	ReleaseChecksum                  string
	ComponentReleaseChecksum         string
	ProductReleaseChecksum           string
	ArtifactDigest                   string
	ManifestDigest                   string
	TargetConfigChecksum             string
	DeploymentPlanChecksum           string
	PolicyChecksum                   string
	ApprovalChecksum                 string
	CalendarChecksum                 string
	AdmissionChecksum                string
	CampaignRevisionChecksum         string
	CampaignControlChecksum          string
	ExecutionChecksum                string
	DesiredStateChecksum             string
	ObservationChecksum              string
	DriftChecksum                    string
	ReconciliationChecksum           string
	AuditExportConfigChecksum        string
	Payload                          json.RawMessage
}

type ControlPlaneAuditEvent struct {
	ID                               uuid.UUID       `db:"id" json:"id"`
	OrganizationID                   uuid.UUID       `db:"organization_id" json:"organizationId"`
	Sequence                         int64           `db:"sequence" json:"sequence"`
	EventType                        string          `db:"event_type" json:"eventType"`
	ActorID                          *uuid.UUID      `db:"actor_id" json:"actorId,omitempty"`
	Outcome                          string          `db:"outcome" json:"outcome"`
	ReleaseID                        *uuid.UUID      `db:"release_id" json:"releaseId,omitempty"`
	ComponentReleaseID               *uuid.UUID      `db:"component_release_id" json:"componentReleaseId,omitempty"`
	ProductReleaseID                 *uuid.UUID      `db:"product_release_id" json:"productReleaseId,omitempty"`
	TargetConfigID                   *uuid.UUID      `db:"target_config_id" json:"targetConfigId,omitempty"`
	DeploymentPlanID                 *uuid.UUID      `db:"deployment_plan_id" json:"deploymentPlanId,omitempty"`
	DeploymentPolicyID               *uuid.UUID      `db:"deployment_policy_id" json:"deploymentPolicyId,omitempty"`
	DeploymentPolicyVersionID        *uuid.UUID      `db:"deployment_policy_version_id" json:"deploymentPolicyVersionId,omitempty"`
	ApprovalID                       *uuid.UUID      `db:"approval_id" json:"approvalId,omitempty"`
	MaintenanceCalendarID            *uuid.UUID      `db:"maintenance_calendar_id" json:"maintenanceCalendarId,omitempty"`
	DeploymentFreezeID               *uuid.UUID      `db:"deployment_freeze_id" json:"deploymentFreezeId,omitempty"`
	AdmissionDecisionID              *uuid.UUID      `db:"admission_decision_id" json:"admissionDecisionId,omitempty"`
	EmergencyOverrideID              *uuid.UUID      `db:"emergency_override_id" json:"emergencyOverrideId,omitempty"`
	CampaignDraftID                  *uuid.UUID      `db:"campaign_draft_id" json:"campaignDraftId,omitempty"`
	CampaignRevisionID               *uuid.UUID      `db:"campaign_revision_id" json:"campaignRevisionId,omitempty"`
	CampaignRunID                    *uuid.UUID      `db:"campaign_run_id" json:"campaignRunId,omitempty"`
	CampaignWaveDefinitionID         *uuid.UUID      `db:"campaign_wave_definition_id" json:"campaignWaveDefinitionId,omitempty"`
	CampaignWaveRunID                *uuid.UUID      `db:"campaign_wave_run_id" json:"campaignWaveRunId,omitempty"`
	CampaignMemberID                 *uuid.UUID      `db:"campaign_member_id" json:"campaignMemberId,omitempty"`
	CampaignMemberRunID              *uuid.UUID      `db:"campaign_member_run_id" json:"campaignMemberRunId,omitempty"`
	CampaignControlRequestID         *uuid.UUID      `db:"campaign_control_request_id" json:"campaignControlRequestId,omitempty"`
	CampaignExclusionID              *uuid.UUID      `db:"campaign_exclusion_id" json:"campaignExclusionId,omitempty"`
	CampaignPrerequisiteEvaluationID *uuid.UUID      `db:"campaign_prerequisite_evaluation_id" json:"campaignPrerequisiteEvaluationId,omitempty"`
	CampaignThresholdEvaluationID    *uuid.UUID      `db:"campaign_threshold_evaluation_id" json:"campaignThresholdEvaluationId,omitempty"`
	ExecutionID                      *uuid.UUID      `db:"execution_id" json:"executionId,omitempty"`
	ExecutionAttemptID               *uuid.UUID      `db:"execution_attempt_id" json:"executionAttemptId,omitempty"`
	AdapterRevisionID                *uuid.UUID      `db:"adapter_revision_id" json:"adapterRevisionId,omitempty"`
	DesiredStateID                   *uuid.UUID      `db:"desired_state_id" json:"desiredStateId,omitempty"`
	ObservationID                    *uuid.UUID      `db:"observation_id" json:"observationId,omitempty"`
	DriftCaseID                      *uuid.UUID      `db:"drift_case_id" json:"driftCaseId,omitempty"`
	ReconciliationID                 *uuid.UUID      `db:"reconciliation_id" json:"reconciliationId,omitempty"`
	DeploymentTargetID               *uuid.UUID      `db:"deployment_target_id" json:"deploymentTargetId,omitempty"`
	EnvironmentID                    *uuid.UUID      `db:"environment_id" json:"environmentId,omitempty"`
	CustomerOrganizationID           *uuid.UUID      `db:"customer_organization_id" json:"customerOrganizationId,omitempty"`
	DeploymentUnitID                 *uuid.UUID      `db:"deployment_unit_id" json:"deploymentUnitId,omitempty"`
	ComponentID                      *uuid.UUID      `db:"component_id" json:"componentId,omitempty"`
	TaskID                           *uuid.UUID      `db:"task_id" json:"taskId,omitempty"`
	StepRunID                        *uuid.UUID      `db:"step_run_id" json:"stepRunId,omitempty"`
	AuditExportSinkID                *uuid.UUID      `db:"audit_export_sink_id" json:"auditExportSinkId,omitempty"`
	AuditExportAttemptID             *uuid.UUID      `db:"audit_export_attempt_id" json:"auditExportAttemptId,omitempty"`
	ReleaseChecksum                  string          `db:"release_checksum" json:"releaseChecksum,omitempty"`
	ComponentReleaseChecksum         string          `db:"component_release_checksum" json:"componentReleaseChecksum,omitempty"`
	ProductReleaseChecksum           string          `db:"product_release_checksum" json:"productReleaseChecksum,omitempty"`
	ArtifactDigest                   string          `db:"artifact_digest" json:"artifactDigest,omitempty"`
	ManifestDigest                   string          `db:"manifest_digest" json:"manifestDigest,omitempty"`
	TargetConfigChecksum             string          `db:"target_config_checksum" json:"targetConfigChecksum,omitempty"`
	DeploymentPlanChecksum           string          `db:"deployment_plan_checksum" json:"deploymentPlanChecksum,omitempty"`
	PolicyChecksum                   string          `db:"policy_checksum" json:"policyChecksum,omitempty"`
	ApprovalChecksum                 string          `db:"approval_checksum" json:"approvalChecksum,omitempty"`
	CalendarChecksum                 string          `db:"calendar_checksum" json:"calendarChecksum,omitempty"`
	AdmissionChecksum                string          `db:"admission_checksum" json:"admissionChecksum,omitempty"`
	CampaignRevisionChecksum         string          `db:"campaign_revision_checksum" json:"campaignRevisionChecksum,omitempty"`
	CampaignControlChecksum          string          `db:"campaign_control_checksum" json:"campaignControlChecksum,omitempty"`
	ExecutionChecksum                string          `db:"execution_checksum" json:"executionChecksum,omitempty"`
	DesiredStateChecksum             string          `db:"desired_state_checksum" json:"desiredStateChecksum,omitempty"`
	ObservationChecksum              string          `db:"observation_checksum" json:"observationChecksum,omitempty"`
	DriftChecksum                    string          `db:"drift_checksum" json:"driftChecksum,omitempty"`
	ReconciliationChecksum           string          `db:"reconciliation_checksum" json:"reconciliationChecksum,omitempty"`
	AuditExportConfigChecksum        string          `db:"audit_export_config_checksum" json:"auditExportConfigChecksum,omitempty"`
	Payload                          json.RawMessage `db:"payload" json:"payload,omitempty"`
	PayloadRedacted                  bool            `db:"payload_redacted" json:"payloadRedacted"`
	PayloadTruncated                 bool            `db:"payload_truncated" json:"payloadTruncated"`
	CreatedAt                        time.Time       `db:"created_at" json:"createdAt"`
}

type AuditCorrelationKind string

const (
	AuditCorrelationRelease                        AuditCorrelationKind = "release"
	AuditCorrelationComponentRelease               AuditCorrelationKind = "component_release"
	AuditCorrelationProductRelease                 AuditCorrelationKind = "product_release"
	AuditCorrelationTargetConfig                   AuditCorrelationKind = "target_config"
	AuditCorrelationDeploymentPlan                 AuditCorrelationKind = "deployment_plan"
	AuditCorrelationDeploymentPolicy               AuditCorrelationKind = "deployment_policy"
	AuditCorrelationDeploymentPolicyVersion        AuditCorrelationKind = "deployment_policy_version"
	AuditCorrelationApproval                       AuditCorrelationKind = "approval"
	AuditCorrelationMaintenanceCalendar            AuditCorrelationKind = "maintenance_calendar"
	AuditCorrelationDeploymentFreeze               AuditCorrelationKind = "deployment_freeze"
	AuditCorrelationAdmissionDecision              AuditCorrelationKind = "admission_decision"
	AuditCorrelationEmergencyOverride              AuditCorrelationKind = "emergency_override"
	AuditCorrelationCampaignDraft                  AuditCorrelationKind = "campaign_draft"
	AuditCorrelationCampaignRevision               AuditCorrelationKind = "campaign_revision"
	AuditCorrelationCampaignRun                    AuditCorrelationKind = "campaign_run"
	AuditCorrelationCampaignWaveDefinition         AuditCorrelationKind = "campaign_wave_definition"
	AuditCorrelationCampaignWaveRun                AuditCorrelationKind = "campaign_wave_run"
	AuditCorrelationCampaignMember                 AuditCorrelationKind = "campaign_member"
	AuditCorrelationCampaignMemberRun              AuditCorrelationKind = "campaign_member_run"
	AuditCorrelationCampaignControlRequest         AuditCorrelationKind = "campaign_control_request"
	AuditCorrelationCampaignExclusion              AuditCorrelationKind = "campaign_exclusion"
	AuditCorrelationCampaignPrerequisiteEvaluation AuditCorrelationKind = "campaign_prerequisite_evaluation"
	AuditCorrelationCampaignThresholdEvaluation    AuditCorrelationKind = "campaign_threshold_evaluation"
	AuditCorrelationExecution                      AuditCorrelationKind = "execution"
	AuditCorrelationExecutionAttempt               AuditCorrelationKind = "execution_attempt"
	AuditCorrelationAdapterRevision                AuditCorrelationKind = "adapter_revision"
	AuditCorrelationDesiredState                   AuditCorrelationKind = "desired_state"
	AuditCorrelationObservation                    AuditCorrelationKind = "observation"
	AuditCorrelationDriftCase                      AuditCorrelationKind = "drift_case"
	AuditCorrelationReconciliation                 AuditCorrelationKind = "reconciliation"
	AuditCorrelationDeploymentTarget               AuditCorrelationKind = "deployment_target"
	AuditCorrelationEnvironment                    AuditCorrelationKind = "environment"
	AuditCorrelationCustomerOrganization           AuditCorrelationKind = "customer_organization"
	AuditCorrelationDeploymentUnit                 AuditCorrelationKind = "deployment_unit"
	AuditCorrelationComponent                      AuditCorrelationKind = "component"
	AuditCorrelationTask                           AuditCorrelationKind = "task"
	AuditCorrelationStepRun                        AuditCorrelationKind = "step_run"
	AuditCorrelationAuditExportSink                AuditCorrelationKind = "audit_export_sink"
	AuditCorrelationAuditExportAttempt             AuditCorrelationKind = "audit_export_attempt"
)

type AuditCorrelation struct {
	Kind AuditCorrelationKind `json:"kind"`
	ID   uuid.UUID            `json:"id"`
}

func (input ControlPlaneAuditEventInput) Correlations() []AuditCorrelation {
	values := []struct {
		kind AuditCorrelationKind
		id   *uuid.UUID
	}{
		{AuditCorrelationRelease, input.ReleaseID},
		{AuditCorrelationComponentRelease, input.ComponentReleaseID},
		{AuditCorrelationProductRelease, input.ProductReleaseID},
		{AuditCorrelationTargetConfig, input.TargetConfigID},
		{AuditCorrelationDeploymentPlan, input.DeploymentPlanID},
		{AuditCorrelationDeploymentPolicy, input.DeploymentPolicyID},
		{AuditCorrelationDeploymentPolicyVersion, input.DeploymentPolicyVersionID},
		{AuditCorrelationApproval, input.ApprovalID},
		{AuditCorrelationMaintenanceCalendar, input.MaintenanceCalendarID},
		{AuditCorrelationDeploymentFreeze, input.DeploymentFreezeID},
		{AuditCorrelationAdmissionDecision, input.AdmissionDecisionID},
		{AuditCorrelationEmergencyOverride, input.EmergencyOverrideID},
		{AuditCorrelationCampaignDraft, input.CampaignDraftID},
		{AuditCorrelationCampaignRevision, input.CampaignRevisionID},
		{AuditCorrelationCampaignRun, input.CampaignRunID},
		{AuditCorrelationCampaignWaveDefinition, input.CampaignWaveDefinitionID},
		{AuditCorrelationCampaignWaveRun, input.CampaignWaveRunID},
		{AuditCorrelationCampaignMember, input.CampaignMemberID},
		{AuditCorrelationCampaignMemberRun, input.CampaignMemberRunID},
		{AuditCorrelationCampaignControlRequest, input.CampaignControlRequestID},
		{AuditCorrelationCampaignExclusion, input.CampaignExclusionID},
		{AuditCorrelationCampaignPrerequisiteEvaluation, input.CampaignPrerequisiteEvaluationID},
		{AuditCorrelationCampaignThresholdEvaluation, input.CampaignThresholdEvaluationID},
		{AuditCorrelationExecution, input.ExecutionID},
		{AuditCorrelationExecutionAttempt, input.ExecutionAttemptID},
		{AuditCorrelationAdapterRevision, input.AdapterRevisionID},
		{AuditCorrelationDesiredState, input.DesiredStateID},
		{AuditCorrelationObservation, input.ObservationID},
		{AuditCorrelationDriftCase, input.DriftCaseID},
		{AuditCorrelationReconciliation, input.ReconciliationID},
		{AuditCorrelationDeploymentTarget, input.DeploymentTargetID},
		{AuditCorrelationEnvironment, input.EnvironmentID},
		{AuditCorrelationCustomerOrganization, input.CustomerOrganizationID},
		{AuditCorrelationDeploymentUnit, input.DeploymentUnitID},
		{AuditCorrelationComponent, input.ComponentID},
		{AuditCorrelationTask, input.TaskID},
		{AuditCorrelationStepRun, input.StepRunID},
		{AuditCorrelationAuditExportSink, input.AuditExportSinkID},
		{AuditCorrelationAuditExportAttempt, input.AuditExportAttemptID},
	}
	result := make([]AuditCorrelation, 0, len(values))
	for _, value := range values {
		if value.id != nil && *value.id != uuid.Nil {
			result = append(result, AuditCorrelation{Kind: value.kind, ID: *value.id})
		}
	}
	return result
}

func (event ControlPlaneAuditEvent) Correlations() []AuditCorrelation {
	return (ControlPlaneAuditEventInput{
		ReleaseID: event.ReleaseID, ComponentReleaseID: event.ComponentReleaseID,
		ProductReleaseID: event.ProductReleaseID, TargetConfigID: event.TargetConfigID,
		DeploymentPlanID: event.DeploymentPlanID, DeploymentPolicyID: event.DeploymentPolicyID,
		DeploymentPolicyVersionID: event.DeploymentPolicyVersionID, ApprovalID: event.ApprovalID,
		MaintenanceCalendarID: event.MaintenanceCalendarID, DeploymentFreezeID: event.DeploymentFreezeID,
		AdmissionDecisionID: event.AdmissionDecisionID, EmergencyOverrideID: event.EmergencyOverrideID,
		CampaignDraftID: event.CampaignDraftID, CampaignRevisionID: event.CampaignRevisionID,
		CampaignRunID: event.CampaignRunID, CampaignWaveDefinitionID: event.CampaignWaveDefinitionID,
		CampaignWaveRunID: event.CampaignWaveRunID, CampaignMemberID: event.CampaignMemberID,
		CampaignMemberRunID:              event.CampaignMemberRunID,
		CampaignControlRequestID:         event.CampaignControlRequestID,
		CampaignExclusionID:              event.CampaignExclusionID,
		CampaignPrerequisiteEvaluationID: event.CampaignPrerequisiteEvaluationID,
		CampaignThresholdEvaluationID:    event.CampaignThresholdEvaluationID,
		ExecutionID:                      event.ExecutionID,
		ExecutionAttemptID:               event.ExecutionAttemptID,
		AdapterRevisionID:                event.AdapterRevisionID, DesiredStateID: event.DesiredStateID,
		ObservationID: event.ObservationID, DriftCaseID: event.DriftCaseID,
		ReconciliationID: event.ReconciliationID, DeploymentTargetID: event.DeploymentTargetID,
		EnvironmentID: event.EnvironmentID, CustomerOrganizationID: event.CustomerOrganizationID,
		DeploymentUnitID: event.DeploymentUnitID, ComponentID: event.ComponentID, TaskID: event.TaskID,
		StepRunID: event.StepRunID, AuditExportSinkID: event.AuditExportSinkID,
		AuditExportAttemptID: event.AuditExportAttemptID,
	}).Correlations()
}

type EvidenceBundleQuery struct {
	OrganizationID   uuid.UUID
	DeploymentPlanID uuid.UUID
}

const EvidenceBundleSchemaV1 = "distr.control-plane-evidence/v1"

type EvidenceBundle struct {
	Version          string                   `json:"version"`
	OrganizationID   uuid.UUID                `json:"organizationId"`
	DeploymentPlanID uuid.UUID                `json:"deploymentPlanId"`
	Events           []ControlPlaneAuditEvent `json:"events"`
	Checksum         string                   `json:"checksum"`
}

type ExportBatchResult struct {
	SinkID        uuid.UUID `json:"sinkId"`
	Exported      int       `json:"exported"`
	LastSequence  int64     `json:"lastSequence"`
	CheckpointLag int64     `json:"checkpointLag"`
}

type AuditExportSinkKind string

const (
	AuditExportSinkKindWebhook     AuditExportSinkKind = "webhook"
	AuditExportSinkKindObjectStore AuditExportSinkKind = "object_store"
	AuditExportSinkKindSIEM        AuditExportSinkKind = "siem"
)

func (kind AuditExportSinkKind) Valid() bool {
	switch kind {
	case AuditExportSinkKindWebhook, AuditExportSinkKindObjectStore, AuditExportSinkKindSIEM:
		return true
	default:
		return false
	}
}

func ValidAuditExportEndpointReference(value string) bool {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "secret:") || len(value) > 519 {
		return false
	}
	reference := strings.TrimPrefix(strings.TrimPrefix(value, "secret:"), "//")
	return auditExportReferencePattern.MatchString(reference) &&
		!strings.Contains(reference, "..") &&
		!strings.ContainsAny(reference, `?#@\`)
}

type CreateAuditExportSinkInput struct {
	OrganizationID    uuid.UUID
	ActorID           uuid.UUID
	Name              string
	Kind              AuditExportSinkKind
	EndpointReference string
	ConfigChecksum    string
	Enabled           bool
}

type AuditExportSink struct {
	ID                  uuid.UUID           `db:"id" json:"id"`
	OrganizationID      uuid.UUID           `db:"organization_id" json:"organizationId"`
	Name                string              `db:"name" json:"name"`
	Kind                AuditExportSinkKind `db:"kind" json:"kind"`
	EndpointReference   string              `db:"endpoint_reference" json:"endpointReference"`
	ConfigChecksum      string              `db:"config_checksum" json:"configChecksum"`
	Enabled             bool                `db:"enabled" json:"enabled"`
	LastSuccessAt       *time.Time          `db:"last_success_at" json:"lastSuccessAt,omitempty"`
	LastFailureAt       *time.Time          `db:"last_failure_at" json:"lastFailureAt,omitempty"`
	ConsecutiveFailures int                 `db:"consecutive_failures" json:"consecutiveFailures"`
	CreatedAt           time.Time           `db:"created_at" json:"createdAt"`
	UpdatedAt           time.Time           `db:"updated_at" json:"updatedAt"`
}

type AuditExportStatus struct {
	Sink                   AuditExportSink `json:"sink"`
	LastExportedSequence   int64           `json:"lastExportedSequence"`
	LastExportedEventID    *uuid.UUID      `json:"lastExportedEventId,omitempty"`
	LatestSequence         int64           `json:"latestSequence"`
	CheckpointLag          int64           `json:"checkpointLag"`
	Alert                  bool            `json:"alert"`
	LastAttemptStatus      string          `json:"lastAttemptStatus,omitempty"`
	LastAttemptError       string          `json:"lastAttemptError,omitempty"`
	LastAttemptCompletedAt *time.Time      `json:"lastAttemptCompletedAt,omitempty"`
}
