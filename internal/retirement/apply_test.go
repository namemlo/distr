package retirement

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestApplyRejectsUnsafeFactsBeforeMutation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*ApplySnapshot, *types.SampleRetirementApplyRequest)
		message string
	}{
		{"stale preview", func(_ *ApplySnapshot, request *types.SampleRetirementApplyRequest) {
			request.PreviewChecksum = checksum("a")
		}, "preview checksum"},
		{"missing approval id", func(_ *ApplySnapshot, request *types.SampleRetirementApplyRequest) {
			request.ApprovalID = ""
		}, "approval"},
		{"missing approval checksum", func(_ *ApplySnapshot, request *types.SampleRetirementApplyRequest) {
			request.ApprovalChecksum = ""
		}, "approval"},
		{"missing backup reference", func(snapshot *ApplySnapshot, _ *types.SampleRetirementApplyRequest) {
			snapshot.Job.BackupReference = ""
		}, "backup"},
		{"malformed restore proof checksum", func(snapshot *ApplySnapshot, _ *types.SampleRetirementApplyRequest) {
			snapshot.Job.RestoreProofChecksum = "sha256:ABC"
		}, "restore proof"},
		{"cross organization subject", func(snapshot *ApplySnapshot, _ *types.SampleRetirementApplyRequest) {
			snapshot.ReferenceReports[0].SubjectOrganizationID = id(99)
		}, "organization"},
		{"changed ownership", func(snapshot *ApplySnapshot, _ *types.SampleRetirementApplyRequest) {
			snapshot.ReferenceReports[0].Subject.OwnershipMarker = "other-owner"
		}, "ownership"},
		{"changed subject checksum", func(snapshot *ApplySnapshot, _ *types.SampleRetirementApplyRequest) {
			snapshot.ReferenceReports[0].CurrentChecksum = checksum("a")
		}, "subject checksum"},
		{"changed before count", func(snapshot *ApplySnapshot, _ *types.SampleRetirementApplyRequest) {
			snapshot.ReferenceReports[0].BeforeCount = 2
		}, "before count"},
		{"mutable subject", func(snapshot *ApplySnapshot, _ *types.SampleRetirementApplyRequest) {
			snapshot.ReferenceReports[0].Immutable = false
		}, "immutable"},
		{"protected reverse reference", func(snapshot *ApplySnapshot, _ *types.SampleRetirementApplyRequest) {
			snapshot.ReferenceReports[0].ProtectedReferenceCount = 1
			snapshot.ReferenceReports[0].Retirable = false
		}, "protected reverse reference"},
		{"cross organization reference", func(snapshot *ApplySnapshot, _ *types.SampleRetirementApplyRequest) {
			snapshot.ReferenceReports[0].CrossOrganizationReferenceCount = 1
			snapshot.ReferenceReports[0].Retirable = false
		}, "cross-organization"},
		{"application audit count fell", func(snapshot *ApplySnapshot, _ *types.SampleRetirementApplyRequest) {
			snapshot.CurrentAuditEventCount = 1
		}, "audit"},
		{"restart checkpoint disagrees with job counts", func(snapshot *ApplySnapshot, _ *types.SampleRetirementApplyRequest) {
			snapshot.Job.State = types.SampleRetirementJobApplying
			snapshot.Job.ApprovalID = new("approval-42")
			snapshot.Job.ApprovalChecksum = new(checksum("9"))
			snapshot.Job.LastCheckpointSequence = 1
			snapshot.LastCheckpoint = &types.SampleRetirementCheckpoint{
				Sequence: 2, AppliedItemCount: 1, TombstoneCount: 1,
			}
		}, "checkpoint"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			snapshot := validApplySnapshot()
			request := validApplyRequest(snapshot)
			test.mutate(&snapshot, &request)
			store := &applyTestStore{snapshots: []ApplySnapshot{snapshot}}

			result, err := NewApplyService(store).Apply(
				context.Background(), snapshot.Job.OrganizationID, snapshot.Job.ID, request,
			)

			g.Expect(result).To(BeNil())
			g.Expect(err).To(MatchError(ContainSubstring(test.message)))
			g.Expect(store.applyCommands).To(BeEmpty())
			g.Expect(store.completeCommands).To(BeEmpty())
		})
	}
}

