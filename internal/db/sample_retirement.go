package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/retirement"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type sampleRetirementSubjectDefinition struct {
	table string
}

var sampleRetirementSubjectDefinitions = map[types.SampleRetirementSubjectType]sampleRetirementSubjectDefinition{
	types.SampleRetirementSubjectApplication:      {table: "application"},
	types.SampleRetirementSubjectDeploymentTarget: {table: "deploymenttarget"},
	types.SampleRetirementSubjectEnvironment:      {table: "environment"},
}

// RegisterSampleRetirementOwnershipEvidence records the independently sourced,
// immutable ownership fact required before a subject can enter a preview.
func RegisterSampleRetirementOwnershipEvidence(
	ctx context.Context,
	input types.SampleRetirementOwnershipEvidenceRegistrationInput,
) (*types.SampleRetirementOwnershipEvidence, error) {
	if err := validateSampleRetirementOwnershipEvidenceInput(input); err != nil {
		return nil, err
	}
	var result *types.SampleRetirementOwnershipEvidence
	err := RunTx(ctx, func(txCtx context.Context) error {
		if err := verifySampleRetirementEvidenceActor(
			txCtx,
			input.OrganizationID,
			input.RecordedByUserAccountID,
		); err != nil {
			return err
		}
		value, inserted, err := insertOrLoadSampleRetirementOwnershipEvidence(
			txCtx,
			input,
		)
		if err != nil {
			return err
		}
		if !sampleRetirementOwnershipEvidenceMatchesInput(value, input) {
			return apierrors.NewConflict(
				"sample retirement ownership evidence is already bound differently",
			)
		}
		if inserted {
			payload, err := json.Marshal(map[string]any{
				"evidenceId":  value.ID,
				"subjectId":   value.SubjectID,
				"subjectType": value.SubjectType,
			})
			if err != nil {
				return fmt.Errorf("marshal sample retirement ownership audit payload: %w", err)
			}
			if _, err := AppendControlPlaneAuditEventInCurrentBoundary(
				txCtx,
				types.ControlPlaneAuditEventInput{
					OrganizationID:                      input.OrganizationID,
					EventType:                           "sample.retirement.ownership_evidence.registered",
					ActorID:                             &input.RecordedByUserAccountID,
					Outcome:                             "SUCCEEDED",
					SampleRetirementOwnershipEvidenceID: &value.ID,
					Payload:                             payload,
				},
			); err != nil {
				return fmt.Errorf("audit sample retirement ownership evidence: %w", err)
			}
		}
		result = value
		return nil
	})
	return result, err
}

// RegisterSampleRetirementRecoveryEvidence records immutable backup or restore
// proof evidence. Preview persistence only resolves these pre-existing facts.
func RegisterSampleRetirementRecoveryEvidence(
	ctx context.Context,
	input types.SampleRetirementRecoveryEvidenceRegistrationInput,
) (*types.SampleRetirementRecoveryEvidence, error) {
	if err := validateSampleRetirementRecoveryEvidenceInput(input); err != nil {
		return nil, err
	}
	input.VerifiedAt = input.VerifiedAt.UTC().Truncate(time.Microsecond)
	var result *types.SampleRetirementRecoveryEvidence
	err := RunTx(ctx, func(txCtx context.Context) error {
		if err := verifySampleRetirementEvidenceActor(
			txCtx,
			input.OrganizationID,
			input.VerifiedByUserAccountID,
		); err != nil {
			return err
		}
		value, inserted, err := insertOrLoadSampleRetirementRecoveryEvidence(
			txCtx,
			input,
		)
		if err != nil {
			return err
		}
		if !sampleRetirementRecoveryEvidenceMatchesInput(value, input) {
			return apierrors.NewConflict(
				"sample retirement recovery evidence is already bound differently",
			)
		}
		if inserted {
			payload, err := json.Marshal(map[string]any{
				"evidenceId":   value.ID,
				"evidenceKind": value.EvidenceKind,
				"sourceId":     value.SourceID,
			})
			if err != nil {
				return fmt.Errorf("marshal sample retirement recovery audit payload: %w", err)
			}
			if _, err := AppendControlPlaneAuditEventInCurrentBoundary(
				txCtx,
				types.ControlPlaneAuditEventInput{
					OrganizationID:                     input.OrganizationID,
					EventType:                          "sample.retirement.recovery_evidence.registered",
					ActorID:                            &input.VerifiedByUserAccountID,
					Outcome:                            "SUCCEEDED",
					SampleRetirementRecoveryEvidenceID: &value.ID,
					Payload:                            payload,
				},
			); err != nil {
				return fmt.Errorf("audit sample retirement recovery evidence: %w", err)
			}
		}
		result = value
		return nil
	})
	return result, err
}

// SampleRetirementRepository is the production adapter for the retirement
// domain's preview and apply stores.
type SampleRetirementRepository struct{}

func (SampleRetirementRepository) InspectSampleRetirementSubjects(
	ctx context.Context,
	organizationID uuid.UUID,
	subjects []types.SampleRetirementSubject,
) ([]types.SampleRetirementCandidate, error) {
	return InspectSampleRetirementSubjects(ctx, organizationID, subjects)
}

func (SampleRetirementRepository) VerifyRetirementReverseReferences(
	ctx context.Context,
	organizationID uuid.UUID,
	subjects []types.SampleRetirementSubject,
) ([]types.ReferenceReport, error) {
	return VerifyRetirementReverseReferences(ctx, organizationID, subjects)
}

func (SampleRetirementRepository) SaveSampleRetirementPreview(
	ctx context.Context,
	preview *types.SampleRetirementPreview,
) (*types.SampleRetirementPreview, error) {
	return SaveSampleRetirementPreview(ctx, preview)
}

func (SampleRetirementRepository) LoadApplySnapshot(
	ctx context.Context,
	organizationID, jobID uuid.UUID,
) (retirement.ApplySnapshot, error) {
	facts, err := LoadSampleRetirementApplySnapshot(ctx, organizationID, jobID)
	if err != nil {
		return retirement.ApplySnapshot{}, err
	}
	return retirement.ApplySnapshot{
		Job: facts.Job, Items: facts.Items,
		LastCheckpoint:         facts.LastCheckpoint,
		ReferenceReports:       facts.CurrentReferenceReports,
		CurrentAuditEventCount: facts.CurrentAuditEventCount,
		LoadedAt:               time.Now().UTC(),
	}, nil
}

func (SampleRetirementRepository) ApplyItemAtomically(
	ctx context.Context,
	command retirement.ApplyItemCommand,
) (retirement.ApplyItemOutcome, error) {
	request := types.SampleRetirementApplyRequest{
		OrganizationID:     command.OrganizationID,
		ActorUserAccountID: command.Tombstone.RetiredByUserAccountID,
		JobID:              command.JobID, PreviewChecksum: command.PreviewChecksum,
		ApprovalID:       command.ApprovalID,
		ApprovalChecksum: command.ApprovalChecksum,
	}
	checkpoint, err := applySampleRetirementItemAtomically(
		ctx,
		command.OrganizationID,
		command.JobID,
		command.Item.ID,
		request,
		&command,
	)
	if err != nil {
		return retirement.ApplyItemOutcome{}, err
	}
	detail, err := GetSampleRetirementDetail(ctx, command.OrganizationID, command.JobID)
	if err != nil {
		return retirement.ApplyItemOutcome{}, err
	}
	var persistedItem types.SampleRetirementItem
	var persistedTombstone *types.AuditSubjectTombstone
	for _, item := range detail.Items {
		if item.ID == command.Item.ID {
			persistedItem = item
			break
		}
	}
	for index := range detail.Tombstones {
		if detail.Tombstones[index].RetirementItemID == command.Item.ID {
			tombstone := detail.Tombstones[index]
			persistedTombstone = &tombstone
			break
		}
	}
	return retirement.ApplyItemOutcome{
		Checkpoint: *checkpoint,
		ItemState:  persistedItem.State,
		Tombstone:  persistedTombstone,
	}, nil
}

func (SampleRetirementRepository) CompleteApplyAtomically(
	ctx context.Context,
	command retirement.CompleteApplyCommand,
) (*types.SampleRetirementResult, error) {
	detail, err := GetSampleRetirementDetail(
		ctx,
		command.OrganizationID,
		command.JobID,
	)
	if err != nil {
		return nil, err
	}
	request := types.SampleRetirementApplyRequest{
		OrganizationID:     command.OrganizationID,
		ActorUserAccountID: detail.Job.RequestedByUserAccountID,
		JobID:              command.JobID, PreviewChecksum: command.PreviewChecksum,
		ApprovalID:       command.ApprovalID,
		ApprovalChecksum: command.ApprovalChecksum,
	}
	return completeSampleRetirementApplyAtomically(
		ctx,
		command.OrganizationID,
		command.JobID,
		request,
		&command,
	)
}

func (SampleRetirementRepository) VerifyApplyAtomically(
	ctx context.Context,
	organizationID, jobID uuid.UUID,
) (retirement.VerificationOutcome, error) {
	report, err := VerifySampleRetirementPersistence(ctx, organizationID, jobID)
	if err != nil {
		return retirement.VerificationOutcome{}, err
	}
	detail, err := GetSampleRetirementDetail(ctx, organizationID, jobID)
	if err != nil {
		return retirement.VerificationOutcome{}, err
	}
	applicationAuditDeleteCount := 0
	for _, tombstone := range detail.Tombstones {
		currentCount, countErr := sampleRetirementAuditEventCount(
			ctx,
			organizationID,
			tombstone.SubjectType,
			tombstone.SubjectID,
		)
		if countErr != nil {
			return retirement.VerificationOutcome{}, countErr
		}
		if currentCount < tombstone.AuditEventCount {
			applicationAuditDeleteCount += tombstone.AuditEventCount - currentCount
		}
	}
	return retirement.VerificationOutcome{
		Report:                      report,
		ApplicationAuditDeleteCount: applicationAuditDeleteCount,
	}, nil
}

var (
	_ retirement.PreviewStore = SampleRetirementRepository{}
	_ retirement.ApplyStore   = SampleRetirementRepository{}
)

const sampleRetirementJobColumns = `
	id, created_at, updated_at, organization_id, requested_by_useraccount_id,
	state, backup_reference, backup_checksum, restore_proof_reference,
	restore_proof_checksum, backup_evidence_id, restore_proof_evidence_id,
	approval_id, approval_checksum, allowlist_checksum, preview_checksum,
	requested_item_count, previewed_item_count, applied_item_count,
	skipped_item_count, tombstone_count, failed_item_count,
	last_checkpoint_sequence, completed_at, verified_at, version`

