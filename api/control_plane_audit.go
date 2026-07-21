package api

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

var auditExportChecksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type ControlPlaneAuditListRequest struct {
	AfterSequence int64 `query:"afterSequence"`
	Limit         int   `query:"limit"`
}

func (r ControlPlaneAuditListRequest) Validate() error {
	if r.AfterSequence < 0 {
		return validation.NewValidationFailedError("afterSequence must not be negative")
	}
	if r.Limit < 0 || r.Limit > 100 {
		return validation.NewValidationFailedError("limit must be between 1 and 100 when provided")
	}
	return nil
}

func (r ControlPlaneAuditListRequest) PageLimit() int {
	if r.Limit == 0 {
		return 50
	}
	return r.Limit
}

type EvidenceBundleRequest struct {
	DeploymentPlanID uuid.UUID `json:"deploymentPlanId"`
}

func (r EvidenceBundleRequest) Validate() error {
	if r.DeploymentPlanID == uuid.Nil {
		return validation.NewValidationFailedError("deploymentPlanId is required")
	}
	return nil
}

type ControlPlaneAuditEvent struct {
	ID                        uuid.UUID       `json:"id"`
	Sequence                  int64           `json:"sequence"`
	EventType                 string          `json:"eventType"`
	ActorID                   *uuid.UUID      `json:"actorId,omitempty"`
	Outcome                   string          `json:"outcome"`
	ReleaseID                 *uuid.UUID      `json:"releaseId,omitempty"`
	ComponentReleaseID        *uuid.UUID      `json:"componentReleaseId,omitempty"`
	ProductReleaseID          *uuid.UUID      `json:"productReleaseId,omitempty"`
	TargetConfigID            *uuid.UUID      `json:"targetConfigId,omitempty"`
	DeploymentPlanID          *uuid.UUID      `json:"deploymentPlanId,omitempty"`
	DeploymentPolicyID        *uuid.UUID      `json:"deploymentPolicyId,omitempty"`
	DeploymentPolicyVersionID *uuid.UUID      `json:"deploymentPolicyVersionId,omitempty"`
	ApprovalID                *uuid.UUID      `json:"approvalId,omitempty"`
	MaintenanceCalendarID     *uuid.UUID      `json:"maintenanceCalendarId,omitempty"`
	DeploymentFreezeID        *uuid.UUID      `json:"deploymentFreezeId,omitempty"`
	AdmissionDecisionID       *uuid.UUID      `json:"admissionDecisionId,omitempty"`
	EmergencyOverrideID       *uuid.UUID      `json:"emergencyOverrideId,omitempty"`
	CampaignID                *uuid.UUID      `json:"campaignId,omitempty"`
	WaveID                    *uuid.UUID      `json:"waveId,omitempty"`
	ExecutionID               *uuid.UUID      `json:"executionId,omitempty"`
	AdapterRevisionID         *uuid.UUID      `json:"adapterRevisionId,omitempty"`
	DesiredStateID            *uuid.UUID      `json:"desiredStateId,omitempty"`
	ObservationID             *uuid.UUID      `json:"observationId,omitempty"`
	DriftCaseID               *uuid.UUID      `json:"driftCaseId,omitempty"`
	ReconciliationID          *uuid.UUID      `json:"reconciliationId,omitempty"`
	DeploymentTargetID        *uuid.UUID      `json:"deploymentTargetId,omitempty"`
	EnvironmentID             *uuid.UUID      `json:"environmentId,omitempty"`
	CustomerOrganizationID    *uuid.UUID      `json:"customerOrganizationId,omitempty"`
	DeploymentUnitID          *uuid.UUID      `json:"deploymentUnitId,omitempty"`
	ComponentID               *uuid.UUID      `json:"componentId,omitempty"`
	TaskID                    *uuid.UUID      `json:"taskId,omitempty"`
	StepRunID                 *uuid.UUID      `json:"stepRunId,omitempty"`
	AuditExportSinkID         *uuid.UUID      `json:"auditExportSinkId,omitempty"`
	AuditExportAttemptID      *uuid.UUID      `json:"auditExportAttemptId,omitempty"`
	ReleaseChecksum           string          `json:"releaseChecksum,omitempty"`
	ComponentReleaseChecksum  string          `json:"componentReleaseChecksum,omitempty"`
	ProductReleaseChecksum    string          `json:"productReleaseChecksum,omitempty"`
	ArtifactDigest            string          `json:"artifactDigest,omitempty"`
	ManifestDigest            string          `json:"manifestDigest,omitempty"`
	TargetConfigChecksum      string          `json:"targetConfigChecksum,omitempty"`
	DeploymentPlanChecksum    string          `json:"deploymentPlanChecksum,omitempty"`
	PolicyChecksum            string          `json:"policyChecksum,omitempty"`
	ApprovalChecksum          string          `json:"approvalChecksum,omitempty"`
	CalendarChecksum          string          `json:"calendarChecksum,omitempty"`
	AdmissionChecksum         string          `json:"admissionChecksum,omitempty"`
	CampaignChecksum          string          `json:"campaignChecksum,omitempty"`
	ExecutionChecksum         string          `json:"executionChecksum,omitempty"`
	DesiredStateChecksum      string          `json:"desiredStateChecksum,omitempty"`
	ObservationChecksum       string          `json:"observationChecksum,omitempty"`
	DriftChecksum             string          `json:"driftChecksum,omitempty"`
	ReconciliationChecksum    string          `json:"reconciliationChecksum,omitempty"`
	AuditExportConfigChecksum string          `json:"auditExportConfigChecksum,omitempty"`
	Payload                   json.RawMessage `json:"payload,omitempty"`
	PayloadRedacted           bool            `json:"payloadRedacted"`
	PayloadTruncated          bool            `json:"payloadTruncated"`
	CreatedAt                 time.Time       `json:"createdAt"`
}

