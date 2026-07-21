package executionruntime

import (
	"context"
	"net/http"

	"github.com/distr-sh/distr/internal/executionprotocol"
	"github.com/distr-sh/distr/internal/executionworker"
)

// Dependencies are the compile-safe boundary between the executor protocol
// and the governance domains integrated by the numbered predecessor slices.
// Missing dependencies remain fail-closed in the owning handlers.
type Dependencies struct {
	ProtocolDispatcher             *executionworker.ProtocolDispatcher
	ReconciliationEvidenceVerifier executionprotocol.ReconciliationEvidenceVerifier
	ReconciliationObserverGate     executionprotocol.ReconciliationObserverGate
	CampaignControlCoordinator     *executionprotocol.CampaignControlCoordinator
}

func (d Dependencies) Inject(ctx context.Context) context.Context {
	if d.ProtocolDispatcher != nil {
		ctx = executionworker.WithProtocolDispatcher(ctx, d.ProtocolDispatcher)
	}
	if d.ReconciliationEvidenceVerifier != nil {
		ctx = executionprotocol.WithReconciliationEvidenceVerifier(
			ctx, d.ReconciliationEvidenceVerifier,
		)
	}
	if d.ReconciliationObserverGate != nil {
		ctx = executionprotocol.WithReconciliationObserverGate(ctx, d.ReconciliationObserverGate)
	}
	if d.CampaignControlCoordinator != nil {
		ctx = executionprotocol.WithCampaignControlCoordinator(ctx, d.CampaignControlCoordinator)
	}
	return ctx
}

func ContextMiddleware(d Dependencies) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(d.Inject(r.Context())))
		})
	}
}
