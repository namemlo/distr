package planning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

func CanonicalizeTargetDeploymentPlan(
	canonical types.TargetDeploymentPlanCanonical,
) ([]byte, string, error) {
	canonical.Schema = types.TargetDeploymentPlanSchemaV2
	canonical.ConfigVerificationFacts = slices.Clone(canonical.ConfigVerificationFacts)
	slices.SortFunc(canonical.ConfigVerificationFacts, func(a, b types.ConfigVerificationFact) int {
		return strings.Compare(a.ObjectKey, b.ObjectKey)
	})
	canonical.ComponentReleasePins = normalizedReleasePins(canonical.ComponentReleasePins)
	canonical.ComponentBindings = slices.Clone(canonical.ComponentBindings)
	slices.SortFunc(canonical.ComponentBindings, func(a, b types.ConfigComponentBinding) int {
		if cmp := strings.Compare(a.ComponentKey, b.ComponentKey); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.ComponentInstanceID.String(), b.ComponentInstanceID.String())
	})
	canonical.RequirementResolutions = slices.Clone(canonical.RequirementResolutions)
	slices.SortFunc(
		canonical.RequirementResolutions,
		func(a, b types.RequirementResolution) int {
			return strings.Compare(a.RequirementKey, b.RequirementKey)
		},
	)
	payload, err := json.Marshal(canonical)
	if err != nil {
		return nil, "", fmt.Errorf("marshal canonical target deployment plan: %w", err)
	}
	sum := sha256.Sum256(payload)
	return payload, "sha256:" + hex.EncodeToString(sum[:]), nil
}

