package migrations

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"embed"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/distr-sh/distr/internal/env"
	"github.com/distr-sh/distr/internal/externalexecutiontimestamp"
	"github.com/distr-sh/distr/internal/types"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/multierr"
	"go.uber.org/zap"
)

//go:embed sql/*
var fs embed.FS

type Logger struct {
	*zap.SugaredLogger
}

// Printf implements migrate.Logger.
func (l *Logger) Printf(format string, v ...interface{}) {
	if strings.HasPrefix(format, "error") {
		l.Errorf(strings.TrimSpace(format), v...)
	} else {
		l.Debugf(strings.TrimSpace(format), v...)
	}
}

// Verbose implements migrate.Logger.
func (l *Logger) Verbose() bool {
	return l.Level() == zap.DebugLevel
}

var _ migrate.Logger = &Logger{}

type SchemaStatus struct {
	Version int
	Dirty   bool
}

type RunOptions struct {
	Down           bool
	Target         *uint
	CheckOnly      bool
	ExpandManifest *types.ExternalExecutionTimestampManifest
	LockTimeout    time.Duration
}

const DefaultMigrationLockTimeout = 10 * time.Second

const migrationStatementTimeout = 5 * time.Minute

type migrationEngine interface {
	Up() error
	Down() error
	Migrate(uint) error
	Stop()
	Close() error
}

type migrationEngineFactory func(
	*sql.DB,
	*zap.Logger,
	string,
	time.Duration,
) (migrationEngine, error)

type golangMigrationEngine struct {
	instance *migrate.Migrate
}

func (engine *golangMigrationEngine) Up() error {
	return engine.instance.Up()
}

func (engine *golangMigrationEngine) Down() error {
	return engine.instance.Down()
}

func (engine *golangMigrationEngine) Migrate(version uint) error {
	return engine.instance.Migrate(version)
}

func (engine *golangMigrationEngine) Stop() {
	select {
	case engine.instance.GracefulStop <- true:
	default:
	}
}

func (engine *golangMigrationEngine) Close() error {
	sourceErr, databaseErr := engine.instance.Close()
	return errors.Join(sourceErr, databaseErr)
}

type Runner struct {
	db                         *sql.DB
	log                        *zap.Logger
	engineFactory              migrationEngineFactory
	recoveryForceEngineFactory timestampDirtyRecoveryForceEngineFactory
	engine                     migrationEngine
	closeOnce                  sync.Once
	closeErr                   error
}

func Open(databaseURL string, log *zap.Logger) (*Runner, error) {
	database, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(4)
	database.SetMaxIdleConns(4)
	if log == nil {
		log = zap.NewNop()
	}
	return &Runner{
		db:                         database,
		log:                        log,
		engineFactory:              newMigrationEngine,
		recoveryForceEngineFactory: newTimestampDirtyRecoveryForceEngine,
	}, nil
}

func (r *Runner) Close() error {
	r.closeOnce.Do(func() {
		var engineErr error
		if r.engine != nil {
			engineErr = r.engine.Close()
		}
		r.closeErr = errors.Join(engineErr, r.db.Close())
	})
	return r.closeErr
}

func (r *Runner) Status(ctx context.Context) (status SchemaStatus, finalErr error) {
	connection, err := r.db.Conn(ctx)
	if err != nil {
		return SchemaStatus{}, fmt.Errorf("acquire status database connection: %w", err)
	}
	defer func() {
		if err := connection.Close(); err != nil {
			finalErr = errors.Join(
				finalErr,
				fmt.Errorf("release status database connection: %w", err),
			)
		}
	}()
	transaction, err := connection.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	if err != nil {
		return SchemaStatus{}, fmt.Errorf("begin read-only schema status: %w", err)
	}
	transactionOpen := true
	defer func() {
		if transactionOpen {
			_ = transaction.Rollback()
		}
	}()
	status, err = readSchemaStatus(ctx, transaction)
	if err != nil {
		return SchemaStatus{}, err
	}
	if err := transaction.Commit(); err != nil {
		return SchemaStatus{}, fmt.Errorf("commit read-only schema status: %w", err)
	}
	transactionOpen = false
	return status, nil
}

