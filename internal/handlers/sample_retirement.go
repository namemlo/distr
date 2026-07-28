package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/authn/authinfo"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/mapping"
	"github.com/distr-sh/distr/internal/middleware"
	"github.com/distr-sh/distr/internal/retirement"
	"github.com/distr-sh/distr/internal/types"
	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/oaswrap/spec/adapter/chiopenapi"
	"github.com/oaswrap/spec/option"
	"go.uber.org/zap"
)

type SampleRetirementService interface {
	PreviewSampleRetirement(
		context.Context,
		types.SampleRetirementRequest,
	) (*types.SampleRetirementPreview, error)
	GetSampleRetirement(
		context.Context,
		uuid.UUID,
		uuid.UUID,
	) (*types.SampleRetirementDetail, error)
	RequestSampleRetirementApproval(
		context.Context,
		types.SampleRetirementApprovalRequestInput,
	) (*types.ApprovalRequest, error)
	ApplySampleRetirement(
		context.Context,
		types.SampleRetirementApplyRequest,
	) (*types.SampleRetirementResult, error)
	VerifySampleRetirement(
		context.Context,
		uuid.UUID,
		uuid.UUID,
	) (*types.SampleRetirementVerification, error)
}

type SampleRetirementEvidenceService interface {
	RegisterSampleRetirementOwnershipEvidence(
		context.Context,
		types.SampleRetirementOwnershipEvidenceRegistrationInput,
	) (*types.SampleRetirementOwnershipEvidence, error)
	RegisterSampleRetirementRecoveryEvidence(
		context.Context,
		types.SampleRetirementRecoveryEvidenceRegistrationInput,
	) (*types.SampleRetirementRecoveryEvidence, error)
}

type unavailableSampleRetirementService struct{}

func (unavailableSampleRetirementService) PreviewSampleRetirement(
	context.Context,
	types.SampleRetirementRequest,
) (*types.SampleRetirementPreview, error) {
	return nil, errors.New("sample retirement service is unavailable")
}

func (unavailableSampleRetirementService) GetSampleRetirement(
	context.Context,
	uuid.UUID,
	uuid.UUID,
) (*types.SampleRetirementDetail, error) {
	return nil, errors.New("sample retirement service is unavailable")
}

func (unavailableSampleRetirementService) RequestSampleRetirementApproval(
	context.Context,
	types.SampleRetirementApprovalRequestInput,
) (*types.ApprovalRequest, error) {
	return nil, errors.New("sample retirement service is unavailable")
}

func (unavailableSampleRetirementService) ApplySampleRetirement(
	context.Context,
	types.SampleRetirementApplyRequest,
) (*types.SampleRetirementResult, error) {
	return nil, errors.New("sample retirement service is unavailable")
}

func (unavailableSampleRetirementService) VerifySampleRetirement(
	context.Context,
	uuid.UUID,
	uuid.UUID,
) (*types.SampleRetirementVerification, error) {
	return nil, errors.New("sample retirement service is unavailable")
}

func SampleRetirementRouter(r chiopenapi.Router) {
	SampleRetirementRouterWithService(newDatabaseSampleRetirementService())(r)
}

func SampleRetirementRouterWithService(
	service SampleRetirementService,
) func(chiopenapi.Router) {
	if service == nil {
		service = unavailableSampleRetirementService{}
	}
	return func(r chiopenapi.Router) {
		sampleRetirementRouterWithService(r, service)
	}
}

func SampleRetirementEvidenceRouter(r chiopenapi.Router) {
	SampleRetirementEvidenceRouterWithService(databaseSampleRetirementEvidenceService{})(r)
}

func SampleRetirementEvidenceRouterWithService(
	service SampleRetirementEvidenceService,
) func(chiopenapi.Router) {
	return func(r chiopenapi.Router) {
		r.WithOptions(option.GroupTags("Sample Retirement Evidence"))
		r.With(
			middleware.RequireVendor,
			middleware.RequireOrgAndRole,
			middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyOperatorControlPlaneV2),
			requireControlPlaneOrganizationAction(types.ActionSampleRetire),
			middleware.BlockSuperAdmin,
		).Group(func(r chiopenapi.Router) {
			r.Post(
				"/ownership",
				registerSampleRetirementOwnershipEvidenceHandler(service),
			).With(option.Description("Register append-only sample ownership evidence")).
				With(option.Request(api.RegisterSampleRetirementOwnershipEvidenceRequest{})).
				With(option.Response(http.StatusCreated, api.SampleRetirementOwnershipEvidence{})).
				With(option.Response(http.StatusConflict, api.ErrorResponse{}))

			r.Post(
				"/recovery",
				registerSampleRetirementRecoveryEvidenceHandler(service),
			).With(option.Description("Register append-only backup or restore-proof evidence")).
				With(option.Request(api.RegisterSampleRetirementRecoveryEvidenceRequest{})).
				With(option.Response(http.StatusCreated, api.SampleRetirementRecoveryEvidence{})).
				With(option.Response(http.StatusConflict, api.ErrorResponse{}))
		})
	}
}

