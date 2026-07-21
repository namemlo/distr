package api

import (
	"errors"
	"fmt"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"sort"
	"strings"
	"time"
)

type CampaignMembershipRequest struct {
	PlanIDs  []uuid.UUID `json:"planIds"`
	TagQuery string      `json:"tagQuery,omitempty"`
}

type CampaignRiskPolicy struct {
	MaximumConcurrency          int `json:"maximumConcurrency"`
	FailureToleranceBasisPoints int `json:"failureToleranceBasisPoints"`
	MinimumHealthyBasisPoints   int `json:"minimumHealthyBasisPoints"`
}

type CampaignWaveRequest struct {
	Order              int         `json:"order"`
	Name               string      `json:"name"`
	PlanIDs            []uuid.UUID `json:"planIds"`
	BakeSeconds        int         `json:"bakeSeconds"`
	MaximumConcurrency int         `json:"maximumConcurrency"`
}

type CampaignPrerequisiteRequest struct {
	DownstreamPlanID             uuid.UUID `json:"downstreamPlanId"`
	UpstreamPlanID               uuid.UUID `json:"upstreamPlanId"`
	UpstreamStepKey              string    `json:"upstreamStepKey"`
	ProviderPlacementID          uuid.UUID `json:"providerPlacementId"`
	ExpectedRuntimeStateChecksum string    `json:"expectedRuntimeStateChecksum"`
}

type CreateDeploymentCampaignDraftRequest struct {
	Name          string                        `json:"name"`
	Description   string                        `json:"description"`
	Membership    CampaignMembershipRequest     `json:"membership"`
	Waves         []CampaignWaveRequest         `json:"waves"`
	Prerequisites []CampaignPrerequisiteRequest `json:"prerequisites"`
	RiskPolicy    CampaignRiskPolicy            `json:"riskPolicy"`
}

func (request CreateDeploymentCampaignDraftRequest) Validate() error {
	if strings.TrimSpace(request.Name) == "" || len(request.Name) > 200 {
		return errors.New("name must contain between 1 and 200 characters")
	}
	if len(request.Description) > 4000 {
		return errors.New("description must contain at most 4000 characters")
	}
	if len(request.Membership.PlanIDs) == 0 &&
		strings.TrimSpace(request.Membership.TagQuery) == "" {
		return errors.New("membership requires planIds or tagQuery")
	}
	if len(request.Membership.PlanIDs) > 1000 ||
		len(request.Membership.TagQuery) > 1000 {
		return errors.New("membership exceeds the supported bound")
	}
	if len(request.Waves) == 0 || len(request.Waves) > 100 {
		return errors.New("waves must contain between 1 and 100 entries")
	}
	waves := append([]CampaignWaveRequest(nil), request.Waves...)
	sort.Slice(waves, func(i, j int) bool { return waves[i].Order < waves[j].Order })
	orders := make(map[int]struct{}, len(waves))
	previousBake := -1
	for index, wave := range waves {
		if wave.Order <= 0 {
			return fmt.Errorf("waves[%d].order must be positive", index)
		}
		if _, duplicate := orders[wave.Order]; duplicate {
			return fmt.Errorf("waves[%d].order must be unique", index)
		}
		orders[wave.Order] = struct{}{}
		if strings.TrimSpace(wave.Name) == "" || len(wave.Name) > 200 {
			return fmt.Errorf("waves[%d].name is invalid", index)
		}
		if wave.BakeSeconds < 0 || wave.BakeSeconds > 31536000 {
			return fmt.Errorf("waves[%d].bakeSeconds is invalid", index)
		}
		if previousBake >= 0 && wave.BakeSeconds < previousBake {
			return errors.New("wave bake durations must be non-decreasing")
		}
		previousBake = wave.BakeSeconds
		if wave.MaximumConcurrency <= 0 || wave.MaximumConcurrency > 1000 {
			return fmt.Errorf("waves[%d].maximumConcurrency is invalid", index)
		}
		if len(wave.PlanIDs) > 1000 {
			return fmt.Errorf("waves[%d].planIds exceeds the supported bound", index)
		}
	}
	if len(request.Prerequisites) > 5000 {
		return errors.New("prerequisites exceeds the supported bound")
	}
	if request.RiskPolicy.MaximumConcurrency <= 0 ||
		request.RiskPolicy.MaximumConcurrency > 1000 ||
		request.RiskPolicy.FailureToleranceBasisPoints < 0 ||
		request.RiskPolicy.FailureToleranceBasisPoints > 10000 ||
		request.RiskPolicy.MinimumHealthyBasisPoints < 0 ||
		request.RiskPolicy.MinimumHealthyBasisPoints > 10000 {
		return errors.New("riskPolicy is invalid")
	}
	return nil
}

