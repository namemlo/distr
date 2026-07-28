package mapping

import (
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestOperatorControlPlanePagesPreserveCursorTotalAndRows(t *testing.T) {
	g := NewWithT(t)
	total := int64(123)
	row := types.FleetRow{ID: uuid.New(), Component: "api"}

	result := OperatorFleetPageToAPI(types.OperatorPage[types.FleetRow]{
		Items:      []types.FleetRow{row},
		NextCursor: "next",
		Total:      &total,
	})

	g.Expect(result).To(Equal(api.OperatorFleetPage{
		Items:      []types.FleetRow{row},
		NextCursor: "next",
		Total:      &total,
	}))
}

func TestOperatorControlPlaneDetailsAndEvidenceUseExplicitResponseEnvelopes(t *testing.T) {
	g := NewWithT(t)
	releaseID := uuid.New()
	evidence := types.OperatorEvidenceRef{ID: uuid.New(), Kind: "provenance"}
	detail := types.OperatorReleaseDetail{
		Release:  types.OperatorReleaseRow{ID: releaseID},
		Evidence: []types.OperatorEvidenceRef{evidence},
	}

	g.Expect(OperatorReleaseDetailToAPI(detail)).To(Equal(
		api.OperatorReleaseDetailResponse{Detail: detail},
	))
	g.Expect(OperatorEvidenceToAPI(detail.Evidence)).To(Equal(
		api.OperatorEvidencePage{Items: []types.OperatorEvidenceRef{evidence}},
	))
}
