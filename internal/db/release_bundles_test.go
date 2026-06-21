package db_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/lifecycle"
	"github.com/distr-sh/distr/internal/releasebundles"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
)

func TestReleaseBundleRepositoryDraftCRUDAndChecksum(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleDependencies(t, ctx)
	bundle := releaseBundleFixture(orgID, applicationID, channelID, versionID)

	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())
	g.Expect(bundle.ID).NotTo(Equal(uuid.Nil))
	g.Expect(bundle.Status).To(Equal(types.ReleaseBundleStatusDraft))
	g.Expect(bundle.CanonicalChecksum).To(HavePrefix("sha256:"))
	g.Expect(bundle.CanonicalPayload).NotTo(BeEmpty())
	g.Expect(bundle.Components).To(HaveLen(1))

	listed, err := db.GetReleaseBundlesByOrganizationID(ctx, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(listed).To(HaveLen(1))
	g.Expect(listed[0].ID).To(Equal(bundle.ID))
	g.Expect(listed[0].Components).To(HaveLen(1))

	createdChecksum := bundle.CanonicalChecksum
	bundle.ReleaseNotes = "Updated notes"
	bundle.Components[0].Version = "1.2.4"
	g.Expect(db.UpdateReleaseBundle(ctx, &bundle)).To(Succeed())
	g.Expect(bundle.CanonicalChecksum).NotTo(Equal(createdChecksum))

	fetched, err := db.GetReleaseBundle(ctx, bundle.ID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(fetched.Components[0].Version).To(Equal("1.2.4"))
	expectedPayload, expectedChecksum, err := releasebundles.Canonicalize(*fetched)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(fetched.CanonicalPayload).To(Equal(expectedPayload))
	g.Expect(fetched.CanonicalChecksum).To(Equal(expectedChecksum))

	g.Expect(db.DeleteReleaseBundleWithID(ctx, bundle.ID, orgID)).To(Succeed())
	_, err = db.GetReleaseBundle(ctx, bundle.ID, orgID)
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
}

func TestReleaseBundleRepositoryRejectsDuplicateReleaseNumbersWithinApplicationScope(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleDependencies(t, ctx)
	first := releaseBundleFixture(orgID, applicationID, channelID, versionID)
	g.Expect(db.CreateReleaseBundle(ctx, &first)).To(Succeed())

	duplicate := releaseBundleFixture(orgID, applicationID, channelID, versionID)
	err := db.CreateReleaseBundle(ctx, &duplicate)
	g.Expect(errors.Is(err, apierrors.ErrAlreadyExists)).To(BeTrue())

	otherApplicationID, otherChannelID, otherVersionID := createReleaseBundleDependenciesForOrganization(t, ctx, orgID)
	sameNumberOtherApplication := releaseBundleFixture(orgID, otherApplicationID, otherChannelID, otherVersionID)
	g.Expect(db.CreateReleaseBundle(ctx, &sameNumberOtherApplication)).To(Succeed())
}

func TestReleaseBundleRepositoryPersistsSourceMetadata(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleDependencies(t, ctx)
	bundle := releaseBundleFixture(orgID, applicationID, channelID, versionID)
	bundle.SourceRepository = "https://example.invalid/org/project"
	bundle.SourceBranch = "main"
	bundle.SourceTag = "v1.2.3"
	bundle.CIProvider = "generic-ci"
	bundle.CIRunID = "run-123"
	bundle.CIRunURL = "https://ci.example.invalid/runs/123"

	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())

	fetched, err := db.GetReleaseBundle(ctx, bundle.ID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(fetched.SourceRepository).To(Equal("https://example.invalid/org/project"))
	g.Expect(fetched.SourceBranch).To(Equal("main"))
	g.Expect(fetched.SourceTag).To(Equal("v1.2.3"))
	g.Expect(fetched.CIProvider).To(Equal("generic-ci"))
	g.Expect(fetched.CIRunID).To(Equal("run-123"))
	g.Expect(fetched.CIRunURL).To(Equal("https://ci.example.invalid/runs/123"))
	g.Expect(string(fetched.CanonicalPayload)).To(ContainSubstring(`"ciRunId":"run-123"`))
}

func TestReleaseBundleRepositoryRejectsInvalidOCIDigest(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, _ := createReleaseBundleDependencies(t, ctx)
	bundle := releaseBundleFixture(orgID, applicationID, channelID, uuid.New())
	bundle.Components = []types.ReleaseBundleComponent{
		{
			Key:        "api-image",
			Name:       "API image",
			Type:       types.ReleaseBundleComponentTypeOCIImage,
			Version:    "1.2.3",
			PackageRef: "registry.example.invalid/org/api",
			Digest:     "sha256:" + strings.Repeat("a", 63),
		},
	}

	err := db.CreateReleaseBundle(ctx, &bundle)

	g.Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())
}

