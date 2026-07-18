package productrelease

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

var (
	productReleaseChecksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	productReleaseKeyPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
)

type capabilityProvider struct {
	nodeKey    string
	component  types.ProductReleaseComponent
	capability types.CapabilityDeclaration
}

type graphRequirement struct {
	consumerKey string
	field       string
	requirement types.CapabilityRequirement
}

func BuildProductReleaseGraph(manifest types.ProductReleaseManifest) types.ProductReleaseGraph {
	components := normalizedComponents(manifest.Components)
	nodes := make([]types.GraphNode, 0, len(components)+len(manifest.Requirements))
	nodeKeys := make(map[string]struct{}, len(components))
	providers := make(map[string][]capabilityProvider)

	for _, component := range components {
		nodeKey := componentNodeKey(component.ComponentKey)
		if _, exists := nodeKeys[nodeKey]; !exists {
			componentReleaseID := component.ComponentReleaseID
			nodes = append(nodes, types.GraphNode{
				Key:                nodeKey,
				Kind:               "component",
				ComponentReleaseID: &componentReleaseID,
				ComponentKey:       component.ComponentKey,
				Version:            component.Version,
			})
			nodeKeys[nodeKey] = struct{}{}
		}
		for _, capability := range component.Provides {
			providers[capability.Name] = append(providers[capability.Name], capabilityProvider{
				nodeKey: nodeKey, component: component, capability: capability,
			})
		}
	}
	for name := range providers {
		slices.SortFunc(providers[name], func(a, b capabilityProvider) int {
			if cmp := strings.Compare(a.nodeKey, b.nodeKey); cmp != 0 {
				return cmp
			}
			return strings.Compare(a.capability.Version, b.capability.Version)
		})
	}

	requirements := make([]graphRequirement, 0)
	for _, component := range components {
		for i, requirement := range component.Requires {
			requirements = append(requirements, graphRequirement{
				consumerKey: componentNodeKey(component.ComponentKey),
				field:       fmt.Sprintf("components.%s.requires.%d", component.ComponentKey, i),
				requirement: requirement,
			})
		}
	}
	if len(manifest.Requirements) > 0 {
		productNode := productNodeKey(manifest.Product)
		nodes = append(nodes, types.GraphNode{Key: productNode, Kind: "product", ComponentKey: manifest.Product})
		for i, requirement := range manifest.Requirements {
			requirements = append(requirements, graphRequirement{
				consumerKey: productNode,
				field:       fmt.Sprintf("requirements.%d", i),
				requirement: requirement,
			})
		}
	}
	slices.SortFunc(requirements, func(a, b graphRequirement) int {
		if cmp := strings.Compare(a.consumerKey, b.consumerKey); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(a.requirement.Name, b.requirement.Name); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.requirement.Range, b.requirement.Range)
	})

	edges := make([]types.GraphEdge, 0, len(requirements))
	for _, entry := range requirements {
		requirement := normalizedRequirement(entry.requirement)
		stage := types.CapabilityResolutionStage(requirement.ResolutionStage)
		if stage == types.CapabilityResolutionStageTarget {
			nodeKey := targetRequirementNodeKey(entry.consumerKey, requirement.Name)
			if _, exists := nodeKeys[nodeKey]; !exists {
				nodes = append(nodes, types.GraphNode{
					Key:             nodeKey,
					Kind:            "target_requirement",
					Capability:      requirement.Name,
					VersionRange:    requirement.Range,
					ResolutionStage: stage,
					AllowedModes:    normalizedResolutionModes(requirement.AllowedModes),
					Unresolved:      true,
				})
				nodeKeys[nodeKey] = struct{}{}
			}
			edges = append(edges, types.GraphEdge{
				Key:             graphEdgeKey(nodeKey, entry.consumerKey, requirement.Name),
				From:            nodeKey,
				To:              entry.consumerKey,
				Capability:      requirement.Name,
				VersionRange:    requirement.Range,
				ResolutionStage: stage,
				AllowedModes:    normalizedResolutionModes(requirement.AllowedModes),
			})
			continue
		}
		matches := matchingProviders(providers[requirement.Name], requirement.Range)
		if len(matches) == 1 {
			provider := matches[0]
			edges = append(edges, types.GraphEdge{
				Key:             graphEdgeKey(provider.nodeKey, entry.consumerKey, requirement.Name),
				From:            provider.nodeKey,
				To:              entry.consumerKey,
				Capability:      requirement.Name,
				VersionRange:    requirement.Range,
				ProviderVersion: provider.capability.Version,
				ResolutionStage: types.CapabilityResolutionStageProduct,
				Ordering:        "provider_deploy_and_health_before_consumer",
			})
		}
	}

	slices.SortFunc(nodes, func(a, b types.GraphNode) int { return strings.Compare(a.Key, b.Key) })
	slices.SortFunc(edges, func(a, b types.GraphEdge) int { return strings.Compare(a.Key, b.Key) })
	return types.ProductReleaseGraph{
		Nodes:            nodes,
		Edges:            edges,
		TopologicalOrder: stableTopologicalOrder(nodes, edges),
	}
}

