package db

import (
	"os"
	"path/filepath"
	"strings"
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
	g.Expect(sql).To(ContainSubstring("scope_reference TEXT NOT NULL"))
	g.Expect(sql).NotTo(ContainSubstring("scope_id UUID NOT NULL"))
	g.Expect(sql).To(ContainSubstring("public_key_fingerprint"))
	g.Expect(sql).To(ContainSubstring("signing_key_version_fingerprint"))
	g.Expect(sql).To(ContainSubstring("DeploymentPlanStepAdapter_append_only"))

	down, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "156_adapter_assignments.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(down)).To(ContainSubstring("refusing migration 156 rollback"))
}

func TestAdapterAssignmentDowngradeLocksBeforeRetainedDataCheck(t *testing.T) {
	g := NewWithT(t)
	down, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "156_adapter_assignments.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())

	sql := string(down)
	lockIndex := strings.Index(sql, "LOCK TABLE")
	guardIndex := strings.Index(sql, "DO $$")
	g.Expect(lockIndex).To(BeNumerically(">=", 0))
	g.Expect(guardIndex).To(BeNumerically(">", lockIndex))
	for _, table := range []string{
		"DeploymentPlanStepAdapter",
		"AdapterAssignment",
		"AdapterCapability",
		"AdapterImplementation",
		"DeploymentPlanStep",
	} {
		g.Expect(sql[lockIndex:guardIndex]).To(ContainSubstring(table))
	}
}

func TestAdapterAssignmentDowngradeRefusesAnyAdapterData(t *testing.T) {
	g := NewWithT(t)
	down, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "156_adapter_assignments.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())

	sql := string(down)
	guardStart := strings.Index(sql, "DO $$")
	guardEnd := strings.Index(sql[guardStart:], "$$;")
	g.Expect(guardStart).To(BeNumerically(">=", 0))
	g.Expect(guardEnd).To(BeNumerically(">", 0))
	guard := sql[guardStart : guardStart+guardEnd]
	for _, table := range []string{
		"DeploymentPlanStepAdapter",
		"AdapterAssignment",
		"AdapterCapability",
		"AdapterImplementation",
	} {
		g.Expect(guard).To(ContainSubstring("SELECT 1 FROM " + table))
	}
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
	g.Expect(text).To(ContainSubstring("FROM ObserverRegistration"))
	g.Expect(text).To(ContainSubstring("validateDatabaseResourceReference"))
	g.Expect(text).To(ContainSubstring("organization_id = @organizationID"))
}

func TestAdapterImplementationListsBatchLoadCapabilities(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("adapters.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)

	g.Expect(text).To(ContainSubstring("getAdapterCapabilitiesByImplementation"))
	g.Expect(text).To(ContainSubstring("adapter_implementation_id = ANY(@implementationIDs)"))
	g.Expect(text).NotTo(ContainSubstring("func getAdapterCapabilities("))
}
