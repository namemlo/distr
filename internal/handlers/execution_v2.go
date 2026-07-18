package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/middleware"
	"github.com/google/uuid"
	"github.com/oaswrap/spec/adapter/chiopenapi"
	"github.com/oaswrap/spec/option"
)

func ExecutionV2ExecutorRouter(r chiopenapi.Router) {
	r.WithOptions(option.GroupTags("Executor Protocol v2"))
	r.Use(
		middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyOperatorControlPlaneV2),
		middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyExecutorProtocolV2),
	)
	r.Post("/executions/claim", claimExecutionV2Handler()).
		With(option.Description("Claim a signed fenced execution attempt")).
		With(option.Request(api.ExecutionV2ClaimRequest{})).
		With(option.Response(http.StatusOK, api.ExecutionV2AttemptResponse{}))

	type attemptPath struct {
		AttemptID uuid.UUID `path:"attemptId"`
	}
	r.Route("/attempts/{attemptId}", func(r chiopenapi.Router) {
		r.Post("/heartbeat", heartbeatExecutionV2Handler()).
			With(option.Request(struct {
				attemptPath
				api.ExecutionV2HeartbeatRequest
			}{})).
			With(option.Response(http.StatusNoContent, nil))
		r.Post("/events", recordExecutionV2EventHandler()).
			With(option.Request(struct {
				attemptPath
				api.ExecutionV2EventRequest
			}{})).
			With(option.Response(http.StatusOK, struct {
				Event any `json:"event"`
			}{}))
		r.Post("/complete", completeExecutionV2Handler()).
			With(option.Request(struct {
				attemptPath
				api.ExecutionV2CompletionRequest
			}{})).
			With(option.Response(http.StatusNoContent, nil))
	})
}

func claimExecutionV2Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := JsonBody[api.ExecutionV2ClaimRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		orgID := auth.AgentAuthentication.Require(r.Context()).CurrentOrgID()
		attempt, err := db.ClaimExecutionAttempt(r.Context(), request.ToTypes(orgID, time.Now().UTC()))
		if err != nil {
			respondExecutionV2Error(w, err)
			return
		}
		intent, err := db.GetExecutionIntent(r.Context(), attempt.ID, orgID)
		if err != nil {
			respondExecutionV2Error(w, err)
			return
		}
		RespondJSON(w, api.ExecutionV2AttemptResponse{Attempt: *attempt, Intent: intent})
	}
}

func heartbeatExecutionV2Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		attemptID, ok := executionV2AttemptID(w, r)
		if !ok {
			return
		}
		request, err := JsonBody[api.ExecutionV2HeartbeatRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		orgID := auth.AgentAuthentication.Require(r.Context()).CurrentOrgID()
		err = db.HeartbeatExecutionAttempt(r.Context(), request.ToTypes(orgID, attemptID, time.Now().UTC()))
		if err != nil {
			respondExecutionV2Error(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func recordExecutionV2EventHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		attemptID, ok := executionV2AttemptID(w, r)
		if !ok {
			return
		}
		request, err := JsonBody[api.ExecutionV2EventRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		orgID := auth.AgentAuthentication.Require(r.Context()).CurrentOrgID()
		event, err := db.RecordExecutionEvent(r.Context(), request.ToTypes(orgID, attemptID))
		if err != nil {
			respondExecutionV2Error(w, err)
			return
		}
		RespondJSON(w, struct {
			Event any `json:"event"`
		}{Event: event})
	}
}

func completeExecutionV2Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		attemptID, ok := executionV2AttemptID(w, r)
		if !ok {
			return
		}
		request, err := JsonBody[api.ExecutionV2CompletionRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		orgID := auth.AgentAuthentication.Require(r.Context()).CurrentOrgID()
		err = db.CompleteExecutionAttempt(r.Context(), request.ToTypes(orgID, attemptID))
		if err != nil {
			respondExecutionV2Error(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func executionV2AttemptID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("attemptId"))
	if err != nil {
		http.NotFound(w, r)
		return uuid.Nil, false
	}
	return id, true
}

func respondExecutionV2Error(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, apierrors.ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, apierrors.ErrBadRequest):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, apierrors.ErrConflict):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}
