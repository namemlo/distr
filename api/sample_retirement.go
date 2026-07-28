package api

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

const maximumSampleRetirementItems = 1000

var (
	sampleRetirementChecksumPattern   = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	sampleRetirementSourceKindPattern = regexp.MustCompile(
		`^[a-z][a-z0-9._-]{0,127}$`,
	)
	sampleRetirementApprovalIDPattern = regexp.MustCompile(
		`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,255}$`,
	)
)

type SampleRetirementSubject struct {
	SubjectType       types.SampleRetirementSubjectType `json:"subjectType"`
	SubjectID         uuid.UUID                         `json:"subjectId"`
	OwnershipMarker   string                            `json:"ownershipMarker"`
	OwnershipChecksum string                            `json:"ownershipChecksum"`
	ExpectedChecksum  string                            `json:"expectedChecksum"`
}

type SampleRetirementSelector struct {
	Wildcard    string     `json:"wildcard,omitempty"`
	NamePattern string     `json:"namePattern,omitempty"`
	OlderThan   *time.Time `json:"olderThan,omitempty"`
}

type SampleRetirementPreviewRequest struct {
	BackupReference       string                    `json:"backupReference"`
	BackupChecksum        string                    `json:"backupChecksum"`
	RestoreProofReference string                    `json:"restoreProofReference"`
	RestoreProofChecksum  string                    `json:"restoreProofChecksum"`
	Items                 []SampleRetirementSubject `json:"items"`
	Selector              SampleRetirementSelector  `json:"selector,omitempty"`
}

