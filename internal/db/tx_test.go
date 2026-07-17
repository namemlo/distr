package db_test

import (
	"context"
	"testing"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/jackc/pgx/v5"
	. "github.com/onsi/gomega"
)

func TestRunReadOnlyTxRRUsesRepeatableReadAndRejectsWrites(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	err := db.RunReadOnlyTxRR(ctx, func(ctx context.Context) error {
		database := internalctx.GetDb(ctx)
		var isolation, readOnly string
		g.Expect(database.QueryRow(ctx, `SHOW transaction_isolation`).
			Scan(&isolation)).To(Succeed())
		g.Expect(database.QueryRow(ctx, `SHOW transaction_read_only`).
			Scan(&readOnly)).To(Succeed())
		g.Expect(isolation).To(Equal("repeatable read"))
		g.Expect(readOnly).To(Equal("on"))
		_, writeErr := database.Exec(ctx,
			`CREATE TABLE timestamp_read_only_write_must_fail (id integer)`)
		return writeErr
	})
	g.Expect(err).To(MatchError(ContainSubstring("read-only transaction")))
}

func TestRunReadOnlyTxRRRejectsNestedTransactions(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	callbackCalled := false
	err := db.RunTxRR(ctx, func(ctx context.Context) error {
		return db.RunReadOnlyTxRR(ctx, func(context.Context) error {
			callbackCalled = true
			return nil
		})
	})
	g.Expect(err).To(MatchError(ContainSubstring(
		"can not be called from within an existing transaction",
	)))
	g.Expect(callbackCalled).To(BeFalse())
}

func TestRunTxIsoPreservesRequestedIsolation(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	var isolation string
	err := db.RunTxIso(ctx, pgx.Serializable, func(ctx context.Context) error {
		return internalctx.GetDb(ctx).QueryRow(
			ctx, `SHOW transaction_isolation`,
		).Scan(&isolation)
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(isolation).To(Equal("serializable"))
}