func TestReleaseBundleRepositoryIdempotentCreateReturnsExistingBundle(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleDependencies(t, ctx)
	first := releaseBundleFixture(orgID, applicationID, channelID, versionID)

	g.Expect(db.CreateReleaseBundleWithIdempotency(ctx, &first, " ci-key-1 ")).To(Succeed())
	second := releaseBundleFixture(orgID, applicationID, channelID, versionID)
	g.Expect(db.CreateReleaseBundleWithIdempotency(ctx, &second, "ci-key-1")).To(Succeed())

	g.Expect(second.ID).To(Equal(first.ID))
	g.Expect(second.CanonicalChecksum).To(Equal(first.CanonicalChecksum))
	listed, err := db.GetReleaseBundlesByOrganizationID(ctx, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(listed).To(HaveLen(1))

	var componentCount int
	g.Expect(internalctx.GetDb(ctx).QueryRow(
		ctx,
		`SELECT count(*) FROM ReleaseBundleComponent WHERE release_bundle_id = @releaseBundleId`,
		pgx.NamedArgs{"releaseBundleId": first.ID},
	).Scan(&componentCount)).To(Succeed())
	g.Expect(componentCount).To(Equal(1))

	var keyHash string
	g.Expect(internalctx.GetDb(ctx).QueryRow(
		ctx,
		`SELECT key_hash FROM ReleaseBundleIdempotencyKey WHERE organization_id = @organizationId`,
		pgx.NamedArgs{"organizationId": orgID},
	).Scan(&keyHash)).To(Succeed())
	g.Expect(keyHash).To(HavePrefix("sha256:"))
	g.Expect(keyHash).NotTo(ContainSubstring("ci-key-1"))
}

func TestReleaseBundleRepositoryIdempotentCreateRejectsDifferentCanonicalRequest(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleDependencies(t, ctx)
	first := releaseBundleFixture(orgID, applicationID, channelID, versionID)
	g.Expect(db.CreateReleaseBundleWithIdempotency(ctx, &first, "ci-key-conflict")).To(Succeed())

	second := releaseBundleFixture(orgID, applicationID, channelID, versionID)
	second.ReleaseNumber = "2026.06.21"
	err := db.CreateReleaseBundleWithIdempotency(ctx, &second, "ci-key-conflict")

	g.Expect(errors.Is(err, db.ErrReleaseBundleIdempotencyConflict)).To(BeTrue())
	listed, listErr := db.GetReleaseBundlesByOrganizationID(ctx, orgID)
	g.Expect(listErr).NotTo(HaveOccurred())
	g.Expect(listed).To(HaveLen(1))
}

func TestReleaseBundleRepositoryPreservesIdempotencyAfterDeleteAttempt(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleDependencies(t, ctx)
	first := releaseBundleFixture(orgID, applicationID, channelID, versionID)
	g.Expect(db.CreateReleaseBundleWithIdempotency(ctx, &first, "delete-protected-key")).To(Succeed())

	err := db.DeleteReleaseBundleWithID(ctx, first.ID, orgID)
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())

	replayed := releaseBundleFixture(orgID, applicationID, channelID, versionID)
	g.Expect(db.CreateReleaseBundleWithIdempotency(ctx, &replayed, "delete-protected-key")).To(Succeed())

	g.Expect(replayed.ID).To(Equal(first.ID))
	listed, err := db.GetReleaseBundlesByOrganizationID(ctx, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(listed).To(HaveLen(1))
}

func TestReleaseBundleRepositoryIdempotencyKeysAreScopedByOrganization(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleDependencies(t, ctx)
	otherOrgID, otherApplicationID, otherChannelID, otherVersionID := createReleaseBundleDependencies(t, ctx)
	first := releaseBundleFixture(orgID, applicationID, channelID, versionID)
	second := releaseBundleFixture(otherOrgID, otherApplicationID, otherChannelID, otherVersionID)

	g.Expect(db.CreateReleaseBundleWithIdempotency(ctx, &first, "shared-ci-key")).To(Succeed())
	g.Expect(db.CreateReleaseBundleWithIdempotency(ctx, &second, "shared-ci-key")).To(Succeed())

	g.Expect(first.ID).NotTo(Equal(second.ID))
}

func TestReleaseBundleRepositoryConcurrentIdempotentCreateCreatesExactlyOneBundle(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleDependencies(t, ctx)
	const workers = 8
	start := make(chan struct{})
	results := make(chan types.ReleaseBundle, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			bundle := releaseBundleFixture(orgID, applicationID, channelID, versionID)
			if err := db.CreateReleaseBundleWithIdempotency(ctx, &bundle, "concurrent-ci-key"); err != nil {
				errs <- err
				return
			}
			results <- bundle
		}()
	}

	close(start)
	wg.Wait()
	close(results)
	close(errs)

	g.Expect(errs).To(BeEmpty())
	var firstID uuid.UUID
	for bundle := range results {
		if firstID == uuid.Nil {
			firstID = bundle.ID
		}
		g.Expect(bundle.ID).To(Equal(firstID))
	}
	listed, err := db.GetReleaseBundlesByOrganizationID(ctx, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(listed).To(HaveLen(1))
}

func TestReleaseBundleRepositoryFailedIdempotentCreateDoesNotReserveKey(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleDependencies(t, ctx)
	invalid := releaseBundleFixture(orgID, applicationID, channelID, uuid.New())

	err := db.CreateReleaseBundleWithIdempotency(ctx, &invalid, "retry-after-failure")
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())

	valid := releaseBundleFixture(orgID, applicationID, channelID, versionID)
	g.Expect(db.CreateReleaseBundleWithIdempotency(ctx, &valid, "retry-after-failure")).To(Succeed())
}

func TestReleaseBundleRepositoryValidatesBundleSourceRulesOnPublish(t *testing.T) {
	tests := []struct {
		name         string
		sourceBranch string
		sourceTag    string
		wantErrField string
	}{
		{
			name:         "accepts matching source branch",
			sourceBranch: "release/2026.06",
		},
		{
			name:         "rejects missing source",
			wantErrField: "sourceMetadata",
		},
		{
			name:         "rejects non matching branch",
			sourceBranch: "feature/demo",
			wantErrField: "sourceMetadata.branch",
		},
		{
			name:         "rejects tag when channel allows only branches",
			sourceTag:    "v1.2.3",
			wantErrField: "sourceMetadata.tag",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := releaseBundleDBTestContext(t)
			g := NewWithT(t)
			orgID := createReleaseBundleTestOrganization(t, ctx)
			applicationID, channelID, _ := createReleaseBundleDependenciesForOrganizationWithRules(
				t, ctx, orgID, nil, nil, []string{"main", "release/*"}, nil,
			)
			actorID := createReleaseBundleTestUser(t, ctx, orgID)
			bundle := ociReleaseBundleFixture(orgID, applicationID, channelID)
			bundle.SourceBranch = tt.sourceBranch
			bundle.SourceTag = tt.sourceTag
			g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())

			published, result, err := db.PublishReleaseBundle(ctx, bundle.ID, orgID, actorID)

			if tt.wantErrField == "" {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(result.Valid).To(BeTrue())
				g.Expect(published).NotTo(BeNil())
				g.Expect(published.Status).To(Equal(types.ReleaseBundleStatusPublished))
			} else {
				g.Expect(published).To(BeNil())
				g.Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())
				g.Expect(result.Valid).To(BeFalse())
				g.Expect(result.Errors).To(ContainElement(MatchFields(IgnoreExtras, Fields{
					"Field": Equal(tt.wantErrField),
				})))
			}
		})
	}
}

