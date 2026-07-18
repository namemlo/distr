package releasebundles

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/distr-sh/distr/internal/stepredaction"
	"github.com/distr-sh/distr/internal/types"
)

const (
	maxReleaseContractItems                     = 256
	maxComponentReleasePlatforms                = 2
	maxComponentReleaseAllowedModes             = 5
	maxComponentReleaseContractPayloadBytes     = 512 * 1024
	maxComponentReleaseKeyLength                = 256
	maxComponentReleaseVersionLength            = 128
	maxComponentReleaseSourceFieldLength        = 512
	maxComponentReleaseBuildFieldLength         = 512
	maxComponentReleaseReferenceLength          = 2048
	maxComponentReleaseSummaryLength            = 4096
	maxComponentReleaseDescriptionLength        = 4096
	maxComponentReleaseConstraintLength         = 512
	maxComponentReleaseDiscriminatorFieldLength = 256
)

var commitPattern = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)
var componentCommitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var componentKeyPattern = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*$`)
var componentDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var windowsAbsolutePathPattern = regexp.MustCompile(`(?i)(^|[\s"'(])[a-z]:\\`)
var componentSensitiveAssignmentPattern = regexp.MustCompile(
	`(?i)\b(password|passwd|client[\s_-]?secret|api[\s_-]?key|access[\s_-]?token|refresh[\s_-]?token|token)\b\s*[:=]`,
)
var privateKeyMarkerPattern = regexp.MustCompile(`(?i)\bBEGIN(?: [A-Z0-9]+)* PRIVATE KEY\b`)

func normalizeReleaseContractV1(contract *types.ReleaseContract) {
	if contract == nil {
		return
	}
	contract.Schema = strings.TrimSpace(contract.Schema)
	contract.Source.Repository = strings.TrimSpace(contract.Source.Repository)
	contract.Source.Branch = strings.TrimSpace(contract.Source.Branch)
	contract.Source.SourceCommit = strings.TrimSpace(contract.Source.SourceCommit)
	contract.Source.BuiltCommit = strings.TrimSpace(contract.Source.BuiltCommit)
	contract.Build.ExternalID = strings.TrimSpace(contract.Build.ExternalID)
	contract.Build.ExternalURL = strings.TrimSpace(contract.Build.ExternalURL)
	for i := range contract.Components {
		contract.Components[i].Name = strings.TrimSpace(contract.Components[i].Name)
		contract.Components[i].Version = strings.TrimSpace(contract.Components[i].Version)
		contract.Components[i].Image = strings.TrimSpace(contract.Components[i].Image)
		contract.Components[i].Platform = strings.ToLower(strings.TrimSpace(contract.Components[i].Platform))
		contract.Components[i].Contracts = normalizeStringSet(contract.Components[i].Contracts)
	}
	for i := range contract.Compatibility.Requires {
		requirement := &contract.Compatibility.Requires[i]
		requirement.Component = strings.TrimSpace(requirement.Component)
		requirement.Contract = strings.TrimSpace(requirement.Contract)
		requirement.MinimumVersion = strings.TrimSpace(requirement.MinimumVersion)
		requirement.Reason = strings.TrimSpace(requirement.Reason)
	}
	contract.Compatibility.AffectedComponents = normalizeStringSet(contract.Compatibility.AffectedComponents)
	contract.Config.RepositoryCommit = strings.TrimSpace(contract.Config.RepositoryCommit)
	contract.Config.ComposePath = strings.TrimSpace(contract.Config.ComposePath)
	contract.Config.ServiceConfigPath = strings.TrimSpace(contract.Config.ServiceConfigPath)
	contract.Config.ComposeChecksum = strings.TrimSpace(contract.Config.ComposeChecksum)
	contract.Config.ServiceConfigChecksum = strings.TrimSpace(contract.Config.ServiceConfigChecksum)
	for i := range contract.Config.ImmutableObjects {
		object := &contract.Config.ImmutableObjects[i]
		object.URI = strings.TrimSpace(object.URI)
		object.VersionID = strings.TrimSpace(object.VersionID)
		object.Checksum = strings.TrimSpace(object.Checksum)
	}
	contract.Changes.Summary = strings.TrimSpace(contract.Changes.Summary)
	contract.Changes.Commits = normalizeStringSet(contract.Changes.Commits)

	slices.SortFunc(contract.Components, func(a, b types.ReleaseContractComponent) int {
		return strings.Compare(
			a.Name+"\x00"+a.Version+"\x00"+a.Platform+"\x00"+a.Image,
			b.Name+"\x00"+b.Version+"\x00"+b.Platform+"\x00"+b.Image,
		)
	})
	slices.SortFunc(contract.Compatibility.Requires, func(a, b types.ReleaseContractRequirement) int {
		return strings.Compare(
			a.Component+"\x00"+a.Contract+"\x00"+a.MinimumVersion+"\x00"+a.Reason,
			b.Component+"\x00"+b.Contract+"\x00"+b.MinimumVersion+"\x00"+b.Reason,
		)
	})
	slices.SortFunc(contract.Config.ImmutableObjects, func(a, b types.ReleaseContractConfigObject) int {
		return strings.Compare(a.URI+"\x00"+a.VersionID+"\x00"+a.Checksum, b.URI+"\x00"+b.VersionID+"\x00"+b.Checksum)
	})
}

func NormalizedReleaseContract(contract *types.ReleaseContract) *types.ReleaseContract {
	if contract == nil {
		return nil
	}
	if contract.ComponentV2 != nil {
		normalized := normalizedComponentReleaseContractV2(*contract.ComponentV2)
		return &types.ReleaseContract{
			Schema:      types.ReleaseContractSchemaV2,
			ComponentV2: &normalized,
		}
	}
	normalized := *contract
	normalized.Components = slices.Clone(contract.Components)
	for i := range normalized.Components {
		normalized.Components[i].Contracts = slices.Clone(contract.Components[i].Contracts)
	}
	normalized.Compatibility.Requires = slices.Clone(contract.Compatibility.Requires)
	normalized.Compatibility.AffectedComponents = slices.Clone(contract.Compatibility.AffectedComponents)
	normalized.Config.ImmutableObjects = slices.Clone(contract.Config.ImmutableObjects)
	normalized.Changes.Commits = slices.Clone(contract.Changes.Commits)
	normalizeReleaseContractV1(&normalized)
	return &normalized
}

func ParseReleaseContract(data []byte) (schema string, contract any, err error) {
	var discriminator struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &discriminator); err != nil {
		return "", nil, fmt.Errorf("could not decode release contract schema: %w", err)
	}
	if strings.TrimSpace(discriminator.Schema) == "" {
		return "", nil, fmt.Errorf("release contract schema is required")
	}
	switch discriminator.Schema {
	case types.ReleaseContractSchemaV1:
		var legacy types.ReleaseContract
		if err := json.Unmarshal(data, &legacy); err != nil {
			return "", nil, fmt.Errorf("could not decode v1 release contract: %w", err)
		}
		return discriminator.Schema, legacy, nil
	case types.ReleaseContractSchemaV2:
		var envelope types.ReleaseContract
		if err := json.Unmarshal(data, &envelope); err != nil {
			return "", nil, fmt.Errorf("could not decode v2 release contract: %w", err)
		}
		if envelope.ComponentV2 == nil {
			return "", nil, fmt.Errorf("could not decode v2 release contract")
		}
		return discriminator.Schema, *envelope.ComponentV2, nil
	default:
		return "", nil, fmt.Errorf("unsupported release contract schema %q", discriminator.Schema)
	}
}

func NormalizeReleaseContract(contract any) ([]byte, error) {
	switch value := contract.(type) {
	case types.ReleaseContract:
		if value.ComponentV2 != nil {
			normalized := normalizedComponentReleaseContractV2(*value.ComponentV2)
			return json.Marshal(normalized)
		}
		normalized := NormalizedReleaseContract(&value)
		return json.Marshal(normalized)
	case *types.ReleaseContract:
		if value == nil {
			return nil, fmt.Errorf("release contract is required")
		}
		return NormalizeReleaseContract(*value)
	case types.ComponentReleaseContractV2:
		normalized := normalizedComponentReleaseContractV2(value)
		return json.Marshal(normalized)
	case *types.ComponentReleaseContractV2:
		if value == nil {
			return nil, fmt.Errorf("release contract is required")
		}
		return NormalizeReleaseContract(*value)
	default:
		return nil, fmt.Errorf("unsupported release contract type %T", contract)
	}
}

func normalizedComponentReleaseContractV2(contract types.ComponentReleaseContractV2) types.ComponentReleaseContractV2 {
	normalized := contract
	normalized.Schema = strings.TrimSpace(normalized.Schema)
	normalized.ComponentKey = strings.TrimSpace(normalized.ComponentKey)
	normalized.Version = strings.TrimSpace(normalized.Version)
	normalized.Source.Repository = strings.TrimSpace(normalized.Source.Repository)
	normalized.Source.RequestedRef = strings.TrimSpace(normalized.Source.RequestedRef)
	normalized.Source.Commit = strings.TrimSpace(normalized.Source.Commit)
	normalized.Build.ID = strings.TrimSpace(normalized.Build.ID)
	normalized.Build.Builder = strings.TrimSpace(normalized.Build.Builder)
	normalized.Artifacts = cloneNonNilSlice(contract.Artifacts)
	for i := range normalized.Artifacts {
		artifact := &normalized.Artifacts[i]
		artifact.Key = strings.TrimSpace(artifact.Key)
		artifact.Type = strings.TrimSpace(artifact.Type)
		artifact.MediaType = strings.TrimSpace(artifact.MediaType)
		artifact.Digest = strings.TrimSpace(artifact.Digest)
		artifact.Platforms = cloneNonNilSlice(artifact.Platforms)
		for j := range artifact.Platforms {
			artifact.Platforms[j].Platform = strings.ToLower(strings.TrimSpace(artifact.Platforms[j].Platform))
			artifact.Platforms[j].Digest = strings.TrimSpace(artifact.Platforms[j].Digest)
		}
		slices.SortFunc(artifact.Platforms, func(a, b types.ComponentReleasePlatform) int {
			return strings.Compare(a.Platform+"\x00"+a.Digest, b.Platform+"\x00"+b.Digest)
		})
	}
	slices.SortFunc(normalized.Artifacts, func(a, b types.ComponentReleaseArtifact) int {
		return strings.Compare(a.Key+"\x00"+a.Type, b.Key+"\x00"+b.Type)
	})
	normalized.Provides = cloneNonNilSlice(contract.Provides)
	for i := range normalized.Provides {
		normalized.Provides[i].Name = strings.TrimSpace(normalized.Provides[i].Name)
		normalized.Provides[i].Version = strings.TrimSpace(normalized.Provides[i].Version)
	}
	slices.SortFunc(normalized.Provides, func(a, b types.CapabilityDeclaration) int {
		return strings.Compare(a.Name+"\x00"+a.Version, b.Name+"\x00"+b.Version)
	})
	normalized.Requires = cloneNonNilSlice(contract.Requires)
	for i := range normalized.Requires {
		requirement := &normalized.Requires[i]
		requirement.Name = strings.TrimSpace(requirement.Name)
		requirement.Range = strings.TrimSpace(requirement.Range)
		requirement.ResolutionStage = strings.TrimSpace(requirement.ResolutionStage)
		requirement.AllowedModes = normalizeSortedStrings(requirement.AllowedModes)
	}
	slices.SortFunc(normalized.Requires, func(a, b types.CapabilityRequirement) int {
		return strings.Compare(a.Name+"\x00"+a.Range+"\x00"+a.ResolutionStage, b.Name+"\x00"+b.Range+"\x00"+b.ResolutionStage)
	})
	normalized.Migrations = cloneNonNilSlice(contract.Migrations)
	for i := range normalized.Migrations {
		migration := &normalized.Migrations[i]
		migration.Key = strings.TrimSpace(migration.Key)
		migration.Type = strings.TrimSpace(migration.Type)
		migration.Compatibility = strings.TrimSpace(migration.Compatibility)
		migration.FailurePolicy = strings.TrimSpace(migration.FailurePolicy)
		migration.Description = strings.TrimSpace(migration.Description)
	}
	slices.SortFunc(normalized.Migrations, func(a, b types.MigrationDeclaration) int {
		if a.Order != b.Order {
			return a.Order - b.Order
		}
		return strings.Compare(a.Key, b.Key)
	})
	normalized.Changes.Summary = strings.TrimSpace(normalized.Changes.Summary)
	normalized.Changes.Commits = normalizeSortedStrings(contract.Changes.Commits)
	normalized.Evidence.Provenance = normalizeSortedStrings(contract.Evidence.Provenance)
	normalized.Evidence.SBOM = normalizeSortedStrings(contract.Evidence.SBOM)
	normalized.Evidence.Signatures = normalizeSortedStrings(contract.Evidence.Signatures)
	normalized.Evidence.Tests = normalizeSortedStrings(contract.Evidence.Tests)
	return normalized
}

func ValidateReleaseContract(contract any) []ValidationIssue {
	switch value := contract.(type) {
	case types.ReleaseContract:
		if value.ComponentV2 != nil {
			return ValidateComponentReleaseContractV2(*value.ComponentV2)
		}
		return ValidateReleaseContractV1(value, releaseContractV1IntrinsicComponents(value)).Errors
	case *types.ReleaseContract:
		if value == nil {
			return []ValidationIssue{{Field: "releaseContract", Rule: "required", Message: "release contract is required"}}
		}
		return ValidateReleaseContract(*value)
	case types.ComponentReleaseContractV2:
		return ValidateComponentReleaseContractV2(value)
	case *types.ComponentReleaseContractV2:
		if value == nil {
			return []ValidationIssue{{Field: "releaseContract", Rule: "required", Message: "release contract is required"}}
		}
		return ValidateComponentReleaseContractV2(*value)
	default:
		return []ValidationIssue{{
			Field: "releaseContract", Rule: "type", Message: fmt.Sprintf("unsupported release contract type %T", contract),
		}}
	}
}

func releaseContractV1IntrinsicComponents(contract types.ReleaseContract) []types.ReleaseBundleComponent {
	components := make([]types.ReleaseBundleComponent, 0, len(contract.Components))
	for _, component := range contract.Components {
		packageRef, digest, _ := splitImmutableImage(component.Image)
		components = append(components, types.ReleaseBundleComponent{
			Key:        component.Name,
			Type:       types.ReleaseBundleComponentTypeOCIImage,
			Version:    component.Version,
			PackageRef: packageRef,
			Digest:     digest,
		})
	}
	return components
}

func ValidateReleaseContractV1(
	contract types.ReleaseContract,
	bundleComponents []types.ReleaseBundleComponent,
) ValidationResult {
	result := NewValidResult()
	normalizeReleaseContractV1(&contract)
	if contract.Schema != types.ReleaseContractSchemaV1 {
		result.AddError("releaseContract.schema", "supported", "schema must be distr.release-contract/v1")
	}
	validateRequiredContractString(&result, "source.repository", contract.Source.Repository, 512)
	validateRequiredContractString(&result, "source.branch", contract.Source.Branch, 512)
	validateCommit(&result, "source.sourceCommit", contract.Source.SourceCommit)
	validateCommit(&result, "source.builtCommit", contract.Source.BuiltCommit)
	validateRequiredContractString(&result, "build.externalId", contract.Build.ExternalID, 512)
	validateHTTPURL(&result, "build.externalUrl", contract.Build.ExternalURL)
	validateReleaseContractComponents(&result, contract, bundleComponents)
	validateReleaseContractCompatibility(&result, contract)
	validateReleaseContractConfig(&result, contract.Config)
	validateRequiredContractString(&result, "changes.summary", contract.Changes.Summary, 4096)
	if len(contract.Changes.Commits) > maxReleaseContractItems {
		result.AddError("releaseContract.changes.commits", "limit", "too many commit references")
	}
	for _, commit := range contract.Changes.Commits {
		validateRequiredContractString(&result, "changes.commits", commit, 1024)
		if strings.ContainsAny(commit, "\r\n") {
			result.AddError("releaseContract.changes.commits", "safe", "commit references must be single-line values")
			break
		}
	}
	result.Valid = len(result.Errors) == 0
	return result
}

func ValidateComponentReleaseContractV2(contract types.ComponentReleaseContractV2) []ValidationIssue {
	if issues := validateComponentReleaseContractV2Bounds(contract); len(issues) > 0 {
		return issues
	}
	contract = normalizedComponentReleaseContractV2(contract)
	issues := make([]ValidationIssue, 0)
	add := func(field, rule, message string) {
		issues = append(issues, ValidationIssue{Field: field, Rule: rule, Message: message})
	}
	if contract.Schema != types.ReleaseContractSchemaV2 {
		add("schema", "supported", "schema must be distr.component-release/v2")
	}
	if !componentKeyPattern.MatchString(contract.ComponentKey) {
		add("componentKey", "key", "component key must be a lowercase stable key")
	}
	if _, err := semver.StrictNewVersion(contract.Version); err != nil {
		add("version", "semver", "component version must be strict semantic version")
	}
	if contract.Source.Repository == "" {
		add("source.repository", "required", "source repository is required")
	}
	if contract.Source.RequestedRef == "" {
		add("source.requestedRef", "required", "requested source ref is required")
	}
	if !componentCommitPattern.MatchString(contract.Source.Commit) {
		add("source.commit", "commit", "source commit must be a lowercase 40-character commit")
	}
	if contract.Build.ID == "" {
		add("build.id", "required", "build id is required")
	}
	if contract.Build.Builder == "" {
		add("build.builder", "required", "build builder is required")
	}
	issues = append(issues, ValidateArtifactIdentity(contract)...)
	issues = append(issues, validateComponentCapabilities(contract)...)
	issues = append(issues, validateComponentMigrations(contract)...)
	if contract.Changes.Summary == "" {
		add("changes.summary", "required", "change summary is required")
	}
	seenCommits := map[string]struct{}{}
	for _, commit := range contract.Changes.Commits {
		if !componentCommitPattern.MatchString(commit) {
			add("changes.commits", "commit", "change commits must be lowercase 40-character commits")
			break
		}
		if _, ok := seenCommits[commit]; ok {
			add("changes.commits", "unique", "change commits must be unique")
			break
		}
		seenCommits[commit] = struct{}{}
	}
	issues = append(issues, validateComponentEvidence(contract)...)
	issues = append(issues, ValidateTargetNeutralContract(contract)...)
	return issues
}

func ValidateArtifactIdentity(contract types.ComponentReleaseContractV2) []ValidationIssue {
	issues := make([]ValidationIssue, 0)
	add := func(field, rule, message string) {
		issues = append(issues, ValidationIssue{Field: field, Rule: rule, Message: message})
	}
	if len(contract.Artifacts) == 0 {
		add("artifacts", "required", "at least one immutable artifact is required")
		return issues
	}
	artifactKeys := map[string]struct{}{}
	for _, artifact := range contract.Artifacts {
		field := "artifacts." + artifact.Key
		if !componentKeyPattern.MatchString(artifact.Key) {
			add(field+".key", "key", "artifact key must be a lowercase stable key")
		}
		if _, ok := artifactKeys[artifact.Key]; ok {
			add(field+".key", "unique", "artifact keys must be unique")
		}
		artifactKeys[artifact.Key] = struct{}{}
		if artifact.Type != "oci-image" && artifact.Type != "oci-artifact" && artifact.Type != "helm-chart" {
			add(field+".type", "supported", "artifact type is not supported")
		}
		if !isSupportedComponentArtifactMediaType(artifact.MediaType) {
			add(field+".mediaType", "supported", "artifact media type is not supported")
		} else if !componentArtifactMediaTypeMatchesType(artifact.Type, artifact.MediaType) {
			add(field+".mediaType", "matchesType", "artifact media type must match the artifact type")
		}
		if !componentDigestPattern.MatchString(artifact.Digest) {
			add(field+".digest", "sha256", "artifact digest must be a lowercase sha256 digest")
		}
		if len(artifact.Platforms) == 0 {
			add(field+".platforms", "required", "artifact must include at least one platform digest")
		}
		platformDigests := map[string]string{}
		for _, platform := range artifact.Platforms {
			platformField := field + ".platforms." + platform.Platform
			if platform.Platform != "linux/amd64" && platform.Platform != "linux/arm64" {
				add(platformField, "supported", "platform must be linux/amd64 or linux/arm64")
			}
			if !componentDigestPattern.MatchString(platform.Digest) {
				add(platformField+".digest", "sha256", "platform digest must be a lowercase sha256 digest")
			}
			if digest, ok := platformDigests[platform.Platform]; ok {
				if digest == platform.Digest {
					add(platformField, "unique", "platform entries must be unique")
				} else {
					add(platformField, "conflict", "component version and platform cannot resolve to different digests")
				}
			} else {
				platformDigests[platform.Platform] = platform.Digest
			}
		}
	}
	return issues
}

func validateComponentCapabilities(contract types.ComponentReleaseContractV2) []ValidationIssue {
	issues := make([]ValidationIssue, 0)
	add := func(field, rule, message string) {
		issues = append(issues, ValidationIssue{Field: field, Rule: rule, Message: message})
	}
	provided := map[string]struct{}{}
	for _, capability := range contract.Provides {
		field := "provides." + capability.Name
		if !componentKeyPattern.MatchString(capability.Name) {
			add(field+".name", "key", "capability name must be a lowercase stable key")
		}
		if _, ok := provided[capability.Name]; ok {
			add(field+".name", "unique", "provided capability names must be unique")
		}
		provided[capability.Name] = struct{}{}
		if _, err := semver.StrictNewVersion(capability.Version); err != nil {
			add(field+".version", "semver", "provided capability version must be strict semantic version")
		}
	}
	required := map[string]struct{}{}
	for _, capability := range contract.Requires {
		field := "requires." + capability.Name
		if !componentKeyPattern.MatchString(capability.Name) {
			add(field+".name", "key", "capability name must be a lowercase stable key")
		}
		if _, ok := required[capability.Name]; ok {
			add(field+".name", "unique", "required capability names must be unique")
		}
		required[capability.Name] = struct{}{}
		if capability.Range == "" {
			add(field+".range", "required", "required capability range must not be empty")
		} else if _, err := semver.NewConstraint(capability.Range); err != nil {
			add(field+".range", "semverRange", "required capability range must be valid")
		}
		if capability.ResolutionStage != "product" && capability.ResolutionStage != "target" {
			add(field+".resolutionStage", "supported", "resolution stage must be product or target")
		}
		if capability.ResolutionStage == "product" && len(capability.AllowedModes) > 0 {
			add(field+".allowedModes", "forbidden", "product requirements must not declare target resolution modes")
		}
		if capability.ResolutionStage == "target" && len(capability.AllowedModes) == 0 {
			add(field+".allowedModes", "required", "target requirements must declare allowed resolution modes")
		}
		modes := map[string]struct{}{}
		for _, mode := range capability.AllowedModes {
			if _, ok := modes[mode]; ok {
				add(field+".allowedModes", "unique", "capability resolution modes must be unique")
			}
			modes[mode] = struct{}{}
			switch mode {
			case "included", "pinned-existing", "shared-provider", "approved-external", "feature-disabled":
			default:
				add(field+".allowedModes", "supported", "capability resolution mode is not supported")
			}
		}
	}
	return issues
}

func validateComponentEvidence(contract types.ComponentReleaseContractV2) []ValidationIssue {
	issues := make([]ValidationIssue, 0)
	for _, group := range []struct {
		field      string
		references []string
	}{
		{field: "evidence.provenance", references: contract.Evidence.Provenance},
		{field: "evidence.sbom", references: contract.Evidence.SBOM},
		{field: "evidence.signatures", references: contract.Evidence.Signatures},
		{field: "evidence.tests", references: contract.Evidence.Tests},
	} {
		seen := map[string]struct{}{}
		for _, reference := range group.references {
			if reference == "" {
				issues = append(issues, ValidationIssue{
					Field: group.field, Rule: "required", Message: "evidence references must not be empty",
				})
				continue
			}
			if _, ok := seen[reference]; ok {
				issues = append(issues, ValidationIssue{
					Field: group.field, Rule: "unique", Message: "evidence references must be unique",
				})
			}
			seen[reference] = struct{}{}
		}
	}
	return issues
}

func validateComponentMigrations(contract types.ComponentReleaseContractV2) []ValidationIssue {
	issues := make([]ValidationIssue, 0)
	add := func(field, rule, message string) {
		issues = append(issues, ValidationIssue{Field: field, Rule: rule, Message: message})
	}
	keys := map[string]struct{}{}
	orders := map[int]struct{}{}
	for _, migration := range contract.Migrations {
		field := "migrations." + migration.Key
		if !componentKeyPattern.MatchString(migration.Key) {
			add(field+".key", "key", "migration key must be a lowercase stable key")
		}
		if _, ok := keys[migration.Key]; ok {
			add(field+".key", "unique", "migration keys must be unique")
		}
		keys[migration.Key] = struct{}{}
		switch migration.Type {
		case "database", "data", "runtime":
		default:
			add(field+".type", "supported", "migration type is not supported")
		}
		if migration.Order <= 0 {
			add(field+".order", "positive", "migration order must be positive")
		}
		if _, ok := orders[migration.Order]; ok {
			add(field+".order", "unique", "migration order must be unique")
		}
		orders[migration.Order] = struct{}{}
		switch migration.Compatibility {
		case "backward-compatible", "forward-compatible", "breaking":
		default:
			add(field+".compatibility", "supported", "migration compatibility is not supported")
		}
		switch migration.FailurePolicy {
		case "stop", "retry", "forward-fix":
		default:
			add(field+".failurePolicy", "supported", "migration failure policy is not supported")
		}
		if migration.Description == "" {
			add(field+".description", "required", "migration description is required")
		}
	}
	return issues
}

func ValidateTargetNeutralContract(contract types.ComponentReleaseContractV2) []ValidationIssue {
	issues := make([]ValidationIssue, 0)
	addIfUnsafe := func(field, value string, allowReference bool) {
		if containsTargetSpecificValue(value, allowReference) {
			issues = append(issues, ValidationIssue{
				Field: field, Rule: "targetNeutral", Message: "component release values must not contain target paths, URLs, or secrets",
			})
		}
	}
	addIfUnsafe("source.repository", contract.Source.Repository, false)
	addIfUnsafe("source.requestedRef", contract.Source.RequestedRef, false)
	addIfUnsafe("build.id", contract.Build.ID, false)
	addIfUnsafe("build.builder", contract.Build.Builder, false)
	addIfUnsafe("changes.summary", contract.Changes.Summary, false)
	for _, migration := range contract.Migrations {
		addIfUnsafe("migrations."+migration.Key+".description", migration.Description, false)
	}
	for _, reference := range contract.Evidence.Provenance {
		addIfUnsafe("evidence.provenance", reference, true)
	}
	for _, reference := range contract.Evidence.SBOM {
		addIfUnsafe("evidence.sbom", reference, true)
	}
	for _, reference := range contract.Evidence.Signatures {
		addIfUnsafe("evidence.signatures", reference, true)
	}
	for _, reference := range contract.Evidence.Tests {
		addIfUnsafe("evidence.tests", reference, true)
	}
	return issues
}

func containsTargetSpecificValue(value string, allowReference bool) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return false
	}
	if _, changed := stepredaction.RedactString(value); changed ||
		componentSensitiveAssignmentPattern.MatchString(value) ||
		privateKeyMarkerPattern.MatchString(value) {
		return true
	}
	for _, marker := range []string{
		"authorization:", "bearer ", "password=", "secret=", "token=", "api_key=", "access_token=",
		"clientsecret", "privatekey",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	if windowsAbsolutePathPattern.MatchString(value) || strings.HasPrefix(normalized, `\\`) {
		return true
	}
	if !allowReference && (strings.Contains(normalized, "://") || strings.HasPrefix(normalized, "/")) {
		return true
	}
	if parsed, err := url.Parse(strings.TrimSpace(value)); err == nil && parsed.Scheme != "" && parsed.User != nil {
		return true
	}
	if allowReference && strings.ContainsAny(value, "?#") {
		return true
	}
	return strings.ContainsAny(value, "\r\n")
}

