package operatorqueries

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestOperatorCursorRoundTripsWithinTenantCollectionAndCanonicalFilter(t *testing.T) {
	g := NewWithT(t)
	scope := CursorScope{
		OrganizationID: uuid.New(),
		Collection:     types.OperatorCollectionFleet,
		DecisionAt:     time.Now().UTC().Round(time.Microsecond),
		ScopeChecksum:  mustFilterChecksum(t, []string{"environment:visible"}),
		FilterChecksum: mustFilterChecksum(t, struct {
			EnvironmentID uuid.UUID `json:"environmentId"`
		}{EnvironmentID: uuid.New()}),
	}
	tuple := CursorTuple{CreatedAt: time.Now().UTC().Round(time.Microsecond), ID: uuid.New()}

	value, err := EncodeCursor(scope, tuple)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(value).NotTo(ContainSubstring(scope.OrganizationID.String()))

	decoded, err := DecodeCursor(value, scope)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(decoded).To(Equal(&tuple))
}

func TestOperatorCursorRejectsForeignTenantCollectionAndFilter(t *testing.T) {
	checksum := mustFilterChecksum(t, struct {
		Status string `json:"status"`
	}{Status: "running"})
	scope := CursorScope{
		OrganizationID: uuid.New(),
		Collection:     types.OperatorCollectionExecutions,
		DecisionAt:     time.Now().UTC().Round(time.Microsecond),
		ScopeChecksum:  mustFilterChecksum(t, []string{"organization:visible"}),
		FilterChecksum: checksum,
	}
	value, err := EncodeCursor(scope, CursorTuple{
		CreatedAt: time.Now().UTC(),
		ID:        uuid.New(),
	})
	NewWithT(t).Expect(err).NotTo(HaveOccurred())

	otherFilter := scope
	otherFilter.FilterChecksum = mustFilterChecksum(t, struct {
		Status string `json:"status"`
	}{Status: "failed"})
	otherTenant := scope
	otherTenant.OrganizationID = uuid.New()
	otherCollection := scope
	otherCollection.Collection = types.OperatorCollectionAudit
	otherDecisionAt := scope
	otherDecisionAt.DecisionAt = scope.DecisionAt.Add(time.Second)
	otherScope := scope
	otherScope.ScopeChecksum = mustFilterChecksum(t, []string{"environment:other"})

	for name, candidate := range map[string]CursorScope{
		"filter":     otherFilter,
		"tenant":     otherTenant,
		"collection": otherCollection,
		"decisionAt": otherDecisionAt,
		"scope":      otherScope,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeCursor(value, candidate)
			NewWithT(t).Expect(err).To(MatchError(ContainSubstring("cursor is invalid")))
			NewWithT(t).Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())
		})
	}
}

func TestOperatorCursorRejectsMalformedNonCanonicalAndUnknownFields(t *testing.T) {
	scope := CursorScope{
		OrganizationID: uuid.New(),
		Collection:     types.OperatorCollectionReleases,
		DecisionAt:     time.Now().UTC().Round(time.Microsecond),
		ScopeChecksum:  mustFilterChecksum(t, []string{"organization:visible"}),
		FilterChecksum: mustFilterChecksum(t, struct{}{}),
	}
	valid, err := EncodeCursor(scope, CursorTuple{
		CreatedAt: time.Now().UTC(),
		ID:        uuid.New(),
	})
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	payload, err := base64.RawURLEncoding.DecodeString(valid)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())

	for name, value := range map[string]string{
		"not base64url": "%%%",
		"trailing json": base64.RawURLEncoding.EncodeToString(append(payload, byte('{'))),
		"unknown field": base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"s":"x","f":"y","t":"2026-01-01T00:00:00Z","i":"00000000-0000-0000-0000-000000000001","extra":true}`)),
		"too long":      string(make([]byte, MaximumCursorLength+1)),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeCursor(value, scope)
			NewWithT(t).Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())
		})
	}
}

func TestNormalizePageRequestDefaultsAndEnforcesMaximum(t *testing.T) {
	g := NewWithT(t)

	limit, err := NormalizePageRequest(types.PageRequest{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(limit).To(Equal(50))

	limit, err = NormalizePageRequest(types.PageRequest{Limit: 100})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(limit).To(Equal(100))

	for _, invalid := range []int{-1, 101} {
		_, err = NormalizePageRequest(types.PageRequest{Limit: invalid})
		g.Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())
	}
}

func mustFilterChecksum(t *testing.T, filter any) string {
	t.Helper()
	checksum, err := CanonicalFilterChecksum(filter)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return checksum
}
