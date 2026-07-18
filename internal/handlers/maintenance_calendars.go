package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

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

const (
	maintenanceCalendarMaximumBodyBytes int64 = 2 << 20
	calendarActionManage                      = "calendar.manage"
	freezeActionManage                        = "freeze.manage"
)

type calendarActionAuthorizer interface {
	AuthorizeCalendarAction(
		context.Context,
		uuid.UUID,
		uuid.UUID,
		string,
		types.CalendarScopeRef,
	) error
}

type calendarActionAuthorizerFunc func(
	context.Context,
	uuid.UUID,
	uuid.UUID,
	string,
	types.CalendarScopeRef,
) error

func (fn calendarActionAuthorizerFunc) AuthorizeCalendarAction(
	ctx context.Context,
	organizationID, actorID uuid.UUID,
	action string,
	scope types.CalendarScopeRef,
) error {
	return fn(ctx, organizationID, actorID, action, scope)
}

// unavailableCalendarActionAuthorizer keeps calendar mutations fail-closed until
// PR-066's shared authorization.Authorize adapter is rebased into this seam.
type unavailableCalendarActionAuthorizer struct{}

func (unavailableCalendarActionAuthorizer) AuthorizeCalendarAction(
	context.Context,
	uuid.UUID,
	uuid.UUID,
	string,
	types.CalendarScopeRef,
) error {
	return apierrors.ErrForbidden
}

func newCalendarActionAuthorizer() calendarActionAuthorizer {
	return unavailableCalendarActionAuthorizer{}
}

type maintenanceCalendarIDRequest struct {
	CalendarID uuid.UUID `path:"calendarId"`
}

type maintenanceCalendarVersionIDRequest struct {
	maintenanceCalendarIDRequest
	VersionID uuid.UUID `path:"versionId"`
}

type deploymentFreezeIDRequest struct {
	FreezeID uuid.UUID `path:"freezeId"`
}

type deploymentFreezeRevisionIDRequest struct {
	deploymentFreezeIDRequest
	RevisionID uuid.UUID `path:"revisionId"`
}

func MaintenanceCalendarsRouter(r chiopenapi.Router) {
	maintenanceCalendarsRouterWithDependencies(
		r,
		env.ExperimentalFeatureFlags(),
		newCalendarActionAuthorizer(),
	)
}

//nolint:dupl // Calendar and freeze route contracts intentionally stay explicit and symmetric.
func maintenanceCalendarsRouterWithDependencies(
	r chiopenapi.Router,
	enabledFlags []featureflags.Key,
	authorizer calendarActionAuthorizer,
) {
	r.WithOptions(option.GroupTags("Maintenance Calendars"))
	r.With(
		maintenanceCalendarFeatureFlagMiddlewareWithFlags(enabledFlags),
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
		middleware.RequireAdmin,
		middleware.BlockSuperAdmin,
	).Group(func(r chiopenapi.Router) {
		r.Get("/", listMaintenanceCalendarsHandler()).
			With(option.Description("List organization-scoped maintenance calendar drafts")).
			With(option.Request(api.MaintenanceCalendarListRequest{})).
			With(option.Response(http.StatusOK, api.MaintenanceCalendarPage{}))
		r.Post("/", createMaintenanceCalendarHandler(authorizer)).
			With(option.Description("Create a mutable maintenance calendar draft")).
			With(option.Request(api.CreateMaintenanceCalendarRequest{})).
			With(option.Response(http.StatusCreated, api.MaintenanceCalendar{}))
		r.Route("/{calendarId}", func(r chiopenapi.Router) {
			r.Get("/", getMaintenanceCalendarHandler()).
				With(option.Description("Get an organization-scoped maintenance calendar draft")).
				With(option.Request(maintenanceCalendarIDRequest{})).
				With(option.Response(http.StatusOK, api.MaintenanceCalendar{}))
			r.Put("/", updateMaintenanceCalendarHandler(authorizer)).
				With(option.Description("Update a maintenance calendar draft using optimistic concurrency")).
				With(option.Request(struct {
					maintenanceCalendarIDRequest
					api.UpdateMaintenanceCalendarRequest
				}{})).
				With(option.Response(http.StatusOK, api.MaintenanceCalendar{}))
			r.Post("/publish", publishMaintenanceCalendarHandler(authorizer)).
				With(option.Description("Publish an immutable checksum-bound calendar version")).
				With(option.Request(struct {
					maintenanceCalendarIDRequest
					api.PublishMaintenanceCalendarRequest
				}{})).
				With(option.Response(http.StatusOK, api.MaintenanceCalendarVersion{}))
			r.Get("/versions", listMaintenanceCalendarVersionsHandler()).
				With(option.Description("List immutable maintenance calendar versions")).
				With(option.Request(struct {
					maintenanceCalendarIDRequest
					api.MaintenanceCalendarListRequest
				}{})).
				With(option.Response(http.StatusOK, api.MaintenanceCalendarVersionPage{}))
			r.Get("/versions/{versionId}", getMaintenanceCalendarVersionHandler()).
				With(option.Description("Get an immutable checksum-bound calendar version")).
				With(option.Request(maintenanceCalendarVersionIDRequest{})).
				With(option.Response(http.StatusOK, api.MaintenanceCalendarVersion{}))
		})
	})
}

