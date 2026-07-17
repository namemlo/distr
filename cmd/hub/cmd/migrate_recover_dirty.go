package cmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/distr-sh/distr/internal/env"
	"github.com/distr-sh/distr/internal/externalexecutiontimestamp"
	"github.com/distr-sh/distr/internal/migrations"
	"github.com/distr-sh/distr/internal/svc"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

type migrateRecoverDirtyRunner interface {
	PlanTimestampDirtyRecovery(
		context.Context,
		migrations.TimestampDirtyRecoveryPlanOptions,
	) (types.TimestampDirtyRecoveryPlan, error)
	ApplyTimestampDirtyRecovery(
		context.Context,
		types.TimestampDirtyRecoveryPlan,
		migrations.TimestampDirtyRecoveryApplyOptions,
	) (types.TimestampDirtyRecoveryResult, error)
	Close() error
}

type migrateRecoverDirtyRuntime struct {
	Initialize      func()
	OpenRunner      func() (migrateRecoverDirtyRunner, error)
	ReadFile        func(string) ([]byte, error)
	ReserveEvidence func(
		string,
		uuid.UUID,
	) (migrateRecoveryEvidenceReservation, error)
	Stdout io.Writer
}

func (runtime migrateRecoverDirtyRuntime) withDefaults() migrateRecoverDirtyRuntime {
	if runtime.Initialize == nil {
		runtime.Initialize = env.Initialize
	}
	if runtime.OpenRunner == nil {
		runtime.OpenRunner = openMigrateRecoverDirtyRunner
	}
	if runtime.ReadFile == nil {
		runtime.ReadFile = os.ReadFile
	}
	if runtime.ReserveEvidence == nil {
		runtime.ReserveEvidence = reserveMigrateRecoveryEvidence
	}
	if runtime.Stdout == nil {
		runtime.Stdout = os.Stdout
	}
	return runtime
}

type managedMigrateRecoverDirtyRunner struct {
	*migrations.Runner
	log *zap.Logger
}

func openMigrateRecoverDirtyRunner() (migrateRecoverDirtyRunner, error) {
	log := svc.NewLogger()
	runner, err := migrations.Open(env.DatabaseUrl(), log)
	if err != nil {
		return nil, errors.Join(err, syncMigrateRecoverDirtyLog(log))
	}
	return &managedMigrateRecoverDirtyRunner{Runner: runner, log: log}, nil
}

func (runner *managedMigrateRecoverDirtyRunner) Close() error {
	return errors.Join(
		runner.Runner.Close(),
		syncMigrateRecoverDirtyLog(runner.log),
	)
}

func syncMigrateRecoverDirtyLog(log *zap.Logger) error {
	if log == nil {
		return nil
	}
	if err := log.Sync(); err != nil && !errors.Is(err, syscall.EINVAL) {
		return fmt.Errorf("sync migration recovery log: %w", err)
	}
	return nil
}

func newMigrateRecoverDirtyCommand(
	runtime migrateRecoverDirtyRuntime,
) *cobra.Command {
	runtime = runtime.withDefaults()
	command := &cobra.Command{
		Use:           "recover-dirty",
		Short:         "plan or apply an audited dirty migration recovery",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	command.AddCommand(
		newMigrateRecoverDirtyPlanCommand(runtime),
		newMigrateRecoverDirtyApplyCommand(runtime),
	)
	return command
}

type migrateRecoverDirtyPlanFlags struct {
	ExpectedDirtyVersion uint
	OperatorIdentity     string
	Reason               string
	WriterFenceID        string
	ManifestPath         string
	OutputPath           string
	LockTimeout          time.Duration
}

func newMigrateRecoverDirtyPlanCommand(
	runtime migrateRecoverDirtyRuntime,
) *cobra.Command {
	flags := migrateRecoverDirtyPlanFlags{
		LockTimeout: migrations.DefaultMigrationLockTimeout,
	}
	command := &cobra.Command{
		Use:   "plan",
		Short: "create an immutable dirty migration recovery plan",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runMigrateRecoverDirtyPlan(command.Context(), flags, runtime)
		},
	}
	command.Flags().UintVar(
		&flags.ExpectedDirtyVersion,
		"expected-dirty-version",
		0,
		"expected dirty migration version (137 or 138)",
	)
	command.Flags().StringVar(
		&flags.OperatorIdentity,
		"operator-identity",
		"",
		"operator identity recorded in the recovery plan",
	)
	command.Flags().StringVar(
		&flags.Reason,
		"reason",
		"",
		"single-line recovery reason",
	)
	command.Flags().StringVar(
		&flags.WriterFenceID,
		"writer-fence-id",
		"",
		"identifier of the active writer fence",
	)
	command.Flags().StringVar(
		&flags.ManifestPath,
		"external-execution-timestamp-manifest",
		"",
		"conditional approved timestamp manifest path",
	)
	command.Flags().StringVar(
		&flags.OutputPath,
		"output",
		"",
		"create-new recovery plan evidence path",
	)
	command.Flags().DurationVar(
		&flags.LockTimeout,
		"lock-timeout",
		migrations.DefaultMigrationLockTimeout,
		"maximum time to wait for recovery locks",
	)
	return command
}