func sampleRetirementRouterWithService(
	r chiopenapi.Router,
	service SampleRetirementService,
) {
	r.WithOptions(option.GroupTags("Sample Retirements"))
	r.With(
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
		middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyOperatorControlPlaneV2),
	).Group(func(r chiopenapi.Router) {
		r.With(
			requireControlPlaneOrganizationAction(types.ActionSampleRetire),
			middleware.BlockSuperAdmin,
		).Post("/preview", previewSampleRetirementHandler(service)).
			With(option.Description("Preview an exact-ID sample-domain retirement")).
			With(option.Request(api.SampleRetirementPreviewRequest{})).
			With(option.Response(http.StatusCreated, api.SampleRetirementPreview{})).
			With(option.Response(http.StatusBadRequest, api.ErrorResponse{})).
			With(option.Response(http.StatusConflict, api.ErrorResponse{}))

		type sampleRetirementIDRequest struct {
			SampleRetirementID uuid.UUID `path:"sampleRetirementId"`
		}
		r.With(
			requireControlPlaneOrganizationAction(types.ActionAuditView),
		).Get("/{sampleRetirementId}", getSampleRetirementHandler(service)).
			With(option.Description("Get an organization-scoped sample retirement")).
			With(option.Request(sampleRetirementIDRequest{})).
			With(option.Response(http.StatusOK, api.SampleRetirementDetail{})).
			With(option.Response(http.StatusNotFound, api.ErrorResponse{}))

		r.With(
			requireControlPlaneOrganizationAction(types.ActionSampleRetire),
			middleware.BlockSuperAdmin,
		).Post(
			"/{sampleRetirementId}/approval-requests",
			requestSampleRetirementApprovalHandler(service),
		).With(option.Description("Request checksum-bound approval for an immutable retirement preview")).
			With(option.Request(struct {
				sampleRetirementIDRequest
				api.CreateApprovalRequestRequest
			}{})).
			With(option.Response(http.StatusCreated, api.ApprovalRequest{})).
			With(option.Response(http.StatusConflict, api.ErrorResponse{}))

		type applySampleRetirementRouteRequest struct {
			sampleRetirementIDRequest
			api.ApplySampleRetirementRequest
		}
		r.With(
			requireControlPlaneOrganizationAction(types.ActionSampleRetire),
			middleware.BlockSuperAdmin,
		).Post("/{sampleRetirementId}/apply", applySampleRetirementHandler(service)).
			With(option.Description("Apply an approved immutable sample retirement preview")).
			With(option.Request(applySampleRetirementRouteRequest{})).
			With(option.Response(http.StatusOK, api.SampleRetirementResult{})).
			With(option.Response(http.StatusBadRequest, api.ErrorResponse{})).
			With(option.Response(http.StatusConflict, api.ErrorResponse{}))

		r.With(
			requireControlPlaneOrganizationAction(types.ActionSampleRetire),
			middleware.BlockSuperAdmin,
		).Post("/{sampleRetirementId}/verify", verifySampleRetirementHandler(service)).
			With(option.Description("Verify exact counts, tombstones, and retained audit history")).
			With(option.Request(sampleRetirementIDRequest{})).
			With(option.Response(http.StatusOK, api.SampleRetirementVerification{})).
			With(option.Response(http.StatusConflict, api.ErrorResponse{}))
	})
}

func previewSampleRetirementHandler(service SampleRetirementService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := sampleRetirementJSONBody[api.SampleRetirementPreviewRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo, ok := sampleRetirementAuthentication(w, r)
		if !ok {
			return
		}
		preview, err := service.PreviewSampleRetirement(
			r.Context(),
			request.ToDomain(*authInfo.CurrentOrgID(), authInfo.CurrentUserID()),
		)
		if err != nil {
			handleSampleRetirementError(w, r, "preview", err)
			return
		}
		if preview == nil {
			handleSampleRetirementError(
				w,
				r,
				"preview",
				errors.New("sample retirement preview result is missing"),
			)
			return
		}
		RespondJSONWithStatus(w, http.StatusCreated, api.SampleRetirementPreview(*preview))
	}
}