func DeploymentFreezesRouter(r chiopenapi.Router) {
	deploymentFreezesRouterWithDependencies(
		r,
		env.ExperimentalFeatureFlags(),
		newCalendarActionAuthorizer(),
	)
}

//nolint:dupl // Calendar and freeze route contracts intentionally stay explicit and symmetric.
func deploymentFreezesRouterWithDependencies(
	r chiopenapi.Router,
	enabledFlags []featureflags.Key,
	authorizer calendarActionAuthorizer,
) {
	r.WithOptions(option.GroupTags("Deployment Freezes"))
	r.With(
		maintenanceCalendarFeatureFlagMiddlewareWithFlags(enabledFlags),
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
		middleware.RequireAdmin,
		middleware.BlockSuperAdmin,
	).Group(func(r chiopenapi.Router) {
		r.Get("/", listDeploymentFreezesHandler()).
			With(option.Description("List organization-scoped deployment freeze drafts")).
			With(option.Request(api.MaintenanceCalendarListRequest{})).
			With(option.Response(http.StatusOK, api.DeploymentFreezePage{}))
		r.Post("/", createDeploymentFreezeHandler(authorizer)).
			With(option.Description("Create a mutable deployment freeze draft")).
			With(option.Request(api.CreateDeploymentFreezeRequest{})).
			With(option.Response(http.StatusCreated, api.DeploymentFreeze{}))
		r.Route("/{freezeId}", func(r chiopenapi.Router) {
			r.Get("/", getDeploymentFreezeHandler()).
				With(option.Description("Get an organization-scoped deployment freeze draft")).
				With(option.Request(deploymentFreezeIDRequest{})).
				With(option.Response(http.StatusOK, api.DeploymentFreeze{}))
			r.Put("/", updateDeploymentFreezeHandler(authorizer)).
				With(option.Description("Update a deployment freeze draft using optimistic concurrency")).
				With(option.Request(struct {
					deploymentFreezeIDRequest
					api.UpdateDeploymentFreezeRequest
				}{})).
				With(option.Response(http.StatusOK, api.DeploymentFreeze{}))
			r.Post("/publish", publishDeploymentFreezeHandler(authorizer)).
				With(option.Description("Publish an immutable checksum-bound freeze revision")).
				With(option.Request(struct {
					deploymentFreezeIDRequest
					api.PublishDeploymentFreezeRequest
				}{})).
				With(option.Response(http.StatusOK, api.DeploymentFreezeRevision{}))
			r.Get("/revisions", listDeploymentFreezeRevisionsHandler()).
				With(option.Description("List immutable deployment freeze revisions")).
				With(option.Request(struct {
					deploymentFreezeIDRequest
					api.MaintenanceCalendarListRequest
				}{})).
				With(option.Response(http.StatusOK, api.DeploymentFreezeRevisionPage{}))
			r.Get("/revisions/{revisionId}", getDeploymentFreezeRevisionHandler()).
				With(option.Description("Get an immutable checksum-bound freeze revision")).
				With(option.Request(deploymentFreezeRevisionIDRequest{})).
				With(option.Response(http.StatusOK, api.DeploymentFreezeRevision{}))
		})
	})
}

func maintenanceCalendarFeatureFlagMiddlewareWithFlags(
	enabledFlags []featureflags.Key,
) func(http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !featureflags.NewRegistry(enabledFlags).
				IsEnabled(featureflags.KeyOperatorControlPlaneV2) {
				http.NotFound(w, r)
				return
			}
			handler.ServeHTTP(w, r)
		})
	}
}

func listMaintenanceCalendarsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, ok := maintenanceCalendarListRequestFromHTTP(w, r)
		if !ok {
			return
		}
		ctx := r.Context()
		userAuth := auth.Authentication.Require(ctx)
		page, err := db.ListMaintenanceCalendars(ctx, types.CalendarListFilter{
			OrganizationID: *userAuth.CurrentOrgID(),
			Cursor:         request.Cursor,
			Limit:          request.Limit,
		})
		if err != nil {
			handleMaintenanceCalendarError(w, r, "list maintenance calendars", err)
			return
		}
		RespondJSON(w, mapping.MaintenanceCalendarPageToAPI(page))
	}
}

func getMaintenanceCalendarHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		calendarID, ok := maintenanceCalendarPathID(w, r, "calendarId")
		if !ok {
			return
		}
		ctx := r.Context()
		userAuth := auth.Authentication.Require(ctx)
		calendar, err := db.GetMaintenanceCalendar(
			ctx,
			*userAuth.CurrentOrgID(),
			calendarID,
		)
		if err != nil {
			handleMaintenanceCalendarError(w, r, "get maintenance calendar", err)
			return
		}
		RespondJSON(w, mapping.MaintenanceCalendarToAPI(*calendar))
	}
}

func createMaintenanceCalendarHandler(
	authorizer calendarActionAuthorizer,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := maintenanceCalendarJSONBody[api.CreateMaintenanceCalendarRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx := r.Context()
		userAuth := auth.Authentication.Require(ctx)
		organizationID := *userAuth.CurrentOrgID()
		if err := authorizeCalendarAction(
			ctx,
			authorizer,
			organizationID,
			userAuth.CurrentUserID(),
			calendarActionManage,
			types.CalendarScopeRef{
				Kind: types.CalendarScopeOrganization,
				ID:   organizationID,
			},
		); err != nil {
			handleMaintenanceCalendarError(w, r, "authorize calendar creation", err)
			return
		}
		calendar := request.ToDomain(organizationID, userAuth.CurrentUserID())
		if err := db.CreateMaintenanceCalendar(ctx, &calendar); err != nil {
			handleMaintenanceCalendarError(w, r, "create maintenance calendar", err)
			return
		}
		RespondJSONWithStatus(
			w,
			http.StatusCreated,
			mapping.MaintenanceCalendarToAPI(calendar),
		)
	}
}

func updateMaintenanceCalendarHandler(
	authorizer calendarActionAuthorizer,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		calendarID, ok := maintenanceCalendarPathID(w, r, "calendarId")
		if !ok {
			return
		}
		request, err := maintenanceCalendarJSONBody[api.UpdateMaintenanceCalendarRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx := r.Context()
		userAuth := auth.Authentication.Require(ctx)
		organizationID := *userAuth.CurrentOrgID()
		if err := authorizeCalendarAction(
			ctx,
			authorizer,
			organizationID,
			userAuth.CurrentUserID(),
			calendarActionManage,
			types.CalendarScopeRef{
				Kind: types.CalendarScopeOrganization,
				ID:   organizationID,
			},
		); err != nil {
			handleMaintenanceCalendarError(w, r, "authorize calendar update", err)
			return
		}
		calendar := (api.CreateMaintenanceCalendarRequest{
			Name:        request.Name,
			Description: request.Description,
			IANAZone:    request.IANAZone,
			RuleVersion: request.RuleVersion,
			WindowRules: request.WindowRules,
		}).ToDomain(organizationID, userAuth.CurrentUserID())
		calendar.ID = calendarID
		calendar.DraftRevision = request.ExpectedDraftRevision
		if err := db.UpdateMaintenanceCalendar(ctx, &calendar); err != nil {
			handleMaintenanceCalendarError(w, r, "update maintenance calendar", err)
			return
		}
		RespondJSON(w, mapping.MaintenanceCalendarToAPI(calendar))
	}
}

func publishMaintenanceCalendarHandler(
	authorizer calendarActionAuthorizer,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		calendarID, ok := maintenanceCalendarPathID(w, r, "calendarId")
		if !ok {
			return
		}
		request, err := maintenanceCalendarJSONBody[api.PublishMaintenanceCalendarRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx := r.Context()
		userAuth := auth.Authentication.Require(ctx)
		organizationID := *userAuth.CurrentOrgID()
		if err := authorizeCalendarAction(
			ctx,
			authorizer,
			organizationID,
			userAuth.CurrentUserID(),
			calendarActionManage,
			types.CalendarScopeRef{
				Kind: types.CalendarScopeOrganization,
				ID:   organizationID,
			},
		); err != nil {
			handleMaintenanceCalendarError(w, r, "authorize calendar publication", err)
			return
		}
		version, err := db.PublishMaintenanceCalendar(
			ctx,
			organizationID,
			calendarID,
			request.ExpectedDraftRevision,
			userAuth.CurrentUserID(),
		)
		if err != nil {
			handleMaintenanceCalendarError(w, r, "publish maintenance calendar", err)
			return
		}
		RespondJSON(w, mapping.MaintenanceCalendarVersionToAPI(*version))
	}
}

func listMaintenanceCalendarVersionsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		calendarID, ok := maintenanceCalendarPathID(w, r, "calendarId")
		if !ok {
			return
		}
		request, ok := maintenanceCalendarListRequestFromHTTP(w, r)
		if !ok {
			return
		}
		ctx := r.Context()
		userAuth := auth.Authentication.Require(ctx)
		organizationID := *userAuth.CurrentOrgID()
		page, err := db.ListMaintenanceCalendarVersions(
			ctx,
			organizationID,
			calendarID,
			types.CalendarListFilter{
				OrganizationID: organizationID,
				Cursor:         request.Cursor,
				Limit:          request.Limit,
			},
		)
		if err != nil {
			handleMaintenanceCalendarError(w, r, "list maintenance calendar versions", err)
			return
		}
		RespondJSON(w, mapping.MaintenanceCalendarVersionPageToAPI(page))
	}
}

func getMaintenanceCalendarVersionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		calendarID, ok := maintenanceCalendarPathID(w, r, "calendarId")
		if !ok {
			return
		}
		versionID, ok := maintenanceCalendarPathID(w, r, "versionId")
		if !ok {
			return
		}
		ctx := r.Context()
		userAuth := auth.Authentication.Require(ctx)
		version, err := db.GetMaintenanceCalendarVersion(
			ctx,
			*userAuth.CurrentOrgID(),
			calendarID,
			versionID,
		)
		if err != nil {
			handleMaintenanceCalendarError(w, r, "get maintenance calendar version", err)
			return
		}
		RespondJSON(w, mapping.MaintenanceCalendarVersionToAPI(*version))
	}
}

func listDeploymentFreezesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, ok := maintenanceCalendarListRequestFromHTTP(w, r)
		if !ok {
			return
		}
		ctx := r.Context()
		userAuth := auth.Authentication.Require(ctx)
		page, err := db.ListDeploymentFreezes(ctx, types.CalendarListFilter{
			OrganizationID: *userAuth.CurrentOrgID(),
			Cursor:         request.Cursor,
			Limit:          request.Limit,
		})
		if err != nil {
			handleMaintenanceCalendarError(w, r, "list deployment freezes", err)
			return
		}
		RespondJSON(w, mapping.DeploymentFreezePageToAPI(page))
	}
}

func getDeploymentFreezeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		freezeID, ok := maintenanceCalendarPathID(w, r, "freezeId")
		if !ok {
			return
		}
		ctx := r.Context()
		userAuth := auth.Authentication.Require(ctx)
		freeze, err := db.GetDeploymentFreeze(ctx, *userAuth.CurrentOrgID(), freezeID)
		if err != nil {
			handleMaintenanceCalendarError(w, r, "get deployment freeze", err)
			return
		}
		RespondJSON(w, mapping.DeploymentFreezeToAPI(*freeze))
	}
}

func createDeploymentFreezeHandler(
	authorizer calendarActionAuthorizer,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := maintenanceCalendarJSONBody[api.CreateDeploymentFreezeRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx := r.Context()
		userAuth := auth.Authentication.Require(ctx)
		organizationID := *userAuth.CurrentOrgID()
		scope := types.CalendarScopeRef{Kind: request.ScopeKind, ID: request.ScopeID}
		if err := authorizeCalendarAction(
			ctx,
			authorizer,
			organizationID,
			userAuth.CurrentUserID(),
			freezeActionManage,
			scope,
		); err != nil {
			handleMaintenanceCalendarError(w, r, "authorize deployment freeze creation", err)
			return
		}
		freeze := request.ToDomain(organizationID, userAuth.CurrentUserID())
		if err := db.CreateDeploymentFreeze(ctx, &freeze); err != nil {
			handleMaintenanceCalendarError(w, r, "create deployment freeze", err)
			return
		}
		RespondJSONWithStatus(w, http.StatusCreated, mapping.DeploymentFreezeToAPI(freeze))
	}
}

