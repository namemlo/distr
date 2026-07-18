package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/distr-sh/distr/internal/env"
	"github.com/distr-sh/distr/internal/migrations"
	"github.com/distr-sh/distr/internal/svc"
	"github.com/distr-sh/distr/internal/types"
	"github.com/spf13/cobra"
)

type MigrateOptions struct {
	Down              bool
	To                uint
	ToSet             bool
	Check             bool
	TimestampManifest string
	LockTimeout       time.Duration
}

type migrateRuntime struct {
	Initialize func()
	ReadFile   func(string) ([]byte, error)
	Run        func(context.Context, migrations.RunOptions) error
}

func (runtime migrateRuntime) withDefaults() migrateRuntime {
	if runtime.Initialize == nil {
		runtime.Initialize = env.Initialize
	}
	if runtime.ReadFile == nil {
		runtime.ReadFile = os.ReadFile
	}
	if runtime.Run == nil {
		runtime.Run = executeMigrations
	}
	return runtime
}

func NewMigrateCommand() *cobra.Command {
	return newMigrateCommand(migrateRuntime{})
}

func newMigrateCommand(runtime migrateRuntime) *cobra.Command {
	runtime = runtime.withDefaults()
	options := MigrateOptions{LockTimeout: migrations.DefaultMigrationLockTimeout}
	command := &cobra.Command{
		Use:           "migrate",
		Short:         "execute database migrations",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(command *cobra.Command, _ []string) error {
			options.ToSet = command.Flags().Changed("to")
			return runMigrate(command.Context(), options, runtime)
		},
	}
	command.Flags().BoolVar(
		&options.Down,
		"down",
		false,
		"run all down migrations. DANGER: This will purge the database!",
	)
	command.Flags().UintVar(
		&options.To,
		"to",
		0,
		"run migrations to reach the specified schema revision",
	)
	command.Flags().BoolVar(
		&options.Check,
		"check",
		false,
		"run status and migration preflight checks without schema mutation",
	)
	command.Flags().StringVar(
		&options.TimestampManifest,
		"external-execution-timestamp-manifest",
		"",
		"approved external execution timestamp manifest path",
	)
	command.Flags().DurationVar(
		&options.LockTimeout,
		"lock-timeout",
		migrations.DefaultMigrationLockTimeout,
		"maximum time to wait for the migration lock",
	)
	command.MarkFlagsMutuallyExclusive("down", "to")
	command.AddCommand(newMigrateRecoverDirtyCommand(migrateRecoverDirtyRuntime{}))
	return command
}

func runMigrate(
	ctx context.Context,
	options MigrateOptions,
	runtime migrateRuntime,
) error {
	runtime = runtime.withDefaults()
	if options.LockTimeout < 0 {
		return errors.New("migration lock timeout must be positive")
	}
	if options.Down && options.ToSet {
		return errors.New("--down and --to are mutually exclusive")
	}
	if options.Down && options.Check {
		return errors.New("--down and --check are mutually exclusive")
	}

	manifestPath := strings.TrimSpace(options.TimestampManifest)
	var manifest *types.ExternalExecutionTimestampManifest
	if manifestPath != "" {
		if options.Down || !options.ToSet || options.To != 138 {
			return errors.New(
				"--external-execution-timestamp-manifest requires explicit --to 138",
			)
		}
		decoded, err := readMigrationTimestampManifest(runtime, manifestPath)
		if err != nil {
			return err
		}
		manifest = &decoded
	}

	runOptions := migrations.RunOptions{
		Down:           options.Down,
		CheckOnly:      options.Check,
		ExpandManifest: manifest,
		LockTimeout:    options.LockTimeout,
	}
	if options.ToSet {
		target := options.To
		runOptions.Target = &target
	}
	runtime.Initialize()
	return runtime.Run(ctx, runOptions)
}

func readMigrationTimestampManifest(
	runtime migrateRuntime,
	path string,
) (types.ExternalExecutionTimestampManifest, error) {
	data, err := runtime.ReadFile(path)
	if err != nil {
		return types.ExternalExecutionTimestampManifest{},
			errors.New("read timestamp manifest failed")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest types.ExternalExecutionTimestampManifest
	if err := decoder.Decode(&manifest); err != nil {
		return types.ExternalExecutionTimestampManifest{},
			errors.New("invalid timestamp manifest JSON")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return types.ExternalExecutionTimestampManifest{},
			errors.New("timestamp manifest JSON must contain exactly one document")
	}
	return manifest, nil
}

func executeMigrations(
	ctx context.Context,
	options migrations.RunOptions,
) (finalErr error) {
	log := svc.NewLogger()
	defer func() {
		if err := log.Sync(); err != nil && !errors.Is(err, syscall.EINVAL) {
			finalErr = errors.Join(finalErr, fmt.Errorf("sync migration log: %w", err))
		}
	}()
	runner, err := migrations.Open(env.DatabaseUrl(), log)
	if err != nil {
		return err
	}
	defer func() {
		if err := runner.Close(); err != nil {
			finalErr = errors.Join(finalErr, fmt.Errorf("close migration runner: %w", err))
		}
	}()
	return runner.Run(ctx, options)
}

var MigrateCommand = NewMigrateCommand()

func init() {
	RootCommand.AddCommand(MigrateCommand)
}