func ValidateProductReleaseGraph(manifest types.ProductReleaseManifest) []types.ProductReleaseValidationIssue {
	collector := &productReleaseIssueCollector{}
	validateProductReleaseIdentity(manifest, collector)
	components, providers := validateProductReleaseComponents(manifest, collector)
	validateProductReleaseRequirements(manifest, components, providers, collector)
	graph := BuildProductReleaseGraph(manifest)
	if path := exactCyclePath(graph.Nodes, graph.Edges); len(path) > 0 {
		collector.add("graph", "cycle", "capability graph contains a cycle: "+strings.Join(path, " -> "), path...)
	}
	slices.SortFunc(collector.issues, func(a, b types.ProductReleaseValidationIssue) int {
		if cmp := strings.Compare(a.Field, b.Field); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(a.Rule, b.Rule); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(a.Message, b.Message); cmp != 0 {
			return cmp
		}
		return strings.Compare(strings.Join(a.Path, "\x00"), strings.Join(b.Path, "\x00"))
	})
	return collector.issues
}

type productReleaseIssueCollector struct {
	issues []types.ProductReleaseValidationIssue
}

func (c *productReleaseIssueCollector) add(field, rule, message string, path ...string) {
	c.issues = append(c.issues, types.ProductReleaseValidationIssue{
		Field: field, Rule: rule, Message: message, Path: slices.Clone(path),
	})
}

func validateProductReleaseIdentity(
	manifest types.ProductReleaseManifest,
	collector *productReleaseIssueCollector,
) {
	if strings.TrimSpace(manifest.Schema) != types.ProductReleaseSchemaV1 {
		collector.add("schema", "supported", "schema must be distr.product-release/v1")
	}
	if !productReleaseKeyPattern.MatchString(strings.TrimSpace(manifest.Product)) {
		collector.add("product", "key", "product must be a lowercase stable key")
	}
	if strings.TrimSpace(manifest.Version) == "" {
		collector.add("version", "required", "product release version is required")
	}
	if manifest.DependencyPolicyVersion == uuid.Nil {
		collector.add("dependencyPolicyVersion", "required", "dependency policy version is required")
	}
	if len(manifest.Components) == 0 {
		collector.add("components", "required", "at least one component release is required")
	}
}

func validateProductReleaseComponents(
	manifest types.ProductReleaseManifest,
	collector *productReleaseIssueCollector,
) ([]types.ProductReleaseComponent, map[string][]capabilityProvider) {
	components := normalizedComponents(manifest.Components)
	seenKeys := make(map[string]struct{}, len(components))
	seenIDs := make(map[uuid.UUID]struct{}, len(components))
	providers := make(map[string][]capabilityProvider)
	migrationContracts := make(map[string]types.MigrationDeclaration)
	requiredPlatforms := normalizedStrings(manifest.RequiredPlatforms)
	for index, component := range components {
		field := fmt.Sprintf("components.%d", index)
		validateProductReleaseComponentIdentity(
			manifest,
			component,
			field,
			seenIDs,
			seenKeys,
			collector,
		)
		validateProductReleaseComponentPlatforms(component, field, requiredPlatforms, collector)
		validateProductReleaseComponentCapabilities(component, field, providers, collector)
		validateProductReleaseComponentMigrations(component, field, migrationContracts, collector)
	}
	return components, providers
}

