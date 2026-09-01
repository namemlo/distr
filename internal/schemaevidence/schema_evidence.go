package schemaevidence

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

const (
	MaxDocumentBytes     = 256 * 1024
	maxEvidenceDocuments = 256
	maxMixedVersionFacts = 1024
)

var checksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type ValidationError struct {
	Code string
	err  error
}

func (err *ValidationError) Error() string {
	return err.err.Error()
}

func (err *ValidationError) Unwrap() error {
	return err.err
}

func ErrorCode(err error) string {
	var validationErr *ValidationError
	if errors.As(err, &validationErr) {
		return validationErr.Code
	}
	return "schema_evidence_invalid"
}

func DecodeSchemaReport(payload []byte) (types.SchemaReport, error) {
	var report types.SchemaReport
	if err := decodeStrict(payload, &report); err != nil {
		return report, validationError("schema_evidence_invalid", err)
	}
	report = normalizeSchemaReport(report)
	if err := validateSchemaReportDocument(report); err != nil {
		return types.SchemaReport{}, err
	}
	return report, nil
}

func DecodeMigrationEvidence(payload []byte) (types.MigrationEvidence, error) {
	var evidence types.MigrationEvidence
	if err := decodeStrict(payload, &evidence); err != nil {
		return evidence, validationError("schema_evidence_invalid", err)
	}
	evidence = normalizeMigrationEvidence(evidence)
	if err := validateMigrationEvidenceDocument(evidence); err != nil {
		return types.MigrationEvidence{}, err
	}
	return evidence, nil
}

func SchemaReportChecksum(report types.SchemaReport) (string, error) {
	report = normalizeSchemaReport(report)
	return canonicalChecksum(struct {
		Schema              string                        `json:"schema"`
		Scope               types.SchemaEvidenceScope     `json:"scope"`
		Component           types.SchemaEvidenceComponent `json:"component"`
		DatabaseResourceKey string                        `json:"databaseResourceKey"`
		Current             types.SchemaState             `json:"current"`
		IssuedAt            time.Time                     `json:"issuedAt"`
		ExpiresAt           time.Time                     `json:"expiresAt"`
	}{
		Schema: report.Schema, Scope: report.Scope, Component: report.Component,
		DatabaseResourceKey: report.DatabaseResourceKey, Current: report.Current,
		IssuedAt: report.IssuedAt, ExpiresAt: report.ExpiresAt,
	})
}

func MigrationEvidenceChecksum(evidence types.MigrationEvidence) (string, error) {
	evidence = normalizeMigrationEvidence(evidence)
	return canonicalChecksum(struct {
		Schema               string                             `json:"schema"`
		Scope                types.SchemaEvidenceScope          `json:"scope"`
		Component            types.SchemaEvidenceComponent      `json:"component"`
		DatabaseResourceKey  string                             `json:"databaseResourceKey"`
		SchemaReportChecksum string                             `json:"schemaReportChecksum"`
		Decision             string                             `json:"decision"`
		ExpectedCurrent      types.SchemaState                  `json:"expectedCurrent"`
		Migrations           []types.SchemaMigrationBinding     `json:"migrations"`
		MixedVersionEvidence []types.MixedVersionSchemaEvidence `json:"mixedVersionEvidence"`
		IssuedAt             time.Time                          `json:"issuedAt"`
		ExpiresAt            time.Time                          `json:"expiresAt"`
	}{
		Schema: evidence.Schema, Scope: evidence.Scope, Component: evidence.Component,
		DatabaseResourceKey:  evidence.DatabaseResourceKey,
		SchemaReportChecksum: evidence.SchemaReportChecksum, Decision: evidence.Decision,
		ExpectedCurrent: evidence.ExpectedCurrent, Migrations: evidence.Migrations,
		MixedVersionEvidence: evidence.MixedVersionEvidence,
		IssuedAt:             evidence.IssuedAt, ExpiresAt: evidence.ExpiresAt,
	})
}

func ValidatePlan(
	input types.PlanResolutionInput,
	baselines []types.DeploymentPlanBaseline,
	contracts []types.MigrationContract,
) ([]types.SchemaEvidenceRequirement, []types.SchemaEvidenceBundle, []types.ValidationIssue) {
	requirements := requirementsFromResolution(input, contracts)
	context := validationContext{
		evaluatedAt: input.EffectiveAt.UTC(),
		scope: types.SchemaEvidenceScope{
			OrganizationID: input.Config.OrganizationID, DeploymentScopeID: input.Unit.DeploymentScopeID,
			DeploymentUnitID: input.Unit.ID, EnvironmentAssignmentID: input.Assignment.ID,
			EnvironmentID:          input.Assignment.EnvironmentID,
			DeploymentTargetID:     input.Assignment.DeploymentTargetID,
			TargetConfigSnapshotID: input.Config.ID,
		},
		pins: pinsByComponent(input.ReleasePins), baselines: baselinesByComponent(baselines),
		contracts: contracts, requirements: requirements,
	}
	bundles, issues := validateRecords(
		context,
		input.Config.SchemaReports,
		input.Config.MigrationEvidence,
	)
	issues = append(issues, input.Config.SchemaEvidenceIssues...)
	sortIssues(issues)
	return requirements, bundles, slices.CompactFunc(issues, sameIssue)
}

