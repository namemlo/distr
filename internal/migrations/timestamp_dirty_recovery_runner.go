package migrations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	internaldb "github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/externalexecutiontimestamp"
	"github.com/distr-sh/distr/internal/types"
	migratepkg "github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"
)

var timestampDirtyRecoveryChecksumPattern = regexp.MustCompile(
	`^sha256:[0-9a-f]{64}$`,
)

type TimestampDirtyRecoveryPlanOptions struct {
	ExpectedDirtyVersion  uint
	OperatorIdentity      string
	Reason                string
	WriterFenceIdentifier string
	Manifest              *types.ExternalExecutionTimestampManifest
	// ManifestDocumentChecksum is SHA-256 of the canonical two-space-indented
	// JSON manifest plus its terminating newline.
	ManifestDocumentChecksum string
	LockTimeout              time.Duration
}

type TimestampDirtyRecoveryApplyOptions struct {
	// PlanDocumentChecksum is SHA-256 of the canonical two-space-indented JSON
	// plan plus its terminating newline.
	PlanDocumentChecksum  string
	WriterFenceIdentifier string
	Manifest              *types.ExternalExecutionTimestampManifest
	// ManifestDocumentChecksum follows the same canonical JSON convention.
	ManifestDocumentChecksum string
	LockTimeout              time.Duration
}

type timestampDirtyRecoveryForceEngine interface {
	Force(int) error
	Close() error
	Discard() error
}

type timestampDirtyRecoveryForceEngineFactory func(
	context.Context,
	*sql.DB,
	*zap.Logger,
	string,
	time.Duration,
) (timestampDirtyRecoveryForceEngine, error)

type golangTimestampDirtyRecoveryForceEngine struct {
	instance    *migratepkg.Migrate
	connection  *sql.Conn
	cleanupOnce sync.Once
	cleanupErr  error
}

func (engine *golangTimestampDirtyRecoveryForceEngine) Force(version int) error {
	return engine.instance.Force(version)
}

func (engine *golangTimestampDirtyRecoveryForceEngine) Close() error {
	return engine.Discard()
}

func (engine *golangTimestampDirtyRecoveryForceEngine) Discard() error {
	engine.cleanupOnce.Do(func() {
		rawErr := engine.connection.Raw(func(any) error {
			return driver.ErrBadConn
		})
		if errors.Is(rawErr, driver.ErrBadConn) ||
			errors.Is(rawErr, sql.ErrConnDone) {
			rawErr = nil
		}
		sourceErr, databaseErr := engine.instance.Close()
		if databaseErr != nil &&
			(errors.Is(databaseErr, sql.ErrConnDone) ||
				strings.Contains(
					databaseErr.Error(),
					"sql: connection is already closed",
				)) {
			databaseErr = nil
		}
		engine.cleanupErr = errors.Join(rawErr, sourceErr, databaseErr)
	})
	return engine.cleanupErr
}

func newTimestampDirtyRecoveryForceEngine(
	ctx context.Context,
	database *sql.DB,
	log *zap.Logger,
	schema string,
	timeout time.Duration,
) (
	engine timestampDirtyRecoveryForceEngine,
	finalErr error,
) {
	if timeout > migrationStatementTimeout {
		timeout = migrationStatementTimeout
	}
	if deadline, hasDeadline := ctx.Deadline(); hasDeadline {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, context.DeadlineExceeded
		}
		if remaining < timeout {
			timeout = remaining
		}
	}
	connection, err := database.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"acquire timestamp dirty recovery force connection: %w",
			err,
		)
	}
	handoff := false
	sessionBound := false
	defer func() {
		if handoff {
			return
		}
		if sessionBound {
			finalErr = errors.Join(
				finalErr,
				discardMigrationConnection(connection),
			)
			return
		}
		finalErr = errors.Join(finalErr, connection.Close())
	}()
	timeoutMilliseconds := timeout.Milliseconds()
	if timeoutMilliseconds < 1 {
		timeoutMilliseconds = 1
	}
	timeoutSetting := fmt.Sprintf("%dms", timeoutMilliseconds)
	sessionBound = true
	if _, err := connection.ExecContext(ctx, `
SELECT
  set_config('lock_timeout', $1, false),
  set_config('statement_timeout', $1, false)`, timeoutSetting); err != nil {
		return nil, fmt.Errorf(
			"bound timestamp dirty recovery force session: %w",
			err,
		)
	}
	databaseDriver, err := postgres.WithConnection(
		ctx,
		connection,
		&postgres.Config{
			SchemaName:       schema,
			StatementTimeout: timeout,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"construct timestamp dirty recovery database driver: %w",
			err,
		)
	}
	sourceDriver, err := iofs.New(fs, "sql")
	if err != nil {
		return nil, err
	}
	instance, err := migratepkg.NewWithInstance(
		"",
		sourceDriver,
		"distr-timestamp-dirty-recovery",
		databaseDriver,
	)
	if err != nil {
		return nil, errors.Join(err, sourceDriver.Close())
	}
	instance.LockTimeout = timeout
	instance.Log = &Logger{log.Sugar()}
	handoff = true
	return &golangTimestampDirtyRecoveryForceEngine{
		instance:   instance,
		connection: connection,
	}, nil
}

func (r *Runner) PlanTimestampDirtyRecovery(
	ctx context.Context,
	options TimestampDirtyRecoveryPlanOptions,
) (plan types.TimestampDirtyRecoveryPlan, finalErr error) {
	defer func() {
		if finalErr != nil {
			plan = types.TimestampDirtyRecoveryPlan{}
		}
	}()
	if options.ExpectedDirtyVersion != 137 &&
		options.ExpectedDirtyVersion != 138 {
		return types.TimestampDirtyRecoveryPlan{}, errors.New(
			"timestamp dirty recovery expected dirty version must be 137 or 138",
		)
	}
	lockTimeout, err := normalizeMigrationLockTimeout(options.LockTimeout)
	if err != nil {
		return types.TimestampDirtyRecoveryPlan{}, err
	}
	err = r.withTimestampDirtyRecoveryLock(
		ctx,
		lockTimeout,
		func(connection *sql.Conn) error {
			return withTimestampDirtyRecoveryPGXConnection(
				connection,
				func(connection *pgx.Conn) error {
					tx, err := connection.BeginTx(
						ctx,
						pgx.TxOptions{
							IsoLevel:   pgx.RepeatableRead,
							AccessMode: pgx.ReadOnly,
						},
					)
					if err != nil {
						return fmt.Errorf(
							"begin timestamp dirty recovery plan: %w",
							err,
						)
					}
					open := true
					defer func() {
						if open {
							rollbackTimestampDirtyRecoveryTx(ctx, tx)
						}
					}()
					if err := setTimestampDirtyRecoveryLocalTimeouts(
						ctx,
						tx,
						lockTimeout,
					); err != nil {
						return err
					}
					status, err := readTimestampDirtyRecoverySchemaStatus(ctx, tx)
					if err != nil {
						return err
					}
					if status != (SchemaStatus{
						Version: int(options.ExpectedDirtyVersion),
						Dirty:   true,
					}) {
						return fmt.Errorf(
							"timestamp dirty recovery expected schema version %d dirty=true; observed version %d dirty=%t",
							options.ExpectedDirtyVersion,
							status.Version,
							status.Dirty,
						)
					}
					catalog, err := classifyTimestampDirtyRecoveryCatalog(ctx, tx)
					if err != nil {
						return err
					}
					forceVersion, err := ClassifyTimestampDirtyRecovery(
						status,
						catalog.Shape,
					)
					if err != nil {
						return err
					}
					manifestBinding, err := validateTimestampDirtyRecoveryEvidence(
						ctx,
						tx,
						catalog,
						options.Manifest,
						options.ManifestDocumentChecksum,
					)
					if err != nil {
						return err
					}
					var createdAt time.Time
					if err := tx.QueryRow(
						ctx,
						`SELECT clock_timestamp()`,
					).Scan(&createdAt); err != nil {
						return fmt.Errorf(
							"read timestamp dirty recovery plan time: %w",
							err,
						)
					}
					plan = types.TimestampDirtyRecoveryPlan{
						FormatVersion:         types.TimestampDirtyRecoveryFormatVersion,
						RecordType:            types.TimestampDirtyRecoveryRecordTypePlan,
						RecoveryID:            uuid.New(),
						CreatedAt:             createdAt.UTC(),
						OperatorIdentity:      options.OperatorIdentity,
						Reason:                options.Reason,
						WriterFenceIdentifier: options.WriterFenceIdentifier,
						ExpectedDirtyVersion:  options.ExpectedDirtyVersion,
						CatalogShape:          catalog.Shape,
						ForceVersion:          forceVersion,
						CatalogChecksum:       catalog.Checksum,
						Manifest:              manifestBinding,
					}
					if err := plan.Validate(); err != nil {
						return err
					}
					if err := tx.Commit(ctx); err != nil {
						return fmt.Errorf(
							"commit timestamp dirty recovery plan: %w",
							err,
						)
					}
					open = false
					return nil
				},
			)
		},
	)
	if err != nil {
		return types.TimestampDirtyRecoveryPlan{}, err
	}
	return plan, nil
}

