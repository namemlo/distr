package db

import (
	"context"
	"errors"
	"strings"
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
	ctx, pool := deploymentRegistryIsolatedPool(t, 145)
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
	ctx, _ := deploymentRegistryIsolatedPool(t, 145)
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
	ctx, _ := deploymentRegistryIsolatedPool(t, 145)
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
	provenanceDigest := "sha256:" + strings.Repeat("1", 64)
	keyFingerprint := "sha256:" + strings.Repeat("5", 64)
	sbomDigest := "sha256:" + strings.Repeat("2", 64)
	_, err := internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO ComponentReleaseEvidence (
		  release_bundle_id, organization_id, evidence_type, reference
		) VALUES
		  (@componentID, @organizationID, 'provenance', 'oci://evidence.invalid/worker/provenance'),
		  (@componentID, @organizationID, 'sbom', 'oci://evidence.invalid/worker/sbom@' || @sbomDigest);
		INSERT INTO ComponentReleaseEvidenceVerification (
		  organization_id, release_bundle_id, artifact_key, platform, artifact_digest,
		  evidence_reference, evidence_digest, policy_checksum, trust_root_id,
		  predicate_type, builder_id, build_id, source_uri, source_commit, build_type,
		  external_parameters_checksum, signer_issuer, signer_identity, verified_at
		) VALUES (
		  @organizationID, @componentID, 'worker', 'linux/amd64', @componentDigest,
		  'oci://evidence.invalid/worker/provenance', @provenanceDigest,
		  'sha256:' || repeat('3', 64), 'release-key-1', 'https://slsa.dev/provenance/v1',
		  'verified-builder', 'verified-build-200', 'git+https://example.invalid/worker.git',
		  'fedcba9876543210fedcba9876543210fedcba98', 'https://slsa.dev/provenance/v1',
		  'sha256:' || repeat('4', 64), 'keyid:release-key-1', @keyFingerprint, now()
		)`, pgx.NamedArgs{
		"componentID": componentID, "organizationID": organizationID,
		"componentDigest": componentDigest, "provenanceDigest": provenanceDigest,
		"sbomDigest": sbomDigest, "keyFingerprint": keyFingerprint,
	})
	g.Expect(err).NotTo(HaveOccurred())
	productID := insertOperatorReleaseFixture(
		t, ctx, organizationID, applicationID, channelID,
		types.ReleaseBundleKindProduct, "2026.07.22.1", decisionAt,
	)
	componentChecksum := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	_, err = internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO ProductReleaseComponent (
		  product_release_bundle_id, organization_id, component_release_bundle_id,
		  component_release_checksum, component_key, component_version, contract_snapshot
		) VALUES (
		  @productID, @organizationID, @componentID,
		  @checksum, 'worker', '2.0.0', @contract::jsonb
		);
		INSERT INTO ProductReleaseCapabilityEdge (
		  product_release_bundle_id, organization_id, edge_key, from_node_key, to_node_key,
		  consumer_component_key, provider_component_key, capability_name, version_range,
		  provider_version, resolution_stage, allowed_modes, ordering
		) VALUES (
		  @productID, @organizationID, 'worker-needs-cache', 'component:worker',
		  'target:cache', 'worker', NULL, 'cache.read', '>=1.0.0', '',
		  'target', ARRAY['included', 'shared_provider']::text[], ''
		), (
		  @productID, @organizationID, 'worker-needs-worker', 'component:worker',
		  'component:worker', 'worker', 'worker', 'worker.rpc', '^2.0.0', '2.0.0',
		  'product', ARRAY[]::text[], 'provider_deploy_and_health_before_consumer'
		)`, pgx.NamedArgs{
		"productID": productID, "organizationID": organizationID,
		"componentID": componentID, "checksum": componentChecksum,
		"contract": `{
		  "schema":"distr.component-release/v2",
		  "componentKey":"worker",
		  "version":"2.0.0",
		  "source":{"repository":"https://example.invalid/worker.git","requestedRef":"refs/tags/2.0.0","commit":"0123456789012345678901234567890123456789"},
		  "build":{"id":"build-200","builder":"ci-provider"},
		  "artifacts":[],
		  "provides":[],
		  "requires":[{"name":"cache.read","range":">=1.0.0","resolutionStage":"target","allowedModes":["included","shared_provider"]}],
		  "migrations":[{"key":"worker-db","type":"sql","order":10,"compatibility":"backward","failurePolicy":"stop","description":"Add worker queue index"}],
		  "changes":{"summary":"Worker retry behavior","commits":["0123456789012345678901234567890123456789"]},
		  "evidence":{"provenance":[],"sbom":[],"signatures":[],"tests":[]}
		}`,
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
	g.Expect(detail.GraphEdges).To(ConsistOf(
		types.OperatorReleaseGraphEdge{
			From: "component:worker", To: "target:cache", Kind: "cache.read",
			ConsumerComponent: "worker", Capability: "cache.read", VersionRange: ">=1.0.0",
			ResolutionStage: "target", AllowedModes: []string{"included", "shared_provider"},
			ProviderArtifacts: []types.OperatorReleaseProviderArtifactIdentity{},
		},
		types.OperatorReleaseGraphEdge{
			From: "component:worker", To: "component:worker", Kind: "worker.rpc",
			ConsumerComponent: "worker", ProviderComponent: "worker", Capability: "worker.rpc",
			VersionRange: "^2.0.0", ProviderVersion: "2.0.0",
			ProviderArtifacts: []types.OperatorReleaseProviderArtifactIdentity{{
				ArtifactKey: "worker", ArtifactType: "oci-image",
				ManifestDigest: "sha256:abababababababababababababababababababababababababababababababab",
				Platform:       "linux/amd64", PlatformDigest: componentDigest,
			}},
			ResolutionStage: "product", AllowedModes: []string{},
			Ordering: "provider_deploy_and_health_before_consumer",
		},
	))
	g.Expect(detail.SourceBuildProof).To(ConsistOf(types.OperatorReleaseSourceBuildProof{
		Component: "worker", Schema: types.ReleaseContractSchemaV2,
		DeclaredRepository:   "https://example.invalid/worker.git",
		DeclaredRequestedRef: "refs/tags/2.0.0",
		DeclaredSourceCommit: "0123456789012345678901234567890123456789",
		DeclaredBuilderID:    "ci-provider", DeclaredBuildID: "build-200",
		VerifiedSourceURI:    "git+https://example.invalid/worker.git",
		VerifiedSourceCommit: "fedcba9876543210fedcba9876543210fedcba98",
		VerifiedBuilderID:    "verified-builder", VerifiedBuildID: "verified-build-200",
		VerifiedBuildType:   "https://slsa.dev/provenance/v1",
		ProvenanceReference: "oci://evidence.invalid/worker/provenance",
		ProvenanceDigest:    provenanceDigest,
		VerificationMode:    "keyful",
		KeyID:               "release-key-1",
		KeyFingerprint:      keyFingerprint,
		SBOMReference:       "oci://evidence.invalid/worker/sbom@" + sbomDigest,
		SBOMDigest:          sbomDigest,
		VerificationState:   "VERIFIED",
	}))
	g.Expect(detail.ChangeContext.State).To(Equal(types.OperatorReleaseChangeContextRequired))
	g.Expect(detail.Changelog).To(BeEmpty())
}

func TestOperatorReleaseDetailWithContextReadsHealthyObservedBaselineAndDivergence(t *testing.T) {
	ctx, _ := deploymentRegistryIsolatedPool(t, 159)
	g := NewWithT(t)
	organizationID, applicationID, channelID := createOperatorReleaseDependencies(t, ctx)
	registry := deploymentRegistryDependencies{
		organizationID:         organizationID,
		customerOrganizationID: createDeploymentRegistryCustomer(t, ctx, organizationID),
		environmentID:          createDeploymentRegistryEnvironment(t, ctx, organizationID),
		deploymentTargetID:     createDeploymentRegistryTarget(t, ctx, organizationID),
	}
	placement := createDeploymentRegistryPlacement(t, ctx, registry, "release-context", time.Now().UTC())
	siblingDefinition := types.ComponentDefinition{
		OrganizationID: organizationID, Key: "api-release-context",
		Name: "API release context", ManagementState: types.RegistryManagementStateManaged,
	}
	g.Expect(CreateComponentDefinition(ctx, &siblingDefinition)).To(Succeed())
	siblingInstance := types.ComponentInstance{
		OrganizationID: organizationID, DeploymentUnitID: placement.Unit.ID,
		ComponentDefinitionID: siblingDefinition.ID, PhysicalName: "api-release-context",
		ManagementState: types.RegistryManagementStateManaged,
	}
	g.Expect(CreateComponentInstance(ctx, &siblingInstance)).To(Succeed())
	actorID := createTargetConfigSnapshotTestUser(t, ctx, organizationID)
	configDraft := targetConfigSnapshotRepositoryFixture(placement, organizationID, actorID)
	config, err := CreateTargetConfigSnapshot(ctx, &configDraft)
	g.Expect(err).NotTo(HaveOccurred())

	decisionAt := time.Now().UTC()
	baselineReleaseID := insertOperatorReleaseFixture(
		t, ctx, organizationID, applicationID, channelID,
		types.ReleaseBundleKindComponent, "2.1.0", decisionAt.Add(-time.Hour),
	)
	selectedReleaseID := insertOperatorReleaseFixture(
		t, ctx, organizationID, applicationID, channelID,
		types.ReleaseBundleKindComponent, "2.2.0", decisionAt,
	)
	siblingBaselineReleaseID := insertOperatorReleaseFixture(
		t, ctx, organizationID, applicationID, channelID,
		types.ReleaseBundleKindComponent, "3.1.0", decisionAt.Add(-time.Hour),
	)
	siblingSelectedReleaseID := insertOperatorReleaseFixture(
		t, ctx, organizationID, applicationID, channelID,
		types.ReleaseBundleKindComponent, "3.2.0", decisionAt,
	)
	productReleaseID := insertOperatorReleaseFixture(
		t, ctx, organizationID, applicationID, channelID,
		types.ReleaseBundleKindProduct, "2026.07.29.1", decisionAt.Add(time.Second),
	)
	planID, draftID := uuid.New(), uuid.New()
	stateID, observationID := uuid.New(), uuid.New()
	siblingStateID, siblingObservationID := uuid.New(), uuid.New()
	stateChecksum := "sha256:" + strings.Repeat("5", 64)
	observationChecksum := "sha256:" + strings.Repeat("6", 64)
	siblingStateChecksum := "sha256:" + strings.Repeat("c", 64)
	siblingObservationChecksum := "sha256:" + strings.Repeat("d", 64)
	payload := []byte(`{}`)
	_, err = internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO ProductReleaseComponent (
		  product_release_bundle_id, organization_id, component_release_bundle_id,
		  component_release_checksum, component_key, component_version, contract_snapshot
		) VALUES
		  (@productReleaseID, @organizationID, @releaseID,
		   'sha256:' || repeat('a', 64), @component, '2.2.0',
		   jsonb_build_object('schema', 'distr.component-release/v2')),
		  (@productReleaseID, @organizationID, @siblingReleaseID,
		   'sha256:' || repeat('b', 64), @siblingComponent, '3.2.0',
		   jsonb_build_object('schema', 'distr.component-release/v2'));
		INSERT INTO DeploymentPlanDraft (
		  id, organization_id, created_by_user_account_id, updated_by_user_account_id,
		  product_release_id, deployment_unit_id, environment_assignment_id,
		  target_config_snapshot_id, protocol_version
		) VALUES (
		  @draftID, @organizationID, @actorID, @actorID, @productReleaseID,
		  @unitID, @assignmentID, @configID, 'v2'
		);
		INSERT INTO DeploymentPlan (
		  id, organization_id, release_bundle_id, application_id, channel_id,
		  environment_id, status, canonical_checksum, canonical_payload,
		  published_by_user_account_id, plan_schema, draft_id, deployment_unit_id,
		  target_config_snapshot_id, protocol_version
		) VALUES (
		  @planID, @organizationID, @productReleaseID, @applicationID, @channelID,
		  @environmentID, 'BLOCKED', 'sha256:' || encode(sha256(@payload), 'hex'), @payload,
		  @actorID, 'distr.target-deployment-plan/v2', @draftID, @unitID, @configID, 'v2'
		);
		INSERT INTO TargetComponentState (
		  id, organization_id, deployment_target_id, application_id, component,
		  state_version, state_checksum, release_bundle_id, version, image,
		  platform, config_checksum, health
		) VALUES
		(
		  @stateID, @organizationID, @targetID, @applicationID, @component,
		  7, @stateChecksum, @baselineReleaseID, '2.1.0', @image,
		  'linux/amd64', @configChecksum, 'HEALTHY'
		), (
		  @siblingStateID, @organizationID, @targetID, @applicationID, @siblingComponent,
		  8, @siblingStateChecksum, @siblingBaselineReleaseID, '3.1.0', @siblingImage,
		  'linux/amd64', @configChecksum, 'HEALTHY'
		);
		INSERT INTO TargetComponentObservation (
		  id, organization_id, target_component_state_id, deployment_target_id,
		  application_id, component, component_instance_id, state_version,
		  state_checksum, release_bundle_id, version, image, platform,
		  config_checksum, health
		) VALUES
		(
		  @observationID, @organizationID, @stateID, @targetID,
		  @applicationID, @component, @componentInstanceID, 7,
		  @stateChecksum, @baselineReleaseID, '2.1.0', @image, 'linux/amd64',
		  @configChecksum, 'HEALTHY'
		), (
		  @siblingObservationID, @organizationID, @siblingStateID, @targetID,
		  @applicationID, @siblingComponent, @siblingComponentInstanceID, 8,
		  @siblingStateChecksum, @siblingBaselineReleaseID, '3.1.0', @siblingImage, 'linux/amd64',
		  @configChecksum, 'HEALTHY'
		);
		INSERT INTO DeploymentPlanBaseline (
		  deployment_plan_id, organization_id, component_instance_id, component_key,
		  observation_id, observed_at, desired_revision, desired_checksum,
		  observation_checksum, release_bundle_id, version, image, platform,
		  target_config_snapshot_id, config_checksum, projection,
		  authorizes_v2_execution, bootstrap, actor_user_account_id,
		  canonical_checksum, sort_order
		) VALUES
		(
		  @planID, @organizationID, @componentInstanceID, @component,
		  @observationID, now(), 7, @stateChecksum,
		  @observationChecksum, @baselineReleaseID, '2.1.0', @image, 'linux/amd64',
		  @configID, @configChecksum, 'legacy_projection',
		  false, false, @actorID, 'sha256:' || repeat('7', 64), 0
		), (
		  @planID, @organizationID, @siblingComponentInstanceID, @siblingComponent,
		  @siblingObservationID, now(), 8, @siblingStateChecksum,
		  @siblingObservationChecksum, @siblingBaselineReleaseID, '3.1.0', @siblingImage, 'linux/amd64',
		  @configID, @configChecksum, 'legacy_projection',
		  false, false, @actorID, 'sha256:' || repeat('e', 64), 1
		);
		INSERT INTO DeploymentPlanChangeEntry (
		  deployment_plan_id, organization_id, component_instance_id, component_key,
		  kind, before_value, after_value, release_notes, actor_user_account_id,
		  canonical_checksum, sort_order
		) VALUES
		(
		  @planID, @organizationID, @componentInstanceID, @component,
		  'source_notes', @baselineReleaseID::text, @releaseID::text,
		  jsonb_build_array(jsonb_build_object(
		    'releaseBundleId', @releaseID, 'version', '2.2.0',
		    'publishedAt', now(), 'sourceRevision', @sourceRevision,
		    'summary', 'healthy baseline to selected release'
		  )),
		  @actorID, 'sha256:' || repeat('8', 64), 0
		), (
		  @planID, @organizationID, @siblingComponentInstanceID, @siblingComponent,
		  'source_notes', @siblingBaselineReleaseID::text, @siblingReleaseID::text,
		  jsonb_build_array(jsonb_build_object(
		    'releaseBundleId', @siblingReleaseID, 'version', '3.2.0',
		    'publishedAt', now(), 'sourceRevision', @siblingSourceRevision,
		    'summary', 'unrelated sibling release'
		  )),
		  @actorID, 'sha256:' || repeat('f', 64), 1
		)`, pgx.NamedArgs{
		"draftID": draftID, "planID": planID, "organizationID": organizationID,
		"actorID": actorID, "releaseID": selectedReleaseID, "baselineReleaseID": baselineReleaseID,
		"productReleaseID": productReleaseID, "siblingReleaseID": siblingSelectedReleaseID,
		"siblingBaselineReleaseID": siblingBaselineReleaseID,
		"unitID":                   placement.Unit.ID, "assignmentID": placement.Assignment.ID, "configID": config.ID,
		"applicationID": applicationID, "channelID": channelID, "environmentID": registry.environmentID,
		"targetID": registry.deploymentTargetID, "component": placement.Definitions[0].Key,
		"componentInstanceID": placement.Instances[0].ID, "stateID": stateID, "observationID": observationID,
		"siblingComponent": siblingDefinition.Key, "siblingComponentInstanceID": siblingInstance.ID,
		"siblingStateID": siblingStateID, "siblingObservationID": siblingObservationID,
		"stateChecksum": stateChecksum, "observationChecksum": observationChecksum,
		"siblingStateChecksum": siblingStateChecksum, "siblingObservationChecksum": siblingObservationChecksum,
		"configChecksum": config.CanonicalChecksum, "image": "registry.example/worker@sha256:" + strings.Repeat("9", 64),
		"siblingImage":   "registry.example/api@sha256:" + strings.Repeat("b", 64),
		"sourceRevision": strings.Repeat("a", 40), "siblingSourceRevision": strings.Repeat("b", 40),
		"payload": payload,
	})
	g.Expect(err).NotTo(HaveOccurred())

	detail, err := GetOperatorReleaseDetailWithContext(
		ctx,
		operatorOrganizationWideScope(organizationID, decisionAt),
		selectedReleaseID,
		types.OperatorReleaseDetailContext{DeploymentUnitID: &placement.Unit.ID},
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(detail.ChangeContext.State).To(Equal(types.OperatorReleaseChangeContextReady))
	g.Expect(detail.Changelog).To(ConsistOf(types.OperatorReleaseChange{
		Category: "code", Component: placement.Definitions[0].Key,
		Summary: "healthy baseline to selected release", Reference: selectedReleaseID.String(),
	}))

	_, err = internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO DeploymentPlanChangeEntry (
		  deployment_plan_id, organization_id, component_key, kind,
		  before_value, after_value, actor_user_account_id, canonical_checksum, sort_order
		) VALUES (
		  @planID, @organizationID, @component, 'planning_limit_exceeded',
		  '', '', @actorID, 'sha256:' || repeat('b', 64), 2
		)`, pgx.NamedArgs{
		"planID": planID, "organizationID": organizationID,
		"component": placement.Definitions[0].Key, "actorID": actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	detail, err = GetOperatorReleaseDetailWithContext(
		ctx, operatorOrganizationWideScope(organizationID, decisionAt),
		selectedReleaseID, types.OperatorReleaseDetailContext{DeploymentUnitID: &placement.Unit.ID},
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(detail.ChangeContext.State).To(Equal(types.OperatorReleaseChangeContextDivergentHistory))
	g.Expect(detail.Changelog).To(BeEmpty())
}

func TestBuildOperatorReleaseChangelogUsesPersistedVerifiedBaselineChanges(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	planID := uuid.New()
	unitID := uuid.New()
	baselineReleaseID := uuid.New()
	intermediateReleaseID := uuid.New()
	selectedReleaseID := uuid.New()

	result := buildOperatorReleaseChangelog(operatorReleaseChangeContextSource{
		DeploymentPlanID: planID,
		DeploymentUnitID: unitID,
		PlannedComponents: []operatorReleasePlannedComponent{{
			ComponentKey: "worker", ReleaseBundleID: selectedReleaseID,
		}},
		Baselines: []operatorReleaseBaselineSource{{
			ComponentKey: "worker", ReleaseBundleID: &baselineReleaseID,
			ObservationID: ptrUUID(uuid.New()), ObservationChecksum: "sha256:" + strings.Repeat("a", 64),
			IndependentlyHealthy: true,
		}},
		Changes: []types.DeploymentPlanChangeEntry{{
			ComponentKey: "worker", Kind: types.DeploymentPlanChangeSourceNotes,
			ReleaseNotes: []types.ReleaseNote{
				{ReleaseBundleID: intermediateReleaseID, Version: "2.1.0", SourceRevision: strings.Repeat("b", 40), Summary: "intermediate"},
				{ReleaseBundleID: selectedReleaseID, Version: "2.2.0", SourceRevision: strings.Repeat("c", 40), Summary: "selected"},
			},
		}, {
			ComponentKey: "worker", Kind: types.DeploymentPlanChangeConfig,
			Before: "sha256:" + strings.Repeat("d", 64), After: "sha256:" + strings.Repeat("e", 64),
		}},
	}, selectedReleaseID)

	g.Expect(result.Context.State).To(Equal(types.OperatorReleaseChangeContextReady))
	g.Expect(result.Changelog).To(ContainElements(
		types.OperatorReleaseChange{Category: "code", Component: "worker", Summary: "intermediate", Reference: intermediateReleaseID.String()},
		types.OperatorReleaseChange{Category: "code", Component: "worker", Summary: "selected", Reference: selectedReleaseID.String()},
		types.OperatorReleaseChange{Category: "config", Component: "worker", Summary: "configuration changed", Reference: "sha256:" + strings.Repeat("e", 64)},
	))
	g.Expect(result.SkippedReleases).To(ConsistOf(types.OperatorReleaseSkippedRelease{
		Component: "worker", ReleaseID: intermediateReleaseID, Version: "2.1.0",
		SourceRevision: strings.Repeat("b", 40), Summary: "intermediate",
	}))
}

func TestBuildOperatorReleaseChangelogIgnoresUnrelatedProductPlanComponents(t *testing.T) {
	t.Parallel()
	selectedReleaseID := uuid.New()
	selectedBaselineReleaseID := uuid.New()
	siblingPlannedReleaseID := uuid.New()
	siblingBaselineReleaseID := uuid.New()

	for name, siblingChanges := range map[string][]types.DeploymentPlanChangeEntry{
		"unchanged sibling": nil,
		"changed sibling": {{
			ComponentKey: "api", Kind: types.DeploymentPlanChangeSourceNotes,
			ReleaseNotes: []types.ReleaseNote{{
				ReleaseBundleID: siblingPlannedReleaseID,
				Version:         "4.0.0",
				SourceRevision:  strings.Repeat("d", 40),
				Summary:         "unrelated API change",
			}},
		}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			siblingBaselineID := siblingBaselineReleaseID
			if name == "unchanged sibling" {
				siblingBaselineID = siblingPlannedReleaseID
			}
			changes := []types.DeploymentPlanChangeEntry{{
				ComponentKey: "worker", Kind: types.DeploymentPlanChangeSourceNotes,
				ReleaseNotes: []types.ReleaseNote{{
					ReleaseBundleID: selectedReleaseID,
					Version:         "3.0.0",
					SourceRevision:  strings.Repeat("c", 40),
					Summary:         "selected worker change",
				}},
			}}
			changes = append(changes, siblingChanges...)

			result := buildOperatorReleaseChangelog(operatorReleaseChangeContextSource{
				DeploymentPlanID: uuid.New(),
				DeploymentUnitID: uuid.New(),
				PlannedComponents: []operatorReleasePlannedComponent{{
					ComponentKey: "worker", ReleaseBundleID: selectedReleaseID,
				}},
				Baselines: []operatorReleaseBaselineSource{{
					ComponentKey: "worker", ReleaseBundleID: &selectedBaselineReleaseID,
					ObservationID: ptrUUID(uuid.New()), ObservationChecksum: "sha256:" + strings.Repeat("a", 64),
					IndependentlyHealthy: true,
				}, {
					ComponentKey: "api", ReleaseBundleID: &siblingBaselineID,
					ObservationID: ptrUUID(uuid.New()), ObservationChecksum: "sha256:" + strings.Repeat("b", 64),
					IndependentlyHealthy: true,
				}},
				Changes: changes,
			}, selectedReleaseID)

			g := NewWithT(t)
			g.Expect(result.Context.State).To(Equal(types.OperatorReleaseChangeContextReady))
			g.Expect(result.Changelog).To(ConsistOf(types.OperatorReleaseChange{
				Category: "code", Component: "worker",
				Summary: "selected worker change", Reference: selectedReleaseID.String(),
			}))
			g.Expect(result.SkippedReleases).To(BeEmpty())
		})
	}
}

func TestBuildOperatorReleaseChangelogValidatesEachProductComponentLineage(t *testing.T) {
	t.Parallel()
	workerReleaseID := uuid.New()
	apiReleaseID := uuid.New()

	for name, apiLastReleaseID := range map[string]uuid.UUID{
		"exact component lineage":   apiReleaseID,
		"crossed component lineage": workerReleaseID,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			workerBaselineReleaseID := uuid.New()
			apiBaselineReleaseID := uuid.New()
			result := buildOperatorReleaseChangelog(operatorReleaseChangeContextSource{
				DeploymentPlanID: uuid.New(),
				DeploymentUnitID: uuid.New(),
				PlannedComponents: []operatorReleasePlannedComponent{
					{ComponentKey: "worker", ReleaseBundleID: workerReleaseID},
					{ComponentKey: "api", ReleaseBundleID: apiReleaseID},
				},
				Baselines: []operatorReleaseBaselineSource{{
					ComponentKey: "worker", ReleaseBundleID: &workerBaselineReleaseID,
					ObservationID: ptrUUID(uuid.New()), ObservationChecksum: "sha256:" + strings.Repeat("a", 64),
					IndependentlyHealthy: true,
				}, {
					ComponentKey: "api", ReleaseBundleID: &apiBaselineReleaseID,
					ObservationID: ptrUUID(uuid.New()), ObservationChecksum: "sha256:" + strings.Repeat("b", 64),
					IndependentlyHealthy: true,
				}},
				Changes: []types.DeploymentPlanChangeEntry{{
					ComponentKey: "worker", Kind: types.DeploymentPlanChangeSourceNotes,
					ReleaseNotes: []types.ReleaseNote{{
						ReleaseBundleID: workerReleaseID, Version: "3.0.0",
						SourceRevision: strings.Repeat("c", 40), Summary: "worker change",
					}},
				}, {
					ComponentKey: "api", Kind: types.DeploymentPlanChangeSourceNotes,
					ReleaseNotes: []types.ReleaseNote{{
						ReleaseBundleID: apiLastReleaseID, Version: "4.0.0",
						SourceRevision: strings.Repeat("d", 40), Summary: "API change",
					}},
				}},
			}, uuid.New())

			g := NewWithT(t)
			if name == "exact component lineage" {
				g.Expect(result.Context.State).To(Equal(types.OperatorReleaseChangeContextReady))
				g.Expect(result.Changelog).To(HaveLen(2))
				return
			}
			g.Expect(result.Context.State).To(Equal(types.OperatorReleaseChangeContextDivergentHistory))
			g.Expect(result.Changelog).To(BeEmpty())
		})
	}
}

