package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"time"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/env"
	"github.com/distr-sh/distr/internal/svc"
	"github.com/distr-sh/distr/internal/targetconfig"
	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/util"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var v1ExtractionChecksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type backfillTargetConfigSnapshotsRuntime struct {
	Stdout io.Writer
	DryRun func(context.Context, uuid.UUID, uuid.UUID, *uuid.UUID, int) (*types.V1ExtractionReport, error)
	Apply  func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, int) (*types.V1ExtractionReport, error)
	Report func(context.Context, uuid.UUID, uuid.UUID) (*types.V1ExtractionReport, error)
}

type backfillTargetConfigSnapshotsOptions struct {
	OrganizationID          string
	ActorUserAccountID      string
	DryRun                  bool
	Apply                   bool
	Report                  string
	CheckpointID            string
	DryRunChecksum          string
	PredecessorCheckpointID string
	BatchSize               int
}

func NewBackfillTargetConfigSnapshotsCommand() *cobra.Command {
	return newBackfillTargetConfigSnapshotsCommand(backfillTargetConfigSnapshotsRuntime{})
}

func newBackfillTargetConfigSnapshotsCommand(
	runtime backfillTargetConfigSnapshotsRuntime,
) *cobra.Command {
	if runtime.Stdout == nil {
		runtime.Stdout = os.Stdout
	}
	if runtime.DryRun == nil {
		runtime.DryRun = runTargetConfigSnapshotV1DryRun
	}
	if runtime.Apply == nil {
		runtime.Apply = runTargetConfigSnapshotV1Apply
	}
	if runtime.Report == nil {
		runtime.Report = runTargetConfigSnapshotV1Report
	}
	options := backfillTargetConfigSnapshotsOptions{BatchSize: 100}
	command := &cobra.Command{
		Use:   "backfill-target-config-snapshots",
		Short: "derive immutable target config snapshots from unambiguous v1 history",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			organizationID, err := uuid.Parse(options.OrganizationID)
			if err != nil {
				return fmt.Errorf("organization-id is required: %w", err)
			}
			if options.BatchSize < 1 || options.BatchSize > 1000 {
				return fmt.Errorf("batch-size must be between 1 and 1000")
			}
			mode, checkpointID, predecessorCheckpointID, err := resolveTargetConfigV1BackfillMode(options)
			if err != nil {
				return err
			}
			actorUserAccountID, err := resolveTargetConfigV1BackfillActor(options, mode)
			if err != nil {
				return err
			}
			var report *types.V1ExtractionReport
			switch mode {
			case "dry-run":
				report, err = runtime.DryRun(
					command.Context(),
					organizationID,
					actorUserAccountID,
					predecessorCheckpointID,
					options.BatchSize,
				)
			case "apply":
				report, err = runtime.Apply(
					command.Context(),
					organizationID,
					actorUserAccountID,
					checkpointID,
					options.DryRunChecksum,
					options.BatchSize,
				)
			case "report":
				report, err = runtime.Report(command.Context(), organizationID, checkpointID)
			}
			if err != nil {
				return err
			}
			return writeTargetConfigV1BackfillReport(runtime.Stdout, mode, report)
		},
	}
	command.Flags().StringVar(&options.OrganizationID, "organization-id", "", "organization id to scan")
	command.Flags().StringVar(
		&options.ActorUserAccountID,
		"actor-user-account-id",
		"",
		"organization-member user account approving and creating snapshots",
	)
	command.Flags().BoolVar(&options.DryRun, "dry-run", false, "create or reuse deterministic dry-run evidence")
	command.Flags().BoolVar(&options.Apply, "apply", false, "apply one approved dry-run checkpoint")
	command.Flags().StringVar(&options.Report, "report", "", "print the persisted report for a checkpoint id")
	command.Flags().StringVar(&options.CheckpointID, "checkpoint-id", "", "approved dry-run checkpoint id")
	command.Flags().StringVar(
		&options.DryRunChecksum,
		"dry-run-checksum",
		"",
		"approved lowercase SHA-256 dry-run checksum",
	)
	command.Flags().StringVar(
		&options.PredecessorCheckpointID,
		"predecessor-checkpoint-id",
		"",
		"fully applied predecessor checkpoint for the next bounded source batch",
	)
	command.Flags().IntVar(&options.BatchSize, "batch-size", options.BatchSize, "stable processing batch size")
	return command
}

