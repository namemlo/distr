package cmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/env"
	"github.com/distr-sh/distr/internal/releasebundles"
	"github.com/distr-sh/distr/internal/svc"
	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/util"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

const (
	releaseBackfillArtifactEvidenceSchema   = "distr.release-backfill-artifact-evidence/v1"
	maxReleaseBackfillArtifactEvidenceBytes = 1 << 20
)

type backfillReleaseContractV2Runtime struct {
	Stdout io.Writer
	Run    func(context.Context, types.ReleaseBackfillRequest) (*types.ReleaseBackfillReport, error)
}

type backfillReleaseContractV2Options struct {
	OrganizationID        string
	CheckpointID          string
	Apply                 bool
	BatchSize             int
	CursorCreatedAt       string
	CursorReleaseBundleID string
	ArtifactEvidenceFile  string
}

type releaseBackfillArtifactEvidenceDocument struct {
	Schema    string                                  `json:"schema"`
	Reference string                                  `json:"reference"`
	Evidence  []types.ReleaseBackfillArtifactEvidence `json:"evidence"`
}

type resolvedReleaseBackfillArtifactEvidence struct {
	Reference string
	Checksum  string
	Evidence  []types.ReleaseBackfillArtifactEvidence
}

func NewBackfillReleaseContractV2Command() *cobra.Command {
	return newBackfillReleaseContractV2Command(backfillReleaseContractV2Runtime{})
}

func newBackfillReleaseContractV2Command(runtime backfillReleaseContractV2Runtime) *cobra.Command {
	if runtime.Stdout == nil {
		runtime.Stdout = os.Stdout
	}
	if runtime.Run == nil {
		runtime.Run = runBackfillReleaseContractV2
	}
	options := backfillReleaseContractV2Options{BatchSize: 100}
	command := &cobra.Command{
		Use:   "backfill-release-contract-v2",
		Short: "preview or apply additive v1-to-v2 Component Release derivation",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			request, err := resolveBackfillReleaseContractV2Request(options)
			if err != nil {
				return err
			}
			report, err := runtime.Run(command.Context(), request)
			if report != nil {
				if outputErr := writeReleaseBackfillReport(runtime.Stdout, report); outputErr != nil {
					return outputErr
				}
			}
			return err
		},
	}
	command.Flags().StringVar(&options.OrganizationID, "organization-id", "", "organization id to backfill")
	command.Flags().StringVar(&options.CheckpointID, "checkpoint-id", "", "stable checkpoint id required with --apply")
	command.Flags().BoolVar(&options.Apply, "apply", false, "write derived v2 drafts and lineage; omitted means dry-run")
	command.Flags().IntVar(&options.BatchSize, "batch-size", options.BatchSize, "maximum legacy releases per batch")
	command.Flags().StringVar(&options.CursorCreatedAt, "cursor-created-at", "", "resume cursor created_at timestamp")
	command.Flags().StringVar(
		&options.CursorReleaseBundleID,
		"cursor-release-bundle-id",
		"",
		"resume cursor source release bundle id",
	)
	command.Flags().StringVar(
		&options.ArtifactEvidenceFile,
		"artifact-evidence-file",
		"",
		"bounded reviewed artifact media-type evidence JSON file",
	)
	return command
}

func resolveBackfillReleaseContractV2Request(
	options backfillReleaseContractV2Options,
) (types.ReleaseBackfillRequest, error) {
	organizationID, err := uuid.Parse(options.OrganizationID)
	if err != nil {
		return types.ReleaseBackfillRequest{}, fmt.Errorf("organization-id is required: %w", err)
	}
	if options.BatchSize <= 0 || options.BatchSize > 1000 {
		return types.ReleaseBackfillRequest{}, fmt.Errorf("batch-size must be between 1 and 1000")
	}
	request := types.ReleaseBackfillRequest{
		OrganizationID: organizationID,
		Apply:          options.Apply,
		BatchSize:      options.BatchSize,
	}
	if options.ArtifactEvidenceFile != "" {
		evidence, err := readReleaseBackfillArtifactEvidence(options.ArtifactEvidenceFile)
		if err != nil {
			return types.ReleaseBackfillRequest{}, err
		}
		request.ArtifactEvidence = evidence.Evidence
		request.EvidenceDocumentReference = evidence.Reference
		request.EvidenceDocumentChecksum = evidence.Checksum
	}
	if options.CheckpointID != "" {
		checkpointID, err := uuid.Parse(options.CheckpointID)
		if err != nil {
			return types.ReleaseBackfillRequest{}, fmt.Errorf("parse checkpoint-id: %w", err)
		}
		request.CheckpointID = checkpointID
	}
	if options.Apply && request.CheckpointID == uuid.Nil {
		return types.ReleaseBackfillRequest{}, fmt.Errorf("checkpoint-id is required with --apply")
	}
	if options.CursorCreatedAt != "" || options.CursorReleaseBundleID != "" {
		if options.CursorCreatedAt == "" || options.CursorReleaseBundleID == "" {
			return types.ReleaseBackfillRequest{}, fmt.Errorf(
				"cursor-created-at and cursor-release-bundle-id must be provided together",
			)
		}
		createdAt, err := time.Parse(time.RFC3339Nano, options.CursorCreatedAt)
		if err != nil {
			return types.ReleaseBackfillRequest{}, fmt.Errorf("parse cursor-created-at: %w", err)
		}
		releaseBundleID, err := uuid.Parse(options.CursorReleaseBundleID)
		if err != nil {
			return types.ReleaseBackfillRequest{}, fmt.Errorf("parse cursor-release-bundle-id: %w", err)
		}
		request.Cursor = &types.ReleaseBackfillCursor{
			CreatedAt: createdAt.UTC(), ReleaseBundleID: releaseBundleID,
		}
	}
	return request, nil
}