func BuildTargetPlanGraph(
	_ context.Context,
	draft types.PlanDraft,
	resolutions []types.RequirementResolution,
) (types.TargetPlanGraph, error) {
	if draft.ResolutionInput == nil {
		return types.TargetPlanGraph{}, fmt.Errorf("resolution input is required")
	}
	input := draft.ResolutionInput
	steps := []types.TargetPlanStep{newTargetPlanStep(
		"config:verify",
		"Verify target configuration",
		"config_verification",
		"",
		nil,
		nil,
		"builtin",
		"target-config.verify",
		"hub",
		map[string]any{
			"snapshotId": draft.TargetConfigSnapshotID,
			"checksum":   input.Config.CanonicalChecksum,
		},
		targetLockKey(*input),
		"",
		300,
		"safe",
		"safe",
		input.Config.CanonicalChecksum,
		"all target config object checksums remain verified",
		false,
	)}
	edges := make([]types.DeploymentPlanStepEdge, 0)
	pins := normalizedReleasePins(input.ReleasePins)
	for _, pin := range pins {
		componentBinding := findComponentBinding(input.Config.ComponentBindings, pin.ComponentKey)
		previousKey := "config:verify"
		migrations := slices.Clone(pin.Migrations)
		slices.SortFunc(migrations, func(a, b types.MigrationDeclaration) int {
			if a.Order != b.Order {
				return a.Order - b.Order
			}
			return strings.Compare(a.Key, b.Key)
		})
		for _, migration := range migrations {
			stepKey := "component:" + pin.ComponentKey + ":migration:" + migration.Key
			databaseLockKey := ""
			if migration.Type == "database" || migration.Type == "data" {
				databaseLockKey = targetLockKey(*input) + ":db:" + pin.ComponentKey
			}
			step := newTargetPlanStep(
				stepKey,
				"Apply "+migration.Key,
				"migration",
				pin.ComponentKey,
				&pin.ComponentReleaseID,
				componentBindingID(componentBinding),
				"builtin",
				"component.migrate",
				"agent",
				map[string]any{
					"migrationKey": migration.Key,
					"type":         migration.Type, "compatibility": migration.Compatibility,
					"failurePolicy": migration.FailurePolicy,
				},
				targetLockKey(*input),
				databaseLockKey,
				1800,
				migrationRetryClass(migration),
				migrationCancellationBehavior(migration),
				pin.ReleaseChecksum,
				"migration completion evidence with exact input checksum",
				false,
			)
			steps = append(steps, step)
			edges = append(edges, edge(previousKey, stepKey))
			previousKey = stepKey
		}
		deployKey := "component:" + pin.ComponentKey + ":deploy"
		deploy := newTargetPlanStep(
			deployKey,
			"Deploy "+pin.ComponentKey,
			"deploy",
			pin.ComponentKey,
			&pin.ComponentReleaseID,
			componentBindingID(componentBinding),
			"builtin",
			"component.deploy",
			"agent",
			map[string]any{
				"releaseChecksum":           pin.ReleaseChecksum,
				"platform":                  input.Config.TargetPlatform,
				"platformDigest":            pin.PlatformDigest,
				"artifacts":                 pin.Artifacts,
				"provenanceBindingChecksum": pin.ProvenanceBindingChecksum,
			},
			targetLockKey(*input),
			"",
			1800,
			"bounded",
			"cooperative",
			pin.ReleaseChecksum,
			"component reports the exact desired digest and healthy state",
			false,
		)
		steps = append(steps, deploy)
		edges = append(edges, edge(previousKey, deployKey))
		healthKey := "component:" + pin.ComponentKey + ":health"
		steps = append(steps, newTargetPlanStep(
			healthKey,
			"Verify "+pin.ComponentKey+" health",
			"health",
			pin.ComponentKey,
			&pin.ComponentReleaseID,
			componentBindingID(componentBinding),
			"builtin",
			"component.health",
			"agent",
			map[string]any{"releaseChecksum": pin.ReleaseChecksum},
			targetLockKey(*input),
			"",
			600,
			"safe",
			"safe",
			pin.ReleaseChecksum,
			"trusted healthy observation for the exact component release",
			false,
		))
		edges = append(edges, edge(deployKey, healthKey))
	}

	for _, productEdge := range normalizedProductEdges(input.ProductEdges) {
		if productEdge.ResolutionStage != types.CapabilityResolutionStageProduct {
			continue
		}
		provider := strings.TrimPrefix(productEdge.From, "component:")
		consumer := strings.TrimPrefix(productEdge.To, "component:")
		from := "component:" + provider + ":health"
		to := "component:" + consumer + ":deploy"
		if hasStep(steps, from) && hasStep(steps, to) {
			edges = append(edges, edge(from, to))
		}
	}

	resolutions = slices.Clone(resolutions)
	slices.SortFunc(resolutions, func(a, b types.RequirementResolution) int {
		return strings.Compare(a.RequirementKey, b.RequirementKey)
	})
	for _, resolution := range resolutions {
		verifyKey := "requirement:" + stableKey(resolution.RequirementKey) + ":verify"
		steps = append(steps, newTargetPlanStep(
			verifyKey,
			"Verify "+resolution.Capability+" binding",
			"requirement_verification",
			resolution.ConsumerKey,
			resolution.ProviderReleaseID,
			resolution.ComponentInstanceID,
			"builtin",
			"requirement.verify",
			"hub",
			map[string]any{
				"mode":            resolution.Mode,
				"bindingChecksum": resolution.BindingChecksum,
				"observationId":   resolution.ObservationID,
			},
			targetLockKey(*input),
			"",
			300,
			"safe",
			"safe",
			resolution.BindingChecksum,
			"binding and observed-state checksum remain exact",
			false,
		))
		edges = append(edges, edge("config:verify", verifyKey))
		consumerDeploy := "component:" + resolution.ConsumerKey + ":deploy"
		if hasStep(steps, consumerDeploy) {
			edges = append(edges, edge(verifyKey, consumerDeploy))
		}
	}

	steps = normalizeTargetPlanSteps(steps)
	edges = normalizeTargetPlanEdges(edges)
	order, err := targetPlanTopologicalOrder(steps, edges)
	if err != nil {
		return types.TargetPlanGraph{}, err
	}
	graph := types.TargetPlanGraph{
		Steps: steps, Edges: edges, TopologicalOrder: order,
	}
	checksum, err := canonicalChecksum(struct {
		Steps            []types.TargetPlanStep         `json:"steps"`
		Edges            []types.DeploymentPlanStepEdge `json:"edges"`
		TopologicalOrder []string                       `json:"topologicalOrder"`
	}{graph.Steps, graph.Edges, graph.TopologicalOrder})
	if err != nil {
		return types.TargetPlanGraph{}, fmt.Errorf("canonicalize target plan graph: %w", err)
	}
	graph.Checksum = checksum
	return graph, nil
}

