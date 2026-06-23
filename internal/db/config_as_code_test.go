package db_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestConfigAsCodeAuthorityRepositoryDefaultsAndUpserts(t *testing.T) {
	ctx := channelDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, lifecycleID := createChannelDependencies(t, ctx)
	channel := types.Channel{
		OrganizationID: orgID,
		ApplicationID:  applicationID,
		LifecycleID:    lifecycleID,
		Name:           "Stable",
	}
	g.Expect(db.CreateChannel(ctx, &channel)).To(Succeed())

	defaultAuthority, err := db.GetConfigAsCodeAuthority(
		ctx,
		orgID,
		types.ConfigAsCodeResourceKindChannel,
		channel.ID,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(defaultAuthority.Authority).To(Equal(types.ConfigAsCodeAuthorityDatabaseManaged))
	g.Expect(defaultAuthority.RepositoryPath).To(BeEmpty())

	actorID := uuid.New()
	expected := types.ConfigAsCodeAuthority{
		OrganizationID:   orgID,
		ResourceKind:     types.ConfigAsCodeResourceKindChannel,
		ResourceID:       channel.ID,
		Authority:        types.ConfigAsCodeAuthorityGitManaged,
		RepositoryPath:   "channels/stable.yaml",
		SourceRevision:   "abc123",
		DocumentChecksum: "1111111111111111111111111111111111111111111111111111111111111111",
		UpdatedByUserID:  &actorID,
	}
	g.Expect(db.UpsertConfigAsCodeAuthority(ctx, &expected)).To(Succeed())
	g.Expect(db.UpsertConfigAsCodeAuthority(ctx, &expected)).To(Succeed())

	loaded, err := db.GetConfigAsCodeAuthority(
		ctx,
		orgID,
		types.ConfigAsCodeResourceKindChannel,
		channel.ID,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(loaded.Authority).To(Equal(types.ConfigAsCodeAuthorityGitManaged))
	g.Expect(loaded.RepositoryPath).To(Equal("channels/stable.yaml"))
	g.Expect(loaded.SourceRevision).To(Equal("abc123"))
	g.Expect(loaded.DocumentChecksum).To(Equal(expected.DocumentChecksum))
	g.Expect(loaded.UpdatedByUserID).To(Equal(expected.UpdatedByUserID))
	g.Expect(loaded.UpdatedAt.IsZero()).To(BeFalse())

	authorities, err := db.GetConfigAsCodeAuthorities(ctx, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(authorities).To(HaveLen(1))
	g.Expect(authorities[0].ResourceID).To(Equal(channel.ID))
}

func TestConfigAsCodeAuthorityRepositoryIsOrganizationScoped(t *testing.T) {
	ctx := channelDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, lifecycleID := createChannelDependencies(t, ctx)
	otherOrgID, _, _ := createChannelDependencies(t, ctx)
	channel := types.Channel{
		OrganizationID: orgID,
		ApplicationID:  applicationID,
		LifecycleID:    lifecycleID,
		Name:           "Stable",
	}
	g.Expect(db.CreateChannel(ctx, &channel)).To(Succeed())

	_, err := db.GetConfigAsCodeAuthority(
		ctx,
		otherOrgID,
		types.ConfigAsCodeResourceKindChannel,
		channel.ID,
	)

	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
}

func TestEnsureConfigAsCodeDatabaseManagedForUpdateRejectsGitManagedResources(t *testing.T) {
	ctx := channelDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, lifecycleID := createChannelDependencies(t, ctx)
	channel := types.Channel{
		OrganizationID: orgID,
		ApplicationID:  applicationID,
		LifecycleID:    lifecycleID,
		Name:           "Stable",
	}
	g.Expect(db.CreateChannel(ctx, &channel)).To(Succeed())
	g.Expect(db.EnsureConfigAsCodeDatabaseManagedForUpdate(
		ctx,
		orgID,
		types.ConfigAsCodeResourceKindChannel,
		channel.ID,
	)).To(Succeed())

	g.Expect(db.UpsertConfigAsCodeAuthority(ctx, &types.ConfigAsCodeAuthority{
		OrganizationID:   orgID,
		ResourceKind:     types.ConfigAsCodeResourceKindChannel,
		ResourceID:       channel.ID,
		Authority:        types.ConfigAsCodeAuthorityGitManaged,
		RepositoryPath:   "channels/stable.yaml",
		SourceRevision:   "abc123",
		DocumentChecksum: "1111111111111111111111111111111111111111111111111111111111111111",
	})).To(Succeed())

	err := db.EnsureConfigAsCodeDatabaseManagedForUpdate(
		ctx,
		orgID,
		types.ConfigAsCodeResourceKindChannel,
		channel.ID,
	)

	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
}

func TestConfigAsCodeAuthorityMigrationDefinesAuthorityTable(t *testing.T) {
	g := NewWithT(t)

	up, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "130_config_as_code_authority.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	sql := string(up)

	g.Expect(sql).To(ContainSubstring("CREATE TABLE ConfigAsCodeAuthority"))
	g.Expect(sql).To(ContainSubstring("organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE"))
	g.Expect(sql).To(ContainSubstring("resource_kind TEXT NOT NULL"))
	g.Expect(sql).To(ContainSubstring("authority TEXT NOT NULL"))
	g.Expect(sql).To(ContainSubstring("CHECK (authority IN ('DATABASE_MANAGED', 'GIT_MANAGED'))"))
	g.Expect(sql).To(ContainSubstring("updated_at TIMESTAMP NOT NULL DEFAULT now()"))
	g.Expect(sql).To(ContainSubstring("configascodeauthority_resource_unique"))
	g.Expect(sql).To(ContainSubstring("CREATE TABLE ConfigAsCodeAuthorityAuditEvent"))
	g.Expect(sql).To(ContainSubstring("previous_authority TEXT NOT NULL"))
	g.Expect(sql).To(ContainSubstring("new_authority TEXT NOT NULL"))
	g.Expect(sql).To(ContainSubstring("actor_useraccount_id UUID REFERENCES UserAccount(id) ON DELETE SET NULL"))

	down, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "130_config_as_code_authority.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(down)).To(ContainSubstring("DROP TABLE IF EXISTS ConfigAsCodeAuthorityAuditEvent"))
	g.Expect(string(down)).To(ContainSubstring("DROP TABLE IF EXISTS ConfigAsCodeAuthority"))
}
