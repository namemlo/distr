package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/pilotexception"
	"github.com/distr-sh/distr/internal/protectedhistory"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type protectedHistoryArtifactRow struct {
	ID                           uuid.UUID   `db:"id"`
	CreatedAt                    time.Time   `db:"created_at"`
	OrganizationID               uuid.UUID   `db:"organization_id"`
	Schema                       string      `db:"schema"`
	SourceSchemaVersion          uint64      `db:"source_schema_version"`
	CustomerOrganizationIDs      []uuid.UUID `db:"customer_organization_ids"`
	DeploymentTargetIDs          []uuid.UUID `db:"deployment_target_ids"`
	ArtifactID                   string      `db:"artifact_id"`
	RecordsRoot                  string      `db:"records_root"`
	RecordCount                  uint64      `db:"record_count"`
	ObjectReference              string      `db:"object_reference"`
	MediaType                    string      `db:"media_type"`
	ByteLength                   int64       `db:"byte_length"`
	ContentChecksum              string      `db:"content_checksum"`
	CapturedAt                   time.Time   `db:"captured_at"`
	IssuerUserAccountID          uuid.UUID   `db:"issuer_useraccount_id"`
	ReviewerUserAccountID        uuid.UUID   `db:"reviewer_useraccount_id"`
	GovernanceExceptionKey       *string     `db:"governance_exception_key"`
	GovernanceExceptionReference *string     `db:"governance_exception_reference"`
	RetentionChecksum            string      `db:"retention_checksum"`
	AuditEventID                 uuid.UUID   `db:"audit_event_id"`
	AuditEventSequence           int64       `db:"audit_event_sequence"`
	AuditBindingChecksum         string      `db:"audit_binding_checksum"`
	IdempotencyKey               string      `db:"idempotency_key"`
	RequestChecksum              string      `db:"request_checksum"`
}

const protectedHistoryArtifactColumns = `
	id, created_at, organization_id, schema, source_schema_version,
	customer_organization_ids, deployment_target_ids,
	artifact_id, records_root, record_count,
	object_reference, media_type, byte_length, content_checksum, captured_at,
	issuer_useraccount_id, reviewer_useraccount_id,
	governance_exception_key, governance_exception_reference, retention_checksum,
	audit_event_id, audit_event_sequence, audit_binding_checksum,
	idempotency_key, request_checksum`

