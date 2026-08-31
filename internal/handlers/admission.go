package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/env"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/mapping"
	"github.com/distr-sh/distr/internal/middleware"
	"github.com/distr-sh/distr/internal/types"
	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/oaswrap/spec/adapter/chiopenapi"
	"github.com/oaswrap/spec/option"
	"go.uber.org/zap"
)

type admissionHandlerDependencies struct {
	admit func(
		context.Context,
		types.AdmitDeploymentPlanRequest,
	) (*types.AdmissionEvaluation, error)
	createOverride func(
		context.Context,
		types.CreateEmergencyOverrideRequest,
	) (*types.EmergencyOverride, error)
	createReviewDecision func(
		context.Context,
		types.CreateReviewAdmissionDecisionRequest,
	) (*types.ReviewAdmissionDecisionRecord, error)
	listReviewDecisions func(
		context.Context,
		uuid.UUID,
		uuid.UUID,
	) ([]types.ReviewAdmissionDecisionRecord, error)
	getReviewMaterial func(
		context.Context,
		uuid.UUID,
		uuid.UUID,
	) (*types.ReviewAdmissionMaterial, error)
	authorize types.AdmissionAuthorizer
	clock     func() time.Time
}

func defaultAdmissionHandlerDependencies() admissionHandlerDependencies {
	return admissionHandlerDependencies{
		admit:                db.AdmitDeploymentPlan,
		createOverride:       db.CreateEmergencyOverride,
		createReviewDecision: db.CreateReviewAdmissionDecision,
		listReviewDecisions:  db.ListReviewAdmissionDecisions,
		getReviewMaterial:    db.GetReviewAdmissionMaterial,
		authorize:            admissionScopedAuthorization,
		clock: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func admissionScopedAuthorization(
	ctx context.Context,
	evidence types.AdmissionAuthorizationContext,
) error {
	return admissionScopedAuthorizationWithDependencies(
		ctx,
		evidence,
		defaultControlPlaneResourceAuthorizationDependencies(),
	)
}

func admissionScopedAuthorizationWithDependencies(
	ctx context.Context,
	evidence types.AdmissionAuthorizationContext,
	dependencies controlPlaneResourceAuthorizationDependencies,
) error {
	authInfo, err := auth.Authentication.Get(ctx)
	if err != nil ||
		authInfo.CurrentOrgID() == nil ||
		*authInfo.CurrentOrgID() != evidence.OrganizationID ||
		authInfo.CurrentUserID() != evidence.ActorUserAccountID {
		return apierrors.ErrForbidden
	}
	resource := types.ResourceRef{
		OrganizationID: evidence.OrganizationID,
		Kind:           types.PermissionScopeEnvironment,
		ID:             evidence.EnvironmentID,
	}
	if evidence.DeploymentUnitID != nil {
		resource.Kind = types.PermissionScopeDeploymentUnit
		resource.ID = *evidence.DeploymentUnitID
	}
	return authorizeControlPlaneResourceWithDependencies(ctx, controlPlaneResourceAuthorizationRequest{
		OrganizationID:    evidence.OrganizationID,
		PrincipalID:       evidence.ActorUserAccountID,
		CredentialRole:    authInfo.CurrentUserRole(),
		IsSuperAdmin:      authInfo.IsSuperAdmin(),
		Action:            types.Action(evidence.Action),
		Resource:          resource,
		DecisionAt:        evidence.DecisionAt,
		RequireEnrollment: true,
		EnvironmentID:     evidence.EnvironmentID,
	}, dependencies)
}

func deploymentPlanAdmissionRoutes(r chiopenapi.Router) {
	dependencies := defaultAdmissionHandlerDependencies()
	type deploymentPlanAdmissionRoute struct {
		DeploymentPlanID uuid.UUID `path:"deploymentPlanId"`
	}
	r.WithOptions(option.GroupTags("Deployment Admission"))
	r.With(
		admissionMutationAccessMiddlewareWithFlags(env.ExperimentalFeatureFlags()),
		middleware.RequireReadWriteOrAdmin,
		middleware.BlockSuperAdmin,
	).Post(
		"/admission",
		admitDeploymentPlanHandlerWithDependencies(dependencies),
	).With(option.Description(
		"Evaluate and append checksum-bound deployment admission evidence",
	)).With(option.Request(struct {
		deploymentPlanAdmissionRoute
		api.AdmitDeploymentPlanRequest
	}{})).With(option.Response(http.StatusOK, api.AdmissionEvaluation{}))

	r.With(
		admissionMutationAccessMiddlewareWithFlags(env.ExperimentalFeatureFlags()),
		middleware.RequireReadWriteOrAdmin,
		middleware.BlockSuperAdmin,
	).Post(
		"/emergency-overrides",
		createEmergencyOverrideHandlerWithDependencies(dependencies),
	).With(option.Description(
		"Create a scoped checksum-bound emergency acceleration override",
	)).With(option.Request(struct {
		deploymentPlanAdmissionRoute
		api.CreateEmergencyOverrideRequest
	}{})).With(option.Response(http.StatusOK, api.EmergencyOverride{}))

	r.With(
		admissionMutationAccessMiddlewareWithFlags(env.ExperimentalFeatureFlags()),
		middleware.RequireReadWriteOrAdmin,
		middleware.BlockSuperAdmin,
	).Post(
		"/review-decisions",
		createReviewAdmissionDecisionHandlerWithDependencies(dependencies),
	).With(option.Description(
		"Append a checksum-bound GO or NO_GO review admission decision",
	)).With(option.Request(struct {
		deploymentPlanAdmissionRoute
		api.CreateReviewAdmissionDecisionRequest
	}{})).With(option.Response(http.StatusOK, types.ReviewAdmissionDecisionRecord{}))

	r.With(
		admissionMutationAccessMiddlewareWithFlags(env.ExperimentalFeatureFlags()),
	).Get(
		"/review-decisions",
		listReviewAdmissionDecisionsHandlerWithDependencies(dependencies),
	).With(option.Description(
		"List append-only GO and NO_GO review admission evidence",
	)).With(option.Request(deploymentPlanAdmissionRoute{})).With(option.Response(
		http.StatusOK, []types.ReviewAdmissionDecisionRecord{},
	))

	r.With(
		admissionMutationAccessMiddlewareWithFlags(env.ExperimentalFeatureFlags()),
	).Get(
		"/review-material",
		getReviewAdmissionMaterialHandlerWithDependencies(dependencies),
	).With(option.Description(
		"Get current observed-state-bound review material and admission readiness",
	)).With(option.Request(deploymentPlanAdmissionRoute{})).With(option.Response(
		http.StatusOK, types.ReviewAdmissionMaterial{},
	))
}

func createReviewAdmissionDecisionHandlerWithDependencies(
	dependencies admissionHandlerDependencies,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		planID, ok := admissionPathPlanID(w, r)
		if !ok {
			return
		}
		request, err := approvalJSONBody[api.CreateReviewAdmissionDecisionRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(dependencies.clock()); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		decision, err := dependencies.createReviewDecision(r.Context(), types.CreateReviewAdmissionDecisionRequest{
			OrganizationID: *authInfo.CurrentOrgID(), DeploymentPlanID: planID,
			ActorUserAccountID: authInfo.CurrentUserID(), ExpectedPlanChecksum: request.ExpectedPlanChecksum,
			ReviewMaterialChecksum: request.ReviewMaterialChecksum,
			ObservedStateChecksum:  request.ObservedStateChecksum, Decision: request.Decision,
			Reason: request.Reason, ExpiresAt: request.ExpiresAt,
			SupersedesDecisionID: request.SupersedesDecisionID, RevokesDecisionID: request.RevokesDecisionID,
			IdempotencyKey: request.IdempotencyKey, Authorize: dependencies.authorize,
		})
		if err != nil {
			handleAdmissionError(w, r, "create review admission decision", err)
			return
		}
		RespondJSON(w, decision)
	}
}

func listReviewAdmissionDecisionsHandlerWithDependencies(
	dependencies admissionHandlerDependencies,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		planID, ok := admissionPathPlanID(w, r)
		if !ok {
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		decisions, err := dependencies.listReviewDecisions(r.Context(), *authInfo.CurrentOrgID(), planID)
		if err != nil {
			handleAdmissionError(w, r, "list review admission decisions", err)
			return
		}
		RespondJSON(w, decisions)
	}
}

func getReviewAdmissionMaterialHandlerWithDependencies(
	dependencies admissionHandlerDependencies,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		planID, ok := admissionPathPlanID(w, r)
		if !ok {
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		material, err := dependencies.getReviewMaterial(
			r.Context(),
			*authInfo.CurrentOrgID(),
			planID,
		)
		if err != nil {
			handleAdmissionError(w, r, "get review admission material", err)
			return
		}
		RespondJSON(w, material)
	}
}

func admissionMutationAccessMiddlewareWithFlags(
	enabledFlags []featureflags.Key,
) func(http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			registry := featureflags.NewRegistry(enabledFlags)
			if !registry.IsEnabled(featureflags.KeyOperatorControlPlaneV2) ||
				!registry.IsEnabled(featureflags.KeyExecutorProtocolV2) {
				http.NotFound(w, r)
				return
			}
			handler.ServeHTTP(w, r)
		})
	}
}

