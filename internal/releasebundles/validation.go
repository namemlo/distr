package releasebundles

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/distr-sh/distr/internal/types"
)

const (
	maxComponentReleaseProjectionNameLength        = 256
	maxComponentReleasePackageReferenceLength      = 2048
	maxComponentReleaseProjectionStringFieldLength = 256
)

type ValidationIssue struct {
	Field   string
	Rule    string
	Message string
}

type ValidationResult struct {
	Valid    bool
	Errors   []ValidationIssue
	Warnings []ValidationIssue
}

func (r *ValidationResult) AddError(field, rule, message string) {
	r.Errors = append(r.Errors, ValidationIssue{
		Field:   field,
		Rule:    rule,
		Message: message,
	})
	r.Valid = false
}

func (r *ValidationResult) Merge(prefix string, other ValidationResult) {
	for _, issue := range other.Errors {
		issue.Field = joinIssueField(prefix, issue.Field)
		r.Errors = append(r.Errors, issue)
	}
	for _, issue := range other.Warnings {
		issue.Field = joinIssueField(prefix, issue.Field)
		r.Warnings = append(r.Warnings, issue)
	}
	r.Valid = len(r.Errors) == 0
}

func NewValidResult() ValidationResult {
	return ValidationResult{Valid: true}
}

func ValidateBundleContent(bundle types.ReleaseBundle) ValidationResult {
	result := NewValidResult()
	if len(bundle.Components) == 0 {
		result.AddError("components", "required", "at least one component is required")
	}
	isComponentRelease := bundle.ReleaseContract != nil && bundle.ReleaseContract.ComponentV2 != nil
	if isComponentRelease && !validateComponentReleaseProjectionBounds(&result, bundle.Components) {
		return result
	}
	if bundle.ReleaseContract != nil {
		if bundle.ReleaseContract.ComponentV2 != nil {
			contractIssues := ValidateComponentReleaseContractV2(*bundle.ReleaseContract.ComponentV2)
			for _, issue := range contractIssues {
				result.AddError("releaseContract."+issue.Field, issue.Rule, issue.Message)
			}
			if hasValidationRule(contractIssues, "limit") {
				return result
			}
			for _, issue := range BindComponentReleaseSourceProjection(&bundle) {
				result.AddError(issue.Field, issue.Rule, issue.Message)
			}
			validateComponentReleaseBundleMatch(&result, bundle, *bundle.ReleaseContract.ComponentV2)
		} else {
			result.Merge("", ValidateReleaseContractV1(*bundle.ReleaseContract, bundle.Components))
		}
	}
	if bundle.CanonicalChecksum != "" || len(bundle.CanonicalPayload) > 0 {
		_, checksum, err := Canonicalize(bundle)
		if err != nil {
			result.AddError("canonicalPayload", "canonicalize", "canonical payload could not be computed")
		} else if checksum != bundle.CanonicalChecksum {
			result.AddError("canonicalChecksum", "sha256", "canonical payload does not match checksum")
		}
	}

	seenKeys := map[string]struct{}{}
	for i, component := range bundle.Components {
		key := strings.TrimSpace(component.Key)
		if key == "" {
			result.AddError("components", "key", "component key is required")
			continue
		}
		field := "components." + key
		if isComponentRelease {
			field = componentReleaseProjectionField(key, i)
		}
		if _, ok := seenKeys[key]; ok {
			result.AddError(field+".key", "unique", "component key must be unique")
		}
		seenKeys[key] = struct{}{}
		validateComponentContent(&result, field, component)
	}
	result.Valid = len(result.Errors) == 0
	return result
}