func getSampleRetirementHandler(service SampleRetirementService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jobID, ok := sampleRetirementID(w, r)
		if !ok {
			return
		}
		authInfo, ok := sampleRetirementAuthentication(w, r)
		if !ok {
			return
		}
		detail, err := service.GetSampleRetirement(
			r.Context(),
			*authInfo.CurrentOrgID(),
			jobID,
		)
		if err != nil {
			handleSampleRetirementError(w, r, "get", err)
			return
		}
		if detail == nil {
			handleSampleRetirementError(
				w,
				r,
				"get",
				errors.New("sample retirement detail result is missing"),
			)
			return
		}
		RespondJSON(w, api.SampleRetirementDetail(*detail))
	}
}

func applySampleRetirementHandler(service SampleRetirementService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jobID, ok := sampleRetirementID(w, r)
		if !ok {
			return
		}
		request, err := sampleRetirementJSONBody[api.ApplySampleRetirementRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo, ok := sampleRetirementAuthentication(w, r)
		if !ok {
			return
		}
		result, err := service.ApplySampleRetirement(
			r.Context(),
			request.ToDomain(
				*authInfo.CurrentOrgID(),
				authInfo.CurrentUserID(),
				jobID,
			),
		)
		if err != nil {
			handleSampleRetirementError(w, r, "apply", err)
			return
		}
		if result == nil {
			handleSampleRetirementError(
				w,
				r,
				"apply",
				errors.New("sample retirement apply result is missing"),
			)
			return
		}
		RespondJSON(w, api.SampleRetirementResult(*result))
	}
}

func requestSampleRetirementApprovalHandler(
	service SampleRetirementService,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jobID, ok := sampleRetirementID(w, r)
		if !ok {
			return
		}
		request, err := sampleRetirementJSONBody[api.CreateApprovalRequestRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(time.Now().UTC()); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo, ok := sampleRetirementAuthentication(w, r)
		if !ok {
			return
		}
		created, err := service.RequestSampleRetirementApproval(
			r.Context(),
			types.SampleRetirementApprovalRequestInput{
				OrganizationID:           *authInfo.CurrentOrgID(),
				SampleRetirementJobID:    jobID,
				RequestedByUserAccountID: authInfo.CurrentUserID(),
				ExpiresAt:                request.ExpiresAt,
				Authorize: func(
					ctx context.Context,
					evidence types.ApprovalAuthorizationContext,
				) error {
					return authorizeSampleRetirementApprovalWithDependencies(
						ctx,
						authInfo,
						jobID,
						evidence,
						defaultControlPlaneResourceAuthorizationDependencies(),
					)
				},
			},
		)
		if err != nil {
			handleSampleRetirementError(w, r, "request approval", err)
			return
		}
		if created == nil {
			handleSampleRetirementError(
				w,
				r,
				"request approval",
				errors.New("sample retirement approval result is missing"),
			)
			return
		}
		RespondJSONWithStatus(
			w,
			http.StatusCreated,
			mapping.ApprovalRequestToAPI(*created),
		)
	}
}

func verifySampleRetirementHandler(service SampleRetirementService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jobID, ok := sampleRetirementID(w, r)
		if !ok {
			return
		}
		authInfo, ok := sampleRetirementAuthentication(w, r)
		if !ok {
			return
		}
		verification, err := service.VerifySampleRetirement(
			r.Context(),
			*authInfo.CurrentOrgID(),
			jobID,
		)
		if err != nil {
			handleSampleRetirementError(w, r, "verify", err)
			return
		}
		if verification == nil {
			handleSampleRetirementError(
				w,
				r,
				"verify",
				errors.New("sample retirement verification result is missing"),
			)
			return
		}
		RespondJSON(w, api.SampleRetirementVerification(*verification))
	}
}

func registerSampleRetirementOwnershipEvidenceHandler(
	service SampleRetirementEvidenceService,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := sampleRetirementJSONBody[api.RegisterSampleRetirementOwnershipEvidenceRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo, ok := sampleRetirementAuthentication(w, r)
		if !ok {
			return
		}
		evidence, err := service.RegisterSampleRetirementOwnershipEvidence(
			r.Context(),
			request.ToDomain(*authInfo.CurrentOrgID(), authInfo.CurrentUserID()),
		)
		if err != nil {
			handleSampleRetirementError(w, r, "register ownership evidence", err)
			return
		}
		if evidence == nil {
			handleSampleRetirementError(
				w,
				r,
				"register ownership evidence",
				errors.New("sample retirement ownership evidence result is missing"),
			)
			return
		}
		RespondJSONWithStatus(
			w,
			http.StatusCreated,
			api.SampleRetirementOwnershipEvidence(*evidence),
		)
	}
}

