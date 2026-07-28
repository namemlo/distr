package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/env"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/mapping"
	"github.com/distr-sh/distr/internal/middleware"
	"github.com/distr-sh/distr/internal/operatorqueries"
	"github.com/distr-sh/distr/internal/types"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/oaswrap/spec/adapter/chiopenapi"
	"github.com/oaswrap/spec/option"
)

var errOperatorReadForbidden = errors.New("operator read access denied")

func operatorControlPlaneFeatureGateWithFlags(
	flags []featureflags.Key,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !featureflags.NewRegistry(flags).IsEnabled(
				featureflags.KeyOperatorControlPlaneV2,
			) {
				http.NotFound(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type operatorReadPrincipal struct {
	OrganizationID uuid.UUID
	PrincipalID    uuid.UUID
	CredentialRole *types.UserRole
	SuperAdmin     bool
}

type operatorReadScopeDependencies struct {
	clock            func() time.Time
	listAccessGrants func(
		context.Context,
		uuid.UUID,
		uuid.UUID,
		time.Time,
	) ([]types.AccessGrant, error)
	getLegacyRole func(
		context.Context,
		uuid.UUID,
		uuid.UUID,
	) (*types.UserRole, error)
}

type operatorControlPlaneDependencies struct {
	resolveScopes func(
		context.Context,
		operatorReadPrincipal,
	) (operatorqueries.AuditViewScopes, error)
	listFleet func(
		context.Context,
		types.FleetFilter,
		types.PageRequest,
	) (types.OperatorPage[types.FleetRow], error)
	listReleases func(
		context.Context,
		types.ReleaseFilter,
		types.PageRequest,
	) (types.OperatorPage[types.OperatorReleaseRow], error)
	getRelease func(
		context.Context,
		types.OperatorScopeFilter,
		uuid.UUID,
	) (*types.OperatorReleaseDetail, error)
	compareReleases func(
		context.Context,
		types.OperatorScopeFilter,
		uuid.UUID,
		uuid.UUID,
	) (*types.OperatorReleaseCompare, error)
	listPlans func(
		context.Context,
		types.OperatorPlanFilter,
		types.PageRequest,
	) (types.OperatorPage[types.OperatorPlanRow], error)
	getPlan func(
		context.Context,
		types.OperatorScopeFilter,
		uuid.UUID,
	) (*types.OperatorPlanDetail, error)
	comparePlans func(
		context.Context,
		types.OperatorScopeFilter,
		uuid.UUID,
		uuid.UUID,
	) (*types.OperatorPlanCompare, error)
	listCampaigns func(
		context.Context,
		types.CampaignFilter,
		types.PageRequest,
	) (types.OperatorPage[types.OperatorCampaignRow], error)
	getCampaign func(
		context.Context,
		uuid.UUID,
		types.CampaignFilter,
	) (*types.OperatorCampaignDetail, error)
	listExecutions func(
		context.Context,
		types.ExecutionFilter,
		operatorqueries.AuditViewScopes,
		types.PageRequest,
	) (types.OperatorPage[types.OperatorExecutionRow], error)
	getExecution func(
		context.Context,
		uuid.UUID,
		operatorqueries.AuditViewScopes,
		uuid.UUID,
	) (*types.OperatorExecutionDetail, error)
	listReconciliation func(
		context.Context,
		types.ReconciliationFilter,
		types.PageRequest,
	) (types.OperatorPage[types.OperatorReconciliationRow], error)
	getReconciliation func(
		context.Context,
		types.OperatorScopeFilter,
		uuid.UUID,
	) (*types.OperatorReconciliationDetail, error)
	listReconciliationEvidence func(
		context.Context,
		types.OperatorScopeFilter,
		uuid.UUID,
	) ([]types.OperatorEvidenceRef, error)
	searchAudit func(
		context.Context,
		types.AuditFilter,
		types.PageRequest,
	) (types.OperatorPage[types.OperatorAuditRow], error)
	getAudit func(
		context.Context,
		types.OperatorScopeFilter,
		uuid.UUID,
	) (*types.OperatorAuditDetail, error)
	listAuditEvidence func(
		context.Context,
		types.OperatorScopeFilter,
		uuid.UUID,
	) ([]types.OperatorEvidenceRef, error)
}

func OperatorControlPlaneRouter(r chiopenapi.Router) {
	operatorControlPlaneRouterWithDependencies(
		r,
		env.ExperimentalFeatureFlags(),
		defaultOperatorControlPlaneDependencies(),
	)
}

func defaultOperatorControlPlaneDependencies() operatorControlPlaneDependencies {
	executionRepository := db.OperatorExecutionRepository{}
	return operatorControlPlaneDependencies{
		resolveScopes: func(
			ctx context.Context,
			principal operatorReadPrincipal,
		) (operatorqueries.AuditViewScopes, error) {
			return resolveOperatorReadScopes(ctx, principal, operatorReadScopeDependencies{
				clock:            time.Now,
				listAccessGrants: db.ListAuthorizationAccessGrants,
				getLegacyRole:    db.GetAuthorizationLegacyUserRole,
			})
		},
		listFleet:                  operatorqueries.ListFleet,
		listReleases:               operatorqueries.ListOperatorReleases,
		getRelease:                 operatorqueries.GetOperatorRelease,
		compareReleases:            operatorqueries.CompareOperatorReleases,
		listPlans:                  operatorqueries.ListOperatorPlans,
		getPlan:                    operatorqueries.GetOperatorPlan,
		listCampaigns:              operatorqueries.ListOperatorCampaigns,
		getCampaign:                operatorqueries.GetOperatorCampaign,
		listReconciliation:         operatorqueries.ListOperatorReconciliation,
		getReconciliation:          operatorqueries.GetOperatorReconciliation,
		listReconciliationEvidence: operatorqueries.ListOperatorReconciliationEvidence,
		searchAudit:                operatorqueries.SearchOperatorAudit,
		getAudit:                   operatorqueries.GetOperatorAudit,
		listAuditEvidence:          operatorqueries.ListOperatorAuditEvidence,
		listExecutions: func(
			ctx context.Context,
			filter types.ExecutionFilter,
			scopes operatorqueries.AuditViewScopes,
			page types.PageRequest,
		) (types.OperatorPage[types.OperatorExecutionRow], error) {
			return operatorqueries.ListOperatorExecutions(
				ctx, executionRepository, filter, scopes, page,
			)
		},
		getExecution: func(
			ctx context.Context,
			organizationID uuid.UUID,
			scopes operatorqueries.AuditViewScopes,
			executionID uuid.UUID,
		) (*types.OperatorExecutionDetail, error) {
			return operatorqueries.GetOperatorExecution(
				ctx, executionRepository, organizationID, scopes, executionID,
			)
		},
		comparePlans: func(
			ctx context.Context,
			scope types.OperatorScopeFilter,
			leftID uuid.UUID,
			rightID uuid.UUID,
		) (*types.OperatorPlanCompare, error) {
			left, err := operatorqueries.GetOperatorPlan(ctx, scope, leftID)
			if err != nil {
				return nil, err
			}
			right, err := operatorqueries.GetOperatorPlan(ctx, scope, rightID)
			if err != nil {
				return nil, err
			}
			if left == nil || right == nil {
				return nil, apierrors.ErrNotFound
			}
			comparison := compareOperatorPlanDetails(*left, *right)
			return &comparison, nil
		},
	}
}

func operatorControlPlaneRouterWithDependencies(
	r chiopenapi.Router,
	enabledFlags []featureflags.Key,
	dependencies operatorControlPlaneDependencies,
) {
	r.WithOptions(option.GroupTags("Operator Control Plane"))
	r.With(
		operatorControlPlaneFeatureGateWithFlags(enabledFlags),
		middleware.RequireOrgAndRole,
	).Group(func(r chiopenapi.Router) {
		r.Get("/fleet", operatorFleetHandler(dependencies)).
			With(option.Description("List scoped operator fleet state")).
			With(option.Request(api.OperatorFleetListRequest{})).
			With(option.Response(http.StatusOK, api.OperatorFleetPage{})).
			With(operatorListErrorResponses()...)
		r.Route("/releases", func(r chiopenapi.Router) {
			r.Get("/", operatorReleaseListHandler(dependencies)).
				With(option.Description("List scoped operator releases")).
				With(option.Request(api.OperatorReleaseListRequest{})).
				With(option.Response(http.StatusOK, api.OperatorReleasePage{})).
				With(operatorListErrorResponses()...)
			r.Route("/{releaseId}", func(r chiopenapi.Router) {
				r.Get("/", operatorReleaseDetailHandler(dependencies)).
					With(option.Description("Get a scoped operator release")).
					With(option.Request(api.OperatorReleaseIDRequest{})).
					With(option.Response(http.StatusOK, api.OperatorReleaseDetailResponse{})).
					With(operatorDetailErrorResponses()...)
				r.Get("/evidence", operatorReleaseEvidenceHandler(dependencies)).
					With(option.Description("List scoped operator release evidence")).
					With(option.Request(api.OperatorReleaseIDRequest{})).
					With(option.Response(http.StatusOK, api.OperatorEvidencePage{})).
					With(operatorDetailErrorResponses()...)
				r.Get("/compare/{otherReleaseId}", operatorReleaseCompareHandler(dependencies)).
					With(option.Description("Compare two scoped operator releases")).
					With(option.Request(api.OperatorReleaseCompareRequest{})).
					With(option.Response(http.StatusOK, api.OperatorReleaseCompareResponse{})).
					With(operatorDetailErrorResponses()...)
			})
		})
		r.Route("/plans", func(r chiopenapi.Router) {
			r.Get("/", operatorPlanListHandler(dependencies)).
				With(option.Description("List scoped operator deployment plans")).
				With(option.Request(api.OperatorPlanListRequest{})).
				With(option.Response(http.StatusOK, api.OperatorPlanPage{})).
				With(operatorListErrorResponses()...)
			r.Route("/{planId}", func(r chiopenapi.Router) {
				r.Get("/", operatorPlanDetailHandler(dependencies)).
					With(option.Description("Get a scoped operator deployment plan")).
					With(option.Request(api.OperatorPlanIDRequest{})).
					With(option.Response(http.StatusOK, api.OperatorPlanDetailResponse{})).
					With(operatorDetailErrorResponses()...)
				r.Get("/evidence", operatorPlanEvidenceHandler(dependencies)).
					With(option.Description("List scoped operator deployment plan evidence")).
					With(option.Request(api.OperatorPlanIDRequest{})).
					With(option.Response(http.StatusOK, api.OperatorEvidencePage{})).
					With(operatorDetailErrorResponses()...)
				r.Get("/compare/{otherPlanId}", operatorPlanCompareHandler(dependencies)).
					With(option.Description("Compare two scoped operator deployment plans")).
					With(option.Request(api.OperatorPlanCompareRequest{})).
					With(option.Response(http.StatusOK, api.OperatorPlanCompareResponse{})).
					With(operatorDetailErrorResponses()...)
			})
		})
		r.Route("/campaigns", func(r chiopenapi.Router) {
			r.Get("/", operatorCampaignListHandler(dependencies)).
				With(option.Description("List scoped operator campaigns")).
				With(option.Request(api.OperatorCampaignListRequest{})).
				With(option.Response(http.StatusOK, api.OperatorCampaignPage{})).
				With(operatorListErrorResponses()...)
			r.Route("/{campaignId}", func(r chiopenapi.Router) {
				r.Get("/", operatorCampaignDetailHandler(dependencies)).
					With(option.Description("Get a scoped operator campaign")).
					With(option.Request(api.OperatorCampaignIDRequest{})).
					With(option.Response(http.StatusOK, api.OperatorCampaignDetailResponse{})).
					With(operatorDetailErrorResponses()...)
				r.Get("/evidence", operatorCampaignEvidenceHandler(dependencies)).
					With(option.Description("List scoped operator campaign evidence")).
					With(option.Request(api.OperatorCampaignIDRequest{})).
					With(option.Response(http.StatusOK, api.OperatorEvidencePage{})).
					With(operatorDetailErrorResponses()...)
			})
		})
		r.Route("/executions", func(r chiopenapi.Router) {
			r.Get("/", operatorExecutionListHandler(dependencies)).
				With(option.Description("List scoped operator executions")).
				With(option.Request(api.OperatorExecutionListRequest{})).
				With(option.Response(http.StatusOK, api.OperatorExecutionPage{})).
				With(operatorListErrorResponses()...)
			r.Route("/{executionId}", func(r chiopenapi.Router) {
				r.Get("/", operatorExecutionDetailHandler(dependencies)).
					With(option.Description("Get a scoped operator execution")).
					With(option.Request(api.OperatorExecutionIDRequest{})).
					With(option.Response(http.StatusOK, api.OperatorExecutionDetailResponse{})).
					With(operatorDetailErrorResponses()...)
				r.Get("/evidence", operatorExecutionEvidenceHandler(dependencies)).
					With(option.Description("List scoped operator execution evidence")).
					With(option.Request(api.OperatorExecutionIDRequest{})).
					With(option.Response(http.StatusOK, api.OperatorEvidencePage{})).
					With(operatorDetailErrorResponses()...)
			})
		})
		r.Route("/reconciliation", func(r chiopenapi.Router) {
			r.Get("/", operatorReconciliationListHandler(dependencies)).
				With(option.Description("List scoped operator reconciliation cases")).
				With(option.Request(api.OperatorReconciliationListRequest{})).
				With(option.Response(http.StatusOK, api.OperatorReconciliationPage{})).
				With(operatorListErrorResponses()...)
			r.Route("/{reconciliationId}", func(r chiopenapi.Router) {
				r.Get("/", operatorReconciliationDetailHandler(dependencies)).
					With(option.Description("Get a scoped operator reconciliation case")).
					With(option.Request(api.OperatorReconciliationIDRequest{})).
					With(option.Response(http.StatusOK, api.OperatorReconciliationDetailResponse{})).
					With(operatorDetailErrorResponses()...)
				r.Get("/evidence", operatorReconciliationEvidenceHandler(dependencies)).
					With(option.Description("List scoped operator reconciliation evidence")).
					With(option.Request(api.OperatorReconciliationIDRequest{})).
					With(option.Response(http.StatusOK, api.OperatorEvidencePage{})).
					With(operatorDetailErrorResponses()...)
			})
		})
		r.Route("/audit", func(r chiopenapi.Router) {
			r.Get("/", operatorAuditListHandler(dependencies)).
				With(option.Description("Search scoped operator audit events")).
				With(option.Request(api.OperatorAuditListRequest{})).
				With(option.Response(http.StatusOK, api.OperatorAuditPage{})).
				With(operatorListErrorResponses()...)
			r.Route("/{auditEventId}", func(r chiopenapi.Router) {
				r.Get("/", operatorAuditDetailHandler(dependencies)).
					With(option.Description("Get a scoped operator audit event")).
					With(option.Request(api.OperatorAuditIDRequest{})).
					With(option.Response(http.StatusOK, api.OperatorAuditDetailResponse{})).
					With(operatorDetailErrorResponses()...)
				r.Get("/evidence", operatorAuditEvidenceHandler(dependencies)).
					With(option.Description("List scoped operator audit evidence")).
					With(option.Request(api.OperatorAuditIDRequest{})).
					With(option.Response(http.StatusOK, api.OperatorEvidencePage{})).
					With(operatorDetailErrorResponses()...)
			})
		})
	})
}

func operatorListErrorResponses() []option.OperationOption {
	return operatorPlainTextResponses(
		http.StatusBadRequest,
		http.StatusForbidden,
		http.StatusInternalServerError,
	)
}

func operatorDetailErrorResponses() []option.OperationOption {
	return operatorPlainTextResponses(
		http.StatusBadRequest,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusInternalServerError,
	)
}

func operatorPlainTextResponses(statuses ...int) []option.OperationOption {
	responses := make([]option.OperationOption, 0, len(statuses))
	for _, status := range statuses {
		responses = append(
			responses,
			option.Response(status, "", option.ContentType("text/plain")),
		)
	}
	return responses
}

func resolveOperatorReadScopes(
	ctx context.Context,
	principal operatorReadPrincipal,
	dependencies operatorReadScopeDependencies,
) (operatorqueries.AuditViewScopes, error) {
	var denied operatorqueries.AuditViewScopes
	if principal.OrganizationID == uuid.Nil || principal.PrincipalID == uuid.Nil {
		return denied, errOperatorReadForbidden
	}
	if dependencies.clock == nil {
		return denied, errOperatorReadForbidden
	}
	decisionAt := dependencies.clock().UTC()
	if principal.SuperAdmin {
		return operatorqueries.ResolveAuditViewScopes(
			principal.OrganizationID,
			[]types.AccessGrant{organizationAuditViewGrant(principal.OrganizationID, decisionAt)},
			decisionAt,
		), nil
	}
	if principal.CredentialRole == nil ||
		!slices.Contains(
			types.ActionsForLegacyRole(*principal.CredentialRole),
			types.ActionAuditView,
		) ||
		dependencies.listAccessGrants == nil ||
		dependencies.getLegacyRole == nil {
		return denied, errOperatorReadForbidden
	}

	grants, err := dependencies.listAccessGrants(
		ctx,
		principal.OrganizationID,
		principal.PrincipalID,
		decisionAt,
	)
	if err != nil {
		return denied, err
	}

	legacyRole, err := dependencies.getLegacyRole(
		ctx,
		principal.OrganizationID,
		principal.PrincipalID,
	)
	if err != nil {
		return denied, err
	}
	if legacyRole != nil &&
		slices.Contains(types.ActionsForLegacyRole(*legacyRole), types.ActionAuditView) {
		grants = append(grants, organizationAuditViewGrant(principal.OrganizationID, decisionAt))
	}
	scopes := operatorqueries.ResolveAuditViewScopes(
		principal.OrganizationID,
		grants,
		decisionAt,
	)
	if scopes.Empty() {
		return denied, errOperatorReadForbidden
	}
	return scopes, nil
}

func organizationAuditViewGrant(
	organizationID uuid.UUID,
	decisionAt time.Time,
) types.AccessGrant {
	return types.AccessGrant{
		BindingID: uuid.New(),
		Scope: types.ScopeRef{
			Kind: types.PermissionScopeOrganization,
			ID:   organizationID,
		},
		Actions:       []types.Action{types.ActionAuditView},
		EffectiveFrom: decisionAt,
	}
}

func operatorScopeFilter(scopes operatorqueries.AuditViewScopes) types.OperatorScopeFilter {
	return scopes.ToOperatorScopeFilter()
}

func operatorPageRequestFromHTTP(r *http.Request) (api.OperatorPageRequest, error) {
	request := api.OperatorPageRequest{Cursor: r.URL.Query().Get("cursor")}
	if values, present := r.URL.Query()["limit"]; present {
		if len(values) != 1 || values[0] == "" {
			return request, fmt.Errorf("limit is invalid")
		}
		limit, err := strconv.Atoi(values[0])
		if err != nil {
			return request, fmt.Errorf("limit is invalid: %w", err)
		}
		request.Limit = &limit
	}
	if err := request.Validate(); err != nil {
		return request, err
	}
	return request, nil
}

func operatorOptionalUUID(r *http.Request, name string) (*uuid.UUID, error) {
	values, present := r.URL.Query()[name]
	if !present {
		return nil, nil
	}
	if len(values) != 1 || values[0] == "" {
		return nil, fmt.Errorf("%s is invalid", name)
	}
	value, err := uuid.Parse(values[0])
	if err != nil || value == uuid.Nil {
		return nil, fmt.Errorf("%s is invalid", name)
	}
	return &value, nil
}

func operatorOptionalTime(r *http.Request, name string) (*time.Time, error) {
	values, present := r.URL.Query()[name]
	if !present {
		return nil, nil
	}
	if len(values) != 1 || values[0] == "" {
		return nil, fmt.Errorf("%s is invalid", name)
	}
	value, err := time.Parse(time.RFC3339Nano, values[0])
	if err != nil {
		return nil, fmt.Errorf("%s is invalid", name)
	}
	value = value.UTC()
	return &value, nil
}

func operatorFleetListRequestFromHTTP(r *http.Request) (api.OperatorFleetListRequest, error) {
	page, err := operatorPageRequestFromHTTP(r)
	if err != nil {
		return api.OperatorFleetListRequest{}, err
	}
	customerID, err := operatorOptionalUUID(r, "customerOrganizationId")
	if err != nil {
		return api.OperatorFleetListRequest{}, err
	}
	environmentID, err := operatorOptionalUUID(r, "environmentId")
	if err != nil {
		return api.OperatorFleetListRequest{}, err
	}
	targetID, err := operatorOptionalUUID(r, "deploymentTargetId")
	if err != nil {
		return api.OperatorFleetListRequest{}, err
	}
	unitID, err := operatorOptionalUUID(r, "deploymentUnitId")
	if err != nil {
		return api.OperatorFleetListRequest{}, err
	}
	query := r.URL.Query()
	return api.OperatorFleetListRequest{
		OperatorPageRequest:    page,
		CustomerOrganizationID: customerID,
		EnvironmentID:          environmentID,
		DeploymentTargetID:     targetID,
		DeploymentUnitID:       unitID,
		Component:              query.Get("component"),
		ObservedState:          query.Get("observedState"),
		Drift:                  query.Get("drift"),
		Enrollment:             query.Get("enrollment"),
		Search:                 query.Get("search"),
	}, nil
}

func operatorExecutionListRequestFromHTTP(
	r *http.Request,
) (api.OperatorExecutionListRequest, error) {
	page, err := operatorPageRequestFromHTTP(r)
	if err != nil {
		return api.OperatorExecutionListRequest{}, err
	}
	campaignID, err := operatorOptionalUUID(r, "campaignId")
	if err != nil {
		return api.OperatorExecutionListRequest{}, err
	}
	planID, err := operatorOptionalUUID(r, "deploymentPlanId")
	if err != nil {
		return api.OperatorExecutionListRequest{}, err
	}
	targetID, err := operatorOptionalUUID(r, "deploymentTargetId")
	if err != nil {
		return api.OperatorExecutionListRequest{}, err
	}
	from, err := operatorOptionalTime(r, "from")
	if err != nil {
		return api.OperatorExecutionListRequest{}, err
	}
	to, err := operatorOptionalTime(r, "to")
	if err != nil {
		return api.OperatorExecutionListRequest{}, err
	}
	return api.OperatorExecutionListRequest{
		OperatorPageRequest: page,
		Status:              r.URL.Query().Get("status"),
		CampaignID:          campaignID,
		DeploymentPlanID:    planID,
		DeploymentTargetID:  targetID,
		From:                from,
		To:                  to,
	}, nil
}

func operatorReleaseListRequestFromHTTP(
	r *http.Request,
) (api.OperatorReleaseListRequest, error) {
	page, err := operatorPageRequestFromHTTP(r)
	if err != nil {
		return api.OperatorReleaseListRequest{}, err
	}
	applicationID, err := operatorOptionalUUID(r, "applicationId")
	if err != nil {
		return api.OperatorReleaseListRequest{}, err
	}
	query := r.URL.Query()
	return api.OperatorReleaseListRequest{
		OperatorPageRequest: page,
		ApplicationID:       applicationID,
		Kind:                query.Get("kind"),
		Status:              query.Get("status"),
		Search:              query.Get("search"),
	}, nil
}

func operatorPlanListRequestFromHTTP(
	r *http.Request,
) (api.OperatorPlanListRequest, error) {
	page, err := operatorPageRequestFromHTTP(r)
	if err != nil {
		return api.OperatorPlanListRequest{}, err
	}
	environmentID, err := operatorOptionalUUID(r, "environmentId")
	if err != nil {
		return api.OperatorPlanListRequest{}, err
	}
	unitID, err := operatorOptionalUUID(r, "deploymentUnitId")
	if err != nil {
		return api.OperatorPlanListRequest{}, err
	}
	productReleaseID, err := operatorOptionalUUID(r, "productReleaseId")
	if err != nil {
		return api.OperatorPlanListRequest{}, err
	}
	return api.OperatorPlanListRequest{
		OperatorPageRequest: page,
		Status:              r.URL.Query().Get("status"),
		EnvironmentID:       environmentID,
		DeploymentUnitID:    unitID,
		ProductReleaseID:    productReleaseID,
	}, nil
}

func operatorCampaignListRequestFromHTTP(
	r *http.Request,
) (api.OperatorCampaignListRequest, error) {
	page, err := operatorPageRequestFromHTTP(r)
	if err != nil {
		return api.OperatorCampaignListRequest{}, err
	}
	environmentID, err := operatorOptionalUUID(r, "environmentId")
	if err != nil {
		return api.OperatorCampaignListRequest{}, err
	}
	planID, err := operatorOptionalUUID(r, "deploymentPlanId")
	if err != nil {
		return api.OperatorCampaignListRequest{}, err
	}
	return api.OperatorCampaignListRequest{
		OperatorPageRequest: page,
		Status:              r.URL.Query().Get("status"),
		EnvironmentID:       environmentID,
		DeploymentPlanID:    planID,
	}, nil
}

func operatorReconciliationListRequestFromHTTP(
	r *http.Request,
) (api.OperatorReconciliationListRequest, error) {
	page, err := operatorPageRequestFromHTTP(r)
	if err != nil {
		return api.OperatorReconciliationListRequest{}, err
	}
	environmentID, err := operatorOptionalUUID(r, "environmentId")
	if err != nil {
		return api.OperatorReconciliationListRequest{}, err
	}
	targetID, err := operatorOptionalUUID(r, "deploymentTargetId")
	if err != nil {
		return api.OperatorReconciliationListRequest{}, err
	}
	query := r.URL.Query()
	return api.OperatorReconciliationListRequest{
		OperatorPageRequest: page,
		Status:              query.Get("status"),
		Drift:               query.Get("drift"),
		EnvironmentID:       environmentID,
		DeploymentTargetID:  targetID,
	}, nil
}

func operatorAuditListRequestFromHTTP(
	r *http.Request,
) (api.OperatorAuditListRequest, error) {
	page, err := operatorPageRequestFromHTTP(r)
	if err != nil {
		return api.OperatorAuditListRequest{}, err
	}
	subjectID, err := operatorOptionalUUID(r, "subjectId")
	if err != nil {
		return api.OperatorAuditListRequest{}, err
	}
	actorID, err := operatorOptionalUUID(r, "actorUserAccountId")
	if err != nil {
		return api.OperatorAuditListRequest{}, err
	}
	from, err := operatorOptionalTime(r, "from")
	if err != nil {
		return api.OperatorAuditListRequest{}, err
	}
	to, err := operatorOptionalTime(r, "to")
	if err != nil {
		return api.OperatorAuditListRequest{}, err
	}
	query := r.URL.Query()
	return api.OperatorAuditListRequest{
		OperatorPageRequest: page,
		Action:              query.Get("action"),
		SubjectType:         query.Get("subjectType"),
		SubjectID:           subjectID,
		ActorUserAccountID:  actorID,
		From:                from,
		To:                  to,
		Search:              query.Get("search"),
	}, nil
}

func operatorFleetHandler(dependencies operatorControlPlaneDependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := operatorFleetListRequestFromHTTP(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		scopes, err := operatorRequestScopes(r, dependencies)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		if dependencies.listFleet == nil {
			writeOperatorReadError(w, r, errors.New("operator fleet query is unavailable"))
			return
		}
		page, err := dependencies.listFleet(
			r.Context(),
			request.ToFilter(operatorScopeFilter(scopes)),
			request.ToPageRequest(),
		)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		RespondJSON(w, mapping.OperatorFleetPageToAPI(page))
	}
}

func operatorReleaseListHandler(dependencies operatorControlPlaneDependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := operatorReleaseListRequestFromHTTP(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		scopes, err := operatorRequestScopes(r, dependencies)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		if dependencies.listReleases == nil {
			writeOperatorReadError(w, r, errors.New("operator release query is unavailable"))
			return
		}
		page, err := dependencies.listReleases(
			r.Context(),
			request.ToFilter(operatorScopeFilter(scopes)),
			request.ToPageRequest(),
		)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		RespondJSON(w, mapping.OperatorReleasePageToAPI(page))
	}
}

func operatorPlanListHandler(dependencies operatorControlPlaneDependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := operatorPlanListRequestFromHTTP(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		scopes, err := operatorRequestScopes(r, dependencies)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		if dependencies.listPlans == nil {
			writeOperatorReadError(w, r, errors.New("operator plan query is unavailable"))
			return
		}
		page, err := dependencies.listPlans(
			r.Context(),
			request.ToFilter(operatorScopeFilter(scopes)),
			request.ToPageRequest(),
		)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		RespondJSON(w, mapping.OperatorPlanPageToAPI(page))
	}
}

func operatorCampaignListHandler(dependencies operatorControlPlaneDependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := operatorCampaignListRequestFromHTTP(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		scopes, err := operatorRequestScopes(r, dependencies)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		if dependencies.listCampaigns == nil {
			writeOperatorReadError(w, r, errors.New("operator campaign query is unavailable"))
			return
		}
		page, err := dependencies.listCampaigns(
			r.Context(),
			request.ToFilter(operatorScopeFilter(scopes)),
			request.ToPageRequest(),
		)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		RespondJSON(w, mapping.OperatorCampaignPageToAPI(page))
	}
}

func operatorExecutionListHandler(dependencies operatorControlPlaneDependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := operatorExecutionListRequestFromHTTP(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		scopes, err := operatorRequestScopes(r, dependencies)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		if dependencies.listExecutions == nil {
			writeOperatorReadError(w, r, errors.New("operator execution query is unavailable"))
			return
		}
		page, err := dependencies.listExecutions(
			r.Context(),
			request.ToFilter(operatorScopeFilter(scopes)),
			scopes,
			request.ToPageRequest(),
		)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		RespondJSON(w, mapping.OperatorExecutionPageToAPI(page))
	}
}

func operatorReconciliationListHandler(
	dependencies operatorControlPlaneDependencies,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := operatorReconciliationListRequestFromHTTP(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		scopes, err := operatorRequestScopes(r, dependencies)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		if dependencies.listReconciliation == nil {
			writeOperatorReadError(w, r, errors.New("operator reconciliation query is unavailable"))
			return
		}
		page, err := dependencies.listReconciliation(
			r.Context(),
			request.ToFilter(operatorScopeFilter(scopes)),
			request.ToPageRequest(),
		)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		RespondJSON(w, mapping.OperatorReconciliationPageToAPI(page))
	}
}

func operatorAuditListHandler(dependencies operatorControlPlaneDependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := operatorAuditListRequestFromHTTP(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		scopes, err := operatorRequestScopes(r, dependencies)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		if dependencies.searchAudit == nil {
			writeOperatorReadError(w, r, errors.New("operator audit query is unavailable"))
			return
		}
		page, err := dependencies.searchAudit(
			r.Context(),
			request.ToFilter(operatorScopeFilter(scopes)),
			request.ToPageRequest(),
		)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		RespondJSON(w, mapping.OperatorAuditPageToAPI(page))
	}
}

func operatorReleaseDetailHandler(dependencies operatorControlPlaneDependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		releaseID, ok := operatorPathUUID(w, r, "releaseId")
		if !ok {
			return
		}
		scopes, err := operatorRequestScopes(r, dependencies)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		if dependencies.getRelease == nil {
			writeOperatorReadError(w, r, errors.New("operator release query is unavailable"))
			return
		}
		detail, err := dependencies.getRelease(
			r.Context(), operatorScopeFilter(scopes), releaseID,
		)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		if detail == nil {
			writeOperatorReadError(w, r, apierrors.ErrNotFound)
			return
		}
		RespondJSON(w, mapping.OperatorReleaseDetailToAPI(*detail))
	}
}

func operatorReleaseCompareHandler(dependencies operatorControlPlaneDependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		releaseID, ok := operatorPathUUID(w, r, "releaseId")
		if !ok {
			return
		}
		otherReleaseID, ok := operatorPathUUID(w, r, "otherReleaseId")
		if !ok {
			return
		}
		scopes, err := operatorRequestScopes(r, dependencies)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		if dependencies.compareReleases == nil {
			writeOperatorReadError(w, r, errors.New("operator release comparison is unavailable"))
			return
		}
		comparison, err := dependencies.compareReleases(
			r.Context(), operatorScopeFilter(scopes), releaseID, otherReleaseID,
		)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		if comparison == nil {
			writeOperatorReadError(w, r, apierrors.ErrNotFound)
			return
		}
		RespondJSON(w, mapping.OperatorReleaseCompareToAPI(*comparison))
	}
}

func operatorPlanDetailHandler(dependencies operatorControlPlaneDependencies) http.HandlerFunc {
	return operatorPlanDetailOrEvidenceHandler(dependencies, false)
}

func operatorPlanEvidenceHandler(dependencies operatorControlPlaneDependencies) http.HandlerFunc {
	return operatorPlanDetailOrEvidenceHandler(dependencies, true)
}

func operatorPlanCompareHandler(dependencies operatorControlPlaneDependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		planID, ok := operatorPathUUID(w, r, "planId")
		if !ok {
			return
		}
		otherPlanID, ok := operatorPathUUID(w, r, "otherPlanId")
		if !ok {
			return
		}
		scopes, err := operatorRequestScopes(r, dependencies)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		if dependencies.comparePlans == nil {
			writeOperatorReadError(w, r, errors.New("operator plan comparison is unavailable"))
			return
		}
		comparison, err := dependencies.comparePlans(
			r.Context(), operatorScopeFilter(scopes), planID, otherPlanID,
		)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		if comparison == nil {
			writeOperatorReadError(w, r, apierrors.ErrNotFound)
			return
		}
		RespondJSON(w, mapping.OperatorPlanCompareToAPI(*comparison))
	}
}

