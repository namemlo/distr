package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/planning"
	"github.com/distr-sh/distr/internal/schemaevidence"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestLoadTargetPlanSchemaEvidenceParsesVerifiedAdapterInputs(t *testing.T) {
	g := NewWithT(t)
	canonical, reportPayload, evidencePayload, _ := schemaGateTestCanonical(t)
	reportObject := targetPlanSchemaEvidenceObject(
		"schema-report",
		types.SchemaReportMediaTypeV1,
		reportPayload,
	)
	evidenceObject := targetPlanSchemaEvidenceObject(
		"migration-evidence",
		types.MigrationEvidenceMediaTypeV1,
		evidencePayload,
	)
	reader := schemaEvidenceReaderVerifier{payloads: map[string][]byte{
		reportObject.Key: reportPayload, evidenceObject.Key: evidencePayload,
	}}

	reports, evidence, issues := loadTargetPlanSchemaEvidence(
		t.Context(),
		reader,
		[]types.TargetPlanConfigObject{evidenceObject, reportObject},
	)

	g.Expect(issues).To(BeEmpty())
	g.Expect(reports).To(HaveLen(1))
	g.Expect(evidence).To(HaveLen(1))
	g.Expect(reports[0].Report).To(Equal(canonical.SchemaEvidence[0].SchemaReport))
	g.Expect(evidence[0].Evidence).To(Equal(canonical.SchemaEvidence[0].MigrationEvidence))
}

func TestLoadTargetPlanSchemaEvidenceFailsClosedWithoutReader(t *testing.T) {
	g := NewWithT(t)
	canonical, reportPayload, evidencePayload, organizationID := schemaGateTestCanonical(t)
	g.Expect(canonical.Schema).To(Equal(types.TargetDeploymentPlanSchemaV2))
	g.Expect(evidencePayload).NotTo(BeEmpty())
	g.Expect(organizationID).NotTo(Equal(uuid.Nil))
	object := targetPlanSchemaEvidenceObject(
		"schema-report",
		types.SchemaReportMediaTypeV1,
		reportPayload,
	)
	verifier := targetPlanConfigVerifierFunc(func(
		_ context.Context,
		object types.TargetPlanConfigObject,
	) (types.TargetPlanConfigObservation, error) {
		return observationForSchemaEvidenceObject(object), nil
	})

	reports, evidence, issues := loadTargetPlanSchemaEvidence(
		t.Context(),
		verifier,
		[]types.TargetPlanConfigObject{object},
	)

	g.Expect(reports).To(BeEmpty())
	g.Expect(evidence).To(BeEmpty())
	g.Expect(issues).To(HaveLen(1))
	g.Expect(issues[0].Code).To(Equal("schema_evidence_unavailable"))
}

func TestCurrentDeploymentPlanSchemaEvidenceGateRejectsExpiredEvidence(t *testing.T) {
	g := NewWithT(t)
	canonical, _, _, organizationID := schemaGateTestCanonical(t)
	payload, checksum, err := planning.CanonicalizeTargetDeploymentPlan(canonical)
	g.Expect(err).NotTo(HaveOccurred())
	plan := types.DeploymentPlan{
		OrganizationID: organizationID, PlanSchema: types.TargetDeploymentPlanSchemaV2,
		CanonicalPayload: payload, CanonicalChecksum: checksum,
	}
	evaluatedAt := canonical.SchemaEvidence[0].SchemaReport.IssuedAt.Add(30 * time.Minute)

	g.Expect(requireCurrentDeploymentPlanSchemaEvidence(plan, evaluatedAt)).To(Succeed())
	err = requireCurrentDeploymentPlanSchemaEvidence(
		plan,
		canonical.SchemaEvidence[0].SchemaReport.ExpiresAt,
	)
	g.Expect(err).To(MatchError(ContainSubstring("schema_evidence_expired")))

	g.Expect(hydrateDeploymentPlanSchemaEvidence(&plan)).To(Succeed())
	g.Expect(plan.SchemaEvidence).To(Equal(canonical.SchemaEvidence))
}