func ValidateCanonicalPlan(
	canonical types.TargetDeploymentPlanCanonical,
	organizationID uuid.UUID,
	evaluatedAt time.Time,
) []types.ValidationIssue {
	requirements := normalizeRequirements(canonical.SchemaEvidenceRequirements)
	for _, contract := range canonical.MigrationContracts {
		requirement := types.SchemaEvidenceRequirement{
			ComponentKey:        strings.TrimSpace(contract.ComponentKey),
			DatabaseResourceKey: strings.TrimSpace(contract.DatabaseResourceKey),
		}
		if requirement.ComponentKey != "" && requirement.DatabaseResourceKey != "" &&
			!slices.Contains(requirements, requirement) {
			requirements = append(requirements, requirement)
		}
	}
	requirements = normalizeRequirements(requirements)
	reports := make([]types.SchemaReportRecord, 0, len(canonical.SchemaEvidence))
	evidence := make([]types.MigrationEvidenceRecord, 0, len(canonical.SchemaEvidence))
	issues := make([]types.ValidationIssue, 0)
	for index, bundle := range canonical.SchemaEvidence {
		if bundle.Requirement.ComponentKey != bundle.SchemaReport.Component.ComponentKey ||
			bundle.Requirement.DatabaseResourceKey != bundle.SchemaReport.DatabaseResourceKey {
			issues = append(issues, issue(
				"schema_evidence_scope_mismatch",
				fmt.Sprintf("schemaEvidence.%d.requirement", index),
				"schema evidence bundle requirement does not match its report",
			))
		}
		reports = append(reports, types.SchemaReportRecord{
			Object: bundle.SchemaReportObject, Report: bundle.SchemaReport,
		})
		evidence = append(evidence, types.MigrationEvidenceRecord{
			Object: bundle.MigrationEvidenceObject, Evidence: bundle.MigrationEvidence,
		})
	}
	context := validationContext{
		evaluatedAt: evaluatedAt.UTC(),
		scope: types.SchemaEvidenceScope{
			OrganizationID:          organizationID,
			DeploymentScopeID:       canonical.DeploymentScopeID,
			DeploymentUnitID:        canonical.DeploymentUnitID,
			EnvironmentAssignmentID: canonical.EnvironmentAssignmentID,
			EnvironmentID:           canonical.EnvironmentID,
			DeploymentTargetID:      canonical.DeploymentTargetID,
			TargetConfigSnapshotID:  canonical.TargetConfigSnapshotID,
		},
		pins:      pinsByComponent(canonical.ComponentReleasePins),
		baselines: baselinesByComponent(canonical.Baselines),
		contracts: canonical.MigrationContracts, requirements: requirements,
	}
	validated, validationIssues := validateRecords(context, reports, evidence)
	issues = append(issues, validationIssues...)
	if len(validated) != len(canonical.SchemaEvidence) && len(validationIssues) == 0 {
		issues = append(issues, issue(
			"schema_evidence_incomplete", "schemaEvidence",
			"canonical schema evidence coverage is incomplete",
		))
	}
	sortIssues(issues)
	return slices.CompactFunc(issues, sameIssue)
}

type validationContext struct {
	evaluatedAt  time.Time
	scope        types.SchemaEvidenceScope
	pins         map[string]types.ComponentReleasePin
	baselines    map[string]types.DeploymentPlanBaseline
	contracts    []types.MigrationContract
	requirements []types.SchemaEvidenceRequirement
}

