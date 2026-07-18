package db

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/releasebundles"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestProjectLegacyReleaseBundleV2DerivesAdditiveDraftWithoutMutatingV1(t *testing.T) {
	g := NewWithT(t)
	source := legacyReleaseBackfillFixture()
	originalChecksum := source.CanonicalChecksum
	originalPayload := append([]byte(nil), source.CanonicalPayload...)
	originalContract := source.ReleaseContract.Schema

	projection := projectLegacyReleaseBundleV2(source, []types.ReleaseBackfillArtifactEvidence{
		legacyReleaseBackfillArtifactEvidence(source),
	})

	g.Expect(projection.ReasonCode).To(BeEmpty())
	g.Expect(projection.Bundle.ID).To(Equal(uuid.Nil))
	g.Expect(projection.Bundle.Status).To(Equal(types.ReleaseBundleStatusDraft))
	g.Expect(projection.Bundle.Kind).To(Equal(types.ReleaseBundleKindComponent))
	g.Expect(projection.Bundle.ReleaseContractSchema).To(Equal(types.ReleaseContractSchemaV2))
	g.Expect(projection.Bundle.ReleaseContract.ComponentV2.Source.Commit).To(Equal(strings.Repeat("b", 40)))
	g.Expect(projection.Bundle.ReleaseContract.ComponentV2.Artifacts[0].Platforms[0].Digest).
		To(Equal("sha256:" + strings.Repeat("a", 64)))
	g.Expect(projection.Bundle.ReleaseContract.ComponentV2.Artifacts[0].MediaType).
		To(Equal("application/vnd.oci.image.index.v1+json"))
	g.Expect(source.CanonicalChecksum).To(Equal(originalChecksum))
	g.Expect(source.CanonicalPayload).To(Equal(originalPayload))
	g.Expect(source.ReleaseContract.Schema).To(Equal(originalContract))
}

func TestProjectLegacyReleaseBundleV2BlocksAmbiguousRows(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*types.ReleaseBundle)
		reason string
	}{
		{
			name: "multiple components",
			mutate: func(bundle *types.ReleaseBundle) {
				bundle.ReleaseContract.Components = append(
					bundle.ReleaseContract.Components,
					bundle.ReleaseContract.Components[0],
				)
			},
			reason: releaseBackfillBlockedAmbiguousComponents,
		},
		{
			name: "migration intent",
			mutate: func(bundle *types.ReleaseBundle) {
				bundle.ReleaseContract.Operations.MigrationRequired = true
			},
			reason: releaseBackfillBlockedAmbiguousOperations,
		},
		{
			name: "legacy config facts",
			mutate: func(bundle *types.ReleaseBundle) {
				bundle.ReleaseContract.Config.ComposePath = "compose.yaml"
			},
			reason: releaseBackfillBlockedAmbiguousOperations,
		},
		{
			name: "short commit",
			mutate: func(bundle *types.ReleaseBundle) {
				bundle.ReleaseContract.Source.BuiltCommit = "abcdef0"
			},
			reason: releaseBackfillBlockedInvalidSource,
		},
		{
			name: "missing builder",
			mutate: func(bundle *types.ReleaseBundle) {
				bundle.CIProvider = ""
			},
			reason: releaseBackfillBlockedInvalidBuild,
		},
		{
			name: "mutable source row",
			mutate: func(bundle *types.ReleaseBundle) {
				bundle.Status = types.ReleaseBundleStatusDraft
			},
			reason: releaseBackfillBlockedMutableSource,
		},
		{
			name: "source checksum mismatch",
			mutate: func(bundle *types.ReleaseBundle) {
				bundle.CanonicalPayload = append(bundle.CanonicalPayload, ' ')
			},
			reason: releaseBackfillBlockedSourceChecksum,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			source := legacyReleaseBackfillFixture()
			tt.mutate(&source)
			if tt.reason != releaseBackfillBlockedSourceChecksum {
				payload, checksum, err := releasebundles.Canonicalize(source)
				g.Expect(err).NotTo(HaveOccurred())
				source.CanonicalPayload = payload
				source.CanonicalChecksum = checksum
			}
			g.Expect(projectLegacyReleaseBundleV2(
				source,
				[]types.ReleaseBackfillArtifactEvidence{legacyReleaseBackfillArtifactEvidence(source)},
			).ReasonCode).To(Equal(tt.reason))
		})
	}
}