func RetainProtectedHistoryArtifact(
	ctx context.Context,
	request protectedhistory.CreateRetentionRequest,
	store protectedhistory.ObjectStore,
	capturedAt time.Time,
) (*protectedhistory.RetainedArtifact, error) {
	if store == nil {
		store = protectedhistory.NewUnavailableObjectStore()
	}
	canonicalScope, err := canonicalizeProtectedHistoryRetentionRequest(request)
	if err != nil {
		return nil, err
	}
	request.Scope = canonicalScope
	if existing, err := getProtectedHistoryArtifactByIdempotencyKey(
		ctx, request.OrganizationID, request.IdempotencyKey,
	); err == nil {
		request.GovernanceException, err = protectedHistoryReplayGovernanceException(*existing)
		if err != nil {
			return nil, err
		}
		requestChecksum, err := protectedhistory.RetentionRequestChecksum(request)
		if err != nil {
			return nil, apierrors.NewBadRequest(err.Error())
		}
		return verifyProtectedHistoryReplay(ctx, store, existing, requestChecksum)
	} else if !errors.Is(err, apierrors.ErrNotFound) {
		return nil, err
	}
	canonicalScope, requestChecksum, err := validateProtectedHistoryRetentionRequest(request)
	if err != nil {
		return nil, err
	}
	if err := validateProtectedHistoryParticipants(ctx, request); err != nil {
		return nil, err
	}
	artifact, err := ExportProtectedHistory(ctx, canonicalScope)
	if err != nil {
		return nil, err
	}
	payload, err := protectedhistory.Marshal(*artifact)
	if err != nil {
		return nil, err
	}
	identity, err := store.WriteOnce(ctx, payload)
	if err != nil {
		return nil, mapProtectedHistoryObjectError(err)
	}
	observed, err := store.Readback(ctx, identity)
	if err != nil {
		return nil, mapProtectedHistoryObjectError(err)
	}
	if err := protectedhistory.VerifyObjectIdentity(identity, observed); err != nil {
		return nil, apierrors.NewConflict(err.Error())
	}
	retained, err := protectedhistory.BuildRetention(protectedhistory.RetentionInput{
		ID: uuid.New(), Artifact: *artifact, ObjectReference: identity.Reference,
		MediaType: identity.MediaType, ByteLength: identity.ByteLength,
		ContentChecksum:       identity.Checksum,
		CapturedAt:            capturedAt.UTC().Truncate(time.Microsecond),
		IssuerUserAccountID:   request.IssuerUserAccountID,
		ReviewerUserAccountID: request.ReviewerUserAccountID,
		GovernanceException:   request.GovernanceException,
	})
	if err != nil {
		return nil, apierrors.NewBadRequest(err.Error())
	}
	retained.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	retained.RequestChecksum = requestChecksum

	var result *protectedhistory.RetainedArtifact
	err = RunTx(ctx, func(txCtx context.Context) error {
		if _, err := internalctx.GetDb(txCtx).Exec(txCtx,
			`SELECT pg_advisory_xact_lock(hashtextextended(@key, 170))`,
			pgx.NamedArgs{"key": request.OrganizationID.String() + ":" + retained.IdempotencyKey},
		); err != nil {
			return fmt.Errorf("lock protected-history idempotency key: %w", err)
		}
		existing, err := getProtectedHistoryArtifactByIdempotencyKey(
			txCtx, request.OrganizationID, retained.IdempotencyKey,
		)
		if err == nil {
			if existing.RequestChecksum != requestChecksum {
				return apierrors.NewConflict(
					"protected-history idempotency key is already bound to different material",
				)
			}
			result = existing
			return nil
		}
		if !errors.Is(err, apierrors.ErrNotFound) {
			return err
		}
		auditPayload, err := protectedHistoryRetentionAuditPayload(*retained)
		if err != nil {
			return err
		}
		event, err := AppendControlPlaneAuditEventInCurrentBoundary(
			txCtx,
			types.ControlPlaneAuditEventInput{
				OrganizationID:             request.OrganizationID,
				EventType:                  "protected_history.retained",
				ActorID:                    &request.IssuerUserAccountID,
				Outcome:                    "SUCCEEDED",
				ProtectedHistoryArtifactID: &retained.ID,
				ArtifactDigest:             retained.ContentChecksum,
				Payload:                    auditPayload,
			},
		)
		if err != nil {
			return err
		}
		if err := protectedhistory.BindRetentionAudit(retained, event.ID, event.Sequence); err != nil {
			return err
		}
		inserted, err := insertProtectedHistoryArtifact(txCtx, *retained)
		if err != nil {
			return err
		}
		result = inserted
		return nil
	})
	if err != nil {
		return nil, err
	}
	if result.ID != retained.ID {
		return verifyProtectedHistoryReplay(ctx, store, result, requestChecksum)
	}
	return result, nil
}

func GetProtectedHistoryArtifact(
	ctx context.Context,
	organizationID,
	id uuid.UUID,
) (*protectedhistory.RetainedArtifact, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+protectedHistoryArtifactColumns+`
		FROM ProtectedHistoryArtifact
		WHERE organization_id = @organizationId AND id = @id`,
		pgx.NamedArgs{"organizationId": organizationID, "id": id},
	)
	if err != nil {
		return nil, fmt.Errorf("query protected-history artifact: %w", err)
	}
	row, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[protectedHistoryArtifactRow])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan protected-history artifact: %w", err)
	}
	return protectedHistoryArtifactFromRow(row)
}

func VerifyProtectedHistoryArtifact(
	ctx context.Context,
	organizationID,
	id uuid.UUID,
	store protectedhistory.ObjectStore,
) (*protectedhistory.ObjectIdentity, error) {
	retained, err := GetProtectedHistoryArtifact(ctx, organizationID, id)
	if err != nil {
		return nil, err
	}
	expected := protectedHistoryObjectIdentity(*retained)
	observed, err := store.Readback(ctx, expected)
	if err != nil {
		return nil, mapProtectedHistoryObjectError(err)
	}
	if err := protectedhistory.VerifyObjectIdentity(expected, observed); err != nil {
		return nil, apierrors.NewConflict(err.Error())
	}
	return &observed, nil
}

