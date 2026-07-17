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
	"time"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/db/queryable"
	"github.com/distr-sh/distr/internal/externalexecutiontimestamp"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
)

type externalExecutionTimestampPool interface {
	queryable.Queryable
	Close()
}

type externalExecutionTimestampCommandRuntime struct {
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
	Getenv   func(string) string
	Now      func() time.Time
	NewPool  func(context.Context, string) (externalExecutionTimestampPool, error)
	Inspect  func(context.Context) (*types.ExternalExecutionTimestampManifest, error)
	Validate func(
		context.Context,
		types.ExternalExecutionTimestampManifest,
	) (*types.ExternalExecutionTimestampValidationReport, error)
	Apply func(
		context.Context,
		types.ExternalExecutionTimestampApplyRequest,
	) (*types.ExternalExecutionTimestampApplyReport, error)
	Verify     func(context.Context, uuid.UUID) (*types.ExternalExecutionTimestampVerificationReport, error)
	Readiness  func(context.Context) (*types.ExternalExecutionTimestampReadiness, error)
	OpenOutput func(string, int, os.FileMode) (io.WriteCloser, error)
	Remove     func(string) error
}

func (runtime externalExecutionTimestampCommandRuntime) withDefaults() externalExecutionTimestampCommandRuntime {
	if runtime.Stdin == nil {
		runtime.Stdin = os.Stdin
	}
	if runtime.Stdout == nil {
		runtime.Stdout = os.Stdout
	}
	if runtime.Stderr == nil {
		runtime.Stderr = os.Stderr
	}
	if runtime.Getenv == nil {
		runtime.Getenv = os.Getenv
	}
	if runtime.Now == nil {
		runtime.Now = time.Now
	}
	if runtime.NewPool == nil {
		runtime.NewPool = func(
			ctx context.Context,
			databaseURL string,
		) (externalExecutionTimestampPool, error) {
			return pgxpool.New(ctx, databaseURL)
		}
	}
	if runtime.Inspect == nil {
		runtime.Inspect = db.InspectExternalExecutionTimestamps
	}
	if runtime.Validate == nil {
		runtime.Validate = db.ValidateExternalExecutionTimestampManifest
	}
	if runtime.Apply == nil {
		runtime.Apply = db.ApplyExternalExecutionTimestampManifest
	}
	if runtime.Verify == nil {
		runtime.Verify = db.VerifyExternalExecutionTimestampManifest
	}
	if runtime.Readiness == nil {
		runtime.Readiness = db.CheckExternalExecutionTimestampExpandReadiness
	}
	if runtime.OpenOutput == nil {
		runtime.OpenOutput = func(
			path string,
			flags int,
			mode os.FileMode,
		) (io.WriteCloser, error) {
			return os.OpenFile(path, flags, mode)
		}
	}
	if runtime.Remove == nil {
		runtime.Remove = os.Remove
	}
	return runtime
}

func NewExternalExecutionTimestampsCommand() *cobra.Command {
	return newExternalExecutionTimestampsCommand(
		externalExecutionTimestampCommandRuntime{},
	)
}

func newExternalExecutionTimestampsCommand(
	runtime externalExecutionTimestampCommandRuntime,
) *cobra.Command {
	runtime = runtime.withDefaults()
	command := &cobra.Command{
		Use:           "external-execution-timestamps",
		Short:         "inspect and validate external execution timestamp evidence",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	command.AddCommand(
		newExternalExecutionTimestampInspectCommand(runtime),
		newExternalExecutionTimestampSealCommand(runtime),
		newExternalExecutionTimestampValidateCommand(runtime),
		newExternalExecutionTimestampApplyCommand(runtime),
		newExternalExecutionTimestampVerifyCommand(runtime),
		newExternalExecutionTimestampReadinessCommand(runtime),
	)
	return command
}

func newExternalExecutionTimestampInspectCommand(
	runtime externalExecutionTimestampCommandRuntime,
) *cobra.Command {
	var output string
	command := &cobra.Command{
		Use:   "inspect",
		Short: "inspect a complete read-only timestamp snapshot",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if strings.TrimSpace(output) == "" {
				return errors.New("--output is required")
			}
			ctx, pool, err := externalExecutionTimestampDirectPoolContext(
				command.Context(), runtime,
			)
			if err != nil {
				return err
			}
			defer pool.Close()
			manifest, err := runtime.Inspect(ctx)
			if err != nil {
				return errors.New("external execution timestamp inspection failed")
			}
			return writeExternalExecutionTimestampJSON(runtime, output, manifest)
		},
	}
	command.Flags().StringVar(&output, "output", "", "draft manifest path, or - for stdout")
	return command
}