type ControlPlaneAuditEventPage struct {
	Items             []ControlPlaneAuditEvent `json:"items"`
	NextAfterSequence int64                    `json:"nextAfterSequence,omitempty"`
}

type EvidenceBundle struct {
	DeploymentPlanID uuid.UUID                `json:"deploymentPlanId"`
	Events           []ControlPlaneAuditEvent `json:"events"`
	Checksum         string                   `json:"checksum"`
}

type CreateAuditExportSinkRequest struct {
	Name              string                    `json:"name"`
	Kind              types.AuditExportSinkKind `json:"kind"`
	EndpointReference string                    `json:"endpointReference"`
	ConfigChecksum    string                    `json:"configChecksum"`
	Enabled           *bool                     `json:"enabled,omitempty"`
}

func (r CreateAuditExportSinkRequest) Validate() error {
	name := strings.TrimSpace(r.Name)
	reference := strings.TrimSpace(r.EndpointReference)
	switch {
	case len(name) < 1 || len(name) > 128:
		return validation.NewValidationFailedError("name must contain between 1 and 128 characters")
	case !r.Kind.Valid():
		return validation.NewValidationFailedError("kind must be webhook, object_store, or siem")
	case !types.ValidAuditExportEndpointReference(reference):
		return validation.NewValidationFailedError("endpointReference must be a safe secret reference")
	case !auditExportChecksumPattern.MatchString(r.ConfigChecksum):
		return validation.NewValidationFailedError("configChecksum must be a SHA-256 checksum")
	default:
		return nil
	}
}

func (r CreateAuditExportSinkRequest) IsEnabled() bool {
	return r.Enabled == nil || *r.Enabled
}

type AuditExportSink struct {
	ID                  uuid.UUID                 `json:"id"`
	Name                string                    `json:"name"`
	Kind                types.AuditExportSinkKind `json:"kind"`
	EndpointReference   string                    `json:"endpointReference"`
	ConfigChecksum      string                    `json:"configChecksum"`
	Enabled             bool                      `json:"enabled"`
	LastSuccessAt       *time.Time                `json:"lastSuccessAt,omitempty"`
	LastFailureAt       *time.Time                `json:"lastFailureAt,omitempty"`
	ConsecutiveFailures int                       `json:"consecutiveFailures"`
	CreatedAt           time.Time                 `json:"createdAt"`
	UpdatedAt           time.Time                 `json:"updatedAt"`
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