func TestSchemaEvidenceGatesRunBeforePublicationAdmissionAndExecutionMutation(t *testing.T) {
	const gate = "requireCurrentDeploymentPlanSchemaEvidence"
	tests := []struct {
		file     string
		gate     string
		mutation string
	}{
		{
			file: "deployment_plan_drafts.go", gate: gate,
			mutation: "insertPublishedTargetPlan(ctx, plan)",
		},
		{
			file: "admission.go", gate: gate,
			mutation: "prepareAdmissionGateEvidenceBeforeEvaluation",
		},
		{
			file: "task_queue.go", gate: gate,
			mutation: "lockTaskResourceAdvisoryGroups",
		},
	}
	for _, test := range tests {
		t.Run(test.file, func(t *testing.T) {
			content, err := os.ReadFile(test.file)
			NewWithT(t).Expect(err).NotTo(HaveOccurred())
			text := string(content)
			gateIndex := strings.Index(text, test.gate)
			mutationIndex := strings.Index(text, test.mutation)
			NewWithT(t).Expect(gateIndex).To(BeNumerically(">=", 0))
			NewWithT(t).Expect(mutationIndex).To(BeNumerically(">", gateIndex))
		})
	}
}

type schemaEvidenceReaderVerifier struct {
	payloads map[string][]byte
}

func (verifier schemaEvidenceReaderVerifier) VerifyTargetConfigObject(
	_ context.Context,
	object types.TargetPlanConfigObject,
) (types.TargetPlanConfigObservation, error) {
	return observationForSchemaEvidenceObject(object), nil
}

func (verifier schemaEvidenceReaderVerifier) ReadTargetConfigObject(
	_ context.Context,
	object types.TargetPlanConfigObject,
	maxBytes int64,
) (types.TargetPlanConfigObservation, []byte, error) {
	payload := verifier.payloads[object.Key]
	if int64(len(payload)) > maxBytes {
		return types.TargetPlanConfigObservation{}, nil, context.DeadlineExceeded
	}
	return observationForSchemaEvidenceObject(object), payload, nil
}