func admitDeploymentPlanHandlerWithDependencies(
	dependencies admissionHandlerDependencies,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		planID, ok := admissionPathPlanID(w, r)
		if !ok {
			return
		}
		request, err := approvalJSONBody[api.AdmitDeploymentPlanRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		evaluation, err := dependencies.admit(
			r.Context(),
			types.AdmitDeploymentPlanRequest{
				OrganizationID:          *authInfo.CurrentOrgID(),
				DeploymentPlanID:        planID,
				ActorUserAccountID:      authInfo.CurrentUserID(),
				SchedulerIdempotencyKey: request.SchedulerIdempotencyKey,
				Campaign:                request.Campaign,
				Authorize:               dependencies.authorize,
			},
		)
		if err != nil {
			handleAdmissionError(w, r, "admit deployment plan", err)
			return
		}
		RespondJSON(w, mapping.AdmissionEvaluationToAPI(*evaluation))
	}
}

func createEmergencyOverrideHandlerWithDependencies(
	dependencies admissionHandlerDependencies,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		planID, ok := admissionPathPlanID(w, r)
		if !ok {
			return
		}
		request, err := approvalJSONBody[api.CreateEmergencyOverrideRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(dependencies.clock()); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		override, err := dependencies.createOverride(
			r.Context(),
			types.CreateEmergencyOverrideRequest{
				OrganizationID:     *authInfo.CurrentOrgID(),
				DeploymentPlanID:   planID,
				ActorUserAccountID: authInfo.CurrentUserID(),
				Accelerations:      request.Accelerations,
				Reason:             request.Reason,
				ApprovalRequestIDs: request.ApprovalRequestIDs,
				ExpiresAt:          request.ExpiresAt,
				IdempotencyKey:     request.IdempotencyKey,
				Authorize:          dependencies.authorize,
			},
		)
		if err != nil {
			handleAdmissionError(w, r, "create emergency override", err)
			return
		}
		RespondJSON(w, mapping.EmergencyOverrideToAPI(*override))
	}
}

func admissionPathPlanID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("deploymentPlanId"))
	if err != nil {
		http.NotFound(w, r)
		return uuid.Nil, false
	}
	return id, true
}

func handleAdmissionError(
	w http.ResponseWriter,
	r *http.Request,
	action string,
	err error,
) {
	switch {
	case errors.Is(err, apierrors.ErrNotFound):
		http.NotFound(w, r)
	case errors.Is(err, apierrors.ErrBadRequest):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, apierrors.ErrForbidden):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, apierrors.ErrConflict):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		log := internalctx.GetLogger(r.Context())
		log.Error("failed to "+action, zap.Error(err))
		sentry.GetHubFromContext(r.Context()).CaptureException(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