func validateProductReleaseComponentIdentity(
	manifest types.ProductReleaseManifest,
	component types.ProductReleaseComponent,
	field string,
	seenIDs map[uuid.UUID]struct{},
	seenKeys map[string]struct{},
	collector *productReleaseIssueCollector,
) {
	if component.ComponentReleaseID == uuid.Nil {
		collector.add(field+".componentReleaseId", "required", "component release id is required")
	}
	if _, duplicate := seenIDs[component.ComponentReleaseID]; duplicate {
		collector.add(field+".componentReleaseId", "uniqueRelease", "component release ids must be unique")
	}
	seenIDs[component.ComponentReleaseID] = struct{}{}
	if !productReleaseKeyPattern.MatchString(strings.TrimSpace(component.ComponentKey)) {
		collector.add(field+".componentKey", "key", "component key must be a lowercase stable key")
	}
	if _, duplicate := seenKeys[component.ComponentKey]; duplicate {
		collector.add(field+".componentKey", "uniqueComponent", "component keys must be unique")
	}
	seenKeys[component.ComponentKey] = struct{}{}
	if !component.Published {
		collector.add(field+".componentReleaseId", "published", "component release must be published")
	}
	if manifest.OrganizationID != uuid.Nil && component.OrganizationID != manifest.OrganizationID {
		collector.add(
			field+".componentReleaseId",
			"organization",
			"component release must belong to the product organization",
		)
	}
	if !productReleaseChecksumPattern.MatchString(component.ComponentReleaseChecksum) {
		collector.add(
			field+".componentReleaseChecksum",
			"checksum",
			"component release checksum must be lowercase sha256",
		)
	}
	if _, err := semver.StrictNewVersion(component.Version); err != nil {
		collector.add(field+".version", "semver", "component version must be strict semantic version")
	}
}

func validateProductReleaseComponentPlatforms(
	component types.ProductReleaseComponent,
	field string,
	requiredPlatforms []string,
	collector *productReleaseIssueCollector,
) {
	platformSet := make(map[string]struct{}, len(component.Platforms))
	for _, platform := range component.Platforms {
		platformSet[platform] = struct{}{}
	}
	for _, platform := range requiredPlatforms {
		if _, ok := platformSet[platform]; !ok {
			collector.add(
				field+".platforms",
				"platformCoverage",
				fmt.Sprintf("component %q does not provide required platform %q", component.ComponentKey, platform),
			)
		}
	}
}

func validateProductReleaseComponentCapabilities(
	component types.ProductReleaseComponent,
	field string,
	providers map[string][]capabilityProvider,
	collector *productReleaseIssueCollector,
) {
	providedNames := make(map[string]struct{}, len(component.Provides))
	for index, capability := range component.Provides {
		provideField := fmt.Sprintf("%s.provides.%d", field, index)
		if !productReleaseKeyPattern.MatchString(strings.TrimSpace(capability.Name)) {
			collector.add(provideField+".name", "key", "provided capability name must be a lowercase stable key")
		}
		if _, duplicate := providedNames[capability.Name]; duplicate {
			collector.add(provideField+".name", "unique", "provided capability names must be unique per component")
		}
		providedNames[capability.Name] = struct{}{}
		if _, err := semver.StrictNewVersion(capability.Version); err != nil {
			collector.add(provideField+".version", "semver", "provided capability version must be strict semantic version")
		}
		providers[capability.Name] = append(providers[capability.Name], capabilityProvider{
			nodeKey: componentNodeKey(component.ComponentKey), component: component, capability: capability,
		})
	}
}

func validateProductReleaseComponentMigrations(
	component types.ProductReleaseComponent,
	field string,
	migrationContracts map[string]types.MigrationDeclaration,
	collector *productReleaseIssueCollector,
) {
	for index, migration := range component.Migrations {
		migrationField := fmt.Sprintf("%s.migrations.%d", field, index)
		previous, exists := migrationContracts[migration.Key]
		if exists &&
			(previous.Type != migration.Type ||
				previous.Compatibility != migration.Compatibility ||
				previous.FailurePolicy != migration.FailurePolicy) {
			collector.add(
				migrationField,
				"migrationConflict",
				"migration key has incompatible declarations across components",
			)
		} else {
			migrationContracts[migration.Key] = migration
		}
	}
}

