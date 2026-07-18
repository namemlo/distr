package cmd

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestBackfillTargetConfigSnapshotsDefaultsToDryRun(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	actorUserAccountID := uuid.New()
	var gotOrganizationID uuid.UUID
	var gotBatchSize int
	var gotPredecessorCheckpointID *uuid.UUID
	stdout, err := executeBackfillTargetConfigSnapshotsForTest(t, backfillTargetConfigSnapshotsRuntime{
		DryRun: func(
			_ context.Context,
			orgID uuid.UUID,
			_ uuid.UUID,
			predecessorCheckpointID *uuid.UUID,
			batchSize int,
		) (*types.V1ExtractionReport, error) {
			gotOrganizationID = orgID
			gotPredecessorCheckpointID = predecessorCheckpointID
			gotBatchSize = batchSize
			return v1ExtractionCommandReport(orgID), nil
		},
	},
		"--organization-id", organizationID.String(),
		"--actor-user-account-id", actorUserAccountID.String(),
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(gotOrganizationID).To(Equal(organizationID))
	g.Expect(gotPredecessorCheckpointID).To(BeNil())
	g.Expect(gotBatchSize).To(Equal(100))
	g.Expect(stdout).To(ContainSubstring("mode=dry-run"))
	g.Expect(stdout).To(ContainSubstring("dryRunChecksum=sha256:"))
}

func TestBackfillTargetConfigSnapshotsRequiresActorForDryRun(t *testing.T) {
	var called bool
	_, err := executeBackfillTargetConfigSnapshotsForTest(t, backfillTargetConfigSnapshotsRuntime{
		DryRun: func(
			context.Context,
			uuid.UUID,
			uuid.UUID,
			*uuid.UUID,
			int,
		) (*types.V1ExtractionReport, error) {
			called = true
			return nil, nil
		},
	}, "--organization-id", uuid.NewString())

	NewWithT(t).Expect(err).To(MatchError(ContainSubstring(
		"actor-user-account-id is required",
	)))
	NewWithT(t).Expect(called).To(BeFalse())
}

func TestBackfillTargetConfigSnapshotsDryRunPassesCheckpointPredecessor(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	actorUserAccountID := uuid.New()
	predecessorCheckpointID := uuid.New()
	var gotPredecessorCheckpointID *uuid.UUID
	_, err := executeBackfillTargetConfigSnapshotsForTest(t, backfillTargetConfigSnapshotsRuntime{
		DryRun: func(
			_ context.Context,
			_ uuid.UUID,
			_ uuid.UUID,
			predecessor *uuid.UUID,
			_ int,
		) (*types.V1ExtractionReport, error) {
			gotPredecessorCheckpointID = predecessor
			return v1ExtractionCommandReport(organizationID), nil
		},
	},
		"--organization-id", organizationID.String(),
		"--actor-user-account-id", actorUserAccountID.String(),
		"--dry-run",
		"--predecessor-checkpoint-id", predecessorCheckpointID.String(),
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(gotPredecessorCheckpointID).NotTo(BeNil())
	g.Expect(*gotPredecessorCheckpointID).To(Equal(predecessorCheckpointID))
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
	actorUserAccountID := uuid.New()
	checkpointID := uuid.New()
	checksum := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var gotActorUserAccountID uuid.UUID
	var gotCheckpointID uuid.UUID
	var gotChecksum string
	stdout, err := executeBackfillTargetConfigSnapshotsForTest(t, backfillTargetConfigSnapshotsRuntime{
		Apply: func(
			_ context.Context,
			_ uuid.UUID,
			actor uuid.UUID,
			checkpoint uuid.UUID,
			dryRunChecksum string,
			_ int,
		) (*types.V1ExtractionReport, error) {
			gotActorUserAccountID = actor
			gotCheckpointID = checkpoint
			gotChecksum = dryRunChecksum
			report := v1ExtractionCommandReport(organizationID)
			report.Checkpoint.ActorUserAccountID = actorUserAccountID
			report.Applied = 1
			return report, nil
		},
	},
		"--organization-id", organizationID.String(),
		"--actor-user-account-id", actorUserAccountID.String(),
		"--apply",
		"--checkpoint-id", checkpointID.String(),
		"--dry-run-checksum", checksum,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(gotActorUserAccountID).To(Equal(actorUserAccountID))
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

func TestBackfillTargetConfigSnapshotsRejectsModeExtraneousFlags(t *testing.T) {
	organizationID := uuid.NewString()
	checkpointID := uuid.NewString()
	checksum := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "report with apply checksum",
			args: []string{
				"--organization-id", organizationID,
				"--report", checkpointID,
				"--dry-run-checksum", checksum,
			},
		},
		{
			name: "report with checkpoint id",
			args: []string{
				"--organization-id", organizationID,
				"--report", checkpointID,
				"--checkpoint-id", checkpointID,
			},
		},
		{
			name: "report with actor",
			args: []string{
				"--organization-id", organizationID,
				"--report", checkpointID,
				"--actor-user-account-id", uuid.NewString(),
			},
		},
		{
			name: "apply with predecessor checkpoint",
			args: []string{
				"--organization-id", organizationID,
				"--apply",
				"--checkpoint-id", checkpointID,
				"--dry-run-checksum", checksum,
				"--predecessor-checkpoint-id", uuid.NewString(),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			called := false
			_, err := executeBackfillTargetConfigSnapshotsForTest(t, backfillTargetConfigSnapshotsRuntime{
				DryRun: func(
					context.Context,
					uuid.UUID,
					uuid.UUID,
					*uuid.UUID,
					int,
				) (*types.V1ExtractionReport, error) {
					called = true
					return nil, nil
				},
				Apply: func(
					context.Context,
					uuid.UUID,
					uuid.UUID,
					uuid.UUID,
					string,
					int,
				) (*types.V1ExtractionReport, error) {
					called = true
					return nil, nil
				},
				Report: func(
					context.Context,
					uuid.UUID,
					uuid.UUID,
				) (*types.V1ExtractionReport, error) {
					called = true
					return nil, nil
				},
			}, test.args...)

			NewWithT(t).Expect(err).To(HaveOccurred())
			NewWithT(t).Expect(called).To(BeFalse())
		})
	}
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

func TestBackfillTargetConfigSnapshotsReportPrintsStableCheckpointWindow(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	actorUserAccountID := uuid.New()
	predecessorCheckpointID := uuid.New()
	afterPlanID := uuid.New()
	throughPlanID := uuid.New()
	highWaterPlanID := uuid.New()
	afterCreatedAt := time.Date(2026, time.July, 18, 1, 2, 3, 0, time.UTC)
	throughCreatedAt := afterCreatedAt.Add(time.Minute)
	highWaterCreatedAt := throughCreatedAt.Add(time.Minute)
	stdout, err := executeBackfillTargetConfigSnapshotsForTest(t, backfillTargetConfigSnapshotsRuntime{
		DryRun: func(
			_ context.Context,
			_ uuid.UUID,
			_ uuid.UUID,
			_ *uuid.UUID,
			_ int,
		) (*types.V1ExtractionReport, error) {
			report := v1ExtractionCommandReport(organizationID)
			report.Checkpoint.ActorUserAccountID = actorUserAccountID
			report.Checkpoint.PredecessorCheckpointID = &predecessorCheckpointID
			report.Checkpoint.SourceMembershipCheckpointID = &predecessorCheckpointID
			report.Checkpoint.SourceMembershipChecksum =
				"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
			report.Checkpoint.SourceAfterCreatedAt = &afterCreatedAt
			report.Checkpoint.SourceAfterPlanID = &afterPlanID
			report.Checkpoint.SourceThroughCreatedAt = &throughCreatedAt
			report.Checkpoint.SourceThroughPlanID = &throughPlanID
			report.Checkpoint.SourceHighWaterCreatedAt = &highWaterCreatedAt
			report.Checkpoint.SourceHighWaterPlanID = &highWaterPlanID
			report.Checkpoint.HasMore = true
			return report, nil
		},
	},
		"--organization-id", organizationID.String(),
		"--actor-user-account-id", actorUserAccountID.String(),
		"--dry-run",
		"--predecessor-checkpoint-id", predecessorCheckpointID.String(),
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(stdout).To(ContainSubstring(
		"predecessorCheckpointId=" + predecessorCheckpointID.String(),
	))
	g.Expect(stdout).To(ContainSubstring(
		"sourceMembershipCheckpointId=" + predecessorCheckpointID.String(),
	))
	g.Expect(stdout).To(ContainSubstring("sourceMembershipChecksum=sha256:cccc"))
	g.Expect(stdout).To(ContainSubstring(
		"sourceAfterCreatedAt=" + afterCreatedAt.Format(time.RFC3339Nano),
	))
	g.Expect(stdout).To(ContainSubstring("sourceAfterPlanId=" + afterPlanID.String()))
	g.Expect(stdout).To(ContainSubstring(
		"sourceThroughCreatedAt=" + throughCreatedAt.Format(time.RFC3339Nano),
	))
	g.Expect(stdout).To(ContainSubstring("sourceThroughPlanId=" + throughPlanID.String()))
	g.Expect(stdout).To(ContainSubstring(
		"sourceHighWaterCreatedAt=" + highWaterCreatedAt.Format(time.RFC3339Nano),
	))
	g.Expect(stdout).To(ContainSubstring("sourceHighWaterPlanId=" + highWaterPlanID.String()))
	g.Expect(stdout).To(ContainSubstring("actorUserAccountId=" + actorUserAccountID.String()))
	g.Expect(stdout).To(ContainSubstring("hasMore=true"))
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
			ID:                       uuid.New(),
			OrganizationID:           organizationID,
			DryRunChecksum:           "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			SourceStateChecksum:      "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			SourceMembershipChecksum: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			SourceCount:              1,
			CandidateCount:           1,
			BatchSize:                100,
		},
		Pending: 1,
	}
}