func schemaGateTestCanonical(
	t *testing.T,
) (types.TargetDeploymentPlanCanonical, []byte, []byte, uuid.UUID) {
	t.Helper()
	g := NewWithT(t)
	organizationID := uuid.MustParse("20000000-0000-0000-0000-000000000001")
	scope := types.SchemaEvidenceScope{
		OrganizationID:          organizationID,
		DeploymentScopeID:       uuid.MustParse("20000000-0000-0000-0000-000000000002"),
		DeploymentUnitID:        uuid.MustParse("20000000-0000-0000-0000-000000000003"),
		EnvironmentAssignmentID: uuid.MustParse("20000000-0000-0000-0000-000000000004"),
		EnvironmentID:           uuid.MustParse("20000000-0000-0000-0000-000000000005"),
		DeploymentTargetID:      uuid.MustParse("20000000-0000-0000-0000-000000000006"),
		TargetConfigSnapshotID:  uuid.MustParse("20000000-0000-0000-0000-000000000007"),
	}
	component := types.SchemaEvidenceComponent{
		ComponentKey:       "transaction-api",
		ComponentReleaseID: uuid.MustParse("20000000-0000-0000-0000-000000000008"),
		ReleaseChecksum:    checksumForDraftTest("a"), Version: "2.0.0",
	}
	current := types.SchemaState{
		ComponentKey: component.ComponentKey, DatabaseResourceKey: "postgres:transaction",
		Version: "40", Checksum: checksumForDraftTest("b"),
	}
	issuedAt := time.Date(2026, time.September, 1, 11, 0, 0, 0, time.UTC)
	report := types.SchemaReport{
		Schema: types.SchemaReportSchemaV1, Scope: scope, Component: component,
		DatabaseResourceKey: current.DatabaseResourceKey, Current: current,
		IssuedAt: issuedAt, ExpiresAt: issuedAt.Add(time.Hour),
	}
	report.Checksum = mustSchemaReportChecksum(t, report)
	evidence := types.MigrationEvidence{
		Schema: types.MigrationEvidenceSchemaV1, Scope: scope, Component: component,
		DatabaseResourceKey:  current.DatabaseResourceKey,
		SchemaReportChecksum: report.Checksum,
		Decision:             types.SchemaDecisionNoMigration, ExpectedCurrent: current,
		IssuedAt: issuedAt.Add(time.Minute), ExpiresAt: issuedAt.Add(59 * time.Minute),
		MixedVersionEvidence: []types.MixedVersionSchemaEvidence{
			{ApplicationVersion: "1.0.0", SchemaVersion: "40", SchemaChecksum: current.Checksum, Compatible: true},
			{ApplicationVersion: "2.0.0", SchemaVersion: "40", SchemaChecksum: current.Checksum, Compatible: true},
		},
	}
	evidence.Checksum = mustMigrationEvidenceChecksum(t, evidence)
	reportPayload, err := json.Marshal(report)
	g.Expect(err).NotTo(HaveOccurred())
	evidencePayload, err := json.Marshal(evidence)
	g.Expect(err).NotTo(HaveOccurred())
	reportObject := schemaEvidenceObjectBinding(
		"schema-report",
		types.SchemaReportMediaTypeV1,
		reportPayload,
	)
	evidenceObject := schemaEvidenceObjectBinding(
		"migration-evidence",
		types.MigrationEvidenceMediaTypeV1,
		evidencePayload,
	)
	requirement := types.SchemaEvidenceRequirement{
		ComponentKey: component.ComponentKey, DatabaseResourceKey: current.DatabaseResourceKey,
	}
	canonical := types.TargetDeploymentPlanCanonical{
		Schema:           types.TargetDeploymentPlanSchemaV2,
		DeploymentUnitID: scope.DeploymentUnitID, DeploymentScopeID: scope.DeploymentScopeID,
		EnvironmentAssignmentID: scope.EnvironmentAssignmentID, EnvironmentID: scope.EnvironmentID,
		DeploymentTargetID:     scope.DeploymentTargetID,
		TargetConfigSnapshotID: scope.TargetConfigSnapshotID,
		ComponentReleasePins: []types.ComponentReleasePin{{
			ComponentKey: component.ComponentKey, ComponentReleaseID: component.ComponentReleaseID,
			ReleaseChecksum: component.ReleaseChecksum, Version: component.Version,
		}},
		Baselines: []types.DeploymentPlanBaseline{{
			ComponentKey: component.ComponentKey, Version: "1.0.0",
			SchemaState: current.Version, SchemaChecksum: current.Checksum,
		}},
		SchemaEvidenceRequirements: []types.SchemaEvidenceRequirement{requirement},
		SchemaEvidence: []types.SchemaEvidenceBundle{{
			Requirement: requirement, SchemaReportObject: reportObject,
			MigrationEvidenceObject: evidenceObject,
			SchemaReport:            report, MigrationEvidence: evidence,
		}},
	}
	return canonical, reportPayload, evidencePayload, organizationID
}

func targetPlanSchemaEvidenceObject(
	key, mediaType string,
	payload []byte,
) types.TargetPlanConfigObject {
	binding := schemaEvidenceObjectBinding(key, mediaType, payload)
	return types.TargetPlanConfigObject{
		Key: binding.ObjectKey, Kind: types.TargetConfigObjectKindAdapterInput,
		Reference: binding.Reference, VersionID: binding.VersionID,
		MediaType: binding.MediaType, SizeBytes: binding.SizeBytes, Checksum: binding.Checksum,
	}
}

func schemaEvidenceObjectBinding(
	key, mediaType string,
	payload []byte,
) types.SchemaEvidenceObject {
	sum := sha256.Sum256(payload)
	return types.SchemaEvidenceObject{
		ObjectKey: key, Reference: "s3://config-bucket/immutable/" + key,
		VersionID: "version-1", MediaType: mediaType, SizeBytes: int64(len(payload)),
		Checksum: "sha256:" + hex.EncodeToString(sum[:]),
	}
}

func observationForSchemaEvidenceObject(
	object types.TargetPlanConfigObject,
) types.TargetPlanConfigObservation {
	return types.TargetPlanConfigObservation{
		Reference: object.Reference, VersionID: object.VersionID,
		MediaType: object.MediaType, SizeBytes: object.SizeBytes, Checksum: object.Checksum,
	}
}

func mustSchemaReportChecksum(t *testing.T, report types.SchemaReport) string {
	t.Helper()
	checksum, err := schemaevidence.SchemaReportChecksum(report)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return checksum
}

func mustMigrationEvidenceChecksum(t *testing.T, evidence types.MigrationEvidence) string {
	t.Helper()
	checksum, err := schemaevidence.MigrationEvidenceChecksum(evidence)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return checksum
}
