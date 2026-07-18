package adapterresolution

import (
	"context"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestResolveStepAdapterFreezesExactReleaseCapabilityAndVersion(t *testing.T) {
	g := NewWithT(t)
	request, implementation, assignment := adapterResolutionFixture()

	resolved, issues := ResolveStepAdapter(context.Background(), request)

	g.Expect(issues).To(BeEmpty())
	g.Expect(resolved).NotTo(BeNil())
	g.Expect(resolved.AdapterImplementationID).To(Equal(implementation.ID))
	g.Expect(resolved.AdapterAssignmentID).To(Equal(assignment.ID))
	g.Expect(resolved.Capability).To(Equal(request.RequiredCapability))
	g.Expect(resolved.CapabilityVersion).To(Equal(request.RequiredCapabilityVersion))
	g.Expect(resolved.ImplementationVersion).To(Equal(implementation.Version))
	g.Expect(resolved.ConfigSnapshotID).To(Equal(request.TargetConfigSnapshotID))
	g.Expect(resolved.ConfigChecksum).To(Equal(assignment.ConfigChecksum))
	g.Expect(resolved.KeyConfiguration.PublicKeyFingerprint).
		To(Equal(assignment.KeyConfiguration.PublicKeyFingerprint))
	g.Expect(resolved.KeyConfiguration.SigningKeyReference).To(Equal("secret-provider://adapter-signing"))
	g.Expect(resolved.KeyConfiguration.SigningKeyVersionFingerprint).
		To(Equal("sha256:" + strings.Repeat("d", 64)))
}

func TestResolveStepAdapterRejectsMissingAndAmbiguousImplementation(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		g := NewWithT(t)
		request, _, _ := adapterResolutionFixture()
		request.Implementations = nil

		resolved, issues := ResolveStepAdapter(context.Background(), request)

		g.Expect(resolved).To(BeNil())
		g.Expect(issueCodes(issues)).To(ContainElement("adapter_implementation_missing"))
	})

	t.Run("ambiguous", func(t *testing.T) {
		g := NewWithT(t)
		request, implementation, assignment := adapterResolutionFixture()
		second := implementation
		second.ID = uuid.New()
		second.Key = "compose-secondary"
		secondAssignment := assignment
		secondAssignment.ID = uuid.New()
		secondAssignment.AdapterImplementationID = second.ID
		request.Implementations = append(request.Implementations, second)
		request.Assignments = append(request.Assignments, secondAssignment)

		resolved, issues := ResolveStepAdapter(context.Background(), request)

		g.Expect(resolved).To(BeNil())
		g.Expect(issueCodes(issues)).To(ContainElement("adapter_implementation_ambiguous"))
	})
}

func TestResolveStepAdapterRequiresExactTargetScopeAndConfigSnapshot(t *testing.T) {
	t.Run("scope", func(t *testing.T) {
		g := NewWithT(t)
		request, _, _ := adapterResolutionFixture()
		request.Assignments[0].ScopeID = uuid.New()

		resolved, issues := ResolveStepAdapter(context.Background(), request)

		g.Expect(resolved).To(BeNil())
		g.Expect(issueCodes(issues)).To(ContainElement("adapter_assignment_missing"))
	})

	t.Run("config snapshot", func(t *testing.T) {
		g := NewWithT(t)
		request, _, _ := adapterResolutionFixture()
		request.Assignments[0].ConfigSnapshotID = uuid.New()

		resolved, issues := ResolveStepAdapter(context.Background(), request)

		g.Expect(resolved).To(BeNil())
		g.Expect(issueCodes(issues)).To(ContainElement("adapter_assignment_missing"))
	})

	t.Run("checksum", func(t *testing.T) {
		g := NewWithT(t)
		request, _, _ := adapterResolutionFixture()
		request.Assignments[0].ConfigChecksum = "sha256:" + strings.Repeat("e", 64)

		resolved, issues := ResolveStepAdapter(context.Background(), request)

		g.Expect(resolved).To(BeNil())
		g.Expect(issueCodes(issues)).To(ContainElement("adapter_config_checksum_mismatch"))
	})
}

func TestResolveStepAdapterRejectsDisabledAssignment(t *testing.T) {
	g := NewWithT(t)
	request, _, _ := adapterResolutionFixture()
	request.Assignments[0].Enabled = false

	resolved, issues := ResolveStepAdapter(context.Background(), request)

	g.Expect(resolved).To(BeNil())
	g.Expect(issueCodes(issues)).To(ContainElement("adapter_assignment_disabled"))
}

func TestResolveStepAdapterKeepsReleaseRequirementAuthoritative(t *testing.T) {
	g := NewWithT(t)
	request, _, _ := adapterResolutionFixture()
	request.RequiredCapability = "database.migrate"
	request.RequiredCapabilityVersion = "3.0.0"

	resolved, issues := ResolveStepAdapter(context.Background(), request)

	g.Expect(resolved).To(BeNil())
	g.Expect(issueCodes(issues)).To(ContainElement("adapter_implementation_missing"))
}

