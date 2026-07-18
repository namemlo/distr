package types

import (
	"time"

	"github.com/google/uuid"
)

type DriftClass string

const (
	DriftClassArtifact         DriftClass = "ARTIFACT"
	DriftClassConfiguration    DriftClass = "CONFIGURATION"
	DriftClassSchema           DriftClass = "SCHEMA"
	DriftClassCapability       DriftClass = "CAPABILITY"
	DriftClassHealth           DriftClass = "HEALTH"
	DriftClassPlatform         DriftClass = "PLATFORM"
	DriftClassTopology         DriftClass = "TOPOLOGY"
	DriftClassMissing          DriftClass = "MISSING"
	DriftClassStale            DriftClass = "STALE"
	DriftClassUnverified       DriftClass = "UNVERIFIED_DESIRED"
	DriftClassExecutorMismatch DriftClass = "EXECUTOR_OBSERVER_MISMATCH"
	DriftClassConflict         DriftClass = "OBSERVER_CONFLICT"
)

type DriftClassification struct {
	Drifted bool         `json:"drifted"`
	Classes []DriftClass `json:"classes"`
	Summary string       `json:"summary"`
}

type DriftCaseStatus string

const (
	DriftCaseStatusOpen      DriftCaseStatus = "OPEN"
	DriftCaseStatusAssigned  DriftCaseStatus = "ASSIGNED"
	DriftCaseStatusException DriftCaseStatus = "EXCEPTION"
	DriftCaseStatusResolved  DriftCaseStatus = "RESOLVED"
)

type DriftInput struct {
	OrganizationID          uuid.UUID           `json:"organizationId"`
	ActiveDesiredRevisionID uuid.UUID           `json:"activeDesiredRevisionId"`
	ObservationID           uuid.UUID           `json:"observationId"`
	Classification          DriftClassification `json:"classification"`
	Reason                  string              `json:"reason"`
}

type DriftCase struct {
	ID                      uuid.UUID       `db:"id" json:"id"`
	CreatedAt               time.Time       `db:"created_at" json:"createdAt"`
	UpdatedAt               time.Time       `db:"updated_at" json:"updatedAt"`
	OrganizationID          uuid.UUID       `db:"organization_id" json:"organizationId"`
	ActiveDesiredRevisionID uuid.UUID       `db:"active_desired_revision_id" json:"activeDesiredRevisionId"`
	ObservationID           uuid.UUID       `db:"observation_id" json:"observationId"`
	DeploymentUnitID        uuid.UUID       `db:"deployment_unit_id" json:"deploymentUnitId"`
	ComponentInstanceID     uuid.UUID       `db:"component_instance_id" json:"componentInstanceId"`
	Status                  DriftCaseStatus `db:"status" json:"status"`
	Classes                 []DriftClass    `db:"-" json:"classes"`
	Summary                 string          `db:"summary" json:"summary"`
	AssignedTo              *uuid.UUID      `db:"assigned_to" json:"assignedTo,omitempty"`
	ResolvedAt              *time.Time      `db:"resolved_at" json:"resolvedAt,omitempty"`
}

type DriftCaseEvent struct {
	ID             uuid.UUID       `db:"id" json:"id"`
	CreatedAt      time.Time       `db:"created_at" json:"createdAt"`
	OrganizationID uuid.UUID       `db:"organization_id" json:"organizationId"`
	DriftCaseID    uuid.UUID       `db:"drift_case_id" json:"driftCaseId"`
	Status         DriftCaseStatus `db:"status" json:"status"`
	ActorID        *uuid.UUID      `db:"actor_id" json:"actorId,omitempty"`
	Reason         string          `db:"reason" json:"reason"`
}

type ReconciliationActionType string

const (
	ReconciliationActionRestoreDesired    ReconciliationActionType = "RESTORE_DESIRED"
	ReconciliationActionCreatePlan        ReconciliationActionType = "CREATE_PLAN"
	ReconciliationActionAcceptDeviation   ReconciliationActionType = "ACCEPT_DEVIATION"
	ReconciliationActionCloseWithEvidence ReconciliationActionType = "CLOSE_WITH_EVIDENCE"
)

type ReconciliationDecision struct {
	OrganizationID       uuid.UUID                `json:"organizationId"`
	DriftCaseID          uuid.UUID                `json:"driftCaseId"`
	Action               ReconciliationActionType `json:"action"`
	Reason               string                   `json:"reason"`
	ActorID              uuid.UUID                `json:"actorId"`
	DeploymentPlanID     *uuid.UUID               `json:"deploymentPlanId,omitempty"`
	OutcomeObservationID *uuid.UUID               `json:"outcomeObservationId,omitempty"`
	AcceptedUntil        *time.Time               `json:"acceptedUntil,omitempty"`
}

type ReconciliationAction struct {
	ID                   uuid.UUID                `db:"id" json:"id"`
	CreatedAt            time.Time                `db:"created_at" json:"createdAt"`
	OrganizationID       uuid.UUID                `db:"organization_id" json:"organizationId"`
	DriftCaseID          uuid.UUID                `db:"drift_case_id" json:"driftCaseId"`
	Action               ReconciliationActionType `db:"action" json:"action"`
	Reason               string                   `db:"reason" json:"reason"`
	ActorID              uuid.UUID                `db:"actor_id" json:"actorId"`
	DeploymentPlanID     *uuid.UUID               `db:"deployment_plan_id" json:"deploymentPlanId,omitempty"`
	OutcomeObservationID *uuid.UUID               `db:"outcome_observation_id" json:"outcomeObservationId,omitempty"`
	AcceptedUntil        *time.Time               `db:"accepted_until" json:"acceptedUntil,omitempty"`
}

type AcceptedDeviation struct {
	DesiredRevisionID uuid.UUID `json:"desiredRevisionId"`
	ObservationID     uuid.UUID `json:"observationId"`
	Reason            string    `json:"reason"`
	ExpiresAt         time.Time `json:"expiresAt"`
}