func resolveTargetConfigV1BackfillMode(
	options backfillTargetConfigSnapshotsOptions,
) (string, uuid.UUID, *uuid.UUID, error) {
	selected := 0
	if options.DryRun {
		selected++
	}
	if options.Apply {
		selected++
	}
	if options.Report != "" {
		selected++
	}
	if selected > 1 {
		return "", uuid.Nil, nil, fmt.Errorf("dry-run, apply, and report are mutually exclusive")
	}
	if options.Apply {
		if options.PredecessorCheckpointID != "" {
			return "", uuid.Nil, nil, fmt.Errorf(
				"predecessor-checkpoint-id is valid only with dry-run",
			)
		}
		checkpointID, err := uuid.Parse(options.CheckpointID)
		if err != nil {
			return "", uuid.Nil, nil, fmt.Errorf("checkpoint-id is required for apply: %w", err)
		}
		if !v1ExtractionChecksumPattern.MatchString(options.DryRunChecksum) {
			return "", uuid.Nil, nil, fmt.Errorf("dry-run-checksum is required for apply")
		}
		return "apply", checkpointID, nil, nil
	}
	if options.Report != "" {
		if options.CheckpointID != "" || options.DryRunChecksum != "" ||
			options.PredecessorCheckpointID != "" {
			return "", uuid.Nil, nil, fmt.Errorf(
				"checkpoint-id, dry-run-checksum, and predecessor-checkpoint-id are invalid with report",
			)
		}
		checkpointID, err := uuid.Parse(options.Report)
		if err != nil {
			return "", uuid.Nil, nil, fmt.Errorf("report must be a checkpoint id: %w", err)
		}
		return "report", checkpointID, nil, nil
	}
	if options.CheckpointID != "" || options.DryRunChecksum != "" {
		return "", uuid.Nil, nil, fmt.Errorf("checkpoint-id and dry-run-checksum are valid only with apply")
	}
	if options.PredecessorCheckpointID == "" {
		return "dry-run", uuid.Nil, nil, nil
	}
	predecessorCheckpointID, err := uuid.Parse(options.PredecessorCheckpointID)
	if err != nil {
		return "", uuid.Nil, nil, fmt.Errorf(
			"predecessor-checkpoint-id must be a UUID: %w",
			err,
		)
	}
	return "dry-run", uuid.Nil, &predecessorCheckpointID, nil
}

func resolveTargetConfigV1BackfillActor(
	options backfillTargetConfigSnapshotsOptions,
	mode string,
) (uuid.UUID, error) {
	if mode == "report" {
		if options.ActorUserAccountID != "" {
			return uuid.Nil, fmt.Errorf("actor-user-account-id is invalid with report")
		}
		return uuid.Nil, nil
	}
	actorUserAccountID, err := uuid.Parse(options.ActorUserAccountID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("actor-user-account-id is required: %w", err)
	}
	return actorUserAccountID, nil
}