func TestReleaseBundleRepositoryValidationRejectsLegacyMalformedOCIDigest(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, _ := createReleaseBundleDependencies(t, ctx)
	actorID := createReleaseBundleTestUser(t, ctx, orgID)
	bundle := ociReleaseBundleFixture(orgID, applicationID, channelID)
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())

	fetched, err := db.GetReleaseBundle(ctx, bundle.ID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	fetched.Components[0].Digest = "sha256:abc"
	payload, checksum, err := releasebundles.Canonicalize(*fetched)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = internalctx.GetDb(ctx).Exec(
		ctx,
		`UPDATE ReleaseBundleComponent SET digest = @digest WHERE release_bundle_id = @releaseBundleId`,
		pgx.NamedArgs{
			"releaseBundleId": bundle.ID,
			"digest":          "sha256:abc",
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = internalctx.GetDb(ctx).Exec(
		ctx,
		`UPDATE ReleaseBundle SET canonical_payload = @payload, canonical_checksum = @checksum WHERE id = @releaseBundleId`,
		pgx.NamedArgs{
			"releaseBundleId": bundle.ID,
			"payload":         payload,
			"checksum":        checksum,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())

	published, result, err := db.PublishReleaseBundle(ctx, bundle.ID, orgID, actorID)

	g.Expect(published).To(BeNil())
	g.Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())
	g.Expect(result.Errors).To(ContainElement(releasebundles.ValidationIssue{
		Field:   "components.api-image.digest",
		Rule:    "sha256",
		Message: "OCI component digest must be a sha256 digest",
	}))
}

func TestReleaseBundleRepositoryRejectsInvalidAndCrossOrganizationReferences(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleDependencies(t, ctx)
	otherOrgID, otherApplicationID, otherChannelID, otherVersionID := createReleaseBundleDependencies(t, ctx)

	tests := []struct {
		name           string
		organizationID uuid.UUID
		applicationID  uuid.UUID
		channelID      uuid.UUID
		versionID      uuid.UUID
	}{
		{
			name:           "missing application",
			organizationID: orgID,
			applicationID:  uuid.New(),
			channelID:      channelID,
			versionID:      versionID,
		},
		{
			name:           "missing channel",
			organizationID: orgID,
			applicationID:  applicationID,
			channelID:      uuid.New(),
			versionID:      versionID,
		},
		{
			name:           "missing application version",
			organizationID: orgID,
			applicationID:  applicationID,
			channelID:      channelID,
			versionID:      uuid.New(),
		},
		{
			name:           "cross-organization application",
			organizationID: orgID,
			applicationID:  otherApplicationID,
			channelID:      channelID,
			versionID:      versionID,
		},
		{
			name:           "cross-organization channel",
			organizationID: orgID,
			applicationID:  applicationID,
			channelID:      otherChannelID,
			versionID:      versionID,
		},
		{
			name:           "cross-organization application version",
			organizationID: orgID,
			applicationID:  applicationID,
			channelID:      channelID,
			versionID:      otherVersionID,
		},
		{
			name:           "inverse cross-organization application",
			organizationID: otherOrgID,
			applicationID:  applicationID,
			channelID:      otherChannelID,
			versionID:      otherVersionID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bundle := releaseBundleFixture(tt.organizationID, tt.applicationID, tt.channelID, tt.versionID)

			err := db.CreateReleaseBundle(ctx, &bundle)

			g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
		})
	}
}

func TestReleaseBundleRepositoryRejectsMutatingNonDraftBundles(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleDependencies(t, ctx)
	bundle := releaseBundleFixture(orgID, applicationID, channelID, versionID)
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())
	markReleaseBundleStatusForTest(t, ctx, bundle.ID, types.ReleaseBundleStatusPublished)

	bundle.ReleaseNotes = "Cannot update"
	err := db.UpdateReleaseBundle(ctx, &bundle)
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())

	err = db.DeleteReleaseBundleWithID(ctx, bundle.ID, orgID)
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
}

func TestReleaseBundleRepositoryValidatePublishAndProtectPublishedBundle(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleDependencies(t, ctx)
	actorID := createReleaseBundleTestUser(t, ctx, orgID)
	bundle := releaseBundleFixture(orgID, applicationID, channelID, versionID)
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())

	result, err := db.ValidateReleaseBundle(ctx, bundle.ID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Valid).To(BeTrue())

	published, publishResult, err := db.PublishReleaseBundle(ctx, bundle.ID, orgID, actorID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(publishResult.Valid).To(BeTrue())
	g.Expect(published.Status).To(Equal(types.ReleaseBundleStatusPublished))
	g.Expect(published.PublishedByUserAccountID).To(Equal(&actorID))
	g.Expect(published.PublishedAt).NotTo(BeNil())
	g.Expect(published.CanonicalChecksum).To(Equal(bundle.CanonicalChecksum))

	published.ReleaseNotes = "Cannot edit after publish"
	err = db.UpdateReleaseBundle(ctx, published)
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
	err = db.DeleteReleaseBundleWithID(ctx, published.ID, orgID)
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())

	events, err := db.GetReleaseBundleAuditEvents(ctx, bundle.ID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(events).To(ContainElement(MatchFields(IgnoreExtras, Fields{
		"OrganizationID":     Equal(orgID),
		"ReleaseBundleID":    Equal(bundle.ID),
		"ActorUserAccountID": Equal(&actorID),
		"EventType":          Equal(types.ReleaseBundleAuditEventTypePublished),
		"FromStatus":         Equal(types.ReleaseBundleStatusDraft),
		"ToStatus":           Equal(releaseBundleStatusPtr(types.ReleaseBundleStatusPublished)),
		"Reason":             Equal(""),
	})))
}