//nolint:gocyclo // Apply is an audited transaction state machine with explicit fail-closed branches.
func (r *Runner) ApplyTimestampDirtyRecovery(
	ctx context.Context,
	plan types.TimestampDirtyRecoveryPlan,
	options TimestampDirtyRecoveryApplyOptions,
) (result types.TimestampDirtyRecoveryResult, finalErr error) {
	defer func() {
		if finalErr != nil {
			result = types.TimestampDirtyRecoveryResult{}
		}
	}()
	if err := plan.Validate(); err != nil {
		return types.TimestampDirtyRecoveryResult{}, err
	}
	if !timestampDirtyRecoveryChecksumPattern.MatchString(
		options.PlanDocumentChecksum,
	) {
		return types.TimestampDirtyRecoveryResult{}, errors.New(
			"timestamp dirty recovery plan document checksum must use lowercase sha256 format",
		)
	}
	actualPlanChecksum, err := computeTimestampDirtyRecoveryDocumentChecksum(plan)
	if err != nil {
		return types.TimestampDirtyRecoveryResult{}, err
	}
	if options.PlanDocumentChecksum != actualPlanChecksum {
		return types.TimestampDirtyRecoveryResult{}, errors.New(
			"timestamp dirty recovery plan document checksum does not match the canonical plan",
		)
	}
	if options.WriterFenceIdentifier != plan.WriterFenceIdentifier {
		return types.TimestampDirtyRecoveryResult{}, errors.New(
			"timestamp dirty recovery writer fence does not match the plan",
		)
	}
	suppliedManifestBinding, err := timestampDirtyRecoveryManifestBinding(
		options.Manifest,
		options.ManifestDocumentChecksum,
	)
	if err != nil {
		return types.TimestampDirtyRecoveryResult{}, err
	}
	if !timestampDirtyRecoveryManifestBindingsEqual(
		suppliedManifestBinding,
		plan.Manifest,
	) {
		return types.TimestampDirtyRecoveryResult{}, errors.New(
			"timestamp dirty recovery manifest does not match the plan",
		)
	}
	lockTimeout, err := normalizeMigrationLockTimeout(options.LockTimeout)
	if err != nil {
		return types.TimestampDirtyRecoveryResult{}, err
	}
	forceAttempted := false
	forceSucceeded := false
	err = r.withTimestampDirtyRecoveryLock(
		ctx,
		lockTimeout,
		func(connection *sql.Conn) error {
			return withTimestampDirtyRecoveryPGXConnection(
				connection,
				func(connection *pgx.Conn) error {
					tx, err := connection.BeginTx(
						ctx,
						pgx.TxOptions{
							IsoLevel:   pgx.ReadCommitted,
							AccessMode: pgx.ReadWrite,
						},
					)
					if err != nil {
						return fmt.Errorf(
							"begin timestamp dirty recovery apply: %w",
							err,
						)
					}
					open := true
					defer func() {
						if open {
							rollbackTimestampDirtyRecoveryTx(ctx, tx)
						}
					}()
					if err := setTimestampDirtyRecoveryLocalTimeouts(
						ctx,
						tx,
						lockTimeout,
					); err != nil {
						return err
					}
					if err := lockTimestampDirtyRecoveryEvidence(
						ctx,
						tx,
						plan.CatalogShape,
					); err != nil {
						return err
					}
					status, err := r.readTimestampDirtyRecoverySchemaStatusIsolated(
						ctx,
						lockTimeout,
					)
					if err != nil {
						return err
					}
					observedStatus, err := timestampDirtyRecoverySchemaStatus(status)
					if err != nil {
						return err
					}
					plannedStatus := types.TimestampDirtyRecoverySchemaStatus{
						Version: plan.ExpectedDirtyVersion,
						Dirty:   true,
					}
					action := types.TimestampDirtyRecoveryActionForced
					switch {
					case observedStatus == plannedStatus:
					case !observedStatus.Dirty &&
						observedStatus.Version == plan.ForceVersion:
						action = types.TimestampDirtyRecoveryActionObservedAlreadyClean
					default:
						return fmt.Errorf(
							"timestamp dirty recovery plan is stale: observed version %d dirty=%t",
							observedStatus.Version,
							observedStatus.Dirty,
						)
					}
					catalog, err := classifyTimestampDirtyRecoveryCatalog(ctx, tx)
					if err != nil {
						return err
					}
					if catalog.Shape != plan.CatalogShape ||
						catalog.Checksum != plan.CatalogChecksum {
						return errors.New(
							"timestamp dirty recovery catalog no longer matches the plan",
						)
					}
					if action == types.TimestampDirtyRecoveryActionForced {
						forceVersion, err := ClassifyTimestampDirtyRecovery(
							status,
							catalog.Shape,
						)
						if err != nil {
							return err
						}
						if forceVersion != plan.ForceVersion {
							return errors.New(
								"timestamp dirty recovery force version no longer matches the plan",
							)
						}
					}
					evidenceBinding, err := validateTimestampDirtyRecoveryEvidence(
						ctx,
						tx,
						catalog,
						options.Manifest,
						options.ManifestDocumentChecksum,
					)
					if err != nil {
						return err
					}
					if !timestampDirtyRecoveryManifestBindingsEqual(
						evidenceBinding,
						plan.Manifest,
					) {
						return errors.New(
							"timestamp dirty recovery evidence no longer matches the plan",
						)
					}

					verifyContext := ctx
					verifyCancel := func() {}
					if action == types.TimestampDirtyRecoveryActionForced {
						if err := ctx.Err(); err != nil {
							return err
						}
						var schema string
						if err := tx.QueryRow(
							ctx,
							`SELECT current_schema()`,
						).Scan(&schema); err != nil {
							return fmt.Errorf(
								"read timestamp dirty recovery schema: %w",
								err,
							)
						}
						engine, err := r.recoveryForceEngineFactory(
							ctx,
							r.db,
							r.log,
							schema,
							lockTimeout,
						)
						if err != nil {
							return fmt.Errorf(
								"construct timestamp dirty recovery engine: %w",
								err,
							)
						}
						if err := ctx.Err(); err != nil {
							return errors.Join(err, engine.Discard())
						}
						forceAttempted = true
						if err := forceTimestampDirtyRecoveryMarker(
							ctx,
							engine,
							plan.ForceVersion,
						); err != nil {
							return err
						}
						forceSucceeded = true
						verifyContext, verifyCancel = context.WithTimeout(
							context.WithoutCancel(ctx),
							migrationStatementTimeout,
						)
					}
					defer verifyCancel()

					postStatus, err := readTimestampDirtyRecoverySchemaStatus(
						verifyContext,
						tx,
					)
					if err != nil {
						return fmt.Errorf(
							"verify timestamp dirty recovery marker: %w",
							err,
						)
					}
					expectedPostStatus := SchemaStatus{
						Version: int(plan.ForceVersion),
						Dirty:   false,
					}
					if postStatus != expectedPostStatus {
						return fmt.Errorf(
							"timestamp dirty recovery post status is version %d dirty=%t; expected version %d dirty=false",
							postStatus.Version,
							postStatus.Dirty,
							plan.ForceVersion,
						)
					}
					postCatalog, err := classifyTimestampDirtyRecoveryCatalog(
						verifyContext,
						tx,
					)
					if err != nil {
						return err
					}
					if postCatalog.Shape != plan.CatalogShape ||
						postCatalog.Checksum != plan.CatalogChecksum {
						return errors.New(
							"timestamp dirty recovery catalog changed during apply",
						)
					}
					postEvidenceBinding, err := validateTimestampDirtyRecoveryEvidence(
						verifyContext,
						tx,
						postCatalog,
						options.Manifest,
						options.ManifestDocumentChecksum,
					)
					if err != nil {
						return err
					}
					if !timestampDirtyRecoveryManifestBindingsEqual(
						postEvidenceBinding,
						plan.Manifest,
					) {
						return errors.New(
							"timestamp dirty recovery evidence changed during apply",
						)
					}
					var completedAt time.Time
					if err := tx.QueryRow(
						verifyContext,
						`SELECT clock_timestamp()`,
					).Scan(&completedAt); err != nil {
						return fmt.Errorf(
							"read timestamp dirty recovery completion time: %w",
							err,
						)
					}
					result = types.TimestampDirtyRecoveryResult{
						FormatVersion:          types.TimestampDirtyRecoveryFormatVersion,
						RecordType:             types.TimestampDirtyRecoveryRecordTypeResult,
						RecoveryID:             plan.RecoveryID,
						PlanChecksum:           options.PlanDocumentChecksum,
						CompletedAt:            completedAt.UTC(),
						PlannedStatus:          plannedStatus,
						ObservedPreApplyStatus: observedStatus,
						Action:                 action,
						ForcedVersion:          plan.ForceVersion,
						CatalogChecksum:        plan.CatalogChecksum,
						Result:                 types.TimestampDirtyRecoveryResultSucceeded,
						PostStatus: types.TimestampDirtyRecoverySchemaStatus{
							Version: plan.ForceVersion,
							Dirty:   false,
						},
					}
					if err := result.Validate(); err != nil {
						return err
					}
					if err := tx.Commit(verifyContext); err != nil {
						return fmt.Errorf(
							"commit timestamp dirty recovery apply: %w",
							err,
						)
					}
					open = false
					if err := ctx.Err(); err != nil {
						return fmt.Errorf(
							"timestamp dirty recovery context ended after marker force: %w",
							err,
						)
					}
					return nil
				},
			)
		},
	)
	if err != nil {
		if forceAttempted {
			phase := "attempt"
			if forceSucceeded {
				phase = "successful force"
			}
			return types.TimestampDirtyRecoveryResult{}, fmt.Errorf(
				"timestamp dirty recovery marker outcome is uncertain after %s: %w",
				phase,
				err,
			)
		}
		return types.TimestampDirtyRecoveryResult{}, err
	}
	return result, nil
}

