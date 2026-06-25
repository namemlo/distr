package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/env"
	"github.com/distr-sh/distr/internal/jobs"
	"github.com/distr-sh/distr/internal/svc"
	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/util"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

type backfillLegacyDeploymentsRuntime struct {
	Stdout io.Writer
	Stderr io.Writer
	Run    func(context.Context, types.DeploymentCompatibilityBackfillRequest) (*types.DeploymentCompatibilityBackfillReport, error)
}

type backfillLegacyDeploymentsOptions struct {
	OrganizationID   string
	Apply            bool
	BatchSize        int
	CursorCreatedAt  string
	CursorRevisionID string
}

func NewBackfillLegacyDeploymentsCommand() *cobra.Command {
	return newBackfillLegacyDeploymentsCommand(backfillLegacyDeploymentsRuntime{})
}

func newBackfillLegacyDeploymentsCommand(runtime backfillLegacyDeploymentsRuntime) *cobra.Command {
	if runtime.Stdout == nil {
		runtime.Stdout = os.Stdout
	}
	if runtime.Stderr == nil {
		runtime.Stderr = os.Stderr
	}
	if runtime.Run == nil {
		runtime.Run = runBackfillLegacyDeployments
	}
	opts := backfillLegacyDeploymentsOptions{BatchSize: 500}
	cmd := &cobra.Command{
		Use:   "backfill-legacy-deployments",
		Short: "backfill compatibility metadata for legacy direct deployments",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			request, err := resolveBackfillLegacyDeploymentsRequest(opts)
			if err != nil {
				return err
			}
			report, err := runtime.Run(cmd.Context(), request)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(
				runtime.Stdout,
				"dryRun=%t scanned=%d eligible=%d projected=%d alreadyPresent=%d skipped=%d failed=%d lastCursor=%s\n",
				!request.Apply,
				report.Scanned,
				report.Eligible,
				report.Projected,
				report.AlreadyPresent,
				report.Skipped,
				report.Failed,
				formatDeploymentCompatibilityCursor(report.LastCursor),
			)
			return err
		},
	}
	cmd.Flags().StringVar(&opts.OrganizationID, "organization-id", "", "organization id to backfill")
	cmd.Flags().BoolVar(&opts.Apply, "apply", false, "write compatibility metadata; omitted means dry-run")
	cmd.Flags().IntVar(&opts.BatchSize, "batch-size", opts.BatchSize, "maximum deployment revisions to scan")
	cmd.Flags().StringVar(&opts.CursorCreatedAt, "cursor-created-at", "", "resume cursor created_at timestamp")
	cmd.Flags().StringVar(&opts.CursorRevisionID, "cursor-revision-id", "", "resume cursor deployment revision id")
	return cmd
}

func init() {
	RootCommand.AddCommand(NewBackfillLegacyDeploymentsCommand())
}

func resolveBackfillLegacyDeploymentsRequest(
	opts backfillLegacyDeploymentsOptions,
) (types.DeploymentCompatibilityBackfillRequest, error) {
	orgID, err := uuid.Parse(opts.OrganizationID)
	if err != nil {
		return types.DeploymentCompatibilityBackfillRequest{}, fmt.Errorf("organization-id is required: %w", err)
	}
	if opts.BatchSize <= 0 {
		return types.DeploymentCompatibilityBackfillRequest{}, fmt.Errorf("batch-size must be positive")
	}
	request := types.DeploymentCompatibilityBackfillRequest{
		OrganizationID: orgID,
		Apply:          opts.Apply,
		BatchSize:      opts.BatchSize,
	}
	if opts.CursorCreatedAt != "" || opts.CursorRevisionID != "" {
		if opts.CursorCreatedAt == "" || opts.CursorRevisionID == "" {
			return types.DeploymentCompatibilityBackfillRequest{}, fmt.Errorf("cursor-created-at and cursor-revision-id must be provided together")
		}
		createdAt, err := time.Parse(time.RFC3339Nano, opts.CursorCreatedAt)
		if err != nil {
			return types.DeploymentCompatibilityBackfillRequest{}, fmt.Errorf("parse cursor-created-at: %w", err)
		}
		revisionID, err := uuid.Parse(opts.CursorRevisionID)
		if err != nil {
			return types.DeploymentCompatibilityBackfillRequest{}, fmt.Errorf("parse cursor-revision-id: %w", err)
		}
		request.Cursor = &types.DeploymentCompatibilityCursor{
			CreatedAt:                  createdAt,
			LegacyDeploymentRevisionID: revisionID,
		}
	}
	return request, nil
}

func runBackfillLegacyDeployments(
	ctx context.Context,
	request types.DeploymentCompatibilityBackfillRequest,
) (*types.DeploymentCompatibilityBackfillReport, error) {
	env.Initialize()
	registry := util.Require(svc.NewDefault(ctx))
	defer func() { util.Must(registry.Shutdown(ctx)) }()
	ctx = internalctx.WithDb(ctx, registry.GetDbPool())
	return jobs.RunDeploymentCompatibilityBackfill(ctx, request)
}

func formatDeploymentCompatibilityCursor(cursor *types.DeploymentCompatibilityCursor) string {
	if cursor == nil {
		return ""
	}
	return cursor.CreatedAt.Format(time.RFC3339Nano) + "/" + cursor.LegacyDeploymentRevisionID.String()
}