func (request CreateDeploymentCampaignDraftRequest) ToDomain(
	organizationID uuid.UUID,
	actorUserID uuid.UUID,
) types.CampaignDraft {
	waves := make([]types.CampaignWaveDraft, len(request.Waves))
	for index, wave := range request.Waves {
		waves[index] = types.CampaignWaveDraft{
			Order:              wave.Order,
			Name:               strings.TrimSpace(wave.Name),
			PlanIDs:            append([]uuid.UUID(nil), wave.PlanIDs...),
			BakeSeconds:        wave.BakeSeconds,
			MaximumConcurrency: wave.MaximumConcurrency,
		}
	}
	prerequisites := make(
		[]types.CampaignPrerequisiteDraft,
		len(request.Prerequisites),
	)
	for index, prerequisite := range request.Prerequisites {
		prerequisites[index] = types.CampaignPrerequisiteDraft{
			DownstreamPlanID:             prerequisite.DownstreamPlanID,
			UpstreamPlanID:               prerequisite.UpstreamPlanID,
			UpstreamStepKey:              strings.TrimSpace(prerequisite.UpstreamStepKey),
			ProviderPlacementID:          prerequisite.ProviderPlacementID,
			ExpectedRuntimeStateChecksum: prerequisite.ExpectedRuntimeStateChecksum,
		}
	}
	return types.CampaignDraft{
		OrganizationID: organizationID,
		Name:           strings.TrimSpace(request.Name),
		Description:    request.Description,
		Membership: types.CampaignMembership{
			PlanIDs:  append([]uuid.UUID(nil), request.Membership.PlanIDs...),
			TagQuery: strings.TrimSpace(request.Membership.TagQuery),
		},
		Waves:         waves,
		Prerequisites: prerequisites,
		RiskPolicy: types.CampaignRiskPolicy{
			MaximumConcurrency:          request.RiskPolicy.MaximumConcurrency,
			FailureToleranceBasisPoints: request.RiskPolicy.FailureToleranceBasisPoints,
			MinimumHealthyBasisPoints:   request.RiskPolicy.MinimumHealthyBasisPoints,
		},
		CreatedByUserAccountID: actorUserID,
		UpdatedByUserAccountID: actorUserID,
	}
}

type UpdateDeploymentCampaignDraftRequest struct {
	CreateDeploymentCampaignDraftRequest
	ExpectedRevision int64 `json:"expectedRevision"`
}

func (request UpdateDeploymentCampaignDraftRequest) Validate() error {
	if request.ExpectedRevision <= 0 {
		return errors.New("expectedRevision must be positive")
	}
	return request.CreateDeploymentCampaignDraftRequest.Validate()
}

type PublishDeploymentCampaignRevisionRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
}

func (request PublishDeploymentCampaignRevisionRequest) Validate() error {
	if strings.TrimSpace(request.IdempotencyKey) == "" ||
		len(request.IdempotencyKey) > 128 {
		return errors.New("idempotencyKey must contain between 1 and 128 characters")
	}
	return nil
}