func registerSampleRetirementRecoveryEvidenceHandler(
	service SampleRetirementEvidenceService,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := sampleRetirementJSONBody[api.RegisterSampleRetirementRecoveryEvidenceRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(time.Now().UTC()); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo, ok := sampleRetirementAuthentication(w, r)
		if !ok {
			return
		}
		evidence, err := service.RegisterSampleRetirementRecoveryEvidence(
			r.Context(),
			request.ToDomain(*authInfo.CurrentOrgID(), authInfo.CurrentUserID()),
		)
		if err != nil {
			handleSampleRetirementError(w, r, "register recovery evidence", err)
			return
		}
		if evidence == nil {
			handleSampleRetirementError(
				w,
				r,
				"register recovery evidence",
				errors.New("sample retirement recovery evidence result is missing"),
			)
			return
		}
		RespondJSONWithStatus(
			w,
			http.StatusCreated,
			api.SampleRetirementRecoveryEvidence(*evidence),
		)
	}
}

func sampleRetirementID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("sampleRetirementId"))
	if err != nil || id == uuid.Nil {
		http.NotFound(w, r)
		return uuid.Nil, false
	}
	return id, true
}

func sampleRetirementJSONBody[T any](
	w http.ResponseWriter,
	r *http.Request,
) (T, error) {
	var value T
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		http.Error(w, "sample retirement request body is invalid", http.StatusBadRequest)
		return value, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		http.Error(w, "sample retirement request body is invalid", http.StatusBadRequest)
		if err == nil {
			err = errors.New("multiple sample retirement request bodies")
		}
		return value, err
	}
	return value, nil
}

func sampleRetirementAuthentication(
	w http.ResponseWriter,
	r *http.Request,
) (authinfo.AuthInfo, bool) {
	authInfo, err := auth.Authentication.Get(r.Context())
	if err != nil ||
		authInfo.CurrentOrgID() == nil ||
		*authInfo.CurrentOrgID() == uuid.Nil ||
		authInfo.CurrentUserID() == uuid.Nil {
		http.Error(w, "sample retirement operation is forbidden", http.StatusForbidden)
		return nil, false
	}
	return authInfo, true
}

func authorizeSampleRetirementApprovalWithDependencies(
	ctx context.Context,
	authInfo authinfo.AuthInfo,
	jobID uuid.UUID,
	evidence types.ApprovalAuthorizationContext,
	dependencies controlPlaneResourceAuthorizationDependencies,
) error {
	if authInfo == nil ||
		authInfo.CurrentOrgID() == nil ||
		*authInfo.CurrentOrgID() == uuid.Nil ||
		authInfo.CurrentUserID() == uuid.Nil ||
		jobID == uuid.Nil ||
		evidence.OrganizationID != *authInfo.CurrentOrgID() ||
		evidence.ActorUserAccountID != authInfo.CurrentUserID() ||
		evidence.SampleRetirementJobID != jobID ||
		evidence.DecisionAt.IsZero() {
		return apierrors.ErrForbidden
	}
	organizationID := *authInfo.CurrentOrgID()
	return authorizeControlPlaneResourceWithDependencies(
		ctx,
		controlPlaneResourceAuthorizationRequest{
			OrganizationID: organizationID,
			PrincipalID:    authInfo.CurrentUserID(),
			CredentialRole: authInfo.CurrentUserRole(),
			IsSuperAdmin:   authInfo.IsSuperAdmin(),
			Action:         types.ActionSampleRetire,
			Resource: types.ResourceRef{
				OrganizationID: organizationID,
				Kind:           types.PermissionScopeOrganization,
				ID:             organizationID,
			},
			DecisionAt: evidence.DecisionAt,
		},
		dependencies,
	)
}

func handleSampleRetirementError(
	w http.ResponseWriter,
	r *http.Request,
	action string,
	err error,
) {
	status, message := sampleRetirementPublicError(err)
	if status == http.StatusInternalServerError {
		log := internalctx.GetLogger(r.Context())
		log.Error("failed to "+action+" sample retirement", zap.Error(err))
		if hub := sentry.GetHubFromContext(r.Context()); hub != nil {
			hub.CaptureException(err)
		}
	}
	http.Error(w, message, status)
}