type externalExecutionTimestampSealFlags struct {
	Input             string
	Output            string
	Author            string
	Reviewer          string
	EvidenceReference string
	EvidenceChecksum  string
	TargetCommit      string
	TargetImageDigest string
}

func newExternalExecutionTimestampSealCommand(
	runtime externalExecutionTimestampCommandRuntime,
) *cobra.Command {
	flags := externalExecutionTimestampSealFlags{}
	command := &cobra.Command{
		Use:   "seal-manifest",
		Short: "seal a reviewed manifest without database access",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			manifest, err := readExternalExecutionTimestampManifest(
				runtime, flags.Input,
			)
			if err != nil {
				return err
			}
			if strings.TrimSpace(flags.Output) == "" {
				return errors.New("--output is required")
			}
			options, err := externalExecutionTimestampSealOptions(runtime, flags)
			if err != nil {
				return err
			}
			sealed, err := externalexecutiontimestamp.SealManifest(
				manifest, options, runtime.Now(),
			)
			if err != nil {
				return errors.New("manifest sealing failed")
			}
			return writeExternalExecutionTimestampJSON(runtime, flags.Output, sealed)
		},
	}
	command.Flags().StringVar(&flags.Input, "input", "", "reviewed draft path, or - for stdin")
	command.Flags().StringVar(&flags.Output, "output", "", "approved manifest path, or - for stdout")
	command.Flags().StringVar(&flags.Author, "author", "", "release author identity")
	command.Flags().StringVar(&flags.Reviewer, "reviewer", "", "independent reviewer identity")
	command.Flags().StringVar(&flags.EvidenceReference, "evidence-reference", "", "evidence bundle reference")
	command.Flags().StringVar(&flags.EvidenceChecksum, "evidence-checksum", "", "evidence bundle checksum")
	command.Flags().StringVar(&flags.TargetCommit, "target-commit", "", "target release commit")
	command.Flags().StringVar(&flags.TargetImageDigest, "target-image-digest", "", "target image digest")
	return command
}

func newExternalExecutionTimestampValidateCommand(
	runtime externalExecutionTimestampCommandRuntime,
) *cobra.Command {
	var manifestPath string
	command := &cobra.Command{
		Use:   "validate-manifest",
		Short: "validate a root manifest against a live read-only snapshot",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			manifest, err := readExternalExecutionTimestampManifest(runtime, manifestPath)
			if err != nil {
				return err
			}
			if manifest.SupersedesManifestID != nil {
				return errors.New(
					"superseding manifest live validation requires verified-tip provenance; use the apply workflow",
				)
			}
			ctx, pool, err := externalExecutionTimestampDirectPoolContext(
				command.Context(), runtime,
			)
			if err != nil {
				return err
			}
			defer pool.Close()
			report, err := runtime.Validate(ctx, manifest)
			if err != nil {
				return errors.New("manifest validation failed")
			}
			return writeExternalExecutionTimestampJSON(runtime, "-", report)
		},
	}
	command.Flags().StringVar(&manifestPath, "manifest", "", "manifest path, or - for stdin")
	return command
}

type externalExecutionTimestampApplyFlags struct {
	ManifestPath     string
	Apply            bool
	WriterFenceID    string
	BackupReference  string
	BackupChecksum   string
	RestoreReference string
	RestoreChecksum  string
}

func newExternalExecutionTimestampApplyCommand(
	runtime externalExecutionTimestampCommandRuntime,
) *cobra.Command {
	flags := externalExecutionTimestampApplyFlags{}
	command := &cobra.Command{
		Use:   "apply",
		Short: "dry-run or atomically apply approved timestamp evidence",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if !flags.Apply {
				for _, name := range []string{
					"writer-fence-id",
					"backup-reference",
					"backup-checksum",
					"restore-reference",
					"restore-checksum",
				} {
					if command.Flags().Changed(name) {
						return fmt.Errorf("--apply is required with --%s", name)
					}
				}
			}
			manifest, err := readExternalExecutionTimestampManifest(
				runtime,
				flags.ManifestPath,
			)
			if err != nil {
				return err
			}
			request := types.ExternalExecutionTimestampApplyRequest{
				Manifest: manifest,
				Apply:    flags.Apply,
			}
			if flags.Apply {
				request, err = externalExecutionTimestampApplyRequestEvidence(
					runtime,
					flags,
					request,
				)
				if err != nil {
					return err
				}
			}
			ctx, pool, err := externalExecutionTimestampDirectPoolContext(
				command.Context(),
				runtime,
			)
			if err != nil {
				return err
			}
			defer pool.Close()
			report, err := runtime.Apply(ctx, request)
			if err != nil {
				return errors.New("manifest apply failed")
			}
			return writeExternalExecutionTimestampJSON(runtime, "-", report)
		},
	}
	command.Flags().StringVar(
		&flags.ManifestPath,
		"manifest",
		"",
		"approved manifest path, or - for stdin",
	)
	command.Flags().BoolVar(
		&flags.Apply,
		"apply",
		false,
		"perform the atomic mutation after a successful preflight",
	)
	command.Flags().StringVar(
		&flags.WriterFenceID,
		"writer-fence-id",
		"",
		"writer fence evidence identifier",
	)
	command.Flags().StringVar(
		&flags.BackupReference,
		"backup-reference",
		"",
		"backup evidence reference",
	)
	command.Flags().StringVar(
		&flags.BackupChecksum,
		"backup-checksum",
		"",
		"backup evidence checksum",
	)
	command.Flags().StringVar(
		&flags.RestoreReference,
		"restore-reference",
		"",
		"restore verification reference",
	)
	command.Flags().StringVar(
		&flags.RestoreChecksum,
		"restore-checksum",
		"",
		"restore verification checksum",
	)
	return command
}