func compareOperatorPlanDetails(
	left types.OperatorPlanDetail,
	right types.OperatorPlanDetail,
) types.OperatorPlanCompare {
	checksums := []struct {
		key   string
		left  string
		right string
	}{
		{"canonical", left.Plan.CanonicalChecksum, right.Plan.CanonicalChecksum},
		{"productRelease", left.ProductReleaseChecksum, right.ProductReleaseChecksum},
		{"targetConfig", left.TargetConfigChecksum, right.TargetConfigChecksum},
		{"effectivePolicy", left.EffectivePolicyChecksum, right.EffectivePolicyChecksum},
		{"subscriberSet", left.SubscriberSetChecksum, right.SubscriberSetChecksum},
		{"graph", left.GraphChecksum, right.GraphChecksum},
		{"change", left.ChangeChecksum, right.ChangeChecksum},
		{"baseline", left.BaselineChecksum, right.BaselineChecksum},
		{"providerResolution", left.ProviderResolutionChecksum, right.ProviderResolutionChecksum},
		{"migration", left.MigrationChecksum, right.MigrationChecksum},
		{"risk", left.RiskChecksum, right.RiskChecksum},
		{"approval", left.ApprovalChecksum, right.ApprovalChecksum},
		{"window", left.WindowChecksum, right.WindowChecksum},
		{"adapter", left.AdapterChecksum, right.AdapterChecksum},
		{"intent", left.IntentChecksum, right.IntentChecksum},
	}
	changes := make([]types.OperatorPlanFact, 0, len(checksums))
	for _, checksum := range checksums {
		if checksum.left == checksum.right {
			continue
		}
		changes = append(changes, types.OperatorPlanFact{
			Key:      checksum.key,
			Kind:     "checksum",
			Status:   "modified",
			Expected: checksum.left,
			Actual:   checksum.right,
			Order:    len(changes) + 1,
		})
	}
	return types.OperatorPlanCompare{
		Left: left.Plan, Right: right.Plan, Changes: changes,
	}
}