func timestampDirtyRecoveryManifestBindingsEqual(
	left *types.TimestampDirtyRecoveryManifestBinding,
	right *types.TimestampDirtyRecoveryManifestBinding,
) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func (r *Runner) readTimestampDirtyRecoverySchemaStatusIsolated(
	ctx context.Context,
	lockTimeout time.Duration,
) (status SchemaStatus, finalErr error) {
	connection, err := r.db.Conn(ctx)
	if err != nil {
		return SchemaStatus{}, fmt.Errorf(
			"acquire timestamp dirty recovery status connection: %w",
			err,
		)
	}
	defer func() {
		finalErr = errors.Join(finalErr, connection.Close())
	}()
	err = withTimestampDirtyRecoveryPGXConnection(
		connection,
		func(connection *pgx.Conn) error {
			tx, err := connection.BeginTx(
				ctx,
				pgx.TxOptions{
					IsoLevel:   pgx.RepeatableRead,
					AccessMode: pgx.ReadOnly,
				},
			)
			if err != nil {
				return fmt.Errorf(
					"begin timestamp dirty recovery status check: %w",
					err,
				)
			}
			open := true
			defer func() {
				if open {
					rollbackTimestampDirtyRecoveryTx(ctx, tx)
				}
			}()
			if err := setTimestampDirtyRecoveryLocalTimeouts(
				ctx,
				tx,
				lockTimeout,
			); err != nil {
				return err
			}
			status, err = readTimestampDirtyRecoverySchemaStatus(ctx, tx)
			if err != nil {
				return err
			}
			if err := tx.Commit(ctx); err != nil {
				return fmt.Errorf(
					"commit timestamp dirty recovery status check: %w",
					err,
				)
			}
			open = false
			return nil
		},
	)
	if err != nil {
		return SchemaStatus{}, err
	}
	return status, nil
}

func timestampDirtyRecoverySchemaStatus(
	status SchemaStatus,
) (types.TimestampDirtyRecoverySchemaStatus, error) {
	if status.Version < 0 {
		return types.TimestampDirtyRecoverySchemaStatus{}, errors.New(
			"timestamp dirty recovery schema marker version must be non-negative",
		)
	}
	return types.TimestampDirtyRecoverySchemaStatus{
		Version: uint(status.Version),
		Dirty:   status.Dirty,
	}, nil
}

