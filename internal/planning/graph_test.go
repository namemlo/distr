package planning

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/migrationplanning"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCanonicalTargetPlanFreezesAdaptersDeterministically(t *testing.T) {
	g := NewWithT(t)
	firstID := uuid.MustParse("10000000-0000-0000-0000-000000000001")
	secondID := uuid.MustParse("10000000-0000-0000-0000-000000000002")
	first := types.ResolvedPlanStepAdapter{
		StepKey: "component:web:health",
		ResolvedStepAdapter: types.ResolvedStepAdapter{
			AdapterAssignmentID: firstID, AdapterImplementationID: firstID,
			ImplementationVersion: "2.0.0", Capability: "health.http",
			CapabilityVersion: "1.0.0",
		},
	}
	second := types.ResolvedPlanStepAdapter{
		StepKey: "component:web:deploy",
		ResolvedStepAdapter: types.ResolvedStepAdapter{
			AdapterAssignmentID: secondID, AdapterImplementationID: secondID,
			ImplementationVersion: "3.0.0", Capability: "deployment.compose",
			CapabilityVersion: "1.0.0",
		},
	}
	left := types.TargetDeploymentPlanCanonical{
		StepAdapters: []types.ResolvedPlanStepAdapter{first, second},
	}
	right := types.TargetDeploymentPlanCanonical{
		StepAdapters: []types.ResolvedPlanStepAdapter{second, first},
	}

	leftPayload, leftChecksum, err := CanonicalizeTargetDeploymentPlan(left)
	g.Expect(err).NotTo(HaveOccurred())
	rightPayload, rightChecksum, err := CanonicalizeTargetDeploymentPlan(right)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rightPayload).To(Equal(leftPayload))
	g.Expect(rightChecksum).To(Equal(leftChecksum))

	right.StepAdapters[0].ImplementationVersion = "3.1.0"
	_, driftedChecksum, err := CanonicalizeTargetDeploymentPlan(right)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(driftedChecksum).NotTo(Equal(leftChecksum))
}

func TestCanonicalTargetPlanFreezesSchemaEvidenceDeterministically(t *testing.T) {
	g := NewWithT(t)
	firstRequirement := types.SchemaEvidenceRequirement{
		ComponentKey: "customer-api", DatabaseResourceKey: "postgres:customer",
	}
	secondRequirement := types.SchemaEvidenceRequirement{
		ComponentKey: "transaction-api", DatabaseResourceKey: "postgres:transaction",
	}
	first := types.SchemaEvidenceBundle{
		Requirement:        firstRequirement,
		SchemaReportObject: types.SchemaEvidenceObject{ObjectKey: "customer-report"},
		MigrationEvidence: types.MigrationEvidence{MixedVersionEvidence: []types.MixedVersionSchemaEvidence{
			{ApplicationVersion: "2.0.0", SchemaVersion: "42", SchemaChecksum: "sha256:b"},
			{ApplicationVersion: "1.0.0", SchemaVersion: "41", SchemaChecksum: "sha256:a"},
		}},
	}
	second := types.SchemaEvidenceBundle{
		Requirement:        secondRequirement,
		SchemaReportObject: types.SchemaEvidenceObject{ObjectKey: "transaction-report"},
	}
	left := types.TargetDeploymentPlanCanonical{
		SchemaEvidenceRequirements: []types.SchemaEvidenceRequirement{firstRequirement, secondRequirement},
		SchemaEvidence:             []types.SchemaEvidenceBundle{first, second},
	}
	right := types.TargetDeploymentPlanCanonical{
		SchemaEvidenceRequirements: []types.SchemaEvidenceRequirement{secondRequirement, firstRequirement},
		SchemaEvidence:             []types.SchemaEvidenceBundle{second, first},
	}
	right.SchemaEvidence[1].MigrationEvidence.MixedVersionEvidence = slices.Clone(
		right.SchemaEvidence[1].MigrationEvidence.MixedVersionEvidence,
	)
	slices.Reverse(right.SchemaEvidence[1].MigrationEvidence.MixedVersionEvidence)

	leftPayload, leftChecksum, err := CanonicalizeTargetDeploymentPlan(left)
	g.Expect(err).NotTo(HaveOccurred())
	rightPayload, rightChecksum, err := CanonicalizeTargetDeploymentPlan(right)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(rightPayload).To(Equal(leftPayload))
	g.Expect(rightChecksum).To(Equal(leftChecksum))
}