func sampleRetirementPublicError(err error) (int, string) {
	switch {
	case errors.Is(err, apierrors.ErrBadRequest):
		return http.StatusBadRequest, "sample retirement request is invalid"
	case errors.Is(err, apierrors.ErrForbidden):
		return http.StatusForbidden, "sample retirement operation is forbidden"
	case errors.Is(err, apierrors.ErrNotFound):
		return http.StatusNotFound, "sample retirement not found"
	case errors.Is(err, apierrors.ErrConflict), errors.Is(err, apierrors.ErrAlreadyExists):
		return http.StatusConflict, "sample retirement conflicts with current state"
	case errors.Is(err, retirement.ErrStaleRetirementPreview),
		errors.Is(err, retirement.ErrRetirementVerification):
		return http.StatusConflict, "sample retirement conflicts with current state"
	default:
		return http.StatusInternalServerError, "sample retirement operation failed"
	}
}

type databaseSampleRetirementService struct {
	apply *retirement.ApplyService
}

type databaseSampleRetirementEvidenceService struct{}

func (databaseSampleRetirementEvidenceService) RegisterSampleRetirementOwnershipEvidence(
	ctx context.Context,
	input types.SampleRetirementOwnershipEvidenceRegistrationInput,
) (*types.SampleRetirementOwnershipEvidence, error) {
	return db.RegisterSampleRetirementOwnershipEvidence(ctx, input)
}

func (databaseSampleRetirementEvidenceService) RegisterSampleRetirementRecoveryEvidence(
	ctx context.Context,
	input types.SampleRetirementRecoveryEvidenceRegistrationInput,
) (*types.SampleRetirementRecoveryEvidence, error) {
	return db.RegisterSampleRetirementRecoveryEvidence(ctx, input)
}

type sampleRetirementApprovalResolver func(
	context.Context,
	uuid.UUID,
	uuid.UUID,
	uuid.UUID,
) (types.SampleRetirementApprovalBinding, error)

func newDatabaseSampleRetirementService() SampleRetirementService {
	store := db.SampleRetirementRepository{}
	return &databaseSampleRetirementService{
		apply: retirement.NewApplyService(store),
	}
}

func (s *databaseSampleRetirementService) PreviewSampleRetirement(
	ctx context.Context,
	request types.SampleRetirementRequest,
) (*types.SampleRetirementPreview, error) {
	return retirement.PreviewSampleRetirement(
		ctx,
		db.SampleRetirementRepository{},
		request,
	)
}

func (s *databaseSampleRetirementService) GetSampleRetirement(
	ctx context.Context,
	organizationID, jobID uuid.UUID,
) (*types.SampleRetirementDetail, error) {
	return db.GetSampleRetirementDetail(ctx, organizationID, jobID)
}

func (s *databaseSampleRetirementService) RequestSampleRetirementApproval(
	ctx context.Context,
	input types.SampleRetirementApprovalRequestInput,
) (*types.ApprovalRequest, error) {
	return db.RequestSampleRetirementApproval(ctx, input)
}

func (s *databaseSampleRetirementService) ApplySampleRetirement(
	ctx context.Context,
	request types.SampleRetirementApplyRequest,
) (*types.SampleRetirementResult, error) {
	resolved, err := resolveSampleRetirementApplyApproval(
		ctx,
		request,
		db.ResolveSampleRetirementApproval,
	)
	if err != nil {
		return nil, err
	}
	return s.apply.Apply(ctx, resolved.OrganizationID, resolved.JobID, resolved)
}

func (s *databaseSampleRetirementService) VerifySampleRetirement(
	ctx context.Context,
	organizationID, jobID uuid.UUID,
) (*types.SampleRetirementVerification, error) {
	return s.apply.Verify(ctx, organizationID, jobID)
}

func resolveSampleRetirementApplyApproval(
	ctx context.Context,
	request types.SampleRetirementApplyRequest,
	resolve sampleRetirementApprovalResolver,
) (types.SampleRetirementApplyRequest, error) {
	if resolve == nil {
		return types.SampleRetirementApplyRequest{},
			errors.New("sample retirement approval resolver is unavailable")
	}
	approvalID, err := uuid.Parse(request.ApprovalID)
	if err != nil || approvalID == uuid.Nil {
		return types.SampleRetirementApplyRequest{},
			apierrors.NewBadRequest("sample retirement approval ID is invalid")
	}
	binding, err := resolve(
		ctx,
		request.OrganizationID,
		request.JobID,
		approvalID,
	)
	if err != nil {
		return types.SampleRetirementApplyRequest{}, err
	}
	if binding.ApprovalRequestID != approvalID ||
		binding.ApprovalChecksum != request.ApprovalChecksum {
		return types.SampleRetirementApplyRequest{},
			apierrors.NewConflict("sample retirement approval evidence is stale")
	}
	request.ApprovalID = binding.ApprovalRequestID.String()
	request.ApprovalChecksum = binding.ApprovalChecksum
	return request, nil
}