const sampleRetirementItemColumns = `
	id, created_at, updated_at, organization_id, retirement_job_id, ordinal,
	subject_type, subject_id, ownership_evidence_id, ownership_marker, ownership_checksum,
	expected_checksum, before_count, reference_report_checksum, state,
	applied_at, tombstone_id, error_code, version`

const sampleRetirementCheckpointColumns = `
	id, created_at, organization_id, retirement_job_id, sequence,
	last_completed_ordinal, applied_item_count, skipped_item_count,
	tombstone_count, checkpoint_checksum`

const sampleRetirementTombstoneColumns = `
	id, created_at, retired_at, organization_id, retirement_job_id,
	retirement_item_id, subject_type, subject_id, ownership_marker,
	ownership_checksum, subject_checksum, first_audit_event_id,
	audit_event_count, retired_by_useraccount_id, lineage_checksum`

// InspectSampleRetirementSubjects resolves only exact IDs from the closed set of
// supported active-domain tables. It never falls back to a name, age, wildcard,
// or cross-organization lookup.
func InspectSampleRetirementSubjects(
	ctx context.Context,
	organizationID uuid.UUID,
	requested []types.SampleRetirementSubject,
) ([]types.SampleRetirementCandidate, error) {
	if organizationID == uuid.Nil {
		return nil, apierrors.NewBadRequest("sample retirement organization is required")
	}
	candidates := make([]types.SampleRetirementCandidate, 0, len(requested))
	seen := make(map[string]struct{}, len(requested))
	for _, subject := range requested {
		definition, ok := sampleRetirementSubjectDefinitions[subject.SubjectType]
		if !ok {
			return nil, apierrors.NewBadRequest(
				"unsupported sample retirement subject type",
			)
		}
		if subject.SubjectID == uuid.Nil {
			return nil, apierrors.NewBadRequest("sample retirement subject ID is required")
		}
		key := string(subject.SubjectType) + ":" + subject.SubjectID.String()
		if _, duplicate := seen[key]; duplicate {
			return nil, apierrors.NewBadRequest("duplicate sample retirement subject")
		}
		seen[key] = struct{}{}
		if strings.TrimSpace(subject.OwnershipMarker) == "" {
			return nil, apierrors.NewBadRequest("sample retirement ownership marker is required")
		}
		if subject.OwnershipChecksum != sampleRetirementTextChecksum(subject.OwnershipMarker) {
			return nil, apierrors.NewBadRequest(
				"sample retirement ownership checksum does not match its marker",
			)
		}

		var resolvedOrganizationID uuid.UUID
		var currentChecksum string
		var evidence types.SampleRetirementOwnershipEvidence
		query := fmt.Sprintf(`
			SELECT subject.organization_id, %s,
			       evidence.id, evidence.created_at, evidence.organization_id,
			       evidence.subject_type, evidence.subject_id,
			       evidence.ownership_marker, evidence.ownership_checksum,
			       evidence.source_reference, evidence.source_checksum,
			       evidence.recorded_by_useraccount_id
			FROM %s AS subject
			JOIN SampleRetirementOwnershipEvidence evidence
			  ON evidence.organization_id=subject.organization_id
			 AND evidence.subject_type=@subjectType
			 AND evidence.subject_id=subject.id
			WHERE subject.id=@subjectID
			  AND subject.organization_id=@organizationID
			  AND evidence.ownership_marker=@ownershipMarker
			  AND evidence.ownership_checksum=@ownershipChecksum`,
			sampleRetirementRowChecksumExpression("subject"),
			pgx.Identifier{definition.table}.Sanitize(),
		)
		err := internalctx.GetDb(ctx).QueryRow(ctx, query, pgx.NamedArgs{
			"subjectID": subject.SubjectID, "organizationID": organizationID,
			"subjectType":       subject.SubjectType,
			"ownershipMarker":   subject.OwnershipMarker,
			"ownershipChecksum": subject.OwnershipChecksum,
		}).Scan(
			&resolvedOrganizationID,
			&currentChecksum,
			&evidence.ID,
			&evidence.CreatedAt,
			&evidence.OrganizationID,
			&evidence.SubjectType,
			&evidence.SubjectID,
			&evidence.OwnershipMarker,
			&evidence.OwnershipChecksum,
			&evidence.SourceReference,
			&evidence.SourceChecksum,
			&evidence.RecordedByUserAccountID,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierrors.ErrNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("inspect sample retirement subject: %w", err)
		}
		candidates = append(candidates, types.SampleRetirementCandidate{
			Subject:                  subject,
			OrganizationID:           resolvedOrganizationID,
			CurrentChecksum:          currentChecksum,
			OwnershipEvidenceID:      evidence.ID,
			OwnershipMarker:          evidence.OwnershipMarker,
			OwnershipChecksum:        evidence.OwnershipChecksum,
			OwnershipSourceReference: evidence.SourceReference,
			OwnershipSourceChecksum:  evidence.SourceChecksum,
			BeforeCount:              1,
			Immutable:                true,
		})
	}
	return candidates, nil
}

// VerifyRetirementReverseReferences reports every physical foreign-key
// reference to an exact candidate. Audit correlations are counted separately:
// they are retained evidence, not a reason to delete audit rows.
func VerifyRetirementReverseReferences(
	ctx context.Context,
	organizationID uuid.UUID,
	subjects []types.SampleRetirementSubject,
) ([]types.ReferenceReport, error) {
	candidates, err := InspectSampleRetirementSubjects(ctx, organizationID, subjects)
	if err != nil {
		return nil, err
	}
	reports := make([]types.ReferenceReport, 0, len(candidates))
	for _, candidate := range candidates {
		definition := sampleRetirementSubjectDefinitions[candidate.Subject.SubjectType]
		references, err := sampleRetirementForeignKeyReferences(
			ctx,
			definition.table,
			candidate.Subject.SubjectID,
			organizationID,
		)
		if err != nil {
			return nil, err
		}
		auditEventCount, err := sampleRetirementAuditEventCount(
			ctx,
			organizationID,
			candidate.Subject.SubjectType,
			candidate.Subject.SubjectID,
		)
		if err != nil {
			return nil, err
		}
		crossOrganizationCount := 0
		for _, reference := range references {
			if reference.OrganizationID != organizationID {
				crossOrganizationCount++
			}
		}
		blockingReasons := make([]string, 0, 2)
		if len(references) > 0 {
			blockingReasons = append(blockingReasons, "protected reverse references exist")
		}
		if crossOrganizationCount > 0 {
			blockingReasons = append(
				blockingReasons,
				"cross-organization reverse references exist",
			)
		}
		reports = append(reports, types.ReferenceReport{
			Subject:                         candidate.Subject,
			SubjectOrganizationID:           candidate.OrganizationID,
			CurrentChecksum:                 candidate.CurrentChecksum,
			BeforeCount:                     candidate.BeforeCount,
			Immutable:                       candidate.Immutable,
			References:                      references,
			ProtectedReferenceCount:         len(references),
			CrossOrganizationReferenceCount: crossOrganizationCount,
			AuditEventCount:                 auditEventCount,
			Retirable:                       len(references) == 0 && crossOrganizationCount == 0,
			BlockingReasons:                 blockingReasons,
		})
	}
	return reports, nil
}

