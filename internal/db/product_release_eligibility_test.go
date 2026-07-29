package db

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/governance"
	"github.com/distr-sh/distr/internal/releasebundles"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	. "github.com/onsi/gomega"
)

type productReleaseEligibilitySourceStub struct {
	component productReleaseComponentEligibilityRecord
	policy    types.DeploymentPolicyVersion
}

func (source productReleaseEligibilitySourceStub) LoadProductReleaseComponentEligibility(
	context.Context,
	uuid.UUID,
	uuid.UUID,
) (productReleaseComponentEligibilityRecord, error) {
	return source.component, nil
}

func (source productReleaseEligibilitySourceStub) LoadProductReleaseDependencyPolicy(
	context.Context,
	uuid.UUID,
	uuid.UUID,
) (types.DeploymentPolicyVersion, error) {
	return source.policy, nil
}

func TestPersistedProductReleaseEligibilityAcceptsExactVerifiedPublishedFacts(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	componentReleaseID := uuid.New()
	manifest, source := productReleaseEligibilityTestMaterial(organizationID, componentReleaseID)
	repository := persistedProductReleaseEligibilityRepository{source: source}

	componentIssue, err := repository.ValidateComponent(
		context.Background(),
		organizationID,
		componentReleaseID,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(componentIssue).To(BeNil())

	policyIssue, err := repository.ValidateDependencyPolicy(
		context.Background(),
		organizationID,
		manifest,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(policyIssue).To(BeNil())
}

func TestPersistedProductReleaseEligibilityFailsClosedForMissingOrMismatchedFacts(t *testing.T) {
	organizationID := uuid.New()
	componentReleaseID := uuid.New()
	baseManifest, baseSource := productReleaseEligibilityTestMaterial(
		organizationID,
		componentReleaseID,
	)
	tests := []struct {
		name    string
		mutate  func(*types.ProductReleaseManifest, *productReleaseEligibilitySourceStub)
		check   func(persistedProductReleaseEligibilityRepository, types.ProductReleaseManifest) *types.ProductReleaseValidationIssue
		rule    string
		message string
	}{
		{
			name: "missing sbom fact",
			mutate: func(_ *types.ProductReleaseManifest, source *productReleaseEligibilitySourceStub) {
				source.component.Evidence = source.component.Evidence[:1]
			},
			check:   validateTestComponentEligibility,
			rule:    "verifiedEvidence",
			message: "exact persisted provenance and SBOM facts",
		},
		{
			name: "stale provenance verification",
			mutate: func(_ *types.ProductReleaseManifest, source *productReleaseEligibilitySourceStub) {
				source.component.Verifications[0].SourceCommit = strings.Repeat("9", 40)
			},
			check:   validateTestComponentEligibility,
			rule:    "verifiedProvenance",
			message: "exact persisted provenance verification",
		},
		{
			name: "cross organization component",
			mutate: func(_ *types.ProductReleaseManifest, source *productReleaseEligibilitySourceStub) {
				source.component.Bundle.OrganizationID = uuid.New()
			},
			check:   validateTestComponentEligibility,
			rule:    "publishedChild",
			message: "exact published component release",
		},
		{
			name: "draft policy",
			mutate: func(_ *types.ProductReleaseManifest, source *productReleaseEligibilitySourceStub) {
				source.policy.State = types.DeploymentPolicyVersionStateDraft
				source.policy.PublishedAt = nil
			},
			check:   validateTestPolicyEligibility,
			rule:    "publishedPolicy",
			message: "published immutable deployment policy",
		},
		{
			name: "stale canonical policy",
			mutate: func(_ *types.ProductReleaseManifest, source *productReleaseEligibilitySourceStub) {
				source.policy.CanonicalPayload = json.RawMessage(`{"stale":true}`)
			},
			check:   validateTestPolicyEligibility,
			rule:    "frozenPolicy",
			message: "canonical policy facts",
		},
		{
			name: "target mode forbidden by policy",
			mutate: func(manifest *types.ProductReleaseManifest, _ *productReleaseEligibilitySourceStub) {
				manifest.Requirements[0].AllowedModes = []string{
					string(types.RequirementResolutionModeApprovedExternal),
				}
			},
			check:   validateTestPolicyEligibility,
			rule:    "dependencyPolicy",
			message: "not allowed by the frozen dependency policy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest := baseManifest
			manifest.Requirements = append([]types.CapabilityRequirement(nil), baseManifest.Requirements...)
			source := baseSource
			source.component.Evidence = append(
				[]productReleaseEvidenceFact(nil),
				baseSource.component.Evidence...,
			)
			source.component.Verifications = append(
				[]types.EvidenceVerification(nil),
				baseSource.component.Verifications...,
			)
			tt.mutate(&manifest, &source)
			repository := persistedProductReleaseEligibilityRepository{source: source}

			issue := tt.check(repository, manifest)

			g := NewWithT(t)
			g.Expect(issue).NotTo(BeNil())
			g.Expect(issue.Rule).To(Equal(tt.rule))
			g.Expect(issue.Message).To(ContainSubstring(tt.message))
		})
	}
}

func validateTestComponentEligibility(
	repository persistedProductReleaseEligibilityRepository,
	manifest types.ProductReleaseManifest,
) *types.ProductReleaseValidationIssue {
	issue, _ := repository.ValidateComponent(
		context.Background(),
		manifest.OrganizationID,
		manifest.Components[0].ComponentReleaseID,
	)
	return issue
}

func validateTestPolicyEligibility(
	repository persistedProductReleaseEligibilityRepository,
	manifest types.ProductReleaseManifest,
) *types.ProductReleaseValidationIssue {
	issue, _ := repository.ValidateDependencyPolicy(
		context.Background(),
		manifest.OrganizationID,
		manifest,
	)
	return issue
}

func productReleaseEligibilityTestMaterial(
	organizationID uuid.UUID,
	componentReleaseID uuid.UUID,
) (types.ProductReleaseManifest, productReleaseEligibilitySourceStub) {
	contract := admissionComponentReleaseContract()
	contract.Requires = []types.CapabilityRequirement{{
		Name:            "external.database",
		Range:           "^1.0.0",
		ResolutionStage: string(types.CapabilityResolutionStageTarget),
		AllowedModes: []string{
			string(types.RequirementResolutionModeIncluded),
		},
	}}
	publishedAt := time.Date(2026, time.July, 29, 8, 0, 0, 0, time.UTC)
	publishedBy := uuid.New()
	policy := types.DeploymentPolicyVersion{
		ID:                       uuid.New(),
		OrganizationID:           organizationID,
		PolicyID:                 uuid.New(),
		VersionNumber:            1,
		State:                    types.DeploymentPolicyVersionStatePublished,
		CreatedByUserAccountID:   uuid.New(),
		PublishedByUserAccountID: &publishedBy,
		PublishedAt:              &publishedAt,
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
	normalizedPolicy, payload, checksum, err := governance.CanonicalizeDeploymentPolicyDocument(
		policy.Document,
	)
	if err != nil {
		panic(err)
	}
	policy.Document = normalizedPolicy
	policy.CanonicalPayload = payload
	policy.CanonicalChecksum = checksum

	policyChecksum := "sha256:" + strings.Repeat("f", 64)
	verification := types.EvidenceVerification{
		OrganizationID:             organizationID,
		ReleaseBundleID:            componentReleaseID,
		ArtifactKey:                contract.Artifacts[0].Key,
		Platform:                   contract.Artifacts[0].Platforms[0].Platform,
		ArtifactDigest:             contract.Artifacts[0].Platforms[0].Digest,
		EvidenceReference:          contract.Evidence.Provenance[0],
		EvidenceDigest:             "sha256:" + strings.Repeat("1", 64),
		PolicyChecksum:             policyChecksum,
		TrustRootID:                "production-root",
		PredicateType:              "https://slsa.dev/provenance/v1",
		BuilderID:                  contract.Build.Builder,
		BuildID:                    contract.Build.ID,
		SourceURI:                  contract.Source.Repository,
		SourceCommit:               contract.Source.Commit,
		BuildType:                  "https://build.example.invalid/types/container/v1",
		ExternalParametersChecksum: "sha256:" + strings.Repeat("2", 64),
		SignerIssuer:               "https://issuer.example.invalid",
		SignerIdentity:             "builder@example.invalid",
		VerifiedAt:                 publishedAt.Add(-time.Minute),
	}
	bundle := types.ReleaseBundle{
		ID:                    componentReleaseID,
		OrganizationID:        organizationID,
		Kind:                  types.ReleaseBundleKindComponent,
		ReleaseContractSchema: types.ReleaseContractSchemaV2,
		Status:                types.ReleaseBundleStatusPublished,
		PublishedAt:           &publishedAt,
		ReleaseContract:       &types.ReleaseContract{ComponentV2: &contract},
		CanonicalChecksum:     "sha256:" + strings.Repeat("a", 64),
	}
	manifest := types.ProductReleaseManifest{
		Schema:                  types.ProductReleaseSchemaV1,
		OrganizationID:          organizationID,
		Product:                 "payments",
		Version:                 "1.0.0",
		DependencyPolicyVersion: policy.ID,
		Components: []types.ProductReleaseComponent{{
			OrganizationID:           organizationID,
			ComponentReleaseID:       componentReleaseID,
			ComponentReleaseChecksum: bundle.CanonicalChecksum,
			ComponentKey:             contract.ComponentKey,
			Version:                  contract.Version,
			Published:                true,
			Platforms:                []string{"linux/amd64"},
			Contract:                 &contract,
		}},
		Requirements: []types.CapabilityRequirement{{
			Name:            "external.database",
			Range:           "^1.0.0",
			ResolutionStage: string(types.CapabilityResolutionStageTarget),
			AllowedModes: []string{
				string(types.RequirementResolutionModeIncluded),
			},
		}},
	}
	return manifest, productReleaseEligibilitySourceStub{
		component: productReleaseComponentEligibilityRecord{
			Bundle: bundle,
			Evidence: []productReleaseEvidenceFact{
				{Type: "provenance", Reference: contract.Evidence.Provenance[0]},
				{Type: "sbom", Reference: contract.Evidence.SBOM[0]},
			},
			Verifications: []types.EvidenceVerification{verification},
		},
		policy: policy,
	}
}

func createProductReleaseEligibilityTestUser(
	t *testing.T,
	ctx context.Context,
	organizationID uuid.UUID,
) uuid.UUID {
	t.Helper()
	var userID uuid.UUID
	err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO UserAccount (email) VALUES (@email) RETURNING id`,
		pgx.NamedArgs{"email": "product-release-" + uuid.NewString() + "@example.com"},
	).Scan(&userID)
	if err != nil {
		t.Fatalf("create Product Release test user: %v", err)
	}
	_, err = internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO Organization_UserAccount (
			organization_id,
			user_account_id,
			user_role
		) VALUES (
			@organizationID,
			@userID,
			'admin'
		)`,
		pgx.NamedArgs{"organizationID": organizationID, "userID": userID},
	)
	if err != nil {
		t.Fatalf("attach Product Release test user: %v", err)
	}
	return userID
}

func productReleasePublicationComponentFixture(
	organizationID uuid.UUID,
	applicationID uuid.UUID,
	channelID uuid.UUID,
) (
	types.ReleaseBundle,
	*releasebundles.PublicationProvenance,
	releasebundles.ProvenanceVerifier,
) {
	contract := admissionComponentReleaseContract()
	component := types.ReleaseBundle{
		OrganizationID:        organizationID,
		ApplicationID:         applicationID,
		ChannelID:             channelID,
		ReleaseNumber:         contract.Version,
		ReleaseNotes:          "Product Release production eligibility fixture",
		Kind:                  types.ReleaseBundleKindComponent,
		ReleaseContractSchema: types.ReleaseContractSchemaV2,
		ReleaseContract: &types.ReleaseContract{
			Schema:      types.ReleaseContractSchemaV2,
			ComponentV2: &contract,
		},
		Components: []types.ReleaseBundleComponent{{
			Key:        contract.Artifacts[0].Key,
			Name:       contract.Artifacts[0].Key,
			Type:       types.ReleaseBundleComponentTypeOCIImage,
			Version:    contract.Version,
			PackageRef: "registry.example.invalid/payments/api",
			Digest:     contract.Artifacts[0].Digest,
		}},
	}
	publication := &releasebundles.PublicationProvenance{
		Evidence: []releasebundles.PublicationProvenanceEvidence{{
			ArtifactKey: contract.Artifacts[0].Key,
			Platform:    contract.Artifacts[0].Platforms[0].Platform,
			Evidence: releasebundles.ComponentReleaseEvidence{
				Reference:   contract.Evidence.Provenance[0],
				TrustRootID: "product-release-root",
			},
		}},
	}
	verifier := productReleaseEligibilityProvenanceVerifierFunc(func(
		_ context.Context,
		_ releasebundles.ProvenancePolicy,
		artifact releasebundles.ProvenanceArtifact,
		evidence releasebundles.ComponentReleaseEvidence,
	) (releasebundles.ProvenanceVerificationResult, error) {
		return releasebundles.ProvenanceVerificationResult{
			EvidenceDigest:             "sha256:" + strings.Repeat("1", 64),
			PolicyChecksum:             "sha256:" + strings.Repeat("2", 64),
			TrustRootID:                evidence.TrustRootID,
			PredicateType:              "https://slsa.dev/provenance/v1",
			BuilderID:                  artifact.BuilderID,
			BuildID:                    artifact.BuildID,
			SourceURI:                  artifact.SourceRepository,
			SourceCommit:               artifact.SourceCommit,
			BuildType:                  "https://build.example.invalid/types/container/v1",
			ExternalParametersChecksum: "sha256:" + strings.Repeat("3", 64),
			SignerIssuer:               "https://issuer.example.invalid",
			SignerIdentity:             "builder@example.invalid",
			VerifiedAt:                 time.Date(2026, time.July, 29, 7, 0, 0, 0, time.UTC),
		}, nil
	})
	return component, publication, verifier
}

type productReleaseEligibilityProvenanceVerifierFunc func(
	context.Context,
	releasebundles.ProvenancePolicy,
	releasebundles.ProvenanceArtifact,
	releasebundles.ComponentReleaseEvidence,
) (releasebundles.ProvenanceVerificationResult, error)

func (verify productReleaseEligibilityProvenanceVerifierFunc) Verify(
	ctx context.Context,
	policy releasebundles.ProvenancePolicy,
	artifact releasebundles.ProvenanceArtifact,
	evidence releasebundles.ComponentReleaseEvidence,
) (releasebundles.ProvenanceVerificationResult, error) {
	return verify(ctx, policy, artifact, evidence)
}
