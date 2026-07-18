package api

import (
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCreateApprovalRequestValidate(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, time.July, 18, 8, 0, 0, 0, time.UTC)
	request := CreateApprovalRequestRequest{ExpiresAt: now.Add(time.Hour)}
	g.Expect(request.Validate(now)).To(Succeed())

	request.ExpiresAt = now
	g.Expect(request.Validate(now)).To(MatchError(ContainSubstring(
		"expiresAt must be in the future",
	)))

	request.ExpiresAt = now.Add(367 * 24 * time.Hour)
	g.Expect(request.Validate(now)).To(MatchError(ContainSubstring(
		"expiresAt must be within 366 days",
	)))
}

func TestRecordApprovalDecisionRequestValidate(t *testing.T) {
	g := NewWithT(t)
	request := RecordApprovalDecisionRequest{
		ApprovalRequirementID:   uuid.New(),
		Decision:                types.ApprovalDecisionApprove,
		Comment:                 "Reviewed immutable plan and policy evidence.",
		ExpectedRequestRevision: 2,
		IdempotencyKey:          "approval-web-42",
	}
	g.Expect(request.Validate()).To(Succeed())

	request.Comment = ""
	g.Expect(request.Validate()).To(MatchError(ContainSubstring("comment is required")))

	request.Comment = "valid"
	request.IdempotencyKey = "spaces are invalid"
	g.Expect(request.Validate()).To(MatchError(ContainSubstring(
		"idempotencyKey must be",
	)))
}

func TestApprovalRequestListValidateBoundsOpaqueCursor(t *testing.T) {
	g := NewWithT(t)
	request := ApprovalRequestListRequest{
		State:  types.ApprovalRequestStatePending,
		Limit:  100,
		Cursor: "eyJ2IjoxfQ",
	}
	g.Expect(request.Validate()).To(Succeed())

	request.Limit = 101
	g.Expect(request.Validate()).To(MatchError(ContainSubstring("limit must be")))

	request.Limit = 1
	request.Cursor = strings.Repeat("a", 2049)
	g.Expect(request.Validate()).To(MatchError(ContainSubstring("cursor is too large")))
}
