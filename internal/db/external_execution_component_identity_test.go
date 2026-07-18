package db

import (
	"context"
	"errors"
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db/queryable"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	. "github.com/onsi/gomega"
)

func TestResolveObservationComponentInstanceRejectsCrossNamespaceCollision(t *testing.T) {
	g := NewWithT(t)
	database := &componentIdentityQueryable{
		row: componentIdentityRow{
			planSchema:     types.TargetDeploymentPlanSchemaV2,
			candidateCount: 2,
			candidateID:    uuid.NewString(),
		},
	}
	ctx := internalctx.WithDb(context.Background(), database)

	componentInstanceID, err := resolveObservationComponentInstanceID(
		ctx,
		uuid.New(),
		uuid.New(),
		"worker",
	)

	g.Expect(componentInstanceID).To(BeNil())
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
}

func TestResolveObservationComponentInstanceReturnsOnlyExactCandidate(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	deploymentPlanID := uuid.New()
	expectedID := uuid.New()
	database := &componentIdentityQueryable{
		row: componentIdentityRow{
			planSchema:     types.TargetDeploymentPlanSchemaV2,
			candidateCount: 1,
			candidateID:    expectedID.String(),
		},
	}
	ctx := internalctx.WithDb(context.Background(), database)

	componentInstanceID, err := resolveObservationComponentInstanceID(
		ctx,
		organizationID,
		deploymentPlanID,
		"worker",
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(componentInstanceID).To(Equal(&expectedID))
	g.Expect(database.args).To(HaveLen(1))
	g.Expect(database.args[0]).To(Equal(pgx.NamedArgs{
		"organizationId":   organizationID,
		"deploymentPlanId": deploymentPlanID,
		"component":        "worker",
	}))
}

func TestResolveObservationComponentInstanceKeepsLegacyProjectionNullable(t *testing.T) {
	g := NewWithT(t)
	database := &componentIdentityQueryable{
		row: componentIdentityRow{
			planSchema: types.LegacyDeploymentPlanSchemaV1,
		},
	}
	ctx := internalctx.WithDb(context.Background(), database)

	componentInstanceID, err := resolveObservationComponentInstanceID(
		ctx,
		uuid.New(),
		uuid.New(),
		"worker",
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(componentInstanceID).To(BeNil())
}

func TestInsertTargetComponentObservationDoesNotWriteAmbiguousIdentity(t *testing.T) {
	g := NewWithT(t)
	database := &componentIdentityQueryable{
		row: componentIdentityRow{
			planSchema:     types.TargetDeploymentPlanSchemaV2,
			candidateCount: 2,
			candidateID:    uuid.NewString(),
		},
	}
	ctx := internalctx.WithDb(context.Background(), database)

	err := insertTargetComponentObservation(
		ctx,
		types.TargetComponentState{
			OrganizationID: uuid.New(),
			Component:      "worker",
		},
		uuid.New(),
		uuid.New(),
	)

	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
	g.Expect(database.execCalls).To(Equal(0))
}

type componentIdentityQueryable struct {
	queryable.Queryable
	row       pgx.Row
	query     string
	args      []any
	execCalls int
}

func (database *componentIdentityQueryable) QueryRow(
	_ context.Context,
	query string,
	args ...any,
) pgx.Row {
	database.query = query
	database.args = args
	return database.row
}

func (database *componentIdentityQueryable) Exec(
	_ context.Context,
	_ string,
	_ ...any,
) (pgconn.CommandTag, error) {
	database.execCalls++
	return pgconn.CommandTag{}, nil
}

type componentIdentityRow struct {
	planSchema     string
	candidateCount int
	candidateID    string
	err            error
}

func (row componentIdentityRow) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(destinations) != 3 {
		return errors.New("unexpected component identity scan width")
	}
	*destinations[0].(*string) = row.planSchema
	*destinations[1].(*int) = row.candidateCount
	*destinations[2].(*string) = row.candidateID
	return nil
}