func validateProtectedHistoryRetentionRequest(
	request protectedhistory.CreateRetentionRequest,
) (protectedhistory.Scope, string, error) {
	canonicalScope, err := canonicalizeProtectedHistoryRetentionRequest(request)
	if err != nil {
		return protectedhistory.Scope{}, "", err
	}
	if request.IssuerUserAccountID == request.ReviewerUserAccountID &&
		(request.GovernanceException == nil || !request.GovernanceException.Valid()) {
		return protectedhistory.Scope{}, "", apierrors.NewBadRequest(
			"protected-history issuer and distinct reviewer are required",
		)
	}
	if request.IssuerUserAccountID != request.ReviewerUserAccountID && request.GovernanceException != nil {
		return protectedhistory.Scope{}, "", apierrors.NewBadRequest(
			"protected-history governance exception is only valid for a single reviewer",
		)
	}
	request.Scope = canonicalScope
	requestChecksum, err := protectedhistory.RetentionRequestChecksum(request)
	if err != nil {
		return protectedhistory.Scope{}, "", apierrors.NewBadRequest(err.Error())
	}
	return canonicalScope, requestChecksum, nil
}

func canonicalizeProtectedHistoryRetentionRequest(
	request protectedhistory.CreateRetentionRequest,
) (protectedhistory.Scope, error) {
	if request.OrganizationID == uuid.Nil || request.IssuerUserAccountID == uuid.Nil ||
		request.ReviewerUserAccountID == uuid.Nil || strings.TrimSpace(request.IdempotencyKey) == "" {
		return protectedhistory.Scope{}, apierrors.NewBadRequest(
			"protected-history retention request identity is incomplete",
		)
	}
	canonicalScope, err := protectedhistory.CanonicalScope(request.Scope)
	if err != nil {
		return protectedhistory.Scope{}, apierrors.NewBadRequest(err.Error())
	}
	if canonicalScope.OrganizationID != request.OrganizationID.String() {
		return protectedhistory.Scope{}, apierrors.NewForbidden(
			"protected-history scope is outside the organization",
		)
	}
	return canonicalScope, nil
}

func protectedHistoryReplayGovernanceException(
	existing protectedhistory.RetainedArtifact,
) (*pilotexception.Evidence, error) {
	if existing.GovernanceExceptionKey == "" && existing.GovernanceExceptionReference == "" {
		return nil, nil
	}
	evidence := &pilotexception.Evidence{
		Key:               existing.GovernanceExceptionKey,
		ApprovalReference: existing.GovernanceExceptionReference,
	}
	if !evidence.Valid() {
		return nil, apierrors.NewConflict(
			"stored protected-history governance exception is invalid",
		)
	}
	return evidence, nil
}