func TestApplyResumesAfterInterruptionAndCommitsOnlyPendingItems(t *testing.T) {
	g := NewWithT(t)
	initial := validApplySnapshot()
	second := initial.Items[0]
	second.ID, second.SubjectID, second.Ordinal = id(5), id(6), 2
	initial.Items = append(initial.Items, second)
	report := initial.ReferenceReports[0]
	report.Subject.SubjectID = second.SubjectID
	initial.ReferenceReports = append(initial.ReferenceReports, report)
	initial.Job.RequestedItemCount, initial.Job.PreviewedItemCount = 2, 2
	initial.CurrentAuditEventCount = 4

	resumed := initial
	resumed.Job.State = types.SampleRetirementJobApplying
	resumed.Job.ApprovalID = new("approval-42")
	resumed.Job.ApprovalChecksum = new(checksum("9"))
	resumed.Job.AppliedItemCount, resumed.Job.TombstoneCount = 1, 1
	resumed.Job.LastCheckpointSequence = 1
	resumed.Items = append([]types.SampleRetirementItem(nil), initial.Items...)
	resumed.Items[0].State = types.SampleRetirementItemApplied
	resumed.ReferenceReports = []types.ReferenceReport{initial.ReferenceReports[1]}
	resumed.CurrentAuditEventCount = 2
	resumed.LastCheckpoint = &types.SampleRetirementCheckpoint{
		Sequence: 1, LastCompletedOrdinal: 1, AppliedItemCount: 1, TombstoneCount: 1,
	}
	completedAt := fixedTime().Add(time.Minute)
	store := &applyTestStore{
		snapshots: []ApplySnapshot{initial, resumed},
		outcomes: []ApplyItemOutcome{
			outcome(1, 1, 1), {},
			outcome(2, 2, 2),
		},
		applyErrors: []error{nil, errors.New("injected interruption"), nil},
		completeResult: &types.SampleRetirementResult{
			JobID: initial.Job.ID, PreviewChecksum: initial.Job.PreviewChecksum,
			State: types.SampleRetirementJobApplied, AppliedCount: 2,
			TombstoneCount: 2, CheckpointSequence: 2, CompletedAt: completedAt,
		},
	}
	service := NewApplyService(store, WithApplyClock(func() time.Time { return completedAt }))
	request := validApplyRequest(initial)

	result, err := service.Apply(
		context.Background(), initial.Job.OrganizationID, initial.Job.ID, request,
	)
	g.Expect(result).To(BeNil())
	g.Expect(err).To(MatchError("apply sample retirement item ordinal 2: injected interruption"))
	g.Expect(store.completeCommands).To(BeEmpty())

	result, err = service.Apply(
		context.Background(), initial.Job.OrganizationID, initial.Job.ID, request,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.AppliedCount).To(Equal(2))
	g.Expect(store.applyCommands).To(HaveLen(3))
	g.Expect([]int{
		store.applyCommands[0].Item.Ordinal,
		store.applyCommands[1].Item.Ordinal,
		store.applyCommands[2].Item.Ordinal,
	}).To(Equal([]int{1, 2, 2}))
	g.Expect(store.applyCommands[2].ExpectedCheckpointSequence).To(Equal(int64(1)))
	g.Expect(store.completeCommands).To(HaveLen(1))
	g.Expect(store.completeCommands[0].ExpectedAppliedCount).To(Equal(2))
	g.Expect(store.completeCommands[0].ExpectedTombstoneCount).To(Equal(2))
}

