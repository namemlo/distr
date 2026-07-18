package types

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const CampaignRevisionSchemaV1 = "distr.deployment-campaign/v1"

type CampaignMembership struct {
	PlanIDs  []uuid.UUID `json:"planIds"`
	TagQuery string      `json:"tagQuery,omitempty"`
}

type CampaignRiskPolicy struct {
	MaximumConcurrency          int `json:"maximumConcurrency"`
	FailureToleranceBasisPoints int `json:"failureToleranceBasisPoints"`
	MinimumHealthyBasisPoints   int `json:"minimumHealthyBasisPoints"`
}

type CampaignWaveDraft struct {
	Order              int         `json:"order"`
	Name               string      `json:"name"`
	PlanIDs            []uuid.UUID `json:"planIds"`
	BakeSeconds        int         `json:"bakeSeconds"`
	MaximumConcurrency int         `json:"maximumConcurrency"`
}

type CampaignPrerequisiteDraft struct {
	DownstreamPlanID              uuid.UUID `json:"downstreamPlanId"`
	UpstreamPlanID                uuid.UUID `json:"upstreamPlanId"`
	UpstreamStepKey               string    `json:"upstreamStepKey"`
	ProviderPlacementID           uuid.UUID `json:"providerPlacementId"`
	ExpectedObservedStateChecksum string    `json:"expectedObservedStateChecksum"`
}

type CampaignDraft struct {
	ID                      uuid.UUID                   `db:"id" json:"id"`
	CreatedAt               time.Time                   `db:"created_at" json:"createdAt"`
	UpdatedAt               time.Time                   `db:"updated_at" json:"updatedAt"`
	OrganizationID          uuid.UUID                   `db:"organization_id" json:"organizationId"`
	Name                    string                      `db:"name" json:"name"`
	Description             string                      `db:"description" json:"description"`
	Revision                int64                       `db:"revision" json:"revision"`
	Membership              CampaignMembership          `db:"membership" json:"membership"`
	Waves                   []CampaignWaveDraft         `db:"waves" json:"waves"`
	Prerequisites           []CampaignPrerequisiteDraft `db:"prerequisites" json:"prerequisites"`
	RiskPolicy              CampaignRiskPolicy          `db:"risk_policy" json:"riskPolicy"`
	LastPublishedRevisionID *uuid.UUID                  `db:"last_published_revision_id" json:"lastPublishedRevisionId,omitempty"`
	CreatedByUserAccountID  uuid.UUID                   `db:"created_by_useraccount_id" json:"createdByUserAccountId"`
	UpdatedByUserAccountID  uuid.UUID                   `db:"updated_by_useraccount_id" json:"updatedByUserAccountId"`
	CandidatePlans          []CampaignPlanCandidate     `db:"-" json:"-"`
}

type CampaignPlanCandidate struct {
	PlanID                        uuid.UUID
	OrganizationID                uuid.UUID
	DeploymentUnitID              uuid.UUID
	PlanChecksum                  string
	CurrentPlanChecksum           string
	EffectivePolicyChecksum       string
	ApprovalRequestID             uuid.UUID
	ApprovalRequestRevision       int64
	ApprovalChecksum              string
	Approved                      bool
	CalendarVersionIDs            []uuid.UUID
	CalendarChecksums             []string
	AdmissionEvaluationID         uuid.UUID
	AdmissionChecksum             string
	Admitted                      bool
	Tags                          []string
	ExpectedStepPlacementEvidence map[CampaignStepPlacement]CampaignStepPlacementEvidence
	SharedProviderPlacements      []uuid.UUID
}

type CampaignStepPlacement struct {
	StepKey     string
	PlacementID uuid.UUID
}

type CampaignStepPlacementEvidence struct {
	ExpectedObservedStateChecksum string
	ProviderDeploymentUnitID      uuid.UUID
	ProviderComponentInstanceID   uuid.UUID
}

type CampaignWave struct {
	ID                 uuid.UUID `db:"id" json:"id,omitempty"`
	CampaignRevisionID uuid.UUID `db:"campaign_revision_id" json:"campaignRevisionId,omitempty"`
	OrganizationID     uuid.UUID `db:"organization_id" json:"organizationId,omitempty"`
	Order              int       `db:"wave_order" json:"order"`
	Name               string    `db:"name" json:"name"`
	BakeSeconds        int       `db:"bake_seconds" json:"bakeSeconds"`
	MaximumConcurrency int       `db:"maximum_concurrency" json:"maximumConcurrency"`
}