func TestBuildTargetPlanGraphExpandsStructuredMigrationSafetyGates(t *testing.T) {
	g := NewWithT(t)
	draft := resolverFixture()
	contract := planningMigrationContract(t)
	draft.ResolutionInput.ReleasePins[0].Migrations = []types.MigrationDeclaration{{
		Key: contract.ID, Type: "database", Order: 1,
		Compatibility: "forward-compatible", FailurePolicy: "retry",
		Description: "Apply the ledger schema transition",
	}}
	draft.ResolutionInput.ReleasePins[0].MigrationContracts = []types.MigrationContract{contract}
	draft.ResolutionInput.ComponentInstances[0].DatabaseBoundary = contract.DatabaseResourceKey
	resolutions, issues := ResolveTargetRequirements(context.Background(), draft)
	g.Expect(issues).To(BeEmpty())

	graph, err := BuildTargetPlanGraph(context.Background(), draft, resolutions)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(graph.TopologicalOrder).To(ContainElements(
		"migration:ledger.042:backup:create",
		"migration:ledger.042:backup:verify",
		"migration:ledger.042:precondition",
		"migration:ledger.042:apply",
		"migration:ledger.042:validate",
	))
	g.Expect(indexOf(graph.TopologicalOrder, "migration:ledger.042:backup:verify")).To(
		BeNumerically("<", indexOf(graph.TopologicalOrder, "migration:ledger.042:apply")),
	)
	g.Expect(graph.Edges).To(ContainElement(types.DeploymentPlanStepEdge{
		Key:         "migration:ledger.042:validate->component:consumer:deploy",
		FromStepKey: "migration:ledger.042:validate", ToStepKey: "component:consumer:deploy",
	}))
	g.Expect(graph.Steps).NotTo(ContainElement(HaveField("ActionType", "component.migrate")))
	apply, found := findTargetPlanStep(graph.Steps, "migration:ledger.042:apply")
	g.Expect(found).To(BeTrue())
	g.Expect(apply.ComponentReleaseID).To(Equal(
		&draft.ResolutionInput.ReleasePins[0].ComponentReleaseID,
	))
	g.Expect(apply.ComponentInstanceID).To(Equal(
		&draft.ResolutionInput.Config.ComponentBindings[0].ComponentInstanceID,
	))
}

func TestBuildTargetPlanGraphAllowsTransitiveSameDatabaseMigrationChain(t *testing.T) {
	g := NewWithT(t)
	draft := resolverFixture()
	first := planningMigrationContract(t)
	second := first
	second.ID = "ledger.043"
	second.DependsOn = []string{first.ID}
	second.ExpectedSourceVersion = first.ResultingVersion
	second.ResultingVersion = "43"
	second.IdempotencyKey = second.ID
	second.RecoveryProcedureReference = "recovery:ledger.043:v1"
	second.Checksum, _ = migrationplanning.CanonicalMigrationContractChecksum(second)
	third := second
	third.ID = "ledger.044"
	third.DependsOn = []string{second.ID}
	third.ExpectedSourceVersion = second.ResultingVersion
	third.ResultingVersion = "44"
	third.IdempotencyKey = third.ID
	third.RecoveryProcedureReference = "recovery:ledger.044:v1"
	third.Checksum, _ = migrationplanning.CanonicalMigrationContractChecksum(third)
	draft.ResolutionInput.ReleasePins[0].Migrations = []types.MigrationDeclaration{
		{Key: first.ID, Type: "database", Order: 1},
		{Key: second.ID, Type: "database", Order: 2},
		{Key: third.ID, Type: "database", Order: 3},
	}
	draft.ResolutionInput.ReleasePins[0].MigrationContracts = []types.MigrationContract{
		third,
		first,
		second,
	}
	draft.ResolutionInput.ComponentInstances[0].DatabaseBoundary = first.DatabaseResourceKey
	resolutions, issues := ResolveTargetRequirements(context.Background(), draft)
	g.Expect(issues).To(BeEmpty())

	graph, err := BuildTargetPlanGraph(context.Background(), draft, resolutions)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(indexOf(graph.TopologicalOrder, "migration:ledger.042:validate")).To(
		BeNumerically("<", indexOf(graph.TopologicalOrder, "migration:ledger.043:apply")),
	)
	g.Expect(indexOf(graph.TopologicalOrder, "migration:ledger.043:validate")).To(
		BeNumerically("<", indexOf(graph.TopologicalOrder, "migration:ledger.044:apply")),
	)
	g.Expect(indexOf(graph.TopologicalOrder, "migration:ledger.044:validate")).To(
		BeNumerically("<", indexOf(graph.TopologicalOrder, "component:consumer:deploy")),
	)
}