func TestApplyCompletedJobIsNoOpOnlyForIdenticalApprovalBinding(t *testing.T) {
	g := NewWithT(t)
	snapshot := validApplySnapshot()
	completedAt := fixedTime().Add(time.Minute)
	snapshot.Job.State = types.SampleRetirementJobApplied
	snapshot.Job.ApprovalID = new("approval-42")
	snapshot.Job.ApprovalChecksum = new(checksum("9"))
	snapshot.Job.AppliedItemCount, snapshot.Job.TombstoneCount = 1, 1
	snapshot.Job.LastCheckpointSequence = 1
	snapshot.Job.CompletedAt = &completedAt
	snapshot.Items[0].State = types.SampleRetirementItemApplied
	snapshot.ReferenceReports = nil
	store := &applyTestStore{snapshots: []ApplySnapshot{snapshot}}
	request := validApplyRequest(snapshot)

	result, err := NewApplyService(store).Apply(
		context.Background(), snapshot.Job.OrganizationID, snapshot.Job.ID, request,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.NoOp).To(BeTrue())
	g.Expect(result.AppliedCount).To(Equal(1))
	g.Expect(store.applyCommands).To(BeEmpty())
	g.Expect(store.completeCommands).To(BeEmpty())

	store.loadIndex = 0
	request.ApprovalChecksum = checksum("8")
	result, err = NewApplyService(store).Apply(
		context.Background(), snapshot.Job.OrganizationID, snapshot.Job.ID, request,
	)
	g.Expect(result).To(BeNil())
	g.Expect(err).To(MatchError(ContainSubstring("approval")))
}

func TestApplyPassesExactTombstoneAndCountFencesToAtomicStore(t *testing.T) {
	g := NewWithT(t)
	snapshot := validApplySnapshot()
	retiredAt := fixedTime().Add(time.Minute)
	store := &applyTestStore{
		snapshots: []ApplySnapshot{snapshot},
		outcomes:  []ApplyItemOutcome{outcome(1, 1, 1)},
		completeResult: &types.SampleRetirementResult{
			JobID: snapshot.Job.ID, PreviewChecksum: snapshot.Job.PreviewChecksum,
			State: types.SampleRetirementJobApplied, AppliedCount: 1,
			TombstoneCount: 1, CheckpointSequence: 1, CompletedAt: retiredAt,
		},
	}

	_, err := NewApplyService(
		store,
		WithApplyClock(func() time.Time { return retiredAt }),
		WithApplyUUID(func() uuid.UUID { return id(7) }),
	).Apply(
		context.Background(), snapshot.Job.OrganizationID, snapshot.Job.ID,
		validApplyRequest(snapshot),
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(store.applyCommands).To(HaveLen(1))
	command := store.applyCommands[0]
	g.Expect(command.ApprovalID).To(Equal("approval-42"))
	g.Expect(command.ApprovalChecksum).To(Equal(checksum("9")))
	g.Expect(command.ExpectedJobVersion).To(Equal(int64(1)))
	g.Expect(command.ExpectedCheckpointSequence).To(BeZero())
	g.Expect(command.ExpectedAppliedCount).To(BeZero())
	g.Expect(command.ExpectedSkippedCount).To(BeZero())
	g.Expect(command.ExpectedTombstoneCount).To(BeZero())
	g.Expect(command.ExpectedAuditEventCount).To(Equal(2))
	g.Expect(command.Tombstone.ID).To(Equal(id(7)))
	g.Expect(command.Tombstone.OrganizationID).To(Equal(snapshot.Job.OrganizationID))
	g.Expect(command.Tombstone.RetirementJobID).To(Equal(snapshot.Job.ID))
	g.Expect(command.Tombstone.RetirementItemID).To(Equal(snapshot.Items[0].ID))
	g.Expect(command.Tombstone.SubjectType).To(Equal(snapshot.Items[0].SubjectType))
	g.Expect(command.Tombstone.SubjectID).To(Equal(snapshot.Items[0].SubjectID))
	g.Expect(command.Tombstone.SubjectChecksum).To(Equal(snapshot.Items[0].ExpectedChecksum))
	g.Expect(command.Tombstone.AuditEventCount).To(Equal(2))
	g.Expect(command.Tombstone.LineageChecksum).To(Equal(
		"sha256:0747b00ccafae6f93aae8660b32334679f42c5ba7d061f238f24e0028b3f65cb",
	))
}

func TestVerifyRequiresExactLineageCountsAndRetainedApplicationAudit(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*VerificationOutcome)
		message string
	}{
		{"counts", func(outcome *VerificationOutcome) {
			outcome.Report.ExactCounts = false
			outcome.Report.Problems = []string{"count mismatch"}
		}, "count mismatch"},
		{"lineage", func(outcome *VerificationOutcome) {
			outcome.Report.TombstoneLineageValid = false
			outcome.Report.Problems = []string{"lineage mismatch"}
		}, "lineage mismatch"},
		{"application audit", func(outcome *VerificationOutcome) {
			outcome.ApplicationAuditDeleteCount = 1
		}, "application audit"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			snapshot := validApplySnapshot()
			outcome := validVerificationOutcome(snapshot)
			test.mutate(&outcome)
			store := &applyTestStore{verification: outcome}

			report, err := NewApplyService(store).Verify(
				context.Background(), snapshot.Job.OrganizationID, snapshot.Job.ID,
			)

			g.Expect(report).NotTo(BeNil())
			g.Expect(err).To(MatchError(ContainSubstring(test.message)))
		})
	}
}

