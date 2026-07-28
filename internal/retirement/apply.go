package retirement

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

var (
	ErrStaleRetirementPreview = errors.New("sample retirement preview checksum is stale")
	ErrRetirementVerification = errors.New("sample retirement verification failed")

	retirementChecksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type ApplySnapshot struct {
	Job                    types.SampleRetirementJob
	Items                  []types.SampleRetirementItem
	LastCheckpoint         *types.SampleRetirementCheckpoint
	ReferenceReports       []types.ReferenceReport
	CurrentAuditEventCount int
	LoadedAt               time.Time
}

// ApplyItemCommand is one transaction boundary. Implementations must lock and
// revalidate the job and item, bind approval facts on the first transition,
// insert the tombstone before deleting the domain subject, update exact counts,
// and persist the checkpoint in the same transaction.
type ApplyItemCommand struct {
	OrganizationID             uuid.UUID
	JobID                      uuid.UUID
	Item                       types.SampleRetirementItem
	PreviewChecksum            string
	ApprovalID                 string
	ApprovalChecksum           string
	ExpectedJobVersion         int64
	ExpectedCheckpointSequence int64
	ExpectedAppliedCount       int
	ExpectedSkippedCount       int
	ExpectedTombstoneCount     int
	ExpectedAuditEventCount    int
	Tombstone                  types.AuditSubjectTombstone
}

type ApplyItemOutcome struct {
	Checkpoint types.SampleRetirementCheckpoint
	ItemState  types.SampleRetirementItemState
	Tombstone  *types.AuditSubjectTombstone
}

// CompleteApplyCommand is a separate final transaction after all per-item
// checkpoints are durable.
type CompleteApplyCommand struct {
	OrganizationID             uuid.UUID
	JobID                      uuid.UUID
	PreviewChecksum            string
	ApprovalID                 string
	ApprovalChecksum           string
	ExpectedCheckpointSequence int64
	ExpectedAppliedCount       int
	ExpectedSkippedCount       int
	ExpectedTombstoneCount     int
	ExpectedAuditEventCount    int
}

type VerificationOutcome struct {
	Report                      *types.SampleRetirementVerification
	ApplicationAuditDeleteCount int
}

type ApplyStore interface {
	LoadApplySnapshot(
		context.Context,
		uuid.UUID,
		uuid.UUID,
	) (ApplySnapshot, error)
	ApplyItemAtomically(
		context.Context,
		ApplyItemCommand,
	) (ApplyItemOutcome, error)
	CompleteApplyAtomically(
		context.Context,
		CompleteApplyCommand,
	) (*types.SampleRetirementResult, error)
	VerifyApplyAtomically(
		context.Context,
		uuid.UUID,
		uuid.UUID,
	) (VerificationOutcome, error)
}

type ApplyService struct {
	store ApplyStore
	now   func() time.Time
	newID func() uuid.UUID
}

type ApplyOption func(*ApplyService)

func WithApplyClock(now func() time.Time) ApplyOption {
	return func(service *ApplyService) {
		if now != nil {
			service.now = now
		}
	}
}

func WithApplyUUID(newID func() uuid.UUID) ApplyOption {
	return func(service *ApplyService) {
		if newID != nil {
			service.newID = newID
		}
	}
}

func NewApplyService(store ApplyStore, options ...ApplyOption) *ApplyService {
	service := &ApplyService{
		store: store,
		now:   func() time.Time { return time.Now().UTC() },
		newID: uuid.New,
	}
	for _, option := range options {
		option(service)
	}
	return service
}

func (service *ApplyService) Apply(
	ctx context.Context,
	organizationID uuid.UUID,
	jobID uuid.UUID,
	request types.SampleRetirementApplyRequest,
) (*types.SampleRetirementResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if service == nil || service.store == nil {
		return nil, errors.New("sample retirement apply store is required")
	}
	if organizationID == uuid.Nil || jobID == uuid.Nil ||
		request.OrganizationID != organizationID || request.JobID != jobID ||
		request.ActorUserAccountID == uuid.Nil {
		return nil, errors.New("sample retirement apply identity is invalid")
	}
	snapshot, err := service.store.LoadApplySnapshot(ctx, organizationID, jobID)
	if err != nil {
		return nil, err
	}
	if err := validateApplySnapshot(snapshot, organizationID, jobID, request); err != nil {
		return nil, err
	}
	if snapshot.Job.State == types.SampleRetirementJobApplied ||
		snapshot.Job.State == types.SampleRetirementJobVerified {
		return completedNoOp(snapshot.Job), nil
	}

	applied := snapshot.Job.AppliedItemCount
	skipped := snapshot.Job.SkippedItemCount
	tombstones := snapshot.Job.TombstoneCount
	sequence := snapshot.Job.LastCheckpointSequence
	for _, item := range snapshot.Items {
		switch item.State {
		case types.SampleRetirementItemApplied, types.SampleRetirementItemSkipped:
			continue
		case types.SampleRetirementItemPending:
		default:
			return nil, fmt.Errorf(
				"sample retirement item ordinal %d has invalid state %s",
				item.Ordinal,
				item.State,
			)
		}
		retiredAt := service.now().UTC()
		report, exists := referenceReportFor(snapshot.ReferenceReports, item.SubjectID)
		if !exists {
			return nil, errors.New("reverse-reference report is missing")
		}
		tombstone, err := buildAuditSubjectTombstone(
			snapshot.Job,
			item,
			request,
			report.AuditEventCount,
			service.newID(),
			retiredAt,
		)
		if err != nil {
			return nil, err
		}
		outcome, err := service.store.ApplyItemAtomically(ctx, ApplyItemCommand{
			OrganizationID:             organizationID,
			JobID:                      jobID,
			Item:                       item,
			PreviewChecksum:            request.PreviewChecksum,
			ApprovalID:                 request.ApprovalID,
			ApprovalChecksum:           request.ApprovalChecksum,
			ExpectedJobVersion:         snapshot.Job.Version,
			ExpectedCheckpointSequence: sequence,
			ExpectedAppliedCount:       applied,
			ExpectedSkippedCount:       skipped,
			ExpectedTombstoneCount:     tombstones,
			ExpectedAuditEventCount:    snapshot.CurrentAuditEventCount,
			Tombstone:                  tombstone,
		})
		if err != nil {
			return nil, fmt.Errorf(
				"apply sample retirement item ordinal %d: %w",
				item.Ordinal,
				err,
			)
		}
		if err := validateItemOutcome(
			outcome,
			item.Ordinal,
			sequence,
			applied,
			skipped,
			tombstones,
		); err != nil {
			return nil, err
		}
		sequence = outcome.Checkpoint.Sequence
		applied = outcome.Checkpoint.AppliedItemCount
		skipped = outcome.Checkpoint.SkippedItemCount
		tombstones = outcome.Checkpoint.TombstoneCount
	}
	return service.store.CompleteApplyAtomically(ctx, CompleteApplyCommand{
		OrganizationID:             organizationID,
		JobID:                      jobID,
		PreviewChecksum:            request.PreviewChecksum,
		ApprovalID:                 request.ApprovalID,
		ApprovalChecksum:           request.ApprovalChecksum,
		ExpectedCheckpointSequence: sequence,
		ExpectedAppliedCount:       applied,
		ExpectedSkippedCount:       skipped,
		ExpectedTombstoneCount:     tombstones,
		ExpectedAuditEventCount:    snapshot.CurrentAuditEventCount,
	})
}

func (service *ApplyService) Verify(
	ctx context.Context,
	organizationID uuid.UUID,
	jobID uuid.UUID,
) (*types.SampleRetirementVerification, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if service == nil || service.store == nil {
		return nil, errors.New("sample retirement apply store is required")
	}
	if organizationID == uuid.Nil || jobID == uuid.Nil {
		return nil, errors.New("sample retirement verification identity is invalid")
	}
	outcome, err := service.store.VerifyApplyAtomically(ctx, organizationID, jobID)
	if err != nil {
		return nil, err
	}
	if outcome.Report == nil {
		return nil, fmt.Errorf("%w: persistence report is missing", ErrRetirementVerification)
	}
	problems := append([]string(nil), outcome.Report.Problems...)
	if !outcome.Report.ExactCounts {
		problems = appendProblem(problems, "exact counts do not match")
	}
	if !outcome.Report.TombstoneLineageValid {
		problems = appendProblem(problems, "tombstone lineage is invalid")
	}
	if !outcome.Report.AuditEventsRetained || outcome.ApplicationAuditDeleteCount != 0 {
		problems = appendProblem(problems, "application audit events were not retained")
	}
	if outcome.Report.RemainingSubjectCount != 0 {
		problems = appendProblem(problems, "retired subjects remain")
	}
	if outcome.Report.State != types.SampleRetirementJobVerified {
		problems = appendProblem(problems, "job is not verified")
	}
	if len(problems) != 0 {
		outcome.Report.Problems = problems
		return outcome.Report, fmt.Errorf("%w: %s", ErrRetirementVerification, problems[0])
	}
	return outcome.Report, nil
}

func validateApplySnapshot(
	snapshot ApplySnapshot,
	organizationID uuid.UUID,
	jobID uuid.UUID,
	request types.SampleRetirementApplyRequest,
) error {
	job := snapshot.Job
	if job.ID != jobID || job.OrganizationID != organizationID {
		return errors.New("sample retirement job organization does not match")
	}
	if request.PreviewChecksum != job.PreviewChecksum ||
		!retirementChecksumPattern.MatchString(request.PreviewChecksum) {
		return fmt.Errorf(
			"%w: supplied preview checksum does not match immutable preview",
			ErrStaleRetirementPreview,
		)
	}
	if strings.TrimSpace(request.ApprovalID) == "" ||
		!retirementChecksumPattern.MatchString(request.ApprovalChecksum) {
		return errors.New("approval ID and checksum are required")
	}
	switch job.State {
	case types.SampleRetirementJobPreviewed:
		if job.ApprovalID != nil || job.ApprovalChecksum != nil {
			return errors.New("previewed job has a partial approval binding")
		}
	case types.SampleRetirementJobApplying,
		types.SampleRetirementJobApplied,
		types.SampleRetirementJobVerified:
		if job.ApprovalID == nil || job.ApprovalChecksum == nil ||
			*job.ApprovalID != request.ApprovalID ||
			*job.ApprovalChecksum != request.ApprovalChecksum {
			return errors.New("approval binding does not match the original apply")
		}
	default:
		return fmt.Errorf("sample retirement job state %s cannot be applied", job.State)
	}
	if err := validateEvidence(
		"backup",
		job.BackupReference,
		job.BackupChecksum,
	); err != nil {
		return err
	}
	if err := validateEvidence(
		"restore proof",
		job.RestoreProofReference,
		job.RestoreProofChecksum,
	); err != nil {
		return err
	}
	if job.RequestedItemCount != job.PreviewedItemCount ||
		job.PreviewedItemCount != len(snapshot.Items) {
		return errors.New("sample retirement exact item counts do not match")
	}
	if job.State == types.SampleRetirementJobApplied ||
		job.State == types.SampleRetirementJobVerified {
		return nil
	}
	switch job.State {
	case types.SampleRetirementJobPreviewed:
		if snapshot.LastCheckpoint != nil ||
			job.LastCheckpointSequence != 0 ||
			job.AppliedItemCount != 0 ||
			job.SkippedItemCount != 0 ||
			job.TombstoneCount != 0 {
			return errors.New("previewed job has an invalid checkpoint")
		}
	case types.SampleRetirementJobApplying:
		if snapshot.LastCheckpoint == nil ||
			snapshot.LastCheckpoint.Sequence != job.LastCheckpointSequence ||
			snapshot.LastCheckpoint.AppliedItemCount != job.AppliedItemCount ||
			snapshot.LastCheckpoint.SkippedItemCount != job.SkippedItemCount ||
			snapshot.LastCheckpoint.TombstoneCount != job.TombstoneCount {
			return errors.New("restart checkpoint disagrees with job counts")
		}
	}
	if snapshot.CurrentAuditEventCount < 0 {
		return errors.New("application audit count is invalid")
	}
	reports := make(map[uuid.UUID]types.ReferenceReport, len(snapshot.ReferenceReports))
	expectedAuditCount := 0
	for _, report := range snapshot.ReferenceReports {
		if _, exists := reports[report.Subject.SubjectID]; exists {
			return errors.New("duplicate reverse-reference report")
		}
		reports[report.Subject.SubjectID] = report
		expectedAuditCount += report.AuditEventCount
	}
	if snapshot.CurrentAuditEventCount < expectedAuditCount {
		return errors.New("application audit events are not retained")
	}
	items := append([]types.SampleRetirementItem(nil), snapshot.Items...)
	sort.Slice(items, func(left, right int) bool {
		return items[left].Ordinal < items[right].Ordinal
	})
	seenItems := make(map[uuid.UUID]struct{}, len(items))
	seenSubjects := make(map[uuid.UUID]struct{}, len(items))
	for index, item := range items {
		if item.Ordinal != index+1 || item.ID == uuid.Nil || item.SubjectID == uuid.Nil ||
			item.OrganizationID != organizationID || item.RetirementJobID != jobID {
			return errors.New("sample retirement item identity or ordinal is invalid")
		}
		if _, exists := seenItems[item.ID]; exists {
			return errors.New("duplicate sample retirement item")
		}
		if _, exists := seenSubjects[item.SubjectID]; exists {
			return errors.New("duplicate sample retirement subject")
		}
		seenItems[item.ID] = struct{}{}
		seenSubjects[item.SubjectID] = struct{}{}
		if item.State == types.SampleRetirementItemApplied ||
			item.State == types.SampleRetirementItemSkipped {
			continue
		}
		report, exists := reports[item.SubjectID]
		if !exists {
			return errors.New("reverse-reference report is missing")
		}
		if report.SubjectOrganizationID != organizationID {
			return errors.New("sample retirement subject organization does not match")
		}
		if report.Subject.SubjectType != item.SubjectType ||
			report.Subject.SubjectID != item.SubjectID {
			return errors.New("reverse-reference subject identity does not match")
		}
		if report.Subject.OwnershipMarker != item.OwnershipMarker ||
			report.Subject.OwnershipChecksum != item.OwnershipChecksum {
			return errors.New("sample retirement ownership proof changed")
		}
		if report.CurrentChecksum != item.ExpectedChecksum ||
			report.Subject.ExpectedChecksum != item.ExpectedChecksum {
			return errors.New("sample retirement subject checksum changed")
		}
		if item.BeforeCount != 1 || report.BeforeCount != item.BeforeCount {
			return errors.New("sample retirement before count changed")
		}
		if !report.Immutable {
			return errors.New("sample retirement subject is not immutable")
		}
		if report.ProtectedReferenceCount != 0 {
			return errors.New("protected reverse reference blocks retirement")
		}
		if report.CrossOrganizationReferenceCount != 0 {
			return errors.New("cross-organization reference blocks retirement")
		}
		if !report.Retirable || len(report.BlockingReasons) != 0 {
			return errors.New("reverse-reference report blocks retirement")
		}
	}
	return nil
}

func validateEvidence(label, reference, checksum string) error {
	if reference == "" || strings.TrimSpace(reference) != reference {
		return fmt.Errorf("%s evidence reference must be a non-empty exact value", label)
	}
	if !retirementChecksumPattern.MatchString(checksum) {
		return fmt.Errorf("%s evidence checksum must be canonical lowercase sha256", label)
	}
	return nil
}

func buildAuditSubjectTombstone(
	job types.SampleRetirementJob,
	item types.SampleRetirementItem,
	request types.SampleRetirementApplyRequest,
	auditEventCount int,
	id uuid.UUID,
	retiredAt time.Time,
) (types.AuditSubjectTombstone, error) {
	tombstone := types.AuditSubjectTombstone{
		ID:                     id,
		CreatedAt:              retiredAt,
		RetiredAt:              retiredAt,
		OrganizationID:         job.OrganizationID,
		RetirementJobID:        job.ID,
		RetirementItemID:       item.ID,
		SubjectType:            item.SubjectType,
		SubjectID:              item.SubjectID,
		OwnershipMarker:        item.OwnershipMarker,
		OwnershipChecksum:      item.OwnershipChecksum,
		SubjectChecksum:        item.ExpectedChecksum,
		AuditEventCount:        auditEventCount,
		RetiredByUserAccountID: request.ActorUserAccountID,
	}
	canonical := struct {
		Version           int                               `json:"version"`
		TombstoneID       uuid.UUID                         `json:"tombstoneId"`
		OrganizationID    uuid.UUID                         `json:"organizationId"`
		JobID             uuid.UUID                         `json:"jobId"`
		ItemID            uuid.UUID                         `json:"itemId"`
		SubjectType       types.SampleRetirementSubjectType `json:"subjectType"`
		SubjectID         uuid.UUID                         `json:"subjectId"`
		OwnershipMarker   string                            `json:"ownershipMarker"`
		OwnershipChecksum string                            `json:"ownershipChecksum"`
		SubjectChecksum   string                            `json:"subjectChecksum"`
		AuditEventCount   int                               `json:"auditEventCount"`
		ActorID           uuid.UUID                         `json:"actorId"`
		PreviewChecksum   string                            `json:"previewChecksum"`
		ApprovalID        string                            `json:"approvalId"`
		ApprovalChecksum  string                            `json:"approvalChecksum"`
		RetiredAt         string                            `json:"retiredAt"`
	}{
		Version: 1, TombstoneID: tombstone.ID, OrganizationID: tombstone.OrganizationID,
		JobID: job.ID, ItemID: item.ID, SubjectType: item.SubjectType,
		SubjectID: item.SubjectID, OwnershipMarker: item.OwnershipMarker,
		OwnershipChecksum: item.OwnershipChecksum, SubjectChecksum: item.ExpectedChecksum,
		AuditEventCount: auditEventCount,
		ActorID:         request.ActorUserAccountID, PreviewChecksum: request.PreviewChecksum,
		ApprovalID: request.ApprovalID, ApprovalChecksum: request.ApprovalChecksum,
		RetiredAt: retiredAt.UTC().Format(time.RFC3339Nano),
	}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return types.AuditSubjectTombstone{}, fmt.Errorf("canonicalize audit tombstone: %w", err)
	}
	sum := sha256.Sum256(payload)
	tombstone.LineageChecksum = "sha256:" + hex.EncodeToString(sum[:])
	return tombstone, nil
}