func runMigrateRecoverDirtyPlan(
	ctx context.Context,
	flags migrateRecoverDirtyPlanFlags,
	runtime migrateRecoverDirtyRuntime,
) error {
	runtime = runtime.withDefaults()
	if flags.ExpectedDirtyVersion != 137 && flags.ExpectedDirtyVersion != 138 {
		return errors.New("--expected-dirty-version must be 137 or 138")
	}
	if strings.TrimSpace(flags.OperatorIdentity) == "" {
		return errors.New("--operator-identity is required")
	}
	if strings.TrimSpace(flags.Reason) == "" {
		return errors.New("--reason is required")
	}
	if strings.TrimSpace(flags.WriterFenceID) == "" {
		return errors.New("--writer-fence-id is required")
	}
	if err := validateMigrateRecoveryOutputPath(flags.OutputPath); err != nil {
		return err
	}
	if flags.LockTimeout <= 0 {
		return errors.New("--lock-timeout must be positive")
	}

	var manifest *types.ExternalExecutionTimestampManifest
	var manifestChecksum string
	var requestedManifestBinding *types.TimestampDirtyRecoveryManifestBinding
	if strings.TrimSpace(flags.ManifestPath) != "" {
		if flags.ManifestPath == "-" {
			return errors.New(
				"--external-execution-timestamp-manifest must be a file path",
			)
		}
		readManifest := readCanonicalMigrateRecoveryJSON[types.ExternalExecutionTimestampManifest]
		decoded, checksum, err := readManifest(
			runtime.ReadFile,
			flags.ManifestPath,
			"timestamp manifest",
		)
		if err != nil {
			return err
		}
		binding, err := validateMigrateRecoveryManifest(decoded, checksum)
		if err != nil {
			return err
		}
		manifest = &decoded
		manifestChecksum = checksum
		requestedManifestBinding = binding
	}

	runtime.Initialize()
	runner, err := runtime.OpenRunner()
	if err != nil {
		return err
	}
	plan, planErr := runner.PlanTimestampDirtyRecovery(
		ctx,
		migrations.TimestampDirtyRecoveryPlanOptions{
			ExpectedDirtyVersion:     flags.ExpectedDirtyVersion,
			OperatorIdentity:         flags.OperatorIdentity,
			Reason:                   flags.Reason,
			WriterFenceIdentifier:    flags.WriterFenceID,
			Manifest:                 manifest,
			ManifestDocumentChecksum: manifestChecksum,
			LockTimeout:              flags.LockTimeout,
		},
	)
	closeErr := runner.Close()
	if err := errors.Join(planErr, closeErr); err != nil {
		return err
	}
	if err := plan.Validate(); err != nil {
		return err
	}
	if err := validateMigrateRecoveryPlanBinding(
		plan,
		flags,
		requestedManifestBinding,
	); err != nil {
		return err
	}
	reservation, err := runtime.ReserveEvidence(
		flags.OutputPath,
		plan.RecoveryID,
	)
	if err != nil {
		return err
	}
	checksum, err := reservation.Publish(plan)
	if err != nil {
		return errors.Join(err, reservation.Cancel())
	}
	return writeMigrateRecoveryChecksum(runtime.Stdout, checksum)
}

type migrateRecoverDirtyApplyFlags struct {
	PlanPath     string
	PlanChecksum string
	WriterFence  string
	ManifestPath string
	OutputPath   string
	LockTimeout  time.Duration
}