func TestProjectLegacyReleaseBundleV2RequiresExactReviewedArtifactMediaTypeEvidence(t *testing.T) {
	g := NewWithT(t)
	source := legacyReleaseBackfillFixture()

	projection := projectLegacyReleaseBundleV2(source, nil)
	g.Expect(projection.ReasonCode).To(Equal(releaseBackfillBlockedAmbiguousArtifactMediaType))

	evidence := legacyReleaseBackfillArtifactEvidence(source)
	evidence.ArtifactDigest = "sha256:" + strings.Repeat("f", 64)
	projection = projectLegacyReleaseBundleV2(source, []types.ReleaseBackfillArtifactEvidence{evidence})
	g.Expect(projection.ReasonCode).To(Equal(releaseBackfillBlockedAmbiguousArtifactMediaType))

	evidence = legacyReleaseBackfillArtifactEvidence(source)
	evidence.MediaType = "application/vnd.oci.image.manifest.v1+json"
	projection = projectLegacyReleaseBundleV2(source, []types.ReleaseBackfillArtifactEvidence{
		legacyReleaseBackfillArtifactEvidence(source),
		evidence,
	})
	g.Expect(projection.ReasonCode).To(Equal(releaseBackfillBlockedAmbiguousArtifactMediaType))
}

func TestDryRunReportsWouldDeriveWithoutIncrementingPersistedDerived(t *testing.T) {
	g := NewWithT(t)
	report := &types.ReleaseBackfillReport{DryRun: true}

	recordReleaseBackfillDryRunProjection(report, releaseBackfillProjection{})

	g.Expect(report.Eligible).To(Equal(1))
	g.Expect(report.WouldDerive).To(Equal(1))
	g.Expect(report.Derived).To(Equal(0))
}

func TestReleaseBackfillApplyRequiresImmutableReviewedEvidenceDocument(t *testing.T) {
	g := NewWithT(t)
	source := legacyReleaseBackfillFixture()
	request := types.ReleaseBackfillRequest{
		OrganizationID:            source.OrganizationID,
		CheckpointID:              uuid.New(),
		Apply:                     true,
		ArtifactEvidence:          []types.ReleaseBackfillArtifactEvidence{legacyReleaseBackfillArtifactEvidence(source)},
		EvidenceDocumentReference: "review://backfill/choice-tp-dev/2026-07-18",
		EvidenceDocumentChecksum:  "sha256:" + strings.Repeat("d", 64),
	}

	g.Expect(validateReleaseBackfillArtifactEvidenceRequest(request)).To(Succeed())
	checkpoint := types.ReleaseBackfillCheckpoint{
		OrganizationID:            request.OrganizationID,
		CheckpointID:              request.CheckpointID,
		EvidenceDocumentReference: request.EvidenceDocumentReference,
		EvidenceDocumentChecksum:  request.EvidenceDocumentChecksum,
	}
	g.Expect(sameReleaseBackfillCheckpointBinding(checkpoint, request)).To(BeTrue())

	swapped := request
	swapped.EvidenceDocumentChecksum = "sha256:" + strings.Repeat("f", 64)
	g.Expect(sameReleaseBackfillCheckpointBinding(checkpoint, swapped)).To(BeFalse())

	missingDocument := request
	missingDocument.EvidenceDocumentReference = ""
	missingDocument.EvidenceDocumentChecksum = ""
	g.Expect(validateReleaseBackfillArtifactEvidenceRequest(missingDocument)).To(HaveOccurred())
}

