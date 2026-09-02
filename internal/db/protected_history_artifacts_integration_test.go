package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/protectedhistory"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	. "github.com/onsi/gomega"
)

type protectedHistoryMemoryObjectStore struct {
	mutex  sync.Mutex
	bytes  map[string][]byte
	writes int
}

func newProtectedHistoryMemoryObjectStore() *protectedHistoryMemoryObjectStore {
	return &protectedHistoryMemoryObjectStore{bytes: map[string][]byte{}}
}

func (store *protectedHistoryMemoryObjectStore) WriteOnce(
	_ context.Context,
	payload []byte,
) (protectedhistory.ObjectIdentity, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	identity := protectedhistory.ObjectIdentityForPayload("protected-history", payload)
	if existing, ok := store.bytes[identity.Reference]; ok {
		observed := protectedhistory.ObjectIdentityForPayload("protected-history", existing)
		observed.Reference = identity.Reference
		if err := protectedhistory.VerifyObjectIdentity(identity, observed); err != nil {
			return protectedhistory.ObjectIdentity{}, protectedhistory.ErrObjectConflict
		}
		return identity, nil
	}
	store.bytes[identity.Reference] = append([]byte(nil), payload...)
	store.writes++
	return identity, nil
}

func (store *protectedHistoryMemoryObjectStore) Readback(
	_ context.Context,
	expected protectedhistory.ObjectIdentity,
) (protectedhistory.ObjectIdentity, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	payload, ok := store.bytes[expected.Reference]
	if !ok {
		return protectedhistory.ObjectIdentity{}, protectedhistory.ErrObjectVerificationUnavailable
	}
	observed := protectedhistory.ObjectIdentityForPayload("protected-history", payload)
	observed.Reference = expected.Reference
	return observed, nil
}

func TestRetainProtectedHistoryArtifactBindsAuditAndReplaysIdempotently(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	organizationID := createReleaseBundleTestOrganization(t, ctx)
	issuerID := createReleaseBundleTestUser(t, ctx, organizationID)
	reviewerID := createReleaseBundleTestUser(t, ctx, organizationID)
	otherReviewerID := createReleaseBundleTestUser(t, ctx, organizationID)
	customerID := createProtectedHistoryCustomer(t, ctx, organizationID)
	store := newProtectedHistoryMemoryObjectStore()
	request := protectedhistory.CreateRetentionRequest{
		OrganizationID: organizationID,
		Scope: protectedhistory.Scope{
			OrganizationID:          organizationID.String(),
			CustomerOrganizationIDs: []string{customerID.String()},
		},
		IssuerUserAccountID:   issuerID,
		ReviewerUserAccountID: reviewerID,
		IdempotencyKey:        "before-upgrade",
	}
	capturedAt := time.Date(2026, time.September, 1, 8, 9, 10, 123456000, time.UTC)

	retained, err := db.RetainProtectedHistoryArtifact(ctx, request, store, capturedAt)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(protectedhistory.ValidateRetention(*retained)).To(Succeed())
	g.Expect(retained.AuditEventID).NotTo(Equal(uuid.Nil))
	g.Expect(retained.AuditEventSequence).To(BeNumerically(">", 0))
	g.Expect(retained.AuditBindingChecksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
	g.Expect(store.writes).To(Equal(1))

	var eventArtifactID uuid.UUID
	var eventSequence int64
	var eventType, eventDigest string
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
SELECT protected_history_artifact_id, sequence, event_type, artifact_digest
FROM ControlPlaneAuditEvent
WHERE id = @id AND organization_id = @organizationId
`, pgx.NamedArgs{
		"id": retained.AuditEventID, "organizationId": organizationID,
	}).Scan(&eventArtifactID, &eventSequence, &eventType, &eventDigest)).To(Succeed())
	g.Expect(eventArtifactID).To(Equal(retained.ID))
	g.Expect(eventSequence).To(Equal(retained.AuditEventSequence))
	g.Expect(eventType).To(Equal("protected_history.retained"))
	g.Expect(eventDigest).To(Equal(retained.ContentChecksum))

	replayed, err := db.RetainProtectedHistoryArtifact(
		ctx,
		request,
		store,
		capturedAt.Add(time.Hour),
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(replayed).To(Equal(retained))
	g.Expect(store.writes).To(Equal(1))

	changed := request
	changed.ReviewerUserAccountID = otherReviewerID
	_, err = db.RetainProtectedHistoryArtifact(ctx, changed, store, capturedAt)
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
}

func TestProtectedHistoryArtifactReadsAreOrganizationBoundAndNeverRewriteMetadata(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	organizationID := createReleaseBundleTestOrganization(t, ctx)
	issuerID := createReleaseBundleTestUser(t, ctx, organizationID)
	reviewerID := createReleaseBundleTestUser(t, ctx, organizationID)
	customerID := createProtectedHistoryCustomer(t, ctx, organizationID)
	store := newProtectedHistoryMemoryObjectStore()
	retained, err := db.RetainProtectedHistoryArtifact(
		ctx,
		protectedhistory.CreateRetentionRequest{
			OrganizationID: organizationID,
			Scope: protectedhistory.Scope{
				OrganizationID:          organizationID.String(),
				CustomerOrganizationIDs: []string{customerID.String()},
			},
			IssuerUserAccountID:   issuerID,
			ReviewerUserAccountID: reviewerID,
			IdempotencyKey:        "readback-without-rewrite",
		},
		store,
		time.Date(2026, time.September, 1, 8, 9, 10, 0, time.UTC),
	)
	g.Expect(err).NotTo(HaveOccurred())

	var xminBefore string
	var auditCountBefore int
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
SELECT xmin::TEXT FROM ProtectedHistoryArtifact WHERE id = @id
`, pgx.NamedArgs{"id": retained.ID}).Scan(&xminBefore)).To(Succeed())
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
SELECT count(*) FROM ControlPlaneAuditEvent WHERE protected_history_artifact_id = @id
`, pgx.NamedArgs{"id": retained.ID}).Scan(&auditCountBefore)).To(Succeed())

	metadata, err := db.GetProtectedHistoryArtifact(ctx, organizationID, retained.ID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(metadata).To(Equal(retained))
	identity, err := db.VerifyProtectedHistoryArtifact(ctx, organizationID, retained.ID, store)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(identity.Checksum).To(Equal(retained.ContentChecksum))

	var xminAfter string
	var auditCountAfter int
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
SELECT xmin::TEXT FROM ProtectedHistoryArtifact WHERE id = @id
`, pgx.NamedArgs{"id": retained.ID}).Scan(&xminAfter)).To(Succeed())
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
SELECT count(*) FROM ControlPlaneAuditEvent WHERE protected_history_artifact_id = @id
`, pgx.NamedArgs{"id": retained.ID}).Scan(&auditCountAfter)).To(Succeed())
	g.Expect(xminAfter).To(Equal(xminBefore))
	g.Expect(auditCountAfter).To(Equal(auditCountBefore))

	foreignOrganizationID := createReleaseBundleTestOrganization(t, ctx)
	_, err = db.GetProtectedHistoryArtifact(ctx, foreignOrganizationID, retained.ID)
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
}

func TestExportProtectedHistorySchema138SerializesRedactedVariableSnapshotRows(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	deps := createReleaseBundleEligibilityDependencies(t, ctx)
	_, revision := createReleaseBundleProcessRevision(
		t, ctx, deps.orgID, deps.applicationID, "Protected history export",
	)
	createDeploymentPlanVariableSet(t, ctx, deps.orgID, deps.applicationID)
	customerID := createProtectedHistoryCustomer(t, ctx, deps.orgID)
	targetID := createReleaseBundleDockerTargetForOrganization(t, ctx, deps.orgID, "protected-history-target")
	_, err := internalctx.GetDb(ctx).Exec(ctx, `