func newMigrateRecoverDirtyApplyCommand(
	runtime migrateRecoverDirtyRuntime,
) *cobra.Command {
	flags := migrateRecoverDirtyApplyFlags{
		LockTimeout: migrations.DefaultMigrationLockTimeout,
	}
	command := &cobra.Command{
		Use:   "apply",
		Short: "apply an exact approved dirty migration recovery plan",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runMigrateRecoverDirtyApply(command.Context(), flags, runtime)
		},
	}
	command.Flags().StringVar(&flags.PlanPath, "plan", "", "recovery plan path")
	command.Flags().StringVar(
		&flags.PlanChecksum,
		"plan-checksum",
		"",
		"required exact raw recovery plan SHA-256",
	)
	command.Flags().StringVar(
		&flags.WriterFence,
		"writer-fence-id",
		"",
		"identifier of the active writer fence",
	)
	command.Flags().StringVar(
		&flags.ManifestPath,
		"external-execution-timestamp-manifest",
		"",
		"conditional approved timestamp manifest path",
	)
	command.Flags().StringVar(
		&flags.OutputPath,
		"output",
		"",
		"create-new recovery result evidence path",
	)
	command.Flags().DurationVar(
		&flags.LockTimeout,
		"lock-timeout",
		migrations.DefaultMigrationLockTimeout,
		"maximum time to wait for recovery locks",
	)
	return command
}

func runMigrateRecoverDirtyApply(
	ctx context.Context,
	flags migrateRecoverDirtyApplyFlags,
	runtime migrateRecoverDirtyRuntime,
) error {
	runtime = runtime.withDefaults()
	if strings.TrimSpace(flags.PlanPath) == "" {
		return errors.New("--plan is required")
	}
	if flags.PlanPath == "-" {
		return errors.New("--plan must be a file path")
	}
	if strings.TrimSpace(flags.PlanChecksum) == "" {
		return errors.New("--plan-checksum is required")
	}
	if strings.TrimSpace(flags.WriterFence) == "" {
		return errors.New("--writer-fence-id is required")
	}
	if err := validateMigrateRecoveryOutputPath(flags.OutputPath); err != nil {
		return err
	}
	if flags.LockTimeout <= 0 {
		return errors.New("--lock-timeout must be positive")
	}
	plan, actualPlanChecksum, err := readCanonicalMigrateRecoveryJSON[types.TimestampDirtyRecoveryPlan](
		runtime.ReadFile,
		flags.PlanPath,
		"recovery plan",
	)
	if err != nil {
		return err
	}
	if flags.PlanChecksum != actualPlanChecksum {
		return errors.New(
			"--plan-checksum does not match the exact recovery plan file",
		)
	}
	if err := plan.Validate(); err != nil {
		return err
	}
	if flags.WriterFence != plan.WriterFenceIdentifier {
		return errors.New("--writer-fence-id does not match the recovery plan")
	}

	manifestPathSupplied := strings.TrimSpace(flags.ManifestPath) != ""
	if (plan.Manifest != nil) != manifestPathSupplied {
		return errors.New(
			"--external-execution-timestamp-manifest is required if and only if the recovery plan binds a manifest",
		)
	}
	var manifest *types.ExternalExecutionTimestampManifest
	var manifestChecksum string
	if manifestPathSupplied {
		if flags.ManifestPath == "-" {
			return errors.New(
				"--external-execution-timestamp-manifest must be a file path",
			)
		}
		readManifest := readCanonicalMigrateRecoveryJSON[types.ExternalExecutionTimestampManifest]
		decoded, checksum, err := readManifest(
			runtime.ReadFile,
			flags.ManifestPath,
			"timestamp manifest",
		)
		if err != nil {
			return err
		}
		binding, err := validateMigrateRecoveryManifest(decoded, checksum)
		if err != nil {
			return err
		}
		if *binding != *plan.Manifest {
			return errors.New(
				"timestamp manifest does not match the recovery plan",
			)
		}
		manifest = &decoded
		manifestChecksum = checksum
	}

	// This reservation is intentionally acquired before environment
	// initialization or opening a database runner. Once Apply is invoked it is
	// never cancelled: any failure leaves a deterministic fail-closed marker.
	reservation, err := runtime.ReserveEvidence(
		flags.OutputPath,
		plan.RecoveryID,
	)
	if err != nil {
		return err
	}
	runtime.Initialize()
	runner, err := runtime.OpenRunner()
	if err != nil {
		return errors.Join(err, reservation.Cancel())
	}
	result, applyErr := runner.ApplyTimestampDirtyRecovery(
		ctx,
		plan,
		migrations.TimestampDirtyRecoveryApplyOptions{
			PlanDocumentChecksum:     actualPlanChecksum,
			WriterFenceIdentifier:    flags.WriterFence,
			Manifest:                 manifest,
			ManifestDocumentChecksum: manifestChecksum,
			LockTimeout:              flags.LockTimeout,
		},
	)
	closeErr := runner.Close()
	if err := errors.Join(applyErr, closeErr); err != nil {
		return errors.Join(err, reservation.FailClosed())
	}
	if err := result.Validate(); err != nil {
		return errors.Join(err, reservation.FailClosed())
	}
	if err := validateMigrateRecoveryResultBinding(
		plan,
		actualPlanChecksum,
		result,
	); err != nil {
		return errors.Join(err, reservation.FailClosed())
	}
	checksum, err := reservation.Publish(result)
	if err != nil {
		return err
	}
	return writeMigrateRecoveryChecksum(runtime.Stdout, checksum)
}