func referenceReportFor(
	reports []types.ReferenceReport,
	subjectID uuid.UUID,
) (types.ReferenceReport, bool) {
	for _, report := range reports {
		if report.Subject.SubjectID == subjectID {
			return report, true
		}
	}
	return types.ReferenceReport{}, false
}

func validateItemOutcome(
	outcome ApplyItemOutcome,
	ordinal int,
	previousSequence int64,
	previousApplied int,
	previousSkipped int,
	previousTombstones int,
) error {
	checkpoint := outcome.Checkpoint
	if checkpoint.Sequence != previousSequence+1 ||
		checkpoint.LastCompletedOrdinal != ordinal {
		return errors.New("sample retirement checkpoint sequence is not exact")
	}
	switch outcome.ItemState {
	case types.SampleRetirementItemApplied:
		if checkpoint.AppliedItemCount != previousApplied+1 ||
			checkpoint.SkippedItemCount != previousSkipped ||
			checkpoint.TombstoneCount != previousTombstones+1 {
			return errors.New("sample retirement applied counts are not exact")
		}
	case types.SampleRetirementItemSkipped:
		if checkpoint.AppliedItemCount != previousApplied ||
			checkpoint.SkippedItemCount != previousSkipped+1 ||
			checkpoint.TombstoneCount != previousTombstones {
			return errors.New("sample retirement skipped counts are not exact")
		}
	default:
		return errors.New("sample retirement atomic item outcome is invalid")
	}
	return nil
}

func completedNoOp(job types.SampleRetirementJob) *types.SampleRetirementResult {
	completedAt := time.Time{}
	if job.CompletedAt != nil {
		completedAt = job.CompletedAt.UTC()
	}
	return &types.SampleRetirementResult{
		JobID:              job.ID,
		PreviewChecksum:    job.PreviewChecksum,
		State:              job.State,
		AppliedCount:       job.AppliedItemCount,
		SkippedCount:       job.SkippedItemCount,
		TombstoneCount:     job.TombstoneCount,
		CheckpointSequence: job.LastCheckpointSequence,
		NoOp:               true,
		CompletedAt:        completedAt,
	}
}

func appendProblem(problems []string, problem string) []string {
	for _, existing := range problems {
		if existing == problem {
			return problems
		}
	}
	return append(problems, problem)
}
