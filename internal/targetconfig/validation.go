package targetconfig

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

const (
	maxValidationIssues            = 100
	maxValidationMessageBytes      = 256
	maxTargetConfigObjects         = 100
	maxTargetConfigComponents      = 500
	maxTargetConfigSecrets         = 200
	maxTargetConfigFlags           = 500
	maxTargetConfigObjectSize      = 16 * 1024 * 1024
	maxTargetConfigVersionIDBytes  = 1024
	maxTargetConfigProviderIDBytes = 128
)

const sensitiveTargetConfigTokenNames = "password|passwd|secret|api[_-]?key|access[_-]?token|" +
	"authorization|private[_-]?key|credential"

var (
	targetConfigKeyPattern       = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*$`)
	targetConfigProviderPattern  = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*$`)
	targetConfigCommitPattern    = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)
	targetConfigChecksumPattern  = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	targetConfigMediaTypePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.+-]*/[a-z0-9][a-z0-9.+-]*$`)
	targetConfigPlatformPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*/[a-z0-9][a-z0-9._-]*$`)
	secretLookingPattern         = regexp.MustCompile(`(?i)(` + sensitiveTargetConfigTokenNames + `)`)
	inlineSecretPattern          = regexp.MustCompile(`(?i)(` + sensitiveTargetConfigTokenNames + `)\s*[:=]`)
	credentialPairPattern        = regexp.MustCompile(`^[^:/\s]+:[^/\s]+$`)
	commonCredentialPattern      = regexp.MustCompile(
		`(?i)^(gh[pousr]_[A-Za-z0-9]{20,}|` +
			`xox[baprs]-[A-Za-z0-9-]{20,}|(?:AKIA|ASIA)[A-Z0-9]{16})$`,
	)
	windowsPathPattern = regexp.MustCompile(`^[A-Za-z]:[\\/]`)
)

type issueCollector struct {
	issues []types.ValidationIssue
}

func (collector *issueCollector) add(code, field, message string) {
	if len(collector.issues) >= maxValidationIssues {
		return
	}
	if len(message) > maxValidationMessageBytes {
		message = string([]byte(message)[:maxValidationMessageBytes])
		for !utf8.ValidString(message) {
			message = message[:len(message)-1]
		}
	}
	collector.issues = append(collector.issues, types.ValidationIssue{
		Code: code, Field: field, Message: message,
	})
}

func ValidateDraft(draft types.TargetConfigSnapshotDraft) []types.ValidationIssue {
	collector := &issueCollector{}
	validateTargetConfigPlacement(collector, draft)
	validateTargetConfigSource(collector, draft)
	validateTargetConfigRuntimeConstraints(collector, draft.RuntimeConstraints)

	validateCollectionLimit(collector, "objects", len(draft.Objects), maxTargetConfigObjects)
	validateCollectionLimit(collector, "components", len(draft.Components), maxTargetConfigComponents)
	validateCollectionLimit(collector, "secretReferences", len(draft.SecretReferences), maxTargetConfigSecrets)
	validateCollectionLimit(collector, "featureFlags", len(draft.FeatureFlags), maxTargetConfigFlags)
	if len(draft.Objects) == 0 {
		collector.add("required", "objects", "at least one immutable object is required")
	}
	if len(draft.Components) == 0 {
		collector.add("required", "components", "at least one component mapping is required")
	}

	for index, object := range draft.Objects {
		validateTargetConfigObject(collector, index, object)
	}
	for index, component := range draft.Components {
		validateTargetConfigComponent(collector, index, draft.DeploymentUnitID, component)
	}
	for index, reference := range draft.SecretReferences {
		validateTargetConfigSecretReference(collector, index, reference)
	}
	for index, flag := range draft.FeatureFlags {
		validateTargetConfigKey(collector, fmt.Sprintf("featureFlags[%d].key", index), flag.Key)
	}

	if duplicate := duplicateObjectKey(draft.Objects); duplicate != "" {
		collector.add("duplicate", "objects", "duplicate object key")
	}
	if duplicate := duplicateComponentKey(draft.Components); duplicate != "" {
		collector.add("duplicate", "components", "duplicate component physical name")
	}
	if duplicate := duplicateSecretReferenceKey(draft.SecretReferences); duplicate != "" {
		collector.add("duplicate", "secretReferences", "duplicate secret reference key")
	}
	if duplicate := duplicateFeatureFlagKey(draft.FeatureFlags); duplicate != "" {
		collector.add("duplicate", "featureFlags", "duplicate feature flag key")
	}
	return collector.issues
}

