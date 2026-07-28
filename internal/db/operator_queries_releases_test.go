package db

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/gomega"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
)

func TestOperatorReleaseListUsesConstantQueriesAtMaximumPageSize(t *testing.T) {
	ctx, pool := deploymentRegistryIsolatedPool(t, 144)
	g := NewWithT(t)
	organizationID, applicationID, channelID := createOperatorReleaseDependencies(t, ctx)
	decisionAt := time.Now().UTC()
	for index := 0; index < 5; index++ {
		insertOperatorReleaseFixture(
			t,
			ctx,
			organizationID,
			applicationID,
			channelID,
			types.ReleaseBundleKindComponent,
			"component-"+uuid.NewString(),
			decisionAt.Add(-time.Duration(index)*time.Minute),
		)
	}
	counting := &operatorReleaseQueryCountingPool{Pool: pool}

	result, err := ListOperatorReleaseRows(internalctx.WithDb(ctx, counting), OperatorReleaseQuery{
		OrganizationID:   organizationID,
		DecisionAt:       decisionAt,
		OrganizationWide: true,
		CustomerScopeIDs: []uuid.UUID{}, EnvironmentScopeIDs: []uuid.UUID{},
		DeploymentUnitScopeIDs: []uuid.UUID{}, ComponentScopeIDs: []uuid.UUID{},
		CampaignScopeIDs: []uuid.UUID{}, Limit: 101,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Items).To(HaveLen(5))
	g.Expect(result.Total).To(Equal(int64(5)))
	g.Expect(counting.queries.Load()).To(Equal(int64(1)))
}

func TestOperatorReleaseDetailExposesImmutableReleaseFacts(t *testing.T) {
	ctx, _ := deploymentRegistryIsolatedPool(t, 144)
	g := NewWithT(t)
	organizationID, applicationID, channelID := createOperatorReleaseDependencies(t, ctx)
	otherOrganizationID := createOperatorReleaseOrganization(t, ctx)
	decisionAt := time.Now().UTC()
	leftID := insertOperatorReleaseFixture(
		t, ctx, organizationID, applicationID, channelID,
		types.ReleaseBundleKindComponent, "1.0.0", decisionAt.Add(-time.Minute),
	)
	leftManifest := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	leftDigest := "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	insertOperatorReleaseArtifact(t, ctx, organizationID, leftID, "api", "1.0.0", leftManifest, leftDigest)
	_, err := internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO ComponentReleaseEvidence (
		  release_bundle_id, organization_id, evidence_type, reference
		) VALUES (@releaseID, @organizationID, 'sbom', @reference)`, pgx.NamedArgs{
		"releaseID": leftID, "organizationID": organizationID,
		"reference": "oci://evidence.example.invalid/api/sbom@" + leftDigest,
	})
	g.Expect(err).NotTo(HaveOccurred())
	scope := operatorOrganizationWideScope(organizationID, decisionAt)

	detail, err := GetOperatorReleaseDetail(ctx, scope, leftID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(detail.Release.ID).To(Equal(leftID))
	g.Expect(detail.Release.Checksum).To(HavePrefix("sha256:"))
	g.Expect(detail.Artifacts).To(HaveLen(1))
	g.Expect(detail.Artifacts[0].ManifestDigest).To(Equal(leftManifest))
	g.Expect(detail.Artifacts[0].PlatformDigests).To(HaveKeyWithValue("linux/amd64", leftDigest))
	g.Expect(detail.Evidence).To(HaveLen(1))
	g.Expect(detail.Evidence[0].Href).To(ContainSubstring("oci://evidence.example.invalid/"))

	_, err = GetOperatorReleaseDetail(ctx, operatorOrganizationWideScope(otherOrganizationID, decisionAt), leftID)
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
}

func TestOperatorProductReleaseDetailExposesPinsAndCapabilityDAG(t *testing.T) {
	ctx, _ := deploymentRegistryIsolatedPool(t, 144)
	g := NewWithT(t)
	organizationID, applicationID, channelID := createOperatorReleaseDependencies(t, ctx)
	decisionAt := time.Now().UTC()
	componentID := insertOperatorReleaseFixture(
		t, ctx, organizationID, applicationID, channelID,
		types.ReleaseBundleKindComponent, "2.0.0", decisionAt.Add(-time.Minute),
	)
	componentDigest := "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	insertOperatorReleaseArtifact(
		t, ctx, organizationID, componentID, "worker", "2.0.0",
		"sha256:abababababababababababababababababababababababababababababababab",
		componentDigest,
	)
	productID := insertOperatorReleaseFixture(
		t, ctx, organizationID, applicationID, channelID,
		types.ReleaseBundleKindProduct, "2026.07.22.1", decisionAt,
	)
	componentChecksum := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	_, err := internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO ProductReleaseComponent (
		  product_release_bundle_id, organization_id, component_release_bundle_id,
		  component_release_checksum, component_key, component_version, contract_snapshot
		) VALUES (
		  @productID, @organizationID, @componentID,
		  @checksum, 'worker', '2.0.0', '{"schema":"distr.component-release/v2"}'::jsonb
		);
		INSERT INTO ProductReleaseCapabilityEdge (
		  product_release_bundle_id, organization_id, edge_key, from_node_key, to_node_key,
		  consumer_component_key, provider_component_key, capability_name, version_range,
		  provider_version, resolution_stage, allowed_modes, ordering
		) VALUES (
		  @productID, @organizationID, 'worker-needs-cache', 'component:worker',
		  'target:cache', 'worker', NULL, 'cache.read', '>=1.0.0', '',
		  'target', ARRAY['included']::text[], ''
		)`, pgx.NamedArgs{
		"productID": productID, "organizationID": organizationID,
		"componentID": componentID, "checksum": componentChecksum,
	})
	g.Expect(err).NotTo(HaveOccurred())

	detail, err := GetOperatorReleaseDetail(
		ctx,
		operatorOrganizationWideScope(organizationID, decisionAt),
		productID,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(detail.ComponentPins).To(ConsistOf(types.OperatorReleaseComponentPin{
		ComponentReleaseID: componentID,
		Component:          "worker",
		Version:            "2.0.0",
		Checksum:           componentChecksum,
		Digest:             componentDigest,
	}))
	g.Expect(detail.GraphEdges).To(ConsistOf(types.OperatorReleaseGraphEdge{
		From: "component:worker", To: "target:cache", Kind: "cache.read",
	}))
}

func insertOperatorReleaseFixture(
	t *testing.T,
	ctx context.Context,
	organizationID uuid.UUID,
	applicationID uuid.UUID,
	channelID uuid.UUID,
	kind types.ReleaseBundleKind,
	version string,
	createdAt time.Time,
) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		INSERT INTO ReleaseBundle (
		  created_at, updated_at, organization_id, application_id, channel_id,
		  release_number, release_notes, source_revision, status,
		  canonical_checksum, canonical_payload, kind, release_contract_schema
		) VALUES (
		  @createdAt, @createdAt, @organizationID, @applicationID, @channelID,
		  @version, '', @sourceRevision, 'PUBLISHED',
		  @checksum, '{}'::bytea, @kind, @schema
		) RETURNING id`, pgx.NamedArgs{
		"createdAt": createdAt, "organizationID": organizationID,
		"applicationID": applicationID, "channelID": channelID,
		"version": version, "sourceRevision": "0123456789012345678901234567890123456789",
		"checksum": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"kind":     kind, "schema": releaseContractSchemaForKind(kind),
	}).Scan(&id)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return id
}

func insertOperatorReleaseArtifact(
	t *testing.T,
	ctx context.Context,
	organizationID uuid.UUID,
	releaseID uuid.UUID,
	component string,
	version string,
	manifestDigest string,
	platformDigest string,
) {
	t.Helper()
	_, err := internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO ComponentReleaseArtifact (
		  release_bundle_id, organization_id, component_key, component_version,
		  artifact_key, artifact_type, media_type, manifest_digest, platform, platform_digest
		) VALUES (
		  @releaseID, @organizationID, @component, @version,
		  @component, 'oci-image', 'application/vnd.oci.image.manifest.v1+json',
		  @manifestDigest, 'linux/amd64', @platformDigest
		)`, pgx.NamedArgs{
		"releaseID": releaseID, "organizationID": organizationID,
		"component": component, "version": version,
		"manifestDigest": manifestDigest, "platformDigest": platformDigest,
	})
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
}

