package mapping

import (
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestDriftCaseMappingRetainsDesiredObservationLineage(t *testing.T) {
	g := NewWithT(t)
	item := types.DriftCase{
		ID: uuid.New(), ActiveDesiredRevisionID: uuid.New(), ObservationID: uuid.New(),
		Status: types.DriftCaseStatusOpen, Classes: []types.DriftClass{types.DriftClassArtifact},
	}

	mapped := DriftCaseToAPI(item)

	g.Expect(mapped.ID).To(Equal(item.ID))
	g.Expect(mapped.ActiveDesiredRevisionID).To(Equal(item.ActiveDesiredRevisionID))
	g.Expect(mapped.ObservationID).To(Equal(item.ObservationID))
	g.Expect(mapped.Classes).To(Equal(item.Classes))
}