func validateComponentReleaseContractV2Bounds(
	contract types.ComponentReleaseContractV2,
) []ValidationIssue {
	issues := make([]ValidationIssue, 0)
	addLimit := func(field, message string) {
		issues = append(issues, ValidationIssue{Field: field, Rule: "limit", Message: message})
	}
	checkString := func(field, value string, limit int) {
		if len(value) > limit {
			addLimit(field, field+" is too long")
		}
	}
	checkCollection := func(field string, length, limit int) {
		if length > limit {
			addLimit(field, field+" contains too many entries")
		}
	}

	checkString("schema", contract.Schema, maxComponentReleaseDiscriminatorFieldLength)
	checkString("componentKey", contract.ComponentKey, maxComponentReleaseKeyLength)
	checkString("version", contract.Version, maxComponentReleaseVersionLength)
	checkString("source.repository", contract.Source.Repository, maxComponentReleaseSourceFieldLength)
	checkString("source.requestedRef", contract.Source.RequestedRef, maxComponentReleaseSourceFieldLength)
	checkString("source.commit", contract.Source.Commit, maxComponentReleaseDiscriminatorFieldLength)
	checkString("build.id", contract.Build.ID, maxComponentReleaseBuildFieldLength)
	checkString("build.builder", contract.Build.Builder, maxComponentReleaseBuildFieldLength)

	checkCollection("artifacts", len(contract.Artifacts), maxReleaseContractItems)
	for i := range min(len(contract.Artifacts), maxReleaseContractItems) {
		artifact := contract.Artifacts[i]
		field := boundedComponentReleaseItemField("artifacts", artifact.Key, i)
		checkString(field+".key", artifact.Key, maxComponentReleaseKeyLength)
		checkString(field+".type", artifact.Type, maxComponentReleaseDiscriminatorFieldLength)
		checkString(field+".mediaType", artifact.MediaType, maxComponentReleaseDiscriminatorFieldLength)
		checkString(field+".digest", artifact.Digest, maxComponentReleaseDiscriminatorFieldLength)
		checkCollection(field+".platforms", len(artifact.Platforms), maxComponentReleasePlatforms)
		for j := range min(len(artifact.Platforms), maxComponentReleasePlatforms) {
			platform := artifact.Platforms[j]
			platformField := boundedComponentReleaseItemField(field+".platforms", platform.Platform, j)
			checkString(
				platformField+".platform",
				platform.Platform,
				maxComponentReleaseDiscriminatorFieldLength,
			)
			checkString(
				platformField+".digest",
				platform.Digest,
				maxComponentReleaseDiscriminatorFieldLength,
			)
		}
	}

	checkCollection("provides", len(contract.Provides), maxReleaseContractItems)
	for i := range min(len(contract.Provides), maxReleaseContractItems) {
		capability := contract.Provides[i]
		field := boundedComponentReleaseItemField("provides", capability.Name, i)
		checkString(field+".name", capability.Name, maxComponentReleaseKeyLength)
		checkString(field+".version", capability.Version, maxComponentReleaseVersionLength)
	}

	checkCollection("requires", len(contract.Requires), maxReleaseContractItems)
	for i := range min(len(contract.Requires), maxReleaseContractItems) {
		requirement := contract.Requires[i]
		field := boundedComponentReleaseItemField("requires", requirement.Name, i)
		checkString(field+".name", requirement.Name, maxComponentReleaseKeyLength)
		checkString(field+".range", requirement.Range, maxComponentReleaseConstraintLength)
		checkString(
			field+".resolutionStage",
			requirement.ResolutionStage,
			maxComponentReleaseDiscriminatorFieldLength,
		)
		checkCollection(field+".allowedModes", len(requirement.AllowedModes), maxComponentReleaseAllowedModes)
		for j := range min(len(requirement.AllowedModes), maxComponentReleaseAllowedModes) {
			checkString(
				fmt.Sprintf("%s.allowedModes[%d]", field, j),
				requirement.AllowedModes[j],
				maxComponentReleaseDiscriminatorFieldLength,
			)
		}
	}

	checkCollection("migrations", len(contract.Migrations), maxReleaseContractItems)
	for i := range min(len(contract.Migrations), maxReleaseContractItems) {
		migration := contract.Migrations[i]
		field := boundedComponentReleaseItemField("migrations", migration.Key, i)
		checkString(field+".key", migration.Key, maxComponentReleaseKeyLength)
		checkString(field+".type", migration.Type, maxComponentReleaseDiscriminatorFieldLength)
		checkString(
			field+".compatibility",
			migration.Compatibility,
			maxComponentReleaseDiscriminatorFieldLength,
		)
		checkString(
			field+".failurePolicy",
			migration.FailurePolicy,
			maxComponentReleaseDiscriminatorFieldLength,
		)
		checkString(field+".description", migration.Description, maxComponentReleaseDescriptionLength)
	}

	checkString("changes.summary", contract.Changes.Summary, maxComponentReleaseSummaryLength)
	checkCollection("changes.commits", len(contract.Changes.Commits), maxReleaseContractItems)
	for i := range min(len(contract.Changes.Commits), maxReleaseContractItems) {
		checkString(
			fmt.Sprintf("changes.commits[%d]", i),
			contract.Changes.Commits[i],
			maxComponentReleaseDiscriminatorFieldLength,
		)
	}

	for _, group := range []struct {
		field      string
		references []string
	}{
		{field: "evidence.provenance", references: contract.Evidence.Provenance},
		{field: "evidence.sbom", references: contract.Evidence.SBOM},
		{field: "evidence.signatures", references: contract.Evidence.Signatures},
		{field: "evidence.tests", references: contract.Evidence.Tests},
	} {
		checkCollection(group.field, len(group.references), maxReleaseContractItems)
		for i := range min(len(group.references), maxReleaseContractItems) {
			checkString(group.field, group.references[i], maxComponentReleaseReferenceLength)
		}
	}

	if len(issues) > 0 {
		return issues
	}
	payload, err := NormalizeReleaseContract(contract)
	if err == nil && len(payload) > maxComponentReleaseContractPayloadBytes {
		addLimit("payload", "component release contract payload is too large")
	}
	return issues
}

