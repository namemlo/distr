package schemaevidence

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestDecodeSchemaReportRequiresStrictChecksummedDocument(t *testing.T) {
	g := NewWithT(t)
	report, evidence, input, baselines := schemaEvidenceFixture(t, false)
	g.Expect(evidence.Checksum).NotTo(BeEmpty())
	g.Expect(input.ReleasePins).NotTo(BeEmpty())
	g.Expect(baselines).NotTo(BeEmpty())
	payload, err := json.Marshal(report)
	g.Expect(err).NotTo(HaveOccurred())

	decoded, err := DecodeSchemaReport(payload)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(decoded).To(Equal(report))

	var document map[string]any
	g.Expect(json.Unmarshal(payload, &document)).To(Succeed())
	document["unexpected"] = true
	payload, err = json.Marshal(document)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = DecodeSchemaReport(payload)
	g.Expect(ErrorCode(err)).To(Equal("schema_evidence_invalid"))

	report.Checksum = testChecksum("f")
	payload, err = json.Marshal(report)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = DecodeSchemaReport(payload)
	g.Expect(ErrorCode(err)).To(Equal("schema_evidence_checksum_mismatch"))
}

func TestValidatePlanAcceptsCompatibleNoMigrationEvidence(t *testing.T) {
	g := NewWithT(t)
	report, evidence, input, baselines := schemaEvidenceFixture(t, false)
	input.Config.SchemaReports = []types.SchemaReportRecord{{
		Object: testEvidenceObject("schema-report", types.SchemaReportMediaTypeV1), Report: report,
	}}
	input.Config.MigrationEvidence = []types.MigrationEvidenceRecord{{
		Object: testEvidenceObject("migration-evidence", types.MigrationEvidenceMediaTypeV1), Evidence: evidence,
	}}

	requirements, bundles, issues := ValidatePlan(input, baselines, nil)

	g.Expect(issues).To(BeEmpty())
	g.Expect(requirements).To(Equal([]types.SchemaEvidenceRequirement{{
		ComponentKey: "transaction-api", DatabaseResourceKey: "postgres:transaction",
	}}))
	g.Expect(bundles).To(HaveLen(1))
	g.Expect(bundles[0].MigrationEvidence.Decision).To(Equal(
		types.SchemaDecisionNoMigration,
	))
}

func TestValidatePlanAcceptsExactMigrationBoundEvidence(t *testing.T) {
	g := NewWithT(t)
	report, evidence, input, baselines := schemaEvidenceFixture(t, true)
	contracts := migrationContractsFixture()
	input.ReleasePins[0].MigrationContracts = contracts
	input.Config.SchemaReports = []types.SchemaReportRecord{{
		Object: testEvidenceObject("schema-report", types.SchemaReportMediaTypeV1), Report: report,
	}}
	input.Config.MigrationEvidence = []types.MigrationEvidenceRecord{{
		Object: testEvidenceObject("migration-evidence", types.MigrationEvidenceMediaTypeV1), Evidence: evidence,
	}}

	_, bundles, issues := ValidatePlan(input, baselines, contracts)

	g.Expect(issues).To(BeEmpty())
	g.Expect(bundles).To(HaveLen(1))
	g.Expect(bundles[0].MigrationEvidence.Migrations).To(HaveLen(2))
	g.Expect(bundles[0].MigrationEvidence.MixedVersionEvidence).To(HaveLen(6))
}