func operatorPlanDetailOrEvidenceHandler(
	dependencies operatorControlPlaneDependencies,
	evidenceOnly bool,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		planID, ok := operatorPathUUID(w, r, "planId")
		if !ok {
			return
		}
		scopes, err := operatorRequestScopes(r, dependencies)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		if dependencies.getPlan == nil {
			writeOperatorReadError(w, r, errors.New("operator plan query is unavailable"))
			return
		}
		detail, err := dependencies.getPlan(r.Context(), operatorScopeFilter(scopes), planID)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		if detail == nil {
			writeOperatorReadError(w, r, apierrors.ErrNotFound)
			return
		}
		if evidenceOnly {
			RespondJSON(w, mapping.OperatorEvidenceToAPI(detail.Evidence))
			return
		}
		RespondJSON(w, mapping.OperatorPlanDetailToAPI(*detail))
	}
}

func operatorCampaignDetailHandler(
	dependencies operatorControlPlaneDependencies,
) http.HandlerFunc {
	return operatorCampaignDetailOrEvidenceHandler(dependencies, false)
}

func operatorCampaignEvidenceHandler(
	dependencies operatorControlPlaneDependencies,
) http.HandlerFunc {
	return operatorCampaignDetailOrEvidenceHandler(dependencies, true)
}

