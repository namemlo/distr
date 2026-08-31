package mapping

import (
	"testing"

	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestProductReleaseToAPIExposesFrozenMigrationContracts(t *testing.T) {
	g := NewWithT(t)
	contract := types.MigrationContract{ID: "ledger.042", Checksum: "sha256:contract"}
	result := ProductReleaseToAPI(types.ReleaseBundle{}, types.ProductReleaseManifest{
		Components: []types.ProductReleaseComponent{{
			MigrationContracts: []types.MigrationContract{contract},
		}},
	})

	g.Expect(result.Manifest.Components).To(HaveLen(1))
	g.Expect(result.Manifest.Components[0].MigrationContracts).To(Equal(
		[]types.MigrationContract{contract},
	))
}