func TestValidatePlanDoesNotHideContractBoundaryBehindInstanceBoundary(t *testing.T) {
	g := NewWithT(t)
	report, evidence, input, baselines := schemaEvidenceFixture(t, true)
	contracts := migrationContractsFixture()
	input.ComponentInstances[0].DatabaseBoundary = "postgres:placement"
	input.Config.SchemaReports = []types.SchemaReportRecord{{
		Object: testEvidenceObject("schema-report", types.SchemaReportMediaTypeV1), Report: report,
	}}
	input.Config.MigrationEvidence = []types.MigrationEvidenceRecord{{
		Object: testEvidenceObject("migration-evidence", types.MigrationEvidenceMediaTypeV1), Evidence: evidence,
	}}

	requirements, _, issues := ValidatePlan(input, baselines, contracts)

	g.Expect(requirements).To(Equal([]types.SchemaEvidenceRequirement{
		{ComponentKey: "transaction-api", DatabaseResourceKey: "postgres:placement"},
		{ComponentKey: "transaction-api", DatabaseResourceKey: "postgres:transaction"},
	}))
	g.Expect(issueCodeList(issues)).To(ContainElement("schema_evidence_missing"))
}

func TestValidatePlanRejectsMissingExpiredMismatchedAndIncompleteEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*types.SchemaReport, *types.MigrationEvidence, *types.PlanResolutionInput)
		code   string
	}{
		{
			name: "missing migration evidence",
			mutate: func(_ *types.SchemaReport, _ *types.MigrationEvidence, input *types.PlanResolutionInput) {
				input.Config.MigrationEvidence = nil
			},
			code: "schema_evidence_missing",
		},
		{
			name: "expired report",
			mutate: func(report *types.SchemaReport, evidence *types.MigrationEvidence, _ *types.PlanResolutionInput) {
				report.ExpiresAt = report.IssuedAt.Add(30 * time.Minute)
				evidence.ExpiresAt = report.ExpiresAt
				sealSchemaDocuments(t, report, evidence)
			},
			code: "schema_evidence_expired",
		},
		{
			name: "wrong target scope",
			mutate: func(report *types.SchemaReport, evidence *types.MigrationEvidence, _ *types.PlanResolutionInput) {
				report.Scope.DeploymentTargetID = uuid.New()
				evidence.Scope.DeploymentTargetID = report.Scope.DeploymentTargetID
				sealSchemaDocuments(t, report, evidence)
			},
			code: "schema_evidence_scope_mismatch",
		},
		{
			name: "wrong release",
			mutate: func(report *types.SchemaReport, evidence *types.MigrationEvidence, _ *types.PlanResolutionInput) {
				report.Component.ComponentReleaseID = uuid.New()
				evidence.Component.ComponentReleaseID = report.Component.ComponentReleaseID
				sealSchemaDocuments(t, report, evidence)
			},
			code: "schema_evidence_component_mismatch",
		},
		{
			name: "stale expected current",
			mutate: func(report *types.SchemaReport, evidence *types.MigrationEvidence, _ *types.PlanResolutionInput) {
				evidence.ExpectedCurrent.Checksum = testChecksum("9")
				sealSchemaDocuments(t, report, evidence)
			},
			code: "schema_evidence_expected_current_stale",
		},
		{
			name: "incomplete migration binding",
			mutate: func(_ *types.SchemaReport, evidence *types.MigrationEvidence, _ *types.PlanResolutionInput) {
				evidence.Migrations = evidence.Migrations[:1]
				sealMigrationEvidence(t, evidence)
			},
			code: "schema_evidence_migration_binding_incomplete",
		},
		{
			name: "incomplete mixed version matrix",
			mutate: func(_ *types.SchemaReport, evidence *types.MigrationEvidence, _ *types.PlanResolutionInput) {
				evidence.MixedVersionEvidence = evidence.MixedVersionEvidence[:5]
				sealMigrationEvidence(t, evidence)
			},
			code: "schema_evidence_mixed_version_incomplete",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			report, evidence, input, baselines := schemaEvidenceFixture(t, true)
			contracts := migrationContractsFixture()
			input.ReleasePins[0].MigrationContracts = contracts
			input.Config.SchemaReports = []types.SchemaReportRecord{{
				Object: testEvidenceObject("schema-report", types.SchemaReportMediaTypeV1), Report: report,
			}}
			input.Config.MigrationEvidence = []types.MigrationEvidenceRecord{{
				Object: testEvidenceObject("migration-evidence", types.MigrationEvidenceMediaTypeV1), Evidence: evidence,
			}}
			test.mutate(&report, &evidence, &input)
			if len(input.Config.SchemaReports) > 0 {
				input.Config.SchemaReports[0].Report = report
			}
			if len(input.Config.MigrationEvidence) > 0 {
				input.Config.MigrationEvidence[0].Evidence = evidence
			}

			_, _, issues := ValidatePlan(input, baselines, contracts)

			g.Expect(issueCodeList(issues)).To(ContainElement(test.code))
		})
	}
}