func TestReleaseBundleRepositoryRejectsPublishWhenValidationFails(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID := createReleaseBundleTestOrganization(t, ctx)
	applicationID, channelID, versionID := createReleaseBundleDependenciesForOrganizationWithRules(
		t, ctx, orgID, []string{">=2.0.0 <3.0.0"}, nil, nil, nil,
	)
	actorID := createReleaseBundleTestUser(t, ctx, orgID)
	bundle := releaseBundleFixture(orgID, applicationID, channelID, versionID)
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())

	published, result, err := db.PublishReleaseBundle(ctx, bundle.ID, orgID, actorID)

	g.Expect(published).To(BeNil())
	g.Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())
	g.Expect(result.Valid).To(BeFalse())
	g.Expect(result.Errors).To(ContainElement(releasebundles.ValidationIssue{
		Field:   "components.api.version",
		Rule:    ">=2.0.0 <3.0.0",
		Message: "version does not match an allowed range",
	}))
	fetched, err := db.GetReleaseBundle(ctx, bundle.ID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(fetched.Status).To(Equal(types.ReleaseBundleStatusDraft))
	events, err := db.GetReleaseBundleAuditEvents(ctx, bundle.ID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(events).To(ContainElement(MatchFields(IgnoreExtras, Fields{
		"OrganizationID":     Equal(orgID),
		"ReleaseBundleID":    Equal(bundle.ID),
		"ActorUserAccountID": Equal(&actorID),
		"EventType":          Equal(types.ReleaseBundleAuditEventTypeStateTransitionRejected),
		"FromStatus":         Equal(types.ReleaseBundleStatusDraft),
		"ToStatus":           Equal(releaseBundleStatusPtr(types.ReleaseBundleStatusPublished)),
		"Reason":             Equal("validation failed"),
	})))
}

func TestReleaseBundleRepositoryBlockArchiveAndRejectedTransitionAudits(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleDependencies(t, ctx)
	actorID := createReleaseBundleTestUser(t, ctx, orgID)
	bundle := releaseBundleFixture(orgID, applicationID, channelID, versionID)
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())

	_, _, err := db.PublishReleaseBundle(ctx, bundle.ID, orgID, actorID)
	g.Expect(err).NotTo(HaveOccurred())
	blocked, err := db.BlockReleaseBundle(ctx, bundle.ID, orgID, actorID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(blocked.Status).To(Equal(types.ReleaseBundleStatusBlocked))
	_, err = db.BlockReleaseBundle(ctx, bundle.ID, orgID, actorID)
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
	archived, err := db.ArchiveReleaseBundle(ctx, bundle.ID, orgID, actorID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(archived.Status).To(Equal(types.ReleaseBundleStatusArchived))

	events, err := db.GetReleaseBundleAuditEvents(ctx, bundle.ID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(events).To(ContainElements(
		MatchFields(IgnoreExtras, Fields{
			"OrganizationID":     Equal(orgID),
			"ReleaseBundleID":    Equal(bundle.ID),
			"ActorUserAccountID": Equal(&actorID),
			"EventType":          Equal(types.ReleaseBundleAuditEventTypeBlocked),
			"FromStatus":         Equal(types.ReleaseBundleStatusPublished),
			"ToStatus":           Equal(releaseBundleStatusPtr(types.ReleaseBundleStatusBlocked)),
			"Reason":             Equal(""),
		}),
		MatchFields(IgnoreExtras, Fields{
			"OrganizationID":     Equal(orgID),
			"ReleaseBundleID":    Equal(bundle.ID),
			"ActorUserAccountID": Equal(&actorID),
			"EventType":          Equal(types.ReleaseBundleAuditEventTypeStateTransitionRejected),
			"FromStatus":         Equal(types.ReleaseBundleStatusBlocked),
			"ToStatus":           Equal(releaseBundleStatusPtr(types.ReleaseBundleStatusBlocked)),
			"Reason":             Equal("release bundle cannot transition from BLOCKED to BLOCKED"),
		}),
		MatchFields(IgnoreExtras, Fields{
			"OrganizationID":     Equal(orgID),
			"ReleaseBundleID":    Equal(bundle.ID),
			"ActorUserAccountID": Equal(&actorID),
			"EventType":          Equal(types.ReleaseBundleAuditEventTypeArchived),
			"FromStatus":         Equal(types.ReleaseBundleStatusBlocked),
			"ToStatus":           Equal(releaseBundleStatusPtr(types.ReleaseBundleStatusArchived)),
			"Reason":             Equal(""),
		}),
	))
}

func TestReleaseBundleRepositoryGetsEligibilityForPublishedBundle(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	deps := createReleaseBundleEligibilityDependencies(t, ctx)
	actorID := createReleaseBundleTestUser(t, ctx, deps.orgID)
	bundle := releaseBundleFixture(deps.orgID, deps.applicationID, deps.channelID, deps.versionID)
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())
	_, publishResult, err := db.PublishReleaseBundle(ctx, bundle.ID, deps.orgID, actorID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(publishResult.Valid).To(BeTrue())

	devResult, err := db.GetReleaseBundleEligibility(ctx, bundle.ID, deps.devEnvironmentID, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(devResult.EngineReady).To(BeTrue())
	g.Expect(devResult.Eligible).To(BeTrue())
	g.Expect(devResult.ReleaseBundleID).To(Equal(bundle.ID))
	g.Expect(devResult.ApplicationID).To(Equal(deps.applicationID))
	g.Expect(devResult.ChannelID).To(Equal(deps.channelID))
	g.Expect(devResult.LifecycleID).To(Equal(deps.lifecycleID))
	g.Expect(devResult.TargetPhase).NotTo(BeNil())
	g.Expect(devResult.TargetPhase.Name).To(Equal("Development"))
	g.Expect(devResult.Reasons).To(BeEmpty())

	prodResult, err := db.GetReleaseBundleEligibility(ctx, bundle.ID, deps.prodEnvironmentID, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(prodResult.Eligible).To(BeFalse())
	g.Expect(prodResult.TargetPhase).NotTo(BeNil())
	g.Expect(prodResult.TargetPhase.Name).To(Equal("Production"))
	g.Expect(prodResult.Reasons).To(ContainElement(MatchFields(IgnoreExtras, Fields{
		"Code": Equal(lifecycle.EligibilityReasonRequiredPriorPhaseIncomplete),
	})))
}

func TestReleaseBundleRepositoryEligibilityPreservesOrganizationIsolation(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	deps := createReleaseBundleEligibilityDependencies(t, ctx)
	otherDeps := createReleaseBundleEligibilityDependencies(t, ctx)
	bundle := releaseBundleFixture(deps.orgID, deps.applicationID, deps.channelID, deps.versionID)
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())

	_, err := db.GetReleaseBundleEligibility(ctx, bundle.ID, otherDeps.devEnvironmentID, deps.orgID)
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())

	_, err = db.GetReleaseBundleEligibility(ctx, bundle.ID, deps.devEnvironmentID, otherDeps.orgID)
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
}

func TestReleaseBundleRepositoryPreventsMovingReferencedChannelAcrossApplications(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, sourceApplicationID, sourceChannelID, versionID := createReleaseBundleDependencies(t, ctx)
	targetApplicationID, _, _ := createReleaseBundleDependenciesForOrganization(t, ctx, orgID)

	sourceChannel, err := db.GetChannel(ctx, sourceChannelID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	preview := types.Channel{
		OrganizationID: orgID,
		ApplicationID:  sourceApplicationID,
		LifecycleID:    sourceChannel.LifecycleID,
		Name:           "Preview",
		SortOrder:      10,
	}
	g.Expect(db.CreateChannel(ctx, &preview)).To(Succeed())
	g.Expect(preview.IsDefault).To(BeFalse())

	bundle := releaseBundleFixture(orgID, sourceApplicationID, preview.ID, versionID)
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())

	preview.ApplicationID = targetApplicationID
	err = db.UpdateChannel(ctx, &preview)

	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
	unchanged, err := db.GetChannel(ctx, preview.ID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(unchanged.ApplicationID).To(Equal(sourceApplicationID))
	fetched, err := db.GetReleaseBundle(ctx, bundle.ID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(fetched.ApplicationID).To(Equal(sourceApplicationID))
	g.Expect(fetched.ChannelID).To(Equal(preview.ID))
}

func TestReleaseBundleRepositoryCreatesProcessSnapshotFromRevision(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleDependencies(t, ctx)
	process, revision := createReleaseBundleProcessRevision(t, ctx, orgID, applicationID, "Standard deploy")
	bundle := releaseBundleFixture(orgID, applicationID, channelID, versionID)
	bundle.DeploymentProcessRevisionID = &revision.ID

	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())

	g.Expect(bundle.ProcessSnapshotID).NotTo(BeNil())
	snapshot, err := db.GetProcessSnapshot(ctx, *bundle.ProcessSnapshotID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(snapshot.ApplicationID).To(Equal(applicationID))
	g.Expect(snapshot.DeploymentProcessID).To(Equal(process.ID))
	g.Expect(snapshot.DeploymentProcessRevisionID).To(Equal(revision.ID))
	g.Expect(snapshot.RevisionNumber).To(Equal(1))
	g.Expect(snapshot.CanonicalChecksum).To(HavePrefix("sha256:"))
	g.Expect(snapshot.CanonicalPayload).NotTo(BeEmpty())
	g.Expect(snapshot.Revision.Steps).To(HaveLen(1))
	g.Expect(snapshot.Revision.Steps[0].Key).To(Equal("deploy"))
	g.Expect(snapshot.Revision.Steps[0].InputBindings).To(HaveKeyWithValue("script", "make deploy"))
	g.Expect(string(bundle.CanonicalPayload)).To(ContainSubstring(
		`"processSnapshotId":"` + bundle.ProcessSnapshotID.String() + `"`,
	))

	fetched, err := db.GetReleaseBundle(ctx, bundle.ID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(fetched.ProcessSnapshotID).To(Equal(bundle.ProcessSnapshotID))

	_, err = db.GetProcessSnapshot(ctx, *bundle.ProcessSnapshotID, uuid.New())
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
}

func TestReleaseBundleRepositoryReusesProcessSnapshotForSameRevision(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleDependencies(t, ctx)
	_, revision := createReleaseBundleProcessRevision(t, ctx, orgID, applicationID, "Standard deploy")
	first := releaseBundleFixture(orgID, applicationID, channelID, versionID)
	first.DeploymentProcessRevisionID = &revision.ID
	g.Expect(db.CreateReleaseBundle(ctx, &first)).To(Succeed())

	second := releaseBundleFixture(orgID, applicationID, channelID, versionID)
	second.ReleaseNumber = "2026.06.21"
	second.DeploymentProcessRevisionID = &revision.ID
	g.Expect(db.CreateReleaseBundle(ctx, &second)).To(Succeed())

	g.Expect(second.ProcessSnapshotID).To(Equal(first.ProcessSnapshotID))
	var count int
	g.Expect(internalctx.GetDb(ctx).QueryRow(
		ctx,
		`SELECT count(*) FROM ProcessSnapshot WHERE deployment_process_revision_id = @revisionId`,
		pgx.NamedArgs{"revisionId": revision.ID},
	).Scan(&count)).To(Succeed())
	g.Expect(count).To(Equal(1))
}

func TestReleaseBundleRepositoryRejectsInvalidProcessRevisionReferences(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleDependencies(t, ctx)
	otherApplicationID, _, _ := createReleaseBundleDependenciesForOrganization(t, ctx, orgID)
	_, otherApplicationRevision := createReleaseBundleProcessRevision(
		t,
		ctx,
		orgID,
		otherApplicationID,
		"Other application process",
	)
	otherOrgID, otherOrgApplicationID, _, _ := createReleaseBundleDependencies(t, ctx)
	_, otherOrgRevision := createReleaseBundleProcessRevision(
		t,
		ctx,
		otherOrgID,
		otherOrgApplicationID,
		"Other organization process",
	)

	tests := []struct {
		name       string
		revisionID uuid.UUID
	}{
		{name: "missing revision", revisionID: uuid.New()},
		{name: "other application revision", revisionID: otherApplicationRevision.ID},
		{name: "other organization revision", revisionID: otherOrgRevision.ID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bundle := releaseBundleFixture(orgID, applicationID, channelID, versionID)
			bundle.DeploymentProcessRevisionID = &tt.revisionID

			err := db.CreateReleaseBundle(ctx, &bundle)

			g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
		})
	}
}

func TestReleaseBundleRepositoryUpdatesDraftProcessSnapshot(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleDependencies(t, ctx)
	_, firstRevision := createReleaseBundleProcessRevision(t, ctx, orgID, applicationID, "Initial process")
	_, secondRevision := createReleaseBundleProcessRevision(t, ctx, orgID, applicationID, "Updated process")
	bundle := releaseBundleFixture(orgID, applicationID, channelID, versionID)
	bundle.DeploymentProcessRevisionID = &firstRevision.ID
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())
	firstSnapshotID := *bundle.ProcessSnapshotID

	bundle.DeploymentProcessRevisionID = &secondRevision.ID
	g.Expect(db.UpdateReleaseBundle(ctx, &bundle)).To(Succeed())

	g.Expect(bundle.ProcessSnapshotID).NotTo(BeNil())
	g.Expect(*bundle.ProcessSnapshotID).NotTo(Equal(firstSnapshotID))
	snapshot, err := db.GetProcessSnapshot(ctx, *bundle.ProcessSnapshotID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(snapshot.DeploymentProcessRevisionID).To(Equal(secondRevision.ID))
}

func TestReleaseBundleMigrationDefinesDraftBundleSchema(t *testing.T) {
	g := NewWithT(t)

	up, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "112_release_bundles.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	sql := string(up)

	g.Expect(sql).To(ContainSubstring("CREATE TABLE ReleaseBundle"))
	g.Expect(sql).To(ContainSubstring("organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE"))
	g.Expect(sql).To(ContainSubstring("application_id UUID NOT NULL REFERENCES Application(id) ON DELETE RESTRICT"))
	g.Expect(sql).To(ContainSubstring("channel_id UUID NOT NULL"))
	g.Expect(sql).To(ContainSubstring("releasebundle_organization_application_number_unique"))
	g.Expect(sql).To(ContainSubstring("canonical_checksum TEXT NOT NULL"))
	g.Expect(sql).To(ContainSubstring("canonical_payload BYTEA NOT NULL"))
	g.Expect(sql).To(ContainSubstring("channel_id_application_organization_unique"))
	g.Expect(sql).To(ContainSubstring("releasebundle_channel_application_organization_fk"))
	g.Expect(sql).To(ContainSubstring("FOREIGN KEY (channel_id, application_id, organization_id)"))
	g.Expect(sql).To(ContainSubstring("CREATE TABLE ReleaseBundleComponent"))
	g.Expect(sql).To(ContainSubstring("releasebundlecomponent_bundle_key_unique"))

	up113, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "113_release_bundle_publication.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	publicationSQL := string(up113)
	g.Expect(publicationSQL).To(ContainSubstring(
		"published_by_user_account_id UUID REFERENCES UserAccount(id) ON DELETE SET NULL",
	))
	g.Expect(publicationSQL).To(ContainSubstring("published_at TIMESTAMP"))
	g.Expect(publicationSQL).To(ContainSubstring("CREATE TABLE ReleaseBundleAuditEvent"))
	g.Expect(publicationSQL).To(ContainSubstring("releasebundleauditevent_type_check"))

	down, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "112_release_bundles.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(down)).To(ContainSubstring("DROP TABLE IF EXISTS ReleaseBundleComponent"))
	g.Expect(string(down)).To(ContainSubstring("DROP TABLE IF EXISTS ReleaseBundle"))
	g.Expect(string(down)).To(ContainSubstring("DROP CONSTRAINT IF EXISTS channel_id_application_organization_unique"))

	down113, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "113_release_bundle_publication.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(down113)).To(ContainSubstring("DROP TABLE IF EXISTS ReleaseBundleAuditEvent"))
	g.Expect(string(down113)).To(ContainSubstring("DROP COLUMN published_by_user_account_id"))

	up114, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "114_release_bundle_ci_idempotency.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	ciSQL := string(up114)
	g.Expect(ciSQL).To(ContainSubstring("ALTER TABLE ReleaseBundle"))
	g.Expect(ciSQL).To(ContainSubstring("ADD COLUMN source_repository TEXT NOT NULL DEFAULT ''"))
	g.Expect(ciSQL).To(ContainSubstring("CREATE TABLE ReleaseBundleIdempotencyKey"))
	g.Expect(ciSQL).To(ContainSubstring("organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE"))
	g.Expect(ciSQL).To(ContainSubstring("key_hash TEXT NOT NULL"))
	g.Expect(ciSQL).To(ContainSubstring("request_checksum TEXT NOT NULL"))
	g.Expect(ciSQL).To(ContainSubstring("release_bundle_id UUID NOT NULL REFERENCES ReleaseBundle(id) ON DELETE RESTRICT"))
	g.Expect(ciSQL).To(ContainSubstring("releasebundleidempotencykey_organization_key_unique"))

	down114, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "114_release_bundle_ci_idempotency.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(down114)).To(ContainSubstring("DROP TABLE IF EXISTS ReleaseBundleIdempotencyKey"))
	g.Expect(string(down114)).To(ContainSubstring(
		"string_agg(component_payload, ',' ORDER BY component_key, component_id)",
	))
	g.Expect(string(down114)).To(ContainSubstring("pg_temp.release_bundle_go_json_string"))
	g.Expect(string(down114)).To(ContainSubstring("sha256(repaired.canonical_payload)"))
	g.Expect(string(down114)).To(ContainSubstring("DROP COLUMN IF EXISTS source_repository"))

	up116, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "116_process_snapshots.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	processSnapshotSQL := string(up116)
	g.Expect(processSnapshotSQL).To(ContainSubstring("CREATE TABLE ProcessSnapshot"))
	g.Expect(processSnapshotSQL).To(ContainSubstring("deployment_process_revision_id UUID NOT NULL"))
	g.Expect(processSnapshotSQL).To(ContainSubstring("canonical_checksum TEXT NOT NULL"))
	g.Expect(processSnapshotSQL).To(ContainSubstring("canonical_payload BYTEA NOT NULL"))
	g.Expect(processSnapshotSQL).To(ContainSubstring("processsnapshot_revision_unique"))
	g.Expect(processSnapshotSQL).To(ContainSubstring("ADD COLUMN process_snapshot_id UUID"))
	g.Expect(processSnapshotSQL).To(ContainSubstring("FOREIGN KEY (process_snapshot_id, application_id, organization_id)"))

	down116, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "116_process_snapshots.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(down116)).To(ContainSubstring("DROP COLUMN IF EXISTS process_snapshot_id"))
	g.Expect(string(down116)).To(ContainSubstring("DROP TABLE IF EXISTS ProcessSnapshot"))
}

func TestReleaseBundleDowngradeRepairsSourceMetadataCanonicalPayload(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleDependencies(t, ctx)
	goEscapedChars := "<>&" + string(rune(0x2028)) + string(rune(0x2029))
	bundle := releaseBundleFixture(orgID, applicationID, channelID, versionID)
	bundle.ReleaseNotes = "Initial release " + goEscapedChars
	bundle.SourceRevision = "abc123-" + goEscapedChars
	bundle.SourceRepository = `https://example.invalid/{org}/"project"`
	bundle.SourceBranch = `release/{2026}/"candidate"`
	bundle.SourceTag = `v1.2.3-{candidate}`
	bundle.CIProvider = `generic-ci\windows`
	bundle.CIRunID = `run-"123"`
	bundle.CIRunURL = `https://ci.example.invalid/runs/{123}?q="yes"`
	bundle.Components[0].Name = "API " + goEscapedChars
	bundle.Components[0].PackageRef = "registry.example.invalid/org/api?" + goEscapedChars
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())
	g.Expect(string(bundle.CanonicalPayload)).To(ContainSubstring("sourceMetadata"))

	expected := bundle
	expected.SourceRepository = ""
	expected.SourceBranch = ""
	expected.SourceTag = ""
	expected.CIProvider = ""
	expected.CIRunID = ""
	expected.CIRunURL = ""
	expectedPayload, expectedChecksum, err := releasebundles.Canonicalize(expected)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(expectedPayload)).NotTo(ContainSubstring("sourceMetadata"))

	down, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "114_release_bundle_ci_idempotency.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	_, err = internalctx.GetDb(ctx).Exec(ctx, string(down))
	g.Expect(err).NotTo(HaveOccurred())

	var repairedPayload []byte
	var repairedChecksum string
	g.Expect(internalctx.GetDb(ctx).QueryRow(
		ctx,
		`SELECT canonical_payload, canonical_checksum FROM ReleaseBundle WHERE id = @id`,
		pgx.NamedArgs{"id": bundle.ID},
	).Scan(&repairedPayload, &repairedChecksum)).To(Succeed())

	g.Expect(string(repairedPayload)).NotTo(ContainSubstring("sourceMetadata"))
	g.Expect(repairedPayload).To(Equal(expectedPayload))
	g.Expect(repairedChecksum).To(Equal(expectedChecksum))
}

func TestReleaseBundleDowngradeRepairsProcessSnapshotCanonicalPayload(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleDependencies(t, ctx)
	_, revision := createReleaseBundleProcessRevision(t, ctx, orgID, applicationID, "Standard deploy")
	bundle := releaseBundleFixture(orgID, applicationID, channelID, versionID)
	bundle.SourceRepository = "https://example.invalid/org/project"
	bundle.SourceBranch = "main"
	bundle.DeploymentProcessRevisionID = &revision.ID
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())
	g.Expect(bundle.ProcessSnapshotID).NotTo(BeNil())
	g.Expect(string(bundle.CanonicalPayload)).To(ContainSubstring("processSnapshotId"))

	expected := bundle
	expected.ProcessSnapshotID = nil
	expected.DeploymentProcessRevisionID = nil
	expectedPayload, expectedChecksum, err := releasebundles.Canonicalize(expected)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(expectedPayload)).NotTo(ContainSubstring("processSnapshotId"))

	down, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "116_process_snapshots.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	_, err = internalctx.GetDb(ctx).Exec(ctx, string(down))
	g.Expect(err).NotTo(HaveOccurred())

	var repairedPayload []byte
	var repairedChecksum string
	g.Expect(internalctx.GetDb(ctx).QueryRow(
		ctx,
		`SELECT canonical_payload, canonical_checksum FROM ReleaseBundle WHERE id = @id`,
		pgx.NamedArgs{"id": bundle.ID},
	).Scan(&repairedPayload, &repairedChecksum)).To(Succeed())

	g.Expect(string(repairedPayload)).NotTo(ContainSubstring("processSnapshotId"))
	g.Expect(repairedPayload).To(Equal(expectedPayload))
	g.Expect(repairedChecksum).To(Equal(expectedChecksum))
}

//nolint:dupl
func releaseBundleDBTestContext(t *testing.T) context.Context {
	t.Helper()
	databaseURL := os.Getenv("DISTR_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DISTR_TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(adminPool.Close)

	schema := "release_bundle_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := adminPool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+quotedSchema+" CASCADE"); err != nil {
			t.Logf("drop test schema: %v", err)
		}
	})

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse test database url: %v", err)
	}
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+quotedSchema)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("connect to isolated test schema: %v", err)
	}
	t.Cleanup(pool.Close)

	runReleaseBundleTestMigrations(t, ctx, pool)
	return internalctx.WithDb(ctx, pool)
}