func lockTimestampDirtyRecoveryEvidence(
	ctx context.Context,
	tx pgx.Tx,
	shape types.TimestampRecoveryCatalogShape,
) error {
	var statement string
	switch shape {
	case types.TimestampRecoveryCatalogShapePredecessor137:
		statement = `
LOCK TABLE ExternalExecution, ExternalExecutionEvent
IN SHARE ROW EXCLUSIVE MODE`
	case types.TimestampRecoveryCatalogShapeExpand138:
		statement = `
LOCK TABLE
  ExternalExecution,
  ExternalExecutionEvent,
  ExternalExecutionTimestampCellProvenance,
  ExternalExecutionTimestampContractGate,
  ExternalExecutionTimestampDeletionTombstone,
  ExternalExecutionTimestampExpandState,
  ExternalExecutionTimestampManifest
IN SHARE ROW EXCLUSIVE MODE`
	default:
		return errors.New(
			"timestamp dirty recovery catalog shape is unsupported",
		)
	}
	if _, err := tx.Exec(ctx, statement); err != nil {
		return fmt.Errorf(
			"lock timestamp dirty recovery evidence: %w",
			err,
		)
	}
	return nil
}

func forceTimestampDirtyRecoveryMarker(
	ctx context.Context,
	engine timestampDirtyRecoveryForceEngine,
	version uint,
) error {
	if err := ctx.Err(); err != nil {
		return errors.Join(err, engine.Discard())
	}
	forceErr := engine.Force(int(version))
	if err := ctx.Err(); err != nil {
		return errors.Join(
			fmt.Errorf(
				"timestamp dirty recovery marker force outcome is uncertain after cancellation: %w",
				err,
			),
			engine.Discard(),
		)
	}
	if forceErr != nil {
		return errors.Join(
			fmt.Errorf(
				"timestamp dirty recovery marker force outcome is uncertain: %w",
				forceErr,
			),
			engine.Discard(),
		)
	}
	if err := engine.Discard(); err != nil {
		return fmt.Errorf(
			"timestamp dirty recovery marker force outcome is uncertain while discarding its pinned connection: %w",
			err,
		)
	}
	return nil
}

func (r *Runner) withTimestampDirtyRecoveryLock(
	ctx context.Context,
	lockTimeout time.Duration,
	run func(*sql.Conn) error,
) (finalErr error) {
	connection, err := r.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf(
			"acquire timestamp dirty recovery connection: %w",
			err,
		)
	}
	locked, uncertain, err := acquireMigrationAdvisoryLock(
		ctx,
		connection,
		lockTimeout,
	)
	if err != nil {
		if uncertain {
			return errors.Join(err, discardMigrationConnection(connection))
		}
		return errors.Join(err, connection.Close())
	}
	if !locked {
		return errors.Join(
			errors.New("timestamp migration advisory lock was not acquired"),
			connection.Close(),
		)
	}
	defer func() {
		unlockContext, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			DefaultMigrationLockTimeout,
		)
		defer cancel()
		var unlocked bool
		unlockErr := connection.QueryRowContext(
			unlockContext,
			`SELECT pg_advisory_unlock($1)`,
			externalexecutiontimestamp.MigrationAdvisoryLockKey,
		).Scan(&unlocked)
		if unlockErr != nil || !unlocked {
			finalErr = errors.Join(
				finalErr,
				errors.New(
					"timestamp dirty recovery advisory lock release is uncertain",
				),
				discardMigrationConnection(connection),
			)
			return
		}
		finalErr = errors.Join(finalErr, connection.Close())
	}()
	return run(connection)
}

func withTimestampDirtyRecoveryPGXConnection(
	connection *sql.Conn,
	run func(*pgx.Conn) error,
) error {
	return connection.Raw(func(driverConnection any) error {
		stdlibConnection, ok := driverConnection.(*stdlib.Conn)
		if !ok {
			return fmt.Errorf(
				"timestamp dirty recovery requires pgx stdlib connection; got %T",
				driverConnection,
			)
		}
		return run(stdlibConnection.Conn())
	})
}

func setTimestampDirtyRecoveryLocalTimeouts(
	ctx context.Context,
	tx pgx.Tx,
	lockTimeout time.Duration,
) error {
	lockMilliseconds := lockTimeout.Milliseconds()
	if lockMilliseconds < 1 {
		lockMilliseconds = 1
	}
	if _, err := tx.Exec(
		ctx,
		`SELECT
		  set_config('lock_timeout', $1, true),
		  set_config('statement_timeout', $2, true)`,
		fmt.Sprintf("%dms", lockMilliseconds),
		fmt.Sprintf("%dms", migrationStatementTimeout.Milliseconds()),
	); err != nil {
		return fmt.Errorf(
			"set timestamp dirty recovery transaction timeouts: %w",
			err,
		)
	}
	return nil
}

func rollbackTimestampDirtyRecoveryTx(ctx context.Context, tx pgx.Tx) {
	rollbackContext, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		DefaultMigrationLockTimeout,
	)
	defer cancel()
	_ = tx.Rollback(rollbackContext)
}