func operatorOrganizationWideScope(
	organizationID uuid.UUID,
	decisionAt time.Time,
) types.OperatorScopeFilter {
	return types.OperatorScopeFilter{
		OrganizationID:    organizationID,
		DecisionAt:        decisionAt,
		OrganizationWide:  true,
		CustomerIDs:       []uuid.UUID{},
		EnvironmentIDs:    []uuid.UUID{},
		DeploymentUnitIDs: []uuid.UUID{},
		ComponentIDs:      []uuid.UUID{},
		CampaignIDs:       []uuid.UUID{},
	}
}

func releaseContractSchemaForKind(kind types.ReleaseBundleKind) string {
	switch kind {
	case types.ReleaseBundleKindComponent:
		return types.ReleaseContractSchemaV2
	case types.ReleaseBundleKindProduct:
		return types.ProductReleaseSchemaV1
	default:
		return "distr.release/v1"
	}
}

type operatorReleaseQueryCountingPool struct {
	*pgxpool.Pool
	queries atomic.Int64
}

func createOperatorReleaseDependencies(t *testing.T, ctx context.Context) (uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	organizationID := createOperatorReleaseOrganization(t, ctx)
	application := types.Application{
		Name: "Operator release application " + uuid.NewString(),
		Type: types.DeploymentTypeDocker,
	}
	if err := CreateApplication(ctx, &application, organizationID); err != nil {
		t.Fatalf("create operator release application: %v", err)
	}

	var lifecycleID uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO Lifecycle (organization_id, name) VALUES (@organizationID, @name) RETURNING id`,
		pgx.NamedArgs{"organizationID": organizationID, "name": "Operator release lifecycle " + uuid.NewString()},
	).Scan(&lifecycleID); err != nil {
		t.Fatalf("create operator release lifecycle: %v", err)
	}

	channel := types.Channel{
		OrganizationID: organizationID,
		ApplicationID:  application.ID,
		LifecycleID:    lifecycleID,
		Name:           "Stable",
		IsDefault:      true,
	}
	if err := CreateChannel(ctx, &channel); err != nil {
		t.Fatalf("create operator release channel: %v", err)
	}
	return organizationID, application.ID, channel.ID
}

func createOperatorReleaseOrganization(t *testing.T, ctx context.Context) uuid.UUID {
	t.Helper()
	var organizationID uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO Organization (name) VALUES (@name) RETURNING id`,
		pgx.NamedArgs{"name": "Operator release organization " + uuid.NewString()},
	).Scan(&organizationID); err != nil {
		t.Fatalf("create operator release organization: %v", err)
	}
	return organizationID
}

func (pool *operatorReleaseQueryCountingPool) Query(
	ctx context.Context,
	sql string,
	args ...any,
) (pgx.Rows, error) {
	pool.queries.Add(1)
	return pool.Pool.Query(ctx, sql, args...)
}

func (pool *operatorReleaseQueryCountingPool) QueryRow(
	ctx context.Context,
	sql string,
	args ...any,
) pgx.Row {
	pool.queries.Add(1)
	return pool.Pool.QueryRow(ctx, sql, args...)
}
