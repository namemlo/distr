package migrations

import (
	"context"
	"database/sql"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

type fakeMigrationEngine struct{}

func (*fakeMigrationEngine) Up() error          { return nil }
func (*fakeMigrationEngine) Down() error        { return nil }
func (*fakeMigrationEngine) Migrate(uint) error { return nil }
func (*fakeMigrationEngine) Stop()              {}
func (*fakeMigrationEngine) Close() error       { return nil }

func TestNormalizeMigrationLockTimeout(t *testing.T) {
	g := NewWithT(t)
	normalized, err := normalizeMigrationLockTimeout(0)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(normalized).To(Equal(10 * time.Second))
	normalized, err = normalizeMigrationLockTimeout(275 * time.Millisecond)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(normalized).To(Equal(275 * time.Millisecond))
	_, err = normalizeMigrationLockTimeout(-time.Nanosecond)
	g.Expect(err).To(MatchError("migration lock timeout must be positive"))
}

func TestOpenDoesNotConnectOrConstructMigrator(t *testing.T) {
	g := NewWithT(t)
	runner, err := Open(
		"postgres://postgres@127.0.0.1:1/postgres?sslmode=disable&connect_timeout=1",
		nil,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(runner.engine).To(BeNil())
	g.Expect(runner.Close()).To(Succeed())
}

func TestRunnerRejectsNegativeLockTimeoutBeforeConnection(t *testing.T) {
	g := NewWithT(t)
	var factoryCalls uint64
	runner := &Runner{
		log: zap.NewNop(),
		engineFactory: func(
			*sql.DB,
			*zap.Logger,
			string,
			time.Duration,
		) (migrationEngine, error) {
			factoryCalls++
			return &fakeMigrationEngine{}, nil
		},
	}
	err := runner.Run(context.Background(), RunOptions{
		LockTimeout: -time.Nanosecond,
	})
	g.Expect(err).To(MatchError("migration lock timeout must be positive"))
	g.Expect(factoryCalls).To(Equal(uint64(0)))
}
