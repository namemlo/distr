package api

import (
	"regexp"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

var deploymentPlanDraftChecksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type CreateDeploymentPlanDraftRequest struct {
	ProductReleaseID           uuid.UUID  `json:"productReleaseId"`
	DeploymentUnitID           uuid.UUID  `json:"deploymentUnitId"`
	EnvironmentAssignmentID    uuid.UUID  `json:"environmentAssignmentId"`
	TargetConfigSnapshotID     uuid.UUID  `json:"targetConfigSnapshotId"`
	ProtocolVersion            string     `json:"protocolVersion"`
	SupersedesDeploymentPlanID *uuid.UUID `json:"supersedesDeploymentPlanId,omitempty"`
	SupersedeReason            string     `json:"supersedeReason,omitempty"`
}

func (r CreateDeploymentPlanDraftRequest) Validate() error {
	required := []struct {
		name  string
		value uuid.UUID
	}{
		{"productReleaseId", r.ProductReleaseID},
		{"deploymentUnitId", r.DeploymentUnitID},
		{"environmentAssignmentId", r.EnvironmentAssignmentID},
		{"targetConfigSnapshotId", r.TargetConfigSnapshotID},
	}
	for _, field := range required {
		if field.value == uuid.Nil {
			return validation.NewValidationFailedError(field.name + " is required")
		}
	}
	if r.ProtocolVersion != types.DeploymentPlanProtocolV1 &&
		r.ProtocolVersion != types.DeploymentPlanProtocolV2 {
		return validation.NewValidationFailedError("protocolVersion must be v1 or v2")
	}
	reason := strings.TrimSpace(r.SupersedeReason)
	if (r.SupersedesDeploymentPlanID == nil) != (reason == "") {
		return validation.NewValidationFailedError(
			"supersedesDeploymentPlanId and supersedeReason must be supplied together",
		)
	}
	if len(reason) > 2048 || strings.ContainsAny(r.SupersedeReason, "\r\n") {
		return validation.NewValidationFailedError("supersedeReason is invalid")
	}
	return nil
}

type UpdateDeploymentPlanDraftRequest struct {
	ExpectedRevision int64 `json:"expectedRevision"`
	CreateDeploymentPlanDraftRequest
}

func (r UpdateDeploymentPlanDraftRequest) Validate() error {
	if r.ExpectedRevision < 1 {
		return validation.NewValidationFailedError("expectedRevision must be positive")
	}
	return r.CreateDeploymentPlanDraftRequest.Validate()
}

type PublishDeploymentPlanDraftRequest struct {
	ExpectedRevision        int64  `json:"expectedRevision"`
	ExpectedPreviewChecksum string `json:"expectedPreviewChecksum"`
}

func (r PublishDeploymentPlanDraftRequest) Validate() error {
	if r.ExpectedRevision < 1 {
		return validation.NewValidationFailedError("expectedRevision must be positive")
	}
	if !deploymentPlanDraftChecksumPattern.MatchString(r.ExpectedPreviewChecksum) {
		return validation.NewValidationFailedError(
			"expectedPreviewChecksum must be lowercase sha256",
		)
	}
	return nil
}

type DeploymentPlanDraft struct {
	ID                            uuid.UUID  `json:"id"`
	CreatedAt                     time.Time  `json:"createdAt"`
	UpdatedAt                     time.Time  `json:"updatedAt"`
	CreatedByUserAccountID        uuid.UUID  `json:"createdByUserAccountId"`
	UpdatedByUserAccountID        uuid.UUID  `json:"updatedByUserAccountId"`
	Revision                      int64      `json:"revision"`
	ProductReleaseID              uuid.UUID  `json:"productReleaseId"`
	DeploymentUnitID              uuid.UUID  `json:"deploymentUnitId"`
	EnvironmentAssignmentID       uuid.UUID  `json:"environmentAssignmentId"`
	TargetConfigSnapshotID        uuid.UUID  `json:"targetConfigSnapshotId"`
	ProtocolVersion               string     `json:"protocolVersion"`
	SupersedesDeploymentPlanID    *uuid.UUID `json:"supersedesDeploymentPlanId,omitempty"`
	SupersedeReason               string     `json:"supersedeReason,omitempty"`
	PreviewChecksum               string     `json:"previewChecksum,omitempty"`
	PublishedDeploymentPlanID     *uuid.UUID `json:"publishedDeploymentPlanId,omitempty"`
	PublishedDeploymentPlanStatus string     `json:"publishedDeploymentPlanStatus,omitempty"`
}

type DeploymentPlanDraftValidation struct {
	Draft           DeploymentPlanDraft           `json:"draft"`
	Resolutions     []types.RequirementResolution `json:"resolutions"`
	Graph           types.TargetPlanGraph         `json:"graph"`
	Issues          []types.ValidationIssue       `json:"issues"`
	PreviewChecksum string                        `json:"previewChecksum,omitempty"`
}
