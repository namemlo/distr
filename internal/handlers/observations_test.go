package handlers

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestIngestObservationWithDispatchDispatchesCommittedTask(t *testing.T) {
	g := NewWithT(t)
	committed := false
	dispatched := false
	state := &types.ObservedComponentState{}
	task := &types.Task{}

	got, err := ingestObservationWithDispatch(
		context.Background(),
		types.ObservationEnvelope{},
		func(context.Context, types.ObservationEnvelope) (*types.ObservedComponentState, *types.Task, error) {
			committed = true
			return state, task, nil
		},
		func(_ context.Context, gotTask types.Task) error {
			g.Expect(committed).To(BeTrue())
			g.Expect(gotTask).To(Equal(*task))
			dispatched = true
			return nil
		},
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(got).To(BeIdenticalTo(state))
	g.Expect(dispatched).To(BeTrue())
}

func TestIngestObservationWithDispatchDoesNotDispatchExactReplayWithoutTask(t *testing.T) {
	g := NewWithT(t)
	state := &types.ObservedComponentState{}
	dispatchErr := errors.New("dispatch must not run")

	got, err := ingestObservationWithDispatch(
		context.Background(),
		types.ObservationEnvelope{},
		func(context.Context, types.ObservationEnvelope) (*types.ObservedComponentState, *types.Task, error) {
			return state, nil, nil
		},
		func(context.Context, types.Task) error { return dispatchErr },
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(got).To(BeIdenticalTo(state))
}

func TestIngestObservationWithDispatchPropagatesDispatchError(t *testing.T) {
	g := NewWithT(t)
	dispatchErr := errors.New("dispatch failed")
	state := &types.ObservedComponentState{}

	got, err := ingestObservationWithDispatch(
		context.Background(),
		types.ObservationEnvelope{},
		func(context.Context, types.ObservationEnvelope) (*types.ObservedComponentState, *types.Task, error) {
			return state, &types.Task{}, nil
		},
		func(context.Context, types.Task) error { return dispatchErr },
	)

	g.Expect(err).To(MatchError(dispatchErr))
	g.Expect(got).To(BeIdenticalTo(state))
}

func TestObservationCredentialParsingRejectsWrongSchemeAndShortSecret(t *testing.T) {
	g := NewWithT(t)
	request, err := http.NewRequest(http.MethodPost, "/", nil)
	g.Expect(err).NotTo(HaveOccurred())
	request.Header.Set("Authorization", "Bearer observer-secret-at-least-32-characters")
	_, err = observerCredential(request)
	g.Expect(err).To(HaveOccurred())

	request.Header.Set("Authorization", "Observer short")
	_, err = observerCredential(request)
	g.Expect(err).To(HaveOccurred())

	request.Header.Set("Authorization", "Observer observer-secret-at-least-32-characters")
	credential, err := observerCredential(request)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(credential).To(Equal("observer-secret-at-least-32-characters"))
}

func TestObservationRoutesSeparateObserverAuthFromManagementAuth(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("observations.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)
	g.Expect(text).To(ContainSubstring("ObserverIngestRouter"))
	g.Expect(text).To(ContainSubstring("ObserverRegistrationsRouter"))
	g.Expect(text).To(ContainSubstring("ObservationsRouter"))
	g.Expect(text).To(ContainSubstring("middleware.RequireOrgAndRole"))
	g.Expect(text).To(ContainSubstring("featureflags.KeyOperatorControlPlaneV2"))
	g.Expect(text).To(ContainSubstring("middleware.RequireReadWriteOrAdmin"))
	g.Expect(text).To(ContainSubstring("api.ObserverCredentialFingerprint"))
}

func TestObservationPublicErrorDoesNotLeakTrustOrTenantDetails(t *testing.T) {
	g := NewWithT(t)
	status, message := observationPublicError(
		apierrors.NewConflict("foreign tenant observer credential fingerprint"),
	)
	g.Expect(status).To(Equal(http.StatusConflict))
	g.Expect(message).To(Equal("observation conflicts with retained evidence"))
	g.Expect(message).NotTo(ContainSubstring("credential"))
}
