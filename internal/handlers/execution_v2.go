package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/executionprotocol"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/middleware"
	"github.com/distr-sh/distr/internal/types"
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
		r.Post("/acknowledge", acknowledgeExecutionV2Handler()).
			With(option.Request(struct {
				attemptPath
				api.ExecutionV2AcknowledgeRequest
			}{})).
			With(option.Response(http.StatusNoContent, nil))
		r.Get("/cancel", getPendingExecutionCancelHandler()).
			With(option.Request(attemptPath{})).
			With(option.Response(http.StatusOK, struct {
				Cancel any `json:"cancel"`
			}{}))
		r.Get("/status-query", getPendingExecutionStatusQueryHandler()).
			With(option.Request(attemptPath{})).
			With(option.Response(http.StatusOK, struct {
				Query any `json:"query"`
			}{}))
		r.Post("/cancel-acknowledgements", acknowledgeExecutionCancelHandler()).
			With(option.Request(struct {
				attemptPath
				api.ExecutionCancelAcknowledgementRequest
			}{})).
			With(option.Response(http.StatusNoContent, nil))
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

func ExecutionV2OperatorRouter(r chiopenapi.Router) {
	r.WithOptions(option.GroupTags("Execution Controls"))
	r.Use(
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
		middleware.RequireReadWriteOrAdmin,
		middleware.BlockSuperAdmin,
		middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyOperatorControlPlaneV2),
		middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyExecutorProtocolV2),
	)
	type executionPath struct {
		ExecutionID uuid.UUID `path:"executionId"`
	}
	r.Route("/{executionId}", func(r chiopenapi.Router) {
		r.Post("/cancel", requestExecutionCancelHandler()).
			With(option.Request(struct {
				executionPath
				api.ExecutionCancelRequest
			}{})).
			With(option.Response(http.StatusNoContent, nil))
		r.Post("/status-queries", requestExecutionStatusHandler()).
			With(option.Request(struct {
				executionPath
				api.ExecutionStatusRequest
			}{})).
			With(option.Response(http.StatusOK, struct {
				Query any `json:"query"`
			}{}))
		r.Post("/reconciliation-events", importExecutionReconciliationHandler()).
			With(option.Request(struct {
				executionPath
				api.ExecutionReconciliationRequest
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
		agent := auth.AgentAuthentication.Require(r.Context())
		orgID := agent.CurrentOrgID()
		attempt, err := db.ClaimExecutionAttempt(r.Context(), request.ToTypes(
			orgID, agent.CurrentDeploymentTargetID(), time.Now().UTC(),
		))
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

func acknowledgeExecutionV2Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		attemptID, ok := executionV2AttemptID(w, r)
		if !ok {
			return
		}
		request, err := JsonBody[api.ExecutionV2AcknowledgeRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		agent := auth.AgentAuthentication.Require(r.Context())
		err = db.AcknowledgeExecutionAttempt(r.Context(), request.ToTypes(
			agent.CurrentOrgID(), agent.CurrentDeploymentTargetID(), attemptID,
		))
		if err != nil {
			respondExecutionV2Error(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
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
		agent := auth.AgentAuthentication.Require(r.Context())
		err = db.HeartbeatExecutionAttempt(r.Context(), request.ToTypes(
			agent.CurrentOrgID(), agent.CurrentDeploymentTargetID(), attemptID, time.Now().UTC(),
		))
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
		agent := auth.AgentAuthentication.Require(r.Context())
		event, err := db.RecordExecutionEvent(r.Context(), request.ToTypes(
			agent.CurrentOrgID(), agent.CurrentDeploymentTargetID(), attemptID,
		))
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
		agent := auth.AgentAuthentication.Require(r.Context())
		err = db.CompleteExecutionAttempt(r.Context(), request.ToTypes(
			agent.CurrentOrgID(), agent.CurrentDeploymentTargetID(), attemptID,
		))
		if err != nil {
			respondExecutionV2Error(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func getPendingExecutionCancelHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		attemptID, ok := executionV2AttemptID(w, r)
		if !ok {
			return
		}
		agent := auth.AgentAuthentication.Require(r.Context())
		cancel, err := db.GetPendingExecutionCancel(
			r.Context(), attemptID, agent.CurrentOrgID(), agent.CurrentDeploymentTargetID(),
		)
		if err != nil {
			respondExecutionV2Error(w, err)
			return
		}
		RespondJSON(w, struct {
			Cancel any `json:"cancel"`
		}{Cancel: cancel})
	}
}

func acknowledgeExecutionCancelHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		attemptID, ok := executionV2AttemptID(w, r)
		if !ok {
			return
		}
		request, err := JsonBody[api.ExecutionCancelAcknowledgementRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		agent := auth.AgentAuthentication.Require(r.Context())
		err = db.RecordCancelAcknowledgement(
			r.Context(), request.ToTypes(
				agent.CurrentOrgID(), agent.CurrentDeploymentTargetID(),
				attemptID, time.Now().UTC(),
			),
		)
		if err != nil {
			respondExecutionV2Error(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func getPendingExecutionStatusQueryHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		attemptID, ok := executionV2AttemptID(w, r)
		if !ok {
			return
		}
		agent := auth.AgentAuthentication.Require(r.Context())
		query, err := db.GetPendingExecutionStatusQuery(
			r.Context(), attemptID, agent.CurrentOrgID(), agent.CurrentDeploymentTargetID(),
		)
		if err != nil {
			respondExecutionV2Error(w, err)
			return
		}
		RespondJSON(w, struct {
			Query any `json:"query"`
		}{Query: query})
	}
}

func requestExecutionCancelHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		executionID, ok := executionV2ExecutionID(w, r)
		if !ok {
			return
		}
		request, err := JsonBody[api.ExecutionCancelRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		err = db.RequestExecutionCancel(r.Context(), request.ToTypes(
			*authInfo.CurrentOrgID(), executionID, authInfo.CurrentUserID(), time.Now().UTC(),
		))
		if err == nil {
			err = executionprotocol.BridgeCampaignCancelIfConfigured(r.Context(), executionID)
		}
		if err != nil {
			respondExecutionV2Error(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func requestExecutionStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		executionID, ok := executionV2ExecutionID(w, r)
		if !ok {
			return
		}
		request, err := JsonBody[api.ExecutionStatusRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		query, err := db.RequestExecutionStatus(r.Context(), request.ToTypes(
			*authInfo.CurrentOrgID(), executionID, authInfo.CurrentUserID(), time.Now().UTC(),
		))
		if err != nil {
			respondExecutionV2Error(w, err)
			return
		}
		RespondJSON(w, struct {
			Query any `json:"query"`
		}{Query: query})
	}
}

func importExecutionReconciliationHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		executionID, ok := executionV2ExecutionID(w, r)
		if !ok {
			return
		}
		request, err := JsonBody[api.ExecutionReconciliationRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		evidence, err := executionprotocol.VerifyImportedReconciliationEvidence(
			r.Context(), request.Evidence,
		)
		if err != nil {
			respondExecutionV2Error(w, apierrors.NewConflict(err.Error()))
			return
		}
		if evidence.OrganizationID != *authInfo.CurrentOrgID() ||
			evidence.ExecutionID != executionID {
			respondExecutionV2Error(w, apierrors.NewConflict("reconciliation evidence scope mismatch"))
			return
		}
		err = db.ImportReconciliationStatus(
			r.Context(), api.ReconciliationEvidenceToTypes(evidence, request.Evidence),
		)
		if err == nil && evidence.RetryRequested {
			err = executionprotocol.BridgeCampaignRetryIfConfigured(
				r.Context(), executionID, types.RetryDispositionAllowed,
			)
		}
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

func executionV2ExecutionID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("executionId"))
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
