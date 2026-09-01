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

	"github.com/distr-sh/distr/internal/migrationplanning"
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
	canonical.Baselines = slices.Clone(canonical.Baselines)
	slices.SortFunc(canonical.Baselines, func(a, b types.DeploymentPlanBaseline) int {
		if a.SortOrder != b.SortOrder {
			return a.SortOrder - b.SortOrder
		}
		return strings.Compare(a.ComponentKey, b.ComponentKey)
	})
	canonical.Changes = slices.Clone(canonical.Changes)
	slices.SortFunc(canonical.Changes, func(a, b types.DeploymentPlanChangeEntry) int {
		if a.SortOrder != b.SortOrder {
			return a.SortOrder - b.SortOrder
		}
		if cmp := strings.Compare(a.ComponentKey, b.ComponentKey); cmp != 0 {
			return cmp
		}
		return strings.Compare(string(a.Kind), string(b.Kind))
	})
	canonical.Risks = slices.Clone(canonical.Risks)
	slices.SortFunc(canonical.Risks, func(a, b types.DeploymentPlanRiskEntry) int {
		if a.SortOrder != b.SortOrder {
			return a.SortOrder - b.SortOrder
		}
		if cmp := strings.Compare(a.ComponentKey, b.ComponentKey); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.Code, b.Code)
	})
	var err error
	canonical.MigrationContracts, err = migrationplanning.OrderMigrationContracts(
		canonical.MigrationContracts,
	)
	if err != nil {
		return nil, "", fmt.Errorf("order canonical target-plan migrations: %w", err)
	}
	canonical.SchemaEvidenceRequirements = slices.Clone(canonical.SchemaEvidenceRequirements)
	slices.SortFunc(
		canonical.SchemaEvidenceRequirements,
		func(left, right types.SchemaEvidenceRequirement) int {
			if cmp := strings.Compare(left.ComponentKey, right.ComponentKey); cmp != 0 {
				return cmp
			}
			return strings.Compare(left.DatabaseResourceKey, right.DatabaseResourceKey)
		},
	)
	canonical.SchemaEvidence = slices.Clone(canonical.SchemaEvidence)
	for index := range canonical.SchemaEvidence {
		facts := slices.Clone(canonical.SchemaEvidence[index].MigrationEvidence.MixedVersionEvidence)
		slices.SortFunc(facts, compareMixedVersionSchemaEvidence)
		canonical.SchemaEvidence[index].MigrationEvidence.MixedVersionEvidence = facts
	}
	slices.SortFunc(canonical.SchemaEvidence, func(left, right types.SchemaEvidenceBundle) int {
		if cmp := strings.Compare(left.Requirement.ComponentKey, right.Requirement.ComponentKey); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(left.Requirement.DatabaseResourceKey, right.Requirement.DatabaseResourceKey); cmp != 0 {
			return cmp
		}
		return strings.Compare(left.SchemaReportObject.ObjectKey, right.SchemaReportObject.ObjectKey)
	})
	canonical.StepAdapters = slices.Clone(canonical.StepAdapters)
	slices.SortFunc(canonical.StepAdapters, func(a, b types.ResolvedPlanStepAdapter) int {
		return strings.Compare(a.StepKey, b.StepKey)
	})
	payload, err := json.Marshal(canonical)
	if err != nil {
		return nil, "", fmt.Errorf("marshal canonical target deployment plan: %w", err)
	}
	if len(payload) > MaxTargetPlanPayloadBytes {
		return nil, "", fmt.Errorf(
			"canonical target deployment plan exceeds payload limit of %d bytes",
			MaxTargetPlanPayloadBytes,
		)
	}
	sum := sha256.Sum256(payload)
	return payload, "sha256:" + hex.EncodeToString(sum[:]), nil
}

