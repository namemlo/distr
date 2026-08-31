package api

import (
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

type CreateReviewAdmissionDecisionRequest struct {
	ExpectedPlanChecksum   string                        `json:"expectedPlanChecksum"`
	ReviewMaterialChecksum string                        `json:"reviewMaterialChecksum"`
	ObservedStateChecksum  string                        `json:"observedStateChecksum"`
	Decision               types.ReviewAdmissionDecision `json:"decision"`
	Reason                 string                        `json:"reason"`
	ExpiresAt              time.Time                     `json:"expiresAt"`
	SupersedesDecisionID   *uuid.UUID                    `json:"supersedesDecisionId,omitempty"`
	RevokesDecisionID      *uuid.UUID                    `json:"revokesDecisionId,omitempty"`
	IdempotencyKey         string                        `json:"idempotencyKey"`
}

func (request *CreateReviewAdmissionDecisionRequest) Validate(now time.Time) error {
	request.ExpectedPlanChecksum = strings.TrimSpace(request.ExpectedPlanChecksum)
	request.ReviewMaterialChecksum = strings.TrimSpace(request.ReviewMaterialChecksum)
	request.ObservedStateChecksum = strings.TrimSpace(request.ObservedStateChecksum)
	request.Reason = strings.TrimSpace(request.Reason)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if !admissionChecksumPattern.MatchString(request.ExpectedPlanChecksum) ||
		!admissionChecksumPattern.MatchString(request.ReviewMaterialChecksum) ||
		!admissionChecksumPattern.MatchString(request.ObservedStateChecksum) {
		return validation.NewValidationFailedError("review admission checksums are invalid")
	}
	if !request.Decision.IsValid() {
		return validation.NewValidationFailedError("decision must be GO or NO_GO")
	}
	if len(request.Reason) < 1 || len(request.Reason) > 4096 ||
		strings.ContainsAny(request.Reason, "\r\n") {
		return validation.NewValidationFailedError("reason must contain 1-4096 single-line characters")
	}
	if !request.ExpiresAt.After(now.UTC()) || request.ExpiresAt.After(now.UTC().Add(7*24*time.Hour)) {
		return validation.NewValidationFailedError("expiresAt must be in the next seven days")
	}
	if !admissionIdempotencyKeyPattern.MatchString(request.IdempotencyKey) {
		return validation.NewValidationFailedError("idempotencyKey must be 1-128 URL-safe characters")
	}
	if request.RevokesDecisionID != nil && request.Decision != types.ReviewAdmissionDecisionNoGo {
		return validation.NewValidationFailedError("only NO_GO may revoke a prior decision")
	}
	return nil
}