func readSchemaStatus(
	ctx context.Context,
	database migrationQueryer,
) (SchemaStatus, error) {
	var exists bool
	if err := database.QueryRowContext(ctx, `
SELECT to_regclass(format('%I.schema_migrations', current_schema()))
       IS NOT NULL`).Scan(&exists); err != nil {
		return SchemaStatus{}, fmt.Errorf("locate schema_migrations: %w", err)
	}
	if !exists {
		return SchemaStatus{Version: -1}, nil
	}
	var relationKind string
	if err := database.QueryRowContext(ctx, `
SELECT relation.relkind::text
FROM pg_catalog.pg_class relation
WHERE relation.oid =
  to_regclass(format('%I.schema_migrations', current_schema()))`).Scan(
		&relationKind,
	); err != nil {
		return SchemaStatus{}, fmt.Errorf(
			"read schema_migrations relation kind: %w", err,
		)
	}
	if relationKind != "r" {
		return SchemaStatus{}, errors.New(
			"schema_migrations must be an ordinary table",
		)
	}

	var columnCount, matchingColumnCount int
	if err := database.QueryRowContext(ctx, `
SELECT
  count(*),
  count(*) FILTER (WHERE
    (column_name = 'version' AND data_type = 'bigint' AND is_nullable = 'NO')
    OR
    (column_name = 'dirty' AND data_type = 'boolean' AND is_nullable = 'NO')
  )
FROM information_schema.columns
WHERE table_schema = current_schema()
  AND table_name = 'schema_migrations'`).Scan(
		&columnCount,
		&matchingColumnCount,
	); err != nil {
		return SchemaStatus{}, fmt.Errorf("read schema_migrations catalog: %w", err)
	}
	if columnCount != 2 || matchingColumnCount != 2 {
		return SchemaStatus{}, errors.New(
			"schema_migrations catalog must contain exactly version BIGINT NOT NULL and dirty BOOLEAN NOT NULL",
		)
	}
	var primaryKeyCount int
	if err := database.QueryRowContext(ctx, `
SELECT count(*)
FROM pg_catalog.pg_constraint constraint_row
WHERE constraint_row.conrelid =
      to_regclass(format('%I.schema_migrations', current_schema()))
  AND constraint_row.contype = 'p'
  AND constraint_row.convalidated
  AND pg_get_constraintdef(constraint_row.oid) = 'PRIMARY KEY (version)'`).Scan(
		&primaryKeyCount,
	); err != nil {
		return SchemaStatus{}, fmt.Errorf(
			"read schema_migrations primary key: %w", err,
		)
	}
	if primaryKeyCount != 1 {
		return SchemaStatus{}, errors.New(
			"schema_migrations must have exactly one primary key on version",
		)
	}

	var rowCount int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(
		&rowCount,
	); err != nil {
		return SchemaStatus{}, fmt.Errorf("count schema_migrations: %w", err)
	}
	if rowCount == 0 {
		return SchemaStatus{Version: -1}, nil
	}
	if rowCount != 1 {
		return SchemaStatus{}, fmt.Errorf(
			"schema_migrations must contain at most one row; found %d", rowCount,
		)
	}
	var status SchemaStatus
	if err := database.QueryRowContext(
		ctx,
		`SELECT version, dirty FROM schema_migrations`,
	).Scan(&status.Version, &status.Dirty); err != nil {
		return SchemaStatus{}, fmt.Errorf("read schema_migrations row: %w", err)
	}
	if status.Version < 0 {
		return SchemaStatus{}, fmt.Errorf(
			"schema_migrations version must be non-negative; found %d",
			status.Version,
		)
	}
	return status, nil
}