UPDATE DeploymentTarget
SET customer_organization_id = @customerOrganizationId
WHERE id = @targetId AND organization_id = @organizationId
`, pgx.NamedArgs{
		"customerOrganizationId": customerID,
		"targetId":               targetID,
		"organizationId":         deps.orgID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	bundle := releaseBundleFixture(deps.orgID, deps.applicationID, deps.channelID, deps.versionID)
	bundle.DeploymentProcessRevisionID = &revision.ID
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())
	published, result, err := db.PublishReleaseBundle(
		ctx, bundle.ID, deps.orgID, createReleaseBundleTestUser(t, ctx, deps.orgID),
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Valid).To(BeTrue())
	_, err = db.CreateDeploymentPlan(ctx, types.CreateDeploymentPlanRequest{
		OrganizationID:  deps.orgID,
		ReleaseBundleID: published.ID,
		EnvironmentID:   deps.devEnvironmentID,
		TargetIDs:       []uuid.UUID{targetID},
	})
	g.Expect(err).NotTo(HaveOccurred())

	_, err = internalctx.GetDb(ctx).Exec(ctx, `UPDATE schema_migrations SET version = 138, dirty = false`)
	g.Expect(err).NotTo(HaveOccurred())
	artifact, err := db.ExportProtectedHistory(ctx, protectedhistory.Scope{
		OrganizationID:          deps.orgID.String(),
		CustomerOrganizationIDs: []string{customerID.String()},
		DeploymentTargetIDs:     []string{targetID.String()},
	})
	g.Expect(err).NotTo(HaveOccurred())

	variableSnapshotRecords := 0
	planComponentRecords := 0
	for _, record := range artifact.Records {
		var payload map[string]any
		g.Expect(json.Unmarshal(record.Payload, &payload)).To(Succeed())
		switch record.Kind {
		case "variablesnapshotvalue":
			variableSnapshotRecords++
			g.Expect(payload).To(HaveKey("variable_snapshot_id"))
			g.Expect(string(record.Payload)).NotTo(ContainSubstring("secret-value"))
		case "deploymentplantargetcomponent":
			planComponentRecords++
			g.Expect(payload).To(HaveKey("deployment_plan_id"))
		}
	}
	g.Expect(variableSnapshotRecords).To(Equal(2))
	g.Expect(planComponentRecords).To(BeNumerically(">", 0))
}

func createProtectedHistoryCustomer(
	t testing.TB,
	ctx context.Context,
	organizationID uuid.UUID,
) uuid.UUID {
	t.Helper()
	var customerID uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(ctx, `
INSERT INTO CustomerOrganization (organization_id, name)
VALUES (@organizationId, @name)
RETURNING id
`, pgx.NamedArgs{
		"organizationId": organizationID,
		"name":           "Protected History Customer " + uuid.NewString(),
	}).Scan(&customerID); err != nil {
		t.Fatalf("create protected-history customer: %v", err)
	}
	return customerID
}