func ValidateProtocolGraph(protocol string, graph types.TargetPlanGraph) error {
	switch protocol {
	case types.DeploymentPlanProtocolV1:
		for _, step := range graph.Steps {
			if !step.V1Compatible {
				return fmt.Errorf("step %q is not compatible with protocol v1", step.StepKey)
			}
		}
		return nil
	case types.DeploymentPlanProtocolV2:
		return nil
	default:
		return fmt.Errorf("unsupported deployment plan protocol %q", protocol)
	}
}

func newTargetPlanStep(
	key, name, kind, componentKey string,
	releaseID, instanceID *uuid.UUID,
	actionType, actionName, location string,
	input any,
	targetLock, databaseLock string,
	timeout int,
	retryClass, cancellation, inputChecksum, observation string,
	v1Compatible bool,
) types.TargetPlanStep {
	inputJSON, _ := json.Marshal(input)
	var typedReleaseID *uuid.UUID
	if releaseID != nil {
		value := *releaseID
		typedReleaseID = &value
	}
	var typedInstanceID *uuid.UUID
	if instanceID != nil {
		value := *instanceID
		typedInstanceID = &value
	}
	return types.TargetPlanStep{
		StepKey: key, Name: name, Kind: kind, ComponentKey: componentKey,
		ComponentReleaseID: typedReleaseID, ComponentInstanceID: typedInstanceID,
		ActionType: actionType, ActionName: actionName, ExecutionLocation: location,
		InputBindings: inputJSON, TargetLockKey: targetLock, DatabaseLockKey: databaseLock,
		TimeoutSeconds: timeout, RetryClass: retryClass,
		CancellationBehavior: cancellation, ExpectedInputChecksum: inputChecksum,
		ObservationRequirement: observation, V1Compatible: v1Compatible,
	}
}

func normalizeTargetPlanSteps(steps []types.TargetPlanStep) []types.TargetPlanStep {
	slices.SortFunc(steps, func(a, b types.TargetPlanStep) int {
		if a.StepKey == "config:verify" {
			return -1
		}
		if b.StepKey == "config:verify" {
			return 1
		}
		return strings.Compare(a.StepKey, b.StepKey)
	})
	for index := range steps {
		steps[index].SortOrder = index
	}
	return steps
}

func normalizeTargetPlanEdges(edges []types.DeploymentPlanStepEdge) []types.DeploymentPlanStepEdge {
	slices.SortFunc(edges, func(a, b types.DeploymentPlanStepEdge) int {
		return strings.Compare(a.Key, b.Key)
	})
	return slices.CompactFunc(edges, func(a, b types.DeploymentPlanStepEdge) bool {
		return a.Key == b.Key && a.FromStepKey == b.FromStepKey && a.ToStepKey == b.ToStepKey
	})
}