func runReleaseBundleTestMigrations(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "migrations", "sql", "*.up.sql"))
	if err != nil {
		t.Fatalf("list migration files: %v", err)
	}
	sort.Slice(files, func(i, j int) bool {
		return releaseBundleMigrationVersion(t, files[i]) < releaseBundleMigrationVersion(t, files[j])
	})
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read migration %s: %v", file, err)
		}
		if _, err := pool.Exec(ctx, string(data)); err != nil {
			t.Fatalf("run migration %s: %v", file, err)
		}
	}
}

func releaseBundleMigrationVersion(t *testing.T, file string) int {
	t.Helper()
	base := filepath.Base(file)
	version, err := strconv.Atoi(strings.SplitN(base, "_", 2)[0])
	if err != nil {
		t.Fatalf("parse migration version %s: %v", file, err)
	}
	return version
}

func createReleaseBundleDependencies(t *testing.T, ctx context.Context) (uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	orgID := createReleaseBundleTestOrganization(t, ctx)
	applicationID, channelID, versionID := createReleaseBundleDependenciesForOrganization(t, ctx, orgID)
	return orgID, applicationID, channelID, versionID
}

func createReleaseBundleDependenciesForOrganization(
	t *testing.T,
	ctx context.Context,
	orgID uuid.UUID,
) (uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	return createReleaseBundleDependenciesForOrganizationWithRules(t, ctx, orgID, nil, nil, nil, nil)
}

