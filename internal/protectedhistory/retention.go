package protectedhistory

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	RetentionSchemaV1   = "distr.protected-history-retention/v1"
	ArtifactMediaTypeV1 = "application/vnd.distr.protected-history.v1+json"

	retentionDomain        = "distr.protected-history-retention/material/v1"
	retentionAuditDomain   = "distr.protected-history-retention/audit-binding/v1"
	maximumArtifactBytes   = 256 * 1024 * 1024
	maximumObjectReference = 2048
)

var objectDigestPathPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type RetentionInput struct {
	ID                    uuid.UUID
	Artifact              Artifact
	ObjectReference       string
	MediaType             string
	ByteLength            int64
	ContentChecksum       string
	CapturedAt            time.Time
	IssuerUserAccountID   uuid.UUID
	ReviewerUserAccountID uuid.UUID
}

type CreateRetentionRequest struct {
	OrganizationID        uuid.UUID
	Scope                 Scope
	IssuerUserAccountID   uuid.UUID
	ReviewerUserAccountID uuid.UUID
	IdempotencyKey        string
}

type RetainedArtifact struct {
	ID                    uuid.UUID `db:"id" json:"id"`
	OrganizationID        uuid.UUID `db:"organization_id" json:"organizationId"`
	Schema                string    `db:"schema" json:"schema"`
	SourceSchemaVersion   uint64    `db:"source_schema_version" json:"sourceSchemaVersion"`
	Scope                 Scope     `json:"scope"`
	ArtifactID            string    `db:"artifact_id" json:"artifactId"`
	RecordsRoot           string    `db:"records_root" json:"recordsRoot"`
	RecordCount           uint64    `db:"record_count" json:"recordCount"`
	ObjectReference       string    `db:"object_reference" json:"objectReference"`
	MediaType             string    `db:"media_type" json:"mediaType"`
	ByteLength            int64     `db:"byte_length" json:"byteLength"`
	ContentChecksum       string    `db:"content_checksum" json:"contentChecksum"`
	CapturedAt            time.Time `db:"captured_at" json:"capturedAt"`
	IssuerUserAccountID   uuid.UUID `db:"issuer_user_account_id" json:"issuerUserAccountId"`
	ReviewerUserAccountID uuid.UUID `db:"reviewer_user_account_id" json:"reviewerUserAccountId"`
	RetentionChecksum     string    `db:"retention_checksum" json:"retentionChecksum"`
	AuditEventID          uuid.UUID `db:"audit_event_id" json:"auditEventId,omitempty"`
	AuditEventSequence    int64     `db:"audit_event_sequence" json:"auditEventSequence,omitempty"`
	AuditBindingChecksum  string    `db:"audit_binding_checksum" json:"auditBindingChecksum,omitempty"`
	IdempotencyKey        string    `db:"idempotency_key" json:"idempotencyKey"`
	RequestChecksum       string    `db:"request_checksum" json:"requestChecksum"`
	CreatedAt             time.Time `db:"created_at" json:"createdAt,omitempty"`
}

func BuildRetention(input RetentionInput) (*RetainedArtifact, error) {
	if input.ID == uuid.Nil {
		return nil, errors.New("retained artifact id is required")
	}
	if err := Validate(input.Artifact); err != nil {
		return nil, fmt.Errorf("validate protected history artifact: %w", err)
	}
	payload, err := Marshal(input.Artifact)
	if err != nil {
		return nil, err
	}
	if input.ByteLength != int64(len(payload)) {
		return nil, errors.New("byte length does not match the canonical protected-history artifact")
	}
	if input.ContentChecksum != ContentChecksum(payload) {
		return nil, errors.New("content checksum does not match the canonical protected-history artifact")
	}
	organizationID, err := uuid.Parse(input.Artifact.Scope.OrganizationID)
	if err != nil || organizationID == uuid.Nil {
		return nil, errors.New("artifact organization id is invalid")
	}
	retained := &RetainedArtifact{
		ID: input.ID, OrganizationID: organizationID, Schema: RetentionSchemaV1,
		SourceSchemaVersion: input.Artifact.SourceSchemaVersion, Scope: input.Artifact.Scope,
		ArtifactID: input.Artifact.ArtifactID, RecordsRoot: input.Artifact.RecordsRoot,
		RecordCount: input.Artifact.RecordCount, ObjectReference: input.ObjectReference,
		MediaType: input.MediaType, ByteLength: input.ByteLength,
		ContentChecksum: input.ContentChecksum, CapturedAt: input.CapturedAt,
		IssuerUserAccountID:   input.IssuerUserAccountID,
		ReviewerUserAccountID: input.ReviewerUserAccountID,
	}
	retained.RetentionChecksum = computeRetentionChecksum(*retained)
	if err := ValidateRetention(*retained); err != nil {
		return nil, err
	}
	return retained, nil
}