func validateProductReleaseRequirements(
	manifest types.ProductReleaseManifest,
	components []types.ProductReleaseComponent,
	providers map[string][]capabilityProvider,
	collector *productReleaseIssueCollector,
) {
	requirements := make([]graphRequirement, 0)
	for _, component := range components {
		for index, requirement := range component.Requires {
			requirements = append(requirements, graphRequirement{
				consumerKey: componentNodeKey(component.ComponentKey),
				field:       fmt.Sprintf("components.%s.requires.%d", component.ComponentKey, index),
				requirement: requirement,
			})
		}
	}
	for index, requirement := range manifest.Requirements {
		requirements = append(requirements, graphRequirement{
			consumerKey: productNodeKey(manifest.Product),
			field:       fmt.Sprintf("requirements.%d", index),
			requirement: requirement,
		})
	}
	slices.SortFunc(requirements, func(a, b graphRequirement) int {
		if cmp := strings.Compare(a.consumerKey, b.consumerKey); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(a.requirement.Name, b.requirement.Name); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.requirement.Range, b.requirement.Range)
	})

	seenRequirements := make(map[string]struct{}, len(requirements))
	for _, entry := range requirements {
		requirement := normalizedRequirement(entry.requirement)
		requirementKey := entry.consumerKey + "\x00" + requirement.Name
		if _, duplicate := seenRequirements[requirementKey]; duplicate {
			collector.add(
				entry.field+".name",
				"uniqueRequirement",
				"required capability names must be unique per consumer",
			)
		}
		seenRequirements[requirementKey] = struct{}{}
		if !productReleaseKeyPattern.MatchString(requirement.Name) {
			collector.add(entry.field+".name", "key", "required capability name must be a lowercase stable key")
		}
		if _, err := semver.NewConstraint(requirement.Range); err != nil {
			collector.add(entry.field+".range", "semverRange", "required capability range must be valid")
		}
		stage := types.CapabilityResolutionStage(requirement.ResolutionStage)
		if !stage.IsValid() {
			collector.add(entry.field+".resolutionStage", "supported", "resolution stage must be product or target")
			continue
		}
		if stage == types.CapabilityResolutionStageTarget {
			validateTargetModes(entry.field, requirement.AllowedModes, collector.add)
			continue
		}
		if len(requirement.AllowedModes) != 0 {
			collector.add(
				entry.field+".allowedModes",
				"productStageGap",
				"product-stage requirements cannot defer to target modes",
			)
		}
		candidates := providers[requirement.Name]
		matches := matchingProviders(candidates, requirement.Range)
		switch {
		case len(candidates) == 0:
			collector.add(entry.field, "missingProvider", "no component release provides the required capability")
			collector.add(entry.field, "productStageGap", "product-stage requirement is unresolved")
		case len(matches) == 0:
			collector.add(
				entry.field+".range",
				"incompatibleRange",
				"no provider version satisfies the required semantic version range",
			)
			collector.add(entry.field, "productStageGap", "product-stage requirement is unresolved")
		case len(matches) > 1:
			collector.add(
				entry.field,
				"ambiguousProvider",
				"more than one component release satisfies the required capability",
			)
			collector.add(
				entry.field,
				"productStageGap",
				"product-stage requirement does not resolve to exactly one provider",
			)
		}
	}
}

func validateTargetModes(
	field string,
	modes []string,
	add func(string, string, string, ...string),
) {
	if len(modes) == 0 {
		add(field+".allowedModes", "allowedModes", "target requirements must declare allowed resolution modes")
		return
	}
	seen := make(map[types.RequirementResolutionMode]struct{}, len(modes))
	for _, rawMode := range modes {
		mode := types.RequirementResolutionMode(strings.TrimSpace(rawMode))
		if !mode.IsValid() {
			add(
				field+".allowedModes",
				"supported",
				"resolution mode must be included, pinned_existing, shared_provider, approved_external, or feature_disabled",
			)
			continue
		}
		if _, duplicate := seen[mode]; duplicate {
			add(field+".allowedModes", "unique", "target resolution modes must be unique")
		}
		seen[mode] = struct{}{}
	}
}

func matchingProviders(providers []capabilityProvider, versionRange string) []capabilityProvider {
	constraint, err := semver.NewConstraint(versionRange)
	if err != nil {
		return nil
	}
	matches := make([]capabilityProvider, 0, len(providers))
	for _, provider := range providers {
		version, err := semver.StrictNewVersion(provider.capability.Version)
		if err == nil && constraint.Check(version) {
			matches = append(matches, provider)
		}
	}
	return matches
}

