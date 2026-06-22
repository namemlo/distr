package runbooks

import (
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCanonicalizeIsStableForRunbookStepOrder(t *testing.T) {
	g := NewWithT(t)
	runbook := types.Runbook{
		ID:            uuid.New(),
		ApplicationID: uuid.New(),
	}
	revision := types.RunbookRevision{
		ID:             uuid.New(),
		RunbookID:      runbook.ID,
		RevisionNumber: 1,
		Description:    "Initial runbook",
		Steps: []types.RunbookStep{
			{
				Key:               "verify",
				Name:              "Verify",
				ActionType:        "distr.http.check",
				ExecutionLocation: "hub",
				FailureMode:       "fail",
				SortOrder:         20,
				Dependencies:      []string{"prepare"},
			},
			{
				Key:               "prepare",
				Name:              "Prepare",
				ActionType:        "distr.preflight",
				ExecutionLocation: "hub",
				FailureMode:       "fail",
				SortOrder:         10,
			},
		},
	}
	reordered := revision
	reordered.Steps = []types.RunbookStep{revision.Steps[1], revision.Steps[0]}

	firstPayload, firstChecksum, err := Canonicalize(runbook, revision)
	g.Expect(err).NotTo(HaveOccurred())
	secondPayload, secondChecksum, err := Canonicalize(runbook, reordered)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(firstPayload).To(Equal(secondPayload))
	g.Expect(firstChecksum).To(Equal(secondChecksum))
	g.Expect(firstChecksum).To(HavePrefix("sha256:"))
	g.Expect(string(firstPayload)).To(ContainSubstring(`"runbookRevisionId":"` + revision.ID.String() + `"`))
}

func TestCanonicalizeChangesWhenRunbookStepContentChanges(t *testing.T) {
	g := NewWithT(t)
	runbook := types.Runbook{
		ID:            uuid.New(),
		ApplicationID: uuid.New(),
	}
	revision := types.RunbookRevision{
		ID:             uuid.New(),
		RunbookID:      runbook.ID,
		RevisionNumber: 1,
		Steps: []types.RunbookStep{
			{
				Key:               "verify",
				Name:              "Verify",
				ActionType:        "distr.http.check",
				ExecutionLocation: "hub",
				InputBindings:     map[string]any{"url": "https://example.com/health"},
				FailureMode:       "fail",
				SortOrder:         10,
			},
		},
	}

	_, firstChecksum, err := Canonicalize(runbook, revision)
	g.Expect(err).NotTo(HaveOccurred())
	revision.Steps[0].InputBindings["url"] = "https://example.com/ready"
	_, secondChecksum, err := Canonicalize(runbook, revision)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(secondChecksum).NotTo(Equal(firstChecksum))
}