func BindRetentionAudit(retained *RetainedArtifact, eventID uuid.UUID, sequence int64) error {
	if retained == nil {
		return errors.New("retained artifact is required")
	}
	if eventID == uuid.Nil || sequence < 1 {
		return errors.New("append-only audit event identity is required")
	}
	if retained.AuditEventID != uuid.Nil || retained.AuditEventSequence != 0 || retained.AuditBindingChecksum != "" {
		return errors.New("retained artifact audit identity is already bound")
	}
	retained.AuditEventID = eventID
	retained.AuditEventSequence = sequence
	retained.AuditBindingChecksum = computeAuditBindingChecksum(*retained)
	return ValidateRetention(*retained)
}

func ValidateRetention(retained RetainedArtifact) error {
	switch {
	case retained.ID == uuid.Nil:
		return errors.New("retained artifact id is required")
	case retained.OrganizationID == uuid.Nil:
		return errors.New("retained artifact organization is required")
	case retained.Schema != RetentionSchemaV1:
		return fmt.Errorf("unsupported retained artifact schema %q", retained.Schema)
	case retained.SourceSchemaVersion < 138 || retained.SourceSchemaVersion > 170:
		return fmt.Errorf("source schema version %d is unsupported", retained.SourceSchemaVersion)
	}
	canonicalScope, err := CanonicalScope(retained.Scope)
	if err != nil {
		return fmt.Errorf("validate retained artifact scope: %w", err)
	}
	if !scopeEqual(canonicalScope, retained.Scope) || retained.Scope.OrganizationID != retained.OrganizationID.String() {
		return errors.New("retained artifact scope is not canonical or organization-bound")
	}
	switch {
	case !checksumPattern.MatchString(retained.ArtifactID):
		return errors.New("artifact id must use lowercase sha256 format")
	case !checksumPattern.MatchString(retained.RecordsRoot):
		return errors.New("records root must use lowercase sha256 format")
	case !validImmutableObjectReference(retained.ObjectReference, retained.ContentChecksum):
		return errors.New("object reference must be a checksum-bound immutable S3 object")
	case retained.MediaType != ArtifactMediaTypeV1:
		return fmt.Errorf("media type must be %s", ArtifactMediaTypeV1)
	case retained.ByteLength < 1 || retained.ByteLength > maximumArtifactBytes:
		return errors.New("byte length is outside the protected-history artifact limit")
	case !checksumPattern.MatchString(retained.ContentChecksum):
		return errors.New("content checksum must use lowercase sha256 format")
	case retained.CapturedAt.IsZero() || retained.CapturedAt.Location() != time.UTC ||
		retained.CapturedAt.Nanosecond()%1000 != 0:
		return errors.New("capture time must be UTC with microsecond precision")
	case retained.IssuerUserAccountID == uuid.Nil:
		return errors.New("issuer user account is required")
	case retained.ReviewerUserAccountID == uuid.Nil:
		return errors.New("reviewer user account is required")
	case retained.IssuerUserAccountID == retained.ReviewerUserAccountID:
		return errors.New("issuer and reviewer user accounts must be distinct")
	case !checksumPattern.MatchString(retained.RetentionChecksum):
		return errors.New("retention checksum must use lowercase sha256 format")
	case retained.RetentionChecksum != computeRetentionChecksum(retained):
		return errors.New("retention checksum mismatch")
	}

	auditFields := 0
	if retained.AuditEventID != uuid.Nil {
		auditFields++
	}
	if retained.AuditEventSequence != 0 {
		auditFields++
	}
	if retained.AuditBindingChecksum != "" {
		auditFields++
	}
	if auditFields == 0 {
		return nil
	}
	if auditFields != 3 || retained.AuditEventSequence < 1 ||
		!checksumPattern.MatchString(retained.AuditBindingChecksum) {
		return errors.New("retained artifact audit binding is incomplete")
	}
	if retained.AuditBindingChecksum != computeAuditBindingChecksum(retained) {
		return errors.New("audit binding checksum mismatch")
	}
	return nil
}

