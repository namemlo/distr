package db

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/governance"
	"github.com/distr-sh/distr/internal/productrelease"
	"github.com/distr-sh/distr/internal/releasebundles"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type productReleaseEvidenceFact struct {
	Type      string `db:"evidence_type"`
	Reference string `db:"reference"`
}

type productReleaseComponentEligibilityRecord struct {
	Bundle        types.ReleaseBundle
	Evidence      []productReleaseEvidenceFact
	Verifications []types.EvidenceVerification
}

type productReleaseEligibilitySource interface {
	LoadProductReleaseComponentEligibility(
		context.Context,
		uuid.UUID,
		uuid.UUID,
	) (productReleaseComponentEligibilityRecord, error)
	LoadProductReleaseDependencyPolicy(
		context.Context,
		uuid.UUID,
		uuid.UUID,
	) (types.DeploymentPolicyVersion, error)
}

type databaseProductReleaseEligibilitySource struct{}

func (databaseProductReleaseEligibilitySource) LoadProductReleaseComponentEligibility(
	ctx context.Context,
	organizationID uuid.UUID,
	componentReleaseID uuid.UUID,
) (productReleaseComponentEligibilityRecord, error) {
	bundle, err := GetReleaseBundle(ctx, componentReleaseID, organizationID)
	if err != nil {
		return productReleaseComponentEligibilityRecord{}, err
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT evidence_type, reference
		FROM ComponentReleaseEvidence
		WHERE organization_id = @organizationID
		  AND release_bundle_id = @componentReleaseID
		ORDER BY evidence_type, reference`,
		pgx.NamedArgs{
			"organizationID":     organizationID,
			"componentReleaseID": componentReleaseID,
		},
	)
	if err != nil {
		return productReleaseComponentEligibilityRecord{},
			fmt.Errorf("query Product Release component evidence: %w", err)
	}
	evidence, err := pgx.CollectRows(
		rows,
		pgx.RowToStructByName[productReleaseEvidenceFact],
	)
	if err != nil {
		return productReleaseComponentEligibilityRecord{},
			fmt.Errorf("collect Product Release component evidence: %w", err)
	}
	verifications, err := GetComponentReleaseEvidenceVerifications(
		ctx,
		componentReleaseID,
		organizationID,
	)
	if err != nil {
		return productReleaseComponentEligibilityRecord{}, err
	}
	return productReleaseComponentEligibilityRecord{
		Bundle:        *bundle,
		Evidence:      evidence,
		Verifications: verifications,
	}, nil
}

func (databaseProductReleaseEligibilitySource) LoadProductReleaseDependencyPolicy(
	ctx context.Context,
	organizationID uuid.UUID,
	policyVersionID uuid.UUID,
) (types.DeploymentPolicyVersion, error) {
	version, err := GetDeploymentPolicyVersion(ctx, policyVersionID, organizationID)
	if err != nil {
		return types.DeploymentPolicyVersion{}, err
	}
	return *version, nil
}

type persistedProductReleaseEligibilityRepository struct {
	source productReleaseEligibilitySource
}

func (repository persistedProductReleaseEligibilityRepository) ValidateComponent(
	ctx context.Context,
	organizationID uuid.UUID,
	componentReleaseID uuid.UUID,
) (*types.ProductReleaseValidationIssue, error) {
	if repository.source == nil {
		return productReleaseProvenanceIssue(
			"provenanceVerifierUnavailable",
			"persisted component provenance verifier is unavailable",
		), nil
	}
	record, err := repository.source.LoadProductReleaseComponentEligibility(
		ctx,
		organizationID,
		componentReleaseID,
	)
	if errors.Is(err, apierrors.ErrNotFound) {
		return productReleaseProvenanceIssue(
			"publishedChild",
			"exact published component release is not available in this organization",
		), nil
	}
	if err != nil {
		return nil, err
	}
	bundle := record.Bundle
	if bundle.ID != componentReleaseID ||
		bundle.OrganizationID != organizationID ||
		bundle.Kind != types.ReleaseBundleKindComponent ||
		bundle.Status != types.ReleaseBundleStatusPublished ||
		bundle.PublishedAt == nil ||
		bundle.ReleaseContractSchema != types.ReleaseContractSchemaV2 ||
		bundle.ReleaseContract == nil ||
		bundle.ReleaseContract.ComponentV2 == nil {
		return productReleaseProvenanceIssue(
			"publishedChild",
			"exact published component release identity is not eligible",
		), nil
	}
	contract := *bundle.ReleaseContract.ComponentV2
	if len(releasebundles.ValidateComponentReleaseContractV2(contract)) != 0 {
		return productReleaseProvenanceIssue(
			"verifiedEvidence",
			"published component release contract is invalid",
		), nil
	}
	if !exactProductReleaseEvidenceFacts(contract.Evidence, record.Evidence) {
		return productReleaseProvenanceIssue(
			"verifiedEvidence",
			"exact persisted provenance and SBOM facts do not match the published component contract",
		), nil
	}
	if len(record.Verifications) == 0 {
		return productReleaseProvenanceIssue(
			"verifiedProvenance",
			"exact persisted provenance verification is missing",
		), nil
	}
	policyChecksum := record.Verifications[0].PolicyChecksum
	for _, verification := range record.Verifications {
		if verification.OrganizationID != organizationID ||
			verification.ReleaseBundleID != componentReleaseID ||
			verification.PolicyChecksum != policyChecksum ||
			verification.VerifiedAt.IsZero() ||
			verification.VerifiedAt.After(*bundle.PublishedAt) ||
			!slices.Contains(contract.Evidence.Provenance, verification.EvidenceReference) {
			return productReleaseProvenanceIssue(
				"verifiedProvenance",
				"exact persisted provenance verification does not match the published component",
			), nil
		}
	}
	artifacts := componentReleaseProvenanceArtifacts(contract)
	if result := releasebundles.ProvenancePreflight(
		artifacts,
		record.Verifications,
		policyChecksum,
	); !result.Valid {
		return productReleaseProvenanceIssue(
			"verifiedProvenance",
			"exact persisted provenance verification does not match the published component",
		), nil
	}
	return nil, nil
}

func (repository persistedProductReleaseEligibilityRepository) ValidateDependencyPolicy(
	ctx context.Context,
	organizationID uuid.UUID,
	manifest types.ProductReleaseManifest,
) (*types.ProductReleaseValidationIssue, error) {
	if repository.source == nil {
		return productReleasePolicyIssue(
			"publishedPolicyUnavailable",
			"persisted dependency policy verifier is unavailable",
		), nil
	}
	policy, err := repository.source.LoadProductReleaseDependencyPolicy(
		ctx,
		organizationID,
		manifest.DependencyPolicyVersion,
	)
	if errors.Is(err, apierrors.ErrNotFound) {
		return productReleasePolicyIssue(
			"publishedPolicy",
			"exact published immutable deployment policy is not available in this organization",
		), nil
	}
	if err != nil {
		return nil, err
	}
	if policy.ID != manifest.DependencyPolicyVersion ||
		policy.OrganizationID != organizationID ||
		policy.State != types.DeploymentPolicyVersionStatePublished ||
		policy.PublishedAt == nil ||
		policy.PublishedByUserAccountID == nil {
		return productReleasePolicyIssue(
			"publishedPolicy",
			"exact published immutable deployment policy is not eligible",
		), nil
	}
	if len(governance.ValidateDeploymentPolicyVersion(policy)) != 0 {
		return productReleasePolicyIssue(
			"frozenPolicy",
			"published dependency policy document is invalid",
		), nil
	}
	_, payload, checksum, err := governance.CanonicalizeDeploymentPolicyDocument(policy.Document)
	if err != nil {
		return nil, fmt.Errorf("canonicalize Product Release dependency policy: %w", err)
	}
	if checksum != policy.CanonicalChecksum || !bytes.Equal(payload, policy.CanonicalPayload) {
		return productReleasePolicyIssue(
			"frozenPolicy",
			"canonical policy facts do not match the published dependency policy",
		), nil
	}
	if manifest.DependencyPolicyChecksum != policy.CanonicalChecksum {
		return productReleasePolicyIssue(
			"dependencyPolicyChecksum",
			"dependency policy checksum does not match the exact published policy version",
		), nil
	}
	allowed := make(map[types.RequirementResolutionMode]struct{},
		len(policy.Document.AdmissionRules.AllowedResolutionModes))
	for _, mode := range policy.Document.AdmissionRules.AllowedResolutionModes {
		allowed[mode] = struct{}{}
	}
	graph := productrelease.BuildProductReleaseGraph(manifest)
	for _, edge := range graph.Edges {
		if edge.ResolutionStage != types.CapabilityResolutionStageTarget {
			continue
		}
		for _, mode := range edge.AllowedModes {
			if _, ok := allowed[mode]; !ok {
				return productReleasePolicyIssue(
					"dependencyPolicy",
					fmt.Sprintf(
						"target resolution mode %q is not allowed by the frozen dependency policy",
						mode,
					),
				), nil
			}
		}
	}
	return nil, nil
}

func exactProductReleaseEvidenceFacts(
	expected types.ComponentReleaseEvidenceReferences,
	actual []productReleaseEvidenceFact,
) bool {
	if len(expected.Provenance) == 0 || len(expected.SBOM) == 0 {
		return false
	}
	expectedByType := map[string][]string{
		"provenance": slices.Clone(expected.Provenance),
		"sbom":       slices.Clone(expected.SBOM),
		"signature":  slices.Clone(expected.Signatures),
		"test":       slices.Clone(expected.Tests),
	}
	actualByType := map[string][]string{
		"provenance": {},
		"sbom":       {},
		"signature":  {},
		"test":       {},
	}
	for _, fact := range actual {
		references, ok := actualByType[fact.Type]
		if !ok || strings.TrimSpace(fact.Reference) != fact.Reference {
			return false
		}
		actualByType[fact.Type] = append(references, fact.Reference)
	}
	for evidenceType, expectedReferences := range expectedByType {
		actualReferences := actualByType[evidenceType]
		slices.Sort(expectedReferences)
		slices.Sort(actualReferences)
		if !slices.Equal(expectedReferences, actualReferences) {
			return false
		}
	}
	return true
}

func componentReleaseProvenanceArtifacts(
	contract types.ComponentReleaseContractV2,
) []releasebundles.ProvenanceArtifact {
	artifacts := make([]releasebundles.ProvenanceArtifact, 0)
	for _, artifact := range contract.Artifacts {
		for _, platform := range artifact.Platforms {
			artifacts = append(artifacts, releasebundles.ProvenanceArtifact{
				Key:              artifact.Key,
				Platform:         platform.Platform,
				Digest:           platform.Digest,
				SourceRepository: contract.Source.Repository,
				SourceCommit:     contract.Source.Commit,
				BuildID:          contract.Build.ID,
				BuilderID:        contract.Build.Builder,
			})
		}
	}
	return artifacts
}

func productReleaseProvenanceIssue(
	rule string,
	message string,
) *types.ProductReleaseValidationIssue {
	return &types.ProductReleaseValidationIssue{
		Field:   "components",
		Rule:    rule,
		Message: message,
	}
}

func productReleasePolicyIssue(
	rule string,
	message string,
) *types.ProductReleaseValidationIssue {
	return &types.ProductReleaseValidationIssue{
		Field:   "dependencyPolicyVersion",
		Rule:    rule,
		Message: message,
	}
}

var productionProductReleaseEligibilityRepository = persistedProductReleaseEligibilityRepository{
	source: databaseProductReleaseEligibilitySource{},
}

func persistedProductReleaseProvenanceEligibility(
	ctx context.Context,
	organizationID uuid.UUID,
	componentReleaseID uuid.UUID,
) (*types.ProductReleaseValidationIssue, error) {
	return productionProductReleaseEligibilityRepository.ValidateComponent(
		ctx,
		organizationID,
		componentReleaseID,
	)
}

func persistedProductReleaseDependencyPolicyEligibility(
	ctx context.Context,
	organizationID uuid.UUID,
	manifest types.ProductReleaseManifest,
) (*types.ProductReleaseValidationIssue, error) {
	return productionProductReleaseEligibilityRepository.ValidateDependencyPolicy(
		ctx,
		organizationID,
		manifest,
	)
}
