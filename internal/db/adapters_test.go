package db

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

type recordingAdapterAuditHook struct {
	events []types.ControlPlaneAuditEventInput
	errAt  int
}

func (hook *recordingAdapterAuditHook) AppendControlPlaneAuditEvent(
	_ context.Context,
	event types.ControlPlaneAuditEventInput,
) error {
	hook.events = append(hook.events, event)
	if hook.errAt > 0 && len(hook.events) == hook.errAt {
		return errors.New("audit unavailable")
	}
	return nil
}

func TestAdapterPublicationAuditEventsAreCorrelatedAndExcludeSecretReferences(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	implementation := types.AdapterImplementation{
		ID: uuid.New(), OrganizationID: organizationID, Key: "jenkins", Name: "Jenkins",
		Version: "2.0.0", Enabled: true,
		Capabilities: []types.AdapterCapability{{Capability: "deploy", Version: "v2"}},
	}
	implementationEvent, err := adapterImplementationPublishedAuditEvent(implementation)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(validateControlPlaneAuditEventInput(implementationEvent)).To(Succeed())
	g.Expect(implementationEvent.EventType).To(Equal("adapter.implementation.published"))
	g.Expect(implementationEvent.AdapterRevisionID).NotTo(BeNil())
	g.Expect(*implementationEvent.AdapterRevisionID).To(Equal(implementation.ID))

	assignment := types.AdapterAssignment{
		ID: uuid.New(), OrganizationID: organizationID,
		AdapterImplementationID: implementation.ID,
		ScopeType:               types.AdapterScopeDeploymentTarget, ScopeReference: uuid.NewString(),
		ConfigSnapshotID: uuid.New(), ConfigChecksum: "sha256:" + strings.Repeat("a", 64),
		KeyConfiguration: types.AdapterKeyConfiguration{
			KeyID: "key-1", PublicKeyFingerprint: "sha256:public",
			SigningKeyReference:          "secret://adapter/private-key",
			SigningKeyVersionFingerprint: "sha256:key-version",
		},
		Enabled: true,
	}
	assignmentEvent, err := adapterAssignmentPublishedAuditEvent(assignment)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(validateControlPlaneAuditEventInput(assignmentEvent)).To(Succeed())
	g.Expect(assignmentEvent.EventType).To(Equal("adapter.assignment.published"))
	g.Expect(assignmentEvent.AdapterRevisionID).NotTo(BeNil())
	g.Expect(*assignmentEvent.AdapterRevisionID).To(Equal(assignment.ID))
	g.Expect(assignmentEvent.TargetConfigID).NotTo(BeNil())
	g.Expect(*assignmentEvent.TargetConfigID).To(Equal(assignment.ConfigSnapshotID))
	g.Expect(assignmentEvent.TargetConfigChecksum).To(Equal(assignment.ConfigChecksum))
	g.Expect(string(assignmentEvent.Payload)).NotTo(ContainSubstring("secret://adapter/private-key"))
}

func TestFrozenAdapterSelectionAuditStopsOnFailure(t *testing.T) {
	g := NewWithT(t)
	plan := types.DeploymentPlan{
		ID: uuid.New(), OrganizationID: uuid.New(),
		CanonicalChecksum: "sha256:" + strings.Repeat("b", 64),
		StepAdapters: []types.DeploymentPlanStepAdapter{
			{ID: uuid.New(), StepKey: "deploy", AdapterAssignmentID: uuid.New(), AdapterImplementationID: uuid.New()},
			{ID: uuid.New(), StepKey: "verify", AdapterAssignmentID: uuid.New(), AdapterImplementationID: uuid.New()},
		},
	}
	hook := &recordingAdapterAuditHook{errAt: 1}

	err := recordFrozenAdapterSelectionAudit(context.Background(), hook, plan)

	g.Expect(err).To(MatchError("audit unavailable"))
	g.Expect(hook.events).To(HaveLen(1))
	g.Expect(hook.events[0].EventType).To(Equal("adapter.revision.selected"))
	g.Expect(hook.events[0].DeploymentPlanID).NotTo(BeNil())
	g.Expect(*hook.events[0].DeploymentPlanID).To(Equal(plan.ID))
	g.Expect(hook.events[0].AdapterRevisionID).NotTo(BeNil())
	g.Expect(*hook.events[0].AdapterRevisionID).To(Equal(plan.StepAdapters[0].ID))
}

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