func validateTargetConfigPlacement(collector *issueCollector, draft types.TargetConfigSnapshotDraft) {
	requiredUUID(collector, "deploymentUnitId", draft.DeploymentUnitID)
	requiredUUID(collector, "targetEnvironmentAssignmentId", draft.TargetEnvironmentAssignmentID)
	requiredUUID(collector, "environmentId", draft.EnvironmentID)
}

func validateTargetConfigSource(collector *issueCollector, draft types.TargetConfigSnapshotDraft) {
	repository := strings.TrimSpace(draft.SourceRepository)
	if repository == "" {
		collector.add("required", "sourceRepository", "source repository is required")
	} else if len(repository) > 2048 {
		collector.add("limit", "sourceRepository", "source repository is too long")
	} else if parsed, err := url.Parse(repository); err != nil ||
		(parsed.Scheme != "https" && parsed.Scheme != "ssh") ||
		parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		collector.add("secret_boundary", "sourceRepository", "source repository must be a credential-free immutable identity")
	}
	if !targetConfigCommitPattern.MatchString(strings.TrimSpace(draft.SourceCommit)) {
		collector.add("invalid", "sourceCommit", "source commit must be a full lowercase hexadecimal commit")
	}
	requiredBoundedString(collector, "sourceAdapter", draft.SourceAdapter, 128)
	requiredBoundedString(collector, "adapterVersion", draft.AdapterVersion, 128)
	platform := strings.TrimSpace(draft.TargetPlatform)
	if !targetConfigPlatformPattern.MatchString(platform) {
		collector.add("invalid", "targetPlatform", "target platform must use os/architecture form")
	}
}

func validateTargetConfigRuntimeConstraints(
	collector *issueCollector,
	constraints map[string]string,
) {
	if len(constraints) > 100 {
		collector.add("limit", "runtimeConstraints", "too many runtime constraints")
	}
	for key, value := range constraints {
		if !targetConfigKeyPattern.MatchString(key) || len(key) > 128 {
			collector.add("invalid", "runtimeConstraints", "runtime constraint key is invalid")
		}
		if len(value) > 1024 {
			collector.add("limit", "runtimeConstraints", "runtime constraint value is too long")
		}
		if secretLookingPattern.MatchString(key) || secretLookingPattern.MatchString(value) {
			collector.add("secret_boundary", "runtimeConstraints", "runtime constraints must not contain secret material")
		}
	}
}

func validateTargetConfigObject(
	collector *issueCollector,
	index int,
	object types.TargetConfigSnapshotObjectDraft,
) {
	prefix := fmt.Sprintf("objects[%d]", index)
	validateTargetConfigKey(collector, prefix+".key", object.Key)
	if !object.Kind.IsValid() {
		collector.add("unsupported", prefix+".kind", "object kind is unsupported")
	}
	if !targetConfigMediaTypePattern.MatchString(strings.TrimSpace(object.MediaType)) ||
		len(object.MediaType) > 128 {
		collector.add("invalid", prefix+".mediaType", "object media type is invalid")
	}
	if object.SizeBytes < 0 || object.SizeBytes > maxTargetConfigObjectSize {
		collector.add("limit", prefix+".sizeBytes", "object size is outside the verification limit")
	}
	if !targetConfigChecksumPattern.MatchString(strings.TrimSpace(object.Checksum)) {
		collector.add("invalid", prefix+".checksum", "object checksum must be lowercase SHA-256")
	}
	if !isImmutableTargetConfigObject(object) {
		collector.add("immutable_reference", prefix+".reference", "object reference is not an immutable object identity")
	}
}

func isImmutableTargetConfigObject(object types.TargetConfigSnapshotObjectDraft) bool {
	reference := strings.TrimSpace(object.Reference)
	if reference == "" || len(reference) > 2048 || strings.Contains(reference, "\\") {
		return false
	}
	parsed, err := url.Parse(reference)
	if err != nil || parsed.Scheme != "s3" || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" ||
		path.Clean(parsed.Path) != parsed.Path || strings.Contains(parsed.Path, "/../") {
		return false
	}
	if object.VersionID != "" {
		return validTargetConfigVersionID(object.VersionID)
	}
	if !targetConfigChecksumPattern.MatchString(strings.TrimSpace(object.Checksum)) {
		return false
	}
	segments := strings.Split(strings.TrimPrefix(parsed.Path, "/"), "/")
	return len(segments) >= 4 &&
		segments[0] == "_immutable" &&
		segments[1] == "sha256" &&
		regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(segments[2]) &&
		segments[len(segments)-1] != "" &&
		"sha256:"+segments[2] == strings.TrimSpace(object.Checksum)
}