func (r SampleRetirementPreviewRequest) Validate() error {
	switch {
	case !validSampleRetirementEvidenceReference(r.BackupReference):
		return validation.NewValidationFailedError("backupReference must be an immutable evidence reference")
	case !sampleRetirementChecksumPattern.MatchString(r.BackupChecksum):
		return validation.NewValidationFailedError("backupChecksum must be a lowercase sha256 checksum")
	case !validSampleRetirementEvidenceReference(r.RestoreProofReference):
		return validation.NewValidationFailedError(
			"restoreProofReference must be an immutable evidence reference",
		)
	case !sampleRetirementChecksumPattern.MatchString(r.RestoreProofChecksum):
		return validation.NewValidationFailedError(
			"restoreProofChecksum must be a lowercase sha256 checksum",
		)
	case r.Selector.Wildcard != "" ||
		r.Selector.NamePattern != "" ||
		r.Selector.OlderThan != nil:
		return validation.NewValidationFailedError(
			"selector cleanup is forbidden; items must be an exact ID allowlist",
		)
	case len(r.Items) == 0 || len(r.Items) > maximumSampleRetirementItems:
		return validation.NewValidationFailedError(
			"items must contain between 1 and 1000 exact subjects",
		)
	}

	seen := make(map[string]struct{}, len(r.Items))
	for index, item := range r.Items {
		if err := validateSampleRetirementSubject(item); err != nil {
			return validation.NewValidationFailedError(
				"items[" + strconv.Itoa(index) + "]: " + err.Error(),
			)
		}
		key := string(item.SubjectType) + ":" + item.SubjectID.String()
		if _, exists := seen[key]; exists {
			return validation.NewValidationFailedError(
				"items must not contain duplicate exact subjects",
			)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func (r SampleRetirementPreviewRequest) ToDomain(
	organizationID, actorID uuid.UUID,
) types.SampleRetirementRequest {
	items := make([]types.SampleRetirementSubject, len(r.Items))
	for index, item := range r.Items {
		items[index] = types.SampleRetirementSubject{
			SubjectType:       item.SubjectType,
			SubjectID:         item.SubjectID,
			OwnershipMarker:   item.OwnershipMarker,
			OwnershipChecksum: item.OwnershipChecksum,
			ExpectedChecksum:  item.ExpectedChecksum,
		}
	}
	return types.SampleRetirementRequest{
		OrganizationID:           organizationID,
		RequestedByUserAccountID: actorID,
		BackupReference:          r.BackupReference,
		BackupChecksum:           r.BackupChecksum,
		RestoreProofReference:    r.RestoreProofReference,
		RestoreProofChecksum:     r.RestoreProofChecksum,
		Items:                    items,
		Selector: types.SampleRetirementSelector{
			Wildcard:    r.Selector.Wildcard,
			NamePattern: r.Selector.NamePattern,
			OlderThan:   r.Selector.OlderThan,
		},
	}
}

type ApplySampleRetirementRequest struct {
	PreviewChecksum  string `json:"previewChecksum"`
	ApprovalID       string `json:"approvalId"`
	ApprovalChecksum string `json:"approvalChecksum"`
}

func (r ApplySampleRetirementRequest) Validate() error {
	approvalID, approvalIDErr := uuid.Parse(r.ApprovalID)
	switch {
	case !sampleRetirementChecksumPattern.MatchString(r.PreviewChecksum):
		return validation.NewValidationFailedError(
			"previewChecksum must be a lowercase sha256 checksum",
		)
	case !sampleRetirementApprovalIDPattern.MatchString(r.ApprovalID) ||
		approvalIDErr != nil ||
		approvalID == uuid.Nil:
		return validation.NewValidationFailedError(
			"approvalId must be a non-nil UUID",
		)
	case !sampleRetirementChecksumPattern.MatchString(r.ApprovalChecksum):
		return validation.NewValidationFailedError(
			"approvalChecksum must be a lowercase sha256 checksum",
		)
	default:
		return nil
	}
}

func (r ApplySampleRetirementRequest) ToDomain(
	organizationID, actorID, jobID uuid.UUID,
) types.SampleRetirementApplyRequest {
	return types.SampleRetirementApplyRequest{
		OrganizationID:     organizationID,
		ActorUserAccountID: actorID,
		JobID:              jobID,
		PreviewChecksum:    r.PreviewChecksum,
		ApprovalID:         r.ApprovalID,
		ApprovalChecksum:   r.ApprovalChecksum,
	}
}

type SampleRetirementPreview types.SampleRetirementPreview
type SampleRetirementDetail types.SampleRetirementDetail
type SampleRetirementResult types.SampleRetirementResult
type SampleRetirementVerification types.SampleRetirementVerification

type RegisterSampleRetirementOwnershipEvidenceRequest struct {
	SubjectType       types.SampleRetirementSubjectType `json:"subjectType"`
	SubjectID         uuid.UUID                         `json:"subjectId"`
	OwnershipMarker   string                            `json:"ownershipMarker"`
	OwnershipChecksum string                            `json:"ownershipChecksum"`
	SourceReference   string                            `json:"sourceReference"`
	SourceChecksum    string                            `json:"sourceChecksum"`
}

func (r RegisterSampleRetirementOwnershipEvidenceRequest) Validate() error {
	switch {
	case !validSampleRetirementSubjectType(r.SubjectType):
		return validation.NewValidationFailedError("subjectType is not allowlisted")
	case r.SubjectID == uuid.Nil:
		return validation.NewValidationFailedError("subjectId is required")
	case strings.TrimSpace(r.OwnershipMarker) == "" ||
		strings.TrimSpace(r.OwnershipMarker) != r.OwnershipMarker ||
		len(r.OwnershipMarker) > 256 ||
		strings.ContainsAny(r.OwnershipMarker, "\r\n"):
		return validation.NewValidationFailedError("ownershipMarker is invalid")
	case !sampleRetirementChecksumPattern.MatchString(r.OwnershipChecksum):
		return validation.NewValidationFailedError(
			"ownershipChecksum must be a lowercase sha256 checksum",
		)
	case !validSampleRetirementEvidenceReference(r.SourceReference):
		return validation.NewValidationFailedError(
			"sourceReference must be an immutable evidence reference",
		)
	case !sampleRetirementChecksumPattern.MatchString(r.SourceChecksum):
		return validation.NewValidationFailedError(
			"sourceChecksum must be a lowercase sha256 checksum",
		)
	default:
		return nil
	}
}

func (r RegisterSampleRetirementOwnershipEvidenceRequest) ToDomain(
	organizationID, actorID uuid.UUID,
) types.SampleRetirementOwnershipEvidenceRegistrationInput {
	return types.SampleRetirementOwnershipEvidenceRegistrationInput{
		OrganizationID:          organizationID,
		RecordedByUserAccountID: actorID,
		SubjectType:             r.SubjectType,
		SubjectID:               r.SubjectID,
		OwnershipMarker:         r.OwnershipMarker,
		OwnershipChecksum:       r.OwnershipChecksum,
		SourceReference:         r.SourceReference,
		SourceChecksum:          r.SourceChecksum,
	}
}

type RegisterSampleRetirementRecoveryEvidenceRequest struct {
	EvidenceKind   types.SampleRetirementRecoveryEvidenceKind `json:"evidenceKind"`
	Reference      string                                     `json:"reference"`
	Checksum       string                                     `json:"checksum"`
	SourceKind     string                                     `json:"sourceKind"`
	SourceID       uuid.UUID                                  `json:"sourceId"`
	SourceChecksum string                                     `json:"sourceChecksum"`
	VerifiedAt     time.Time                                  `json:"verifiedAt"`
}

func (r RegisterSampleRetirementRecoveryEvidenceRequest) Validate(now time.Time) error {
	switch {
	case !r.EvidenceKind.IsValid():
		return validation.NewValidationFailedError(
			"evidenceKind must be backup or restore_proof",
		)
	case !validSampleRetirementEvidenceReference(r.Reference):
		return validation.NewValidationFailedError(
			"reference must be an immutable evidence reference",
		)
	case !sampleRetirementChecksumPattern.MatchString(r.Checksum):
		return validation.NewValidationFailedError(
			"checksum must be a lowercase sha256 checksum",
		)
	case !sampleRetirementSourceKindPattern.MatchString(r.SourceKind):
		return validation.NewValidationFailedError("sourceKind is invalid")
	case r.SourceID == uuid.Nil:
		return validation.NewValidationFailedError("sourceId is required")
	case !sampleRetirementChecksumPattern.MatchString(r.SourceChecksum):
		return validation.NewValidationFailedError(
			"sourceChecksum must be a lowercase sha256 checksum",
		)
	case r.VerifiedAt.IsZero() || r.VerifiedAt.After(now):
		return validation.NewValidationFailedError(
			"verifiedAt must not be in the future",
		)
	default:
		return nil
	}
}

func (r RegisterSampleRetirementRecoveryEvidenceRequest) ToDomain(
	organizationID, actorID uuid.UUID,
) types.SampleRetirementRecoveryEvidenceRegistrationInput {
	return types.SampleRetirementRecoveryEvidenceRegistrationInput{
		OrganizationID:          organizationID,
		VerifiedByUserAccountID: actorID,
		EvidenceKind:            r.EvidenceKind,
		Reference:               r.Reference,
		Checksum:                r.Checksum,
		SourceKind:              r.SourceKind,
		SourceID:                r.SourceID,
		SourceChecksum:          r.SourceChecksum,
		VerifiedAt:              r.VerifiedAt,
	}
}

type SampleRetirementOwnershipEvidence types.SampleRetirementOwnershipEvidence
type SampleRetirementRecoveryEvidence types.SampleRetirementRecoveryEvidence

func validateSampleRetirementSubject(item SampleRetirementSubject) error {
	switch {
	case !validSampleRetirementSubjectType(item.SubjectType):
		return validation.NewValidationFailedError("subjectType is not allowlisted")
	case item.SubjectID == uuid.Nil:
		return validation.NewValidationFailedError("subjectId is required")
	case strings.TrimSpace(item.OwnershipMarker) == "" ||
		strings.TrimSpace(item.OwnershipMarker) != item.OwnershipMarker ||
		len(item.OwnershipMarker) > 256 ||
		strings.ContainsAny(item.OwnershipMarker, "\r\n"):
		return validation.NewValidationFailedError("ownershipMarker is required")
	case !sampleRetirementChecksumPattern.MatchString(item.OwnershipChecksum):
		return validation.NewValidationFailedError(
			"ownershipChecksum must be a lowercase sha256 checksum",
		)
	case !sampleRetirementChecksumPattern.MatchString(item.ExpectedChecksum):
		return validation.NewValidationFailedError(
			"expectedChecksum must be a lowercase sha256 checksum",
		)
	default:
		return nil
	}
}

func validSampleRetirementSubjectType(
	subjectType types.SampleRetirementSubjectType,
) bool {
	return subjectType == types.SampleRetirementSubjectApplication ||
		subjectType == types.SampleRetirementSubjectDeploymentTarget ||
		subjectType == types.SampleRetirementSubjectEnvironment
}

func validSampleRetirementEvidenceReference(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || len(value) > 1024 {
		return false
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return r <= ' ' || r == '\u007f'
	}) >= 0 {
		return false
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme != "" && (parsed.Host != "" || parsed.Opaque != "" || parsed.Path != "")
}
