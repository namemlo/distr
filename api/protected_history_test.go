package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCreateProtectedHistoryArtifactRequestAcceptsOnlyScopeReviewAndIdempotency(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	request := CreateProtectedHistoryArtifactRequest{
		CustomerOrganizationIDs: []uuid.UUID{uuid.New()},
		ReviewerUserAccountID:   uuid.New(), IdempotencyKey: "history:before-upgrade",
	}
	g.Expect(request.Validate()).To(Succeed())
	payload, err := json.Marshal(request)
	g.Expect(err).NotTo(HaveOccurred())
	text := string(payload)
	for _, forbidden := range []string{"artifact", "records", "payload", "objectReference", "checksum"} {
		g.Expect(strings.ToLower(text)).NotTo(ContainSubstring(strings.ToLower(forbidden)))
	}
}

func TestCreateProtectedHistoryArtifactRequestRejectsIncompleteMaterial(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	request := CreateProtectedHistoryArtifactRequest{}
	g.Expect(request.Validate()).To(HaveOccurred())
	request.CustomerOrganizationIDs = []uuid.UUID{uuid.New()}
	g.Expect(request.Validate()).To(HaveOccurred())
	request.ReviewerUserAccountID = uuid.New()
	request.IdempotencyKey = "not safe"
	g.Expect(request.Validate()).To(HaveOccurred())
}
