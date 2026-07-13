package releasebundles

import (
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/distr-sh/distr/internal/types"
)

const maxReleaseContractItems = 256

var commitPattern = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)

func NormalizeReleaseContract(contract *types.ReleaseContract) {
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
		contract.Components[i].Image = strings.TrimSpace(contract.Components[i].Image)
		contract.Components[i].Platform = strings.ToLower(strings.TrimSpace(contract.Components[i].Platform))
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
		return strings.Compare(a.Name+"\x00"+a.Platform+"\x00"+a.Image, b.Name+"\x00"+b.Platform+"\x00"+b.Image)
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
	normalized := *contract
	normalized.Components = slices.Clone(contract.Components)
	normalized.Compatibility.Requires = slices.Clone(contract.Compatibility.Requires)
	normalized.Compatibility.AffectedComponents = slices.Clone(contract.Compatibility.AffectedComponents)
	normalized.Config.ImmutableObjects = slices.Clone(contract.Config.ImmutableObjects)
	normalized.Changes.Commits = slices.Clone(contract.Changes.Commits)
	NormalizeReleaseContract(&normalized)
	return &normalized
}

func ValidateReleaseContract(
	contract types.ReleaseContract,
	bundleComponents []types.ReleaseBundleComponent,
) ValidationResult {
	result := NewValidResult()
	NormalizeReleaseContract(&contract)
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
		if _, ok := seen[component.Name]; ok {
			result.AddError(field+".name", "unique", "component names must be unique")
		}
		seen[component.Name] = struct{}{}
		if component.Platform != "linux/amd64" && component.Platform != "linux/arm64" {
			result.AddError(field+".platform", "supported", "platform must be linux/amd64 or linux/arm64")
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
		validateOptionalContractString(result, "compatibility.requires.reason", requirement.Reason, 2048)
	}
	if len(contract.Compatibility.AffectedComponents) == 0 {
		result.AddError("releaseContract.compatibility.affectedComponents", "required", "at least one affected component is required")
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
		validateRequiredContractString(result, "config.immutableObjects.versionId", object.VersionID, 1024)
		validateDigest(result, "config.immutableObjects.checksum", object.Checksum)
	}
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
