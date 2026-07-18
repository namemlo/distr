package releasebundles

import (
	"strings"

	"github.com/distr-sh/distr/internal/types"
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
	if bundle.CanonicalChecksum != "" || len(bundle.CanonicalPayload) > 0 {
		_, checksum, err := Canonicalize(bundle)
		if err != nil {
			result.AddError("canonicalPayload", "canonicalize", "canonical payload could not be computed")
		} else if checksum != bundle.CanonicalChecksum {
			result.AddError("canonicalChecksum", "sha256", "canonical payload does not match checksum")
		}
	}
	if bundle.ReleaseContract != nil {
		if bundle.ReleaseContract.ComponentV2 != nil {
			for _, issue := range ValidateComponentReleaseContractV2(*bundle.ReleaseContract.ComponentV2) {
				result.AddError("releaseContract."+issue.Field, issue.Rule, issue.Message)
			}
			validateComponentReleaseBundleMatch(&result, bundle, *bundle.ReleaseContract.ComponentV2)
		} else {
			result.Merge("", ValidateReleaseContractV1(*bundle.ReleaseContract, bundle.Components))
		}
	}

	seenKeys := map[string]struct{}{}
	for _, component := range bundle.Components {
		key := strings.TrimSpace(component.Key)
		if key == "" {
			result.AddError("components", "key", "component key is required")
			continue
		}
		if _, ok := seenKeys[key]; ok {
			result.AddError("components."+key+".key", "unique", "component key must be unique")
		}
		seenKeys[key] = struct{}{}
		validateComponentContent(&result, key, component)
	}
	result.Valid = len(result.Errors) == 0
	return result
}

func validateComponentReleaseBundleMatch(
	result *ValidationResult,
	bundle types.ReleaseBundle,
	contract types.ComponentReleaseContractV2,
) {
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
	components := make(map[string]types.ReleaseBundleComponent, len(bundle.Components))
	for _, component := range bundle.Components {
		components[component.Key] = component
	}
	for _, artifact := range contract.Artifacts {
		component, ok := components[artifact.Key]
		field := "releaseContract.artifacts." + artifact.Key
		if !ok {
			result.AddError(field, "matchesBundle", "component release artifact must match a release bundle component")
			continue
		}
		if component.Type != types.ReleaseBundleComponentTypeOCIImage &&
			component.Type != types.ReleaseBundleComponentTypeOCIArtifact &&
			component.Type != types.ReleaseBundleComponentTypeHelmChart {
			result.AddError(field+".type", "matchesBundle", "component release artifact type must match the bundle")
		}
		if component.Version != contract.Version {
			result.AddError(field+".version", "matchesBundle", "component release artifact version must match the contract")
		}
		if component.Digest != artifact.Digest {
			result.AddError(field+".digest", "matchesBundle", "component release artifact digest must match the bundle")
		}
	}
}

func validateComponentContent(result *ValidationResult, key string, component types.ReleaseBundleComponent) {
	fieldPrefix := "components." + key
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
