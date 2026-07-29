package api

import (
	"encoding/base64"
	"fmt"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

type OperatorPageRequest struct {
	Cursor string `query:"cursor"`
	Limit  *int   `query:"limit"`
}

func (request OperatorPageRequest) Validate() error {
	if request.Limit != nil &&
		(*request.Limit < 1 || *request.Limit > types.OperatorMaximumPageLimit) {
		return fmt.Errorf("limit must be between 1 and 100")
	}
	if request.Cursor != "" {
		if len(request.Cursor) > 2048 {
			return fmt.Errorf("cursor is invalid")
		}
		if _, err := base64.RawURLEncoding.Strict().DecodeString(request.Cursor); err != nil {
			return fmt.Errorf("cursor is invalid")
		}
	}
	return nil
}

func (request OperatorPageRequest) ToPageRequest() types.PageRequest {
	limit := types.OperatorDefaultPageLimit
	if request.Limit != nil {
		limit = *request.Limit
	}
	return types.PageRequest{Cursor: request.Cursor, Limit: limit}
}

type OperatorFleetListRequest struct {
	OperatorPageRequest
	CustomerOrganizationID *uuid.UUID `query:"customerOrganizationId"`
	EnvironmentID          *uuid.UUID `query:"environmentId"`
	DeploymentTargetID     *uuid.UUID `query:"deploymentTargetId"`
	DeploymentUnitID       *uuid.UUID `query:"deploymentUnitId"`
	Component              string     `query:"component"`
	ObservedState          string     `query:"observedState"`
	Drift                  string     `query:"drift"`
	Enrollment             string     `query:"enrollment"`
	Search                 string     `query:"search"`
}

func (request OperatorFleetListRequest) ToFilter(
	scope types.OperatorScopeFilter,
) types.FleetFilter {
	return types.FleetFilter{
		OperatorScopeFilter:    scope,
		CustomerOrganizationID: request.CustomerOrganizationID,
		EnvironmentID:          request.EnvironmentID,
		DeploymentTargetID:     request.DeploymentTargetID,
		DeploymentUnitID:       request.DeploymentUnitID,
		Component:              request.Component,
		ObservedState:          request.ObservedState,
		Drift:                  request.Drift,
		Enrollment:             request.Enrollment,
		Search:                 request.Search,
	}
}

type OperatorReleaseListRequest struct {
	OperatorPageRequest
	CustomerOrganizationID *uuid.UUID `query:"customerOrganizationId"`
	ApplicationID          *uuid.UUID `query:"applicationId"`
	DeploymentUnitID       *uuid.UUID `query:"deploymentUnitId"`
	Kind                   string     `query:"kind"`
	Status                 string     `query:"status"`
	Search                 string     `query:"search"`
}

func (request OperatorReleaseListRequest) ToFilter(
	scope types.OperatorScopeFilter,
) types.ReleaseFilter {
	return types.ReleaseFilter{
		OperatorScopeFilter:    scope,
		CustomerOrganizationID: request.CustomerOrganizationID,
		ApplicationID:          request.ApplicationID,
		DeploymentUnitID:       request.DeploymentUnitID,
		Kind:                   request.Kind,
		Status:                 request.Status,
		Search:                 request.Search,
	}
}

type OperatorPlanListRequest struct {
	OperatorPageRequest
	Status           string     `query:"status"`
	EnvironmentID    *uuid.UUID `query:"environmentId"`
	DeploymentUnitID *uuid.UUID `query:"deploymentUnitId"`
	ProductReleaseID *uuid.UUID `query:"productReleaseId"`
}

func (request OperatorPlanListRequest) ToFilter(
	scope types.OperatorScopeFilter,
) types.OperatorPlanFilter {
	return types.OperatorPlanFilter{
		OperatorScopeFilter: scope,
		Status:              request.Status,
		EnvironmentID:       request.EnvironmentID,
		DeploymentUnitID:    request.DeploymentUnitID,
		ProductReleaseID:    request.ProductReleaseID,
	}
}

type OperatorCampaignListRequest struct {
	OperatorPageRequest
	Status           string     `query:"status"`
	EnvironmentID    *uuid.UUID `query:"environmentId"`
	DeploymentPlanID *uuid.UUID `query:"deploymentPlanId"`
}

func (request OperatorCampaignListRequest) ToFilter(
	scope types.OperatorScopeFilter,
) types.CampaignFilter {
	return types.CampaignFilter{
		OperatorScopeFilter: scope,
		Status:              request.Status,
		EnvironmentID:       request.EnvironmentID,
		DeploymentPlanID:    request.DeploymentPlanID,
	}
}

type OperatorExecutionListRequest struct {
	OperatorPageRequest
	Status             string     `query:"status"`
	CampaignID         *uuid.UUID `query:"campaignId"`
	DeploymentPlanID   *uuid.UUID `query:"deploymentPlanId"`
	DeploymentTargetID *uuid.UUID `query:"deploymentTargetId"`
	From               *time.Time `query:"from"`
	To                 *time.Time `query:"to"`
}

func (request OperatorExecutionListRequest) ToFilter(
	scope types.OperatorScopeFilter,
) types.ExecutionFilter {
	return types.ExecutionFilter{
		OperatorScopeFilter: scope,
		Status:              request.Status,
		CampaignID:          request.CampaignID,
		DeploymentPlanID:    request.DeploymentPlanID,
		DeploymentTargetID:  request.DeploymentTargetID,
		From:                request.From,
		To:                  request.To,
	}
}

type OperatorReconciliationListRequest struct {
	OperatorPageRequest
	Status             string     `query:"status"`
	Drift              string     `query:"drift"`
	EnvironmentID      *uuid.UUID `query:"environmentId"`
	DeploymentTargetID *uuid.UUID `query:"deploymentTargetId"`
}

func (request OperatorReconciliationListRequest) ToFilter(
	scope types.OperatorScopeFilter,
) types.ReconciliationFilter {
	return types.ReconciliationFilter{
		OperatorScopeFilter: scope,
		Status:              request.Status,
		Drift:               request.Drift,
		EnvironmentID:       request.EnvironmentID,
		DeploymentTargetID:  request.DeploymentTargetID,
	}
}

type OperatorAuditListRequest struct {
	OperatorPageRequest
	Action             string     `query:"action"`
	SubjectType        string     `query:"subjectType"`
	SubjectID          *uuid.UUID `query:"subjectId"`
	ActorUserAccountID *uuid.UUID `query:"actorUserAccountId"`
	From               *time.Time `query:"from"`
	To                 *time.Time `query:"to"`
	Search             string     `query:"search"`
}

func (request OperatorAuditListRequest) ToFilter(
	scope types.OperatorScopeFilter,
) types.AuditFilter {
	return types.AuditFilter{
		OperatorScopeFilter: scope,
		Action:              request.Action,
		SubjectType:         request.SubjectType,
		SubjectID:           request.SubjectID,
		ActorUserAccountID:  request.ActorUserAccountID,
		From:                request.From,
		To:                  request.To,
		Search:              request.Search,
	}
}

type OperatorReleaseIDRequest struct {
	ReleaseID        uuid.UUID  `path:"releaseId"`
	DeploymentUnitID *uuid.UUID `query:"deploymentUnitId"`
}

type OperatorReleaseCompareRequest struct {
	ReleaseID      uuid.UUID `path:"releaseId"`
	OtherReleaseID uuid.UUID `path:"otherReleaseId"`
}

type OperatorPlanIDRequest struct {
	PlanID uuid.UUID `path:"planId"`
}

type OperatorPlanCompareRequest struct {
	PlanID      uuid.UUID `path:"planId"`
	OtherPlanID uuid.UUID `path:"otherPlanId"`
}

type OperatorCampaignIDRequest struct {
	CampaignID uuid.UUID `path:"campaignId"`
}

type OperatorExecutionIDRequest struct {
	ExecutionID uuid.UUID `path:"executionId"`
}

type OperatorReconciliationIDRequest struct {
	ReconciliationID uuid.UUID `path:"reconciliationId"`
}

type OperatorAuditIDRequest struct {
	AuditEventID uuid.UUID `path:"auditEventId"`
}

type OperatorFleetPage struct {
	Items      []types.FleetRow `json:"items"`
	NextCursor string           `json:"nextCursor,omitempty"`
	Total      *int64           `json:"total,omitempty"`
}

type OperatorReleasePage struct {
	Items      []types.OperatorReleaseRow `json:"items"`
	NextCursor string                     `json:"nextCursor,omitempty"`
	Total      *int64                     `json:"total,omitempty"`
}

type OperatorPlanPage struct {
	Items      []types.OperatorPlanRow `json:"items"`
	NextCursor string                  `json:"nextCursor,omitempty"`
	Total      *int64                  `json:"total,omitempty"`
}

type OperatorCampaignPage struct {
	Items      []types.OperatorCampaignRow `json:"items"`
	NextCursor string                      `json:"nextCursor,omitempty"`
	Total      *int64                      `json:"total,omitempty"`
}

type OperatorExecutionPage struct {
	Items      []types.OperatorExecutionRow `json:"items"`
	NextCursor string                       `json:"nextCursor,omitempty"`
	Total      *int64                       `json:"total,omitempty"`
}

type OperatorReconciliationPage struct {
	Items      []types.OperatorReconciliationRow `json:"items"`
	NextCursor string                            `json:"nextCursor,omitempty"`
	Total      *int64                            `json:"total,omitempty"`
}

type OperatorAuditPage struct {
	Items      []types.OperatorAuditRow `json:"items"`
	NextCursor string                   `json:"nextCursor,omitempty"`
	Total      *int64                   `json:"total,omitempty"`
}

type OperatorEvidencePage struct {
	Items      []types.OperatorEvidenceRef `json:"items"`
	NextCursor string                      `json:"nextCursor,omitempty"`
}

type OperatorReleaseDetailResponse struct {
	Detail types.OperatorReleaseDetail `json:"detail"`
}

type OperatorReleaseCompareResponse struct {
	Comparison types.OperatorReleaseCompare `json:"comparison"`
}

type OperatorPlanDetailResponse struct {
	Detail types.OperatorPlanDetail `json:"detail"`
}

type OperatorPlanCompareResponse struct {
	Comparison types.OperatorPlanCompare `json:"comparison"`
}

type OperatorCampaignDetailResponse struct {
	Detail types.OperatorCampaignDetail `json:"detail"`
}

type OperatorExecutionDetailResponse struct {
	Detail types.OperatorExecutionDetail `json:"detail"`
}

type OperatorReconciliationDetailResponse struct {
	Detail types.OperatorReconciliationDetail `json:"detail"`
}

type OperatorAuditDetailResponse struct {
	Detail types.OperatorAuditDetail `json:"detail"`
}
