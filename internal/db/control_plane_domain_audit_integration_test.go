package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestReleaseControlPlaneAuditFailureRollsBackDraftCreation(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	organizationID, applicationID, channelID, versionID := createReleaseBundleDependencies(t, ctx)
	bundle := releaseBundleFixture(organizationID, applicationID, channelID, versionID)
	auditFailure := errors.New("control-plane audit unavailable")
	auditCtx := db.WithControlPlaneDomainAuditHook(ctx, db.ControlPlaneAuditAppendHookFunc(
		func(context.Context, types.ControlPlaneAuditEventInput) error {
			return auditFailure
		},
	))

	err := db.CreateReleaseBundle(auditCtx, &bundle)
	g.Expect(err).To(MatchError(auditFailure))
	_, err = db.GetReleaseBundle(ctx, bundle.ID, organizationID)
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
}

func TestReleaseControlPlaneAuditDoesNotDuplicateIdempotentDraftReplay(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	organizationID, applicationID, channelID, versionID := createReleaseBundleDependencies(t, ctx)
	first := releaseBundleFixture(organizationID, applicationID, channelID, versionID)
	var events []types.ControlPlaneAuditEventInput
	auditCtx := db.WithControlPlaneDomainAuditHook(ctx, db.ControlPlaneAuditAppendHookFunc(
		func(_ context.Context, event types.ControlPlaneAuditEventInput) error {
			events = append(events, event)
			return nil
		},
	))

	g.Expect(db.CreateReleaseBundleWithIdempotency(auditCtx, &first, "release-create-1")).To(Succeed())
	replayed := releaseBundleFixture(organizationID, applicationID, channelID, versionID)
	g.Expect(db.CreateReleaseBundleWithIdempotency(auditCtx, &replayed, "release-create-1")).To(Succeed())
	g.Expect(replayed.ID).To(Equal(first.ID))
	g.Expect(events).To(HaveLen(1))
	g.Expect(events[0].EventType).To(Equal("release.draft.created"))
}