func schemaEvidenceFixture(
	t *testing.T,
	withMigrations bool,
) (types.SchemaReport, types.MigrationEvidence, types.PlanResolutionInput, []types.DeploymentPlanBaseline) {
	t.Helper()
	evaluatedAt := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	scope := types.SchemaEvidenceScope{
		OrganizationID:          uuid.MustParse("10000000-0000-0000-0000-000000000001"),
		DeploymentScopeID:       uuid.MustParse("10000000-0000-0000-0000-000000000002"),
		DeploymentUnitID:        uuid.MustParse("10000000-0000-0000-0000-000000000003"),
		EnvironmentAssignmentID: uuid.MustParse("10000000-0000-0000-0000-000000000004"),
		EnvironmentID:           uuid.MustParse("10000000-0000-0000-0000-000000000005"),
		DeploymentTargetID:      uuid.MustParse("10000000-0000-0000-0000-000000000006"),
		TargetConfigSnapshotID:  uuid.MustParse("10000000-0000-0000-0000-000000000007"),
	}
	component := types.SchemaEvidenceComponent{
		ComponentKey:       "transaction-api",
		ComponentReleaseID: uuid.MustParse("10000000-0000-0000-0000-000000000008"),
		ReleaseChecksum:    testChecksum("a"), Version: "2.0.0",
	}
	current := types.SchemaState{
		ComponentKey: component.ComponentKey, DatabaseResourceKey: "postgres:transaction",
		Version: "40", Checksum: testChecksum("b"),
	}
	report := types.SchemaReport{
		Schema: types.SchemaReportSchemaV1, Scope: scope, Component: component,
		DatabaseResourceKey: current.DatabaseResourceKey, Current: current,
		IssuedAt: evaluatedAt.Add(-time.Hour), ExpiresAt: evaluatedAt.Add(time.Hour),
	}
	evidence := types.MigrationEvidence{
		Schema: types.MigrationEvidenceSchemaV1, Scope: scope, Component: component,
		DatabaseResourceKey: current.DatabaseResourceKey, Decision: types.SchemaDecisionNoMigration,
		ExpectedCurrent: current, IssuedAt: evaluatedAt.Add(-30 * time.Minute),
		ExpiresAt: evaluatedAt.Add(30 * time.Minute),
		MixedVersionEvidence: []types.MixedVersionSchemaEvidence{
			{ApplicationVersion: "1.0.0", SchemaVersion: "40", SchemaChecksum: current.Checksum, Compatible: true},
			{ApplicationVersion: "2.0.0", SchemaVersion: "40", SchemaChecksum: current.Checksum, Compatible: true},
		},
	}
	if withMigrations {
		evidence.Decision = types.SchemaDecisionMigrationBound
		for _, contract := range migrationContractsFixture() {
			evidence.Migrations = append(evidence.Migrations, types.SchemaMigrationBinding{
				MigrationID: contract.ID, ContractChecksum: contract.Checksum,
				ExpectedSourceVersion:   contract.ExpectedSourceVersion,
				ExpectedSourceChecksum:  contract.ExpectedSourceChecksum,
				ResultingVersion:        contract.ResultingVersion,
				ResultingSchemaChecksum: contract.ResultingSchemaChecksum,
			})
		}
		evidence.MixedVersionEvidence = nil
		for _, applicationVersion := range []string{"1.0.0", "2.0.0"} {
			for _, state := range []types.SchemaState{
				current,
				{Version: "41", Checksum: testChecksum("c")},
				{Version: "42", Checksum: testChecksum("d")},
			} {
				evidence.MixedVersionEvidence = append(evidence.MixedVersionEvidence, types.MixedVersionSchemaEvidence{
					ApplicationVersion: applicationVersion, SchemaVersion: state.Version,
					SchemaChecksum: state.Checksum, Compatible: true,
				})
			}
		}
	}
	sealSchemaDocuments(t, &report, &evidence)
	instanceID := uuid.MustParse("10000000-0000-0000-0000-000000000009")
	input := types.PlanResolutionInput{
		EffectiveAt: evaluatedAt,
		Assignment: types.TargetEnvironmentAssignment{
			ID: scope.EnvironmentAssignmentID, EnvironmentID: scope.EnvironmentID,
			DeploymentTargetID: scope.DeploymentTargetID,
		},
		Unit: types.DeploymentUnit{
			ID: scope.DeploymentUnitID, DeploymentScopeID: scope.DeploymentScopeID,
		},
		Config: types.TargetConfigBinding{
			ID: scope.TargetConfigSnapshotID, OrganizationID: scope.OrganizationID,
			ComponentBindings: []types.ConfigComponentBinding{{
				ComponentKey: component.ComponentKey, ComponentInstanceID: instanceID,
			}},
		},
		ReleasePins: []types.ComponentReleasePin{{
			ComponentKey: component.ComponentKey, ComponentReleaseID: component.ComponentReleaseID,
			ReleaseChecksum: component.ReleaseChecksum, Version: component.Version,
		}},
		ComponentInstances: []types.ComponentInstance{{
			ID: instanceID, DatabaseBoundary: current.DatabaseResourceKey,
		}},
	}
	baselines := []types.DeploymentPlanBaseline{{
		ComponentKey: component.ComponentKey, Version: "1.0.0",
		SchemaState: current.Version, SchemaChecksum: current.Checksum,
	}}
	return report, evidence, input, baselines
}

