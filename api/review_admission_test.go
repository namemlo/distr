package api_test

import (
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestReviewAdmissionRequestBindsChecksumsAndRevocation(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	request := api.CreateReviewAdmissionDecisionRequest{
		ExpectedPlanChecksum: checksum('a'), ReviewMaterialChecksum: checksum('b'),
		ObservedStateChecksum: checksum('c'), Decision: types.ReviewAdmissionDecisionGo,
		Reason: "reviewed", ExpiresAt: now.Add(time.Hour), IdempotencyKey: "review-1",
	}
	g.Expect(request.Validate(now)).To(Succeed())
	revoked := uuid.New()
	request.RevokesDecisionID = &revoked
	g.Expect(request.Validate(now)).To(MatchError(ContainSubstring("only NO_GO")))
}

func checksum(character byte) string {
	return "sha256:" + strings.Repeat(string(character), 64)
}