func externalExecutionTimestampApplyRequestEvidence(
	runtime externalExecutionTimestampCommandRuntime,
	flags externalExecutionTimestampApplyFlags,
	request types.ExternalExecutionTimestampApplyRequest,
) (types.ExternalExecutionTimestampApplyRequest, error) {
	type evidenceField struct {
		flagName string
		envName  string
		value    string
		assign   func(string)
	}
	fields := []evidenceField{
		{
			flagName: "--writer-fence-id",
			envName:  "DISTR_TIMESTAMP_FENCE_ID",
			value:    flags.WriterFenceID,
			assign: func(value string) {
				request.WriterFenceIdentifier = value
			},
		},
		{
			flagName: "--backup-reference",
			envName:  "DISTR_TIMESTAMP_BACKUP_REFERENCE",
			value:    flags.BackupReference,
			assign: func(value string) {
				request.BackupReference = value
			},
		},
		{
			flagName: "--backup-checksum",
			envName:  "DISTR_TIMESTAMP_BACKUP_CHECKSUM",
			value:    flags.BackupChecksum,
			assign: func(value string) {
				request.BackupChecksum = value
			},
		},
		{
			flagName: "--restore-reference",
			envName:  "DISTR_TIMESTAMP_RESTORE_REFERENCE",
			value:    flags.RestoreReference,
			assign: func(value string) {
				request.RestoreVerificationReference = value
			},
		},
		{
			flagName: "--restore-checksum",
			envName:  "DISTR_TIMESTAMP_RESTORE_CHECKSUM",
			value:    flags.RestoreChecksum,
			assign: func(value string) {
				request.RestoreVerificationChecksum = value
			},
		},
	}
	for _, field := range fields {
		value := strings.TrimSpace(field.value)
		if value == "" {
			value = strings.TrimSpace(runtime.Getenv(field.envName))
		}
		if value == "" {
			return request, fmt.Errorf(
				"%s or %s is required with --apply",
				field.flagName,
				field.envName,
			)
		}
		field.assign(value)
	}
	return request, nil
}

func newExternalExecutionTimestampVerifyCommand(
	runtime externalExecutionTimestampCommandRuntime,
) *cobra.Command {
	var manifestIDText string
	command := &cobra.Command{
		Use:   "verify",
		Short: "verify stored timestamp provenance against current paired writes",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			manifestID, err := uuid.Parse(strings.TrimSpace(manifestIDText))
			if err != nil || manifestID == uuid.Nil {
				return errors.New("a valid manifest id is required via --manifest-id")
			}
			ctx, pool, err := externalExecutionTimestampDirectPoolContext(
				command.Context(),
				runtime,
			)
			if err != nil {
				return err
			}
			defer pool.Close()
			report, err := runtime.Verify(ctx, manifestID)
			if err != nil {
				return errors.New("manifest verification failed")
			}
			return writeExternalExecutionTimestampJSON(runtime, "-", report)
		},
	}
	command.Flags().StringVar(
		&manifestIDText,
		"manifest-id",
		"",
		"verified manifest UUID",
	)
	return command
}

func newExternalExecutionTimestampReadinessCommand(
	runtime externalExecutionTimestampCommandRuntime,
) *cobra.Command {
	return &cobra.Command{
		Use:   "readiness",
		Short: "verify the external execution timestamp expand lifecycle",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			ctx, pool, err := externalExecutionTimestampDirectPoolContext(
				command.Context(),
				runtime,
			)
			if err != nil {
				return err
			}
			defer pool.Close()
			report, err := runtime.Readiness(ctx)
			if err != nil {
				return fmt.Errorf(
					"external execution timestamp readiness failed: %w",
					err,
				)
			}
			return writeExternalExecutionTimestampJSON(runtime, "-", report)
		},
	}
}