func validateRecords(
	context validationContext,
	reports []types.SchemaReportRecord,
	evidence []types.MigrationEvidenceRecord,
) ([]types.SchemaEvidenceBundle, []types.ValidationIssue) {
	issues := make([]types.ValidationIssue, 0)
	if len(reports) > maxEvidenceDocuments || len(evidence) > maxEvidenceDocuments {
		return nil, []types.ValidationIssue{issue(
			"schema_evidence_limit_exceeded", "schemaEvidence",
			"schema evidence document limit exceeded",
		)}
	}
	usedReports := make(map[int]struct{}, len(reports))
	usedEvidence := make(map[int]struct{}, len(evidence))
	bundles := make([]types.SchemaEvidenceBundle, 0, len(context.requirements))
	orderedContracts := slices.Clone(context.contracts)
	if !migrationContractsOrdered(orderedContracts) {
		issues = append(issues, issue(
			"schema_evidence_migration_binding_incomplete", "schemaEvidence",
			"schema evidence cannot bind an invalid migration graph",
		))
	}
	for _, requirement := range context.requirements {
		field := "schemaEvidence." + requirement.ComponentKey + "." + requirement.DatabaseResourceKey
		reportIndex, reportIssue := selectSchemaReport(requirement, reports, usedReports, field)
		if reportIssue != nil {
			issues = append(issues, *reportIssue)
			continue
		}
		evidenceIndex, evidenceIssue := selectMigrationEvidence(requirement, evidence, usedEvidence, field)
		if evidenceIssue != nil {
			issues = append(issues, *evidenceIssue)
			continue
		}
		usedReports[reportIndex] = struct{}{}
		usedEvidence[evidenceIndex] = struct{}{}
		reportRecord := reports[reportIndex]
		evidenceRecord := evidence[evidenceIndex]
		if !evidenceObjectValid(reportRecord.Object, types.SchemaReportMediaTypeV1) ||
			!evidenceObjectValid(evidenceRecord.Object, types.MigrationEvidenceMediaTypeV1) {
			issues = append(issues, issue(
				"schema_evidence_object_mismatch", field+".objects",
				"schema evidence object identity and checksum bindings are invalid",
			))
			continue
		}
		pin, pinFound := context.pins[requirement.ComponentKey]
		baseline, baselineFound := context.baselines[requirement.ComponentKey]
		componentContracts := make([]types.MigrationContract, 0)
		for _, contract := range orderedContracts {
			if strings.TrimSpace(contract.ComponentKey) == requirement.ComponentKey &&
				strings.TrimSpace(contract.DatabaseResourceKey) == requirement.DatabaseResourceKey {
				componentContracts = append(componentContracts, contract)
			}
		}
		componentIssues := validateBundle(
			context, requirement, reportRecord.Report, evidenceRecord.Evidence,
			pin, pinFound, baseline, baselineFound, componentContracts, field,
		)
		issues = append(issues, componentIssues...)
		if len(componentIssues) == 0 {
			bundles = append(bundles, types.SchemaEvidenceBundle{
				Requirement:             requirement,
				SchemaReportObject:      reportRecord.Object,
				MigrationEvidenceObject: evidenceRecord.Object,
				SchemaReport:            reportRecord.Report,
				MigrationEvidence:       evidenceRecord.Evidence,
			})
		}
	}
	for index := range reports {
		if _, used := usedReports[index]; !used {
			issues = append(issues, issue(
				"schema_evidence_wrong_scope", "schemaEvidence.schemaReports",
				"schema report does not belong to a required component database scope",
			))
		}
	}
	for index := range evidence {
		if _, used := usedEvidence[index]; !used {
			issues = append(issues, issue(
				"schema_evidence_wrong_scope", "schemaEvidence.migrationEvidence",
				"migration evidence does not belong to a required component database scope",
			))
		}
	}
	slices.SortFunc(bundles, func(left, right types.SchemaEvidenceBundle) int {
		if cmp := strings.Compare(left.Requirement.ComponentKey, right.Requirement.ComponentKey); cmp != 0 {
			return cmp
		}
		return strings.Compare(left.Requirement.DatabaseResourceKey, right.Requirement.DatabaseResourceKey)
	})
	return bundles, issues
}