func operatorCampaignDetailOrEvidenceHandler(
	dependencies operatorControlPlaneDependencies,
	evidenceOnly bool,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		campaignID, ok := operatorPathUUID(w, r, "campaignId")
		if !ok {
			return
		}
		scopes, err := operatorRequestScopes(r, dependencies)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		if dependencies.getCampaign == nil {
			writeOperatorReadError(w, r, errors.New("operator campaign query is unavailable"))
			return
		}
		detail, err := dependencies.getCampaign(
			r.Context(),
			campaignID,
			types.CampaignFilter{OperatorScopeFilter: operatorScopeFilter(scopes)},
		)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		if detail == nil {
			writeOperatorReadError(w, r, apierrors.ErrNotFound)
			return
		}
		if evidenceOnly {
			RespondJSON(w, mapping.OperatorEvidenceToAPI(detail.Evidence))
			return
		}
		RespondJSON(w, mapping.OperatorCampaignDetailToAPI(*detail))
	}
}

func operatorExecutionDetailHandler(
	dependencies operatorControlPlaneDependencies,
) http.HandlerFunc {
	return operatorExecutionDetailOrEvidenceHandler(dependencies, false)
}

func operatorExecutionEvidenceHandler(
	dependencies operatorControlPlaneDependencies,
) http.HandlerFunc {
	return operatorExecutionDetailOrEvidenceHandler(dependencies, true)
}