func TestReleaseBackfillInvocationProcessesOneBatchAndAdvancesNextCursor(t *testing.T) {
	g := NewWithT(t)
	request := types.ReleaseBackfillRequest{BatchSize: 2}
	candidates := []releaseBackfillCandidate{
		{Bundle: types.ReleaseBundle{
			ID:        uuid.MustParse("11111111-1111-4111-8111-111111111111"),
			CreatedAt: time.Date(2026, 7, 18, 8, 30, 0, 0, time.UTC),
		}},
		{Bundle: types.ReleaseBundle{
			ID:        uuid.MustParse("22222222-2222-4222-8222-222222222222"),
			CreatedAt: time.Date(2026, 7, 18, 8, 31, 0, 0, time.UTC),
		}},
	}
	listCalls := 0
	processed := make([]uuid.UUID, 0)
	report := &types.ReleaseBackfillReport{}

	listed, err := runReleaseBackfillInvocation(
		context.Background(),
		request,
		report,
		func(context.Context, types.ReleaseBackfillRequest) ([]releaseBackfillCandidate, error) {
			listCalls++
			if listCalls > 1 {
				return []releaseBackfillCandidate{{
					Bundle: types.ReleaseBundle{ID: uuid.New(), CreatedAt: time.Now()},
				}}, nil
			}
			return candidates, nil
		},
		func(candidate releaseBackfillCandidate) (bool, bool, error) {
			processed = append(processed, candidate.Bundle.ID)
			return true, false, nil
		},
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(listed).To(BeTrue())
	g.Expect(listCalls).To(Equal(1))
	g.Expect(processed).To(Equal([]uuid.UUID{candidates[0].Bundle.ID, candidates[1].Bundle.ID}))
	g.Expect(report.Scanned).To(Equal(request.BatchSize))
	g.Expect(report.LastCursor).NotTo(BeNil())
	g.Expect(report.LastCursor.ReleaseBundleID).To(Equal(candidates[1].Bundle.ID))
	g.Expect(report.NextCursor).To(Equal(report.LastCursor))
}

func TestSingleReleaseBackfillArtifactEvidenceRequiresOneReviewedRow(t *testing.T) {
	g := NewWithT(t)
	source := legacyReleaseBackfillFixture()
	evidence := legacyReleaseBackfillArtifactEvidence(source)
	selected, reviewed := singleReleaseBackfillArtifactEvidence(source.ID, []types.ReleaseBackfillArtifactEvidence{evidence})
	g.Expect(reviewed).To(BeTrue())
	g.Expect(selected).To(Equal(evidence))

	_, reviewed = singleReleaseBackfillArtifactEvidence(source.ID, nil)
	g.Expect(reviewed).To(BeFalse())
	_, reviewed = singleReleaseBackfillArtifactEvidence(
		source.ID,
		[]types.ReleaseBackfillArtifactEvidence{evidence, evidence},
	)
	g.Expect(reviewed).To(BeFalse())
}

func legacyReleaseBackfillArtifactEvidence(source types.ReleaseBundle) types.ReleaseBackfillArtifactEvidence {
	return types.ReleaseBackfillArtifactEvidence{
		SourceReleaseBundleID: source.ID,
		ArtifactKey:           source.Components[0].Key,
		ArtifactDigest:        source.Components[0].Digest,
		MediaType:             "application/vnd.oci.image.index.v1+json",
		Reference:             "review://release-manifest/11111111-1111-4111-8111-111111111111",
		EvidenceDigest:        "sha256:" + strings.Repeat("e", 64),
	}
}

func legacyReleaseBackfillFixture() types.ReleaseBundle {
	digest := "sha256:" + strings.Repeat("a", 64)
	component := types.ReleaseBundleComponent{
		Key: "service", Name: "Service", Type: types.ReleaseBundleComponentTypeOCIImage,
		Version: "1.2.3", PackageRef: "registry.example.invalid/platform/service", Digest: digest,
	}
	bundle := types.ReleaseBundle{
		ID:                    uuid.MustParse("11111111-1111-4111-8111-111111111111"),
		OrganizationID:        uuid.New(),
		ApplicationID:         uuid.New(),
		ChannelID:             uuid.New(),
		ReleaseNumber:         "2026.07.18",
		SourceRepository:      "platform/service",
		SourceBranch:          "refs/heads/main",
		CIProvider:            "generic-ci",
		CIRunID:               "build-42",
		Kind:                  types.ReleaseBundleKindLegacy,
		ReleaseContractSchema: types.ReleaseContractStorageSchemaV1,
		Status:                types.ReleaseBundleStatusPublished,
		Components:            []types.ReleaseBundleComponent{component},
		ReleaseContract: &types.ReleaseContract{
			Schema: types.ReleaseContractSchemaV1,
			Source: types.ReleaseContractSource{
				Repository: "platform/service",
				Branch:     "refs/heads/main", SourceCommit: strings.Repeat("b", 40), BuiltCommit: strings.Repeat("b", 40),
			},
			Build: types.ReleaseContractBuild{ExternalID: "build-42"},
			Components: []types.ReleaseContractComponent{{
				Name: "service", Version: "1.2.3",
				Image: component.PackageRef + "@" + digest, Platform: "linux/amd64",
			}},
			Changes: types.ReleaseContractChanges{
				Summary: "Release service", Commits: []string{strings.Repeat("b", 40)},
			},
		},
	}
	payload, checksum, err := releasebundles.Canonicalize(bundle)
	if err != nil {
		panic(err)
	}
	bundle.CanonicalPayload = payload
	bundle.CanonicalChecksum = checksum
	return bundle
}