func validateBundle(
	context validationContext,
	requirement types.SchemaEvidenceRequirement,
	report types.SchemaReport,
	evidence types.MigrationEvidence,
	pin types.ComponentReleasePin,
	pinFound bool,
	baseline types.DeploymentPlanBaseline,
	baselineFound bool,
	contracts []types.MigrationContract,
	field string,
) []types.ValidationIssue {
	issues := make([]types.ValidationIssue, 0)
	if report.Scope != context.scope || evidence.Scope != context.scope {
		issues = append(issues, issue(
			"schema_evidence_scope_mismatch", field+".scope",
			"schema evidence target and placement scope must match the plan exactly",
		))
	}
	if !pinFound || !schemaComponentMatches(report.Component, pin) ||
		!schemaComponentMatches(evidence.Component, pin) {
		issues = append(issues, issue(
			"schema_evidence_component_mismatch", field+".component",
			"schema evidence component release must match the pinned release exactly",
		))
	}
	if report.DatabaseResourceKey != requirement.DatabaseResourceKey ||
		evidence.DatabaseResourceKey != requirement.DatabaseResourceKey ||
		report.Current.ComponentKey != requirement.ComponentKey ||
		report.Current.DatabaseResourceKey != requirement.DatabaseResourceKey ||
		evidence.ExpectedCurrent.ComponentKey != requirement.ComponentKey ||
		evidence.ExpectedCurrent.DatabaseResourceKey != requirement.DatabaseResourceKey {
		issues = append(issues, issue(
			"schema_evidence_schema_mismatch", field+".schema",
			"schema evidence database and schema identity must match the required boundary",
		))
	}
	issues = append(issues, freshnessIssues(
		report.IssuedAt,
		report.ExpiresAt,
		context.evaluatedAt,
		field+".schemaReport",
	)...)
	issues = append(issues, freshnessIssues(
		evidence.IssuedAt,
		evidence.ExpiresAt,
		context.evaluatedAt,
		field+".migrationEvidence",
	)...)
	if evidence.IssuedAt.Before(report.IssuedAt) || evidence.ExpiresAt.After(report.ExpiresAt) {
		issues = append(issues, issue(
			"schema_evidence_freshness_mismatch", field+".migrationEvidence",
			"migration evidence validity must be contained by its schema report",
		))
	}
	reportChecksum, reportChecksumErr := SchemaReportChecksum(report)
	evidenceChecksum, evidenceChecksumErr := MigrationEvidenceChecksum(evidence)
	if reportChecksumErr != nil || evidenceChecksumErr != nil ||
		report.Checksum != reportChecksum || evidence.Checksum != evidenceChecksum ||
		evidence.SchemaReportChecksum != report.Checksum {
		issues = append(issues, issue(
			"schema_evidence_checksum_mismatch", field+".checksum",
			"schema report and migration evidence checksums must be complete and linked",
		))
	}
	if !sameSchemaState(report.Current, evidence.ExpectedCurrent) {
		issues = append(issues, issue(
			"schema_evidence_expected_current_stale", field+".expectedCurrent",
			"migration evidence expected-current state does not match the schema report",
		))
	}
	if !baselineFound || (!baseline.Bootstrap &&
		(baseline.SchemaState != report.Current.Version || baseline.SchemaChecksum != report.Current.Checksum)) {
		issues = append(issues, issue(
			"schema_evidence_expected_current_stale", field+".expectedCurrent",
			"schema evidence expected-current state does not match the authoritative baseline",
		))
	}
	issues = append(issues, migrationDecisionIssues(report.Current, evidence, contracts, field)...)
	if pinFound && baselineFound && !mixedVersionEvidenceMatches(
		evidence.MixedVersionEvidence,
		mixedVersionExpectation(pin.Version, baseline, report.Current, contracts),
	) {
		issues = append(issues, issue(
			"schema_evidence_mixed_version_incomplete", field+".mixedVersionEvidence",
			"mixed-version evidence must cover prior and target applications across every schema state",
		))
	}
	return issues
}

func migrationDecisionIssues(
	current types.SchemaState,
	evidence types.MigrationEvidence,
	contracts []types.MigrationContract,
	field string,
) []types.ValidationIssue {
	if len(contracts) == 0 {
		if evidence.Decision == types.SchemaDecisionNoMigration && len(evidence.Migrations) == 0 {
			return nil
		}
		return []types.ValidationIssue{issue(
			"schema_evidence_decision_mismatch", field+".decision",
			"components without database migrations require COMPATIBLE_NO_MIGRATION_REQUIRED evidence",
		)}
	}
	issues := make([]types.ValidationIssue, 0, 2)
	if evidence.Decision != types.SchemaDecisionMigrationBound {
		issues = append(issues, issue(
			"schema_evidence_decision_mismatch", field+".decision",
			"database migrations require explicit MIGRATION_BOUND evidence",
		))
	}
	if !migrationBindingsMatch(current, evidence.Migrations, contracts) {
		issues = append(issues, issue(
			"schema_evidence_migration_binding_incomplete", field+".migrations",
			"migration evidence must bind every applicable migration contract and schema transition exactly",
		))
	}
	return issues
}

func requirementsFromResolution(
	input types.PlanResolutionInput,
	contracts []types.MigrationContract,
) []types.SchemaEvidenceRequirement {
	bindings := make(map[string]uuid.UUID, len(input.Config.ComponentBindings))
	for _, binding := range input.Config.ComponentBindings {
		bindings[strings.TrimSpace(binding.ComponentKey)] = binding.ComponentInstanceID
	}
	instances := make(map[uuid.UUID]types.ComponentInstance, len(input.ComponentInstances))
	for _, instance := range input.ComponentInstances {
		instances[instance.ID] = instance
	}
	result := make([]types.SchemaEvidenceRequirement, 0)
	for _, pin := range input.ReleasePins {
		componentKey := strings.TrimSpace(pin.ComponentKey)
		if instance, ok := instances[bindings[componentKey]]; ok && strings.TrimSpace(instance.DatabaseBoundary) != "" {
			result = append(result, types.SchemaEvidenceRequirement{
				ComponentKey:        componentKey,
				DatabaseResourceKey: strings.TrimSpace(instance.DatabaseBoundary),
			})
		}
	}
	for _, contract := range contracts {
		result = append(result, types.SchemaEvidenceRequirement{
			ComponentKey:        strings.TrimSpace(contract.ComponentKey),
			DatabaseResourceKey: strings.TrimSpace(contract.DatabaseResourceKey),
		})
	}
	return normalizeRequirements(result)
}