type DeploymentCampaignDraft struct {
	ID                      uuid.UUID                     `json:"id"`
	CreatedAt               time.Time                     `json:"createdAt"`
	UpdatedAt               time.Time                     `json:"updatedAt"`
	OrganizationID          uuid.UUID                     `json:"organizationId"`
	Name                    string                        `json:"name"`
	Description             string                        `json:"description"`
	Revision                int64                         `json:"revision"`
	Membership              CampaignMembershipRequest     `json:"membership"`
	Waves                   []CampaignWaveRequest         `json:"waves"`
	Prerequisites           []CampaignPrerequisiteRequest `json:"prerequisites"`
	RiskPolicy              CampaignRiskPolicy            `json:"riskPolicy"`
	LastPublishedRevisionID *uuid.UUID                    `json:"lastPublishedRevisionId,omitempty"`
}

type CampaignWave struct {
	Order              int    `json:"order"`
	Name               string `json:"name"`
	BakeSeconds        int    `json:"bakeSeconds"`
	MaximumConcurrency int    `json:"maximumConcurrency"`
}

type CampaignMember struct {
	PlanID                  uuid.UUID   `json:"planId"`
	DeploymentUnitID        uuid.UUID   `json:"deploymentUnitId"`
	PlanChecksum            string      `json:"planChecksum"`
	EffectivePolicyChecksum string      `json:"effectivePolicyChecksum"`
	ApprovalRequestID       uuid.UUID   `json:"approvalRequestId"`
	ApprovalRequestRevision int64       `json:"approvalRequestRevision"`
	ApprovalChecksum        string      `json:"approvalChecksum"`
	CalendarVersionIDs      []uuid.UUID `json:"calendarVersionIds"`
	CalendarChecksums       []string    `json:"calendarChecksums"`
	AdmissionEvaluationID   uuid.UUID   `json:"admissionEvaluationId"`
	AdmissionChecksum       string      `json:"admissionChecksum"`
	WaveOrder               int         `json:"waveOrder"`
	MemberOrder             int         `json:"memberOrder"`
}

type CampaignPrerequisite struct {
	DownstreamPlanID             uuid.UUID `json:"downstreamPlanId"`
	UpstreamPlanID               uuid.UUID `json:"upstreamPlanId"`
	UpstreamStepKey              string    `json:"upstreamStepKey"`
	ProviderPlacementID          uuid.UUID `json:"providerPlacementId"`
	ProviderDeploymentUnitID     uuid.UUID `json:"providerDeploymentUnitId"`
	ProviderComponentInstanceID  uuid.UUID `json:"providerComponentInstanceId"`
	ExpectedRuntimeStateChecksum string    `json:"expectedRuntimeStateChecksum"`
}

type DeploymentCampaignRevision struct {
	ID                       uuid.UUID              `json:"id"`
	PublishedAt              time.Time              `json:"publishedAt"`
	OrganizationID           uuid.UUID              `json:"organizationId"`
	CampaignDraftID          uuid.UUID              `json:"campaignDraftId"`
	RevisionNumber           int64                  `json:"revisionNumber"`
	SourceDraftRevision      int64                  `json:"sourceDraftRevision"`
	Name                     string                 `json:"name"`
	Description              string                 `json:"description"`
	MembershipTagQuery       string                 `json:"membershipTagQuery,omitempty"`
	RiskPolicy               CampaignRiskPolicy     `json:"riskPolicy"`
	CanonicalChecksum        string                 `json:"canonicalChecksum"`
	PublishedByUserAccountID uuid.UUID              `json:"publishedByUserAccountId"`
	Waves                    []CampaignWave         `json:"waves"`
	Members                  []CampaignMember       `json:"members"`
	Prerequisites            []CampaignPrerequisite `json:"prerequisites"`
}

type DeploymentCampaignValidationResponse struct {
	Valid  bool                    `json:"valid"`
	Issues []types.ValidationIssue `json:"issues"`
}

