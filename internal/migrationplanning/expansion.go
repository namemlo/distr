package migrationplanning

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/distr-sh/distr/internal/types"
)

func ExpandMigrationGraph(
	contract types.MigrationContract,
	base types.TargetPlanGraph,
) (types.TargetPlanGraph, error) {
	if issues := ValidateMigrationContract(contract); len(issues) > 0 {
		return types.TargetPlanGraph{}, fmt.Errorf(
			"invalid migration contract %q: %s",
			contract.ID,
			issues[0].Message,
		)
	}
	prefix := "migration:" + contract.ID
	databaseLock := "database:" + contract.DatabaseResourceKey
	if hasMigrationSubgraph(base, prefix) {
		stripped := withoutMigrationSubgraph(base, prefix)
		expected, err := buildMigrationGraph(contract, stripped)
		if err != nil {
			return types.TargetPlanGraph{}, err
		}
		if !reflect.DeepEqual(
			migrationSubgraph(base, prefix),
			migrationSubgraph(expected, prefix),
		) {
			return types.TargetPlanGraph{}, fmt.Errorf(
				"existing migration subgraph %q retry input checksum or complete expected graph changed",
				contract.ID,
			)
		}
		return finalizeGraph(base)
	}
	for _, step := range base.Steps {
		if step.DatabaseLockKey == databaseLock &&
			!dependencyStep(contract.DependsOn, step.StepKey) {
			return types.TargetPlanGraph{}, fmt.Errorf(
				"database lock %q conflicts with unordered step %q",
				databaseLock,
				step.StepKey,
			)
		}
	}
	return buildMigrationGraph(contract, base)
}

func buildMigrationGraph(
	contract types.MigrationContract,
	base types.TargetPlanGraph,
) (types.TargetPlanGraph, error) {
	prefix := "migration:" + contract.ID
	applyKey := prefix + ":apply"
	databaseLock := "database:" + contract.DatabaseResourceKey
	targetLock := migrationTargetLock(contract, base)
	steps := slices.Clone(base.Steps)
	edges := slices.Clone(base.Edges)
	firstKey := prefix + ":precondition"
	if contract.BackupRequired {
		createKey := prefix + ":backup:create"
		verifyKey := prefix + ":backup:verify"
		steps = append(steps,
			migrationStep(contract, createKey, "Create backup for "+contract.ID,
				"backup", "database.backup.create", targetLock, databaseLock,
				map[string]any{
					"databaseResourceKey":  contract.DatabaseResourceKey,
					"databaseLockKey":      databaseLock,
					"destinationReference": "backup:" + contract.ID,
					"credentialsSecretRef": "database:" + contract.DatabaseResourceKey,
					"idempotencyKey": migrationOperationIdempotencyKey(
						contract,
						"backup",
					),
					"timeoutSeconds": contract.LockTimeoutSeconds,
				}, types.MigrationRetrySafe, "cooperative",
				"backup identity, checksum, and bounded creation evidence"),
			migrationStep(contract, verifyKey, "Verify backup for "+contract.ID,
				"backup_verification", "database.backup.verify", targetLock, databaseLock,
				map[string]any{
					"databaseResourceKey": contract.DatabaseResourceKey,
					"databaseLockKey":     databaseLock,
					"backupReference":     createKey,
					"verifierReference":   contract.BackupVerifier,
					"timeoutSeconds":      contract.LockTimeoutSeconds,
				}, types.MigrationRetrySafe, "safe",
				"verified backup checksum and verifier evidence"),
		)
		edges = append(edges,
			newEdge(createKey, verifyKey),
			newEdge(verifyKey, firstKey),
		)
		firstKey = createKey
	}
	preconditionKey := prefix + ":precondition"
	validateKey := prefix + ":validate"
	steps = append(steps,
		migrationStep(contract, preconditionKey, "Validate source schema for "+contract.ID,
			"migration_precondition", "database.migration.validate", targetLock, databaseLock,
			map[string]any{
				"migrationId": contract.ID, "migrationChecksum": contract.Checksum,
				"databaseResourceKey":    contract.DatabaseResourceKey,
				"databaseLockKey":        databaseLock,
				"expectedSchemaVersion":  contract.ExpectedSourceVersion,
				"expectedSchemaChecksum": contract.ExpectedSourceChecksum,
				"probes":                 contract.PreconditionProbes,
				"timeoutSeconds":         contract.LockTimeoutSeconds,
			}, types.MigrationRetrySafe, "safe",
			"source schema and precondition probe evidence"),
		migrationStep(contract, applyKey, "Apply "+contract.ID,
			"migration", "database.migration.apply", targetLock, databaseLock,
			migrationApplyInput(contract, databaseLock),
			contract.RetryClass, migrationCancellation(contract),
			"migration completion evidence bound to the exact input checksum"),
		migrationStep(contract, validateKey, "Validate "+contract.ID,
			"migration_validation", "database.migration.validate", targetLock, databaseLock,
			map[string]any{
				"migrationId": contract.ID, "migrationChecksum": contract.Checksum,
				"databaseResourceKey":    contract.DatabaseResourceKey,
				"databaseLockKey":        databaseLock,
				"expectedSchemaVersion":  contract.ResultingVersion,
				"expectedSchemaChecksum": contract.PostconditionProbes[0].ExpectedChecksum,
				"probes":                 contract.PostconditionProbes,
				"timeoutSeconds":         contract.LockTimeoutSeconds,
			}, types.MigrationRetrySafe, "safe",
			"resulting schema observation and postcondition probe evidence"),
	)
	edges = append(edges, newEdge(preconditionKey, applyKey), newEdge(applyKey, validateKey))
	for _, dependency := range contract.DependsOn {
		dependencyKey := "migration:" + dependency + ":validate"
		if hasGraphStep(steps, dependencyKey) {
			edges = append(edges, newEdge(dependencyKey, firstKey))
		}
	}
	for _, step := range base.Steps {
		if step.ComponentKey == contract.ComponentKey && isMutationStep(step) {
			edges = append(edges, newEdge(validateKey, step.StepKey))
		}
	}
	return finalizeGraph(types.TargetPlanGraph{Steps: steps, Edges: edges})
}