func createReleaseBundleDependenciesForOrganizationWithRules(
	t *testing.T,
	ctx context.Context,
	orgID uuid.UUID,
	ranges []string,
	prereleasePatterns []string,
	sourceBranchPatterns []string,
	sourceTagPatterns []string,
) (uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	application := types.Application{
		Name: "Application " + uuid.NewString(),
		Type: types.DeploymentTypeDocker,
	}
	if err := db.CreateApplication(ctx, &application, orgID); err != nil {
		t.Fatalf("create application: %v", err)
	}
	version := types.ApplicationVersion{
		Name:            "1.2.3",
		ApplicationID:   application.ID,
		LinkTemplate:    "https://example.com/{{.version}}",
		ComposeFileData: []byte("services: {}\n"),
	}
	if err := db.CreateApplicationVersion(ctx, &version); err != nil {
		t.Fatalf("create application version: %v", err)
	}
	var lifecycleID uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO Lifecycle (organization_id, name) VALUES (@organizationId, @name) RETURNING id`,
		pgx.NamedArgs{"organizationId": orgID, "name": "Lifecycle " + uuid.NewString()},
	).Scan(&lifecycleID); err != nil {
		t.Fatalf("create lifecycle: %v", err)
	}
	channel := types.Channel{
		OrganizationID:              orgID,
		ApplicationID:               application.ID,
		LifecycleID:                 lifecycleID,
		Name:                        "Stable",
		IsDefault:                   true,
		AllowedVersionRanges:        ranges,
		AllowedPrereleasePatterns:   prereleasePatterns,
		AllowedSourceBranchPatterns: sourceBranchPatterns,
		AllowedSourceTagPatterns:    sourceTagPatterns,
	}
	if err := db.CreateChannel(ctx, &channel); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	return application.ID, channel.ID, version.ID
}

type releaseBundleEligibilityDependencies struct {
	orgID             uuid.UUID
	applicationID     uuid.UUID
	channelID         uuid.UUID
	lifecycleID       uuid.UUID
	versionID         uuid.UUID
	devEnvironmentID  uuid.UUID
	prodEnvironmentID uuid.UUID
}

func createReleaseBundleEligibilityDependencies(
	t *testing.T,
	ctx context.Context,
) releaseBundleEligibilityDependencies {
	t.Helper()
	orgID := createReleaseBundleTestOrganization(t, ctx)
	application := types.Application{
		Name: "Application " + uuid.NewString(),
		Type: types.DeploymentTypeDocker,
	}
	if err := db.CreateApplication(ctx, &application, orgID); err != nil {
		t.Fatalf("create application: %v", err)
	}
	version := types.ApplicationVersion{
		Name:            "1.2.3",
		ApplicationID:   application.ID,
		LinkTemplate:    "https://example.com/{{.version}}",
		ComposeFileData: []byte("services: {}\n"),
	}
	if err := db.CreateApplicationVersion(ctx, &version); err != nil {
		t.Fatalf("create application version: %v", err)
	}
	devEnvironment := types.Environment{
		OrganizationID: orgID,
		Name:           "Development",
		SortOrder:      10,
	}
	if err := db.CreateEnvironment(ctx, &devEnvironment); err != nil {
		t.Fatalf("create development environment: %v", err)
	}
	prodEnvironment := types.Environment{
		OrganizationID: orgID,
		Name:           "Production",
		SortOrder:      20,
		IsProduction:   true,
	}
	if err := db.CreateEnvironment(ctx, &prodEnvironment); err != nil {
		t.Fatalf("create production environment: %v", err)
	}
	lifecycleModel := types.Lifecycle{
		OrganizationID: orgID,
		Name:           "Lifecycle " + uuid.NewString(),
		Phases: []types.LifecyclePhase{
			{
				Name:                         "Development",
				SortOrder:                    10,
				EnvironmentIDs:               []uuid.UUID{devEnvironment.ID},
				MinimumSuccessfulDeployments: 1,
			},
			{
				Name:                         "Production",
				SortOrder:                    20,
				EnvironmentIDs:               []uuid.UUID{prodEnvironment.ID},
				MinimumSuccessfulDeployments: 1,
			},
		},
	}
	if err := db.CreateLifecycle(ctx, &lifecycleModel); err != nil {
		t.Fatalf("create lifecycle: %v", err)
	}
	channel := types.Channel{
		OrganizationID: orgID,
		ApplicationID:  application.ID,
		LifecycleID:    lifecycleModel.ID,
		Name:           "Stable",
		IsDefault:      true,
	}
	if err := db.CreateChannel(ctx, &channel); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	return releaseBundleEligibilityDependencies{
		orgID:             orgID,
		applicationID:     application.ID,
		channelID:         channel.ID,
		lifecycleID:       lifecycleModel.ID,
		versionID:         version.ID,
		devEnvironmentID:  devEnvironment.ID,
		prodEnvironmentID: prodEnvironment.ID,
	}
}

func createReleaseBundleTestUser(t *testing.T, ctx context.Context, orgID uuid.UUID) uuid.UUID {
	t.Helper()
	var userID uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO UserAccount (email) VALUES (@email) RETURNING id`,
		pgx.NamedArgs{"email": "release-bundle-" + uuid.NewString() + "@example.com"},
	).Scan(&userID); err != nil {
		t.Fatalf("create user account: %v", err)
	}
	if _, err := internalctx.GetDb(ctx).Exec(
		ctx,
		`INSERT INTO Organization_UserAccount (organization_id, user_account_id, user_role)
		VALUES (@organizationId, @userId, 'admin')`,
		pgx.NamedArgs{"organizationId": orgID, "userId": userID},
	); err != nil {
		t.Fatalf("create organization user account: %v", err)
	}
	return userID
}

