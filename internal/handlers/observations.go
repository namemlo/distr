package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/mapping"
	"github.com/distr-sh/distr/internal/middleware"
	"github.com/distr-sh/distr/internal/observation"
	"github.com/distr-sh/distr/internal/types"
	"github.com/getsentry/sentry-go"
	"github.com/oaswrap/spec/adapter/chiopenapi"
	"github.com/oaswrap/spec/option"
	"go.uber.org/zap"
)

func ObserverIngestRouter(r chiopenapi.Router) {
	r.WithOptions(option.GroupTags("Independent Observer"))
	r.With(
		middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyOperatorControlPlaneV2),
	).Post("/", ingestObservationHandler()).
		With(option.Description("Ingest authenticated independent runtime evidence")).
		With(option.Request(api.ObservationRequest{})).
		With(option.Response(http.StatusAccepted, api.ObservedComponentState{})).
		With(option.Response(http.StatusUnauthorized, api.ErrorResponse{})).
		With(option.Response(http.StatusConflict, api.ErrorResponse{}))
}

func ObserverRegistrationsRouter(r chiopenapi.Router) {
	r.WithOptions(option.GroupTags("Observer Registrations"))
	r.With(
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
		middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyOperatorControlPlaneV2),
	).Group(func(r chiopenapi.Router) {
		r.Get("/", listObserverRegistrationsHandler()).
			With(option.Description("List independent observer registrations")).
			With(option.Response(http.StatusOK, []api.ObserverRegistration{}))
		r.With(
			middleware.RequireReadWriteOrAdmin,
			middleware.BlockSuperAdmin,
		).Post("/", createObserverRegistrationHandler()).
			With(option.Description("Register an independent observer trust boundary")).
			With(option.Request(api.CreateObserverRegistrationRequest{})).
			With(option.Response(http.StatusOK, api.ObserverRegistration{}))
	})
}

func ObservationsRouter(r chiopenapi.Router) {
	r.WithOptions(option.GroupTags("Observed State"))
	r.With(
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
		middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyOperatorControlPlaneV2),
	).Get("/", listObservationsHandler()).
		With(option.Description("List retained independent observations")).
		With(option.Response(http.StatusOK, []api.ObservedComponentState{}))
}

func ingestObservationHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		credential, err := observerCredential(r)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		request, err := JsonBody[api.ObservationRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		envelope := request.ToEnvelope(api.ObserverCredentialFingerprint(credential))
		state, err := db.IngestObservation(r.Context(), envelope)
		if err != nil {
			handleObservationError(w, r, "ingest", err)
			return
		}
		RespondJSONWithStatus(
			w,
			http.StatusAccepted,
			mapping.ObservedComponentStateToAPI(*state),
		)
	}
}

func createObserverRegistrationHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := JsonBody[api.CreateObserverRegistrationRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authentication := auth.Authentication.Require(r.Context())
		registration, err := db.CreateObserverRegistration(
			r.Context(),
			&types.ObserverRegistration{
				OrganizationID:        *authentication.CurrentOrgID(),
				DeploymentUnitID:      request.DeploymentUnitID,
				ComponentInstanceID:   request.ComponentInstanceID,
				ObserverKey:           strings.TrimSpace(request.ObserverKey),
				AdapterImplementation: strings.TrimSpace(request.AdapterImplementation),
				AdapterVersion:        strings.TrimSpace(request.AdapterVersion),
				Enabled:               true,
				CredentialFingerprint: api.ObserverCredentialFingerprint(request.Credential),
				MaxFreshness:          time.Duration(request.MaxFreshnessSeconds) * time.Second,
				MaxClockSkew:          time.Duration(request.MaxClockSkewSeconds) * time.Second,
				Measurements:          request.Measurements,
			},
		)
		if err != nil {
			handleObservationError(w, r, "create registration", err)
			return
		}
		RespondJSON(w, mapping.ObserverRegistrationToAPI(*registration))
	}
}

func listObserverRegistrationsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authentication := auth.Authentication.Require(r.Context())
		registrations, err := db.ListObserverRegistrations(
			r.Context(), *authentication.CurrentOrgID(),
		)
		if err != nil {
			handleObservationError(w, r, "list registrations", err)
			return
		}
		RespondJSON(w, mapping.List(registrations, mapping.ObserverRegistrationToAPI))
	}
}

func listObservationsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authentication := auth.Authentication.Require(r.Context())
		observations, err := db.ListObservedComponentStates(
			r.Context(), *authentication.CurrentOrgID(),
		)
		if err != nil {
			handleObservationError(w, r, "list", err)
			return
		}
		RespondJSON(w, mapping.List(observations, mapping.ObservedComponentStateToAPI))
	}
}

func observerCredential(r *http.Request) (string, error) {
	const prefix = "Observer "
	value := r.Header.Get("Authorization")
	if !strings.HasPrefix(value, prefix) || strings.Count(value, " ") != 1 {
		return "", apierrors.ErrUnauthorized
	}
	credential := strings.TrimPrefix(value, prefix)
	if len(credential) < 32 || len(credential) > 512 ||
		strings.ContainsAny(credential, " \t\r\n") {
		return "", apierrors.ErrUnauthorized
	}
	return credential, nil
}

func handleObservationError(
	w http.ResponseWriter,
	r *http.Request,
	action string,
	err error,
) {
	status, message := observationPublicError(err)
	if status == http.StatusInternalServerError {
		internalctx.GetLogger(r.Context()).Error(
			"failed to "+action+" independent observation",
			zap.Error(err),
		)
		sentry.GetHubFromContext(r.Context()).CaptureException(err)
	}
	http.Error(w, message, status)
}

func observationPublicError(err error) (int, string) {
	switch {
	case errors.Is(err, apierrors.ErrUnauthorized),
		errors.Is(err, observation.ErrObserverMismatch),
		errors.Is(err, observation.ErrUntrustedObservation):
		return http.StatusUnauthorized, "independent observer authentication failed"
	case errors.Is(err, apierrors.ErrNotFound):
		return http.StatusNotFound, "observation resource not found"
	case errors.Is(err, apierrors.ErrBadRequest):
		return http.StatusBadRequest, "observation request is invalid"
	case errors.Is(err, apierrors.ErrConflict),
		errors.Is(err, observation.ErrConflictingReplay):
		return http.StatusConflict, "observation conflicts with retained evidence"
	default:
		return http.StatusInternalServerError, "observation operation failed"
	}
}