//nolint:gocyclo // Migration locking, execution, and fail-closed cleanup form one explicit state machine.
func (r *Runner) Run(ctx context.Context, options RunOptions) (finalErr error) {
	lockTimeout, err := normalizeMigrationLockTimeout(options.LockTimeout)
	if err != nil {
		return err
	}
	connection, err := r.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration database connection: %w", err)
	}
	locked, uncertain, err := acquireMigrationAdvisoryLock(
		ctx, connection, lockTimeout,
	)
	if err != nil {
		if uncertain {
			err = errors.Join(err, discardMigrationConnection(connection))
		} else {
			err = errors.Join(err, connection.Close())
		}
		return err
	}
	if !locked {
		_ = connection.Close()
		return errors.New("timestamp migration advisory lock was not acquired")
	}
	defer func() {
		unlockContext, cancel := context.WithTimeout(
			context.WithoutCancel(ctx), DefaultMigrationLockTimeout,
		)
		defer cancel()
		var unlocked bool
		unlockErr := connection.QueryRowContext(
			unlockContext,
			`SELECT pg_advisory_unlock($1)`,
			externalexecutiontimestamp.MigrationAdvisoryLockKey,
		).Scan(&unlocked)
		if unlockErr != nil || !unlocked {
			r.log.Error(
				"failed to release timestamp migration advisory lock",
				zap.Error(unlockErr),
				zap.Bool("unlocked", unlocked),
			)
			discardErr := discardMigrationConnection(connection)
			finalErr = errors.Join(
				finalErr,
				errors.New("timestamp migration advisory lock release is uncertain"),
				discardErr,
			)
			return
		}
		if closeErr := connection.Close(); closeErr != nil {
			finalErr = errors.Join(
				finalErr,
				fmt.Errorf("release migration database connection: %w", closeErr),
			)
		}
	}()

	transaction, err := connection.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	if err != nil {
		return fmt.Errorf("begin read-only migration preflight: %w", err)
	}
	transactionOpen := true
	defer func() {
		if transactionOpen {
			_ = transaction.Rollback()
		}
	}()
	status, err := readSchemaStatus(ctx, transaction)
	if err != nil {
		return err
	}
	latest, err := latestMigrationVersion()
	if err != nil {
		return err
	}
	if err := externalExecutionTimestampPreflight(
		ctx, transaction, status, options, latest,
	); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit read-only migration preflight: %w", err)
	}
	transactionOpen = false
	if options.CheckOnly {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var schema string
	if err := connection.QueryRowContext(ctx, `SELECT current_schema()`).Scan(
		&schema,
	); err != nil {
		return fmt.Errorf("read current migration schema: %w", err)
	}
	if strings.TrimSpace(schema) == "" {
		return errors.New("current migration schema is empty")
	}
	if r.engine != nil {
		return errors.New("migration runner supports one mutating run")
	}
	engine, err := r.engineFactory(r.db, r.log, schema, lockTimeout)
	if err != nil {
		return fmt.Errorf("construct migration engine: %w", err)
	}
	r.engine = engine
	finished := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-ctx.Done():
			engine.Stop()
		case <-finished:
		}
	}()
	var migrationErr error
	switch {
	case options.Down:
		migrationErr = engine.Down()
	case options.Target != nil:
		migrationErr = engine.Migrate(*options.Target)
	default:
		migrationErr = engine.Up()
	}
	close(finished)
	<-watcherDone
	if migrationErr != nil &&
		status.Version >= int(externalExecutionTimestampExpandVersion) &&
		desiredVersion(options, latest) < externalExecutionTimestampExpandVersion &&
		timestampDowngradeGuardRejected(migrationErr) {
		repairContext, repairCancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			DefaultMigrationLockTimeout,
		)
		repairErr := restoreTimestampDowngradeGuardStatus(
			repairContext,
			connection,
		)
		repairCancel()
		if repairErr != nil {
			return errors.Join(
				migrationErr,
				fmt.Errorf(
					"restore schema status after guarded timestamp downgrade: %w",
					repairErr,
				),
			)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if migrationErr != nil && !errors.Is(migrationErr, migrate.ErrNoChange) {
		return migrationErr
	}
	if errors.Is(migrationErr, migrate.ErrNoChange) {
		r.log.Info("migrations completed", zap.Error(migrationErr))
	}
	return nil
}

func timestampDowngradeGuardRejected(err error) bool {
	for _, message := range []string{
		"downgrade crossing 138 is forbidden after timestamp manifest application",
		"downgrade crossing 138 is forbidden after timestamp retention",
		"downgrade crossing 138 is forbidden after ZERO_HISTORY timestamp rows",
	} {
		if strings.Contains(err.Error(), message) {
			return true
		}
	}
	return false
}