func operatorExecutionDetailOrEvidenceHandler(
	dependencies operatorControlPlaneDependencies,
	evidenceOnly bool,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		executionID, ok := operatorPathUUID(w, r, "executionId")
		if !ok {
			return
		}
		scopes, err := operatorRequestScopes(r, dependencies)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		if dependencies.getExecution == nil {
			writeOperatorReadError(w, r, errors.New("operator execution query is unavailable"))
			return
		}
		detail, err := dependencies.getExecution(
			r.Context(), scopes.OrganizationID, scopes, executionID,
		)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		if detail == nil {
			writeOperatorReadError(w, r, apierrors.ErrNotFound)
			return
		}
		if evidenceOnly {
			RespondJSON(w, mapping.OperatorEvidenceToAPI(detail.Evidence))
			return
		}
		RespondJSON(w, mapping.OperatorExecutionDetailToAPI(*detail))
	}
}

func operatorReconciliationDetailHandler(
	dependencies operatorControlPlaneDependencies,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reconciliationID, ok := operatorPathUUID(w, r, "reconciliationId")
		if !ok {
			return
		}
		scopes, err := operatorRequestScopes(r, dependencies)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		if dependencies.getReconciliation == nil {
			writeOperatorReadError(w, r, errors.New("operator reconciliation query is unavailable"))
			return
		}
		detail, err := dependencies.getReconciliation(
			r.Context(), operatorScopeFilter(scopes), reconciliationID,
		)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		if detail == nil {
			writeOperatorReadError(w, r, apierrors.ErrNotFound)
			return
		}
		RespondJSON(w, mapping.OperatorReconciliationDetailToAPI(*detail))
	}
}

