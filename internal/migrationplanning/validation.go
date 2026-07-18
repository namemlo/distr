package migrationplanning

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/distr-sh/distr/internal/types"
)

var (
	migrationIDPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
	checksumPattern       = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	resourceKeyPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,255}$`)
	idempotencyPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	artifactDigestPattern = regexp.MustCompile(`^\S+@sha256:[A-Fa-f0-9]{64}$`)
)

func ValidateMigrationContract(contract types.MigrationContract) []types.ValidationIssue {
	issues := make([]types.ValidationIssue, 0)
	add := func(code, field, message string) {
		issues = append(issues, types.ValidationIssue{Code: code, Field: field, Message: message})
	}
	if !migrationIDPattern.MatchString(contract.ID) {
		add("migration_id_invalid", "id", "migration ID must be a stable bounded key")
	}
	if !checksumPattern.MatchString(contract.Checksum) {
		add("migration_checksum_invalid", "checksum", "migration checksum must be an immutable sha256 digest")
	}
	if !migrationIDPattern.MatchString(contract.ComponentKey) {
		add("migration_component_invalid", "componentKey", "component key must be a stable bounded key")
	}
	if !resourceKeyPattern.MatchString(contract.DatabaseResourceKey) {
		add("database_resource_invalid", "databaseResourceKey", "database resource key must be a bounded stable key")
	}
	if !artifactDigestPattern.MatchString(contract.ArtifactDigest) {
		add("migration_artifact_invalid", "artifactDigest", "migration artifact must use an immutable sha256 digest")
	}
	if !resourceKeyPattern.MatchString(contract.AdapterType) {
		add("migration_adapter_invalid", "adapterType", "migration adapter type must be a bounded stable key")
	}
	if strings.TrimSpace(contract.ExpectedSourceVersion) == "" {
		add("source_version_required", "expectedSourceVersion", "expected source version is required")
	}
	if !checksumPattern.MatchString(contract.ExpectedSourceChecksum) {
		add(
			"source_checksum_invalid",
			"expectedSourceChecksum",
			"expected source checksum must be an immutable sha256 digest",
		)
	}
	if strings.TrimSpace(contract.ResultingVersion) == "" {
		add("resulting_version_required", "resultingVersion", "resulting schema version is required")
	}
	if !checksumPattern.MatchString(contract.ResultingSchemaChecksum) {
		add(
			"resulting_schema_checksum_invalid",
			"resultingSchemaChecksum",
			"resulting schema checksum must be an immutable sha256 digest",
		)
	}
	switch contract.Phase {
	case types.MigrationPhaseExpand, types.MigrationPhaseData,
		types.MigrationPhaseSwitch, types.MigrationPhaseContract:
	default:
		add("migration_phase_invalid", "phase", "phase must be expand, data, switch, or contract")
	}
	if contract.LockType != "shared" && contract.LockType != "exclusive" {
		add("migration_lock_type_invalid", "lockType", "lock type must be shared or exclusive")
	}
	if contract.LockTimeoutSeconds < 1 || contract.LockTimeoutSeconds > 86400 {
		add("migration_lock_timeout_invalid", "lockTimeoutSeconds", "lock timeout must be between 1 and 86400 seconds")
	}
	if strings.TrimSpace(contract.OperationalImpact) == "" {
		add("operational_impact_required", "operationalImpact", "estimated operational impact is required")
	}
	if contract.BackupRequired && strings.TrimSpace(contract.BackupVerifier) == "" {
		add("backup_verifier_required", "backupVerifier", "required backup must declare an evidence verifier")
	}
	if len(contract.PreconditionProbes) == 0 {
		add("precondition_probe_required", "preconditionProbes", "at least one precondition probe is required")
	}
	if len(contract.PostconditionProbes) == 0 {
		add("postcondition_probe_required", "postconditionProbes", "at least one postcondition probe is required")
	}
	validateProbes(contract.PreconditionProbes, "preconditionProbes", add)
	validateProbes(contract.PostconditionProbes, "postconditionProbes", add)
	switch contract.RetryClass {
	case types.MigrationRetryNone:
	case types.MigrationRetryBounded, types.MigrationRetrySafe:
		if !idempotencyPattern.MatchString(contract.IdempotencyKey) {
			add("idempotency_key_required", "idempotencyKey", "retryable migration must declare a stable idempotency key")
		}
	default:
		add("retry_class_invalid", "retryClass", "retry class must be none, bounded, or safe")
	}
	switch contract.Reversibility {
	case types.MigrationReversibilityReversible, types.MigrationReversibilityManual:
	case types.MigrationReversibilityForwardOnly:
		if !contract.RequiresForwardFix {
			add("forward_fix_required", "requiresForwardFix", "forward-only migration must require a forward-fix")
		}
	default:
		add("reversibility_invalid", "reversibility", "reversibility must be reversible, manual, or forward_only")
	}
	if strings.TrimSpace(contract.RecoveryProcedureReference) == "" {
		add("recovery_procedure_required", "recoveryProcedureReference", "a versioned recovery procedure is required")
	}
	if strings.TrimSpace(contract.PreviousApplicationCompatibility) == "" ||
		len(contract.PreviousApplicationCompatibility) > 256 {
		add("previous_compatibility_required", "previousApplicationCompatibility",
			"previous application compatibility must be a bounded declared range")
	}
	if contract.EvidenceRetentionDays < 1 || contract.EvidenceRetentionDays > 3650 {
		add("evidence_retention_invalid", "evidenceRetentionDays", "evidence retention must be between 1 and 3650 days")
	}
	if len(contract.DependsOn) > 64 {
		add("migration_dependency_limit", "dependsOn", "no more than 64 migration dependencies are allowed")
	}
	seenDependencies := map[string]struct{}{}
	for index, dependency := range contract.DependsOn {
		if !migrationIDPattern.MatchString(dependency) {
			add(
				"migration_dependency_invalid",
				fmt.Sprintf("dependsOn[%d]", index),
				"migration dependency must be a stable bounded key",
			)
		}
		if dependency == contract.ID {
			add("migration_self_dependency", fmt.Sprintf("dependsOn[%d]", index), "migration cannot depend on itself")
		}
		if _, exists := seenDependencies[dependency]; exists {
			add("migration_duplicate_dependency", fmt.Sprintf("dependsOn[%d]", index), "migration dependency is duplicated")
		}
		seenDependencies[dependency] = struct{}{}
	}
	slices.SortFunc(issues, func(a, b types.ValidationIssue) int {
		if cmp := strings.Compare(a.Field, b.Field); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.Code, b.Code)
	})
	return issues
}

func ValidatePreviousReleaseCompatibility(
	current types.SchemaState,
	planned types.PlannedState,
) []types.ValidationIssue {
	issues := make([]types.ValidationIssue, 0, 8)
	add := func(code, field, message string) {
		issues = append(issues, types.ValidationIssue{Code: code, Field: field, Message: message})
	}
	if planned.ForwardOnly {
		add(
			"previous_release_forward_only",
			"forwardOnly",
			"previous release is not compatible after a forward-only schema transition",
		)
	}
	required := []struct {
		value, field string
		known        func(string) bool
	}{
		{current.ComponentKey, "current.componentKey", migrationIDPattern.MatchString},
		{current.DatabaseResourceKey, "current.databaseResourceKey", resourceKeyPattern.MatchString},
		{current.Version, "current.version", knownSchemaVersion},
		{planned.ComponentKey, "planned.componentKey", migrationIDPattern.MatchString},
		{planned.DatabaseResourceKey, "planned.databaseResourceKey", resourceKeyPattern.MatchString},
		{planned.SchemaState, "planned.schemaState", knownSchemaVersion},
	}
	for _, candidate := range required {
		if !candidate.known(candidate.value) {
			add(
				"previous_release_state_unknown",
				candidate.field,
				"previous release compatibility requires a complete known schema state",
			)
		}
	}
	for _, candidate := range []struct {
		value, field string
	}{
		{current.Checksum, "current.checksum"},
		{planned.SchemaChecksum, "planned.schemaChecksum"},
	} {
		if !checksumPattern.MatchString(candidate.value) {
			add(
				"previous_release_checksum_unknown",
				candidate.field,
				"previous release compatibility requires a valid immutable schema checksum",
			)
		}
	}
	if current.ComponentKey != planned.ComponentKey {
		add(
			"previous_release_component_mismatch",
			"componentKey",
			"previous release schema state belongs to a different component",
		)
	}
	if current.DatabaseResourceKey != planned.DatabaseResourceKey {
		add(
			"previous_release_database_mismatch",
			"databaseResourceKey",
			"previous release schema state belongs to a different database resource",
		)
	}
	if current.Version != planned.SchemaState || current.Checksum != planned.SchemaChecksum {
		add(
			"previous_release_schema_incompatible",
			"schemaState",
			"schema version and checksum must both match the previous release plan",
		)
	}
	slices.SortFunc(issues, func(a, b types.ValidationIssue) int {
		if cmp := strings.Compare(a.Field, b.Field); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.Code, b.Code)
	})
	return issues
}

func knownSchemaVersion(value string) bool {
	return strings.TrimSpace(value) != "" &&
		len(value) <= 128 &&
		!strings.ContainsAny(value, "\r\n")
}

func validateProbes(
	probes []types.MigrationProbe,
	field string,
	add func(string, string, string),
) {
	if len(probes) > 32 {
		add("migration_probe_limit", field, "no more than 32 probes are allowed")
	}
	seen := map[string]struct{}{}
	for index, probe := range probes {
		prefix := fmt.Sprintf("%s[%d]", field, index)
		if strings.TrimSpace(probe.Name) == "" || len(probe.Name) > 128 {
			add("migration_probe_name_invalid", prefix+".name", "probe name must be between 1 and 128 characters")
		}
		if !resourceKeyPattern.MatchString(probe.Reference) {
			add("migration_probe_reference_invalid", prefix+".reference", "probe must use a bounded reference")
		}
		if !checksumPattern.MatchString(probe.ExpectedChecksum) {
			add(
				"migration_probe_checksum_invalid",
				prefix+".expectedChecksum",
				"probe result must bind an expected sha256 checksum",
			)
		}
		if _, exists := seen[probe.Reference]; exists {
			add("migration_probe_duplicate", prefix+".reference", "probe reference is duplicated")
		}
		seen[probe.Reference] = struct{}{}
	}
}