type comparableMigrationSubgraph struct {
	Steps []types.TargetPlanStep
	Edges []types.DeploymentPlanStepEdge
}

func hasMigrationSubgraph(graph types.TargetPlanGraph, prefix string) bool {
	for _, step := range graph.Steps {
		if strings.HasPrefix(step.StepKey, prefix+":") {
			return true
		}
	}
	return false
}

func withoutMigrationSubgraph(
	graph types.TargetPlanGraph,
	prefix string,
) types.TargetPlanGraph {
	result := types.TargetPlanGraph{}
	for _, step := range graph.Steps {
		if !strings.HasPrefix(step.StepKey, prefix+":") {
			result.Steps = append(result.Steps, step)
		}
	}
	for _, edge := range graph.Edges {
		if !strings.HasPrefix(edge.FromStepKey, prefix+":") &&
			!strings.HasPrefix(edge.ToStepKey, prefix+":") {
			result.Edges = append(result.Edges, edge)
		}
	}
	return result
}

func migrationSubgraph(
	graph types.TargetPlanGraph,
	prefix string,
) comparableMigrationSubgraph {
	result := comparableMigrationSubgraph{}
	for _, step := range graph.Steps {
		if strings.HasPrefix(step.StepKey, prefix+":") {
			step.SortOrder = 0
			result.Steps = append(result.Steps, step)
		}
	}
	for _, edge := range graph.Edges {
		if strings.HasPrefix(edge.FromStepKey, prefix+":") ||
			strings.HasPrefix(edge.ToStepKey, prefix+":") {
			result.Edges = append(result.Edges, edge)
		}
	}
	slices.SortFunc(result.Steps, func(a, b types.TargetPlanStep) int {
		return strings.Compare(a.StepKey, b.StepKey)
	})
	slices.SortFunc(result.Edges, func(a, b types.DeploymentPlanStepEdge) int {
		return strings.Compare(a.Key, b.Key)
	})
	return result
}

func migrationApplyInput(
	contract types.MigrationContract,
	databaseLock string,
) map[string]any {
	input := map[string]any{
		"migrationId": contract.ID, "migrationChecksum": contract.Checksum,
		"databaseResourceKey":    contract.DatabaseResourceKey,
		"databaseLockKey":        databaseLock,
		"expectedSourceVersion":  contract.ExpectedSourceVersion,
		"expectedSourceChecksum": contract.ExpectedSourceChecksum,
		"resultingVersion":       contract.ResultingVersion,
		"idempotencyKey": migrationOperationIdempotencyKey(
			contract,
			"apply",
		),
		"timeoutSeconds": contract.LockTimeoutSeconds,
	}
	if contract.ArtifactDigest != "" {
		input["artifactDigest"] = contract.ArtifactDigest
	}
	return input
}

func migrationOperationIdempotencyKey(
	contract types.MigrationContract,
	operation string,
) string {
	candidate := contract.IdempotencyKey
	if candidate != "" && operation != "apply" {
		candidate = operation + ":" + candidate
	}
	if idempotencyPattern.MatchString(candidate) {
		return candidate
	}
	sum := sha256.Sum256([]byte(operation + "\x00" + contract.ID + "\x00" + contract.Checksum))
	return "migration." + operation + "." + hex.EncodeToString(sum[:])
}

func migrationStep(
	contract types.MigrationContract,
	key, name, kind, actionType, targetLock, databaseLock string,
	input map[string]any,
	retry types.MigrationRetryClass,
	cancellation, observation string,
) types.TargetPlanStep {
	inputJSON, _ := json.Marshal(input)
	return types.TargetPlanStep{
		StepKey: key, Name: name, Kind: kind, ComponentKey: contract.ComponentKey,
		ActionType: actionType, ActionName: actionType, ExecutionLocation: "agent",
		InputBindings: inputJSON, TargetLockKey: targetLock, DatabaseLockKey: databaseLock,
		TimeoutSeconds: contract.LockTimeoutSeconds, RetryClass: string(retry),
		CancellationBehavior:   cancellation,
		ExpectedInputChecksum:  checksumBytes(inputJSON),
		ObservationRequirement: observation, V1Compatible: false,
	}
}

