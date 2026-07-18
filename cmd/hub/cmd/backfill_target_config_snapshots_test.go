package cmd

import (
	"bytes"
	"context"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestBackfillTargetConfigSnapshotsDefaultsToDryRun(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	var gotOrganizationID uuid.UUID
	var gotBatchSize int
	stdout, err := executeBackfillTargetConfigSnapshotsForTest(t, backfillTargetConfigSnapshotsRuntime{
		DryRun: func(_ context.Context, orgID uuid.UUID, batchSize int) (*types.V1ExtractionReport, error) {
			gotOrganizationID = orgID
			gotBatchSize = batchSize
			return v1ExtractionCommandReport(orgID), nil
		},
	}, "--organization-id", organizationID.String())

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(gotOrganizationID).To(Equal(organizationID))
	g.Expect(gotBatchSize).To(Equal(100))
	g.Expect(stdout).To(ContainSubstring("mode=dry-run"))
	g.Expect(stdout).To(ContainSubstring("dryRunChecksum=sha256:"))
}

func TestBackfillTargetConfigSnapshotsApplyRequiresCheckpointAndChecksum(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	var called bool
	_, err := executeBackfillTargetConfigSnapshotsForTest(t, backfillTargetConfigSnapshotsRuntime{
		Apply: func(
			context.Context,
			uuid.UUID,
			uuid.UUID,
			string,
			int,
		) (*types.V1ExtractionReport, error) {
			called = true
			return nil, nil
		},
	}, "--organization-id", organizationID.String(), "--apply")

	g.Expect(err).To(HaveOccurred())
	g.Expect(called).To(BeFalse())
}

func TestBackfillTargetConfigSnapshotsApplyPassesApprovedDryRun(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	checkpointID := uuid.New()
	checksum := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var gotCheckpointID uuid.UUID
	var gotChecksum string
	stdout, err := executeBackfillTargetConfigSnapshotsForTest(t, backfillTargetConfigSnapshotsRuntime{
		Apply: func(
			_ context.Context,
			_ uuid.UUID,
			checkpoint uuid.UUID,
			dryRunChecksum string,
			_ int,
		) (*types.V1ExtractionReport, error) {
			gotCheckpointID = checkpoint
			gotChecksum = dryRunChecksum
			report := v1ExtractionCommandReport(organizationID)
			report.Applied = 1
			return report, nil
		},
	},
		"--organization-id", organizationID.String(),
		"--apply",
		"--checkpoint-id", checkpointID.String(),
		"--dry-run-checksum", checksum,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(gotCheckpointID).To(Equal(checkpointID))
	g.Expect(gotChecksum).To(Equal(checksum))
	g.Expect(stdout).To(ContainSubstring("mode=apply"))
	g.Expect(stdout).To(ContainSubstring("applied=1"))
}

func TestBackfillTargetConfigSnapshotsReportIsReadOnly(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	checkpointID := uuid.New()
	var gotCheckpointID uuid.UUID
	stdout, err := executeBackfillTargetConfigSnapshotsForTest(t, backfillTargetConfigSnapshotsRuntime{
		Report: func(_ context.Context, _ uuid.UUID, checkpoint uuid.UUID) (*types.V1ExtractionReport, error) {
			gotCheckpointID = checkpoint
			return v1ExtractionCommandReport(organizationID), nil
		},
	}, "--organization-id", organizationID.String(), "--report", checkpointID.String())

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(gotCheckpointID).To(Equal(checkpointID))
	g.Expect(stdout).To(ContainSubstring("mode=report"))
}

func TestBackfillTargetConfigSnapshotsReportPrintsBoundedBlockedItems(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	checkpointID := uuid.New()
	planID := uuid.New()
	releaseID := uuid.New()
	stdout, err := executeBackfillTargetConfigSnapshotsForTest(t, backfillTargetConfigSnapshotsRuntime{
		Report: func(_ context.Context, _ uuid.UUID, _ uuid.UUID) (*types.V1ExtractionReport, error) {
			report := v1ExtractionCommandReport(organizationID)
			report.Checkpoint.BlockedCount = 1
			report.Blocked = 1
			report.Items = []types.V1ExtractionLineage{{
				OriginalPlanID:          planID,
				OriginalReleaseBundleID: releaseID,
				Status:                  types.V1ExtractionStatusBlocked,
				BlockedReasonCode:       "multi_target_plan",
			}}
			return report, nil
		},
	}, "--organization-id", organizationID.String(), "--report", checkpointID.String())

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(stdout).To(ContainSubstring("planId=" + planID.String()))
	g.Expect(stdout).To(ContainSubstring("releaseBundleId=" + releaseID.String()))
	g.Expect(stdout).To(ContainSubstring("reason=multi_target_plan"))
}

func executeBackfillTargetConfigSnapshotsForTest(
	t *testing.T,
	runtime backfillTargetConfigSnapshotsRuntime,
	args ...string,
) (string, error) {
	t.Helper()
	var stdout bytes.Buffer
	if runtime.Stdout == nil {
		runtime.Stdout = &stdout
	}
	command := newBackfillTargetConfigSnapshotsCommand(runtime)
	command.SetArgs(args)
	command.SetOut(&stdout)
	command.SetErr(&stdout)
	err := command.Execute()
	return stdout.String(), err
}

func v1ExtractionCommandReport(organizationID uuid.UUID) *types.V1ExtractionReport {
	return &types.V1ExtractionReport{
		Checkpoint: types.V1ExtractionCheckpoint{
			ID:                  uuid.New(),
			OrganizationID:      organizationID,
			DryRunChecksum:      "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			SourceStateChecksum: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			SourceCount:         1,
			CandidateCount:      1,
			BatchSize:           100,
		},
		Pending: 1,
	}
}
