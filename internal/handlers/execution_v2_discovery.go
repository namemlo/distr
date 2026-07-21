package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
)

type leaseExecutionV2AttemptFunc func(
	context.Context,
	types.LeaseExecutionV2Request,
) (*types.ExecutionV2Lease, error)

func leaseExecutionV2Handler() http.HandlerFunc {
	return leaseExecutionV2HandlerWith(db.LeaseExecutionV2Attempt)
}

func leaseExecutionV2HandlerWith(lease leaseExecutionV2AttemptFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := JsonBody[api.ExecutionV2LeaseRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		credential := auth.AgentAuthentication.Require(r.Context())
		leased, err := lease(r.Context(), request.ToTypes(
			credential.CurrentOrgID(), credential.CurrentDeploymentTargetID(), time.Now().UTC(),
		))
		if err != nil {
			respondExecutionV2Error(w, err)
			return
		}
		if leased == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		RespondJSON(w, api.ExecutionV2LeaseResponse{
			Attempt: leased.Attempt,
			Intent:  leased.Intent,
		})
	}
}