func TestBuildOperatorReleaseChangelogStopsOnDivergentHistory(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	selectedReleaseID := uuid.New()
	baselineReleaseID := uuid.New()

	result := buildOperatorReleaseChangelog(operatorReleaseChangeContextSource{
		DeploymentPlanID: uuid.New(),
		DeploymentUnitID: uuid.New(),
		PlannedComponents: []operatorReleasePlannedComponent{{
			ComponentKey: "worker", ReleaseBundleID: selectedReleaseID,
		}},
		Baselines: []operatorReleaseBaselineSource{{
			ComponentKey: "worker", ReleaseBundleID: &baselineReleaseID,
			ObservationID: ptrUUID(uuid.New()), ObservationChecksum: "sha256:" + strings.Repeat("a", 64),
			IndependentlyHealthy: true,
		}},
		Changes: []types.DeploymentPlanChangeEntry{{
			ComponentKey: "worker", Kind: types.DeploymentPlanChangeLimitExceeded,
		}},
	}, selectedReleaseID)

	g.Expect(result.Context.State).To(Equal(types.OperatorReleaseChangeContextDivergentHistory))
	g.Expect(result.Changelog).To(BeEmpty())
	g.Expect(result.SkippedReleases).To(BeEmpty())
}

func TestOperatorReleaseDetailSQLUsesQualifiedProviderAndVerifiedEvidence(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	for _, required := range []string{
		"'providerArtifacts'",
		"'artifactKey', artifact.artifact_key",
		"'platform', artifact.platform",
		"'verifiedSourceCommit', verification.source_commit",
		"'verifiedBuilderId', verification.builder_id",
		"'verifiedBuildId', verification.build_id",
		"'verifiedSourceUri', verification.source_uri",
		"'verificationMode'",
		"'trustRootId'",
		"'keyId'",
		"'keyFingerprint'",
		"verification.signer_issuer LIKE 'keyid:%'",
		"evidence.evidence_type = 'sbom'",
		"'changeContextSource'",
		"DeploymentPlanBaseline",
		"DeploymentPlanChangeEntry",
		"@deploymentUnitID",
	} {
		g.Expect(operatorReleaseDetailSQL).To(ContainSubstring(required))
	}
	g.Expect(operatorReleaseDetailSQL).NotTo(ContainSubstring("'skippedReleases', '[]'::jsonb"))
	g.Expect(operatorReleaseDetailSQL).NotTo(ContainSubstring("'sbomDigest', ''"))
}

func TestImmutableEvidenceDigestAcceptsEveryValidatedImmutableReferenceForm(t *testing.T) {
	t.Parallel()
	digest := "sha256:" + strings.Repeat("a", 64)
	for name, test := range map[string]struct {
		reference string
		want      string
	}{
		"OCI digest": {
			reference: "oci://evidence.example.invalid/team/sbom@" + digest,
			want:      digest,
		},
		"HTTPS digest path with trailing object": {
			reference: "https://evidence.example.invalid/team/sha256/" + strings.Repeat("a", 64) + "/sbom.spdx.json",
			want:      digest,
		},
		"HTTPS terminal filename digest": {
			reference: "https://evidence.example.invalid/team/sbom.spdx.json@" + digest,
			want:      digest,
		},
		"mutable HTTPS reference": {
			reference: "https://evidence.example.invalid/team/sbom.spdx.json",
			want:      "",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			NewWithT(t).Expect(immutableEvidenceDigest(test.reference)).To(Equal(test.want))
		})
	}
}

func ptrUUID(value uuid.UUID) *uuid.UUID {
	return &value
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