func readTimestampDirtyRecoverySchemaStatus(
	ctx context.Context,
	tx pgx.Tx,
) (SchemaStatus, error) {
	var valid bool
	if err := tx.QueryRow(ctx, `
SELECT
  to_regclass(format('%I.schema_migrations', current_schema())) IS NOT NULL
  AND (
    SELECT relation.relkind = 'r'
      AND relation.relpersistence = 'p'
      AND NOT relation.relrowsecurity
      AND NOT relation.relforcerowsecurity
    FROM pg_catalog.pg_class relation
    WHERE relation.oid =
      to_regclass(format('%I.schema_migrations', current_schema()))
  )
  AND (
    SELECT count(*) = 2
      AND count(*) FILTER (WHERE
        (
          attribute_row.attname = 'version'
          AND attribute_row.atttypid = 'bigint'::regtype
        )
        OR
        (
          attribute_row.attname = 'dirty'
          AND attribute_row.atttypid = 'boolean'::regtype
        )
      ) = 2
      AND bool_and(attribute_row.attnotnull)
      AND bool_and(attribute_row.attidentity = '')
      AND bool_and(attribute_row.attgenerated = '')
      AND count(attribute_default.oid) = 0
    FROM pg_catalog.pg_attribute attribute_row
    LEFT JOIN pg_catalog.pg_attrdef attribute_default
      ON attribute_default.adrelid = attribute_row.attrelid
     AND attribute_default.adnum = attribute_row.attnum
    WHERE attribute_row.attrelid =
      to_regclass(format('%I.schema_migrations', current_schema()))
      AND attribute_row.attnum > 0
      AND NOT attribute_row.attisdropped
  )
  AND (
    SELECT count(*) FILTER (WHERE
        constraint_row.contype = 'p'
        AND constraint_row.convalidated
        AND COALESCE(
          (to_jsonb(constraint_row)->>'conenforced')::boolean,
          TRUE
        )
        AND NOT constraint_row.condeferrable
        AND NOT constraint_row.condeferred
        AND constraint_row.conislocal
        AND constraint_row.connoinherit
        AND constraint_row.coninhcount = 0
        AND constraint_row.conparentid = 0
        AND cardinality(constraint_row.conkey) = 1
        AND pg_get_constraintdef(constraint_row.oid) =
          'PRIMARY KEY (version)'
      ) = 1
      AND count(*) FILTER (
        WHERE constraint_row.contype = 'p'
      ) = 1
      AND count(*) FILTER (
        WHERE constraint_row.contype NOT IN ('p', 'n')
      ) = 0
      AND (
        (
          current_setting('server_version_num')::integer < 180000
          AND count(*) FILTER (WHERE constraint_row.contype = 'n') = 0
        )
        OR (
          current_setting('server_version_num')::integer >= 180000
          AND
          count(*) FILTER (WHERE constraint_row.contype = 'n') = 2
          AND count(*) FILTER (
            WHERE constraint_row.contype = 'n'
              AND constraint_row.convalidated
              AND COALESCE(
                (to_jsonb(constraint_row)->>'conenforced')::boolean,
                TRUE
              )
              AND NOT constraint_row.condeferrable
              AND NOT constraint_row.condeferred
              AND constraint_row.conislocal
              AND NOT constraint_row.connoinherit
              AND constraint_row.coninhcount = 0
              AND constraint_row.conparentid = 0
              AND cardinality(constraint_row.conkey) = 1
              AND constrained_attribute.attname IN ('version', 'dirty')
          ) = 2
          AND count(DISTINCT constrained_attribute.attname) FILTER (
            WHERE constraint_row.contype = 'n'
          ) = 2
        )
      )
    FROM pg_catalog.pg_constraint constraint_row
    LEFT JOIN LATERAL unnest(constraint_row.conkey)
      constrained_key(attribute_number)
      ON constraint_row.contype = 'n'
    LEFT JOIN pg_catalog.pg_attribute constrained_attribute
      ON constrained_attribute.attrelid = constraint_row.conrelid
     AND constrained_attribute.attnum = constrained_key.attribute_number
    WHERE constraint_row.conrelid =
      to_regclass(format('%I.schema_migrations', current_schema()))
  )
  AND (
    SELECT count(*) = 1
      AND count(*) FILTER (WHERE
        primary_constraint.contype = 'p'
        AND primary_constraint.conrelid = index_row.indrelid
        AND index_row.indisprimary
        AND index_row.indisunique
        AND index_row.indisvalid
        AND index_row.indisready
        AND index_row.indislive
        AND index_row.indnkeyatts = 1
        AND index_row.indnatts = 1
        AND index_row.indexprs IS NULL
        AND index_row.indpred IS NULL
        AND access_method.amname = 'btree'
        AND pg_get_indexdef(index_row.indexrelid, 1, true) = 'version'
      ) = 1
    FROM pg_catalog.pg_index index_row
    JOIN pg_catalog.pg_class index_relation
      ON index_relation.oid = index_row.indexrelid
    JOIN pg_catalog.pg_am access_method
      ON access_method.oid = index_relation.relam
    LEFT JOIN pg_catalog.pg_constraint primary_constraint
      ON primary_constraint.conindid = index_row.indexrelid
    WHERE index_row.indrelid =
      to_regclass(format('%I.schema_migrations', current_schema()))
  )
  AND NOT EXISTS (
    SELECT 1
    FROM pg_catalog.pg_trigger trigger_row
    WHERE trigger_row.tgrelid =
      to_regclass(format('%I.schema_migrations', current_schema()))
      AND NOT trigger_row.tgisinternal
  )
  AND NOT EXISTS (
    SELECT 1
    FROM pg_catalog.pg_rewrite rule_row
    WHERE rule_row.ev_class =
      to_regclass(format('%I.schema_migrations', current_schema()))
  )
  AND NOT EXISTS (
    SELECT 1
    FROM pg_catalog.pg_policy policy_row
    WHERE policy_row.polrelid =
      to_regclass(format('%I.schema_migrations', current_schema()))
  )
  AND NOT EXISTS (
    SELECT 1
    FROM pg_catalog.pg_inherits inheritance_row
    WHERE inheritance_row.inhrelid =
        to_regclass(format('%I.schema_migrations', current_schema()))
      OR inheritance_row.inhparent =
        to_regclass(format('%I.schema_migrations', current_schema()))
  )
  AND NOT EXISTS (
    SELECT 1
    FROM pg_catalog.pg_constraint constraint_row
    WHERE constraint_row.contype = 'f'
      AND constraint_row.confrelid =
        to_regclass(format('%I.schema_migrations', current_schema()))
  )`).Scan(&valid); err != nil {
		return SchemaStatus{}, fmt.Errorf(
			"inspect timestamp dirty recovery schema marker: %w",
			err,
		)
	}
	if !valid {
		return SchemaStatus{}, errors.New(
			"timestamp dirty recovery schema_migrations catalog is malformed or absent",
		)
	}
	var rowCount int
	if err := tx.QueryRow(
		ctx,
		`SELECT count(*) FROM schema_migrations`,
	).Scan(&rowCount); err != nil {
		return SchemaStatus{}, fmt.Errorf(
			"count timestamp dirty recovery schema marker: %w",
			err,
		)
	}
	if rowCount != 1 {
		return SchemaStatus{}, fmt.Errorf(
			"timestamp dirty recovery schema_migrations must contain exactly one row; found %d",
			rowCount,
		)
	}
	var status SchemaStatus
	if err := tx.QueryRow(
		ctx,
		`SELECT version, dirty FROM schema_migrations`,
	).Scan(&status.Version, &status.Dirty); err != nil {
		return SchemaStatus{}, fmt.Errorf(
			"read timestamp dirty recovery schema marker: %w",
			err,
		)
	}
	if status.Version < 0 {
		return SchemaStatus{}, errors.New(
			"timestamp dirty recovery schema marker version must be non-negative",
		)
	}
	return status, nil
}

