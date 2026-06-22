package mapping

import (
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestRunbookToAPI(t *testing.T) {
	g := NewWithT(t)
	id := uuid.New()
	applicationID := uuid.New()

	response := RunbookToAPI(types.Runbook{
		ID:            id,
		ApplicationID: applicationID,
		Name:          "Rotate keys",
		Description:   "description",
		SortOrder:     10,
	})

	g.Expect(response).To(Equal(api.Runbook{
		ID:            id,
		ApplicationID: applicationID,
		Name:          "Rotate keys",
		Description:   "description",
		SortOrder:     10,
	}))
}

func TestRunbookRevisionToAPI(t *testing.T) {
	g := NewWithT(t)
	revisionID := uuid.New()
	stepID := uuid.New()

	response := RunbookRevisionToAPI(types.RunbookRevision{
		ID:             revisionID,
		RunbookID:      uuid.New(),
		RevisionNumber: 2,
		Description:    "second",
		Steps: []types.RunbookStep{{
			ID:                   stepID,
			RunbookRevisionID:    revisionID,
			Key:                  "verify",
			Name:                 "Verify",
			ActionType:           "distr.preflight",
			ExecutionLocation:    "hub",
			InputBindings:        map[string]any{},
			FailureMode:          "fail",
			TimeoutSeconds:       30,
			RetryMaxAttempts:     2,
			RetryIntervalSeconds: 5,
			RequiredPermissions:  []string{"runbook:execute"},
			SortOrder:            10,
			Dependencies:         []string{"prepare"},
		}},
	})

	g.Expect(response.ID).To(Equal(revisionID))
	g.Expect(response.Steps).To(HaveLen(1))
	g.Expect(response.Steps[0].ID).To(Equal(stepID))
	g.Expect(response.Steps[0].RetryPolicy).To(Equal(api.RunbookStepRetryPolicy{
		MaxAttempts:     2,
		IntervalSeconds: 5,
	}))
	g.Expect(response.Steps[0].Dependencies).To(Equal([]string{"prepare"}))
}

func TestRunbookSnapshotToAPI(t *testing.T) {
	g := NewWithT(t)
	snapshotID := uuid.New()
	revisionID := uuid.New()

	response := RunbookSnapshotToAPI(types.RunbookSnapshot{
		ID:                snapshotID,
		RunbookRevisionID: revisionID,
		RevisionNumber:    3,
		CanonicalChecksum: "sha256:abc",
		Revision: types.RunbookRevision{
			ID:             revisionID,
			RevisionNumber: 3,
		},
	})

	g.Expect(response.ID).To(Equal(snapshotID))
	g.Expect(response.RunbookRevisionID).To(Equal(revisionID))
	g.Expect(response.CanonicalChecksum).To(Equal("sha256:abc"))
	g.Expect(response.Revision.ID).To(Equal(revisionID))
}