// SaveSampleRetirementPreview freezes a preview and its ordered items in one
// transaction. A job ID is create-only; callers cannot mutate an existing
// checksum-bound preview by submitting it again.
func SaveSampleRetirementPreview(
	ctx context.Context,
	preview *types.SampleRetirementPreview,
) (*types.SampleRetirementPreview, error) {
	if err := validateSampleRetirementPreview(preview); err != nil {
		return nil, err
	}
	persisted := *preview
	persisted.Items = append([]types.SampleRetirementItem(nil), preview.Items...)
	persisted.ReferenceReports = append(
		[]types.ReferenceReport(nil),
		preview.ReferenceReports...,
	)
	err := RunTx(ctx, func(txCtx context.Context) error {
		database := internalctx.GetDb(txCtx)
		backupEvidenceID, err := resolveSampleRetirementRecoveryEvidence(
			txCtx,
			persisted.Job.OrganizationID,
			"backup",
			persisted.Job.BackupReference,
			persisted.Job.BackupChecksum,
		)
		if err != nil {
			return err
		}
		restoreProofEvidenceID, err := resolveSampleRetirementRecoveryEvidence(
			txCtx,
			persisted.Job.OrganizationID,
			"restore_proof",
			persisted.Job.RestoreProofReference,
			persisted.Job.RestoreProofChecksum,
		)
		if err != nil {
			return err
		}
		persisted.Job.BackupEvidenceID = backupEvidenceID
		persisted.Job.RestoreProofEvidenceID = restoreProofEvidenceID
		tag, err := database.Exec(txCtx, `
			INSERT INTO SampleRetirementJob (
				id, organization_id, requested_by_useraccount_id, state,
				backup_reference, backup_checksum, restore_proof_reference,
				restore_proof_checksum, backup_evidence_id,
				restore_proof_evidence_id, allowlist_checksum,
				preview_checksum, requested_item_count, previewed_item_count
			) VALUES (
				@id, @organizationID, @requestedBy, @state,
				@backupReference, @backupChecksum, @restoreProofReference,
				@restoreProofChecksum, @backupEvidenceID,
				@restoreProofEvidenceID, @allowlistChecksum,
				@previewChecksum, @requestedItemCount, @previewedItemCount
			)
			ON CONFLICT (id) DO NOTHING`,
			pgx.NamedArgs{
				"id":                     persisted.Job.ID,
				"organizationID":         persisted.Job.OrganizationID,
				"requestedBy":            persisted.Job.RequestedByUserAccountID,
				"state":                  persisted.Job.State,
				"backupReference":        persisted.Job.BackupReference,
				"backupChecksum":         persisted.Job.BackupChecksum,
				"restoreProofReference":  persisted.Job.RestoreProofReference,
				"restoreProofChecksum":   persisted.Job.RestoreProofChecksum,
				"backupEvidenceID":       persisted.Job.BackupEvidenceID,
				"restoreProofEvidenceID": persisted.Job.RestoreProofEvidenceID,
				"allowlistChecksum":      persisted.Job.AllowlistChecksum,
				"previewChecksum":        persisted.Job.PreviewChecksum,
				"requestedItemCount":     persisted.Job.RequestedItemCount,
				"previewedItemCount":     persisted.Job.PreviewedItemCount,
			},
		)
		if err != nil {
			return fmt.Errorf("persist sample retirement job: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return apierrors.NewConflict("sample retirement preview is immutable")
		}
		for index := range persisted.Items {
			item := &persisted.Items[index]
			_, err = database.Exec(txCtx, `
				INSERT INTO SampleRetirementItem (
					id, organization_id, retirement_job_id, ordinal,
					subject_type, subject_id, ownership_evidence_id, ownership_marker,
					ownership_checksum, expected_checksum, before_count,
					reference_report_checksum, state
				) VALUES (
					@id, @organizationID, @jobID, @ordinal,
					@subjectType, @subjectID, @ownershipEvidenceID, @ownershipMarker,
					@ownershipChecksum, @expectedChecksum, @beforeCount,
					@referenceReportChecksum, @state
				)`,
				pgx.NamedArgs{
					"id":                      item.ID,
					"organizationID":          item.OrganizationID,
					"jobID":                   item.RetirementJobID,
					"ordinal":                 item.Ordinal,
					"subjectType":             item.SubjectType,
					"subjectID":               item.SubjectID,
					"ownershipEvidenceID":     item.OwnershipEvidenceID,
					"ownershipMarker":         item.OwnershipMarker,
					"ownershipChecksum":       item.OwnershipChecksum,
					"expectedChecksum":        item.ExpectedChecksum,
					"beforeCount":             item.BeforeCount,
					"referenceReportChecksum": item.ReferenceReportChecksum,
					"state":                   item.State,
				},
			)
			if err != nil {
				return fmt.Errorf("persist sample retirement item: %w", err)
			}
		}
		job, err := getSampleRetirementJob(
			txCtx,
			persisted.Job.OrganizationID,
			persisted.Job.ID,
			false,
		)
		if err != nil {
			return err
		}
		items, err := listSampleRetirementItems(
			txCtx,
			persisted.Job.OrganizationID,
			persisted.Job.ID,
			false,
		)
		if err != nil {
			return err
		}
		persisted.Job = *job
		persisted.Items = items
		persisted.CreatedAt = job.CreatedAt
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &persisted, nil
}

func GetSampleRetirementDetail(
	ctx context.Context,
	organizationID, jobID uuid.UUID,
) (*types.SampleRetirementDetail, error) {
	job, err := getSampleRetirementJob(ctx, organizationID, jobID, false)
	if err != nil {
		return nil, err
	}
	items, err := listSampleRetirementItems(ctx, organizationID, jobID, false)
	if err != nil {
		return nil, err
	}
	checkpoints, err := listSampleRetirementCheckpoints(ctx, organizationID, jobID)
	if err != nil {
		return nil, err
	}
	tombstones, err := listSampleRetirementTombstones(ctx, organizationID, jobID)
	if err != nil {
		return nil, err
	}
	return &types.SampleRetirementDetail{
		Job: *job, Items: items, Checkpoints: checkpoints, Tombstones: tombstones,
	}, nil
}

func LoadSampleRetirementApplySnapshot(
	ctx context.Context,
	organizationID, jobID uuid.UUID,
) (*types.SampleRetirementApplyFacts, error) {
	detail, err := GetSampleRetirementDetail(ctx, organizationID, jobID)
	if err != nil {
		return nil, err
	}
	pendingItems := make([]types.SampleRetirementItem, 0, len(detail.Items))
	for _, item := range detail.Items {
		if item.State == types.SampleRetirementItemPending {
			pendingItems = append(pendingItems, item)
		}
	}
	subjects := sampleRetirementSubjectsFromItems(pendingItems)
	reports, err := VerifyRetirementReverseReferences(ctx, organizationID, subjects)
	if err != nil && !errors.Is(err, apierrors.ErrNotFound) {
		return nil, err
	}
	var lastCheckpoint *types.SampleRetirementCheckpoint
	if len(detail.Checkpoints) > 0 {
		checkpoint := detail.Checkpoints[len(detail.Checkpoints)-1]
		lastCheckpoint = &checkpoint
	}
	auditCount := 0
	for _, item := range detail.Items {
		count, countErr := sampleRetirementAuditEventCount(
			ctx,
			organizationID,
			item.SubjectType,
			item.SubjectID,
		)
		if countErr != nil {
			return nil, countErr
		}
		auditCount += count
	}
	return &types.SampleRetirementApplyFacts{
		Job: detail.Job, Items: detail.Items, LastCheckpoint: lastCheckpoint,
		CurrentReferenceReports: reports, CurrentAuditEventCount: auditCount,
	}, nil
}

// ApplySampleRetirementItemAtomically commits exactly one durable restart
// boundary: job/item locks, live checksum and reverse-reference revalidation,
// tombstone creation, active-row deletion, counts, and checkpoint.
func ApplySampleRetirementItemAtomically(
	ctx context.Context,
	organizationID, jobID, itemID uuid.UUID,
	request types.SampleRetirementApplyRequest,
) (*types.SampleRetirementCheckpoint, error) {
	return applySampleRetirementItemAtomically(
		ctx,
		organizationID,
		jobID,
		itemID,
		request,
		nil,
	)
}

func applySampleRetirementItemAtomically(
	ctx context.Context,
	organizationID, jobID, itemID uuid.UUID,
	request types.SampleRetirementApplyRequest,
	command *retirement.ApplyItemCommand,
) (*types.SampleRetirementCheckpoint, error) {
	var result *types.SampleRetirementCheckpoint
	if err := validateSampleRetirementApplyRequest(organizationID, jobID, request); err != nil {
		return nil, err
	}
	err := RunTx(ctx, func(txCtx context.Context) error {
		job, err := getSampleRetirementJob(txCtx, organizationID, jobID, true)
		if err != nil {
			return err
		}
		if job.PreviewChecksum != request.PreviewChecksum {
			return apierrors.NewConflict("sample retirement preview checksum is stale")
		}
		approvalRequestID, parseErr := uuid.Parse(request.ApprovalID)
		if parseErr != nil {
			return apierrors.NewBadRequest("sample retirement approvalId must be a UUID")
		}
		approvalBinding, err := resolveSampleRetirementApprovalForJob(
			txCtx,
			*job,
			approvalRequestID,
		)
		if err != nil {
			return err
		}
		if request.ApprovalChecksum != approvalBinding.ApprovalChecksum {
			return apierrors.NewConflict(
				"sample retirement approval checksum is stale",
			)
		}
		request.ApprovalID = approvalBinding.ApprovalRequestID.String()
		request.ApprovalChecksum = approvalBinding.ApprovalChecksum
		var actorMembershipCount int
		err = internalctx.GetDb(txCtx).QueryRow(txCtx, `
			SELECT count(*)
			FROM Organization_UserAccount
			WHERE organization_id=@organizationID
			  AND user_account_id=@actorID`,
			pgx.NamedArgs{
				"organizationID": organizationID,
				"actorID":        request.ActorUserAccountID,
			},
		).Scan(&actorMembershipCount)
		if err != nil {
			return fmt.Errorf("verify sample retirement actor scope: %w", err)
		}
		if actorMembershipCount != 1 {
			return apierrors.ErrNotFound
		}
		if job.State != types.SampleRetirementJobPreviewed &&
			job.State != types.SampleRetirementJobApplying {
			return apierrors.NewConflict("sample retirement job cannot be applied")
		}
		if command != nil {
			expectedVersion := command.ExpectedJobVersion +
				command.ExpectedCheckpointSequence
			if command.ExpectedCheckpointSequence > 0 {
				expectedVersion++
			}
			if job.Version != expectedVersion ||
				job.LastCheckpointSequence != command.ExpectedCheckpointSequence ||
				job.AppliedItemCount != command.ExpectedAppliedCount ||
				job.SkippedItemCount != command.ExpectedSkippedCount ||
				job.TombstoneCount != command.ExpectedTombstoneCount {
				return apierrors.NewConflict("sample retirement command checkpoint is stale")
			}
			allItems, listErr := listSampleRetirementItems(
				txCtx,
				organizationID,
				jobID,
				false,
			)
			if listErr != nil {
				return listErr
			}
			currentAuditEventCount := 0
			for _, persistedItem := range allItems {
				count, countErr := sampleRetirementAuditEventCount(
					txCtx,
					organizationID,
					persistedItem.SubjectType,
					persistedItem.SubjectID,
				)
				if countErr != nil {
					return countErr
				}
				currentAuditEventCount += count
			}
			if currentAuditEventCount != command.ExpectedAuditEventCount {
				return apierrors.NewConflict("sample retirement audit count is stale")
			}
		}
		item, err := getSampleRetirementItem(
			txCtx,
			organizationID,
			jobID,
			itemID,
			true,
		)
		if err != nil {
			return err
		}
		if item.State == types.SampleRetirementItemApplied {
			result, err = getLastSampleRetirementCheckpoint(
				txCtx,
				organizationID,
				jobID,
			)
			return err
		}
		if item.State != types.SampleRetirementItemPending {
			return apierrors.NewConflict("sample retirement item is not pending")
		}
		if command != nil &&
			(item.ID != command.Item.ID ||
				item.Version != command.Item.Version ||
				item.SubjectType != command.Item.SubjectType ||
				item.SubjectID != command.Item.SubjectID ||
				item.ExpectedChecksum != command.Item.ExpectedChecksum) {
			return apierrors.NewConflict("sample retirement command item is stale")
		}
		subject := types.SampleRetirementSubject{
			SubjectType: item.SubjectType, SubjectID: item.SubjectID,
			OwnershipMarker:   item.OwnershipMarker,
			OwnershipChecksum: item.OwnershipChecksum,
			ExpectedChecksum:  item.ExpectedChecksum,
		}
		if err := lockSampleRetirementSubject(
			txCtx,
			organizationID,
			item,
		); err != nil {
			return err
		}
		candidates, err := InspectSampleRetirementSubjects(
			txCtx,
			organizationID,
			[]types.SampleRetirementSubject{subject},
		)
		if errors.Is(err, apierrors.ErrNotFound) {
			return apierrors.NewConflict("sample retirement subject disappeared before apply")
		}
		if err != nil {
			return err
		}
		if candidates[0].CurrentChecksum != item.ExpectedChecksum ||
			item.BeforeCount != 1 {
			return apierrors.NewConflict("sample retirement subject checksum is stale")
		}
		reports, err := VerifyRetirementReverseReferences(
			txCtx,
			organizationID,
			[]types.SampleRetirementSubject{subject},
		)
		if err != nil {
			return err
		}
		if !reports[0].Retirable {
			return apierrors.NewConflict(
				"sample retirement subject has protected reverse references",
			)
		}
		currentReportChecksum, err := sampleRetirementReferenceReportChecksum(
			reports[0],
		)
		if err != nil {
			return err
		}
		if currentReportChecksum != item.ReferenceReportChecksum {
			return apierrors.NewConflict(
				"sample retirement reverse-reference report is stale",
			)
		}
		if job.State == types.SampleRetirementJobPreviewed {
			tag, err := internalctx.GetDb(txCtx).Exec(txCtx, `
				UPDATE SampleRetirementJob
				SET state='APPLYING', approval_id=@approvalID,
				    approval_checksum=@approvalChecksum,
				    updated_at=clock_timestamp(), version=version+1
				WHERE id=@jobID AND organization_id=@organizationID
				  AND state='PREVIEWED'
				  AND approval_id IS NULL AND approval_checksum IS NULL`,
				pgx.NamedArgs{
					"jobID": jobID, "organizationID": organizationID,
					"approvalID":       request.ApprovalID,
					"approvalChecksum": request.ApprovalChecksum,
				},
			)
			if err != nil {
				return fmt.Errorf("start sample retirement apply: %w", err)
			}
			if tag.RowsAffected() != 1 {
				return apierrors.NewConflict("sample retirement job state changed")
			}
		}

		tombstoneID := uuid.New()
		firstAuditEventID, auditEventCount, err := sampleRetirementAuditLineage(
			txCtx,
			organizationID,
			item.SubjectType,
			item.SubjectID,
		)
		if err != nil {
			return err
		}
		retiredAt := time.Now().UTC()
		retiredBy := request.ActorUserAccountID
		lineageChecksum := ""
		if command != nil {
			if command.ExpectedAuditEventCount < auditEventCount ||
				command.Tombstone.ID == uuid.Nil ||
				command.Tombstone.OrganizationID != organizationID ||
				command.Tombstone.RetirementJobID != jobID ||
				command.Tombstone.RetirementItemID != item.ID ||
				command.Tombstone.SubjectType != item.SubjectType ||
				command.Tombstone.SubjectID != item.SubjectID ||
				command.Tombstone.SubjectChecksum != item.ExpectedChecksum ||
				command.Tombstone.AuditEventCount != auditEventCount {
				return apierrors.NewConflict("sample retirement tombstone binding is stale")
			}
			tombstoneID = command.Tombstone.ID
			lineageChecksum = command.Tombstone.LineageChecksum
			retiredAt = command.Tombstone.RetiredAt.UTC()
			retiredBy = command.Tombstone.RetiredByUserAccountID
		}
		expectedLineageChecksum, err := sampleRetirementCanonicalTombstoneChecksum(
			job,
			item,
			request,
			tombstoneID,
			retiredAt,
			retiredBy,
			auditEventCount,
		)
		if err != nil {
			return err
		}
		if command != nil &&
			lineageChecksum != expectedLineageChecksum {
			return apierrors.NewConflict("sample retirement tombstone lineage is stale")
		}
		lineageChecksum = expectedLineageChecksum
		_, err = internalctx.GetDb(txCtx).Exec(txCtx, `
			INSERT INTO AuditSubjectTombstone (
				id, created_at, organization_id, retirement_job_id, retirement_item_id,
				subject_type, subject_id, ownership_marker, ownership_checksum,
				subject_checksum, retired_at, first_audit_event_id,
				audit_event_count, retired_by_useraccount_id, lineage_checksum
			) VALUES (
				@id, @retiredAt, @organizationID, @jobID, @itemID,
				@subjectType, @subjectID, @ownershipMarker, @ownershipChecksum,
				@subjectChecksum, @retiredAt, @firstAuditEventID,
				@auditEventCount, @retiredBy, @lineageChecksum
			)`,
			pgx.NamedArgs{
				"id": tombstoneID, "organizationID": organizationID,
				"jobID": jobID, "itemID": item.ID,
				"subjectType": item.SubjectType, "subjectID": item.SubjectID,
				"ownershipMarker":   item.OwnershipMarker,
				"ownershipChecksum": item.OwnershipChecksum,
				"subjectChecksum":   item.ExpectedChecksum,
				"retiredAt":         retiredAt,
				"firstAuditEventID": firstAuditEventID,
				"auditEventCount":   auditEventCount,
				"retiredBy":         retiredBy,
				"lineageChecksum":   lineageChecksum,
			},
		)
		if err != nil {
			return fmt.Errorf("persist sample retirement tombstone: %w", err)
		}
		if err := deleteSampleRetirementSubject(txCtx, organizationID, item); err != nil {
			return err
		}
		tag, err := internalctx.GetDb(txCtx).Exec(txCtx, `
			UPDATE SampleRetirementItem
			SET state='APPLIED', applied_at=clock_timestamp(), tombstone_id=@tombstoneID,
			    updated_at=clock_timestamp(), version=version+1
			WHERE id=@itemID AND retirement_job_id=@jobID
			  AND organization_id=@organizationID AND state='PENDING'`,
			pgx.NamedArgs{
				"tombstoneID": tombstoneID, "itemID": item.ID,
				"jobID": jobID, "organizationID": organizationID,
			},
		)
		if err != nil {
			return fmt.Errorf("complete sample retirement item: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return apierrors.NewConflict("sample retirement item state changed")
		}
		result, err = writeSampleRetirementCheckpoint(
			txCtx,
			organizationID,
			job,
			item.Ordinal,
		)
		return err
	})
	return result, err
}

func CompleteSampleRetirementApplyAtomically(
	ctx context.Context,
	organizationID, jobID uuid.UUID,
	request types.SampleRetirementApplyRequest,
) (*types.SampleRetirementResult, error) {
	return completeSampleRetirementApplyAtomically(
		ctx,
		organizationID,
		jobID,
		request,
		nil,
	)
}

func completeSampleRetirementApplyAtomically(
	ctx context.Context,
	organizationID, jobID uuid.UUID,
	request types.SampleRetirementApplyRequest,
	command *retirement.CompleteApplyCommand,
) (*types.SampleRetirementResult, error) {
	var result *types.SampleRetirementResult
	if err := validateSampleRetirementApplyRequest(organizationID, jobID, request); err != nil {
		return nil, err
	}
	err := RunTx(ctx, func(txCtx context.Context) error {
		job, err := getSampleRetirementJob(txCtx, organizationID, jobID, true)
		if err != nil {
			return err
		}
		if job.PreviewChecksum != request.PreviewChecksum ||
			job.ApprovalID == nil ||
			job.ApprovalChecksum == nil {
			return apierrors.NewConflict("sample retirement completion binding is stale")
		}
		approvalRequestID, parseErr := uuid.Parse(request.ApprovalID)
		if parseErr != nil {
			return apierrors.NewBadRequest("sample retirement approvalId must be a UUID")
		}
		approvalBinding, err := resolveSampleRetirementApprovalForJob(
			txCtx,
			*job,
			approvalRequestID,
		)
		if err != nil {
			return err
		}
		if request.ApprovalChecksum != approvalBinding.ApprovalChecksum {
			return apierrors.NewConflict(
				"sample retirement approval checksum is stale",
			)
		}
		request.ApprovalID = approvalBinding.ApprovalRequestID.String()
		request.ApprovalChecksum = approvalBinding.ApprovalChecksum
		if command != nil &&
			(job.LastCheckpointSequence != command.ExpectedCheckpointSequence ||
				job.AppliedItemCount != command.ExpectedAppliedCount ||
				job.SkippedItemCount != command.ExpectedSkippedCount ||
				job.TombstoneCount != command.ExpectedTombstoneCount) {
			return apierrors.NewConflict("sample retirement completion counts are stale")
		}
		if job.State == types.SampleRetirementJobApplied ||
			job.State == types.SampleRetirementJobVerified {
			completedAt := time.Now().UTC()
			if job.CompletedAt != nil {
				completedAt = *job.CompletedAt
			}
			result = &types.SampleRetirementResult{
				JobID: job.ID, PreviewChecksum: job.PreviewChecksum,
				State: job.State, AppliedCount: job.AppliedItemCount,
				SkippedCount:       job.SkippedItemCount,
				TombstoneCount:     job.TombstoneCount,
				CheckpointSequence: job.LastCheckpointSequence,
				NoOp:               true, CompletedAt: completedAt,
			}
			return nil
		}
		if job.State != types.SampleRetirementJobApplying {
			return apierrors.NewConflict("sample retirement job is not applying")
		}
		if command != nil {
			items, listErr := listSampleRetirementItems(
				txCtx,
				organizationID,
				jobID,
				false,
			)
			if listErr != nil {
				return listErr
			}
			currentAuditEventCount := 0
			for _, item := range items {
				count, countErr := sampleRetirementAuditEventCount(
					txCtx,
					organizationID,
					item.SubjectType,
					item.SubjectID,
				)
				if countErr != nil {
					return countErr
				}
				currentAuditEventCount += count
			}
			if currentAuditEventCount != command.ExpectedAuditEventCount {
				return apierrors.NewConflict("sample retirement completion audit count is stale")
			}
		}
		counts, err := getSampleRetirementCounts(txCtx, organizationID, jobID)
		if err != nil {
			return err
		}
		if counts.pending != 0 || counts.failed != 0 ||
			counts.applied+counts.skipped != job.PreviewedItemCount ||
			counts.tombstones != counts.applied {
			return apierrors.NewConflict("sample retirement counts do not reconcile")
		}
		var completedAt time.Time
		err = internalctx.GetDb(txCtx).QueryRow(txCtx, `
			UPDATE SampleRetirementJob
			SET state='APPLIED', applied_item_count=@applied,
			    skipped_item_count=@skipped, tombstone_count=@tombstones,
			    failed_item_count=@failed, completed_at=clock_timestamp(),
			    updated_at=clock_timestamp(), version=version+1
			WHERE id=@jobID AND organization_id=@organizationID
			  AND state='APPLYING'
			RETURNING completed_at`,
			pgx.NamedArgs{
				"applied": counts.applied, "skipped": counts.skipped,
				"tombstones": counts.tombstones, "failed": counts.failed,
				"jobID": jobID, "organizationID": organizationID,
			},
		).Scan(&completedAt)
		if err != nil {
			return fmt.Errorf("complete sample retirement job: %w", err)
		}
		result = &types.SampleRetirementResult{
			JobID: jobID, PreviewChecksum: request.PreviewChecksum,
			State:        types.SampleRetirementJobApplied,
			AppliedCount: counts.applied, SkippedCount: counts.skipped,
			TombstoneCount:     counts.tombstones,
			CheckpointSequence: job.LastCheckpointSequence,
			CompletedAt:        completedAt,
		}
		return nil
	})
	return result, err
}

// VerifySampleRetirementPersistence is read-only. It reports VERIFIED only
// when active counts, tombstones, lineage bindings, and retained audit
// correlations reconcile; it does not mutate audit or job history.
func VerifySampleRetirementPersistence(
	ctx context.Context,
	organizationID, jobID uuid.UUID,
) (*types.SampleRetirementVerification, error) {
	detail, err := GetSampleRetirementDetail(ctx, organizationID, jobID)
	if err != nil {
		return nil, err
	}
	if detail.Job.State != types.SampleRetirementJobApplied &&
		detail.Job.State != types.SampleRetirementJobVerified {
		return nil, apierrors.NewConflict("sample retirement job is not applied")
	}
	problems := make([]string, 0)
	remaining := 0
	auditEventCount := 0
	tombstonesByItem := make(map[uuid.UUID]types.AuditSubjectTombstone, len(detail.Tombstones))
	for _, tombstone := range detail.Tombstones {
		tombstonesByItem[tombstone.RetirementItemID] = tombstone
	}
	lineageValid := true
	auditEventsRetained := true
	for index := range detail.Items {
		item := &detail.Items[index]
		count, err := countSampleRetirementSubject(ctx, organizationID, item)
		if err != nil {
			return nil, err
		}
		remaining += count
		tombstone, ok := tombstonesByItem[item.ID]
		if item.State == types.SampleRetirementItemApplied {
			if !ok ||
				tombstone.SubjectType != item.SubjectType ||
				tombstone.SubjectID != item.SubjectID ||
				tombstone.SubjectChecksum != item.ExpectedChecksum ||
				tombstone.OrganizationID != organizationID ||
				tombstone.RetirementJobID != jobID {
				lineageValid = false
			}
			if ok && detail.Job.ApprovalID != nil &&
				detail.Job.ApprovalChecksum != nil {
				expectedLineage, checksumErr :=
					sampleRetirementCanonicalTombstoneChecksum(
						&detail.Job,
						item,
						types.SampleRetirementApplyRequest{
							OrganizationID:     organizationID,
							ActorUserAccountID: tombstone.RetiredByUserAccountID,
							JobID:              jobID,
							PreviewChecksum:    detail.Job.PreviewChecksum,
							ApprovalID:         *detail.Job.ApprovalID,
							ApprovalChecksum:   *detail.Job.ApprovalChecksum,
						},
						tombstone.ID,
						tombstone.RetiredAt,
						tombstone.RetiredByUserAccountID,
						tombstone.AuditEventCount,
					)
				if checksumErr != nil ||
					tombstone.LineageChecksum != expectedLineage {
					lineageValid = false
				}
			}
		}
		currentFirstAuditEventID, currentAuditCount, err :=
			sampleRetirementAuditLineage(
				ctx,
				organizationID,
				item.SubjectType,
				item.SubjectID,
			)
		if err != nil {
			return nil, err
		}
		auditEventCount += currentAuditCount
		if ok &&
			(currentAuditCount < tombstone.AuditEventCount ||
				!sampleRetirementUUIDPointersEqual(
					currentFirstAuditEventID,
					tombstone.FirstAuditEventID,
				)) {
			auditEventsRetained = false
		}
	}
	checkpointsValid := len(detail.Checkpoints) ==
		int(detail.Job.LastCheckpointSequence) &&
		len(detail.Checkpoints) ==
			detail.Job.AppliedItemCount+detail.Job.SkippedItemCount
	previousOrdinal := 0
	for index, checkpoint := range detail.Checkpoints {
		if checkpoint.Sequence != int64(index+1) ||
			checkpoint.LastCompletedOrdinal <= previousOrdinal ||
			checkpoint.TombstoneCount != checkpoint.AppliedItemCount ||
			checkpoint.AppliedItemCount+checkpoint.SkippedItemCount != index+1 {
			checkpointsValid = false
		}
		previousOrdinal = checkpoint.LastCompletedOrdinal
	}
	if len(detail.Checkpoints) > 0 {
		last := detail.Checkpoints[len(detail.Checkpoints)-1]
		if last.Sequence != detail.Job.LastCheckpointSequence ||
			last.AppliedItemCount != detail.Job.AppliedItemCount ||
			last.SkippedItemCount != detail.Job.SkippedItemCount ||
			last.TombstoneCount != detail.Job.TombstoneCount {
			checkpointsValid = false
		}
	}
	exactCounts := checkpointsValid && remaining == 0 &&
		detail.Job.AppliedItemCount == len(detail.Tombstones) &&
		detail.Job.TombstoneCount == len(detail.Tombstones) &&
		detail.Job.AppliedItemCount+detail.Job.SkippedItemCount ==
			detail.Job.PreviewedItemCount
	if !exactCounts {
		problems = append(problems, "sample retirement counts do not reconcile")
	}
	if !checkpointsValid {
		problems = append(problems, "sample retirement checkpoints do not reconcile")
	}
	if !lineageValid {
		problems = append(problems, "sample retirement tombstone lineage is invalid")
	}
	if !auditEventsRetained {
		problems = append(problems, "sample retirement audit events are not retained")
	}
	state := detail.Job.State
	if exactCounts && lineageValid && auditEventsRetained {
		state = types.SampleRetirementJobVerified
	}
	return &types.SampleRetirementVerification{
		JobID: jobID, State: state,
		PreviewChecksum: detail.Job.PreviewChecksum,
		ExactCounts:     exactCounts, TombstoneLineageValid: lineageValid,
		AuditEventsRetained: auditEventsRetained, RemainingSubjectCount: remaining,
		AuditEventCount: auditEventCount,
		AppliedCount:    detail.Job.AppliedItemCount,
		TombstoneCount:  detail.Job.TombstoneCount,
		VerifiedAt:      time.Now().UTC(), Problems: problems,
	}, nil
}

type sampleRetirementForeignKey struct {
	schemaName      string
	tableName       string
	columnName      string
	hasUUIDID       bool
	hasOrganization bool
}

func sampleRetirementForeignKeyReferences(
	ctx context.Context,
	targetTable string,
	subjectID, subjectOrganizationID uuid.UUID,
) ([]types.RetirementReference, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT
			namespace.nspname,
			source.relname,
			source_column.attname,
			EXISTS (
				SELECT 1 FROM pg_attribute id_column
				WHERE id_column.attrelid=source.oid
				  AND id_column.attname='id'
				  AND id_column.atttypid='uuid'::regtype
				  AND NOT id_column.attisdropped
			),
			EXISTS (
				SELECT 1 FROM pg_attribute organization_column
				WHERE organization_column.attrelid=source.oid
				  AND organization_column.attname='organization_id'
				  AND organization_column.atttypid='uuid'::regtype
				  AND NOT organization_column.attisdropped
			)
		FROM pg_constraint constraint_definition
		JOIN pg_class source
		  ON source.oid=constraint_definition.conrelid
		JOIN pg_namespace namespace
		  ON namespace.oid=source.relnamespace
		JOIN LATERAL unnest(constraint_definition.conkey)
		  WITH ORDINALITY AS source_key(attnum, ordinal)
		  ON TRUE
		JOIN LATERAL unnest(constraint_definition.confkey)
		  WITH ORDINALITY AS target_key(attnum, ordinal)
		  ON target_key.ordinal=source_key.ordinal
		JOIN pg_attribute source_column
		  ON source_column.attrelid=source.oid
		 AND source_column.attnum=source_key.attnum
		JOIN pg_attribute target_column
		  ON target_column.attrelid=constraint_definition.confrelid
		 AND target_column.attnum=target_key.attnum
		WHERE constraint_definition.contype='f'
		  AND constraint_definition.confrelid=to_regclass(@targetTable)
		  AND target_column.attname='id'
		ORDER BY namespace.nspname, source.relname, source_column.attname`,
		pgx.NamedArgs{"targetTable": targetTable},
	)
	if err != nil {
		return nil, fmt.Errorf("discover sample retirement reverse references: %w", err)
	}
	foreignKeys, err := pgx.CollectRows(
		rows,
		func(row pgx.CollectableRow) (sampleRetirementForeignKey, error) {
			var foreignKey sampleRetirementForeignKey
			err := row.Scan(
				&foreignKey.schemaName,
				&foreignKey.tableName,
				&foreignKey.columnName,
				&foreignKey.hasUUIDID,
				&foreignKey.hasOrganization,
			)
			return foreignKey, err
		},
	)
	if err != nil {
		return nil, fmt.Errorf("collect sample retirement reverse references: %w", err)
	}

	references := make([]types.RetirementReference, 0)
	for _, foreignKey := range foreignKeys {
		sourceIDExpression := fmt.Sprintf(
			"md5(%s || ':' || row_to_json(source_row)::text)::uuid",
			sampleRetirementSQLLiteral(foreignKey.tableName),
		)
		if foreignKey.hasUUIDID {
			sourceIDExpression = "source_row.id"
		}
		organizationExpression := "@subjectOrganizationID"
		if foreignKey.hasOrganization {
			organizationExpression = "source_row.organization_id"
		}
		query := fmt.Sprintf(`
			SELECT %s, %s
			FROM %s AS source_row
			WHERE %s=@subjectID
			ORDER BY 1`,
			sourceIDExpression,
			organizationExpression,
			pgx.Identifier{foreignKey.schemaName, foreignKey.tableName}.Sanitize(),
			pgx.Identifier{foreignKey.columnName}.Sanitize(),
		)
		rows, err := internalctx.GetDb(ctx).Query(ctx, query, pgx.NamedArgs{
			"subjectID":             subjectID,
			"subjectOrganizationID": subjectOrganizationID,
		})
		if err != nil {
			return nil, fmt.Errorf(
				"query sample retirement reverse reference %s.%s: %w",
				foreignKey.tableName,
				foreignKey.columnName,
				err,
			)
		}
		found, err := pgx.CollectRows(
			rows,
			func(row pgx.CollectableRow) (types.RetirementReference, error) {
				reference := types.RetirementReference{
					SourceType: strings.ToLower(foreignKey.tableName),
					Relationship: strings.ToLower(
						foreignKey.tableName + "." + foreignKey.columnName,
					),
					Protected: true,
				}
				err := row.Scan(&reference.SourceID, &reference.OrganizationID)
				return reference, err
			},
		)
		if err != nil {
			return nil, fmt.Errorf(
				"collect sample retirement reverse reference %s.%s: %w",
				foreignKey.tableName,
				foreignKey.columnName,
				err,
			)
		}
		references = append(references, found...)
	}
	return references, nil
}

func validateSampleRetirementPreview(preview *types.SampleRetirementPreview) error {
	if preview == nil {
		return apierrors.NewBadRequest("sample retirement preview is required")
	}
	job := preview.Job
	if job.ID == uuid.Nil || job.OrganizationID == uuid.Nil ||
		job.RequestedByUserAccountID == uuid.Nil {
		return apierrors.NewBadRequest("sample retirement preview identity is incomplete")
	}
	if job.State != types.SampleRetirementJobPreviewed {
		return apierrors.NewBadRequest("sample retirement preview must start PREVIEWED")
	}
	if job.ApprovalID != nil || job.ApprovalChecksum != nil {
		return apierrors.NewBadRequest("sample retirement preview cannot bind approval")
	}
	if job.PreviewChecksum == "" || job.PreviewChecksum != preview.PreviewChecksum {
		return apierrors.NewBadRequest("sample retirement preview checksum does not match")
	}
	if job.BackupReference == "" || job.RestoreProofReference == "" ||
		!sampleRetirementIsChecksum(job.BackupChecksum) ||
		!sampleRetirementIsChecksum(job.RestoreProofChecksum) ||
		!sampleRetirementIsChecksum(job.AllowlistChecksum) ||
		!sampleRetirementIsChecksum(job.PreviewChecksum) {
		return apierrors.NewBadRequest(
			"sample retirement backup, restore proof, and checksums are required",
		)
	}
	if len(preview.Items) == 0 ||
		job.RequestedItemCount != len(preview.Items) ||
		job.PreviewedItemCount != len(preview.Items) {
		return apierrors.NewBadRequest("sample retirement preview counts do not match")
	}
	seenOrdinals := make(map[int]struct{}, len(preview.Items))
	seenSubjects := make(map[string]struct{}, len(preview.Items))
	for index := range preview.Items {
		item := &preview.Items[index]
		if _, ok := sampleRetirementSubjectDefinitions[item.SubjectType]; !ok {
			return apierrors.NewBadRequest(
				"unsupported sample retirement subject type",
			)
		}
		if item.ID == uuid.Nil ||
			item.OwnershipEvidenceID == uuid.Nil ||
			item.OrganizationID != job.OrganizationID ||
			item.RetirementJobID != job.ID ||
			item.State != types.SampleRetirementItemPending ||
			item.BeforeCount != 1 ||
			item.Ordinal < 1 ||
			!sampleRetirementIsChecksum(item.OwnershipChecksum) ||
			!sampleRetirementIsChecksum(item.ExpectedChecksum) ||
			!sampleRetirementIsChecksum(item.ReferenceReportChecksum) {
			return apierrors.NewBadRequest("sample retirement item is invalid")
		}
		if item.OwnershipChecksum != sampleRetirementTextChecksum(item.OwnershipMarker) {
			return apierrors.NewBadRequest("sample retirement ownership checksum is invalid")
		}
		if _, exists := seenOrdinals[item.Ordinal]; exists {
			return apierrors.NewBadRequest("sample retirement item ordinal is duplicated")
		}
		seenOrdinals[item.Ordinal] = struct{}{}
		key := string(item.SubjectType) + ":" + item.SubjectID.String()
		if _, exists := seenSubjects[key]; exists {
			return apierrors.NewBadRequest("sample retirement subject is duplicated")
		}
		seenSubjects[key] = struct{}{}
	}
	return nil
}

func getSampleRetirementJob(
	ctx context.Context,
	organizationID, jobID uuid.UUID,
	lock bool,
) (*types.SampleRetirementJob, error) {
	lockClause := ""
	if lock {
		lockClause = " FOR UPDATE"
	}
	rows, err := internalctx.GetDb(ctx).Query(
		ctx,
		`SELECT `+sampleRetirementJobColumns+`
		 FROM SampleRetirementJob
		 WHERE id=@jobID AND organization_id=@organizationID`+lockClause,
		pgx.NamedArgs{"jobID": jobID, "organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("query sample retirement job: %w", err)
	}
	job, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.SampleRetirementJob],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan sample retirement job: %w", err)
	}
	return &job, nil
}

func listSampleRetirementItems(
	ctx context.Context,
	organizationID, jobID uuid.UUID,
	lock bool,
) ([]types.SampleRetirementItem, error) {
	lockClause := ""
	if lock {
		lockClause = " FOR UPDATE"
	}
	rows, err := internalctx.GetDb(ctx).Query(
		ctx,
		`SELECT `+sampleRetirementItemColumns+`
		 FROM SampleRetirementItem
		 WHERE retirement_job_id=@jobID AND organization_id=@organizationID
		 ORDER BY ordinal, id`+lockClause,
		pgx.NamedArgs{"jobID": jobID, "organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("query sample retirement items: %w", err)
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.SampleRetirementItem])
	if err != nil {
		return nil, fmt.Errorf("scan sample retirement items: %w", err)
	}
	return items, nil
}

func getSampleRetirementItem(
	ctx context.Context,
	organizationID, jobID, itemID uuid.UUID,
	lock bool,
) (*types.SampleRetirementItem, error) {
	lockClause := ""
	if lock {
		lockClause = " FOR UPDATE"
	}
	rows, err := internalctx.GetDb(ctx).Query(
		ctx,
		`SELECT `+sampleRetirementItemColumns+`
		 FROM SampleRetirementItem
		 WHERE id=@itemID AND retirement_job_id=@jobID
		   AND organization_id=@organizationID`+lockClause,
		pgx.NamedArgs{
			"itemID": itemID, "jobID": jobID, "organizationID": organizationID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("query sample retirement item: %w", err)
	}
	item, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.SampleRetirementItem],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan sample retirement item: %w", err)
	}
	return &item, nil
}

func listSampleRetirementCheckpoints(
	ctx context.Context,
	organizationID, jobID uuid.UUID,
) ([]types.SampleRetirementCheckpoint, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+sampleRetirementCheckpointColumns+`
		FROM SampleRetirementCheckpoint
		WHERE retirement_job_id=@jobID AND organization_id=@organizationID
		ORDER BY sequence`,
		pgx.NamedArgs{"jobID": jobID, "organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("query sample retirement checkpoints: %w", err)
	}
	checkpoints, err := pgx.CollectRows(
		rows,
		pgx.RowToStructByName[types.SampleRetirementCheckpoint],
	)
	if err != nil {
		return nil, fmt.Errorf("scan sample retirement checkpoints: %w", err)
	}
	return checkpoints, nil
}

func getLastSampleRetirementCheckpoint(
	ctx context.Context,
	organizationID, jobID uuid.UUID,
) (*types.SampleRetirementCheckpoint, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+sampleRetirementCheckpointColumns+`
		FROM SampleRetirementCheckpoint
		WHERE retirement_job_id=@jobID AND organization_id=@organizationID
		ORDER BY sequence DESC
		LIMIT 1`,
		pgx.NamedArgs{"jobID": jobID, "organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("query sample retirement checkpoint: %w", err)
	}
	checkpoint, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.SampleRetirementCheckpoint],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.NewConflict("sample retirement checkpoint is missing")
	}
	if err != nil {
		return nil, fmt.Errorf("scan sample retirement checkpoint: %w", err)
	}
	return &checkpoint, nil
}

func listSampleRetirementTombstones(
	ctx context.Context,
	organizationID, jobID uuid.UUID,
) ([]types.AuditSubjectTombstone, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+sampleRetirementTombstoneColumns+`
		FROM AuditSubjectTombstone
		WHERE retirement_job_id=@jobID AND organization_id=@organizationID
		ORDER BY retired_at, id`,
		pgx.NamedArgs{"jobID": jobID, "organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("query sample retirement tombstones: %w", err)
	}
	tombstones, err := pgx.CollectRows(
		rows,
		pgx.RowToStructByName[types.AuditSubjectTombstone],
	)
	if err != nil {
		return nil, fmt.Errorf("scan sample retirement tombstones: %w", err)
	}
	return tombstones, nil
}

type sampleRetirementCounts struct {
	applied    int
	skipped    int
	failed     int
	pending    int
	tombstones int
}

func getSampleRetirementCounts(
	ctx context.Context,
	organizationID, jobID uuid.UUID,
) (sampleRetirementCounts, error) {
	var counts sampleRetirementCounts
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE state='APPLIED'),
			count(*) FILTER (WHERE state='SKIPPED'),
			count(*) FILTER (WHERE state='FAILED'),
			count(*) FILTER (WHERE state='PENDING'),
			(
				SELECT count(*)
				FROM AuditSubjectTombstone tombstone
				WHERE tombstone.retirement_job_id=@jobID
				  AND tombstone.organization_id=@organizationID
			)
		FROM SampleRetirementItem
		WHERE retirement_job_id=@jobID AND organization_id=@organizationID`,
		pgx.NamedArgs{"jobID": jobID, "organizationID": organizationID},
	).Scan(
		&counts.applied,
		&counts.skipped,
		&counts.failed,
		&counts.pending,
		&counts.tombstones,
	)
	if err != nil {
		return counts, fmt.Errorf("count sample retirement outcomes: %w", err)
	}
	return counts, nil
}

func writeSampleRetirementCheckpoint(
	ctx context.Context,
	organizationID uuid.UUID,
	job *types.SampleRetirementJob,
	lastCompletedOrdinal int,
) (*types.SampleRetirementCheckpoint, error) {
	counts, err := getSampleRetirementCounts(ctx, organizationID, job.ID)
	if err != nil {
		return nil, err
	}
	sequence := job.LastCheckpointSequence + 1
	checkpointID := uuid.New()
	checksum := sampleRetirementTextChecksum(fmt.Sprintf(
		"%s:%s:%d:%d:%d:%d:%d",
		organizationID,
		job.ID,
		sequence,
		lastCompletedOrdinal,
		counts.applied,
		counts.skipped,
		counts.tombstones,
	))
	var checkpoint types.SampleRetirementCheckpoint
	err = internalctx.GetDb(ctx).QueryRow(ctx, `
		INSERT INTO SampleRetirementCheckpoint (
			id, organization_id, retirement_job_id, sequence,
			last_completed_ordinal, applied_item_count,
			skipped_item_count, tombstone_count, checkpoint_checksum
		) VALUES (
			@id, @organizationID, @jobID, @sequence,
			@lastCompletedOrdinal, @appliedItemCount,
			@skippedItemCount, @tombstoneCount, @checkpointChecksum
		)
		RETURNING `+sampleRetirementCheckpointColumns,
		pgx.NamedArgs{
			"id": checkpointID, "organizationID": organizationID,
			"jobID": job.ID, "sequence": sequence,
			"lastCompletedOrdinal": lastCompletedOrdinal,
			"appliedItemCount":     counts.applied,
			"skippedItemCount":     counts.skipped,
			"tombstoneCount":       counts.tombstones,
			"checkpointChecksum":   checksum,
		},
	).Scan(
		&checkpoint.ID,
		&checkpoint.CreatedAt,
		&checkpoint.OrganizationID,
		&checkpoint.RetirementJobID,
		&checkpoint.Sequence,
		&checkpoint.LastCompletedOrdinal,
		&checkpoint.AppliedItemCount,
		&checkpoint.SkippedItemCount,
		&checkpoint.TombstoneCount,
		&checkpoint.CheckpointChecksum,
	)
	if err != nil {
		return nil, fmt.Errorf("persist sample retirement checkpoint: %w", err)
	}
	tag, err := internalctx.GetDb(ctx).Exec(ctx, `
		UPDATE SampleRetirementJob
		SET applied_item_count=@applied, skipped_item_count=@skipped,
		    tombstone_count=@tombstones, failed_item_count=@failed,
		    last_checkpoint_sequence=@sequence,
		    updated_at=clock_timestamp(), version=version+1
		WHERE id=@jobID AND organization_id=@organizationID
		  AND last_checkpoint_sequence=@previousSequence`,
		pgx.NamedArgs{
			"applied": counts.applied, "skipped": counts.skipped,
			"tombstones": counts.tombstones, "failed": counts.failed,
			"sequence": sequence, "jobID": job.ID,
			"organizationID":   organizationID,
			"previousSequence": job.LastCheckpointSequence,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("advance sample retirement checkpoint: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return nil, apierrors.NewConflict("sample retirement checkpoint changed")
	}
	return &checkpoint, nil
}

func lockSampleRetirementSubject(
	ctx context.Context,
	organizationID uuid.UUID,
	item *types.SampleRetirementItem,
) error {
	definition, ok := sampleRetirementSubjectDefinitions[item.SubjectType]
	if !ok {
		return apierrors.NewBadRequest("unsupported sample retirement subject type")
	}
	query := fmt.Sprintf(
		`SELECT id FROM %s WHERE id=@subjectID AND organization_id=@organizationID FOR UPDATE`,
		pgx.Identifier{definition.table}.Sanitize(),
	)
	var lockedID uuid.UUID
	err := internalctx.GetDb(ctx).QueryRow(ctx, query, pgx.NamedArgs{
		"subjectID": item.SubjectID, "organizationID": organizationID,
	}).Scan(&lockedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return apierrors.NewConflict("sample retirement subject disappeared before apply")
	}
	if err != nil {
		return fmt.Errorf("lock sample retirement subject: %w", err)
	}
	return nil
}

func deleteSampleRetirementSubject(
	ctx context.Context,
	organizationID uuid.UUID,
	item *types.SampleRetirementItem,
) error {
	definition, ok := sampleRetirementSubjectDefinitions[item.SubjectType]
	if !ok {
		return apierrors.NewBadRequest("unsupported sample retirement subject type")
	}
	query := fmt.Sprintf(`
		DELETE FROM %s AS subject
		WHERE id=@subjectID
		  AND organization_id=@organizationID
		  AND %s=@expectedChecksum`,
		pgx.Identifier{definition.table}.Sanitize(),
		sampleRetirementRowChecksumExpression("subject"),
	)
	tag, err := internalctx.GetDb(ctx).Exec(ctx, query, pgx.NamedArgs{
		"subjectID": item.SubjectID, "organizationID": organizationID,
		"expectedChecksum": item.ExpectedChecksum,
	})
	if err != nil {
		return fmt.Errorf("delete exact sample retirement subject: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return apierrors.NewConflict("sample retirement subject changed before deletion")
	}
	return nil
}

func countSampleRetirementSubject(
	ctx context.Context,
	organizationID uuid.UUID,
	item *types.SampleRetirementItem,
) (int, error) {
	definition, ok := sampleRetirementSubjectDefinitions[item.SubjectType]
	if !ok {
		return 0, apierrors.NewBadRequest("unsupported sample retirement subject type")
	}
	query := fmt.Sprintf(
		`SELECT count(*) FROM %s WHERE id=@subjectID AND organization_id=@organizationID`,
		pgx.Identifier{definition.table}.Sanitize(),
	)
	var count int
	err := internalctx.GetDb(ctx).QueryRow(ctx, query, pgx.NamedArgs{
		"subjectID": item.SubjectID, "organizationID": organizationID,
	}).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count sample retirement subject: %w", err)
	}
	return count, nil
}

func sampleRetirementAuditEventCount(
	ctx context.Context,
	organizationID uuid.UUID,
	subjectType types.SampleRetirementSubjectType,
	subjectID uuid.UUID,
) (int, error) {
	var count int
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT count(DISTINCT event_subject.event_id)
		FROM ControlPlaneAuditEventSubject event_subject
		JOIN ControlPlaneAuditEvent event
		  ON event.id=event_subject.event_id
		 AND event.organization_id=event_subject.organization_id
		WHERE event_subject.organization_id=@organizationID
		  AND event_subject.correlation_kind=@subjectType
		  AND event_subject.subject_id=@subjectID`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"subjectType":    subjectType,
			"subjectID":      subjectID,
		},
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count retained sample retirement audit events: %w", err)
	}
	return count, nil
}

func sampleRetirementAuditLineage(
	ctx context.Context,
	organizationID uuid.UUID,
	subjectType types.SampleRetirementSubjectType,
	subjectID uuid.UUID,
) (*uuid.UUID, int, error) {
	var firstEventID *uuid.UUID
	var count int
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT
			(array_agg(event.id ORDER BY event.sequence, event.id))[1],
			count(DISTINCT event.id)
		FROM ControlPlaneAuditEventSubject event_subject
		JOIN ControlPlaneAuditEvent event
		  ON event.id=event_subject.event_id
		 AND event.organization_id=event_subject.organization_id
		WHERE event_subject.organization_id=@organizationID
		  AND event_subject.correlation_kind=@subjectType
		  AND event_subject.subject_id=@subjectID`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"subjectType":    subjectType,
			"subjectID":      subjectID,
		},
	).Scan(&firstEventID, &count)
	if err != nil {
		return nil, 0, fmt.Errorf("load retained sample retirement audit lineage: %w", err)
	}
	return firstEventID, count, nil
}

func validateSampleRetirementApplyRequest(
	organizationID, jobID uuid.UUID,
	request types.SampleRetirementApplyRequest,
) error {
	if organizationID == uuid.Nil || jobID == uuid.Nil ||
		request.OrganizationID != organizationID ||
		request.JobID != jobID ||
		request.ActorUserAccountID == uuid.Nil {
		return apierrors.NewBadRequest("sample retirement apply identity is invalid")
	}
	if !sampleRetirementIsChecksum(request.PreviewChecksum) ||
		strings.TrimSpace(request.ApprovalID) == "" ||
		!sampleRetirementIsChecksum(request.ApprovalChecksum) {
		return apierrors.NewBadRequest("sample retirement approval binding is invalid")
	}
	return nil
}

func validateSampleRetirementOwnershipEvidenceInput(
	input types.SampleRetirementOwnershipEvidenceRegistrationInput,
) error {
	if input.OrganizationID == uuid.Nil ||
		input.RecordedByUserAccountID == uuid.Nil ||
		!input.SubjectType.IsValid() ||
		input.SubjectID == uuid.Nil {
		return apierrors.NewBadRequest(
			"sample retirement ownership evidence identity is invalid",
		)
	}
	if !sampleRetirementExactBoundedText(input.OwnershipMarker, 256) ||
		input.OwnershipChecksum != sampleRetirementTextChecksum(input.OwnershipMarker) {
		return apierrors.NewBadRequest(
			"sample retirement ownership evidence marker is invalid",
		)
	}
	if !sampleRetirementExactBoundedText(input.SourceReference, 1024) ||
		!sampleRetirementIsChecksum(input.SourceChecksum) {
		return apierrors.NewBadRequest(
			"sample retirement ownership evidence source is invalid",
		)
	}
	return nil
}

func validateSampleRetirementRecoveryEvidenceInput(
	input types.SampleRetirementRecoveryEvidenceRegistrationInput,
) error {
	if input.OrganizationID == uuid.Nil ||
		input.VerifiedByUserAccountID == uuid.Nil ||
		!input.EvidenceKind.IsValid() ||
		input.SourceID == uuid.Nil {
		return apierrors.NewBadRequest(
			"sample retirement recovery evidence identity is invalid",
		)
	}
	if !sampleRetirementExactBoundedText(input.Reference, 1024) ||
		!sampleRetirementIsChecksum(input.Checksum) {
		return apierrors.NewBadRequest(
			"sample retirement recovery evidence reference is invalid",
		)
	}
	if !controlPlaneAuditEventTypePattern.MatchString(input.SourceKind) ||
		!sampleRetirementIsChecksum(input.SourceChecksum) ||
		input.VerifiedAt.IsZero() ||
		input.VerifiedAt.After(time.Now().UTC()) {
		return apierrors.NewBadRequest(
			"sample retirement recovery evidence source is invalid",
		)
	}
	return nil
}

func sampleRetirementExactBoundedText(value string, maximum int) bool {
	return value != "" &&
		value == strings.TrimSpace(value) &&
		len([]rune(value)) <= maximum &&
		!strings.ContainsAny(value, "\r\n")
}

func verifySampleRetirementEvidenceActor(
	ctx context.Context,
	organizationID, actorID uuid.UUID,
) error {
	var membershipCount int
	if err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT count(*)
		FROM Organization_UserAccount
		WHERE organization_id=@organizationID
		  AND user_account_id=@actorID`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"actorID":        actorID,
		},
	).Scan(&membershipCount); err != nil {
		return fmt.Errorf("verify sample retirement evidence actor scope: %w", err)
	}
	if membershipCount != 1 {
		return apierrors.ErrNotFound
	}
	return nil
}

func insertOrLoadSampleRetirementOwnershipEvidence(
	ctx context.Context,
	input types.SampleRetirementOwnershipEvidenceRegistrationInput,
) (*types.SampleRetirementOwnershipEvidence, bool, error) {
	value, err := scanSampleRetirementOwnershipEvidence(
		internalctx.GetDb(ctx).QueryRow(ctx, `
			INSERT INTO SampleRetirementOwnershipEvidence (
				organization_id, subject_type, subject_id, ownership_marker,
				ownership_checksum, source_reference, source_checksum,
				recorded_by_useraccount_id
			) VALUES (
				@organizationID, @subjectType, @subjectID, @ownershipMarker,
				@ownershipChecksum, @sourceReference, @sourceChecksum, @actorID
			)
			ON CONFLICT (organization_id, subject_type, subject_id) DO NOTHING
			RETURNING
				id, created_at, organization_id, subject_type, subject_id,
				ownership_marker, ownership_checksum, source_reference,
				source_checksum, recorded_by_useraccount_id`,
			pgx.NamedArgs{
				"organizationID":    input.OrganizationID,
				"subjectType":       input.SubjectType,
				"subjectID":         input.SubjectID,
				"ownershipMarker":   input.OwnershipMarker,
				"ownershipChecksum": input.OwnershipChecksum,
				"sourceReference":   input.SourceReference,
				"sourceChecksum":    input.SourceChecksum,
				"actorID":           input.RecordedByUserAccountID,
			},
		),
	)
	if err == nil {
		return value, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, fmt.Errorf(
			"register sample retirement ownership evidence: %w",
			err,
		)
	}
	value, err = scanSampleRetirementOwnershipEvidence(
		internalctx.GetDb(ctx).QueryRow(ctx, `
			SELECT
				id, created_at, organization_id, subject_type, subject_id,
				ownership_marker, ownership_checksum, source_reference,
				source_checksum, recorded_by_useraccount_id
			FROM SampleRetirementOwnershipEvidence
			WHERE organization_id=@organizationID
			  AND subject_type=@subjectType
			  AND subject_id=@subjectID
			FOR UPDATE`,
			pgx.NamedArgs{
				"organizationID": input.OrganizationID,
				"subjectType":    input.SubjectType,
				"subjectID":      input.SubjectID,
			},
		),
	)
	if err != nil {
		return nil, false, fmt.Errorf(
			"load replayed sample retirement ownership evidence: %w",
			err,
		)
	}
	return value, false, nil
}

func scanSampleRetirementOwnershipEvidence(
	row pgx.Row,
) (*types.SampleRetirementOwnershipEvidence, error) {
	var value types.SampleRetirementOwnershipEvidence
	err := row.Scan(
		&value.ID,
		&value.CreatedAt,
		&value.OrganizationID,
		&value.SubjectType,
		&value.SubjectID,
		&value.OwnershipMarker,
		&value.OwnershipChecksum,
		&value.SourceReference,
		&value.SourceChecksum,
		&value.RecordedByUserAccountID,
	)
	return &value, err
}

func sampleRetirementOwnershipEvidenceMatchesInput(
	value *types.SampleRetirementOwnershipEvidence,
	input types.SampleRetirementOwnershipEvidenceRegistrationInput,
) bool {
	return value.OrganizationID == input.OrganizationID &&
		value.SubjectType == input.SubjectType &&
		value.SubjectID == input.SubjectID &&
		value.OwnershipMarker == input.OwnershipMarker &&
		value.OwnershipChecksum == input.OwnershipChecksum &&
		value.SourceReference == input.SourceReference &&
		value.SourceChecksum == input.SourceChecksum &&
		value.RecordedByUserAccountID == input.RecordedByUserAccountID
}

func insertOrLoadSampleRetirementRecoveryEvidence(
	ctx context.Context,
	input types.SampleRetirementRecoveryEvidenceRegistrationInput,
) (*types.SampleRetirementRecoveryEvidence, bool, error) {
	value, err := scanSampleRetirementRecoveryEvidence(
		internalctx.GetDb(ctx).QueryRow(ctx, `
			INSERT INTO SampleRetirementRecoveryEvidence (
				organization_id, evidence_kind, reference, checksum, source_kind,
				source_id, source_checksum, verified_at,
				verified_by_useraccount_id
			) VALUES (
				@organizationID, @evidenceKind, @reference, @checksum, @sourceKind,
				@sourceID, @sourceChecksum, @verifiedAt, @actorID
			)
			ON CONFLICT (organization_id, evidence_kind, reference, checksum)
			DO NOTHING
			RETURNING
				id, created_at, organization_id, evidence_kind, reference,
				checksum, source_kind, source_id, source_checksum, verified_at,
				verified_by_useraccount_id`,
			pgx.NamedArgs{
				"organizationID": input.OrganizationID,
				"evidenceKind":   input.EvidenceKind,
				"reference":      input.Reference,
				"checksum":       input.Checksum,
				"sourceKind":     input.SourceKind,
				"sourceID":       input.SourceID,
				"sourceChecksum": input.SourceChecksum,
				"verifiedAt":     input.VerifiedAt,
				"actorID":        input.VerifiedByUserAccountID,
			},
		),
	)
	if err == nil {
		return value, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, fmt.Errorf(
			"register sample retirement recovery evidence: %w",
			err,
		)
	}
	value, err = scanSampleRetirementRecoveryEvidence(
		internalctx.GetDb(ctx).QueryRow(ctx, `
			SELECT
				id, created_at, organization_id, evidence_kind, reference,
				checksum, source_kind, source_id, source_checksum, verified_at,
				verified_by_useraccount_id
			FROM SampleRetirementRecoveryEvidence
			WHERE organization_id=@organizationID
			  AND evidence_kind=@evidenceKind
			  AND reference=@reference
			  AND checksum=@checksum
			FOR UPDATE`,
			pgx.NamedArgs{
				"organizationID": input.OrganizationID,
				"evidenceKind":   input.EvidenceKind,
				"reference":      input.Reference,
				"checksum":       input.Checksum,
			},
		),
	)
	if err != nil {
		return nil, false, fmt.Errorf(
			"load replayed sample retirement recovery evidence: %w",
			err,
		)
	}
	return value, false, nil
}

func scanSampleRetirementRecoveryEvidence(
	row pgx.Row,
) (*types.SampleRetirementRecoveryEvidence, error) {
	var value types.SampleRetirementRecoveryEvidence
	err := row.Scan(
		&value.ID,
		&value.CreatedAt,
		&value.OrganizationID,
		&value.EvidenceKind,
		&value.Reference,
		&value.Checksum,
		&value.SourceKind,
		&value.SourceID,
		&value.SourceChecksum,
		&value.VerifiedAt,
		&value.VerifiedByUserAccountID,
	)
	return &value, err
}

func sampleRetirementRecoveryEvidenceMatchesInput(
	value *types.SampleRetirementRecoveryEvidence,
	input types.SampleRetirementRecoveryEvidenceRegistrationInput,
) bool {
	return value.OrganizationID == input.OrganizationID &&
		value.EvidenceKind == input.EvidenceKind &&
		value.Reference == input.Reference &&
		value.Checksum == input.Checksum &&
		value.SourceKind == input.SourceKind &&
		value.SourceID == input.SourceID &&
		value.SourceChecksum == input.SourceChecksum &&
		value.VerifiedAt.Equal(input.VerifiedAt) &&
		value.VerifiedByUserAccountID == input.VerifiedByUserAccountID
}

func resolveSampleRetirementRecoveryEvidence(
	ctx context.Context,
	organizationID uuid.UUID,
	evidenceKind, reference, checksum string,
) (uuid.UUID, error) {
	var evidenceID uuid.UUID
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT id
		FROM SampleRetirementRecoveryEvidence
		WHERE organization_id=@organizationID
		  AND evidence_kind=@evidenceKind
		  AND reference=@reference
		  AND checksum=@checksum
		  AND verified_at IS NOT NULL`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"evidenceKind":   evidenceKind,
			"reference":      reference,
			"checksum":       checksum,
		},
	).Scan(&evidenceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, apierrors.NewConflict(
			"sample retirement recovery evidence is missing or unverified",
		)
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf(
			"resolve sample retirement recovery evidence: %w",
			err,
		)
	}
	return evidenceID, nil
}

func sampleRetirementSubjectsFromItems(
	items []types.SampleRetirementItem,
) []types.SampleRetirementSubject {
	subjects := make([]types.SampleRetirementSubject, 0, len(items))
	for _, item := range items {
		subjects = append(subjects, types.SampleRetirementSubject{
			SubjectType: item.SubjectType, SubjectID: item.SubjectID,
			OwnershipMarker:   item.OwnershipMarker,
			OwnershipChecksum: item.OwnershipChecksum,
			ExpectedChecksum:  item.ExpectedChecksum,
		})
	}
	return subjects
}

func sampleRetirementRowChecksumExpression(alias string) string {
	return fmt.Sprintf(
		"'sha256:' || encode(sha256(convert_to(to_jsonb(%s)::text, 'UTF8')), 'hex')",
		pgx.Identifier{alias}.Sanitize(),
	)
}

func sampleRetirementReferenceReportChecksum(
	report types.ReferenceReport,
) (string, error) {
	payload, err := json.Marshal(struct {
		Schema string                `json:"schema"`
		Report types.ReferenceReport `json:"report"`
	}{
		Schema: "sample-retirement-reference-report/v1",
		Report: report,
	})
	if err != nil {
		return "", fmt.Errorf("canonicalize sample retirement reference report: %w", err)
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func sampleRetirementCanonicalTombstoneChecksum(
	job *types.SampleRetirementJob,
	item *types.SampleRetirementItem,
	request types.SampleRetirementApplyRequest,
	tombstoneID uuid.UUID,
	retiredAt time.Time,
	retiredBy uuid.UUID,
	auditEventCount int,
) (string, error) {
	payload, err := json.Marshal(struct {
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
		Version: 1, TombstoneID: tombstoneID,
		OrganizationID: job.OrganizationID, JobID: job.ID, ItemID: item.ID,
		SubjectType: item.SubjectType, SubjectID: item.SubjectID,
		OwnershipMarker:   item.OwnershipMarker,
		OwnershipChecksum: item.OwnershipChecksum,
		SubjectChecksum:   item.ExpectedChecksum, AuditEventCount: auditEventCount,
		ActorID: retiredBy, PreviewChecksum: request.PreviewChecksum,
		ApprovalID:       request.ApprovalID,
		ApprovalChecksum: request.ApprovalChecksum,
		RetiredAt:        retiredAt.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return "", fmt.Errorf("canonicalize sample retirement tombstone: %w", err)
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func sampleRetirementTextChecksum(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("sha256:%x", sum)
}

func sampleRetirementUUIDPointersEqual(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func sampleRetirementIsChecksum(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, character := range value[len("sha256:"):] {
		if (character < '0' || character > '9') &&
			(character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func sampleRetirementSQLLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