func validateProtectedHistoryParticipants(
	ctx context.Context,
	request protectedhistory.CreateRetentionRequest,
) error {
	var count int
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT count(*)
		FROM Organization_UserAccount
		WHERE organization_id = @organizationId
		  AND user_account_id = ANY(@userAccountIds::uuid[])`, pgx.NamedArgs{
		"organizationId": request.OrganizationID,
		"userAccountIds": []uuid.UUID{request.IssuerUserAccountID, request.ReviewerUserAccountID},
	}).Scan(&count)
	if err != nil {
		return fmt.Errorf("validate protected-history participants: %w", err)
	}
	expectedParticipants := 2
	if request.IssuerUserAccountID == request.ReviewerUserAccountID {
		expectedParticipants = 1
	}
	if count != expectedParticipants {
		return apierrors.NewBadRequest("issuer and reviewer must be current organization members")
	}
	return nil
}

func getProtectedHistoryArtifactByIdempotencyKey(
	ctx context.Context,
	organizationID uuid.UUID,
	idempotencyKey string,
) (*protectedhistory.RetainedArtifact, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+protectedHistoryArtifactColumns+`
		FROM ProtectedHistoryArtifact
		WHERE organization_id = @organizationId AND idempotency_key = @idempotencyKey`,
		pgx.NamedArgs{
			"organizationId": organizationID,
			"idempotencyKey": strings.TrimSpace(idempotencyKey),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("query protected-history idempotency key: %w", err)
	}
	row, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[protectedHistoryArtifactRow])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan protected-history idempotency key: %w", err)
	}
	return protectedHistoryArtifactFromRow(row)
}

func insertProtectedHistoryArtifact(
	ctx context.Context,
	retained protectedhistory.RetainedArtifact,
) (*protectedhistory.RetainedArtifact, error) {
	customerIDs, targetIDs, err := protectedHistoryScopeUUIDs(retained.Scope)
	if err != nil {
		return nil, err
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		INSERT INTO ProtectedHistoryArtifact (
			id, organization_id, schema, source_schema_version,
			customer_organization_ids, deployment_target_ids,
			artifact_id, records_root, record_count,
			object_reference, media_type, byte_length, content_checksum, captured_at,
			issuer_useraccount_id, reviewer_useraccount_id,
			governance_exception_key, governance_exception_reference, retention_checksum,
			audit_event_id, audit_event_sequence, audit_binding_checksum,
			idempotency_key, request_checksum
		) VALUES (
			@id, @organizationId, @schema, @sourceSchemaVersion,
			@customerOrganizationIds, @deploymentTargetIds,
			@artifactId, @recordsRoot, @recordCount,
			@objectReference, @mediaType, @byteLength, @contentChecksum, @capturedAt,
			@issuerUserAccountId, @reviewerUserAccountId,
			@governanceExceptionKey, @governanceExceptionReference, @retentionChecksum,
			@auditEventId, @auditEventSequence, @auditBindingChecksum,
			@idempotencyKey, @requestChecksum
		)
		RETURNING `+protectedHistoryArtifactColumns, pgx.NamedArgs{
		"id": retained.ID, "organizationId": retained.OrganizationID,
		"schema": retained.Schema, "sourceSchemaVersion": retained.SourceSchemaVersion,
		"customerOrganizationIds": customerIDs, "deploymentTargetIds": targetIDs,
		"artifactId": retained.ArtifactID, "recordsRoot": retained.RecordsRoot,
		"recordCount": retained.RecordCount, "objectReference": retained.ObjectReference,
		"mediaType": retained.MediaType, "byteLength": retained.ByteLength,
		"contentChecksum": retained.ContentChecksum, "capturedAt": retained.CapturedAt,
		"issuerUserAccountId":          retained.IssuerUserAccountID,
		"reviewerUserAccountId":        retained.ReviewerUserAccountID,
		"governanceExceptionKey":       protectedHistoryNullableString(retained.GovernanceExceptionKey),
		"governanceExceptionReference": protectedHistoryNullableString(retained.GovernanceExceptionReference),
		"retentionChecksum":            retained.RetentionChecksum, "auditEventId": retained.AuditEventID,
		"auditEventSequence":   retained.AuditEventSequence,
		"auditBindingChecksum": retained.AuditBindingChecksum,
		"idempotencyKey":       retained.IdempotencyKey, "requestChecksum": retained.RequestChecksum,
	})
	if err != nil {
		return nil, mapProtectedHistoryWriteError("insert", err)
	}
	row, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[protectedHistoryArtifactRow])
	if err != nil {
		return nil, mapProtectedHistoryWriteError("scan", err)
	}
	return protectedHistoryArtifactFromRow(row)
}

func protectedHistoryArtifactFromRow(
	row protectedHistoryArtifactRow,
) (*protectedhistory.RetainedArtifact, error) {
	retained := &protectedhistory.RetainedArtifact{
		ID: row.ID, CreatedAt: row.CreatedAt, OrganizationID: row.OrganizationID,
		Schema: row.Schema, SourceSchemaVersion: row.SourceSchemaVersion,
		Scope:      protectedhistory.Scope{OrganizationID: row.OrganizationID.String()},
		ArtifactID: row.ArtifactID, RecordsRoot: row.RecordsRoot, RecordCount: row.RecordCount,
		ObjectReference: row.ObjectReference, MediaType: row.MediaType, ByteLength: row.ByteLength,
		ContentChecksum: row.ContentChecksum, CapturedAt: row.CapturedAt,
		IssuerUserAccountID:          row.IssuerUserAccountID,
		ReviewerUserAccountID:        row.ReviewerUserAccountID,
		GovernanceExceptionKey:       protectedHistoryStringValue(row.GovernanceExceptionKey),
		GovernanceExceptionReference: protectedHistoryStringValue(row.GovernanceExceptionReference),
		RetentionChecksum:            row.RetentionChecksum, AuditEventID: row.AuditEventID,
		AuditEventSequence:   row.AuditEventSequence,
		AuditBindingChecksum: row.AuditBindingChecksum,
		IdempotencyKey:       row.IdempotencyKey, RequestChecksum: row.RequestChecksum,
	}
	for _, id := range row.CustomerOrganizationIDs {
		retained.Scope.CustomerOrganizationIDs = append(retained.Scope.CustomerOrganizationIDs, id.String())
	}
	for _, id := range row.DeploymentTargetIDs {
		retained.Scope.DeploymentTargetIDs = append(retained.Scope.DeploymentTargetIDs, id.String())
	}
	if err := protectedhistory.ValidateRetention(*retained); err != nil {
		return nil, fmt.Errorf("validate stored protected-history artifact: %w", err)
	}
	return retained, nil
}