func TestCanonicalTargetPlanOmitsEmptyStructuredMigrationFields(t *testing.T) {
	g := NewWithT(t)
	payload, _, err := CanonicalizeTargetDeploymentPlan(types.TargetDeploymentPlanCanonical{})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(payload)).NotTo(ContainSubstring("migrationContracts"))
	g.Expect(string(payload)).NotTo(ContainSubstring(`"migrations"`))
}

func planningMigrationContract(t *testing.T) types.MigrationContract {
	t.Helper()
	contract := types.MigrationContract{
		ID: "ledger.042", ComponentKey: "consumer",
		DatabaseResourceKey: "postgres:ledger", ExpectedSourceVersion: "41",
		ExpectedSourceChecksum: checksum("1"), ResultingVersion: "42",
		ResultingSchemaChecksum: checksum("2"), Phase: types.MigrationPhaseExpand,
		LockType: "exclusive", LockTimeoutSeconds: 30, OperationalImpact: "brief write lock",
		BackupRequired: true, BackupVerifier: "backup-verifier:v1",
		PreconditionProbes: []types.MigrationProbe{{
			Name: "source", Reference: "probe:ledger:source:v1", ExpectedChecksum: checksum("3"),
		}},
		PostconditionProbes: []types.MigrationProbe{{
			Name: "result", Reference: "probe:ledger:result:v1", ExpectedChecksum: checksum("4"),
		}},
		RetryClass: types.MigrationRetrySafe, IdempotencyKey: "ledger.042",
		Reversibility:                    types.MigrationReversibilityReversible,
		PreviousApplicationCompatibility: ">=1.8.0",
		RecoveryProcedureReference:       "recovery:ledger.042:v1",
		AdapterType:                      "database.migrate",
		ArtifactDigest:                   "registry.example.com/migrations/ledger@sha256:" + strings.Repeat("5", 64),
		EvidenceRetentionDays:            90,
	}
	value, err := migrationplanning.CanonicalMigrationContractChecksum(contract)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	contract.Checksum = value
	return contract
}

