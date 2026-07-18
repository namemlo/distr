package types

import (
	"time"

	"github.com/google/uuid"
)

// EvidenceVerification is the bounded, non-secret result of verifying one
// signed provenance envelope against one immutable artifact platform digest.
// The raw envelope, trusted-root document, and verifier errors are deliberately
// excluded.
type EvidenceVerification struct {
	ID                         uuid.UUID `db:"id" json:"id"`
	CreatedAt                  time.Time `db:"created_at" json:"createdAt"`
	OrganizationID             uuid.UUID `db:"organization_id" json:"organizationId"`
	ReleaseBundleID            uuid.UUID `db:"release_bundle_id" json:"releaseBundleId"`
	ArtifactKey                string    `db:"artifact_key" json:"artifactKey"`
	Platform                   string    `db:"platform" json:"platform"`
	ArtifactDigest             string    `db:"artifact_digest" json:"artifactDigest"`
	EvidenceReference          string    `db:"evidence_reference" json:"evidenceReference"`
	EvidenceDigest             string    `db:"evidence_digest" json:"evidenceDigest"`
	PolicyChecksum             string    `db:"policy_checksum" json:"policyChecksum"`
	TrustRootID                string    `db:"trust_root_id" json:"trustRootId"`
	PredicateType              string    `db:"predicate_type" json:"predicateType"`
	BuilderID                  string    `db:"builder_id" json:"builderId"`
	BuildID                    string    `db:"build_id" json:"buildId"`
	SourceURI                  string    `db:"source_uri" json:"sourceUri"`
	SourceCommit               string    `db:"source_commit" json:"sourceCommit"`
	BuildType                  string    `db:"build_type" json:"buildType"`
	ExternalParametersChecksum string    `db:"external_parameters_checksum" json:"externalParametersChecksum"`
	SignerIssuer               string    `db:"signer_issuer" json:"signerIssuer"`
	SignerIdentity             string    `db:"signer_identity" json:"signerIdentity"`
	VerifiedAt                 time.Time `db:"verified_at" json:"verifiedAt"`
}

type ReleaseBackfillCursor struct {
	CreatedAt       time.Time `json:"createdAt"`
	ReleaseBundleID uuid.UUID `json:"releaseBundleId"`
}

type ReleaseBackfillRequest struct {
	OrganizationID            uuid.UUID
	CheckpointID              uuid.UUID
	Apply                     bool
	BatchSize                 int
	Cursor                    *ReleaseBackfillCursor
	ArtifactEvidence          []ReleaseBackfillArtifactEvidence
	EvidenceDocumentReference string
	EvidenceDocumentChecksum  string
}

type ReleaseBackfillArtifactEvidence struct {
	SourceReleaseBundleID uuid.UUID `json:"sourceReleaseBundleId"`
	ArtifactKey           string    `json:"artifactKey"`
	ArtifactDigest        string    `json:"artifactDigest"`
	MediaType             string    `json:"mediaType"`
	Reference             string    `json:"reference"`
	EvidenceDigest        string    `json:"evidenceDigest"`
}

type ReleaseBackfillReport struct {
	CheckpointID     uuid.UUID
	DryRun           bool
	Scanned          int
	Eligible         int
	WouldDerive      int
	Derived          int
	AlreadyPresent   int
	AwaitingEvidence int
	Blocked          int
	Failed           int
	LastCursor       *ReleaseBackfillCursor
	NextCursor       *ReleaseBackfillCursor
}

type ReleaseBackfillLineageState string

const (
	ReleaseBackfillLineageStateDerived ReleaseBackfillLineageState = "derived"
	ReleaseBackfillLineageStateBlocked ReleaseBackfillLineageState = "blocked"
)

type ReleaseBackfillLineage struct {
	ID                        uuid.UUID                   `db:"id" json:"id"`
	CreatedAt                 time.Time                   `db:"created_at" json:"createdAt"`
	OrganizationID            uuid.UUID                   `db:"organization_id" json:"organizationId"`
	CheckpointID              uuid.UUID                   `db:"checkpoint_id" json:"checkpointId"`
	SourceReleaseBundleID     uuid.UUID                   `db:"source_release_bundle_id" json:"sourceReleaseBundleId"`
	SourceCanonicalChecksum   string                      `db:"source_canonical_checksum" json:"sourceCanonicalChecksum"`
	DerivedReleaseBundleID    *uuid.UUID                  `db:"derived_release_bundle_id" json:"derivedReleaseBundleId,omitempty"`
	DerivedCanonicalChecksum  string                      `db:"derived_canonical_checksum" json:"derivedCanonicalChecksum"`
	State                     ReleaseBackfillLineageState `db:"state" json:"state"`
	ReasonCode                string                      `db:"reason_code" json:"reasonCode"`
	ReviewedArtifactKey       string                      `db:"reviewed_artifact_key" json:"reviewedArtifactKey"`
	ReviewedArtifactDigest    string                      `db:"reviewed_artifact_digest" json:"reviewedArtifactDigest"`
	ArtifactMediaType         string                      `db:"artifact_media_type" json:"artifactMediaType"`
	ArtifactEvidenceReference string                      `db:"artifact_evidence_reference" json:"artifactEvidenceReference"`
	ArtifactEvidenceDigest    string                      `db:"artifact_evidence_digest" json:"artifactEvidenceDigest"`
}

type ReleaseBackfillCheckpoint struct {
	ID                        uuid.UUID `db:"id" json:"id"`
	CreatedAt                 time.Time `db:"created_at" json:"createdAt"`
	OrganizationID            uuid.UUID `db:"organization_id" json:"organizationId"`
	CheckpointID              uuid.UUID `db:"checkpoint_id" json:"checkpointId"`
	EvidenceDocumentReference string    `db:"evidence_document_reference" json:"evidenceDocumentReference"`
	EvidenceDocumentChecksum  string    `db:"evidence_document_checksum" json:"evidenceDocumentChecksum"`
}