func TestVerifyReturnsVerifiedReport(t *testing.T) {
	g := NewWithT(t)
	snapshot := validApplySnapshot()
	outcome := validVerificationOutcome(snapshot)
	store := &applyTestStore{verification: outcome}

	report, err := NewApplyService(store).Verify(
		context.Background(), snapshot.Job.OrganizationID, snapshot.Job.ID,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(report).To(Equal(outcome.Report))
	g.Expect(report.State).To(Equal(types.SampleRetirementJobVerified))
}

type applyTestStore struct {
	snapshots        []ApplySnapshot
	loadIndex        int
	applyCommands    []ApplyItemCommand
	outcomes         []ApplyItemOutcome
	applyErrors      []error
	applyIndex       int
	completeCommands []CompleteApplyCommand
	completeResult   *types.SampleRetirementResult
	verification     VerificationOutcome
}

func (store *applyTestStore) LoadApplySnapshot(
	_ context.Context, _, _ uuid.UUID,
) (ApplySnapshot, error) {
	index := store.loadIndex
	if index >= len(store.snapshots) {
		index = len(store.snapshots) - 1
	}
	store.loadIndex++
	return store.snapshots[index], nil
}

func (store *applyTestStore) ApplyItemAtomically(
	_ context.Context, command ApplyItemCommand,
) (ApplyItemOutcome, error) {
	store.applyCommands = append(store.applyCommands, command)
	index := store.applyIndex
	store.applyIndex++
	var result ApplyItemOutcome
	if index < len(store.outcomes) {
		result = store.outcomes[index]
	}
	if index < len(store.applyErrors) {
		return result, store.applyErrors[index]
	}
	return result, nil
}

func (store *applyTestStore) CompleteApplyAtomically(
	_ context.Context, command CompleteApplyCommand,
) (*types.SampleRetirementResult, error) {
	store.completeCommands = append(store.completeCommands, command)
	return store.completeResult, nil
}

func (store *applyTestStore) VerifyApplyAtomically(
	context.Context, uuid.UUID, uuid.UUID,
) (VerificationOutcome, error) {
	return store.verification, nil
}

func validApplySnapshot() ApplySnapshot {
	item := types.SampleRetirementItem{
		ID: id(3), OrganizationID: id(2), RetirementJobID: id(1), Ordinal: 1,
		SubjectType: types.SampleRetirementSubjectApplication, SubjectID: id(4),
		OwnershipMarker: "sample-fixture", OwnershipChecksum: checksum("b"),
		ExpectedChecksum: checksum("c"), BeforeCount: 1,
		ReferenceReportChecksum: checksum("d"), State: types.SampleRetirementItemPending, Version: 1,
	}
	return ApplySnapshot{
		Job: types.SampleRetirementJob{
			ID: id(1), OrganizationID: id(2), RequestedByUserAccountID: id(8),
			State:           types.SampleRetirementJobPreviewed,
			BackupReference: "s3://evidence/backup.dump?versionId=abc%2Fdef", BackupChecksum: checksum("e"),
			RestoreProofReference: "https://evidence.example/restore/42?digest=sha256%3Aabc", RestoreProofChecksum: checksum("f"),
			AllowlistChecksum: checksum("1"), PreviewChecksum: checksum("2"),
			RequestedItemCount: 1, PreviewedItemCount: 1, Version: 1,
		},
		Items: []types.SampleRetirementItem{item},
		ReferenceReports: []types.ReferenceReport{{
			Subject: types.SampleRetirementSubject{
				SubjectType: item.SubjectType, SubjectID: item.SubjectID,
				OwnershipMarker: item.OwnershipMarker, OwnershipChecksum: item.OwnershipChecksum,
				ExpectedChecksum: item.ExpectedChecksum,
			},
			SubjectOrganizationID: id(2), CurrentChecksum: item.ExpectedChecksum,
			BeforeCount: 1, Immutable: true, References: []types.RetirementReference{},
			AuditEventCount: 2, Retirable: true, BlockingReasons: []string{},
		}},
		CurrentAuditEventCount: 2,
		LoadedAt:               fixedTime(),
	}
}

func validApplyRequest(snapshot ApplySnapshot) types.SampleRetirementApplyRequest {
	return types.SampleRetirementApplyRequest{
		OrganizationID:     snapshot.Job.OrganizationID,
		ActorUserAccountID: snapshot.Job.RequestedByUserAccountID,
		JobID:              snapshot.Job.ID,
		PreviewChecksum:    snapshot.Job.PreviewChecksum,
		ApprovalID:         "approval-42",
		ApprovalChecksum:   checksum("9"),
	}
}

func validVerificationOutcome(snapshot ApplySnapshot) VerificationOutcome {
	return VerificationOutcome{
		Report: &types.SampleRetirementVerification{
			JobID: snapshot.Job.ID, State: types.SampleRetirementJobVerified,
			PreviewChecksum: snapshot.Job.PreviewChecksum, ExactCounts: true,
			TombstoneLineageValid: true, AuditEventsRetained: true,
			RemainingSubjectCount: 0, AuditEventCount: 2, AppliedCount: 1,
			TombstoneCount: 1, VerifiedAt: fixedTime().Add(2 * time.Minute),
			Problems: []string{},
		},
	}
}

func outcome(sequence int64, ordinal, applied int) ApplyItemOutcome {
	return ApplyItemOutcome{
		ItemState: types.SampleRetirementItemApplied,
		Checkpoint: types.SampleRetirementCheckpoint{
			Sequence: sequence, LastCompletedOrdinal: ordinal,
			AppliedItemCount: applied, TombstoneCount: applied,
		},
	}
}

func fixedTime() time.Time {
	return time.Date(2026, time.July, 28, 10, 30, 0, 123, time.UTC)
}

func id(last byte) uuid.UUID {
	var value uuid.UUID
	value[15] = last
	return value
}

func checksum(digit string) string {
	return "sha256:" + repeat(digit)
}

func repeat(digit string) string {
	result := ""
	for range 64 {
		result += digit
	}
	return result
}