func writeTargetConfigV1BackfillReport(
	writer io.Writer,
	mode string,
	report *types.V1ExtractionReport,
) error {
	if report == nil {
		return fmt.Errorf("backfill returned no report")
	}
	predecessorCheckpointID := ""
	if report.Checkpoint.PredecessorCheckpointID != nil {
		predecessorCheckpointID = report.Checkpoint.PredecessorCheckpointID.String()
	}
	sourceMembershipCheckpointID := report.Checkpoint.ID.String()
	if report.Checkpoint.SourceMembershipCheckpointID != nil {
		sourceMembershipCheckpointID = report.Checkpoint.SourceMembershipCheckpointID.String()
	}
	sourceAfterCreatedAt := ""
	if report.Checkpoint.SourceAfterCreatedAt != nil {
		sourceAfterCreatedAt = report.Checkpoint.SourceAfterCreatedAt.UTC().Format(time.RFC3339Nano)
	}
	sourceAfterPlanID := ""
	if report.Checkpoint.SourceAfterPlanID != nil {
		sourceAfterPlanID = report.Checkpoint.SourceAfterPlanID.String()
	}
	sourceThroughCreatedAt := ""
	if report.Checkpoint.SourceThroughCreatedAt != nil {
		sourceThroughCreatedAt = report.Checkpoint.SourceThroughCreatedAt.UTC().Format(time.RFC3339Nano)
	}
	sourceThroughPlanID := ""
	if report.Checkpoint.SourceThroughPlanID != nil {
		sourceThroughPlanID = report.Checkpoint.SourceThroughPlanID.String()
	}
	sourceHighWaterCreatedAt := ""
	if report.Checkpoint.SourceHighWaterCreatedAt != nil {
		sourceHighWaterCreatedAt = report.Checkpoint.SourceHighWaterCreatedAt.UTC().Format(time.RFC3339Nano)
	}
	sourceHighWaterPlanID := ""
	if report.Checkpoint.SourceHighWaterPlanID != nil {
		sourceHighWaterPlanID = report.Checkpoint.SourceHighWaterPlanID.String()
	}
	_, err := fmt.Fprintf(
		writer,
		"mode=%s checkpointId=%s predecessorCheckpointId=%s sourceMembershipCheckpointId=%s sourceMembershipChecksum=%s actorUserAccountId=%s dryRunChecksum=%s sourceStateChecksum=%s sourceAfterCreatedAt=%s sourceAfterPlanId=%s sourceThroughCreatedAt=%s sourceThroughPlanId=%s sourceHighWaterCreatedAt=%s sourceHighWaterPlanId=%s hasMore=%t source=%d candidate=%d blocked=%d applied=%d pending=%d noOp=%d\n",
		mode,
		report.Checkpoint.ID,
		predecessorCheckpointID,
		sourceMembershipCheckpointID,
		report.Checkpoint.SourceMembershipChecksum,
		report.Checkpoint.ActorUserAccountID,
		report.Checkpoint.DryRunChecksum,
		report.Checkpoint.SourceStateChecksum,
		sourceAfterCreatedAt,
		sourceAfterPlanID,
		sourceThroughCreatedAt,
		sourceThroughPlanID,
		sourceHighWaterCreatedAt,
		sourceHighWaterPlanID,
		report.Checkpoint.HasMore,
		report.Checkpoint.SourceCount,
		report.Checkpoint.CandidateCount,
		report.Blocked,
		report.Applied,
		report.Pending,
		report.NoOp,
	)
	if err != nil {
		return err
	}
	for _, item := range report.Items {
		derivedSnapshotID := ""
		if item.DerivedSnapshotID != nil {
			derivedSnapshotID = item.DerivedSnapshotID.String()
		}
		if _, err := fmt.Fprintf(
			writer,
			"item planId=%s releaseBundleId=%s status=%s reason=%s originalPlanChecksum=%s originalReleaseChecksum=%s derivedSnapshotId=%s derivedSnapshotChecksum=%s\n",
			item.OriginalPlanID,
			item.OriginalReleaseBundleID,
			item.Status,
			item.BlockedReasonCode,
			item.OriginalPlanChecksum,
			item.OriginalReleaseChecksum,
			derivedSnapshotID,
			item.DerivedSnapshotChecksum,
		); err != nil {
			return err
		}
	}
	return nil
}

func runTargetConfigSnapshotV1DryRun(
	ctx context.Context,
	organizationID uuid.UUID,
	actorUserAccountID uuid.UUID,
	predecessorCheckpointID *uuid.UUID,
	batchSize int,
) (*types.V1ExtractionReport, error) {
	return withTargetConfigSnapshotBackfillRegistry(ctx, func(
		ctx context.Context,
		verifier targetconfig.ObjectVerifier,
	) (*types.V1ExtractionReport, error) {
		return db.CreateTargetConfigV1ExtractionDryRun(
			ctx,
			organizationID,
			actorUserAccountID,
			predecessorCheckpointID,
			batchSize,
			verifier,
		)
	})
}

func runTargetConfigSnapshotV1Apply(
	ctx context.Context,
	organizationID,
	actorUserAccountID,
	checkpointID uuid.UUID,
	dryRunChecksum string,
	batchSize int,
) (*types.V1ExtractionReport, error) {
	return withTargetConfigSnapshotBackfillRegistry(ctx, func(
		ctx context.Context,
		verifier targetconfig.ObjectVerifier,
	) (*types.V1ExtractionReport, error) {
		return db.ApplyTargetConfigV1Extraction(
			ctx,
			organizationID,
			actorUserAccountID,
			checkpointID,
			dryRunChecksum,
			batchSize,
			verifier,
		)
	})
}

func runTargetConfigSnapshotV1Report(
	ctx context.Context,
	organizationID,
	checkpointID uuid.UUID,
) (*types.V1ExtractionReport, error) {
	return withTargetConfigSnapshotBackfillRegistry(ctx, func(
		ctx context.Context,
		_ targetconfig.ObjectVerifier,
	) (*types.V1ExtractionReport, error) {
		return db.GetTargetConfigV1ExtractionReport(ctx, organizationID, checkpointID)
	})
}

func withTargetConfigSnapshotBackfillRegistry(
	ctx context.Context,
	run func(context.Context, targetconfig.ObjectVerifier) (*types.V1ExtractionReport, error),
) (*types.V1ExtractionReport, error) {
	env.Initialize()
	registry := util.Require(svc.NewDefault(ctx))
	defer func() { util.Must(registry.Shutdown(ctx)) }()
	return run(
		internalctx.WithDb(ctx, registry.GetDbPool()),
		registry.GetTargetConfigObjectVerifier(),
	)
}

func init() {
	RootCommand.AddCommand(NewBackfillTargetConfigSnapshotsCommand())
}