func protectedHistoryScopeUUIDs(scope protectedhistory.Scope) ([]uuid.UUID, []uuid.UUID, error) {
	parse := func(values []string) ([]uuid.UUID, error) {
		result := make([]uuid.UUID, len(values))
		for index, value := range values {
			id, err := uuid.Parse(value)
			if err != nil {
				return nil, err
			}
			result[index] = id
		}
		return result, nil
	}
	customers, err := parse(scope.CustomerOrganizationIDs)
	if err != nil {
		return nil, nil, err
	}
	targets, err := parse(scope.DeploymentTargetIDs)
	return customers, targets, err
}

func protectedHistoryRetentionAuditPayload(
	retained protectedhistory.RetainedArtifact,
) (json.RawMessage, error) {
	payload, err := json.Marshal(struct {
		RetentionChecksum            string `json:"retentionChecksum"`
		RequestChecksum              string `json:"requestChecksum"`
		ArtifactID                   string `json:"artifactId"`
		RecordsRoot                  string `json:"recordsRoot"`
		ObjectReference              string `json:"objectReference"`
		MediaType                    string `json:"mediaType"`
		ByteLength                   int64  `json:"byteLength"`
		ContentChecksum              string `json:"contentChecksum"`
		CapturedAt                   string `json:"capturedAt"`
		IssuerUserAccountID          string `json:"issuerUserAccountId"`
		ReviewerUserAccountID        string `json:"reviewerUserAccountId"`
		GovernanceExceptionKey       string `json:"governanceExceptionKey,omitempty"`
		GovernanceExceptionReference string `json:"governanceExceptionReference,omitempty"`
	}{
		RetentionChecksum: retained.RetentionChecksum, RequestChecksum: retained.RequestChecksum,
		ArtifactID: retained.ArtifactID, RecordsRoot: retained.RecordsRoot,
		ObjectReference: retained.ObjectReference, MediaType: retained.MediaType,
		ByteLength: retained.ByteLength, ContentChecksum: retained.ContentChecksum,
		CapturedAt:                   retained.CapturedAt.Format(time.RFC3339Nano),
		IssuerUserAccountID:          retained.IssuerUserAccountID.String(),
		ReviewerUserAccountID:        retained.ReviewerUserAccountID.String(),
		GovernanceExceptionKey:       retained.GovernanceExceptionKey,
		GovernanceExceptionReference: retained.GovernanceExceptionReference,
	})
	if err != nil {
		return nil, fmt.Errorf("encode protected-history audit payload: %w", err)
	}
	return payload, nil
}

func protectedHistoryNullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func protectedHistoryStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func protectedHistoryObjectIdentity(
	retained protectedhistory.RetainedArtifact,
) protectedhistory.ObjectIdentity {
	return protectedhistory.ObjectIdentity{
		Reference: retained.ObjectReference, MediaType: retained.MediaType,
		ByteLength: retained.ByteLength, Checksum: retained.ContentChecksum,
	}
}

func verifyProtectedHistoryReplay(
	ctx context.Context,
	store protectedhistory.ObjectStore,
	existing *protectedhistory.RetainedArtifact,
	requestChecksum string,
) (*protectedhistory.RetainedArtifact, error) {
	if existing.RequestChecksum != requestChecksum {
		return nil, apierrors.NewConflict(
			"protected-history idempotency key is already bound to different material",
		)
	}
	expected := protectedHistoryObjectIdentity(*existing)
	observed, err := store.Readback(ctx, expected)
	if err != nil {
		return nil, mapProtectedHistoryObjectError(err)
	}
	if err := protectedhistory.VerifyObjectIdentity(expected, observed); err != nil {
		return nil, apierrors.NewConflict(err.Error())
	}
	return existing, nil
}

func mapProtectedHistoryObjectError(err error) error {
	if errors.Is(err, protectedhistory.ErrObjectConflict) {
		return apierrors.NewConflict(err.Error())
	}
	if errors.Is(err, protectedhistory.ErrObjectVerificationUnavailable) {
		return apierrors.NewConflict(err.Error())
	}
	return fmt.Errorf("protected-history object operation failed: %w", err)
}

func mapProtectedHistoryWriteError(action string, err error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case pgerrcode.UniqueViolation:
			return apierrors.NewConflict("protected-history artifact already exists")
		case pgerrcode.ForeignKeyViolation:
			return apierrors.NewBadRequest("protected-history scope, issuer, reviewer, or audit binding is invalid")
		case pgerrcode.CheckViolation:
			return apierrors.NewBadRequest("protected-history artifact violates the immutable retention contract")
		}
	}
	return fmt.Errorf("%s protected-history artifact: %w", action, err)
}