func boundedComponentReleaseItemField(prefix, key string, index int) string {
	key = strings.TrimSpace(key)
	if key == "" || len(key) > maxComponentReleaseKeyLength {
		return fmt.Sprintf("%s[%d]", prefix, index)
	}
	return prefix + "." + key
}

func validateReleaseContractComponents(
	result *ValidationResult,
	contract types.ReleaseContract,
	bundleComponents []types.ReleaseBundleComponent,
) {
	if len(contract.Components) == 0 {
		result.AddError("releaseContract.components", "required", "at least one component is required")
		return
	}
	if len(contract.Components) > maxReleaseContractItems {
		result.AddError("releaseContract.components", "limit", "too many components")
	}
	bundleByKey := make(map[string]types.ReleaseBundleComponent, len(bundleComponents))
	for _, component := range bundleComponents {
		bundleByKey[strings.TrimSpace(component.Key)] = component
	}
	seen := map[string]struct{}{}
	for _, component := range contract.Components {
		field := "releaseContract.components." + component.Name
		validateRequiredContractString(result, "components.name", component.Name, 256)
		validateRequiredContractString(result, "components.version", component.Version, 128)
		if _, err := semver.StrictNewVersion(component.Version); err != nil {
			result.AddError(field+".version", "semver", "component version must be strict semantic version")
		}
		if _, ok := seen[component.Name]; ok {
			result.AddError(field+".name", "unique", "component names must be unique")
		}
		seen[component.Name] = struct{}{}
		if component.Platform != "linux/amd64" && component.Platform != "linux/arm64" {
			result.AddError(field+".platform", "supported", "platform must be linux/amd64 or linux/arm64")
		}
		if len(component.Contracts) > maxReleaseContractItems {
			result.AddError(field+".contracts", "limit", "too many provided contracts")
		}
		for _, providedContract := range component.Contracts {
			validateRequiredContractString(result, "components.contracts", providedContract, 512)
		}
		packageRef, digest, ok := splitImmutableImage(component.Image)
		if !ok {
			result.AddError(field+".image", "immutable", "component image must use an immutable image digest")
			continue
		}
		bundleComponent, ok := bundleByKey[component.Name]
		if !ok || (bundleComponent.Type != types.ReleaseBundleComponentTypeOCIImage &&
			bundleComponent.Type != types.ReleaseBundleComponentTypeOCIArtifact) ||
			strings.TrimSpace(bundleComponent.PackageRef) != packageRef ||
			!strings.EqualFold(strings.TrimSpace(bundleComponent.Digest), digest) {
			result.AddError(field+".image", "matchesBundle", "component image must match the release bundle OCI component")
		}
		if ok && strings.TrimSpace(bundleComponent.Version) != component.Version {
			result.AddError(field+".version", "matchesBundle", "component version must match the release bundle component")
		}
	}
}

