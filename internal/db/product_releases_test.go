package db

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestProductReleaseMigrationIsTenantScopedAndImmutable(t *testing.T) {
	g := NewWithT(t)
	up, err := os.ReadFile("../migrations/sql/144_product_release_capability_graph.up.sql")
	g.Expect(err).NotTo(HaveOccurred())
	sql := string(up)

	for _, fragment := range []string{
		"CREATE TABLE ProductReleaseComponent",
		"CREATE TABLE ProductReleaseCapabilityEdge",
		"FOREIGN KEY (product_release_bundle_id, organization_id)",
		"FOREIGN KEY (component_release_bundle_id, organization_id)",
		"component_release_checksum",
		"contract_snapshot",
		"resolution_stage IN ('product', 'target')",
		"'pinned_existing'",
		"'shared_provider'",
		"'approved_external'",
		"'feature_disabled'",
		"provider_deploy_and_health_before_consumer",
	} {
		g.Expect(sql).To(ContainSubstring(fragment))
	}
	g.Expect(strings.ToUpper(sql)).NotTo(ContainSubstring("TIMESTAMPTZ"))

	down, err := os.ReadFile("../migrations/sql/144_product_release_capability_graph.down.sql")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(down)).To(ContainSubstring("downgrade crossing 144 is forbidden"))
}

func TestProductReleaseContractRoundTripKeepsOnlyTargetNeutralPublicFacts(t *testing.T) {
	g := NewWithT(t)
	manifest := types.ProductReleaseManifest{
		Schema:                  types.ProductReleaseSchemaV1,
		ReleaseBundleID:         uuid.New(),
		OrganizationID:          uuid.New(),
		ApplicationID:           uuid.New(),
		ChannelID:               uuid.New(),
		Product:                 "neutral-suite",
		Version:                 "1.2.3",
		DependencyPolicyVersion: uuid.New(),
		GraphChecksum:           "sha256:" + strings.Repeat("a", 64),
		Components: []types.ProductReleaseComponent{{
			ComponentReleaseID:       uuid.New(),
			ComponentReleaseChecksum: "sha256:" + strings.Repeat("b", 64),
			ComponentKey:             "api",
			Version:                  "2.0.0",
			OrganizationID:           uuid.New(),
			Contract: &types.ComponentReleaseContractV2{
				Schema: types.ReleaseContractSchemaV2,
			},
		}},
	}

	data, err := json.Marshal(productReleaseContract(manifest))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(data)).To(ContainSubstring(`"schema":"distr.product-release/v1"`))
	g.Expect(string(data)).NotTo(ContainSubstring("organizationId"))
	g.Expect(string(data)).NotTo(ContainSubstring("applicationId"))
	g.Expect(string(data)).NotTo(ContainSubstring("contract_snapshot"))
	g.Expect(string(data)).NotTo(ContainSubstring("target"))

	var decoded types.ReleaseContract
	g.Expect(json.Unmarshal(data, &decoded)).To(Succeed())
	g.Expect(decoded.ProductV1).NotTo(BeNil())
	g.Expect(decoded.ProductV1.Product).To(Equal("neutral-suite"))
	g.Expect(decoded.ProductV1.Components[0].ComponentKey).To(Equal("api"))
}

func TestProductReleaseValidationErrorIsBadRequest(t *testing.T) {
	g := NewWithT(t)
	err := &ProductReleaseValidationError{Issues: []types.ProductReleaseValidationIssue{{
		Field: "graph", Rule: "cycle", Message: "cycle",
	}}}
	g.Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())
}