func validateComponentReleaseBundleMatch(
	result *ValidationResult,
	bundle types.ReleaseBundle,
	contract types.ComponentReleaseContractV2,
) {
	contract = normalizedComponentReleaseContractV2(contract)
	if bundle.Kind != "" && bundle.Kind != types.ReleaseBundleKindComponent {
		result.AddError("kind", "matchesContract", "component release contract requires component bundle kind")
	}
	if bundle.ReleaseContractSchema != "" && bundle.ReleaseContractSchema != types.ReleaseContractSchemaV2 {
		result.AddError(
			"releaseContractSchema",
			"matchesContract",
			"component release contract requires distr.component-release/v2 metadata",
		)
	}
	for i, component := range bundle.Components {
		validateComponentReleaseProjection(
			result,
			componentReleaseProjectionField(component.Key, i),
			component,
		)
	}
	components := make(map[string]types.ReleaseBundleComponent, len(bundle.Components))
	for _, component := range bundle.Components {
		components[strings.TrimSpace(component.Key)] = component
	}
	artifacts := make(map[string]struct{}, len(contract.Artifacts))
	for _, artifact := range contract.Artifacts {
		artifacts[artifact.Key] = struct{}{}
		component, ok := components[artifact.Key]
		field := "releaseContract.artifacts." + artifact.Key
		if !ok {
			result.AddError(field, "matchesBundle", "component release artifact must match a release bundle component")
			continue
		}
		if component.Type != componentTypeForArtifact(artifact.Type) {
			result.AddError(
				field+".type",
				"matchesBundle",
				"component release artifact type must exactly match the release bundle component type",
			)
		}
		if component.Version != contract.Version {
			result.AddError(field+".version", "matchesBundle", "component release artifact version must match the contract")
		}
		if component.Digest != artifact.Digest {
			result.AddError(field+".digest", "matchesBundle", "component release artifact digest must match the bundle")
		}
	}
	for _, component := range bundle.Components {
		key := strings.TrimSpace(component.Key)
		if _, ok := artifacts[key]; !ok {
			result.AddError(
				"components."+key,
				"matchesContract",
				"release bundle component must match exactly one component release artifact",
			)
		}
	}
}

func validateComponentReleaseProjectionBounds(
	result *ValidationResult,
	components []types.ReleaseBundleComponent,
) bool {
	if len(components) > MaxComponentReleaseProjectionItems {
		result.AddError("components", "limit", "component release contains too many outer components")
		return false
	}
	valid := true
	check := func(field, value string, limit int) {
		if len(value) > limit {
			result.AddError(field, "limit", field+" is too long")
			valid = false
		}
	}
	for i, component := range components {
		field := componentReleaseProjectionField(component.Key, i)
		check(field+".key", component.Key, maxComponentReleaseProjectionStringFieldLength)
		check(field+".name", component.Name, maxComponentReleaseProjectionNameLength)
		check(field+".type", string(component.Type), maxComponentReleaseProjectionStringFieldLength)
		check(field+".version", component.Version, maxComponentReleaseVersionLength)
		check(field+".packageRef", component.PackageRef, maxComponentReleasePackageReferenceLength)
		check(field+".digest", component.Digest, maxComponentReleaseProjectionStringFieldLength)
		check(field+".checksum", component.Checksum, maxComponentReleaseProjectionStringFieldLength)
	}
	return valid
}

func validateComponentReleaseProjection(
	result *ValidationResult,
	field string,
	component types.ReleaseBundleComponent,
) {
	for _, candidate := range []struct {
		field string
		value string
	}{
		{field: field + ".key", value: component.Key},
		{field: field + ".name", value: component.Name},
		{field: field + ".type", value: string(component.Type)},
		{field: field + ".version", value: component.Version},
		{field: field + ".packageRef", value: component.PackageRef},
		{field: field + ".digest", value: component.Digest},
		{field: field + ".checksum", value: component.Checksum},
	} {
		if candidate.value != strings.TrimSpace(candidate.value) {
			result.AddError(
				candidate.field,
				"normalized",
				"component release projection values must not have surrounding whitespace",
			)
		}
		allowReference := candidate.field == field+".packageRef"
		unsafe := candidate.value != "" && containsTargetSpecificValue(candidate.value, allowReference)
		if candidate.field == field+".packageRef" && packageReferenceContainsUserInfo(candidate.value) {
			unsafe = true
		}
		if unsafe {
			result.AddError(
				candidate.field,
				"targetNeutral",
				"component release projection values must not contain target paths, URLs, or secrets",
			)
		}
	}
	if component.PackageRef != "" &&
		!isPortableImmutablePackageReference(component.PackageRef, component.Digest) {
		result.AddError(
			field+".packageRef",
			"immutableReference",
			"component release packageRef must be a canonical credential-free OCI repository",
		)
	}
	if component.Checksum != "" {
		result.AddError(
			field+".checksum",
			"forbidden",
			"component release components use the contract artifact digest and must not declare checksum",
		)
	}
	if component.ApplicationVersionID != nil {
		result.AddError(
			field+".applicationVersionId",
			"forbidden",
			"component release components cannot reference application versions",
		)
	}
	if component.ChildReleaseBundleID != nil {
		result.AddError(
			field+".childReleaseBundleId",
			"forbidden",
			"component release components cannot reference child release bundles",
		)
	}
}