func selectSchemaReport(
	requirement types.SchemaEvidenceRequirement,
	reports []types.SchemaReportRecord,
	used map[int]struct{},
	field string,
) (int, *types.ValidationIssue) {
	return selectEvidenceRecord(
		requirement,
		len(reports),
		used,
		field+".schemaReport",
		"exactly one schema report is required for each component database boundary",
		"a current checksummed schema report is required",
		func(index int) (string, string) {
			return reports[index].Report.Component.ComponentKey,
				reports[index].Report.DatabaseResourceKey
		},
	)
}

func selectMigrationEvidence(
	requirement types.SchemaEvidenceRequirement,
	evidence []types.MigrationEvidenceRecord,
	used map[int]struct{},
	field string,
) (int, *types.ValidationIssue) {
	return selectEvidenceRecord(
		requirement,
		len(evidence),
		used,
		field+".migrationEvidence",
		"exactly one migration evidence document is required for each component database boundary",
		"checksummed migration decision evidence is required",
		func(index int) (string, string) {
			return evidence[index].Evidence.Component.ComponentKey,
				evidence[index].Evidence.DatabaseResourceKey
		},
	)
}

func selectEvidenceRecord(
	requirement types.SchemaEvidenceRequirement,
	count int,
	used map[int]struct{},
	field string,
	ambiguousMessage string,
	missingMessage string,
	identity func(int) (string, string),
) (int, *types.ValidationIssue) {
	matches := make([]int, 0, 1)
	for index := range count {
		if _, exists := used[index]; exists {
			continue
		}
		componentKey, databaseResourceKey := identity(index)
		if componentKey == requirement.ComponentKey &&
			databaseResourceKey == requirement.DatabaseResourceKey {
			matches = append(matches, index)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		value := issue(
			"schema_evidence_ambiguous", field, ambiguousMessage,
		)
		return -1, &value
	}
	for index := range count {
		if _, exists := used[index]; exists {
			continue
		}
		componentKey, databaseResourceKey := identity(index)
		if databaseResourceKey == requirement.DatabaseResourceKey ||
			componentKey == requirement.ComponentKey {
			return index, nil
		}
	}
	value := issue(
		"schema_evidence_missing", field, missingMessage,
	)
	return -1, &value
}

func migrationBindingsMatch(
	current types.SchemaState,
	bindings []types.SchemaMigrationBinding,
	contracts []types.MigrationContract,
) bool {
	if len(bindings) != len(contracts) {
		return false
	}
	expectedVersion, expectedChecksum := current.Version, current.Checksum
	for index, contract := range contracts {
		binding := bindings[index]
		if binding.MigrationID != contract.ID || binding.ContractChecksum != contract.Checksum ||
			binding.ExpectedSourceVersion != contract.ExpectedSourceVersion ||
			binding.ExpectedSourceChecksum != contract.ExpectedSourceChecksum ||
			binding.ResultingVersion != contract.ResultingVersion ||
			binding.ResultingSchemaChecksum != contract.ResultingSchemaChecksum ||
			contract.ExpectedSourceVersion != expectedVersion ||
			contract.ExpectedSourceChecksum != expectedChecksum {
			return false
		}
		expectedVersion = contract.ResultingVersion
		expectedChecksum = contract.ResultingSchemaChecksum
	}
	return true
}

func migrationContractsOrdered(contracts []types.MigrationContract) bool {
	seen := make(map[string]struct{}, len(contracts))
	for _, contract := range contracts {
		if strings.TrimSpace(contract.ID) == "" {
			return false
		}
		if _, duplicate := seen[contract.ID]; duplicate {
			return false
		}
		for _, dependency := range contract.DependsOn {
			if _, available := seen[dependency]; !available {
				return false
			}
		}
		seen[contract.ID] = struct{}{}
	}
	return true
}

func mixedVersionExpectation(
	targetVersion string,
	baseline types.DeploymentPlanBaseline,
	current types.SchemaState,
	contracts []types.MigrationContract,
) []types.MixedVersionSchemaEvidence {
	applicationVersions := []string{strings.TrimSpace(targetVersion)}
	if previous := strings.TrimSpace(baseline.Version); previous != "" {
		applicationVersions = append(applicationVersions, previous)
	}
	sort.Strings(applicationVersions)
	applicationVersions = slices.Compact(applicationVersions)
	states := []types.SchemaState{current}
	for _, contract := range contracts {
		state := types.SchemaState{
			ComponentKey: current.ComponentKey, DatabaseResourceKey: current.DatabaseResourceKey,
			Version: contract.ResultingVersion, Checksum: contract.ResultingSchemaChecksum,
		}
		if !slices.ContainsFunc(states, func(existing types.SchemaState) bool {
			return existing.Version == state.Version && existing.Checksum == state.Checksum
		}) {
			states = append(states, state)
		}
	}
	result := make([]types.MixedVersionSchemaEvidence, 0, len(applicationVersions)*len(states))
	for _, applicationVersion := range applicationVersions {
		for _, state := range states {
			result = append(result, types.MixedVersionSchemaEvidence{
				ApplicationVersion: applicationVersion,
				SchemaVersion:      state.Version, SchemaChecksum: state.Checksum, Compatible: true,
			})
		}
	}
	return normalizeMixedVersionEvidence(result)
}

func mixedVersionEvidenceMatches(
	actual, expected []types.MixedVersionSchemaEvidence,
) bool {
	actual = normalizeMixedVersionEvidence(actual)
	expected = normalizeMixedVersionEvidence(expected)
	return slices.Equal(actual, expected)
}

func freshnessIssues(
	issuedAt, expiresAt, evaluatedAt time.Time,
	field string,
) []types.ValidationIssue {
	switch {
	case issuedAt.After(evaluatedAt):
		return []types.ValidationIssue{issue(
			"schema_evidence_not_yet_valid", field+".issuedAt",
			"schema evidence was issued after the plan decision time",
		)}
	case !expiresAt.After(evaluatedAt):
		return []types.ValidationIssue{issue(
			"schema_evidence_expired", field+".expiresAt",
			"schema evidence is expired at the plan decision time",
		)}
	default:
		return nil
	}
}

func validateSchemaReportDocument(report types.SchemaReport) error {
	if report.Schema != types.SchemaReportSchemaV1 {
		return validationError("schema_evidence_schema_mismatch", errors.New("unsupported schema report schema"))
	}
	if err := validateCommonDocument(
		report.Scope, report.Component, report.DatabaseResourceKey,
		report.Current, report.IssuedAt, report.ExpiresAt,
	); err != nil {
		return err
	}
	checksum, err := SchemaReportChecksum(report)
	if err != nil || report.Checksum != checksum {
		return validationError("schema_evidence_checksum_mismatch", errors.New("schema report checksum mismatch"))
	}
	return nil
}

func validateMigrationEvidenceDocument(evidence types.MigrationEvidence) error {
	if evidence.Schema != types.MigrationEvidenceSchemaV1 {
		return validationError("schema_evidence_schema_mismatch", errors.New("unsupported migration evidence schema"))
	}
	if err := validateCommonDocument(
		evidence.Scope, evidence.Component, evidence.DatabaseResourceKey,
		evidence.ExpectedCurrent, evidence.IssuedAt, evidence.ExpiresAt,
	); err != nil {
		return err
	}
	if !checksumPattern.MatchString(evidence.SchemaReportChecksum) {
		return validationError("schema_evidence_checksum_mismatch", errors.New("schema report checksum reference is invalid"))
	}
	if len(evidence.Migrations) > maxEvidenceDocuments ||
		len(evidence.MixedVersionEvidence) == 0 ||
		len(evidence.MixedVersionEvidence) > maxMixedVersionFacts {
		return validationError("schema_evidence_incomplete", errors.New("migration or mixed-version evidence is incomplete"))
	}
	seenMigrations := make(map[string]struct{}, len(evidence.Migrations))
	for _, binding := range evidence.Migrations {
		if !bounded(binding.MigrationID, 256) || !checksumPattern.MatchString(binding.ContractChecksum) ||
			!bounded(binding.ExpectedSourceVersion, 256) ||
			!checksumPattern.MatchString(binding.ExpectedSourceChecksum) ||
			!bounded(binding.ResultingVersion, 256) ||
			!checksumPattern.MatchString(binding.ResultingSchemaChecksum) {
			return validationError("schema_evidence_migration_binding_incomplete", errors.New("migration binding is invalid"))
		}
		if _, duplicate := seenMigrations[binding.MigrationID]; duplicate {
			return validationError("schema_evidence_migration_binding_incomplete", errors.New("migration binding is duplicated"))
		}
		seenMigrations[binding.MigrationID] = struct{}{}
	}
	seenMixed := make(map[string]struct{}, len(evidence.MixedVersionEvidence))
	for _, fact := range evidence.MixedVersionEvidence {
		key := fact.ApplicationVersion + "\x00" + fact.SchemaVersion + "\x00" + fact.SchemaChecksum
		if !bounded(fact.ApplicationVersion, 128) || !bounded(fact.SchemaVersion, 256) ||
			!checksumPattern.MatchString(fact.SchemaChecksum) {
			return validationError("schema_evidence_mixed_version_incomplete", errors.New("mixed-version evidence is invalid"))
		}
		if _, duplicate := seenMixed[key]; duplicate {
			return validationError(
				"schema_evidence_mixed_version_incomplete",
				errors.New("mixed-version evidence is duplicated"),
			)
		}
		seenMixed[key] = struct{}{}
	}
	switch evidence.Decision {
	case types.SchemaDecisionNoMigration:
		if len(evidence.Migrations) != 0 {
			return validationError(
				"schema_evidence_decision_mismatch",
				errors.New("no-migration evidence cannot contain migration bindings"),
			)
		}
	case types.SchemaDecisionMigrationBound:
		if len(evidence.Migrations) == 0 {
			return validationError(
				"schema_evidence_migration_binding_incomplete",
				errors.New("migration-bound evidence requires migration bindings"),
			)
		}
	default:
		return validationError("schema_evidence_decision_mismatch", errors.New("unsupported schema evidence decision"))
	}
	checksum, err := MigrationEvidenceChecksum(evidence)
	if err != nil || evidence.Checksum != checksum {
		return validationError("schema_evidence_checksum_mismatch", errors.New("migration evidence checksum mismatch"))
	}
	return nil
}

func validateCommonDocument(
	scope types.SchemaEvidenceScope,
	component types.SchemaEvidenceComponent,
	databaseResourceKey string,
	state types.SchemaState,
	issuedAt, expiresAt time.Time,
) error {
	if scope.OrganizationID == uuid.Nil || scope.DeploymentScopeID == uuid.Nil ||
		scope.DeploymentUnitID == uuid.Nil || scope.EnvironmentAssignmentID == uuid.Nil ||
		scope.EnvironmentID == uuid.Nil || scope.DeploymentTargetID == uuid.Nil ||
		scope.TargetConfigSnapshotID == uuid.Nil {
		return validationError("schema_evidence_scope_mismatch", errors.New("schema evidence scope is incomplete"))
	}
	if !bounded(component.ComponentKey, 128) || component.ComponentReleaseID == uuid.Nil ||
		!checksumPattern.MatchString(component.ReleaseChecksum) || !bounded(component.Version, 128) {
		return validationError(
			"schema_evidence_component_mismatch",
			errors.New("schema evidence component identity is invalid"),
		)
	}
	if !bounded(databaseResourceKey, 256) || state.ComponentKey != component.ComponentKey ||
		state.DatabaseResourceKey != databaseResourceKey || !bounded(state.Version, 256) ||
		!checksumPattern.MatchString(state.Checksum) {
		return validationError("schema_evidence_schema_mismatch", errors.New("schema identity or state is invalid"))
	}
	if issuedAt.IsZero() || expiresAt.IsZero() || !expiresAt.After(issuedAt) {
		return validationError(
			"schema_evidence_freshness_mismatch",
			errors.New("schema evidence validity interval is invalid"),
		)
	}
	return nil
}

func decodeStrict(payload []byte, destination any) error {
	if len(payload) == 0 || len(payload) > MaxDocumentBytes {
		return fmt.Errorf("schema evidence document must be between 1 and %d bytes", MaxDocumentBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode schema evidence document: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("schema evidence document contains trailing JSON")
	}
	return nil
}

func normalizeSchemaReport(report types.SchemaReport) types.SchemaReport {
	report.Schema = strings.TrimSpace(report.Schema)
	report.Component = normalizeComponent(report.Component)
	report.DatabaseResourceKey = strings.TrimSpace(report.DatabaseResourceKey)
	report.Current = normalizeSchemaState(report.Current)
	report.IssuedAt = report.IssuedAt.UTC()
	report.ExpiresAt = report.ExpiresAt.UTC()
	report.Checksum = strings.TrimSpace(report.Checksum)
	return report
}

func normalizeMigrationEvidence(evidence types.MigrationEvidence) types.MigrationEvidence {
	evidence.Schema = strings.TrimSpace(evidence.Schema)
	evidence.Component = normalizeComponent(evidence.Component)
	evidence.DatabaseResourceKey = strings.TrimSpace(evidence.DatabaseResourceKey)
	evidence.SchemaReportChecksum = strings.TrimSpace(evidence.SchemaReportChecksum)
	evidence.Decision = strings.TrimSpace(evidence.Decision)
	evidence.ExpectedCurrent = normalizeSchemaState(evidence.ExpectedCurrent)
	evidence.IssuedAt = evidence.IssuedAt.UTC()
	evidence.ExpiresAt = evidence.ExpiresAt.UTC()
	evidence.Checksum = strings.TrimSpace(evidence.Checksum)
	evidence.Migrations = slices.Clone(evidence.Migrations)
	for index := range evidence.Migrations {
		binding := &evidence.Migrations[index]
		binding.MigrationID = strings.TrimSpace(binding.MigrationID)
		binding.ContractChecksum = strings.TrimSpace(binding.ContractChecksum)
		binding.ExpectedSourceVersion = strings.TrimSpace(binding.ExpectedSourceVersion)
		binding.ExpectedSourceChecksum = strings.TrimSpace(binding.ExpectedSourceChecksum)
		binding.ResultingVersion = strings.TrimSpace(binding.ResultingVersion)
		binding.ResultingSchemaChecksum = strings.TrimSpace(binding.ResultingSchemaChecksum)
	}
	evidence.MixedVersionEvidence = normalizeMixedVersionEvidence(evidence.MixedVersionEvidence)
	return evidence
}

func normalizeMixedVersionEvidence(
	facts []types.MixedVersionSchemaEvidence,
) []types.MixedVersionSchemaEvidence {
	result := slices.Clone(facts)
	for index := range result {
		result[index].ApplicationVersion = strings.TrimSpace(result[index].ApplicationVersion)
		result[index].SchemaVersion = strings.TrimSpace(result[index].SchemaVersion)
		result[index].SchemaChecksum = strings.TrimSpace(result[index].SchemaChecksum)
	}
	slices.SortFunc(result, func(left, right types.MixedVersionSchemaEvidence) int {
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
	})
	return result
}

func normalizeRequirements(
	requirements []types.SchemaEvidenceRequirement,
) []types.SchemaEvidenceRequirement {
	result := make([]types.SchemaEvidenceRequirement, 0, len(requirements))
	for _, requirement := range requirements {
		requirement.ComponentKey = strings.TrimSpace(requirement.ComponentKey)
		requirement.DatabaseResourceKey = strings.TrimSpace(requirement.DatabaseResourceKey)
		if requirement.ComponentKey != "" && requirement.DatabaseResourceKey != "" {
			result = append(result, requirement)
		}
	}
	slices.SortFunc(result, func(left, right types.SchemaEvidenceRequirement) int {
		if cmp := strings.Compare(left.ComponentKey, right.ComponentKey); cmp != 0 {
			return cmp
		}
		return strings.Compare(left.DatabaseResourceKey, right.DatabaseResourceKey)
	})
	return slices.Compact(result)
}

func normalizeComponent(component types.SchemaEvidenceComponent) types.SchemaEvidenceComponent {
	component.ComponentKey = strings.TrimSpace(component.ComponentKey)
	component.ReleaseChecksum = strings.TrimSpace(component.ReleaseChecksum)
	component.Version = strings.TrimSpace(component.Version)
	return component
}

func normalizeSchemaState(state types.SchemaState) types.SchemaState {
	state.ComponentKey = strings.TrimSpace(state.ComponentKey)
	state.DatabaseResourceKey = strings.TrimSpace(state.DatabaseResourceKey)
	state.Version = strings.TrimSpace(state.Version)
	state.Checksum = strings.TrimSpace(state.Checksum)
	return state
}

func schemaComponentMatches(
	component types.SchemaEvidenceComponent,
	pin types.ComponentReleasePin,
) bool {
	return component.ComponentKey == strings.TrimSpace(pin.ComponentKey) &&
		component.ComponentReleaseID == pin.ComponentReleaseID &&
		component.ReleaseChecksum == pin.ReleaseChecksum &&
		component.Version == strings.TrimSpace(pin.Version)
}

func sameSchemaState(left, right types.SchemaState) bool {
	return normalizeSchemaState(left) == normalizeSchemaState(right)
}

func evidenceObjectValid(object types.SchemaEvidenceObject, expectedMediaType string) bool {
	return bounded(object.ObjectKey, 256) && bounded(object.Reference, 4096) &&
		object.MediaType == expectedMediaType && object.SizeBytes > 0 &&
		object.SizeBytes <= MaxDocumentBytes && checksumPattern.MatchString(object.Checksum)
}

func pinsByComponent(pins []types.ComponentReleasePin) map[string]types.ComponentReleasePin {
	result := make(map[string]types.ComponentReleasePin, len(pins))
	for _, pin := range pins {
		result[strings.TrimSpace(pin.ComponentKey)] = pin
	}
	return result
}

func baselinesByComponent(
	baselines []types.DeploymentPlanBaseline,
) map[string]types.DeploymentPlanBaseline {
	result := make(map[string]types.DeploymentPlanBaseline, len(baselines))
	for _, baseline := range baselines {
		result[strings.TrimSpace(baseline.ComponentKey)] = baseline
	}
	return result
}

func canonicalChecksum(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func validationError(code string, err error) error {
	return &ValidationError{Code: code, err: err}
}

func bounded(value string, maximum int) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= maximum
}

func issue(code, field, message string) types.ValidationIssue {
	return types.ValidationIssue{Code: code, Field: field, Message: message}
}

func sortIssues(issues []types.ValidationIssue) {
	slices.SortFunc(issues, func(left, right types.ValidationIssue) int {
		if cmp := strings.Compare(left.Field, right.Field); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(left.Code, right.Code); cmp != 0 {
			return cmp
		}
		return strings.Compare(left.Message, right.Message)
	})
}

func sameIssue(left, right types.ValidationIssue) bool {
	return left.Code == right.Code && left.Field == right.Field && left.Message == right.Message
}