func migrationContractsFixture() []types.MigrationContract {
	return []types.MigrationContract{
		{
			ID: "transaction.041", Checksum: testChecksum("1"),
			ComponentKey: "transaction-api", DatabaseResourceKey: "postgres:transaction",
			ExpectedSourceVersion: "40", ExpectedSourceChecksum: testChecksum("b"),
			ResultingVersion: "41", ResultingSchemaChecksum: testChecksum("c"),
		},
		{
			ID: "transaction.042", Checksum: testChecksum("2"),
			ComponentKey: "transaction-api", DatabaseResourceKey: "postgres:transaction",
			ExpectedSourceVersion: "41", ExpectedSourceChecksum: testChecksum("c"),
			ResultingVersion: "42", ResultingSchemaChecksum: testChecksum("d"),
			DependsOn: []string{"transaction.041"},
		},
	}
}

func sealSchemaDocuments(
	t *testing.T,
	report *types.SchemaReport,
	evidence *types.MigrationEvidence,
) {
	t.Helper()
	checksum, err := SchemaReportChecksum(*report)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	report.Checksum = checksum
	evidence.SchemaReportChecksum = checksum
	sealMigrationEvidence(t, evidence)
}

func sealMigrationEvidence(t *testing.T, evidence *types.MigrationEvidence) {
	t.Helper()
	checksum, err := MigrationEvidenceChecksum(*evidence)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	evidence.Checksum = checksum
}

func issueCodeList(issues []types.ValidationIssue) []string {
	result := make([]string, 0, len(issues))
	for _, issue := range issues {
		result = append(result, issue.Code)
	}
	return result
}

func testChecksum(character string) string {
	return "sha256:" + strings.Repeat(character, 64)
}

func testEvidenceObject(key, mediaType string) types.SchemaEvidenceObject {
	return types.SchemaEvidenceObject{
		ObjectKey: key, Reference: "s3://config-bucket/immutable/" + key,
		MediaType: mediaType, SizeBytes: 100, Checksum: testChecksum("e"),
	}
}