func validateReleaseContractCompatibility(result *ValidationResult, contract types.ReleaseContract) {
	if len(contract.Compatibility.Requires) > maxReleaseContractItems {
		result.AddError("releaseContract.compatibility.requires", "limit", "too many compatibility requirements")
	}
	for _, requirement := range contract.Compatibility.Requires {
		field := "releaseContract.compatibility.requires." + requirement.Component
		validateRequiredContractString(result, "compatibility.requires.component", requirement.Component, 256)
		if requirement.Contract == "" && requirement.MinimumVersion == "" {
			result.AddError(field, "required", "a contract or minimumVersion is required")
		}
		validateOptionalContractString(result, "compatibility.requires.contract", requirement.Contract, 512)
		validateOptionalContractString(result, "compatibility.requires.minimumVersion", requirement.MinimumVersion, 128)
		if requirement.MinimumVersion != "" {
			if _, err := semver.StrictNewVersion(requirement.MinimumVersion); err != nil {
				result.AddError(field+".minimumVersion", "semver", "minimumVersion must be strict semantic version")
			}
		}
		validateOptionalContractString(result, "compatibility.requires.reason", requirement.Reason, 2048)
	}
	if len(contract.Compatibility.AffectedComponents) == 0 {
		result.AddError(
			"releaseContract.compatibility.affectedComponents",
			"required",
			"at least one affected component is required",
		)
	}
	componentNames := map[string]struct{}{}
	for _, component := range contract.Components {
		componentNames[component.Name] = struct{}{}
	}
	for _, affected := range contract.Compatibility.AffectedComponents {
		if _, ok := componentNames[affected]; !ok {
			result.AddError(
				"releaseContract.compatibility.affectedComponents",
				"component",
				"affected components must reference release contract components",
			)
		}
	}
}