func operatorReconciliationEvidenceHandler(
	dependencies operatorControlPlaneDependencies,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reconciliationID, ok := operatorPathUUID(w, r, "reconciliationId")
		if !ok {
			return
		}
		scopes, err := operatorRequestScopes(r, dependencies)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		if dependencies.listReconciliationEvidence == nil {
			writeOperatorReadError(w, r, errors.New("operator reconciliation evidence query is unavailable"))
			return
		}
		items, err := dependencies.listReconciliationEvidence(
			r.Context(), operatorScopeFilter(scopes), reconciliationID,
		)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		RespondJSON(w, mapping.OperatorEvidenceToAPI(items))
	}
}

func operatorAuditDetailHandler(dependencies operatorControlPlaneDependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auditEventID, ok := operatorPathUUID(w, r, "auditEventId")
		if !ok {
			return
		}
		scopes, err := operatorRequestScopes(r, dependencies)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		if dependencies.getAudit == nil {
			writeOperatorReadError(w, r, errors.New("operator audit query is unavailable"))
			return
		}
		detail, err := dependencies.getAudit(
			r.Context(), operatorScopeFilter(scopes), auditEventID,
		)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		if detail == nil {
			writeOperatorReadError(w, r, apierrors.ErrNotFound)
			return
		}
		RespondJSON(w, mapping.OperatorAuditDetailToAPI(*detail))
	}
}

