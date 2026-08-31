package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/env"
	"github.com/distr-sh/distr/internal/protectedhistory"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
)

type protectedHistoryRuntime struct {
	Stdout io.Writer
	Export func(context.Context, protectedhistory.Scope) (*protectedhistory.Artifact, error)
	Read   func(string) ([]byte, error)
	Write  func(string, []byte) error
}

func (runtime protectedHistoryRuntime) withDefaults() protectedHistoryRuntime {
	if runtime.Stdout == nil {
		runtime.Stdout = os.Stdout
	}
	if runtime.Export == nil {
		runtime.Export = exportProtectedHistory
	}
	if runtime.Read == nil {
		runtime.Read = os.ReadFile
	}
	if runtime.Write == nil {
		runtime.Write = writeProtectedHistoryArtifact
	}
	return runtime
}

func NewProtectedHistoryCommand() *cobra.Command {
	return newProtectedHistoryCommand(protectedHistoryRuntime{})
}

func newProtectedHistoryCommand(runtime protectedHistoryRuntime) *cobra.Command {
	runtime = runtime.withDefaults()
	command := &cobra.Command{
		Use:   "protected-history",
		Short: "export and compare sealed protected-history artifacts",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(newProtectedHistoryExportCommand(runtime))
	command.AddCommand(newProtectedHistoryCompareCommand(runtime))
	return command
}

func newProtectedHistoryExportCommand(runtime protectedHistoryRuntime) *cobra.Command {
	var organizationID string
	var customerOrganizationIDs []string
	var deploymentTargetIDs []string
	var output string
	command := &cobra.Command{
		Use:   "export",
		Short: "export one read-only repeatable-read protected-history snapshot",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if output == "" {
				return errors.New("output is required")
			}
			artifact, err := runtime.Export(command.Context(), protectedhistory.Scope{
				OrganizationID:          organizationID,
				CustomerOrganizationIDs: customerOrganizationIDs,
				DeploymentTargetIDs:     deploymentTargetIDs,
			})
			if err != nil {
				return err
			}
			payload, err := protectedhistory.Marshal(*artifact)
			if err != nil {
				return err
			}
			if err := runtime.Write(output, payload); err != nil {
				return fmt.Errorf("write protected history artifact: %w", err)
			}
			_, err = fmt.Fprintf(runtime.Stdout, "artifactId=%s records=%d output=%s\n",
				artifact.ArtifactID, artifact.RecordCount, output)
			return err
		},
	}
	command.Flags().StringVar(&organizationID, "organization-id", "", "owning organization UUID")
	command.Flags().StringSliceVar(
		&customerOrganizationIDs,
		"customer-organization-id",
		nil,
		"protected customer organization UUID (repeatable)",
	)
	command.Flags().StringSliceVar(
		&deploymentTargetIDs,
		"deployment-target-id",
		nil,
		"protected deployment target UUID (repeatable)",
	)
	command.Flags().StringVar(&output, "output", "", "exclusive output artifact path")
	return command
}

func newProtectedHistoryCompareCommand(runtime protectedHistoryRuntime) *cobra.Command {
	var baselinePath string
	var currentPath string
	command := &cobra.Command{
		Use:   "compare",
		Short: "compare two sealed artifacts and reject missing or modified baseline records",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if baselinePath == "" || currentPath == "" {
				return errors.New("baseline and current are required")
			}
			baselinePayload, err := runtime.Read(baselinePath)
			if err != nil {
				return fmt.Errorf("read baseline artifact: %w", err)
			}
			baseline, err := protectedhistory.Parse(baselinePayload)
			if err != nil {
				return fmt.Errorf("parse baseline artifact: %w", err)
			}
			currentPayload, err := runtime.Read(currentPath)
			if err != nil {
				return fmt.Errorf("read current artifact: %w", err)
			}
			current, err := protectedhistory.Parse(currentPayload)
			if err != nil {
				return fmt.Errorf("parse current artifact: %w", err)
			}
			comparison, err := protectedhistory.Compare(*baseline, *current)
			if err != nil {
				return err
			}
			encoder := json.NewEncoder(runtime.Stdout)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(comparison); err != nil {
				return err
			}
			if comparison.Status == protectedhistory.ComparisonViolation {
				return errors.New("protected history comparison found a violation")
			}
			return nil
		},
	}
	command.Flags().StringVar(&baselinePath, "baseline", "", "sealed baseline artifact path")
	command.Flags().StringVar(&currentPath, "current", "", "sealed current artifact path")
	return command
}

func exportProtectedHistory(
	ctx context.Context,
	scope protectedhistory.Scope,
) (*protectedhistory.Artifact, error) {
	env.Initialize()
	config, err := pgxpool.ParseConfig(env.DatabaseUrl())
	if err != nil {
		return nil, fmt.Errorf("parse database configuration: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return db.ExportProtectedHistory(internalctx.WithDb(ctx, pool), scope)
}

func writeProtectedHistoryArtifact(path string, payload []byte) (finalErr error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			finalErr = errors.Join(finalErr, err)
		}
		if finalErr != nil {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(payload); err != nil {
		return err
	}
	return file.Sync()
}

func init() {
	RootCommand.AddCommand(NewProtectedHistoryCommand())
}