func validateReleaseContractConfig(result *ValidationResult, config types.ReleaseContractConfig) {
	validateCommit(result, "config.repositoryCommit", config.RepositoryCommit)
	validateSafeRelativePath(result, "config.composePath", config.ComposePath)
	validateSafeRelativePath(result, "config.serviceConfigPath", config.ServiceConfigPath)
	validateDigest(result, "config.composeChecksum", config.ComposeChecksum)
	validateDigest(result, "config.serviceConfigChecksum", config.ServiceConfigChecksum)
	if len(config.ImmutableObjects) > maxReleaseContractItems {
		result.AddError("releaseContract.config.immutableObjects", "limit", "too many immutable config objects")
	}
	for _, object := range config.ImmutableObjects {
		validateRequiredContractString(result, "config.immutableObjects.uri", object.URI, 2048)
		validateDigest(result, "config.immutableObjects.checksum", object.Checksum)
		if object.VersionID != "" {
			validateRequiredContractString(result, "config.immutableObjects.versionId", object.VersionID, 1024)
		} else if !IsContentAddressedConfigObject(object) {
			result.AddError(
				"releaseContract.config.immutableObjects",
				"immutable",
				"config objects require a versionId or matching content-addressed S3 URI",
			)
		}
	}
}

func IsContentAddressedConfigObject(object types.ReleaseContractConfigObject) bool {
	if strings.TrimSpace(object.VersionID) != "" || !IsSHA256Digest(strings.TrimSpace(object.Checksum)) {
		return false
	}
	parsed, err := url.Parse(strings.TrimSpace(object.URI))
	if err != nil || !strings.EqualFold(parsed.Scheme, "s3") || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" ||
		strings.Contains(parsed.Path, "\\") || path.Clean(parsed.Path) != parsed.Path {
		return false
	}
	segments := strings.Split(strings.TrimPrefix(parsed.Path, "/"), "/")
	if len(segments) < 4 || segments[0] != "_immutable" || segments[1] != "sha256" ||
		segments[len(segments)-1] == "" || !IsSHA256Digest("sha256:"+segments[2]) {
		return false
	}
	return strings.EqualFold("sha256:"+segments[2], strings.TrimSpace(object.Checksum))
}