func createReleaseBundleTestOrganization(t *testing.T, ctx context.Context) uuid.UUID {
	t.Helper()
	var orgID uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO Organization (name) VALUES (@name) RETURNING id`,
		pgx.NamedArgs{"name": "Organization " + uuid.NewString()},
	).Scan(&orgID); err != nil {
		t.Fatalf("create organization: %v", err)
	}
	return orgID
}

func releaseBundleFixture(
	orgID uuid.UUID,
	applicationID uuid.UUID,
	channelID uuid.UUID,
	versionID uuid.UUID,
) types.ReleaseBundle {
	return types.ReleaseBundle{
		OrganizationID: orgID,
		ApplicationID:  applicationID,
		ChannelID:      channelID,
		ReleaseNumber:  "2026.06.20",
		ReleaseNotes:   "Initial release",
		SourceRevision: "abc123",
		Components: []types.ReleaseBundleComponent{
			{
				Key:                  "api",
				Name:                 "API",
				Type:                 types.ReleaseBundleComponentTypeApplicationVersion,
				Version:              "1.2.3",
				ApplicationVersionID: &versionID,
			},
		},
	}
}

func ociReleaseBundleFixture(
	orgID uuid.UUID,
	applicationID uuid.UUID,
	channelID uuid.UUID,
) types.ReleaseBundle {
	return types.ReleaseBundle{
		OrganizationID: orgID,
		ApplicationID:  applicationID,
		ChannelID:      channelID,
		ReleaseNumber:  "2026.06.20",
		ReleaseNotes:   "Initial release",
		SourceRevision: "abc123",
		Components: []types.ReleaseBundleComponent{
			{
				Key:        "api-image",
				Name:       "API image",
				Type:       types.ReleaseBundleComponentTypeOCIImage,
				Version:    "1.2.3",
				PackageRef: "registry.example.invalid/org/api",
				Digest:     "sha256:" + strings.Repeat("a", 64),
			},
		},
	}
}

func createReleaseBundleProcessRevision(
	t *testing.T,
	ctx context.Context,
	orgID uuid.UUID,
	applicationID uuid.UUID,
	name string,
) (types.DeploymentProcess, types.DeploymentProcessRevision) {
	t.Helper()
	process := types.DeploymentProcess{
		OrganizationID: orgID,
		ApplicationID:  applicationID,
		Name:           name + " " + uuid.NewString(),
	}
	if err := db.CreateDeploymentProcess(ctx, &process); err != nil {
		t.Fatalf("create deployment process: %v", err)
	}
	revision := types.DeploymentProcessRevision{
		OrganizationID:      orgID,
		DeploymentProcessID: process.ID,
		Description:         "Initial revision",
		Steps: []types.DeploymentProcessStep{
			{
				Key:                  "deploy",
				Name:                 "Deploy",
				ActionType:           "script",
				ExecutionLocation:    "hub",
				InputBindings:        map[string]any{"script": "make deploy"},
				FailureMode:          "fail",
				TimeoutSeconds:       120,
				RetryMaxAttempts:     3,
				RetryIntervalSeconds: 10,
				RequiredPermissions:  []string{"deploy:write"},
				SortOrder:            10,
			},
		},
	}
	if err := db.CreateDeploymentProcessRevision(ctx, &revision); err != nil {
		t.Fatalf("create deployment process revision: %v", err)
	}
	return process, revision
}

func markReleaseBundleStatusForTest(
	t *testing.T,
	ctx context.Context,
	id uuid.UUID,
	status types.ReleaseBundleStatus,
) {
	t.Helper()
	if _, err := internalctx.GetDb(ctx).Exec(
		ctx,
		`UPDATE ReleaseBundle SET status = @status WHERE id = @id`,
		pgx.NamedArgs{"id": id, "status": status},
	); err != nil {
		t.Fatalf("mark release bundle status: %v", err)
	}
}

func releaseBundleStatusPtr(status types.ReleaseBundleStatus) *types.ReleaseBundleStatus {
	return &status
}