func TestVerifyAdapterAtStartRejectsVersionAndFingerprintDrift(t *testing.T) {
	g := NewWithT(t)
	request, _, _ := adapterResolutionFixture()
	resolved, issues := ResolveStepAdapter(context.Background(), request)
	g.Expect(issues).To(BeEmpty())
	frozen := types.DeploymentPlanStepAdapter{
		AdapterAssignmentID:     resolved.AdapterAssignmentID,
		AdapterImplementationID: resolved.AdapterImplementationID,
		ImplementationVersion:   resolved.ImplementationVersion,
		Capability:              resolved.Capability,
		CapabilityVersion:       resolved.CapabilityVersion,
		ScopeType:               resolved.ScopeType,
		ScopeID:                 resolved.ScopeID,
		ConfigSnapshotID:        resolved.ConfigSnapshotID,
		ConfigChecksum:          resolved.ConfigChecksum,
		KeyConfiguration:        resolved.KeyConfiguration,
	}
	current := types.AdapterRuntimeState{
		AdapterAssignmentID:     frozen.AdapterAssignmentID,
		AdapterImplementationID: frozen.AdapterImplementationID,
		ImplementationVersion:   "2.1.0",
		Capability:              frozen.Capability,
		CapabilityVersion:       frozen.CapabilityVersion,
		ScopeType:               frozen.ScopeType,
		ScopeID:                 frozen.ScopeID,
		ConfigSnapshotID:        frozen.ConfigSnapshotID,
		ConfigChecksum:          frozen.ConfigChecksum,
		KeyConfiguration:        frozen.KeyConfiguration,
		Enabled:                 true,
	}
	current.KeyConfiguration.PublicKeyFingerprint = "sha256:" + strings.Repeat("f", 64)

	err := VerifyAdapterAtStart(WithRuntimeState(context.Background(), current), frozen)

	g.Expect(err).To(MatchError(ContainSubstring("adapter implementation version changed")))
}

func TestVerifyAdapterAtStartAcceptsExactFrozenState(t *testing.T) {
	g := NewWithT(t)
	request, _, _ := adapterResolutionFixture()
	resolved, issues := ResolveStepAdapter(context.Background(), request)
	g.Expect(issues).To(BeEmpty())
	frozen := types.DeploymentPlanStepAdapter{
		AdapterAssignmentID:     resolved.AdapterAssignmentID,
		AdapterImplementationID: resolved.AdapterImplementationID,
		ImplementationVersion:   resolved.ImplementationVersion,
		Capability:              resolved.Capability,
		CapabilityVersion:       resolved.CapabilityVersion,
		ScopeType:               resolved.ScopeType,
		ScopeID:                 resolved.ScopeID,
		ConfigSnapshotID:        resolved.ConfigSnapshotID,
		ConfigChecksum:          resolved.ConfigChecksum,
		KeyConfiguration:        resolved.KeyConfiguration,
	}
	current := types.AdapterRuntimeState{
		AdapterAssignmentID:     frozen.AdapterAssignmentID,
		AdapterImplementationID: frozen.AdapterImplementationID,
		ImplementationVersion:   frozen.ImplementationVersion,
		Capability:              frozen.Capability,
		CapabilityVersion:       frozen.CapabilityVersion,
		ScopeType:               frozen.ScopeType,
		ScopeID:                 frozen.ScopeID,
		ConfigSnapshotID:        frozen.ConfigSnapshotID,
		ConfigChecksum:          frozen.ConfigChecksum,
		KeyConfiguration:        frozen.KeyConfiguration,
		Enabled:                 true,
	}

	g.Expect(VerifyAdapterAtStart(WithRuntimeState(context.Background(), current), frozen)).
		To(Succeed())
}

func adapterResolutionFixture() (
	types.AdapterResolutionRequest,
	types.AdapterImplementation,
	types.AdapterAssignment,
) {
	orgID := uuid.New()
	targetID := uuid.New()
	configSnapshotID := uuid.New()
	configChecksum := "sha256:" + strings.Repeat("a", 64)
	implementation := types.AdapterImplementation{
		ID:             uuid.New(),
		OrganizationID: orgID,
		Key:            "compose",
		Name:           "Compose deployment adapter",
		Version:        "2.0.0",
		Enabled:        true,
		Capabilities: []types.AdapterCapability{{
			Capability: "deployment.compose",
			Version:    "1.0.0",
		}},
	}
	assignment := types.AdapterAssignment{
		ID:                      uuid.New(),
		OrganizationID:          orgID,
		AdapterImplementationID: implementation.ID,
		ScopeType:               types.AdapterScopeDeploymentTarget,
		ScopeID:                 targetID,
		ConfigSnapshotID:        configSnapshotID,
		ConfigChecksum:          configChecksum,
		KeyConfiguration: types.AdapterKeyConfiguration{
			KeyID:                        "adapter-signing-v3",
			PublicKeyFingerprint:         "sha256:" + strings.Repeat("c", 64),
			SigningKeyReference:          "secret-provider://adapter-signing",
			SigningKeyVersionFingerprint: "sha256:" + strings.Repeat("d", 64),
		},
		Enabled: true,
	}
	return types.AdapterResolutionRequest{
		OrganizationID:            orgID,
		StepKey:                   "component:api:deploy",
		RequiredCapability:        "deployment.compose",
		RequiredCapabilityVersion: "1.0.0",
		ScopeType:                 types.AdapterScopeDeploymentTarget,
		ScopeID:                   targetID,
		TargetConfigSnapshotID:    configSnapshotID,
		TargetConfigChecksum:      configChecksum,
		Implementations:           []types.AdapterImplementation{implementation},
		Assignments:               []types.AdapterAssignment{assignment},
	}, implementation, assignment
}

func issueCodes(issues []types.ValidationIssue) []string {
	result := make([]string, 0, len(issues))
	for _, issue := range issues {
		result = append(result, issue.Code)
	}
	return result
}