func compareMixedVersionSchemaEvidence(
	left, right types.MixedVersionSchemaEvidence,
) int {
	if cmp := strings.Compare(left.ApplicationVersion, right.ApplicationVersion); cmp != 0 {
		return cmp
	}
	if cmp := strings.Compare(left.SchemaVersion, right.SchemaVersion); cmp != 0 {
		return cmp
	}
	if cmp := strings.Compare(left.SchemaChecksum, right.SchemaChecksum); cmp != 0 {
		return cmp
	}
	if left.Compatible == right.Compatible {
		return 0
	}
	if left.Compatible {
		return 1
	}
	return -1
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
	if issues := validateTargetPlanSize(*input); len(issues) > 0 {
		return types.TargetPlanGraph{}, fmt.Errorf("%s", issues[0].Message)
	}
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
		true,
	)}
	edges := make([]types.DeploymentPlanStepEdge, 0)
	pins := normalizedReleasePins(input.ReleasePins)
	entryStepsByComponent := make(map[string][]string, len(pins))
	pinByComponent := make(map[string]types.ComponentReleasePin, len(pins))
	structuredContracts := make([]types.MigrationContract, 0)
	for _, pin := range pins {
		pinByComponent[pin.ComponentKey] = pin
		declarations := make(map[string]types.MigrationDeclaration, len(pin.Migrations))
		for _, declaration := range pin.Migrations {
			declarations[declaration.Key] = declaration
		}
		structured := make(map[string]struct{}, len(pin.MigrationContracts))
		binding := findComponentBinding(input.Config.ComponentBindings, pin.ComponentKey)
		instance := componentInstanceForBinding(input.ComponentInstances, binding)
		for _, contract := range pin.MigrationContracts {
			declaration, declared := declarations[contract.ID]
			if !declared || (declaration.Type != "database" && declaration.Type != "data") {
				return types.TargetPlanGraph{}, fmt.Errorf(
					"structured migration %q has no matching database or data declaration",
					contract.ID,
				)
			}
			if contract.ComponentKey != pin.ComponentKey {
				return types.TargetPlanGraph{}, fmt.Errorf(
					"structured migration %q belongs to component %q, not %q",
					contract.ID,
					contract.ComponentKey,
					pin.ComponentKey,
				)
			}
			if instance == nil ||
				strings.TrimSpace(instance.DatabaseBoundary) != contract.DatabaseResourceKey {
				return types.TargetPlanGraph{}, fmt.Errorf(
					"structured migration %q does not match the component database boundary",
					contract.ID,
				)
			}
			structured[contract.ID] = struct{}{}
		}
		for _, declaration := range pin.Migrations {
			if declaration.Type != "database" && declaration.Type != "data" {
				continue
			}
			if _, exists := structured[declaration.Key]; !exists {
				return types.TargetPlanGraph{}, fmt.Errorf(
					"database or data migration %q requires a structured contract",
					declaration.Key,
				)
			}
		}
		structuredContracts = append(structuredContracts, pin.MigrationContracts...)
	}
	orderedContracts, err := migrationplanning.OrderMigrationContracts(structuredContracts)
	if err != nil {
		return types.TargetPlanGraph{}, fmt.Errorf("order structured migration contracts: %w", err)
	}
	structuredByComponent := make(map[string]bool, len(orderedContracts))
	for _, contract := range orderedContracts {
		structuredByComponent[contract.ComponentKey] = true
	}
	for _, pin := range pins {
		componentBinding := findComponentBinding(input.Config.ComponentBindings, pin.ComponentKey)
		previousKey := "config:verify"
		migrations := make([]types.MigrationDeclaration, 0, len(pin.Migrations))
		for _, migration := range pin.Migrations {
			if migration.Type == "runtime" {
				migrations = append(migrations, migration)
			}
		}
		slices.SortFunc(migrations, func(a, b types.MigrationDeclaration) int {
			if a.Order != b.Order {
				return a.Order - b.Order
			}
			return strings.Compare(a.Key, b.Key)
		})
		for _, migration := range migrations {
			stepKey := "component:" + pin.ComponentKey + ":migration:" + migration.Key
			step := newTargetPlanStep(
				stepKey,
				"Apply "+migration.Key,
				"migration",
				pin.ComponentKey,
				&pin.ComponentReleaseID,
				componentBindingID(componentBinding),
				"builtin",
				"component.migrate",
				"target",
				map[string]any{
					"migrationKey": migration.Key,
					"type":         migration.Type, "compatibility": migration.Compatibility,
					"failurePolicy": migration.FailurePolicy,
				},
				targetLockKey(*input),
				"",
				900,
				migrationRetryClass(migration),
				migrationCancellationBehavior(migration),
				pin.ReleaseChecksum,
				"migration completion evidence with exact input checksum",
				migration.Compatibility != "breaking",
			)
			steps = append(steps, step)
			if len(entryStepsByComponent[pin.ComponentKey]) == 0 {
				entryStepsByComponent[pin.ComponentKey] = append(
					entryStepsByComponent[pin.ComponentKey],
					stepKey,
				)
			}
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
			"target",
			map[string]any{
				"releaseChecksum":           pin.ReleaseChecksum,
				"platform":                  input.Config.TargetPlatform,
				"platformDigest":            pin.PlatformDigest,
				"artifacts":                 pin.Artifacts,
				"provenanceBindingChecksum": pin.ProvenanceBindingChecksum,
			},
			targetLockKey(*input),
			"",
			900,
			"bounded",
			"cooperative",
			pin.ReleaseChecksum,
			"component reports the exact desired digest and healthy state",
			true,
		)
		steps = append(steps, deploy)
		if len(entryStepsByComponent[pin.ComponentKey]) == 0 &&
			!structuredByComponent[pin.ComponentKey] {
			entryStepsByComponent[pin.ComponentKey] = append(
				entryStepsByComponent[pin.ComponentKey],
				deployKey,
			)
		}
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
			"target",
			map[string]any{"releaseChecksum": pin.ReleaseChecksum},
			targetLockKey(*input),
			"",
			600,
			"safe",
			"safe",
			pin.ReleaseChecksum,
			"trusted healthy observation for the exact component release",
			true,
		))
		edges = append(edges, edge(deployKey, healthKey))
	}

	graph := types.TargetPlanGraph{Steps: steps, Edges: edges}
	for _, contract := range orderedContracts {
		entryKey := migrationContractEntryStepKey(contract)
		graph.Edges = append(graph.Edges, edge("config:verify", entryKey))
		graph, err = migrationplanning.ExpandMigrationGraph(contract, graph)
		if err != nil {
			return types.TargetPlanGraph{}, err
		}
		entryStepsByComponent[contract.ComponentKey] = append(
			entryStepsByComponent[contract.ComponentKey],
			entryKey,
		)
	}
	steps = graph.Steps
	edges = graph.Edges
	for index := range steps {
		if !strings.HasPrefix(steps[index].StepKey, "migration:") {
			continue
		}
		pin, exists := pinByComponent[steps[index].ComponentKey]
		if !exists {
			continue
		}
		steps[index].ComponentReleaseID = cloneUUID(&pin.ComponentReleaseID)
		steps[index].ComponentInstanceID = componentBindingID(
			findComponentBinding(input.Config.ComponentBindings, pin.ComponentKey),
		)
	}
	for componentKey := range entryStepsByComponent {
		slices.Sort(entryStepsByComponent[componentKey])
		entryStepsByComponent[componentKey] = slices.Compact(entryStepsByComponent[componentKey])
	}

	for _, productEdge := range normalizedProductEdges(input.ProductEdges) {
		if productEdge.ResolutionStage != types.CapabilityResolutionStageProduct {
			continue
		}
		provider := strings.TrimPrefix(productEdge.From, "component:")
		consumer := strings.TrimPrefix(productEdge.To, "component:")
		from := "component:" + provider + ":health"
		for _, to := range entryStepsByComponent[consumer] {
			if hasStep(steps, from) && hasStep(steps, to) {
				edges = append(edges, edge(from, to))
			}
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
				"mode":                     resolution.Mode,
				"bindingChecksum":          resolution.BindingChecksum,
				"observationId":            resolution.ObservationID,
				"activeDesiredRevisionId":  resolution.ActiveDesiredRevisionID,
				"observedComponentStateId": resolution.ObservedComponentStateID,
			},
			targetLockKey(*input),
			"",
			300,
			"safe",
			"safe",
			resolution.BindingChecksum,
			"binding and observed-state checksum remain exact",
			resolution.V1Compatible,
		))
		edges = append(edges, edge("config:verify", verifyKey))
		for _, consumerEntryStep := range entryStepsByComponent[resolution.ConsumerKey] {
			if hasStep(steps, consumerEntryStep) {
				edges = append(edges, edge(verifyKey, consumerEntryStep))
			}
		}
	}

	if len(steps) > MaxTargetPlanSteps {
		return types.TargetPlanGraph{}, fmt.Errorf(
			"target plan exceeds step limit of %d",
			MaxTargetPlanSteps,
		)
	}
	if len(edges) > MaxTargetPlanEdges {
		return types.TargetPlanGraph{}, fmt.Errorf(
			"target plan exceeds edge limit of %d",
			MaxTargetPlanEdges,
		)
	}
	steps = normalizeTargetPlanSteps(steps)
	edges = normalizeTargetPlanEdges(edges)
	order, err := targetPlanTopologicalOrder(steps, edges)
	if err != nil {
		return types.TargetPlanGraph{}, err
	}
	graph = types.TargetPlanGraph{
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

func migrationContractEntryStepKey(contract types.MigrationContract) string {
	if contract.BackupRequired {
		return "migration:" + contract.ID + ":backup:create"
	}
	return "migration:" + contract.ID + ":precondition"
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
		pins[index].MigrationContracts = slices.Clone(pins[index].MigrationContracts)
		for migrationIndex := range pins[index].MigrationContracts {
			pins[index].MigrationContracts[migrationIndex] =
				migrationplanning.NormalizeMigrationContract(
					pins[index].MigrationContracts[migrationIndex],
				)
		}
		slices.SortFunc(
			pins[index].MigrationContracts,
			func(a, b types.MigrationContract) int { return strings.Compare(a.ID, b.ID) },
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

func componentInstanceForBinding(
	instances []types.ComponentInstance,
	binding *types.ConfigComponentBinding,
) *types.ComponentInstance {
	if binding == nil {
		return nil
	}
	for index := range instances {
		if instances[index].ID == binding.ComponentInstanceID {
			value := instances[index]
			return &value
		}
	}
	return nil
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