func operatorAuditEvidenceHandler(dependencies operatorControlPlaneDependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auditEventID, ok := operatorPathUUID(w, r, "auditEventId")
		if !ok {
			return
		}
		scopes, err := operatorRequestScopes(r, dependencies)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		if dependencies.listAuditEvidence == nil {
			writeOperatorReadError(w, r, errors.New("operator audit evidence query is unavailable"))
			return
		}
		items, err := dependencies.listAuditEvidence(
			r.Context(), operatorScopeFilter(scopes), auditEventID,
		)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		RespondJSON(w, mapping.OperatorEvidenceToAPI(items))
	}
}

func operatorReleaseEvidenceHandler(
	dependencies operatorControlPlaneDependencies,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		releaseID, ok := operatorPathUUID(w, r, "releaseId")
		if !ok {
			return
		}
		scopes, err := operatorRequestScopes(r, dependencies)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		if dependencies.getRelease == nil {
			writeOperatorReadError(w, r, errors.New("operator release query is unavailable"))
			return
		}
		detail, err := dependencies.getRelease(
			r.Context(), operatorScopeFilter(scopes), releaseID,
		)
		if err != nil {
			writeOperatorReadError(w, r, err)
			return
		}
		if detail == nil {
			writeOperatorReadError(w, r, apierrors.ErrNotFound)
			return
		}
		RespondJSON(w, mapping.OperatorEvidenceToAPI(detail.Evidence))
	}
}