func validateRequiredContractString(result *ValidationResult, field, value string, limit int) {
	fullField := "releaseContract." + field
	if value == "" {
		result.AddError(fullField, "required", field+" is required")
	} else if len(value) > limit {
		result.AddError(fullField, "limit", field+" is too long")
	} else if containsUnsafeContractValue(value) {
		result.AddError(fullField, "safe", field+" must not contain secrets or authorization data")
	}
}

func validateOptionalContractString(result *ValidationResult, field, value string, limit int) {
	if value == "" {
		return
	}
	validateRequiredContractString(result, field, value, limit)
}

func validateCommit(result *ValidationResult, field, value string) {
	if !commitPattern.MatchString(value) {
		result.AddError("releaseContract."+field, "commit", field+" must be a 7 to 64 character hexadecimal commit")
	}
}

func validateDigest(result *ValidationResult, field, value string) {
	if !IsSHA256Digest(value) {
		result.AddError("releaseContract."+field, "sha256", field+" must be a sha256 digest")
	}
}

func validateHTTPURL(result *ValidationResult, field, value string) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		result.AddError("releaseContract."+field, "url", field+" must be an absolute HTTP(S) URL")
		return
	}
	validateRequiredContractString(result, field, value, 2048)
}

func validateSafeRelativePath(result *ValidationResult, field, value string) {
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, "\\") ||
		path.Clean(value) != value || value == "." || strings.HasPrefix(value, "../") {
		result.AddError("releaseContract."+field, "safePath", field+" must be a normalized relative path")
		return
	}
	validateRequiredContractString(result, field, value, 2048)
}