func TestBuildTargetPlanGraphIsStableAndAcyclic(t *testing.T) {
	g := NewWithT(t)
	draft := resolverFixture()
	resolutions, issues := ResolveTargetRequirements(context.Background(), draft)
	g.Expect(issues).To(BeEmpty())

	graph, err := BuildTargetPlanGraph(context.Background(), draft, resolutions)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(graph.Checksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
	g.Expect(graph.TopologicalOrder).To(HaveLen(len(graph.Steps)))
	g.Expect(graph.Steps[0].StepKey).To(Equal("config:verify"))
	for _, edge := range graph.Edges {
		g.Expect(indexOf(graph.TopologicalOrder, edge.FromStepKey)).To(
			BeNumerically("<", indexOf(graph.TopologicalOrder, edge.ToStepKey)),
		)
	}

	draft.ResolutionInput.ReleasePins = append(
		[]types.ComponentReleasePin(nil),
		draft.ResolutionInput.ReleasePins...,
	)
	second, err := BuildTargetPlanGraph(context.Background(), draft, reverseResolutions(resolutions))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(second).To(Equal(graph))
}

func TestBuildTargetPlanGraphUsesTargetDispatcherLocationForExecutableSteps(t *testing.T) {
	g := NewWithT(t)
	draft := resolverFixture()
	draft.ResolutionInput.ReleasePins[0].Migrations = []types.MigrationDeclaration{{
		Key: "schema", Type: "runtime", Order: 1,
	}}
	resolutions, issues := ResolveTargetRequirements(context.Background(), draft)
	g.Expect(issues).To(BeEmpty())

	graph, err := BuildTargetPlanGraph(context.Background(), draft, resolutions)

	g.Expect(err).NotTo(HaveOccurred())
	targetLocation := types.TaskExecutorTypeAgent.ExecutionLocation()
	for _, stepKey := range []string{
		"component:consumer:migration:schema",
		"component:consumer:deploy",
		"component:consumer:health",
	} {
		step, found := findTargetPlanStep(graph.Steps, stepKey)
		g.Expect(found).To(BeTrue(), stepKey)
		g.Expect(step.ExecutionLocation).To(Equal(targetLocation), stepKey)
	}
}

func TestBuildTargetPlanGraphKeepsExecutableStepTimeoutWithinSignedIntentLimit(t *testing.T) {
	g := NewWithT(t)
	draft := resolverFixture()
	draft.ResolutionInput.ReleasePins[0].Migrations = []types.MigrationDeclaration{{
		Key: "schema", Type: "runtime", Order: 1,
	}}
	resolutions, issues := ResolveTargetRequirements(context.Background(), draft)
	g.Expect(issues).To(BeEmpty())

	graph, err := BuildTargetPlanGraph(context.Background(), draft, resolutions)

	g.Expect(err).NotTo(HaveOccurred())
	for _, step := range graph.Steps {
		if step.ExecutionLocation == types.TaskExecutorTypeAgent.ExecutionLocation() {
			g.Expect(step.TimeoutSeconds).To(
				BeNumerically("<=", 15*60),
				step.StepKey,
			)
		}
	}
}

func TestBuildTargetPlanGraphKeepsVerificationStepsAtHubLocation(t *testing.T) {
	g := NewWithT(t)
	draft := resolverFixture()
	resolutions, issues := ResolveTargetRequirements(context.Background(), draft)
	g.Expect(issues).To(BeEmpty())

	graph, err := BuildTargetPlanGraph(context.Background(), draft, resolutions)

	g.Expect(err).NotTo(HaveOccurred())
	hubLocation := types.TaskExecutorTypeHub.ExecutionLocation()
	config, found := findTargetPlanStep(graph.Steps, "config:verify")
	g.Expect(found).To(BeTrue())
	g.Expect(config.ExecutionLocation).To(Equal(hubLocation))
	for _, step := range graph.Steps {
		if step.Kind == "requirement_verification" {
			g.Expect(step.ExecutionLocation).To(Equal(hubLocation), step.StepKey)
		}
	}
}

func TestBuildTargetPlanGraphOrdersProviderHealthBeforeConsumerDeploy(t *testing.T) {
	g := NewWithT(t)
	draft := resolverFixture()
	draft.ResolutionInput.ReleasePins = append(
		draft.ResolutionInput.ReleasePins,
		types.ComponentReleasePin{
			ComponentKey:       "provider",
			ComponentReleaseID: *draft.ResolutionInput.Candidates[0].ProviderReleaseID,
			ReleaseChecksum:    checksum("f"),
			Platforms:          []string{"linux/amd64"},
			ProvenanceVerified: true,
		},
	)
	draft.ResolutionInput.ProductEdges = []types.GraphEdge{{
		Key:             "component:provider->component:consumer:cache",
		From:            "component:provider",
		To:              "component:consumer",
		Capability:      "cache",
		VersionRange:    "^1.0.0",
		ProviderVersion: "1.4.0",
		ResolutionStage: types.CapabilityResolutionStageProduct,
		Ordering:        "provider_deploy_and_health_before_consumer",
	}}

	resolutions, issues := ResolveTargetRequirements(context.Background(), draft)
	g.Expect(issues).To(BeEmpty())
	graph, err := BuildTargetPlanGraph(context.Background(), draft, resolutions)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(graph.Edges).To(ContainElement(types.DeploymentPlanStepEdge{
		Key:         "component:provider:health->component:consumer:deploy",
		FromStepKey: "component:provider:health",
		ToStepKey:   "component:consumer:deploy",
	}))
}

func TestBuildTargetPlanGraphGatesConsumerFirstMigration(t *testing.T) {
	g := NewWithT(t)
	draft := resolverFixture()
	draft.ResolutionInput.ReleasePins[0].Migrations = []types.MigrationDeclaration{{
		Key: "schema", Type: "runtime", Order: 1,
	}}
	draft.ResolutionInput.ReleasePins = append(
		draft.ResolutionInput.ReleasePins,
		types.ComponentReleasePin{
			ComponentKey:       "provider",
			ComponentReleaseID: *draft.ResolutionInput.Candidates[0].ProviderReleaseID,
			ReleaseChecksum:    checksum("f"),
			Platforms:          []string{"linux/amd64"},
			ProvenanceVerified: true,
		},
	)
	draft.ResolutionInput.ProductEdges = []types.GraphEdge{{
		Key:             "component:provider->component:consumer:cache",
		From:            "component:provider",
		To:              "component:consumer",
		Capability:      "cache",
		VersionRange:    "^1.0.0",
		ProviderVersion: "1.4.0",
		ResolutionStage: types.CapabilityResolutionStageProduct,
	}}

	resolutions, issues := ResolveTargetRequirements(context.Background(), draft)
	g.Expect(issues).To(BeEmpty())
	graph, err := BuildTargetPlanGraph(context.Background(), draft, resolutions)

	g.Expect(err).NotTo(HaveOccurred())
	first := "component:consumer:migration:schema"
	g.Expect(graph.Edges).To(ContainElement(types.DeploymentPlanStepEdge{
		Key:         "component:provider:health->" + first,
		FromStepKey: "component:provider:health",
		ToStepKey:   first,
	}))
	requirementKey := "requirement:target.consumer.cache:verify"
	g.Expect(graph.Edges).To(ContainElement(types.DeploymentPlanStepEdge{
		Key:         requirementKey + "->" + first,
		FromStepKey: requirementKey,
		ToStepKey:   first,
	}))
}

func TestBuildTargetPlanGraphRejectsCycles(t *testing.T) {
	g := NewWithT(t)
	draft := resolverFixture()
	draft.ResolutionInput.ReleasePins = append(
		draft.ResolutionInput.ReleasePins,
		types.ComponentReleasePin{
			ComponentKey:       "provider",
			ComponentReleaseID: *draft.ResolutionInput.Candidates[0].ProviderReleaseID,
			ReleaseChecksum:    checksum("f"),
			Platforms:          []string{"linux/amd64"},
			ProvenanceVerified: true,
		},
	)
	draft.ResolutionInput.ProductEdges = []types.GraphEdge{
		{
			Key: "a", From: "component:provider", To: "component:consumer",
			ResolutionStage: types.CapabilityResolutionStageProduct,
		},
		{
			Key: "b", From: "component:consumer", To: "component:provider",
			ResolutionStage: types.CapabilityResolutionStageProduct,
		},
	}
	resolutions, _ := ResolveTargetRequirements(context.Background(), draft)

	_, err := BuildTargetPlanGraph(context.Background(), draft, resolutions)

	g.Expect(err).To(MatchError(ContainSubstring("cycle")))
}

func TestValidateProtocolV1RequiresCompatibleSteps(t *testing.T) {
	g := NewWithT(t)
	graph := types.TargetPlanGraph{
		Steps: []types.TargetPlanStep{{
			StepKey: "future", V1Compatible: false,
		}},
	}

	g.Expect(ValidateProtocolGraph(types.DeploymentPlanProtocolV1, graph)).
		To(MatchError(ContainSubstring("not compatible")))
	g.Expect(ValidateProtocolGraph(types.DeploymentPlanProtocolV2, graph)).To(Succeed())
}

func TestBuildTargetPlanGraphHasReachableProtocolV1Projection(t *testing.T) {
	g := NewWithT(t)
	draft := resolverFixture()
	draft.ProtocolVersion = types.DeploymentPlanProtocolV1
	resolutions, issues := ResolveTargetRequirements(context.Background(), draft)
	g.Expect(issues).To(BeEmpty())

	graph, err := BuildTargetPlanGraph(context.Background(), draft, resolutions)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(ValidateProtocolGraph(types.DeploymentPlanProtocolV1, graph)).To(Succeed())
	for _, step := range graph.Steps {
		g.Expect(step.V1Compatible).To(BeTrue(), step.StepKey)
	}
}

func TestCanonicalizeTargetDeploymentPlanRejectsOversizedPayload(t *testing.T) {
	g := NewWithT(t)
	canonical := types.TargetDeploymentPlanCanonical{
		Schema:                 types.TargetDeploymentPlanSchemaV2,
		ProductReleaseID:       uuid.New(),
		ProductReleaseChecksum: checksum("a"),
		Graph: types.TargetPlanGraph{Steps: []types.TargetPlanStep{{
			StepKey:       "huge",
			Name:          strings.Repeat("x", MaxTargetPlanPayloadBytes),
			V1Compatible:  true,
			InputBindings: []byte(`{}`),
		}}},
	}

	_, _, err := CanonicalizeTargetDeploymentPlan(canonical)

	g.Expect(err).To(MatchError(ContainSubstring("payload limit")))
}

func reverseResolutions(values []types.RequirementResolution) []types.RequirementResolution {
	result := append([]types.RequirementResolution(nil), values...)
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}
	return result
}

func indexOf(values []string, target string) int {
	for index, value := range values {
		if value == target {
			return index
		}
	}
	return -1
}

func findTargetPlanStep(
	steps []types.TargetPlanStep,
	stepKey string,
) (types.TargetPlanStep, bool) {
	for _, step := range steps {
		if step.StepKey == stepKey {
			return step, true
		}
	}
	return types.TargetPlanStep{}, false
}