func packageReferenceContainsUserInfo(value string) bool {
	parsed, err := url.Parse("oci://" + strings.TrimSpace(value))
	return err == nil && parsed.User != nil
}

func componentReleaseProjectionField(key string, index int) string {
	key = strings.TrimSpace(key)
	if !componentKeyPattern.MatchString(key) || len(key) > maxComponentReleaseProjectionStringFieldLength {
		return fmt.Sprintf("components[%d]", index)
	}
	return "components." + key
}

func hasValidationRule(issues []ValidationIssue, rule string) bool {
	for _, issue := range issues {
		if issue.Rule == rule {
			return true
		}
	}
	return false
}

func componentTypeForArtifact(artifactType string) types.ReleaseBundleComponentType {
	switch artifactType {
	case "oci-image":
		return types.ReleaseBundleComponentTypeOCIImage
	case "oci-artifact":
		return types.ReleaseBundleComponentTypeOCIArtifact
	case "helm-chart":
		return types.ReleaseBundleComponentTypeHelmChart
	default:
		return ""
	}
}

func validateComponentContent(
	result *ValidationResult,
	fieldPrefix string,
	component types.ReleaseBundleComponent,
) {
	if !component.Type.IsValid() {
		result.AddError(fieldPrefix+".type", "valid", "component type is invalid")
	}
	if strings.TrimSpace(component.Version) == "" {
		result.AddError(fieldPrefix+".version", "required", "component version is required")
	}
	switch component.Type {
	case types.ReleaseBundleComponentTypeApplicationVersion:
		if component.ApplicationVersionID == nil {
			result.AddError(
				fieldPrefix+".applicationVersionId",
				"required",
				"application version component must reference an application version",
			)
		}
		if component.ChildReleaseBundleID != nil {
			result.AddError(
				fieldPrefix+".childReleaseBundleId",
				"forbidden",
				"application version component cannot reference a child release bundle",
			)
		}
	case types.ReleaseBundleComponentTypeOCIImage, types.ReleaseBundleComponentTypeOCIArtifact:
		if strings.TrimSpace(component.PackageRef) == "" {
			result.AddError(fieldPrefix+".packageRef", "required", "OCI component package reference is required")
		}
		if !IsSHA256Digest(strings.TrimSpace(component.Digest)) {
			result.AddError(fieldPrefix+".digest", "sha256", "OCI component digest must be a sha256 digest")
		}
		if component.ApplicationVersionID != nil || component.ChildReleaseBundleID != nil {
			result.AddError(
				fieldPrefix+".references",
				"forbidden",
				"OCI component cannot reference application versions or child bundles",
			)
		}
	case types.ReleaseBundleComponentTypeHelmChart:
		if strings.TrimSpace(component.PackageRef) == "" {
			result.AddError(fieldPrefix+".packageRef", "required", "helm chart package reference is required")
		}
		if component.ApplicationVersionID != nil || component.ChildReleaseBundleID != nil {
			result.AddError(
				fieldPrefix+".references",
				"forbidden",
				"helm chart component cannot reference application versions or child bundles",
			)
		}
	case types.ReleaseBundleComponentTypeChildReleaseBundle:
		if component.ChildReleaseBundleID == nil {
			result.AddError(
				fieldPrefix+".childReleaseBundleId",
				"required",
				"child release bundle component must reference a child release bundle",
			)
		}
		if component.ApplicationVersionID != nil {
			result.AddError(
				fieldPrefix+".applicationVersionId",
				"forbidden",
				"child release bundle component cannot reference an application version",
			)
		}
	case types.ReleaseBundleComponentTypeExternalArtifact:
		if strings.TrimSpace(component.PackageRef) == "" {
			result.AddError(fieldPrefix+".packageRef", "required", "external artifact package reference is required")
		}
		if strings.TrimSpace(component.Checksum) == "" {
			result.AddError(fieldPrefix+".checksum", "required", "external artifact checksum is required")
		}
		if component.ApplicationVersionID != nil || component.ChildReleaseBundleID != nil {
			result.AddError(
				fieldPrefix+".references",
				"forbidden",
				"external artifact component cannot reference application versions or child bundles",
			)
		}
	}
}

func joinIssueField(prefix, field string) string {
	if prefix == "" {
		return field
	}
	if field == "" {
		return prefix
	}
	return prefix + "." + field
}