func stableTopologicalOrder(nodes []types.GraphNode, edges []types.GraphEdge) []string {
	indegree := make(map[string]int, len(nodes))
	adjacency := make(map[string][]string, len(nodes))
	for _, node := range nodes {
		indegree[node.Key] = 0
	}
	for _, edge := range edges {
		if edge.From == edge.To {
			indegree[edge.To]++
			adjacency[edge.From] = append(adjacency[edge.From], edge.To)
			continue
		}
		indegree[edge.To]++
		adjacency[edge.From] = append(adjacency[edge.From], edge.To)
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
	order := make([]string, 0, len(nodes))
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
	return order
}

func exactCyclePath(nodes []types.GraphNode, edges []types.GraphEdge) []string {
	adjacency := make(map[string][]string, len(nodes))
	nodeKeys := make([]string, 0, len(nodes))
	for _, node := range nodes {
		nodeKeys = append(nodeKeys, node.Key)
	}
	for _, edge := range edges {
		adjacency[edge.From] = append(adjacency[edge.From], edge.To)
	}
	sort.Strings(nodeKeys)
	for key := range adjacency {
		sort.Strings(adjacency[key])
	}
	state := make(map[string]uint8, len(nodes))
	stack := make([]string, 0, len(nodes))
	stackIndex := make(map[string]int, len(nodes))
	var cycle []string
	var visit func(string) bool
	visit = func(node string) bool {
		state[node] = 1
		stackIndex[node] = len(stack)
		stack = append(stack, node)
		for _, next := range adjacency[node] {
			switch state[next] {
			case 0:
				if visit(next) {
					return true
				}
			case 1:
				start := stackIndex[next]
				cycle = append(slices.Clone(stack[start:]), next)
				return true
			}
		}
		delete(stackIndex, node)
		stack = stack[:len(stack)-1]
		state[node] = 2
		return false
	}
	for _, node := range nodeKeys {
		if state[node] == 0 && visit(node) {
			return cycle
		}
	}
	return nil
}

func normalizedComponents(input []types.ProductReleaseComponent) []types.ProductReleaseComponent {
	components := slices.Clone(input)
	for index := range components {
		components[index].ComponentKey = strings.TrimSpace(components[index].ComponentKey)
		components[index].Version = strings.TrimSpace(components[index].Version)
		components[index].ComponentReleaseChecksum = strings.TrimSpace(components[index].ComponentReleaseChecksum)
		components[index].Platforms = normalizedStrings(components[index].Platforms)
		components[index].Provides = slices.Clone(components[index].Provides)
		for i := range components[index].Provides {
			components[index].Provides[i].Name = strings.TrimSpace(components[index].Provides[i].Name)
			components[index].Provides[i].Version = strings.TrimSpace(components[index].Provides[i].Version)
		}
		slices.SortFunc(components[index].Provides, func(a, b types.CapabilityDeclaration) int {
			if cmp := strings.Compare(a.Name, b.Name); cmp != 0 {
				return cmp
			}
			return strings.Compare(a.Version, b.Version)
		})
		components[index].Requires = slices.Clone(components[index].Requires)
		for i := range components[index].Requires {
			components[index].Requires[i] = normalizedRequirement(components[index].Requires[i])
		}
		slices.SortFunc(components[index].Requires, func(a, b types.CapabilityRequirement) int {
			if cmp := strings.Compare(a.Name, b.Name); cmp != 0 {
				return cmp
			}
			if cmp := strings.Compare(a.Range, b.Range); cmp != 0 {
				return cmp
			}
			return strings.Compare(a.ResolutionStage, b.ResolutionStage)
		})
		components[index].Migrations = slices.Clone(components[index].Migrations)
		slices.SortFunc(components[index].Migrations, func(a, b types.MigrationDeclaration) int {
			if a.Order != b.Order {
				return a.Order - b.Order
			}
			return strings.Compare(a.Key, b.Key)
		})
	}
	slices.SortFunc(components, func(a, b types.ProductReleaseComponent) int {
		if cmp := strings.Compare(a.ComponentKey, b.ComponentKey); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.ComponentReleaseID.String(), b.ComponentReleaseID.String())
	})
	return components
}

func normalizedRequirement(requirement types.CapabilityRequirement) types.CapabilityRequirement {
	requirement.Name = strings.TrimSpace(requirement.Name)
	requirement.Range = strings.TrimSpace(requirement.Range)
	requirement.ResolutionStage = strings.TrimSpace(requirement.ResolutionStage)
	requirement.AllowedModes = normalizedStrings(requirement.AllowedModes)
	return requirement
}

func normalizedResolutionModes(input []string) []types.RequirementResolutionMode {
	values := normalizedStrings(input)
	result := make([]types.RequirementResolutionMode, 0, len(values))
	for _, value := range values {
		result = append(result, types.RequirementResolutionMode(value))
	}
	return result
}

func normalizedStrings(input []string) []string {
	values := make([]string, 0, len(input))
	for _, value := range input {
		value = strings.TrimSpace(value)
		if value != "" {
			values = append(values, value)
		}
	}
	sort.Strings(values)
	return values
}

func componentNodeKey(componentKey string) string {
	return "component:" + strings.TrimSpace(componentKey)
}

func productNodeKey(product string) string {
	return "product:" + strings.TrimSpace(product)
}

func targetRequirementNodeKey(consumerKey, capability string) string {
	return "target:" + strings.TrimPrefix(consumerKey, "component:") + ":" + capability
}

func graphEdgeKey(from, to, capability string) string {
	return from + "->" + to + ":" + capability
}