func validateMigrateRecoveryPlanBinding(
	plan types.TimestampDirtyRecoveryPlan,
	request migrateRecoverDirtyPlanFlags,
	manifest *types.TimestampDirtyRecoveryManifestBinding,
) error {
	if plan.ExpectedDirtyVersion != request.ExpectedDirtyVersion ||
		plan.OperatorIdentity != request.OperatorIdentity ||
		plan.Reason != request.Reason ||
		plan.WriterFenceIdentifier != request.WriterFenceID ||
		!migrateRecoveryManifestBindingsEqual(plan.Manifest, manifest) {
		return errors.New(
			"timestamp dirty recovery plan does not match the requested inputs",
		)
	}
	return nil
}

func migrateRecoveryManifestBindingsEqual(
	left *types.TimestampDirtyRecoveryManifestBinding,
	right *types.TimestampDirtyRecoveryManifestBinding,
) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func validateMigrateRecoveryResultBinding(
	plan types.TimestampDirtyRecoveryPlan,
	planChecksum string,
	result types.TimestampDirtyRecoveryResult,
) error {
	expectedStatus := types.TimestampDirtyRecoverySchemaStatus{
		Version: plan.ExpectedDirtyVersion,
		Dirty:   true,
	}
	if result.RecoveryID != plan.RecoveryID ||
		result.PlanChecksum != planChecksum ||
		result.PlannedStatus != expectedStatus ||
		result.ForcedVersion != plan.ForceVersion ||
		result.CatalogChecksum != plan.CatalogChecksum {
		return errors.New(
			"timestamp dirty recovery result does not match the recovery plan",
		)
	}
	return nil
}

func validateMigrateRecoveryOutputPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("--output is required")
	}
	if path == "-" {
		return errors.New("--output must be a create-new file path")
	}
	return nil
}

func readCanonicalMigrateRecoveryJSON[T any](
	readFile func(string) ([]byte, error),
	path string,
	label string,
) (T, string, error) {
	var value T
	data, err := readFile(path)
	if err != nil {
		return value, "", fmt.Errorf("read %s failed", label)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, "", fmt.Errorf("invalid %s JSON", label)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return value, "", fmt.Errorf(
			"%s JSON must contain exactly one document",
			label,
		)
	}
	canonical, checksum, err := encodeMigrateRecoveryEvidence(value)
	if err != nil {
		return value, "", err
	}
	if !bytes.Equal(data, canonical) {
		return value, "", fmt.Errorf(
			"%s must use canonical two-space JSON with one trailing newline",
			label,
		)
	}
	rawSum := sha256.Sum256(data)
	rawChecksum := "sha256:" + hex.EncodeToString(rawSum[:])
	if rawChecksum != checksum {
		return value, "", fmt.Errorf("%s checksum is not canonical", label)
	}
	return value, rawChecksum, nil
}

func validateMigrateRecoveryManifest(
	manifest types.ExternalExecutionTimestampManifest,
	documentChecksum string,
) (*types.TimestampDirtyRecoveryManifestBinding, error) {
	if manifest.State != types.ExternalExecutionTimestampManifestStateApproved {
		return nil, errors.New("timestamp manifest must be APPROVED")
	}
	if problems := externalexecutiontimestamp.ValidateManifestDocument(
		manifest,
	); len(problems) > 0 {
		return nil, fmt.Errorf(
			"timestamp manifest is invalid: %w",
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

func writeMigrateRecoveryChecksum(writer io.Writer, checksum string) error {
	if _, err := fmt.Fprintln(writer, checksum); err != nil {
		return errors.New("write recovery evidence checksum failed")
	}
	return nil
}
