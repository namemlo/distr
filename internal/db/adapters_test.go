package db

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
)

func TestAdapterAssignmentMigrationFreezesVersionConfigAndKeyFingerprints(t *testing.T) {
	g := NewWithT(t)
	up, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "156_adapter_assignments.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	sql := string(up)
	g.Expect(sql).To(ContainSubstring("CREATE TABLE AdapterImplementation"))
	g.Expect(sql).To(ContainSubstring("CREATE TABLE AdapterCapability"))
	g.Expect(sql).To(ContainSubstring("CREATE TABLE AdapterAssignment"))
	g.Expect(sql).To(ContainSubstring("CREATE TABLE DeploymentPlanStepAdapter"))
	g.Expect(sql).To(ContainSubstring("implementation_version"))
	g.Expect(sql).To(ContainSubstring("config_checksum"))
	g.Expect(sql).To(ContainSubstring("public_key_fingerprint"))
	g.Expect(sql).To(ContainSubstring("signing_key_version_fingerprint"))
	g.Expect(sql).To(ContainSubstring("DeploymentPlanStepAdapter_append_only"))

	down, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "156_adapter_assignments.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(down)).To(ContainSubstring("refusing migration 156 rollback"))
}

func TestAdapterAssignmentRepositoryValidatesOrganizationScopedTargets(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("adapters.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)
	g.Expect(text).To(ContainSubstring("requireAdapterScope"))
	g.Expect(text).To(ContainSubstring("FROM DeploymentTarget"))
	g.Expect(text).To(ContainSubstring("FROM DeploymentUnit"))
	g.Expect(text).To(ContainSubstring("FROM ComponentInstance"))
	g.Expect(text).To(ContainSubstring("organization_id = @organizationID"))
}