func ContentChecksum(payload []byte) string {
	return checksum(payload)
}

func RetentionRequestChecksum(request CreateRetentionRequest) (string, error) {
	if request.OrganizationID == uuid.Nil || request.IssuerUserAccountID == uuid.Nil ||
		request.ReviewerUserAccountID == uuid.Nil || strings.TrimSpace(request.IdempotencyKey) == "" {
		return "", errors.New("protected-history retention request identity is incomplete")
	}
	canonicalScope, err := CanonicalScope(request.Scope)
	if err != nil {
		return "", err
	}
	var buffer bytes.Buffer
	writeField(&buffer, "distr.protected-history-retention/request/v1")
	writeField(&buffer, request.OrganizationID.String())
	writeField(&buffer, canonicalScope.OrganizationID)
	writeStringSet(&buffer, canonicalScope.CustomerOrganizationIDs)
	writeStringSet(&buffer, canonicalScope.DeploymentTargetIDs)
	writeField(&buffer, request.IssuerUserAccountID.String())
	writeField(&buffer, request.ReviewerUserAccountID.String())
	writeField(&buffer, strings.TrimSpace(request.IdempotencyKey))
	return checksum(buffer.Bytes()), nil
}

func validImmutableObjectReference(reference, contentChecksum string) bool {
	if reference == "" || len(reference) > maximumObjectReference || strings.TrimSpace(reference) != reference ||
		strings.Contains(reference, "\\") || !checksumPattern.MatchString(contentChecksum) {
		return false
	}
	parsed, err := url.Parse(reference)
	if err != nil || parsed.Scheme != "s3" || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" ||
		path.Clean(parsed.Path) != parsed.Path || strings.Contains(parsed.Path, "/../") {
		return false
	}
	segments := strings.Split(strings.TrimPrefix(parsed.Path, "/"), "/")
	return len(segments) >= 4 && segments[0] == "_immutable" && segments[1] == "sha256" &&
		objectDigestPathPattern.MatchString(segments[2]) && segments[len(segments)-1] != "" &&
		"sha256:"+segments[2] == contentChecksum
}

func computeRetentionChecksum(retained RetainedArtifact) string {
	var buffer bytes.Buffer
	writeField(&buffer, retentionDomain)
	writeField(&buffer, retained.Schema)
	writeField(&buffer, retained.ID.String())
	writeField(&buffer, retained.OrganizationID.String())
	writeField(&buffer, strconv.FormatUint(retained.SourceSchemaVersion, 10))
	writeField(&buffer, retained.Scope.OrganizationID)
	writeStringSet(&buffer, retained.Scope.CustomerOrganizationIDs)
	writeStringSet(&buffer, retained.Scope.DeploymentTargetIDs)
	writeField(&buffer, retained.ArtifactID)
	writeField(&buffer, retained.RecordsRoot)
	writeField(&buffer, strconv.FormatUint(retained.RecordCount, 10))
	writeField(&buffer, retained.ObjectReference)
	writeField(&buffer, retained.MediaType)
	writeField(&buffer, strconv.FormatInt(retained.ByteLength, 10))
	writeField(&buffer, retained.ContentChecksum)
	writeField(&buffer, retained.CapturedAt.Format(time.RFC3339Nano))
	writeField(&buffer, retained.IssuerUserAccountID.String())
	writeField(&buffer, retained.ReviewerUserAccountID.String())
	return checksum(buffer.Bytes())
}

func computeAuditBindingChecksum(retained RetainedArtifact) string {
	var buffer bytes.Buffer
	writeField(&buffer, retentionAuditDomain)
	writeField(&buffer, retained.ID.String())
	writeField(&buffer, retained.RetentionChecksum)
	writeField(&buffer, retained.AuditEventID.String())
	writeField(&buffer, strconv.FormatInt(retained.AuditEventSequence, 10))
	return checksum(buffer.Bytes())
}