type CampaignMember struct {
	ID                      uuid.UUID   `db:"id" json:"id,omitempty"`
	CampaignRevisionID      uuid.UUID   `db:"campaign_revision_id" json:"campaignRevisionId,omitempty"`
	OrganizationID          uuid.UUID   `db:"organization_id" json:"organizationId,omitempty"`
	PlanID                  uuid.UUID   `db:"deployment_plan_id" json:"planId"`
	DeploymentUnitID        uuid.UUID   `db:"deployment_unit_id" json:"deploymentUnitId"`
	PlanChecksum            string      `db:"plan_checksum" json:"planChecksum"`
	EffectivePolicyChecksum string      `db:"effective_policy_checksum" json:"effectivePolicyChecksum"`
	ApprovalRequestID       uuid.UUID   `db:"approval_request_id" json:"approvalRequestId"`
	ApprovalRequestRevision int64       `db:"approval_request_revision" json:"approvalRequestRevision"`
	ApprovalChecksum        string      `db:"approval_checksum" json:"approvalChecksum"`
	CalendarVersionIDs      []uuid.UUID `db:"calendar_version_ids" json:"calendarVersionIds"`
	CalendarChecksums       []string    `db:"calendar_checksums" json:"calendarChecksums"`
	AdmissionEvaluationID   uuid.UUID   `db:"admission_evaluation_id" json:"admissionEvaluationId"`
	AdmissionChecksum       string      `db:"admission_checksum" json:"admissionChecksum"`
	WaveOrder               int         `db:"wave_order" json:"waveOrder"`
	MemberOrder             int         `db:"member_order" json:"memberOrder"`
}

type CampaignPrerequisite struct {
	ID                            uuid.UUID `db:"id" json:"id,omitempty"`
	CampaignRevisionID            uuid.UUID `db:"campaign_revision_id" json:"campaignRevisionId,omitempty"`
	OrganizationID                uuid.UUID `db:"organization_id" json:"organizationId,omitempty"`
	DownstreamPlanID              uuid.UUID `db:"downstream_plan_id" json:"downstreamPlanId"`
	UpstreamPlanID                uuid.UUID `db:"upstream_plan_id" json:"upstreamPlanId"`
	UpstreamStepKey               string    `db:"upstream_step_key" json:"upstreamStepKey"`
	ProviderPlacementID           uuid.UUID `db:"provider_placement_id" json:"providerPlacementId"`
	ProviderDeploymentUnitID      uuid.UUID `db:"provider_deployment_unit_id" json:"providerDeploymentUnitId"`
	ProviderComponentInstanceID   uuid.UUID `db:"provider_component_instance_id" json:"providerComponentInstanceId"`
	ExpectedObservedStateChecksum string    `db:"expected_observed_state_checksum" json:"expectedObservedStateChecksum"`
}

type CampaignRevision struct {
	ID                       uuid.UUID              `db:"id" json:"id"`
	PublishedAt              time.Time              `db:"published_at" json:"publishedAt"`
	OrganizationID           uuid.UUID              `db:"organization_id" json:"organizationId"`
	CampaignDraftID          uuid.UUID              `db:"deployment_campaign_draft_id" json:"campaignDraftId"`
	RevisionNumber           int64                  `db:"revision_number" json:"revisionNumber"`
	SourceDraftRevision      int64                  `db:"source_draft_revision" json:"sourceDraftRevision"`
	Name                     string                 `db:"name" json:"name"`
	Description              string                 `db:"description" json:"description"`
	MembershipTagQuery       string                 `db:"membership_tag_query" json:"membershipTagQuery,omitempty"`
	RiskPolicy               CampaignRiskPolicy     `db:"risk_policy" json:"riskPolicy"`
	CanonicalPayload         json.RawMessage        `db:"canonical_payload" json:"-"`
	CanonicalChecksum        string                 `db:"canonical_checksum" json:"canonicalChecksum"`
	PublishedByUserAccountID uuid.UUID              `db:"published_by_useraccount_id" json:"publishedByUserAccountId"`
	Waves                    []CampaignWave         `db:"-" json:"waves"`
	Members                  []CampaignMember       `db:"-" json:"members"`
	Prerequisites            []CampaignPrerequisite `db:"-" json:"prerequisites"`
}

type CampaignAuthorizationContext struct {
	OrganizationID  uuid.UUID
	ActorUserID     uuid.UUID
	CampaignDraftID uuid.UUID
}

type campaignPublicationContextKey struct{}

type CampaignPublicationContext struct {
	OrganizationID uuid.UUID
	ActorUserID    uuid.UUID
}

func WithCampaignPublicationContext(
	ctx context.Context,
	publication CampaignPublicationContext,
) context.Context {
	return context.WithValue(ctx, campaignPublicationContextKey{}, publication)
}

func CampaignPublicationFromContext(
	ctx context.Context,
) (CampaignPublicationContext, bool) {
	publication, ok := ctx.Value(campaignPublicationContextKey{}).(CampaignPublicationContext)
	return publication, ok
}
