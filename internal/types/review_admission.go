package types

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type ReviewAdmissionDecision string

const (
	ReviewAdmissionDecisionGo   ReviewAdmissionDecision = "GO"
	ReviewAdmissionDecisionNoGo ReviewAdmissionDecision = "NO_GO"
)

func (decision ReviewAdmissionDecision) IsValid() bool {
	return decision == ReviewAdmissionDecisionGo || decision == ReviewAdmissionDecisionNoGo
}

type ReviewAdmissionMaterialState string

const (
	ReviewAdmissionMaterialStateMissing ReviewAdmissionMaterialState = "MISSING"
	ReviewAdmissionMaterialStateGo      ReviewAdmissionMaterialState = "GO"
	ReviewAdmissionMaterialStateNoGo    ReviewAdmissionMaterialState = "NO_GO"
	ReviewAdmissionMaterialStateStale   ReviewAdmissionMaterialState = "STALE"
)

type ReviewAdmissionDecisionRecord struct {
	ID                     uuid.UUID               `db:"id" json:"id"`
	CreatedAt              time.Time               `db:"created_at" json:"createdAt"`
	OrganizationID         uuid.UUID               `db:"organization_id" json:"organizationId"`
	DeploymentPlanID       uuid.UUID               `db:"deployment_plan_id" json:"deploymentPlanId"`
	PlanRevision           int64                   `db:"plan_revision" json:"planRevision"`
	PlanChecksum           string                  `db:"plan_checksum" json:"planChecksum"`
	ReviewMaterialChecksum string                  `db:"review_material_checksum" json:"reviewMaterialChecksum"`
	ObservedStateChecksum  string                  `db:"observed_state_checksum" json:"observedStateChecksum"`
	Decision               ReviewAdmissionDecision `db:"decision" json:"decision"`
	Reason                 string                  `db:"reason" json:"reason"`
	ActorUserAccountID     uuid.UUID               `db:"actor_useraccount_id" json:"actorUserAccountId"`
	ExpiresAt              time.Time               `db:"expires_at" json:"expiresAt"`
	SupersedesDecisionID   *uuid.UUID              `db:"supersedes_decision_id" json:"supersedesDecisionId,omitempty"`
	RevokesDecisionID      *uuid.UUID              `db:"revokes_decision_id" json:"revokesDecisionId,omitempty"`
	AuthorizationEvidence  string                  `db:"authorization_evidence" json:"authorizationEvidence"`
	CanonicalChecksum      string                  `db:"canonical_checksum" json:"canonicalChecksum"`
	IdempotencyKey         string                  `db:"idempotency_key" json:"idempotencyKey"`
}

type ReviewAdmissionMaterial struct {
	DeploymentPlanID          uuid.UUID                      `json:"deploymentPlanId"`
	PlanRevision              int64                          `json:"planRevision"`
	PlanChecksum              string                         `json:"planChecksum"`
	ObservedStateChecksum     string                         `json:"observedStateChecksum"`
	ReviewMaterialChecksum    string                         `json:"reviewMaterialChecksum"`
	ReviewMaterialValid       bool                           `json:"reviewMaterialValid"`
	AdmissionValid            bool                           `json:"admissionValid"`
	AdmissionEvaluationID     *uuid.UUID                     `json:"admissionEvaluationId,omitempty"`
	AdmissionDecision         AdmissionDecision              `json:"admissionDecision,omitempty"`
	AdmissionDecisionChecksum string                         `json:"admissionDecisionChecksum,omitempty"`
	State                     ReviewAdmissionMaterialState   `json:"state"`
	CanDecide                 bool                           `json:"canDecide"`
	Blockers                  []string                       `json:"blockers"`
	LatestDecision            *ReviewAdmissionDecisionRecord `json:"latestDecision,omitempty"`
}

type CreateReviewAdmissionDecisionRequest struct {
	OrganizationID         uuid.UUID
	DeploymentPlanID       uuid.UUID
	ActorUserAccountID     uuid.UUID
	ExpectedPlanChecksum   string
	ReviewMaterialChecksum string
	ObservedStateChecksum  string
	Decision               ReviewAdmissionDecision
	Reason                 string
	ExpiresAt              time.Time
	SupersedesDecisionID   *uuid.UUID
	RevokesDecisionID      *uuid.UUID
	IdempotencyKey         string
	Authorize              AdmissionAuthorizer
}

type ReviewAdmissionExecutionContext struct {
	OrganizationID     uuid.UUID
	DeploymentPlanID   uuid.UUID
	ActorUserAccountID uuid.UUID
	EnvironmentID      uuid.UUID
	DeploymentUnitID   *uuid.UUID
	DecisionAt         time.Time
}

type ReviewAdmissionExecutionAuthorizer func(context.Context, ReviewAdmissionExecutionContext) error