func splitImmutableImage(image string) (string, string, bool) {
	packageRef, digest, ok := strings.Cut(strings.TrimSpace(image), "@")
	return packageRef, digest, ok && packageRef != "" && IsSHA256Digest(digest) && !strings.Contains(packageRef, "@")
}

func containsUnsafeContractValue(value string) bool {
	if strings.ContainsAny(value, "\r\n") {
		return true
	}
	normalized := strings.ToLower(value)
	for _, marker := range []string{
		"authorization:", "bearer ", "accesstoken ", "access_token=", "api_key=", "password=", "secret=", "token=",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func normalizeStringSet(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}

func normalizeSortedStrings(values []string) []string {
	result := cloneNonNilSlice(values)
	for i := range result {
		result[i] = strings.TrimSpace(result[i])
	}
	slices.Sort(result)
	return result
}

func cloneNonNilSlice[S ~[]E, E any](values S) S {
	result := make(S, len(values))
	copy(result, values)
	return result
}

func isSupportedComponentArtifactMediaType(mediaType string) bool {
	switch mediaType {
	case "application/vnd.oci.image.index.v1+json",
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.oci.artifact.manifest.v1+json",
		"application/vnd.cncf.helm.chart.content.v1.tar+gzip":
		return true
	default:
		return false
	}
}

func componentArtifactMediaTypeMatchesType(artifactType, mediaType string) bool {
	switch artifactType {
	case "oci-image":
		return mediaType == "application/vnd.oci.image.index.v1+json" ||
			mediaType == "application/vnd.oci.image.manifest.v1+json"
	case "oci-artifact":
		return mediaType == "application/vnd.oci.artifact.manifest.v1+json"
	case "helm-chart":
		return mediaType == "application/vnd.cncf.helm.chart.content.v1.tar+gzip"
	default:
		return false
	}
}