func operatorPathUUID(
	w http.ResponseWriter,
	r *http.Request,
	name string,
) (uuid.UUID, bool) {
	value, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil || value == uuid.Nil {
		http.Error(w, name+" is invalid", http.StatusBadRequest)
		return uuid.Nil, false
	}
	return value, true
}

func operatorRequestScopes(
	r *http.Request,
	dependencies operatorControlPlaneDependencies,
) (operatorqueries.AuditViewScopes, error) {
	if dependencies.resolveScopes == nil {
		return operatorqueries.AuditViewScopes{}, errOperatorReadForbidden
	}
	authInfo := auth.Authentication.Require(r.Context())
	if authInfo.CurrentOrgID() == nil {
		return operatorqueries.AuditViewScopes{}, errOperatorReadForbidden
	}
	return dependencies.resolveScopes(r.Context(), operatorReadPrincipal{
		OrganizationID: *authInfo.CurrentOrgID(),
		PrincipalID:    authInfo.CurrentUserID(),
		CredentialRole: authInfo.CurrentUserRole(),
		SuperAdmin:     authInfo.IsSuperAdmin(),
	})
}

func writeOperatorReadError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, apierrors.ErrBadRequest):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, apierrors.ErrNotFound):
		http.NotFound(w, r)
	case errors.Is(err, apierrors.ErrForbidden), errors.Is(err, errOperatorReadForbidden):
		http.Error(w, "insufficient permissions", http.StatusForbidden)
	default:
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}