func restoreTimestampDowngradeGuardStatus(
	ctx context.Context,
	connection *sql.Conn,
) (finalErr error) {
	transaction, err := connection.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	open := true
	defer func() {
		if open {
			finalErr = errors.Join(finalErr, transaction.Rollback())
		}
	}()
	var version int
	var dirty bool
	if err := transaction.QueryRowContext(ctx, `
SELECT version, dirty
FROM schema_migrations
FOR UPDATE`).Scan(&version, &dirty); err != nil {
		return fmt.Errorf("read guarded migration status: %w", err)
	}
	if version != 137 || !dirty {
		return fmt.Errorf(
			"guarded migration status is version %d dirty=%t, expected version 137 dirty=true",
			version,
			dirty,
		)
	}
	var retainedTableCount, instantColumnCount int
	if err := transaction.QueryRowContext(ctx, `
SELECT
  (
    SELECT count(*)
    FROM pg_catalog.pg_class relation
    JOIN pg_catalog.pg_namespace namespace
      ON namespace.oid=relation.relnamespace
    WHERE namespace.nspname=current_schema()
      AND relation.relkind='r'
      AND relation.relname IN (
        'externalexecutiontimestampmanifest',
        'externalexecutiontimestampcellprovenance',
        'externalexecutiontimestampdeletiontombstone',
        'externalexecutiontimestampexpandstate',
        'externalexecutiontimestampcontractgate'
      )
  ),
  (
    SELECT count(*)
    FROM information_schema.columns
    WHERE table_schema=current_schema()
      AND (
        (
          table_name='externalexecution'
          AND column_name IN (
            'created_at_instant',
            'updated_at_instant',
            'started_at_instant',
            'completed_at_instant',
            'callback_deadline_at_instant'
          )
        )
        OR (
          table_name='externalexecutionevent'
          AND column_name='created_at_instant'
        )
      )
  )`).Scan(&retainedTableCount, &instantColumnCount); err != nil {
		return fmt.Errorf("inspect guarded timestamp schema: %w", err)
	}
	if retainedTableCount != 5 || instantColumnCount != 6 {
		return fmt.Errorf(
			"guarded timestamp schema is incomplete: tables=%d instant_columns=%d",
			retainedTableCount,
			instantColumnCount,
		)
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE schema_migrations
SET version=138, dirty=FALSE
WHERE version=137 AND dirty=TRUE`)
	if err != nil {
		return fmt.Errorf("restore guarded migration status: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read restored migration status count: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf(
			"restore guarded migration status affected %d rows",
			affected,
		)
	}
	if err := transaction.Commit(); err != nil {
		return err
	}
	open = false
	return nil
}

func normalizeMigrationLockTimeout(value time.Duration) (time.Duration, error) {
	if value == 0 {
		return DefaultMigrationLockTimeout, nil
	}
	if value < 0 {
		return 0, errors.New("migration lock timeout must be positive")
	}
	return value, nil
}

func acquireMigrationAdvisoryLock(
	ctx context.Context,
	connection *sql.Conn,
	timeout time.Duration,
) (locked bool, uncertain bool, err error) {
	lockContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		var acquired bool
		if err := connection.QueryRowContext(
			lockContext,
			`SELECT pg_try_advisory_lock($1)`,
			externalexecutiontimestamp.MigrationAdvisoryLockKey,
		).Scan(&acquired); err != nil {
			return false, true, migrationAdvisoryLockWaitError(
				ctx, lockContext, timeout, err,
			)
		}
		if acquired {
			return true, false, nil
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-lockContext.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return false, false, migrationAdvisoryLockWaitError(
				ctx, lockContext, timeout, lockContext.Err(),
			)
		case <-timer.C:
		}
	}
}

func migrationAdvisoryLockWaitError(
	ctx context.Context,
	lockContext context.Context,
	timeout time.Duration,
	cause error,
) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(lockContext.Err(), context.DeadlineExceeded) {
		return fmt.Errorf(
			"timestamp migration advisory lock timeout after %s", timeout,
		)
	}
	return fmt.Errorf("acquire timestamp migration advisory lock: %w", cause)
}

func discardMigrationConnection(connection *sql.Conn) error {
	rawErr := connection.Raw(func(any) error { return driver.ErrBadConn })
	if errors.Is(rawErr, driver.ErrBadConn) {
		rawErr = nil
	}
	closeErr := connection.Close()
	if errors.Is(closeErr, sql.ErrConnDone) {
		closeErr = nil
	}
	return errors.Join(rawErr, closeErr)
}

func newMigrationEngine(
	database *sql.DB,
	log *zap.Logger,
	schema string,
	lockTimeout time.Duration,
) (migrationEngine, error) {
	databaseDriver, err := postgres.WithInstance(database, &postgres.Config{
		SchemaName:       schema,
		StatementTimeout: migrationStatementTimeout,
	})
	if err != nil {
		return nil, err
	}
	sourceDriver, err := iofs.New(fs, "sql")
	if err != nil {
		return nil, err
	}
	instance, err := migrate.NewWithInstance(
		"", sourceDriver, "distr", databaseDriver,
	)
	if err != nil {
		return nil, err
	}
	instance.LockTimeout = lockTimeout
	instance.Log = &Logger{log.Sugar()}
	return &golangMigrationEngine{instance: instance}, nil
}

func Up(ctx context.Context, log *zap.Logger) error {
	return runCompatibilityMigration(ctx, log, RunOptions{})
}

func Down(ctx context.Context, log *zap.Logger) error {
	return runCompatibilityMigration(ctx, log, RunOptions{Down: true})
}

func Migrate(ctx context.Context, log *zap.Logger, to uint) error {
	return runCompatibilityMigration(ctx, log, RunOptions{Target: &to})
}

func runCompatibilityMigration(
	ctx context.Context,
	log *zap.Logger,
	options RunOptions,
) (err error) {
	runner, err := Open(env.DatabaseUrl(), log)
	if err != nil {
		return err
	}
	defer func() { multierr.AppendInto(&err, runner.Close()) }()
	return runner.Run(ctx, options)
}