func targetPlanTopologicalOrder(
	steps []types.TargetPlanStep,
	edges []types.DeploymentPlanStepEdge,
) ([]string, error) {
	indegree := make(map[string]int, len(steps))
	adjacency := make(map[string][]string, len(steps))
	for _, step := range steps {
		if _, duplicate := indegree[step.StepKey]; duplicate {
			return nil, fmt.Errorf("duplicate target plan step key %q", step.StepKey)
		}
		indegree[step.StepKey] = 0
	}
	for _, edge := range edges {
		if _, ok := indegree[edge.FromStepKey]; !ok {
			return nil, fmt.Errorf("target plan edge %q references unknown source", edge.Key)
		}
		if _, ok := indegree[edge.ToStepKey]; !ok {
			return nil, fmt.Errorf("target plan edge %q references unknown destination", edge.Key)
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
		return nil, fmt.Errorf("target plan graph contains a cycle")
	}
	return order, nil
}

func normalizedReleasePins(pins []types.ComponentReleasePin) []types.ComponentReleasePin {
	pins = slices.Clone(pins)
	for index := range pins {
		pins[index].ComponentKey = strings.TrimSpace(pins[index].ComponentKey)
		pins[index].Platforms = normalizedStrings(pins[index].Platforms)
		pins[index].Artifacts = slices.Clone(pins[index].Artifacts)
		slices.SortFunc(pins[index].Artifacts, func(a, b types.PinnedReleaseArtifact) int {
			if cmp := strings.Compare(a.Key, b.Key); cmp != 0 {
				return cmp
			}
			return strings.Compare(a.Platform, b.Platform)
		})
		pins[index].ProvenanceFacts = slices.Clone(pins[index].ProvenanceFacts)
		slices.SortFunc(
			pins[index].ProvenanceFacts,
			func(a, b types.ComponentProvenanceFact) int {
				if cmp := strings.Compare(a.ArtifactKey, b.ArtifactKey); cmp != 0 {
					return cmp
				}
				if cmp := strings.Compare(a.Platform, b.Platform); cmp != 0 {
					return cmp
				}
				return strings.Compare(a.VerificationID.String(), b.VerificationID.String())
			},
		)
	}
	slices.SortFunc(pins, func(a, b types.ComponentReleasePin) int {
		return strings.Compare(a.ComponentKey, b.ComponentKey)
	})
	return pins
}

func normalizedProductEdges(edges []types.GraphEdge) []types.GraphEdge {
	edges = slices.Clone(edges)
	slices.SortFunc(edges, func(a, b types.GraphEdge) int {
		return strings.Compare(a.Key, b.Key)
	})
	return edges
}

func findComponentBinding(
	bindings []types.ConfigComponentBinding,
	componentKey string,
) *types.ConfigComponentBinding {
	for index := range bindings {
		if strings.TrimSpace(bindings[index].ComponentKey) == componentKey {
			value := bindings[index]
			return &value
		}
	}
	return nil
}

func componentBindingID(binding *types.ConfigComponentBinding) *uuid.UUID {
	if binding == nil {
		return nil
	}
	value := binding.ComponentInstanceID
	return &value
}

func migrationRetryClass(migration types.MigrationDeclaration) string {
	if migration.FailurePolicy == "retry" {
		return "bounded"
	}
	return "none"
}

func migrationCancellationBehavior(migration types.MigrationDeclaration) string {
	if migration.Compatibility == "breaking" {
		return "forward_fix_only"
	}
	return "cooperative"
}

func targetLockKey(input types.PlanResolutionInput) string {
	return "target:" + input.Assignment.DeploymentTargetID.String() +
		":unit:" + input.Unit.ID.String()
}

func stableKey(value string) string {
	replacer := strings.NewReplacer(":", ".", "/", ".", " ", "-")
	return replacer.Replace(strings.TrimSpace(value))
}

func edge(from, to string) types.DeploymentPlanStepEdge {
	return types.DeploymentPlanStepEdge{
		Key: from + "->" + to, FromStepKey: from, ToStepKey: to,
	}
}

func hasStep(steps []types.TargetPlanStep, key string) bool {
	for _, step := range steps {
		if step.StepKey == key {
			return true
		}
	}
	return false
}
