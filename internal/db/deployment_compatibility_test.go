package db_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestBackfillLegacyDeploymentCompatibilityDryRunApplyAndIdempotency(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, _, _, versionID := createReleaseBundleDependencies(t, ctx)
	targetID := createReleaseBundleDockerTargetForOrganization(t, ctx, orgID, "legacy-target")
	request := api.DeploymentRequest{
		DeploymentTargetID:   targetID,
		ApplicationVersionID: versionID,
		ValuesYaml:           []byte("password: super-secret\n"),
		EnvFileData:          []byte("TOKEN=super-secret\n"),
		ValuesHash:           []byte("stored-values-hash"),
	}
	g.Expect(db.CreateDeployment(ctx, &request)).To(Succeed())
	revision, err := db.CreateDeploymentRevision(ctx, &request)
	g.Expect(err).NotTo(HaveOccurred())

	dryRun, err := db.BackfillLegacyDeploymentCompatibility(ctx, types.DeploymentCompatibilityBackfillRequest{
		OrganizationID: orgID,
		BatchSize:      10,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(dryRun.Scanned).To(Equal(1))
	g.Expect(dryRun.Eligible).To(Equal(1))
	g.Expect(dryRun.Projected).To(Equal(1))
	g.Expect(dryRun.AlreadyPresent).To(Equal(0))
	g.Expect(dryRun.Failed).To(Equal(0))
	_, err = db.GetDeploymentCompatibilityByRevision(ctx, orgID, revision.ID)
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())

	applied, err := db.BackfillLegacyDeploymentCompatibility(ctx, types.DeploymentCompatibilityBackfillRequest{
		OrganizationID: orgID,
		Apply:          true,
		BatchSize:      10,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(applied.Projected).To(Equal(1))
	g.Expect(applied.AlreadyPresent).To(Equal(0))
	g.Expect(applied.LastCursor).NotTo(BeNil())

	metadata, err := db.GetDeploymentCompatibilityByRevision(ctx, orgID, revision.ID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(metadata.OrganizationID).To(Equal(orgID))
	g.Expect(metadata.LegacyDeploymentID).To(Equal(*request.DeploymentID))
	g.Expect(metadata.LegacyDeploymentRevisionID).To(Equal(revision.ID))
	g.Expect(metadata.Source).To(Equal(types.DeploymentCompatibilitySourceLegacyDirectDeployment))
	g.Expect(metadata.CanonicalChecksum).To(HavePrefix("sha256:"))
	g.Expect(string(metadata.CanonicalPayload)).NotTo(ContainSubstring("super-secret"))

	reapplied, err := db.BackfillLegacyDeploymentCompatibility(ctx, types.DeploymentCompatibilityBackfillRequest{
		OrganizationID: orgID,
		Apply:          true,
		BatchSize:      10,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(reapplied.Projected).To(Equal(0))
	g.Expect(reapplied.AlreadyPresent).To(Equal(1))
	reloaded, err := db.GetDeploymentCompatibilityByRevision(ctx, orgID, revision.ID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(reloaded.CanonicalChecksum).To(Equal(metadata.CanonicalChecksum))
}

func TestBackfillLegacyDeploymentCompatibilityProcessesMultipleBatchesAndCanResume(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, _, _, versionID := createReleaseBundleDependencies(t, ctx)
	targetID := createReleaseBundleDockerTargetForOrganization(t, ctx, orgID, "legacy-target")
	revisions := make([]types.DeploymentRevision, 0, 3)
	for i := 0; i < 3; i++ {
		_, revision := createLegacyDeploymentRevisionForTimelineTest(t, ctx, targetID, versionID, "stored-values-hash")
		revisions = append(revisions, revision)
	}

	applied, err := db.BackfillLegacyDeploymentCompatibility(ctx, types.DeploymentCompatibilityBackfillRequest{
		OrganizationID: orgID,
		Apply:          true,
		BatchSize:      1,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(applied.Scanned).To(Equal(3))
	g.Expect(applied.Projected).To(Equal(3))
	g.Expect(applied.AlreadyPresent).To(Equal(0))
	g.Expect(applied.LastCursor).NotTo(BeNil())
	for _, revision := range revisions {
		_, err := db.GetDeploymentCompatibilityByRevision(ctx, orgID, revision.ID)
		g.Expect(err).NotTo(HaveOccurred())
	}

	reapplied, err := db.BackfillLegacyDeploymentCompatibility(ctx, types.DeploymentCompatibilityBackfillRequest{
		OrganizationID: orgID,
		Apply:          true,
		BatchSize:      1,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(reapplied.Scanned).To(Equal(3))
	g.Expect(reapplied.Projected).To(Equal(0))
	g.Expect(reapplied.AlreadyPresent).To(Equal(3))

	resumedFromEnd, err := db.BackfillLegacyDeploymentCompatibility(ctx, types.DeploymentCompatibilityBackfillRequest{
		OrganizationID: orgID,
		Apply:          true,
		BatchSize:      1,
		Cursor:         applied.LastCursor,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(resumedFromEnd.Scanned).To(Equal(0))
	g.Expect(resumedFromEnd.Projected).To(Equal(0))
}

func TestDeploymentCompatibilityMigrationDefinesReversibleAdditiveSchema(t *testing.T) {
	g := NewWithT(t)
	up, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "131_deployment_compatibility.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	upSQL := string(up)
	g.Expect(upSQL).To(ContainSubstring("CREATE TABLE DeploymentCompatibilityMetadata"))
	g.Expect(upSQL).To(ContainSubstring("legacy_deployment_revision_id UUID NOT NULL"))
	g.Expect(upSQL).To(ContainSubstring("canonical_checksum TEXT NOT NULL"))
	g.Expect(upSQL).To(ContainSubstring("UNIQUE (organization_id, legacy_deployment_revision_id)"))
	g.Expect(upSQL).NotTo(ContainSubstring("values_yaml"))
	g.Expect(upSQL).NotTo(ContainSubstring("env_file_data"))

	down, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "131_deployment_compatibility.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(down)).To(ContainSubstring("DROP TABLE IF EXISTS DeploymentCompatibilityMetadata"))
}