type DeploymentCampaignRun struct {
	ID                     uuid.UUID              `json:"id"`
	CreatedAt              time.Time              `json:"createdAt"`
	UpdatedAt              time.Time              `json:"updatedAt"`
	CampaignRevisionID     uuid.UUID              `json:"campaignRevisionId"`
	State                  types.CampaignRunState `json:"state"`
	Version                int64                  `json:"version"`
	CurrentWaveOrder       int                    `json:"currentWaveOrder"`
	CurrentMemberOrder     int                    `json:"currentMemberOrder"`
	AdmissionsBlocked      bool                   `json:"admissionsBlocked"`
	ResumeState            types.CampaignRunState `json:"resumeState,omitempty"`
	PauseRequested         bool                   `json:"pauseRequested"`
	ReconciliationRequired bool                   `json:"reconciliationRequired"`
	FencingToken           int64                  `json:"fencingToken"`
	LeaseExpiresAt         *time.Time             `json:"leaseExpiresAt,omitempty"`
}

type StartDeploymentCampaignRunRequest struct {
	CampaignRevisionID uuid.UUID `json:"campaignRevisionId"`
}

type TransitionDeploymentCampaignRunRequest struct {
	ExpectedVersion int64                  `json:"expectedVersion"`
	To              types.CampaignRunState `json:"to"`
	Reason          string                 `json:"reason"`
}

func (request TransitionDeploymentCampaignRunRequest) Validate() error {
	if request.ExpectedVersion < 1 {
		return errors.New("expectedVersion must be positive")
	}
	if strings.TrimSpace(request.Reason) == "" || request.Reason != strings.TrimSpace(request.Reason) {
		return errors.New("reason is required and must be trimmed")
	}
	if len(request.Reason) > 4000 {
		return errors.New("reason must be at most 4000 characters")
	}
	return nil
}

func (request StartDeploymentCampaignRunRequest) Validate() error {
	if request.CampaignRevisionID == uuid.Nil {
		return errors.New("campaignRevisionId is required")
	}
	return nil
}

type CampaignControlRequest struct {
	RequestID       uuid.UUID `json:"requestId"`
	ExpectedVersion int64     `json:"expectedVersion"`
	Reason          string    `json:"reason"`
}

func (request CampaignControlRequest) Validate() error {
	if request.RequestID == uuid.Nil {
		return errors.New("requestId is required")
	}
	if request.ExpectedVersion <= 0 {
		return errors.New("expectedVersion must be positive")
	}
	if strings.TrimSpace(request.Reason) == "" {
		return errors.New("reason is required")
	}
	if request.Reason != strings.TrimSpace(request.Reason) {
		return errors.New("reason must be trimmed")
	}
	if len(request.Reason) > 4000 {
		return errors.New("reason must be at most 4000 characters")
	}
	return nil
}

type CampaignMemberControlRequest struct {
	CampaignControlRequest
	MemberRunID     uuid.UUID `json:"memberRunId"`
	ProtocolVersion string    `json:"protocolVersion,omitempty"`
}

func (request CampaignMemberControlRequest) Validate(retry bool) error {
	if err := request.CampaignControlRequest.Validate(); err != nil {
		return err
	}
	if request.MemberRunID == uuid.Nil {
		return errors.New("memberRunId is required")
	}
	if retry && request.ProtocolVersion != "v1" && request.ProtocolVersion != "v2" {
		return errors.New("protocolVersion must be v1 or v2")
	}
	return nil
}

type DeploymentCampaignControlResult struct {
	RequestID              uuid.UUID                   `json:"requestId"`
	Status                 types.CampaignControlStatus `json:"status"`
	Run                    DeploymentCampaignRun       `json:"run"`
	PausePending           bool                        `json:"pausePending"`
	ReconciliationRequired bool                        `json:"reconciliationRequired"`
	Duplicate              bool                        `json:"duplicate"`
}

type DeploymentCampaignExclusion struct {
	ID                uuid.UUID `json:"id"`
	CampaignRunID     uuid.UUID `json:"campaignRunId"`
	MemberRunID       uuid.UUID `json:"memberRunId"`
	Reason            string    `json:"reason"`
	VisibleIncomplete bool      `json:"visibleIncomplete"`
	DriftReason       string    `json:"driftReason"`
	ExcludedAt        time.Time `json:"excludedAt"`
}