//nolint:gocyclo // Evidence validation is an exhaustive matrix over catalog shapes and manifest states.
func validateTimestampDirtyRecoveryEvidence(
	ctx context.Context,
	tx pgx.Tx,
	catalog timestampDirtyRecoveryCatalog,
	manifest *types.ExternalExecutionTimestampManifest,
	manifestDocumentChecksum string,
) (*types.TimestampDirtyRecoveryManifestBinding, error) {
	binding, err := timestampDirtyRecoveryManifestBinding(
		manifest,
		manifestDocumentChecksum,
	)
	if err != nil {
		return nil, err
	}
	switch catalog.Shape {
	case types.TimestampRecoveryCatalogShapePredecessor137:
		identity, err := readTimestampDirtyRecoveryIdentity(ctx, tx)
		if err != nil {
			return nil, err
		}
		if identity.ExecutionCount == 0 && identity.EventCount == 0 {
			if binding != nil {
				return nil, errors.New(
					"empty predecessor timestamp recovery does not accept a manifest",
				)
			}
			return nil, nil
		}
		if manifest == nil {
			return nil, errors.New(
				"non-empty predecessor timestamp recovery requires an APPROVED root manifest",
			)
		}
		if err := requireExternalExecutionTimestampManifestMatches(
			*manifest,
			identity,
		); err != nil {
			return nil, fmt.Errorf(
				"predecessor timestamp recovery manifest: %w",
				err,
			)
		}
		return binding, nil
	case types.TimestampRecoveryCatalogShapeExpand138:
		state, err := readTimestampDirtyRecoveryExpandState(ctx, tx)
		if err != nil {
			return nil, err
		}
		switch state.TransitionKind {
		case "ZERO_HISTORY":
			if binding != nil {
				return nil, errors.New(
					"zero-history expand timestamp recovery does not accept a manifest",
				)
			}
			readiness, err := internaldb.CheckExternalExecutionTimestampExpandRecoveryReadinessInTx(
				ctx,
				tx,
				138,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"timestamp dirty recovery expand readiness: %w",
					err,
				)
			}
			if readiness.TransitionKind != "ZERO_HISTORY" {
				return nil, errors.New(
					"zero-history timestamp recovery evidence changed",
				)
			}
			return nil, nil
		case "MANIFEST_REQUIRED":
			if manifest == nil {
				return nil, errors.New(
					"manifest-required expand timestamp recovery requires an APPROVED manifest",
				)
			}
			mode, err := classifyTimestampDirtyRecoveryExpandEvidence(ctx, tx)
			if err != nil {
				return nil, err
			}
			switch mode {
			case timestampDirtyRecoveryExpandEvidencePreApply:
				if manifest.SupersedesManifestID != nil {
					return nil, errors.New(
						"pre-apply timestamp dirty recovery requires a root manifest",
					)
				}
				if err := requireTimestampDirtyRecoveryManifestMatchesExpandState(
					*manifest,
					state,
				); err != nil {
					return nil, err
				}
				identity, err := readTimestampDirtyRecoveryIdentity(ctx, tx)
				if err != nil {
					return nil, err
				}
				if err := requireExternalExecutionTimestampManifestMatches(
					*manifest,
					identity,
				); err != nil {
					return nil, fmt.Errorf(
						"pre-apply expand timestamp recovery manifest: %w",
						err,
					)
				}
				if err := requireTimestampDirtyRecoveryHistoricalShadowsEmpty(
					ctx,
					tx,
				); err != nil {
					return nil, err
				}
				return binding, nil
			case timestampDirtyRecoveryExpandEvidenceVerified:
				readiness, err := internaldb.CheckExternalExecutionTimestampExpandRecoveryReadinessInTx(
					ctx,
					tx,
					138,
				)
				if err != nil {
					return nil, fmt.Errorf(
						"timestamp dirty recovery expand readiness: %w",
						err,
					)
				}
				if readiness.TransitionKind != "MANIFEST_REQUIRED" ||
					readiness.ManifestID == nil ||
					*readiness.ManifestID != manifest.ID {
					return nil, errors.New(
						"verified timestamp recovery tip does not match the supplied approved manifest",
					)
				}
				storedManifest, storedState, err := readTimestampDirtyRecoveryStoredManifest(
					ctx,
					tx,
					manifest.ID,
				)
				if err != nil {
					return nil, err
				}
				if storedState !=
					types.ExternalExecutionTimestampManifestStateVerified {
					return nil, errors.New(
						"timestamp dirty recovery stored tip is not VERIFIED",
					)
				}
				storedManifest.State = types.ExternalExecutionTimestampManifestStateApproved
				if !timestampDirtyRecoveryManifestsEqual(
					storedManifest,
					*manifest,
				) {
					return nil, errors.New(
						"verified timestamp recovery document does not match the supplied approved manifest",
					)
				}
				return binding, nil
			default:
				return nil, errors.New(
					"manifest-required timestamp recovery evidence is partial or mixed",
				)
			}
		default:
			return nil, errors.New(
				"timestamp dirty recovery expand transition is unsupported",
			)
		}
	default:
		return nil, errors.New(
			"timestamp dirty recovery catalog shape is unsupported",
		)
	}
}

func timestampDirtyRecoveryManifestBinding(
	manifest *types.ExternalExecutionTimestampManifest,
	documentChecksum string,
) (*types.TimestampDirtyRecoveryManifestBinding, error) {
	if manifest == nil {
		if documentChecksum != "" {
			return nil, errors.New(
				"timestamp dirty recovery manifest checksum requires a manifest",
			)
		}
		return nil, nil
	}
	if !timestampDirtyRecoveryChecksumPattern.MatchString(documentChecksum) {
		return nil, errors.New(
			"timestamp dirty recovery manifest document checksum must use lowercase sha256 format",
		)
	}
	actualDocumentChecksum, err := computeTimestampDirtyRecoveryDocumentChecksum(*manifest)
	if err != nil {
		return nil, err
	}
	if documentChecksum != actualDocumentChecksum {
		return nil, errors.New(
			"timestamp dirty recovery manifest document checksum does not match the canonical manifest",
		)
	}
	if manifest.State != types.ExternalExecutionTimestampManifestStateApproved {
		return nil, errors.New(
			"timestamp dirty recovery requires an APPROVED manifest document",
		)
	}
	if problems := externalexecutiontimestamp.ValidateManifestDocument(
		*manifest,
	); len(problems) != 0 {
		return nil, fmt.Errorf(
			"timestamp dirty recovery manifest is invalid: %w",
			errors.Join(problems...),
		)
	}
	return &types.TimestampDirtyRecoveryManifestBinding{
		ID:                       manifest.ID,
		DocumentChecksum:         documentChecksum,
		DecisionContentChecksum:  manifest.DecisionContentChecksum,
		RawSetChecksum:           manifest.RawCellChecksum,
		DatabaseIdentityChecksum: manifest.DatabaseIdentityChecksum,
		ExecutionCount:           manifest.ExecutionCount,
		EventCount:               manifest.EventCount,
		RawCellCount:             manifest.RawCellCount,
	}, nil
}

func computeTimestampDirtyRecoveryDocumentChecksum(value any) (string, error) {
	document, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", fmt.Errorf(
			"marshal canonical timestamp dirty recovery document: %w",
			err,
		)
	}
	document = append(document, '\n')
	checksum := sha256.Sum256(document)
	return fmt.Sprintf("sha256:%x", checksum), nil
}

