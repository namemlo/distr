package migrationplanning

import (
	"encoding/json"
	"testing"

	"github.com/distr-sh/distr/internal/actionregistry"
	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestExpandMigrationGraphRequiresVerifiedBackupBeforeMutation(t *testing.T) {
	g := NewWithT(t)
	contract := migrationContractFixture()
	base := types.TargetPlanGraph{Steps: []types.TargetPlanStep{{
		StepKey: "component:ledger:deploy", Kind: "deploy",
		ComponentKey: "ledger", TargetLockKey: "target:one",
	}}}

	graph, err := ExpandMigrationGraph(contract, base)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(graph.TopologicalOrder).To(ContainElements(
		"migration:ledger.042:backup:create",
		"migration:ledger.042:backup:verify",
		"migration:ledger.042:apply",
		"component:ledger:deploy",
	))
	g.Expect(indexOf(graph.TopologicalOrder, "migration:ledger.042:backup:verify")).To(
		BeNumerically("<", indexOf(graph.TopologicalOrder, "migration:ledger.042:apply")),
	)
	g.Expect(indexOf(graph.TopologicalOrder, "migration:ledger.042:apply")).To(
		BeNumerically("<", indexOf(graph.TopologicalOrder, "component:ledger:deploy")),
	)
	g.Expect(graph.Edges).To(ContainElement(types.DeploymentPlanStepEdge{
		Key:         "migration:ledger.042:backup:verify->migration:ledger.042:precondition",
		FromStepKey: "migration:ledger.042:backup:verify",
		ToStepKey:   "migration:ledger.042:precondition",
	}))
}

func TestExpandMigrationGraphUsesStableRetryInputAndDatabaseLock(t *testing.T) {
	g := NewWithT(t)
	contract := migrationContractFixture()

	first, err := ExpandMigrationGraph(contract, types.TargetPlanGraph{})
	g.Expect(err).NotTo(HaveOccurred())
	second, err := ExpandMigrationGraph(contract, types.TargetPlanGraph{})
	g.Expect(err).NotTo(HaveOccurred())

	apply := stepByKey(first, "migration:ledger.042:apply")
	secondApply := stepByKey(second, "migration:ledger.042:apply")
	g.Expect(apply.ExpectedInputChecksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
	g.Expect(apply.ExpectedInputChecksum).To(Equal(secondApply.ExpectedInputChecksum))
	g.Expect(apply.DatabaseLockKey).To(Equal("database:postgres:ledger"))
	g.Expect(apply.RetryClass).To(Equal(string(types.MigrationRetrySafe)))
	var input map[string]any
	g.Expect(json.Unmarshal(apply.InputBindings, &input)).To(Succeed())
	g.Expect(input).To(HaveKeyWithValue("idempotencyKey", "ledger.042"))
	g.Expect(input).To(HaveKeyWithValue("expectedSourceChecksum", contract.ExpectedSourceChecksum))
	g.Expect(input).NotTo(HaveKey("credentials"))
	precondition := stepByKey(first, "migration:ledger.042:precondition")
	g.Expect(json.Unmarshal(precondition.InputBindings, &input)).To(Succeed())
	g.Expect(input).To(HaveKeyWithValue("expectedSchemaChecksum", contract.ExpectedSourceChecksum))
}

func TestExpandMigrationGraphUsesExplicitResultingSchemaChecksum(t *testing.T) {
	g := NewWithT(t)
	contract := migrationContractFixture()
	contract.ResultingSchemaChecksum = checksum("9")
	contract.PostconditionProbes[0].ExpectedChecksum = checksum("8")

	graph, err := ExpandMigrationGraph(contract, types.TargetPlanGraph{})

	g.Expect(err).NotTo(HaveOccurred())
	apply := stepByKey(graph, "migration:ledger.042:apply")
	var input map[string]any
	g.Expect(json.Unmarshal(apply.InputBindings, &input)).To(Succeed())
	g.Expect(input).To(HaveKeyWithValue("resultingSchemaChecksum", checksum("9")))
	validate := stepByKey(graph, "migration:ledger.042:validate")
	g.Expect(json.Unmarshal(validate.InputBindings, &input)).To(Succeed())
	g.Expect(input).To(HaveKeyWithValue("expectedSchemaChecksum", checksum("9")))
	g.Expect(input).NotTo(HaveKeyWithValue("expectedSchemaChecksum", checksum("8")))
}

func TestRetryNoneDerivesDeterministicUniqueOperationKeys(t *testing.T) {
	g := NewWithT(t)
	first := migrationContractFixture()
	first.RetryClass = types.MigrationRetryNone
	first.IdempotencyKey = ""
	second := first
	second.ID = "ledger.043"

	firstGraph, err := ExpandMigrationGraph(first, types.TargetPlanGraph{})
	g.Expect(err).NotTo(HaveOccurred())
	repeatedGraph, err := ExpandMigrationGraph(first, types.TargetPlanGraph{})
	g.Expect(err).NotTo(HaveOccurred())
	secondGraph, err := ExpandMigrationGraph(second, types.TargetPlanGraph{})
	g.Expect(err).NotTo(HaveOccurred())
	reverseGraph, err := buildReverseGraph(types.FailedPlan{
		Contracts:             []types.MigrationContract{first, second},
		CompletedMigrationIDs: []string{first.ID, second.ID},
	})
	g.Expect(err).NotTo(HaveOccurred())

	firstApplyKey := stepInputString(t, firstGraph, "migration:ledger.042:apply", "idempotencyKey")
	g.Expect(
		stepInputString(t, repeatedGraph, "migration:ledger.042:apply", "idempotencyKey"),
	).To(Equal(firstApplyKey))
	keys := []string{
		firstApplyKey,
		stepInputString(t, firstGraph, "migration:ledger.042:backup:create", "idempotencyKey"),
		stepInputString(t, secondGraph, "migration:ledger.043:apply", "idempotencyKey"),
		stepInputString(t, secondGraph, "migration:ledger.043:backup:create", "idempotencyKey"),
		stepInputString(t, reverseGraph, "recovery:ledger.042:reverse", "idempotencyKey"),
		stepInputString(t, reverseGraph, "recovery:ledger.043:reverse", "idempotencyKey"),
	}
	g.Expect(keys).To(HaveLen(6))
	g.Expect(keys).To(ConsistOf(uniqueStrings(keys)))
	for _, key := range keys {
		g.Expect(key).To(MatchRegexp(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`))
	}
}

func TestExpandMigrationGraphProducesRegistryValidBoundedActions(t *testing.T) {
	g := NewWithT(t)

	graph, err := ExpandMigrationGraph(migrationContractFixture(), types.TargetPlanGraph{})

	g.Expect(err).NotTo(HaveOccurred())
	registry := actionregistry.DefaultRegistry()
	for _, step := range graph.Steps {
		var input map[string]any
		g.Expect(json.Unmarshal(step.InputBindings, &input)).To(Succeed())
		g.Expect(registry.ValidateInput(step.ActionType, input)).To(Succeed(), step.StepKey)
	}
}

func TestExpandMigrationGraphRejectsUnorderedDatabaseLockConflict(t *testing.T) {
	g := NewWithT(t)
	contract := migrationContractFixture()
	base := types.TargetPlanGraph{Steps: []types.TargetPlanStep{{
		StepKey: "migration:other:apply", Kind: "migration",
		DatabaseLockKey: "database:postgres:ledger",
	}}}

	_, err := ExpandMigrationGraph(contract, base)

	g.Expect(err).To(MatchError(ContainSubstring("database lock")))
}

func TestExpandMigrationGraphDoesNotInsertRestoreShortcut(t *testing.T) {
	g := NewWithT(t)

	graph, err := ExpandMigrationGraph(migrationContractFixture(), types.TargetPlanGraph{})

	g.Expect(err).NotTo(HaveOccurred())
	for _, step := range graph.Steps {
		g.Expect(step.ActionType).NotTo(Equal("database.restore.execute"))
	}
}

func TestExpandMigrationGraphMakesRetryCallbackIdempotentButRejectsChangedInput(t *testing.T) {
	g := NewWithT(t)
	contract := migrationContractFixture()
	first, err := ExpandMigrationGraph(contract, types.TargetPlanGraph{})
	g.Expect(err).NotTo(HaveOccurred())

	repeated, err := ExpandMigrationGraph(contract, first)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(repeated).To(Equal(first))
	g.Expect(repeated.Steps).To(HaveLen(len(first.Steps)))

	changed := first
	for index := range changed.Steps {
		if changed.Steps[index].StepKey == "migration:ledger.042:apply" {
			changed.Steps[index].ExpectedInputChecksum = checksum("f")
		}
	}
	_, err = ExpandMigrationGraph(contract, changed)
	g.Expect(err).To(MatchError(ContainSubstring("retry input checksum")))
}

func TestExpandMigrationGraphRejectsIncompleteOrChangedExistingSubgraph(t *testing.T) {
	g := NewWithT(t)
	contract := migrationContractFixture()
	complete, err := ExpandMigrationGraph(contract, types.TargetPlanGraph{})
	g.Expect(err).NotTo(HaveOccurred())

	cases := map[string]func(*types.TargetPlanGraph){
		"missing backup creation": func(graph *types.TargetPlanGraph) {
			removeStep(graph, "migration:ledger.042:backup:create")
		},
		"changed backup verification action": func(graph *types.TargetPlanGraph) {
			stepByKeyPointer(graph, "migration:ledger.042:backup:verify").ActionType = "database.backup.create"
		},
		"changed precondition lock": func(graph *types.TargetPlanGraph) {
			stepByKeyPointer(graph, "migration:ledger.042:precondition").DatabaseLockKey = "database:other"
		},
		"changed postcondition observation": func(graph *types.TargetPlanGraph) {
			stepByKeyPointer(graph, "migration:ledger.042:validate").ObservationRequirement = "weaker evidence"
		},
		"missing backup edge": func(graph *types.TargetPlanGraph) {
			removeEdge(graph, "migration:ledger.042:backup:verify->migration:ledger.042:precondition")
		},
	}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			graph := cloneGraph(t, complete)
			corrupt(&graph)

			_, expandErr := ExpandMigrationGraph(contract, graph)

			NewWithT(t).Expect(expandErr).To(MatchError(ContainSubstring("existing migration subgraph")))
		})
	}
}

func cloneGraph(t *testing.T, graph types.TargetPlanGraph) types.TargetPlanGraph {
	t.Helper()
	payload, err := json.Marshal(graph)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	var result types.TargetPlanGraph
	NewWithT(t).Expect(json.Unmarshal(payload, &result)).To(Succeed())
	return result
}

func stepInputString(
	t *testing.T,
	graph types.TargetPlanGraph,
	stepKey, inputKey string,
) string {
	t.Helper()
	step := stepByKey(graph, stepKey)
	var input map[string]any
	NewWithT(t).Expect(json.Unmarshal(step.InputBindings, &input)).To(Succeed())
	value, _ := input[inputKey].(string)
	return value
}

func uniqueStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		if _, exists := seen[value]; !exists {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	return result
}

func stepByKeyPointer(graph *types.TargetPlanGraph, key string) *types.TargetPlanStep {
	for index := range graph.Steps {
		if graph.Steps[index].StepKey == key {
			return &graph.Steps[index]
		}
	}
	return nil
}

func removeStep(graph *types.TargetPlanGraph, key string) {
	for index := range graph.Steps {
		if graph.Steps[index].StepKey == key {
			graph.Steps = append(graph.Steps[:index], graph.Steps[index+1:]...)
			return
		}
	}
}

func removeEdge(graph *types.TargetPlanGraph, key string) {
	for index := range graph.Edges {
		if graph.Edges[index].Key == key {
			graph.Edges = append(graph.Edges[:index], graph.Edges[index+1:]...)
			return
		}
	}
}

func stepByKey(graph types.TargetPlanGraph, key string) types.TargetPlanStep {
	for _, step := range graph.Steps {
		if step.StepKey == key {
			return step
		}
	}
	return types.TargetPlanStep{}
}

func indexOf(values []string, value string) int {
	for index, candidate := range values {
		if candidate == value {
			return index
		}
	}
	return -1
}