func validateTargetConfigComponent(
	collector *issueCollector,
	index int,
	deploymentUnitID uuid.UUID,
	component types.TargetConfigSnapshotComponentDraft,
) {
	prefix := fmt.Sprintf("components[%d]", index)
	requiredBoundedString(collector, prefix+".physicalName", component.PhysicalName, 255)
	requiredUUID(collector, prefix+".componentInstanceId", component.ComponentInstanceID)
	requiredUUID(collector, prefix+".deploymentUnitId", component.DeploymentUnitID)
	if deploymentUnitID != uuid.Nil && component.DeploymentUnitID != uuid.Nil &&
		component.DeploymentUnitID != deploymentUnitID {
		collector.add("scope_mismatch", prefix+".deploymentUnitId", "component is outside the snapshot placement")
	}
}

func validateTargetConfigSecretReference(
	collector *issueCollector,
	index int,
	reference types.TargetConfigSnapshotSecretReferenceDraft,
) {
	prefix := fmt.Sprintf("secretReferences[%d]", index)
	validateTargetConfigKey(collector, prefix+".key", reference.Key)
	validateTargetConfigSecretProvider(collector, prefix+".provider", reference.Provider)
	value := strings.TrimSpace(reference.Reference)
	if value == "" || len(value) > 1024 || strings.Contains(value, "\\") ||
		strings.Contains(value, "://") || strings.Contains(value, "../") ||
		strings.HasPrefix(value, "/") || windowsPathPattern.MatchString(value) ||
		strings.ContainsAny(value, "?#\r\n") || inlineSecretPattern.MatchString(value) ||
		looksLikeClientConfigPath(value) {
		collector.add("secret_boundary", prefix+".reference", "secret reference must be an opaque provider identifier")
	}
	if !targetConfigChecksumPattern.MatchString(strings.TrimSpace(reference.VersionFingerprint)) {
		collector.add("invalid", prefix+".versionFingerprint", "secret version fingerprint must be lowercase SHA-256")
	}
}

func validateTargetConfigSecretProvider(collector *issueCollector, field, value string) {
	trimmed := strings.TrimSpace(value)
	switch {
	case trimmed == "":
		collector.add("required", field, field+" is required")
	case len(value) > maxTargetConfigProviderIDBytes:
		collector.add("limit", field, field+" is too long")
	case unsafeTargetConfigSecretProvider(trimmed):
		collector.add("secret_boundary", field, "secret provider must not contain credentials")
	case value != trimmed || !targetConfigProviderPattern.MatchString(value):
		collector.add("invalid", field, "secret provider must be a portable identifier")
	}
}

func unsafeTargetConfigSecretProvider(value string) bool {
	if inlineSecretPattern.MatchString(value) ||
		credentialPairPattern.MatchString(value) ||
		commonCredentialPattern.MatchString(value) {
		return true
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.User != nil
}

func validTargetConfigVersionID(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > maxTargetConfigVersionIDBytes ||
		value != strings.TrimSpace(value) ||
		!utf8.ValidString(value) ||
		inlineSecretPattern.MatchString(value) ||
		commonCredentialPattern.MatchString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func looksLikeClientConfigPath(value string) bool {
	lower := strings.ToLower(value)
	for _, prefix := range []string{
		"client/", "clients/", "config/", "configs/", "etc/", "opt/", "home/", "var/", "tmp/",
	} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	for _, suffix := range []string{
		".json", ".yaml", ".yml", ".env", ".ini", ".toml", ".config", ".xml", ".properties",
	} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

func requiredUUID(collector *issueCollector, field string, value uuid.UUID) {
	if value == uuid.Nil {
		collector.add("required", field, field+" is required")
	}
}

func requiredBoundedString(collector *issueCollector, field, value string, limit int) {
	value = strings.TrimSpace(value)
	if value == "" {
		collector.add("required", field, field+" is required")
	} else if len(value) > limit {
		collector.add("limit", field, field+" is too long")
	}
}

func validateTargetConfigKey(collector *issueCollector, field, value string) {
	value = strings.TrimSpace(value)
	if len(value) > 128 || !targetConfigKeyPattern.MatchString(value) {
		collector.add("invalid", field, field+" is invalid")
	}
}

func validateCollectionLimit(collector *issueCollector, field string, count, limit int) {
	if count > limit {
		collector.add("limit", field, field+" exceeds the item limit")
	}
}
