package api

import (
	"regexp"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/protectedhistory"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

var protectedHistoryIdempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type CreateProtectedHistoryArtifactRequest struct {
	CustomerOrganizationIDs []uuid.UUID `json:"customerOrganizationIds"`
	DeploymentTargetIDs     []uuid.UUID `json:"deploymentTargetIds"`
	ReviewerUserAccountID   uuid.UUID   `json:"reviewerUserAccountId"`
	IdempotencyKey          string      `json:"idempotencyKey"`
}

func (request *CreateProtectedHistoryArtifactRequest) Validate() error {
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	switch {
	case len(request.CustomerOrganizationIDs) == 0 && len(request.DeploymentTargetIDs) == 0:
		return validation.NewValidationFailedError(
			"at least one customerOrganizationId or deploymentTargetId is required",
		)
	case request.ReviewerUserAccountID == uuid.Nil:
		return validation.NewValidationFailedError("reviewerUserAccountId is required")
	case !protectedHistoryIdempotencyKeyPattern.MatchString(request.IdempotencyKey):
		return validation.NewValidationFailedError(
			"idempotencyKey must contain 1-128 URL-safe characters",
		)
	default:
		return nil
	}
}

func (request CreateProtectedHistoryArtifactRequest) Scope(organizationID uuid.UUID) protectedhistory.Scope {
	customerIDs := make([]string, len(request.CustomerOrganizationIDs))
	for index, id := range request.CustomerOrganizationIDs {
		customerIDs[index] = id.String()
	}
	targetIDs := make([]string, len(request.DeploymentTargetIDs))
	for index, id := range request.DeploymentTargetIDs {
		targetIDs[index] = id.String()
	}
	return protectedhistory.Scope{
		OrganizationID:          organizationID.String(),
		CustomerOrganizationIDs: customerIDs,
		DeploymentTargetIDs:     targetIDs,
	}
}

type ProtectedHistoryArtifact struct {
	ID                           uuid.UUID              `json:"id"`
	Schema                       string                 `json:"schema"`
	SourceSchemaVersion          uint64                 `json:"sourceSchemaVersion"`
	Scope                        protectedhistory.Scope `json:"scope"`
	ArtifactID                   string                 `json:"artifactId"`
	RecordsRoot                  string                 `json:"recordsRoot"`
	RecordCount                  uint64                 `json:"recordCount"`
	ObjectReference              string                 `json:"objectReference"`
	MediaType                    string                 `json:"mediaType"`
	ByteLength                   int64                  `json:"byteLength"`
	ContentChecksum              string                 `json:"contentChecksum"`
	CapturedAt                   time.Time              `json:"capturedAt"`
	IssuerUserAccountID          uuid.UUID              `json:"issuerUserAccountId"`
	ReviewerUserAccountID        uuid.UUID              `json:"reviewerUserAccountId"`
	GovernanceExceptionKey       string                 `json:"governanceExceptionKey,omitempty"`
	GovernanceExceptionReference string                 `json:"governanceExceptionReference,omitempty"`
	RetentionChecksum            string                 `json:"retentionChecksum"`
	AuditEventID                 uuid.UUID              `json:"auditEventId"`
	AuditEventSequence           int64                  `json:"auditEventSequence"`
	AuditBindingChecksum         string                 `json:"auditBindingChecksum"`
	IdempotencyKey               string                 `json:"idempotencyKey"`
	RequestChecksum              string                 `json:"requestChecksum"`
	CreatedAt                    time.Time              `json:"createdAt"`
}

func ProtectedHistoryArtifactFromDomain(
	artifact protectedhistory.RetainedArtifact,
) ProtectedHistoryArtifact {
	return ProtectedHistoryArtifact{
		ID: artifact.ID, Schema: artifact.Schema,
		SourceSchemaVersion: artifact.SourceSchemaVersion, Scope: artifact.Scope,
		ArtifactID: artifact.ArtifactID, RecordsRoot: artifact.RecordsRoot,
		RecordCount: artifact.RecordCount, ObjectReference: artifact.ObjectReference,
		MediaType: artifact.MediaType, ByteLength: artifact.ByteLength,
		ContentChecksum: artifact.ContentChecksum, CapturedAt: artifact.CapturedAt,
		IssuerUserAccountID:          artifact.IssuerUserAccountID,
		ReviewerUserAccountID:        artifact.ReviewerUserAccountID,
		GovernanceExceptionKey:       artifact.GovernanceExceptionKey,
		GovernanceExceptionReference: artifact.GovernanceExceptionReference,
		RetentionChecksum:            artifact.RetentionChecksum, AuditEventID: artifact.AuditEventID,
		AuditEventSequence:   artifact.AuditEventSequence,
		AuditBindingChecksum: artifact.AuditBindingChecksum,
		IdempotencyKey:       artifact.IdempotencyKey, RequestChecksum: artifact.RequestChecksum,
		CreatedAt: artifact.CreatedAt,
	}
}

type ProtectedHistoryArtifactVerification struct {
	ProtectedHistoryArtifactID uuid.UUID `json:"protectedHistoryArtifactId"`
	ObjectReference            string    `json:"objectReference"`
	MediaType                  string    `json:"mediaType"`
	ByteLength                 int64     `json:"byteLength"`
	ContentChecksum            string    `json:"contentChecksum"`
	VerifiedAt                 time.Time `json:"verifiedAt"`
}
