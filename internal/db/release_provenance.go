package db

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/releasebundles"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const evidenceVerificationOutputExpr = `
	id,
	created_at,
	organization_id,
	release_bundle_id,
	artifact_key,
	platform,
	artifact_digest,
	evidence_reference,
	evidence_digest,
	policy_checksum,
	trust_root_id,
	predicate_type,
	builder_id,
	build_id,
	source_uri,
	source_commit,
	build_type,
	external_parameters_checksum,
	signer_issuer,
	signer_identity,
	verified_at
`

func RecordComponentReleaseEvidenceVerification(
	ctx context.Context,
	verification types.EvidenceVerification,
) error {
	return RunTx(ctx, func(ctx context.Context) error {
		return recordComponentReleaseEvidenceVerifications(ctx, []types.EvidenceVerification{verification})
	})
}

func recordComponentReleaseEvidenceVerifications(
	ctx context.Context,
	verifications []types.EvidenceVerification,
) error {
	if len(verifications) == 0 {
		return nil
	}
	db := internalctx.GetDb(ctx)
	for _, verification := range verifications {
		verification.VerifiedAt = verification.VerifiedAt.UTC().Truncate(time.Microsecond)
		if err := validateEvidenceVerification(verification); err != nil {
			return err
		}
		command, err := db.Exec(ctx, `
			INSERT INTO ComponentReleaseEvidenceVerification (
				organization_id,
				release_bundle_id,
				artifact_key,
				platform,
				artifact_digest,
				evidence_reference,
				evidence_digest,
				policy_checksum,
				trust_root_id,
				predicate_type,
				builder_id,
				build_id,
				source_uri,
				source_commit,
				build_type,
				external_parameters_checksum,
				signer_issuer,
				signer_identity,
				verified_at
			) VALUES (
				@organizationId,
				@releaseBundleId,
				@artifactKey,
				@platform,
				@artifactDigest,
				@evidenceReference,
				@evidenceDigest,
				@policyChecksum,
				@trustRootId,
				@predicateType,
				@builderId,
				@buildId,
				@sourceUri,
				@sourceCommit,
				@buildType,
				@externalParametersChecksum,
				@signerIssuer,
				@signerIdentity,
				@verifiedAt
			)
			ON CONFLICT (
				release_bundle_id,
				artifact_key,
				platform
			) DO NOTHING`,
			pgx.NamedArgs{
				"organizationId":             verification.OrganizationID,
				"releaseBundleId":            verification.ReleaseBundleID,
				"artifactKey":                verification.ArtifactKey,
				"platform":                   verification.Platform,
				"artifactDigest":             verification.ArtifactDigest,
				"evidenceReference":          verification.EvidenceReference,
				"evidenceDigest":             verification.EvidenceDigest,
				"policyChecksum":             verification.PolicyChecksum,
				"trustRootId":                verification.TrustRootID,
				"predicateType":              verification.PredicateType,
				"builderId":                  verification.BuilderID,
				"buildId":                    verification.BuildID,
				"sourceUri":                  verification.SourceURI,
				"sourceCommit":               verification.SourceCommit,
				"buildType":                  verification.BuildType,
				"externalParametersChecksum": verification.ExternalParametersChecksum,
				"signerIssuer":               verification.SignerIssuer,
				"signerIdentity":             verification.SignerIdentity,
				"verifiedAt":                 verification.VerifiedAt.UTC(),
			},
		)
		if err != nil {
			return fmt.Errorf("could not record bounded Component Release provenance verification: %w", err)
		}
		if command.RowsAffected() == 0 {
			existing, err := getMatchingEvidenceVerification(ctx, verification)
			if err != nil {
				return err
			}
			if !sameEvidenceVerification(existing, verification) {
				return fmt.Errorf("could not record Component Release provenance verification: %w", apierrors.ErrConflict)
			}
		}
	}
	return nil
}

func GetComponentReleaseEvidenceVerifications(
	ctx context.Context,
	releaseBundleID uuid.UUID,
	organizationID uuid.UUID,
) ([]types.EvidenceVerification, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx, `
		SELECT `+evidenceVerificationOutputExpr+`
		FROM ComponentReleaseEvidenceVerification
		WHERE release_bundle_id = @releaseBundleId
		  AND organization_id = @organizationId
		ORDER BY artifact_key, platform, evidence_digest, policy_checksum, id`,
		pgx.NamedArgs{"releaseBundleId": releaseBundleID, "organizationId": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query Component Release provenance verification facts: %w", err)
	}
	verifications, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.EvidenceVerification])
	if err != nil {
		return nil, fmt.Errorf("could not collect Component Release provenance verification facts: %w", err)
	}
	return verifications, nil
}

func ComponentReleaseProvenancePreflight(
	ctx context.Context,
	releaseBundleID uuid.UUID,
	organizationID uuid.UUID,
	policyChecksum string,
) (releasebundles.ValidationResult, error) {
	bundle, err := GetReleaseBundle(ctx, releaseBundleID, organizationID)
	if err != nil {
		return releasebundles.ValidationResult{}, err
	}
	verifications, err := GetComponentReleaseEvidenceVerifications(ctx, releaseBundleID, organizationID)
	if err != nil {
		return releasebundles.ValidationResult{}, err
	}
	artifacts := make([]releasebundles.ProvenanceArtifact, 0)
	if bundle.ReleaseContract != nil && bundle.ReleaseContract.ComponentV2 != nil {
		for _, artifact := range bundle.ReleaseContract.ComponentV2.Artifacts {
			for _, platform := range artifact.Platforms {
				artifacts = append(artifacts, releasebundles.ProvenanceArtifact{
					Key:              artifact.Key,
					Platform:         platform.Platform,
					Digest:           platform.Digest,
					SourceRepository: bundle.ReleaseContract.ComponentV2.Source.Repository,
					SourceCommit:     bundle.ReleaseContract.ComponentV2.Source.Commit,
					BuildID:          bundle.ReleaseContract.ComponentV2.Build.ID,
					BuilderID:        bundle.ReleaseContract.ComponentV2.Build.Builder,
				})
			}
		}
	}
	return releasebundles.ProvenancePreflight(artifacts, verifications, policyChecksum), nil
}

func getMatchingEvidenceVerification(
	ctx context.Context,
	verification types.EvidenceVerification,
) (types.EvidenceVerification, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx, `
		SELECT `+evidenceVerificationOutputExpr+`
		FROM ComponentReleaseEvidenceVerification
		WHERE release_bundle_id = @releaseBundleId
		  AND organization_id = @organizationId
		  AND artifact_key = @artifactKey
		  AND platform = @platform`,
		pgx.NamedArgs{
			"releaseBundleId": verification.ReleaseBundleID,
			"organizationId":  verification.OrganizationID,
			"artifactKey":     verification.ArtifactKey,
			"platform":        verification.Platform,
		},
	)
	if err != nil {
		return types.EvidenceVerification{}, fmt.Errorf("could not query Component Release provenance verification: %w", err)
	}
	existing, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.EvidenceVerification])
	if err != nil {
		return types.EvidenceVerification{}, fmt.Errorf("could not collect Component Release provenance verification: %w", err)
	}
	return existing, nil
}

func validateEvidenceVerification(verification types.EvidenceVerification) error {
	if verification.OrganizationID == uuid.Nil ||
		verification.ReleaseBundleID == uuid.Nil ||
		verification.VerifiedAt.IsZero() ||
		!releasebundles.IsSHA256Digest(verification.ArtifactDigest) ||
		!releasebundles.IsSHA256Digest(verification.EvidenceDigest) ||
		!releasebundles.IsSHA256Digest(verification.PolicyChecksum) ||
		!releasebundles.IsSHA256Digest(verification.ExternalParametersChecksum) {
		return apierrors.NewBadRequest("bounded Component Release provenance verification is invalid")
	}
	text := []struct {
		value string
		limit int
	}{
		{verification.ArtifactKey, 128},
		{verification.EvidenceReference, 2048},
		{verification.TrustRootID, 256},
		{verification.PredicateType, 1024},
		{verification.BuilderID, 1024},
		{verification.BuildID, 1024},
		{verification.SourceURI, 2048},
		{verification.BuildType, 1024},
		{verification.SignerIssuer, 1024},
		{verification.SignerIdentity, 1024},
	}
	for _, field := range text {
		if strings.TrimSpace(field.value) != field.value ||
			field.value == "" ||
			len(field.value) > field.limit ||
			!utf8.ValidString(field.value) ||
			strings.IndexFunc(field.value, unicode.IsControl) >= 0 {
			return apierrors.NewBadRequest("bounded Component Release provenance verification is invalid")
		}
	}
	if verification.Platform != "linux/amd64" && verification.Platform != "linux/arm64" {
		return apierrors.NewBadRequest("bounded Component Release provenance verification is invalid")
	}
	if !releaseBackfillCommitPattern.MatchString(verification.SourceCommit) {
		return apierrors.NewBadRequest("bounded Component Release provenance verification is invalid")
	}
	return nil
}

func sameEvidenceVerification(a, b types.EvidenceVerification) bool {
	return a.OrganizationID == b.OrganizationID &&
		a.ReleaseBundleID == b.ReleaseBundleID &&
		a.ArtifactKey == b.ArtifactKey &&
		a.Platform == b.Platform &&
		a.ArtifactDigest == b.ArtifactDigest &&
		a.EvidenceReference == b.EvidenceReference &&
		a.EvidenceDigest == b.EvidenceDigest &&
		a.PolicyChecksum == b.PolicyChecksum &&
		a.TrustRootID == b.TrustRootID &&
		a.PredicateType == b.PredicateType &&
		a.BuilderID == b.BuilderID &&
		a.BuildID == b.BuildID &&
		a.SourceURI == b.SourceURI &&
		a.SourceCommit == b.SourceCommit &&
		a.BuildType == b.BuildType &&
		a.ExternalParametersChecksum == b.ExternalParametersChecksum &&
		a.SignerIssuer == b.SignerIssuer &&
		a.SignerIdentity == b.SignerIdentity &&
		a.VerifiedAt.UTC().Truncate(time.Microsecond).
			Equal(b.VerifiedAt.UTC().Truncate(time.Microsecond))
}