func finalizeGraph(graph types.TargetPlanGraph) (types.TargetPlanGraph, error) {
	graph.Steps = slices.Clone(graph.Steps)
	slices.SortFunc(graph.Steps, func(a, b types.TargetPlanStep) int {
		return strings.Compare(a.StepKey, b.StepKey)
	})
	for index := range graph.Steps {
		graph.Steps[index].SortOrder = index
	}
	graph.Edges = slices.Clone(graph.Edges)
	slices.SortFunc(graph.Edges, func(a, b types.DeploymentPlanStepEdge) int {
		return strings.Compare(a.Key, b.Key)
	})
	graph.Edges = slices.CompactFunc(graph.Edges, func(a, b types.DeploymentPlanStepEdge) bool {
		return a.Key == b.Key && a.FromStepKey == b.FromStepKey && a.ToStepKey == b.ToStepKey
	})
	order, err := topologicalOrder(graph.Steps, graph.Edges)
	if err != nil {
		return types.TargetPlanGraph{}, err
	}
	graph.TopologicalOrder = order
	canonical, err := json.Marshal(struct {
		Steps            []types.TargetPlanStep         `json:"steps"`
		Edges            []types.DeploymentPlanStepEdge `json:"edges"`
		TopologicalOrder []string                       `json:"topologicalOrder"`
	}{graph.Steps, graph.Edges, graph.TopologicalOrder})
	if err != nil {
		return types.TargetPlanGraph{}, fmt.Errorf("canonicalize migration graph: %w", err)
	}
	graph.Checksum = checksumBytes(canonical)
	return graph, nil
}

func topologicalOrder(
	steps []types.TargetPlanStep,
	edges []types.DeploymentPlanStepEdge,
) ([]string, error) {
	indegree := make(map[string]int, len(steps))
	adjacency := make(map[string][]string, len(steps))
	for _, step := range steps {
		if _, duplicate := indegree[step.StepKey]; duplicate {
			return nil, fmt.Errorf("duplicate migration graph step %q", step.StepKey)
		}
		indegree[step.StepKey] = 0
	}
	for _, edge := range edges {
		if _, ok := indegree[edge.FromStepKey]; !ok {
			return nil, fmt.Errorf("migration graph edge %q has unknown source", edge.Key)
		}
		if _, ok := indegree[edge.ToStepKey]; !ok {
			return nil, fmt.Errorf("migration graph edge %q has unknown destination", edge.Key)
		}
		indegree[edge.ToStepKey]++
		adjacency[edge.FromStepKey] = append(adjacency[edge.FromStepKey], edge.ToStepKey)
	}
	for key := range adjacency {
		sort.Strings(adjacency[key])
	}
	ready := make([]string, 0)
	for key, degree := range indegree {
		if degree == 0 {
			ready = append(ready, key)
		}
	}
	sort.Strings(ready)
	order := make([]string, 0, len(steps))
	for len(ready) > 0 {
		key := ready[0]
		ready = ready[1:]
		order = append(order, key)
		for _, next := range adjacency[key] {
			indegree[next]--
			if indegree[next] == 0 {
				index, _ := slices.BinarySearch(ready, next)
				ready = slices.Insert(ready, index, next)
			}
		}
	}
	if len(order) != len(steps) {
		return nil, fmt.Errorf("migration graph contains a cycle")
	}
	return order, nil
}

func migrationTargetLock(contract types.MigrationContract, graph types.TargetPlanGraph) string {
	for _, step := range graph.Steps {
		if step.ComponentKey == contract.ComponentKey && step.TargetLockKey != "" {
			return step.TargetLockKey
		}
	}
	return "target:" + contract.ComponentKey
}

func migrationCancellation(contract types.MigrationContract) string {
	if contract.RequiresForwardFix || contract.Reversibility == types.MigrationReversibilityForwardOnly {
		return "forward_fix_only"
	}
	return "cooperative"
}

func dependencyStep(dependencies []string, stepKey string) bool {
	for _, dependency := range dependencies {
		if strings.HasPrefix(stepKey, "migration:"+dependency+":") {
			return true
		}
	}
	return false
}

func isMutationStep(step types.TargetPlanStep) bool {
	switch step.Kind {
	case "deploy", "migration", "mutation":
		return true
	default:
		return false
	}
}

func hasGraphStep(steps []types.TargetPlanStep, key string) bool {
	for _, step := range steps {
		if step.StepKey == key {
			return true
		}
	}
	return false
}

func newEdge(from, to string) types.DeploymentPlanStepEdge {
	return types.DeploymentPlanStepEdge{
		Key: from + "->" + to, FromStepKey: from, ToStepKey: to,
	}
}

func checksumBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}