func externalExecutionTimestampDirectPoolContext(
	ctx context.Context,
	runtime externalExecutionTimestampCommandRuntime,
) (context.Context, externalExecutionTimestampPool, error) {
	databaseURL := strings.TrimSpace(runtime.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return nil, nil, errors.New("DATABASE_URL is required")
	}
	pool, err := runtime.NewPool(ctx, databaseURL)
	if err != nil {
		if pool != nil {
			pool.Close()
		}
		return nil, nil, errors.New("open direct database pool failed")
	}
	if pool == nil {
		return nil, nil, errors.New("open direct database pool failed")
	}
	return internalctx.WithDb(ctx, pool), pool, nil
}

func externalExecutionTimestampSealOptions(
	runtime externalExecutionTimestampCommandRuntime,
	flags externalExecutionTimestampSealFlags,
) (types.ExternalExecutionTimestampSealOptions, error) {
	type field struct {
		name string
		env  string
		flag string
	}
	fields := []field{
		{name: "--author", env: "DISTR_TIMESTAMP_AUTHOR", flag: flags.Author},
		{name: "--reviewer", env: "DISTR_TIMESTAMP_REVIEWER", flag: flags.Reviewer},
		{name: "--evidence-reference", env: "DISTR_TIMESTAMP_EVIDENCE_REFERENCE", flag: flags.EvidenceReference},
		{name: "--evidence-checksum", env: "DISTR_TIMESTAMP_EVIDENCE_CHECKSUM", flag: flags.EvidenceChecksum},
		{name: "--target-commit", env: "DISTR_RELEASE_COMMIT", flag: flags.TargetCommit},
		{name: "--target-image-digest", env: "DISTR_IMAGE_DIGEST", flag: flags.TargetImageDigest},
	}
	values := make([]string, len(fields))
	for index, item := range fields {
		values[index] = strings.TrimSpace(item.flag)
		if values[index] == "" {
			values[index] = strings.TrimSpace(runtime.Getenv(item.env))
		}
		if values[index] == "" {
			return types.ExternalExecutionTimestampSealOptions{}, fmt.Errorf(
				"%s or %s is required", item.name, item.env,
			)
		}
	}
	return types.ExternalExecutionTimestampSealOptions{
		AuthorIdentity:          values[0],
		ReviewerIdentity:        values[1],
		EvidenceBundleReference: values[2],
		EvidenceBundleChecksum:  values[3],
		TargetReleaseCommit:     values[4],
		TargetImageDigest:       values[5],
	}, nil
}

func readExternalExecutionTimestampManifest(
	runtime externalExecutionTimestampCommandRuntime,
	path string,
) (types.ExternalExecutionTimestampManifest, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return types.ExternalExecutionTimestampManifest{}, errors.New("manifest input path is required")
	}
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(runtime.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return types.ExternalExecutionTimestampManifest{}, errors.New("read manifest input failed")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest types.ExternalExecutionTimestampManifest
	if err := decoder.Decode(&manifest); err != nil {
		return types.ExternalExecutionTimestampManifest{}, errors.New("invalid manifest JSON")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return types.ExternalExecutionTimestampManifest{}, errors.New(
			"manifest JSON must contain exactly one document",
		)
	}
	return manifest, nil
}

func writeExternalExecutionTimestampJSON(
	runtime externalExecutionTimestampCommandRuntime,
	path string,
	value any,
) error {
	path = strings.TrimSpace(path)
	if path == "-" {
		if err := encodeExternalExecutionTimestampJSON(runtime.Stdout, value); err != nil {
			return errors.New("write manifest JSON failed")
		}
		return nil
	}
	writer, err := runtime.OpenOutput(
		path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600,
	)
	if err != nil {
		return fmt.Errorf("create new output: %w", err)
	}
	if err := encodeExternalExecutionTimestampJSON(writer, value); err != nil {
		_ = writer.Close()
		removeErr := runtime.Remove(path)
		return errors.Join(
			errors.New("write manifest JSON failed"),
			redactedPartialOutputRemovalError(removeErr),
		)
	}
	if err := writer.Close(); err != nil {
		removeErr := runtime.Remove(path)
		return errors.Join(
			errors.New("close manifest output failed"),
			redactedPartialOutputRemovalError(removeErr),
		)
	}
	return nil
}

func redactedPartialOutputRemovalError(err error) error {
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return errors.New("remove partial manifest output failed")
}

func encodeExternalExecutionTimestampJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func init() {
	RootCommand.AddCommand(NewExternalExecutionTimestampsCommand())
}
