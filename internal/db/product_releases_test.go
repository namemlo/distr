package db

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	. "github.com/onsi/gomega"
)

func TestCreateProductReleaseDraftIdempotencyReplaysAndConflicts(t *testing.T) {
	ctx, _ := deploymentRegistryIsolatedPool(t, 144)
	g := NewWithT(t)
	organizationID, applicationID, channelID := createOperatorReleaseDependencies(t, ctx)
	componentID := insertOperatorReleaseFixture(
		t,
		ctx,
		organizationID,
		applicationID,
		channelID,
		types.ReleaseBundleKindComponent,
		"2.0.0",
		time.Now().UTC(),
	)
	componentChecksum := "sha256:" + strings.Repeat("a", 64)
	_, err := internalctx.GetDb(ctx).Exec(ctx, `
		UPDATE ReleaseBundle
		SET release_contract = @contract::jsonb
		WHERE id = @componentID AND organization_id = @organizationID`, pgx.NamedArgs{
		"componentID": componentID, "organizationID": organizationID,
		"contract": `{
		  "schema":"distr.component-release/v2",
		  "componentKey":"worker",
		  "version":"2.0.0",
		  "source":{"repository":"https://example.invalid/worker.git","requestedRef":"refs/tags/2.0.0","commit":"0123456789012345678901234567890123456789"},
		  "build":{"id":"build-200","builder":"ci-provider"},
		  "artifacts":[],
		  "provides":[],
		  "requires":[],
		  "migrations":[],
		  "changes":{"summary":"Worker release","commits":["0123456789012345678901234567890123456789"]},
		  "evidence":{"provenance":[],"sbom":[],"signatures":[],"tests":[]}
		}`,
	})
	g.Expect(err).NotTo(HaveOccurred())

	newManifest := func(version string) types.ProductReleaseManifest {
		return types.ProductReleaseManifest{
			Schema: types.ProductReleaseSchemaV1, OrganizationID: organizationID,
			ApplicationID: applicationID, ChannelID: channelID, Product: "worker-suite",
			Version: version, DependencyPolicyVersion: uuid.New(),
			Components: []types.ProductReleaseComponent{{
				ComponentReleaseID: componentID, ComponentReleaseChecksum: componentChecksum,
			}},
			RequiredPlatforms: []string{}, Requirements: []types.CapabilityRequirement{},
		}
	}
	firstManifest := newManifest("2026.7.1")
	policyVersion := firstManifest.DependencyPolicyVersion
	first, err := CreateProductReleaseDraftWithIdempotency(ctx, &firstManifest, " product-create-1 ")
	g.Expect(err).NotTo(HaveOccurred())

	replayManifest := newManifest("2026.7.1")
	replayManifest.DependencyPolicyVersion = policyVersion
	replayed, err := CreateProductReleaseDraftWithIdempotency(ctx, &replayManifest, "product-create-1")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(replayed.ID).To(Equal(first.ID))
	g.Expect(replayManifest.ReleaseBundleID).To(Equal(first.ID))

	conflictManifest := newManifest("2026.7.2")
	conflictManifest.DependencyPolicyVersion = policyVersion
	_, err = CreateProductReleaseDraftWithIdempotency(ctx, &conflictManifest, "product-create-1")
	g.Expect(errors.Is(err, ErrReleaseBundleIdempotencyConflict)).To(BeTrue())

	var releases, pins int
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM ReleaseBundle WHERE organization_id = @organizationID AND kind = 'product'),
		  (SELECT count(*) FROM ProductReleaseComponent WHERE organization_id = @organizationID)`,
		pgx.NamedArgs{"organizationID": organizationID},
	).Scan(&releases, &pins)).To(Succeed())
	g.Expect(releases).To(Equal(1))
	g.Expect(pins).To(Equal(1))
}

func TestPublishProductReleaseUsesPersistedProductionEligibilityWithoutHooks(t *testing.T) {
	ctx, _ := deploymentRegistryIsolatedPool(t, 149)
	g := NewWithT(t)
	organizationID, applicationID, channelID := createOperatorReleaseDependencies(t, ctx)
	actorID := createProductReleaseEligibilityTestUser(t, ctx, organizationID)
	component, publication, verifier := productReleasePublicationComponentFixture(
		organizationID,
		applicationID,
		channelID,
	)
	g.Expect(CreateReleaseBundle(ctx, &component)).To(Succeed())
	publishedComponent, result, err := PublishReleaseBundleWithProvenance(
		ctx,
		component.ID,
		organizationID,
		actorID,
		publication,
		verifier,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Valid).To(BeTrue())

	policy := types.DeploymentPolicy{
		OrganizationID: organizationID,
		Key:            "product-release-dependencies",
		Name:           "Product Release dependencies",
	}
	g.Expect(CreateDeploymentPolicy(ctx, &policy)).To(Succeed())
	policyVersion := types.DeploymentPolicyVersion{
		OrganizationID:         organizationID,
		PolicyID:               policy.ID,
		CreatedByUserAccountID: actorID,
		Document: types.DeploymentPolicyDocument{
			Schema: types.DeploymentPolicySchemaV1,
			AdmissionRules: types.AdmissionRules{
				AllowedResolutionModes: []types.RequirementResolutionMode{
					types.RequirementResolutionModeIncluded,
				},
			},
			CampaignRules: types.CampaignRules{
				MaximumWaveSize:    1,
				MaximumConcurrency: 1,
			},
			OverrideRules: types.OverrideRules{Allowed: false},
			BootstrapRules: types.BootstrapRules{
				Mode: types.BootstrapModeAllowAfterPreflight,
			},
		},
	}
	g.Expect(CreateDeploymentPolicyVersion(ctx, &policyVersion)).To(Succeed())
	publishedPolicy, issues, err := PublishDeploymentPolicyVersion(
		ctx,
		policyVersion.ID,
		organizationID,
		actorID,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(issues).To(BeEmpty())

	manifest := types.ProductReleaseManifest{
		Schema:                  types.ProductReleaseSchemaV1,
		OrganizationID:          organizationID,
		ApplicationID:           applicationID,
		ChannelID:               channelID,
		Product:                 "payments-suite",
		Version:                 "2026.7.29",
		DependencyPolicyVersion: publishedPolicy.ID,
		RequiredPlatforms:       []string{"linux/amd64"},
		Components: []types.ProductReleaseComponent{{
			ComponentReleaseID:       publishedComponent.ID,
			ComponentReleaseChecksum: publishedComponent.CanonicalChecksum,
		}},
	}
	draft, err := CreateProductReleaseDraft(ctx, &manifest)
	g.Expect(err).NotTo(HaveOccurred())

	publishedProduct, err := PublishProductRelease(
		WithProductReleaseOrganizationID(ctx, organizationID),
		draft.ID,
		actorID,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(publishedProduct).NotTo(BeNil())
	g.Expect(publishedProduct.Status).To(Equal(types.ReleaseBundleStatusPublished))
}

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

func TestProductReleaseExternalEligibilityNilRestoresPersistedProductionResolvers(t *testing.T) {
	organizationID := uuid.New()
	componentReleaseID := uuid.New()
	manifest, source := productReleaseEligibilityTestMaterial(organizationID, componentReleaseID)
	previousRepository := productionProductReleaseEligibilityRepository
	productionProductReleaseEligibilityRepository = persistedProductReleaseEligibilityRepository{
		source: source,
	}
	defer func() {
		productionProductReleaseEligibilityRepository = previousRepository
	}()
	restoreProvenance := SetProductReleaseProvenanceEligibilityHook(nil)
	defer restoreProvenance()
	restorePolicy := SetProductReleaseDependencyPolicyEligibilityHook(nil)
	defer restorePolicy()

	g := NewWithT(t)
	provenanceIssue, err := productReleaseProvenanceEligibility(
		context.Background(),
		organizationID,
		componentReleaseID,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(provenanceIssue).To(BeNil())

	policyIssue, err := productReleaseDependencyPolicyEligibility(
		context.Background(),
		organizationID,
		manifest,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(policyIssue).To(BeNil())
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
		gotManifest types.ProductReleaseManifest,
	) (*types.ProductReleaseValidationIssue, error) {
		g.Expect(gotOrganizationID).To(Equal(organizationID))
		g.Expect(gotManifest.DependencyPolicyVersion).To(Equal(policyVersionID))
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
		types.ProductReleaseManifest{DependencyPolicyVersion: policyVersionID},
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