func readTimestampDirtyRecoveryStoredManifest(
	ctx context.Context,
	tx pgx.Tx,
	manifestID uuid.UUID,
) (types.ExternalExecutionTimestampManifest,
	types.ExternalExecutionTimestampManifestState, error,
) {
	var manifest types.ExternalExecutionTimestampManifest
	var storedState types.ExternalExecutionTimestampManifestState
	if err := tx.QueryRow(ctx, `
SELECT id, supersedes_manifest_id, database_identity_checksum,
  source_schema_version,
  to_char(snapshot_started_at AT TIME ZONE 'UTC',
    'YYYY-MM-DD"T"HH24:MI:SS.US') || 'Z',
  to_char(snapshot_ended_at AT TIME ZONE 'UTC',
    'YYYY-MM-DD"T"HH24:MI:SS.US') || 'Z',
  execution_count, event_count, raw_cell_count, populated_cell_count,
  raw_cell_checksum, evidence_bundle_reference, evidence_bundle_checksum,
  tool_version, conversion_expression_version, author_identity,
  reviewer_identity,
  to_char(approved_at AT TIME ZONE 'UTC',
    'YYYY-MM-DD"T"HH24:MI:SS.US') || 'Z',
  target_release_commit, target_image_digest, state,
  decision_content_checksum
FROM ExternalExecutionTimestampManifest
WHERE id=$1`, manifestID).Scan(
		&manifest.ID,
		&manifest.SupersedesManifestID,
		&manifest.DatabaseIdentityChecksum,
		&manifest.SourceSchemaVersion,
		&manifest.SnapshotStartedAt,
		&manifest.SnapshotEndedAt,
		&manifest.ExecutionCount,
		&manifest.EventCount,
		&manifest.RawCellCount,
		&manifest.PopulatedCellCount,
		&manifest.RawCellChecksum,
		&manifest.EvidenceBundleReference,
		&manifest.EvidenceBundleChecksum,
		&manifest.ToolVersion,
		&manifest.ConversionExpressionVersion,
		&manifest.AuthorIdentity,
		&manifest.ReviewerIdentity,
		&manifest.ApprovedAt,
		&manifest.TargetReleaseCommit,
		&manifest.TargetImageDigest,
		&storedState,
		&manifest.DecisionContentChecksum,
	); err != nil {
		return types.ExternalExecutionTimestampManifest{}, "", fmt.Errorf(
			"read verified timestamp recovery manifest: %w",
			err,
		)
	}
	manifest.State = storedState
	rows, err := tx.Query(ctx, `
SELECT source_table, source_row_id, source_column, column_ordinal,
  CASE WHEN raw_is_null THEN NULL ELSE
    to_char(raw_value, 'YYYY-MM-DD"T"HH24:MI:SS.US') END,
  decision, source_zone, source_offset_seconds,
  CASE WHEN converted_value IS NULL THEN NULL ELSE
    to_char(converted_value AT TIME ZONE 'UTC',
      'YYYY-MM-DD"T"HH24:MI:SS.US') || 'Z' END,
  evidence_reference, evidence_checksum, approving_identity,
  raw_cell_checksum, conversion_expression_version
FROM ExternalExecutionTimestampCellProvenance
WHERE manifest_id=$1
ORDER BY source_table, source_row_id, column_ordinal`, manifestID)
	if err != nil {
		return types.ExternalExecutionTimestampManifest{}, "", fmt.Errorf(
			"read verified timestamp recovery provenance: %w",
			err,
		)
	}
	defer rows.Close()
	manifest.Cells = make(
		[]types.ExternalExecutionTimestampCellDecision,
		0,
		manifest.RawCellCount,
	)
	for rows.Next() {
		var cell types.ExternalExecutionTimestampCellDecision
		var sourceZone, evidenceReference, evidenceChecksum *string
		var approvingIdentity *string
		if err := rows.Scan(
			&cell.SourceTable,
			&cell.SourceRowID,
			&cell.SourceColumn,
			&cell.ColumnOrdinal,
			&cell.RawValue,
			&cell.Decision,
			&sourceZone,
			&cell.SourceOffsetSeconds,
			&cell.ConvertedValue,
			&evidenceReference,
			&evidenceChecksum,
			&approvingIdentity,
			&cell.RawCellChecksum,
			&cell.ConversionExpressionVersion,
		); err != nil {
			return types.ExternalExecutionTimestampManifest{}, "", fmt.Errorf(
				"scan verified timestamp recovery provenance: %w",
				err,
			)
		}
		if sourceZone != nil {
			cell.SourceZone = *sourceZone
		}
		if evidenceReference != nil {
			cell.EvidenceReference = *evidenceReference
		}
		if evidenceChecksum != nil {
			cell.EvidenceChecksum = *evidenceChecksum
		}
		if approvingIdentity != nil {
			cell.ApprovingIdentity = *approvingIdentity
		}
		manifest.Cells = append(manifest.Cells, cell)
	}
	if err := rows.Err(); err != nil {
		return types.ExternalExecutionTimestampManifest{}, "", fmt.Errorf(
			"iterate verified timestamp recovery provenance: %w",
			err,
		)
	}
	return manifest, storedState, nil
}

func timestampDirtyRecoveryManifestsEqual(
	left types.ExternalExecutionTimestampManifest,
	right types.ExternalExecutionTimestampManifest,
) bool {
	leftCells := left.Cells
	rightCells := right.Cells
	left.Cells = nil
	right.Cells = nil
	if !reflect.DeepEqual(left, right) || len(leftCells) != len(rightCells) {
		return false
	}
	cellKey := func(cell types.ExternalExecutionTimestampCellDecision) string {
		return fmt.Sprintf(
			"%s/%s/%s/%d",
			cell.SourceTable,
			cell.SourceRowID,
			cell.SourceColumn,
			cell.ColumnOrdinal,
		)
	}
	rightByKey := make(
		map[string]types.ExternalExecutionTimestampCellDecision,
		len(rightCells),
	)
	for _, cell := range rightCells {
		rightByKey[cellKey(cell)] = cell
	}
	for _, cell := range leftCells {
		rightCell, exists := rightByKey[cellKey(cell)]
		if !exists || !reflect.DeepEqual(cell, rightCell) {
			return false
		}
	}
	return true
}

//nolint:dupl // Recovery re-reads identity inside its fenced transaction instead of sharing preflight state.
func readTimestampDirtyRecoveryIdentity(
	ctx context.Context,
	tx pgx.Tx,
) (externalExecutionTimestampIdentity, error) {
	rows, err := tx.Query(ctx, migrationExternalExecutionTimestampRawCellsSQL)
	if err != nil {
		return externalExecutionTimestampIdentity{}, fmt.Errorf(
			"read timestamp dirty recovery raw identity: %w",
			err,
		)
	}
	defer rows.Close()

	rawCells := make([]types.ExternalExecutionTimestampRawCell, 0)
	executionIDs := make([]uuid.UUID, 0)
	eventIDs := make([]uuid.UUID, 0)
	seenExecutions := make(map[uuid.UUID]struct{})
	seenEvents := make(map[uuid.UUID]struct{})
	for rows.Next() {
		var sourceTable, sourceColumn string
		var sourceRowID uuid.UUID
		var columnOrdinal int16
		var rawValue sql.NullString
		if err := rows.Scan(
			&sourceTable,
			&sourceRowID,
			&sourceColumn,
			&columnOrdinal,
			&rawValue,
		); err != nil {
			return externalExecutionTimestampIdentity{}, fmt.Errorf(
				"scan timestamp dirty recovery raw identity: %w",
				err,
			)
		}
		cell := types.ExternalExecutionTimestampRawCell{
			SourceTable:   sourceTable,
			SourceRowID:   sourceRowID,
			SourceColumn:  sourceColumn,
			ColumnOrdinal: uint8(columnOrdinal),
		}
		if rawValue.Valid {
			value := rawValue.String
			cell.RawValue = &value
		}
		cell.RawCellChecksum, err = externalexecutiontimestamp.ComputeRawCellChecksum(cell)
		if err != nil {
			return externalExecutionTimestampIdentity{}, err
		}
		rawCells = append(rawCells, cell)
		switch sourceTable {
		case "externalexecution":
			if _, exists := seenExecutions[sourceRowID]; !exists {
				seenExecutions[sourceRowID] = struct{}{}
				executionIDs = append(executionIDs, sourceRowID)
			}
		case "externalexecutionevent":
			if _, exists := seenEvents[sourceRowID]; !exists {
				seenEvents[sourceRowID] = struct{}{}
				eventIDs = append(eventIDs, sourceRowID)
			}
		default:
			return externalExecutionTimestampIdentity{}, fmt.Errorf(
				"unexpected timestamp recovery source table %q",
				sourceTable,
			)
		}
	}
	if err := rows.Err(); err != nil {
		return externalExecutionTimestampIdentity{}, fmt.Errorf(
			"iterate timestamp dirty recovery raw identity: %w",
			err,
		)
	}
	rawChecksum, err := externalexecutiontimestamp.ComputeRawSetChecksum(rawCells)
	if err != nil {
		return externalExecutionTimestampIdentity{}, err
	}
	identityChecksum, err := externalexecutiontimestamp.ComputeDatabaseIdentityChecksum(
		137,
		executionIDs,
		eventIDs,
		uint64(len(rawCells)),
		rawChecksum,
	)
	if err != nil {
		return externalExecutionTimestampIdentity{}, err
	}
	return externalExecutionTimestampIdentity{
		SourceSchemaVersion:      137,
		ExecutionCount:           uint64(len(executionIDs)),
		EventCount:               uint64(len(eventIDs)),
		RawCellCount:             uint64(len(rawCells)),
		RawCellChecksum:          rawChecksum,
		DatabaseIdentityChecksum: identityChecksum,
	}, nil
}

