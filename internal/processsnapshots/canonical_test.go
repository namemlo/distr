package processsnapshots

import (
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCanonicalizeIsStableForStepOrder(t *testing.T) {
	g := NewWithT(t)
	process := types.DeploymentProcess{
		ID:            uuid.New(),
		ApplicationID: uuid.New(),
	}
	revision := types.DeploymentProcessRevision{
		ID:                  uuid.New(),
		DeploymentProcessID: process.ID,
		RevisionNumber:      1,
		Description:         "Initial revision",
		Steps: []types.DeploymentProcessStep{
			{
				Key:               "deploy",
				Name:              "Deploy",
				ActionType:        "script",
				ExecutionLocation: "hub",
				FailureMode:       "fail",
				SortOrder:         20,
				Dependencies:      []string{"prepare"},
			},
			{
				Key:               "prepare",
				Name:              "Prepare",
				ActionType:        "script",
				ExecutionLocation: "hub",
				FailureMode:       "fail",
				SortOrder:         10,
			},
		},
	}
	reordered := revision
	reordered.Steps = []types.DeploymentProcessStep{revision.Steps[1], revision.Steps[0]}

	firstPayload, firstChecksum, err := Canonicalize(process, revision)
	g.Expect(err).NotTo(HaveOccurred())
	secondPayload, secondChecksum, err := Canonicalize(process, reordered)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(firstPayload).To(Equal(secondPayload))
	g.Expect(firstChecksum).To(Equal(secondChecksum))
	g.Expect(firstChecksum).To(HavePrefix("sha256:"))
	g.Expect(string(firstPayload)).To(ContainSubstring(`"deploymentProcessRevisionId":"` + revision.ID.String() + `"`))
}

func TestCanonicalizeChangesWhenStepContentChanges(t *testing.T) {
	g := NewWithT(t)
	process := types.DeploymentProcess{
		ID:            uuid.New(),
		ApplicationID: uuid.New(),
	}
	revision := types.DeploymentProcessRevision{
		ID:                  uuid.New(),
		DeploymentProcessID: process.ID,
		RevisionNumber:      1,
		Steps: []types.DeploymentProcessStep{
			{
				Key:               "deploy",
				Name:              "Deploy",
				ActionType:        "script",
				ExecutionLocation: "hub",
				InputBindings:     map[string]any{"script": "make deploy"},
				FailureMode:       "fail",
				SortOrder:         10,
			},
		},
	}

	_, firstChecksum, err := Canonicalize(process, revision)
	g.Expect(err).NotTo(HaveOccurred())
	revision.Steps[0].InputBindings["script"] = "make deploy-prod"
	_, secondChecksum, err := Canonicalize(process, revision)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(secondChecksum).NotTo(Equal(firstChecksum))
}
