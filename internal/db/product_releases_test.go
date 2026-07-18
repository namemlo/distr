package db

import (
	"context"
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
		"releasebundle_product_version_length_check",
		"productreleasecomponent_version_length_check",
		"productreleasecapabilityedge_indexed_values_check",
		"octet_length(edge_key) BETWEEN 1 AND 512",
	} {
		g.Expect(sql).To(ContainSubstring(fragment))
	}
	g.Expect(strings.ToUpper(sql)).NotTo(ContainSubstring("TIMESTAMPTZ"))

	down, err := os.ReadFile("../migrations/sql/144_product_release_capability_graph.down.sql")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(down)).To(ContainSubstring("downgrade crossing 144 is forbidden"))
	g.Expect(string(down)).To(ContainSubstring(
		"DROP CONSTRAINT releasebundle_product_version_length_check",
	))
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

func TestProductReleaseExternalEligibilityDefaultsFailClosed(t *testing.T) {
	restoreProvenance := SetProductReleaseProvenanceEligibilityHook(nil)
	defer restoreProvenance()
	restorePolicy := SetProductReleaseDependencyPolicyEligibilityHook(nil)
	defer restorePolicy()

	g := NewWithT(t)
	provenanceIssue, err := productReleaseProvenanceEligibility(
		context.Background(),
		uuid.New(),
		uuid.New(),
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(provenanceIssue).NotTo(BeNil())
	g.Expect(provenanceIssue.Rule).To(Equal("provenanceVerifierUnavailable"))

	policyIssue, err := productReleaseDependencyPolicyEligibility(
		context.Background(),
		uuid.New(),
		uuid.New(),
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(policyIssue).NotTo(BeNil())
	g.Expect(policyIssue.Rule).To(Equal("publishedPolicyUnavailable"))
}

func TestProductReleaseExternalEligibilityAdaptersReceiveExactScopedPins(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	componentReleaseID := uuid.New()
	policyVersionID := uuid.New()

	restoreProvenance := SetProductReleaseProvenanceEligibilityHook(func(
		_ context.Context,
		gotOrganizationID uuid.UUID,
		gotComponentReleaseID uuid.UUID,
	) (*types.ProductReleaseValidationIssue, error) {
		g.Expect(gotOrganizationID).To(Equal(organizationID))
		g.Expect(gotComponentReleaseID).To(Equal(componentReleaseID))
		return nil, nil
	})
	defer restoreProvenance()
	restorePolicy := SetProductReleaseDependencyPolicyEligibilityHook(func(
		_ context.Context,
		gotOrganizationID uuid.UUID,
		gotPolicyVersionID uuid.UUID,
	) (*types.ProductReleaseValidationIssue, error) {
		g.Expect(gotOrganizationID).To(Equal(organizationID))
		g.Expect(gotPolicyVersionID).To(Equal(policyVersionID))
		return nil, nil
	})
	defer restorePolicy()

	issue, err := productReleaseProvenanceEligibility(
		context.Background(),
		organizationID,
		componentReleaseID,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(issue).To(BeNil())
	issue, err = productReleaseDependencyPolicyEligibility(
		context.Background(),
		organizationID,
		policyVersionID,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(issue).To(BeNil())
}

func TestProductReleasePublicationLocksChildrenAndRunsEligibilityBeforeUpdate(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("product_releases.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)
	g.Expect(text).To(ContainSubstring("rb.id = ANY(@componentReleaseIds)"))
	g.Expect(text).To(ContainSubstring("ORDER BY rb.id`+lockClause"))
	g.Expect(text).To(ContainSubstring(`lockClause = " FOR UPDATE OF rb"`))

	start := strings.Index(text, "func PublishProductRelease(")
	end := strings.Index(text[start:], "func currentOrganizationID(")
	g.Expect(start).To(BeNumerically(">=", 0))
	g.Expect(end).To(BeNumerically(">", 0))
	publishBody := text[start : start+end]
	eligibility := strings.Index(
		publishBody,
		"validateProductReleaseEligibility(ctx, *manifest, organizationID, true)",
	)
	update := strings.Index(publishBody, "publishProductReleaseRow(ctx")
	g.Expect(eligibility).To(BeNumerically(">=", 0))
	g.Expect(update).To(BeNumerically(">", eligibility))
}