func readReleaseBackfillArtifactEvidence(
	filePath string,
) (resolvedReleaseBackfillArtifactEvidence, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return resolvedReleaseBackfillArtifactEvidence{}, fmt.Errorf("open artifact-evidence-file: %w", err)
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxReleaseBackfillArtifactEvidenceBytes+1))
	if err != nil {
		return resolvedReleaseBackfillArtifactEvidence{}, fmt.Errorf("read artifact-evidence-file: %w", err)
	}
	if len(raw) == 0 || len(raw) > maxReleaseBackfillArtifactEvidenceBytes {
		return resolvedReleaseBackfillArtifactEvidence{}, fmt.Errorf(
			"artifact-evidence-file must be non-empty and no larger than 1 MiB",
		)
	}
	if err := releasebundles.ValidateProvenanceJSONDocument(raw); err != nil {
		return resolvedReleaseBackfillArtifactEvidence{}, fmt.Errorf(
			"artifact-evidence-file must contain one unambiguous JSON document: %w",
			err,
		)
	}
	var document releaseBackfillArtifactEvidenceDocument
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return resolvedReleaseBackfillArtifactEvidence{}, fmt.Errorf("decode artifact-evidence-file: %w", err)
	}
	if document.Schema != releaseBackfillArtifactEvidenceSchema {
		return resolvedReleaseBackfillArtifactEvidence{}, fmt.Errorf(
			"artifact-evidence-file schema must be %s",
			releaseBackfillArtifactEvidenceSchema,
		)
	}
	sum := sha256.Sum256(raw)
	return resolvedReleaseBackfillArtifactEvidence{
		Reference: document.Reference,
		Checksum:  fmt.Sprintf("sha256:%x", sum),
		Evidence:  document.Evidence,
	}, nil
}

func runBackfillReleaseContractV2(
	ctx context.Context,
	request types.ReleaseBackfillRequest,
) (*types.ReleaseBackfillReport, error) {
	env.Initialize()
	registry := util.Require(svc.NewDefault(ctx))
	defer func() { util.Must(registry.Shutdown(ctx)) }()
	ctx = internalctx.WithDb(ctx, registry.GetDbPool())
	return db.BackfillComponentReleaseV2(ctx, request)
}

func writeReleaseBackfillReport(stdout io.Writer, report *types.ReleaseBackfillReport) error {
	_, err := fmt.Fprintf(
		stdout,
		"dryRun=%t checkpointId=%s scanned=%d eligible=%d wouldDerive=%d derived=%d alreadyPresent=%d awaitingEvidence=%d blocked=%d failed=%d lastCursor=%s nextCursor=%s\n",
		report.DryRun,
		formatOptionalUUID(report.CheckpointID),
		report.Scanned,
		report.Eligible,
		report.WouldDerive,
		report.Derived,
		report.AlreadyPresent,
		report.AwaitingEvidence,
		report.Blocked,
		report.Failed,
		formatReleaseBackfillCursor(report.LastCursor),
		formatReleaseBackfillCursor(report.NextCursor),
	)
	return err
}

func formatReleaseBackfillCursor(cursor *types.ReleaseBackfillCursor) string {
	if cursor == nil {
		return ""
	}
	return cursor.CreatedAt.Format(time.RFC3339Nano) + "/" + cursor.ReleaseBundleID.String()
}

func formatOptionalUUID(value uuid.UUID) string {
	if value == uuid.Nil {
		return ""
	}
	return value.String()
}

func init() {
	RootCommand.AddCommand(NewBackfillReleaseContractV2Command())
}