type timestampDirtyRecoveryExpandState struct {
	TransitionKind string
	SourceVersion  uint
	ExecutionCount uint64
	EventCount     uint64
	RawCellCount   uint64
	TransitionedAt time.Time
}

func readTimestampDirtyRecoveryExpandState(
	ctx context.Context,
	tx pgx.Tx,
) (timestampDirtyRecoveryExpandState, error) {
	var state timestampDirtyRecoveryExpandState
	if err := tx.QueryRow(ctx, `
SELECT transition_kind, source_schema_version,
       transition_execution_count, transition_event_count,
       transition_raw_cell_count, transitioned_at
FROM ExternalExecutionTimestampExpandState
WHERE singleton`).Scan(
		&state.TransitionKind,
		&state.SourceVersion,
		&state.ExecutionCount,
		&state.EventCount,
		&state.RawCellCount,
		&state.TransitionedAt,
	); err != nil {
		return timestampDirtyRecoveryExpandState{}, fmt.Errorf(
			"read timestamp dirty recovery expand state: %w",
			err,
		)
	}
	if state.SourceVersion != 137 ||
		state.RawCellCount != 5*state.ExecutionCount+state.EventCount {
		return timestampDirtyRecoveryExpandState{}, errors.New(
			"timestamp dirty recovery expand state is malformed",
		)
	}
	switch state.TransitionKind {
	case "ZERO_HISTORY":
		if state.ExecutionCount != 0 || state.EventCount != 0 {
			return timestampDirtyRecoveryExpandState{}, errors.New(
				"timestamp dirty recovery zero-history counts are nonzero",
			)
		}
	case "MANIFEST_REQUIRED":
		if state.ExecutionCount == 0 && state.EventCount == 0 {
			return timestampDirtyRecoveryExpandState{}, errors.New(
				"timestamp dirty recovery manifest-required counts are empty",
			)
		}
	default:
		return timestampDirtyRecoveryExpandState{}, fmt.Errorf(
			"timestamp dirty recovery expand transition %q is unsupported",
			state.TransitionKind,
		)
	}
	return state, nil
}

func requireTimestampDirtyRecoveryManifestMatchesExpandState(
	manifest types.ExternalExecutionTimestampManifest,
	state timestampDirtyRecoveryExpandState,
) error {
	if manifest.ExecutionCount != state.ExecutionCount ||
		manifest.EventCount != state.EventCount ||
		manifest.RawCellCount != state.RawCellCount {
		return errors.New(
			"timestamp dirty recovery manifest counts differ from expand state",
		)
	}
	snapshotEndedAt, err := externalexecutiontimestamp.ParseInstant(
		manifest.SnapshotEndedAt,
	)
	if err != nil {
		return fmt.Errorf(
			"timestamp dirty recovery manifest snapshot end: %w",
			err,
		)
	}
	if snapshotEndedAt.After(state.TransitionedAt) {
		return errors.New(
			"timestamp dirty recovery manifest snapshot ends after expand transition",
		)
	}
	return nil
}

type timestampDirtyRecoveryExpandEvidenceMode uint8

const (
	timestampDirtyRecoveryExpandEvidenceUnknown timestampDirtyRecoveryExpandEvidenceMode = iota
	timestampDirtyRecoveryExpandEvidencePreApply
	timestampDirtyRecoveryExpandEvidenceVerified
)

func classifyTimestampDirtyRecoveryExpandEvidence(
	ctx context.Context,
	tx pgx.Tx,
) (timestampDirtyRecoveryExpandEvidenceMode, error) {
	var manifestCount, verifiedManifestCount, provenanceCount uint64
	var tombstoneCount, contractGateCount uint64
	if err := tx.QueryRow(ctx, `
SELECT
  (SELECT count(*) FROM ExternalExecutionTimestampManifest),
  (SELECT count(*) FROM ExternalExecutionTimestampManifest
    WHERE state='VERIFIED'),
  (SELECT count(*) FROM ExternalExecutionTimestampCellProvenance),
  (SELECT count(*) FROM ExternalExecutionTimestampDeletionTombstone),
  (SELECT count(*) FROM ExternalExecutionTimestampContractGate)`).Scan(
		&manifestCount,
		&verifiedManifestCount,
		&provenanceCount,
		&tombstoneCount,
		&contractGateCount,
	); err != nil {
		return timestampDirtyRecoveryExpandEvidenceUnknown, fmt.Errorf(
			"read timestamp dirty recovery evidence ledger: %w",
			err,
		)
	}
	if contractGateCount != 0 {
		return timestampDirtyRecoveryExpandEvidenceUnknown, errors.New(
			"timestamp dirty recovery refuses an existing contract gate",
		)
	}
	if manifestCount == 0 && verifiedManifestCount == 0 &&
		provenanceCount == 0 && tombstoneCount == 0 {
		return timestampDirtyRecoveryExpandEvidencePreApply, nil
	}
	if manifestCount > 0 &&
		manifestCount == verifiedManifestCount &&
		provenanceCount > 0 {
		return timestampDirtyRecoveryExpandEvidenceVerified, nil
	}
	return timestampDirtyRecoveryExpandEvidenceUnknown, errors.New(
		"timestamp dirty recovery evidence ledger is partial or mixed",
	)
}

func requireTimestampDirtyRecoveryHistoricalShadowsEmpty(
	ctx context.Context,
	tx pgx.Tx,
) error {
	var populated uint64
	if err := tx.QueryRow(ctx, `
SELECT
  (SELECT count(*) FROM ExternalExecution
   WHERE created_at_instant IS NOT NULL
      OR updated_at_instant IS NOT NULL
      OR started_at_instant IS NOT NULL
      OR completed_at_instant IS NOT NULL
      OR callback_deadline_at_instant IS NOT NULL)
  +
  (SELECT count(*) FROM ExternalExecutionEvent
   WHERE created_at_instant IS NOT NULL)`).Scan(&populated); err != nil {
		return fmt.Errorf(
			"read timestamp dirty recovery historical shadows: %w",
			err,
		)
	}
	if populated != 0 {
		return errors.New(
			"pre-apply timestamp dirty recovery requires every historical shadow to remain null",
		)
	}
	return nil
}