func updateDeploymentFreezeHandler(
	authorizer calendarActionAuthorizer,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		freezeID, ok := maintenanceCalendarPathID(w, r, "freezeId")
		if !ok {
			return
		}
		request, err := maintenanceCalendarJSONBody[api.UpdateDeploymentFreezeRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx := r.Context()
		userAuth := auth.Authentication.Require(ctx)
		organizationID := *userAuth.CurrentOrgID()
		current, err := db.GetDeploymentFreeze(ctx, organizationID, freezeID)
		if err != nil {
			handleMaintenanceCalendarError(w, r, "get deployment freeze for update", err)
			return
		}
		if current.DraftRevision != request.ExpectedDraftRevision {
			handleMaintenanceCalendarError(
				w,
				r,
				"bind deployment freeze update revision",
				apierrors.NewConflict("deployment freeze draft revision changed"),
			)
			return
		}
		if err := authorizeDeploymentFreezeScopeTransition(
			ctx,
			authorizer,
			organizationID,
			userAuth.CurrentUserID(),
			types.CalendarScopeRef{
				Kind: current.DraftScopeKind,
				ID:   current.DraftScopeID,
			},
			types.CalendarScopeRef{Kind: request.ScopeKind, ID: request.ScopeID},
		); err != nil {
			handleMaintenanceCalendarError(w, r, "authorize deployment freeze update", err)
			return
		}
		freeze := (api.CreateDeploymentFreezeRequest{
			Name:        request.Name,
			StartAt:     request.StartAt,
			EndAt:       request.EndAt,
			IANAZone:    request.IANAZone,
			RuleVersion: request.RuleVersion,
			ScopeKind:   request.ScopeKind,
			ScopeID:     request.ScopeID,
			Priority:    request.Priority,
			Reason:      request.Reason,
		}).ToDomain(organizationID, userAuth.CurrentUserID())
		freeze.ID = freezeID
		freeze.DraftRevision = request.ExpectedDraftRevision
		if err := db.UpdateDeploymentFreeze(ctx, &freeze); err != nil {
			handleMaintenanceCalendarError(w, r, "update deployment freeze", err)
			return
		}
		RespondJSON(w, mapping.DeploymentFreezeToAPI(freeze))
	}
}

func publishDeploymentFreezeHandler(
	authorizer calendarActionAuthorizer,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		freezeID, ok := maintenanceCalendarPathID(w, r, "freezeId")
		if !ok {
			return
		}
		request, err := maintenanceCalendarJSONBody[api.PublishDeploymentFreezeRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx := r.Context()
		userAuth := auth.Authentication.Require(ctx)
		organizationID := *userAuth.CurrentOrgID()
		revision, err := db.PublishDeploymentFreeze(
			ctx,
			organizationID,
			freezeID,
			request.ExpectedDraftRevision,
			userAuth.CurrentUserID(),
			func(txCtx context.Context, scope types.CalendarScopeRef) error {
				return authorizeCalendarAction(
					txCtx,
					authorizer,
					organizationID,
					userAuth.CurrentUserID(),
					freezeActionManage,
					scope,
				)
			},
		)
		if err != nil {
			handleMaintenanceCalendarError(w, r, "publish deployment freeze", err)
			return
		}
		RespondJSON(w, mapping.DeploymentFreezeRevisionToAPI(*revision))
	}
}

func listDeploymentFreezeRevisionsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		freezeID, ok := maintenanceCalendarPathID(w, r, "freezeId")
		if !ok {
			return
		}
		request, ok := maintenanceCalendarListRequestFromHTTP(w, r)
		if !ok {
			return
		}
		ctx := r.Context()
		userAuth := auth.Authentication.Require(ctx)
		organizationID := *userAuth.CurrentOrgID()
		page, err := db.ListDeploymentFreezeRevisions(
			ctx,
			organizationID,
			freezeID,
			types.CalendarListFilter{
				OrganizationID: organizationID,
				Cursor:         request.Cursor,
				Limit:          request.Limit,
			},
		)
		if err != nil {
			handleMaintenanceCalendarError(w, r, "list deployment freeze revisions", err)
			return
		}
		RespondJSON(w, mapping.DeploymentFreezeRevisionPageToAPI(page))
	}
}

func getDeploymentFreezeRevisionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		freezeID, ok := maintenanceCalendarPathID(w, r, "freezeId")
		if !ok {
			return
		}
		revisionID, ok := maintenanceCalendarPathID(w, r, "revisionId")
		if !ok {
			return
		}
		ctx := r.Context()
		userAuth := auth.Authentication.Require(ctx)
		revision, err := db.GetDeploymentFreezeRevision(
			ctx,
			*userAuth.CurrentOrgID(),
			freezeID,
			revisionID,
		)
		if err != nil {
			handleMaintenanceCalendarError(w, r, "get deployment freeze revision", err)
			return
		}
		RespondJSON(w, mapping.DeploymentFreezeRevisionToAPI(*revision))
	}
}

func maintenanceCalendarListRequestFromHTTP(
	w http.ResponseWriter,
	r *http.Request,
) (api.MaintenanceCalendarListRequest, bool) {
	query := r.URL.Query()
	request := api.MaintenanceCalendarListRequest{}
	if values, exists := query["cursor"]; exists {
		if len(values) != 1 {
			http.Error(w, "cursor must be provided once", http.StatusBadRequest)
			return api.MaintenanceCalendarListRequest{}, false
		}
		request.Cursor = values[0]
	}
	if values, exists := query["limit"]; exists {
		if len(values) != 1 {
			http.Error(w, "limit must be provided once", http.StatusBadRequest)
			return api.MaintenanceCalendarListRequest{}, false
		}
		limit, err := strconv.Atoi(values[0])
		if err != nil || limit < 1 {
			http.Error(
				w,
				"limit must be between 1 and 100 when provided",
				http.StatusBadRequest,
			)
			return api.MaintenanceCalendarListRequest{}, false
		}
		request.Limit = limit
	}
	if err := request.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return api.MaintenanceCalendarListRequest{}, false
	}
	return request, true
}

func maintenanceCalendarJSONBody[T any](
	w http.ResponseWriter,
	r *http.Request,
) (T, error) {
	var value T
	r.Body = http.MaxBytesReader(w, r.Body, maintenanceCalendarMaximumBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return value, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("request body must contain exactly one JSON value")
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return value, err
	}
	return value, nil
}

func maintenanceCalendarPathID(
	w http.ResponseWriter,
	r *http.Request,
	pathKey string,
) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue(pathKey))
	if err != nil || id == uuid.Nil {
		http.NotFound(w, r)
		return uuid.Nil, false
	}
	return id, true
}

func authorizeCalendarAction(
	ctx context.Context,
	authorizer calendarActionAuthorizer,
	organizationID, actorID uuid.UUID,
	action string,
	scope types.CalendarScopeRef,
) error {
	if authorizer == nil {
		return errors.New("calendar action authorizer is not configured")
	}
	if organizationID == uuid.Nil || actorID == uuid.Nil ||
		action == "" || !scope.Kind.IsValid() || scope.ID == uuid.Nil {
		return apierrors.ErrForbidden
	}
	return authorizer.AuthorizeCalendarAction(
		ctx,
		organizationID,
		actorID,
		action,
		scope,
	)
}

func authorizeDeploymentFreezeScopeTransition(
	ctx context.Context,
	authorizer calendarActionAuthorizer,
	organizationID, actorID uuid.UUID,
	current, destination types.CalendarScopeRef,
) error {
	if err := authorizeCalendarAction(
		ctx,
		authorizer,
		organizationID,
		actorID,
		freezeActionManage,
		current,
	); err != nil {
		return err
	}
	if destination == current {
		return nil
	}
	return authorizeCalendarAction(
		ctx,
		authorizer,
		organizationID,
		actorID,
		freezeActionManage,
		destination,
	)
}

func handleMaintenanceCalendarError(
	w http.ResponseWriter,
	r *http.Request,
	action string,
	err error,
) {
	switch {
	case errors.Is(err, apierrors.ErrBadRequest):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, apierrors.ErrForbidden):
		http.Error(w, "insufficient permissions", http.StatusForbidden)
	case errors.Is(err, apierrors.ErrNotFound):
		http.NotFound(w, r)
	case errors.Is(err, apierrors.ErrAlreadyExists):
		http.Error(w, "resource already exists", http.StatusConflict)
	case errors.Is(err, apierrors.ErrConflict):
		http.Error(w, "resource revision changed", http.StatusConflict)
	default:
		log := internalctx.GetLogger(r.Context())
		if log == nil {
			log = zap.NewNop()
		}
		log.Error("failed to "+action, zap.Error(err))
		if hub := sentry.GetHubFromContext(r.Context()); hub != nil {
			hub.CaptureException(err)
		}
		http.Error(
			w,
			http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError,
		)
	}
}
