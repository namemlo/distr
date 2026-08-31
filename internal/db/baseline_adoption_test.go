package db

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	. "github.com/onsi/gomega"
)

func TestBaselineAdoptionConcurrentReplayRecognizesRetryableWriteConflicts(t *testing.T) {
	g := NewWithT(t)
	for _, code := range []string{pgerrcode.UniqueViolation, pgerrcode.SerializationFailure} {
		g.Expect(baselineAdoptionConcurrentWriteConflict(&pgconn.PgError{Code: code})).To(BeTrue())
	}
	g.Expect(baselineAdoptionConcurrentWriteConflict(
		&pgconn.PgError{Code: pgerrcode.CheckViolation},
	)).To(BeFalse())
}

func TestMigration166CreatesImmutableNativeBaselineAdoption(t *testing.T) {
	g := NewWithT(t)
	root := filepath.Join("..", "migrations", "sql")
	up, err := os.ReadFile(filepath.Join(root, "166_native_baseline_adoption.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	down, err := os.ReadFile(filepath.Join(root, "166_native_baseline_adoption.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	upText, downText := string(up), string(down)

	for _, fact := range []string{
		"CREATE TABLE BaselineAdoption",
		"CREATE TABLE BaselineAdoptionComponent",
		"source_kind",
		"BASELINE_ADOPTION",
		"baseline_adoption_component_id",
		"baseline adoption cannot coexist with deployment tasks or executions",
		"baseline_adoption_commit_guard",
		"ControlPlaneAuditEvent",
		"LEGACY_LIVENESS_ONLY",
		"BASELINE_OR_ROLLBACK_ONLY",
		"observation_evidence_reference",
		"ObservedComponentState_health_evidence_immutable",
		"observedcomponentstate_health_evidence_shape",
		"evidence://sha256/",
		"baseline_adoption.adopted",
		"event.outcome = 'ADOPTED'",
	} {
		g.Expect(upText).To(ContainSubstring(fact))
	}
	g.Expect(upText).To(ContainSubstring("deployment_performed = FALSE"))
	g.Expect(upText).To(ContainSubstring("task_count = 0"))
	g.Expect(upText).To(ContainSubstring("lock_count = 0"))
	g.Expect(upText).To(ContainSubstring("execution_count = 0"))
	g.Expect(upText).To(ContainSubstring("substring(evidence_checksum FROM 8)"))
	g.Expect(downText).To(ContainSubstring(
		"refusing migration 166 rollback: native baseline or observation health evidence exists",
	))
	g.Expect(upText).To(ContainSubstring(
		"BEFORE INSERT OR UPDATE OF deployment_plan_id, organization_id ON Task",
	))
	g.Expect(upText).To(ContainSubstring(
		"BEFORE INSERT OR UPDATE OF deployment_plan_id, organization_id ON ExternalExecution",
	))
	guardStart := strings.Index(upText, "CREATE FUNCTION baseline_adoption_execution_exclusion_guard()")
	g.Expect(guardStart).To(BeNumerically(">=", 0))
	guardEnd := strings.Index(upText[guardStart:], "CREATE TRIGGER Task_baseline_adoption_exclusion")
	g.Expect(guardEnd).To(BeNumerically(">", 0))
	guardText := upText[guardStart : guardStart+guardEnd]
	g.Expect(guardText).To(ContainSubstring("FROM DeploymentPlan"))
	g.Expect(guardText).To(ContainSubstring("FOR UPDATE"))
}

func TestBaselineAdoptionCanonicalMaterialIsOrderStableAndEvidenceBound(t *testing.T) {
	g := NewWithT(t)
	input := baselineAdoptionCanonicalTestInput()
	firstPayload, firstChecksum, err := canonicalizeBaselineAdoptionInput(input)
	g.Expect(err).NotTo(HaveOccurred())

	input.Components[0], input.Components[1] = input.Components[1], input.Components[0]
	secondPayload, secondChecksum, err := canonicalizeBaselineAdoptionInput(input)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(secondPayload).To(Equal(firstPayload))
	g.Expect(secondChecksum).To(Equal(firstChecksum))

	input.Components[0].ObservationRuntimeStateChecksum = baselineAdoptionDBTestChecksum("f")
	_, changedChecksum, err := canonicalizeBaselineAdoptionInput(input)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(changedChecksum).NotTo(Equal(firstChecksum))
}

func TestBaselineAdoptionReplayRequiresIdenticalImmutableMaterial(t *testing.T) {
	g := NewWithT(t)
	input := baselineAdoptionCanonicalTestInput()
	_, requestChecksum, err := canonicalizeBaselineAdoptionInput(input)
	g.Expect(err).NotTo(HaveOccurred())
	existing := types.BaselineAdoption{
		OrganizationID: input.OrganizationID, DeploymentPlanID: input.DeploymentPlanID,
		RequestChecksum: requestChecksum,
	}
	g.Expect(baselineAdoptionReplayMatches(existing, input, requestChecksum)).To(BeTrue())

	changed := input
	changed.Components = slices.Clone(input.Components)
	changed.Components[0].ObservationEvidenceChecksum = baselineAdoptionDBTestChecksum("f")
	_, changedChecksum, err := canonicalizeBaselineAdoptionInput(changed)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(baselineAdoptionReplayMatches(existing, changed, changedChecksum)).To(BeFalse())
}

func TestBaselineAdoptionRepositoryHasNoSyntheticExecutionPath(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("baseline_adoption.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)

	g.Expect(text).To(ContainSubstring("RecordControlPlaneAuditMutation"))
	g.Expect(text).To(ContainSubstring("FOR UPDATE"))
	g.Expect(text).To(ContainSubstring("executor_outcome = ''"))
	g.Expect(text).To(ContainSubstring("source_kind"))
	g.Expect(text).To(ContainSubstring("BASELINE_ADOPTION"))
	g.Expect(text).NotTo(ContainSubstring("INSERT INTO Task"))
	g.Expect(text).NotTo(ContainSubstring("INSERT INTO ExecutionAttempt"))
	g.Expect(text).NotTo(ContainSubstring("INSERT INTO ExternalExecution"))
	g.Expect(text).NotTo(ContainSubstring("INSERT INTO TaskResourceLock"))
	g.Expect(text).To(ContainSubstring("baseline_adoption.adopted"))
	g.Expect(text).To(ContainSubstring(`Outcome:                "ADOPTED"`))
	g.Expect(text).NotTo(ContainSubstring("DEPLOYED"))
	g.Expect(text).To(ContainSubstring("baselineAdoptionConcurrentWriteConflict"))
	g.Expect(text).To(ContainSubstring(
		"case replayErr == nil && baselineAdoptionReplayMatches(",
	))
	g.Expect(strings.Count(text, "getBaselineAdoptionByIdempotencyKey(")).To(
		BeNumerically(">=", 3),
	)
	g.Expect(text).NotTo(ContainSubstring("input.HealthEvidenceKind"))
}

func TestBaselineAdoptionRequiresExactCurrentDesiredObservedLineage(t *testing.T) {
	g := NewWithT(t)
	repository, err := os.ReadFile("baseline_adoption.go")
	g.Expect(err).NotTo(HaveOccurred())
	migration, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "166_native_baseline_adoption.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	text := string(repository) + string(migration)

	for _, fact := range []string{
		"ComponentObservationHead",
		"observation.is_current",
		"observation.fresh_until >=",
		"observation.artifact_digest = component.artifact_digest",
		"observation.config_checksum = component.config_checksum",
		"observation.platform = component.platform",
		"active.artifact_digest = component.artifact_digest",
		"active.config_checksum = component.config_checksum",
		"active.platform = component.platform",
		"componentReleasePins",
		"componentBindings",
		"BASELINE_OR_ROLLBACK_ONLY",
		"source_kind = 'EXECUTION'",
		"health_evidence_kind = 'STANDARD_READINESS'",
		"observation.health_evidence_kind",
		"observation.health_policy_checksum",
		"evidence://sha256/",
	} {
		g.Expect(text).To(ContainSubstring(fact))
	}
}

func TestActiveDesiredRevisionOutputIncludesAdoptionSourceLineage(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("desired_observed_state.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)

	for _, field := range []string{
		"a.source_kind",
		"a.baseline_adoption_component_id",
		"a.health_evidence_kind",
		"a.health_evidence_use",
	} {
		g.Expect(text).To(ContainSubstring(field))
	}
}

func TestLegacyLivenessBaselineIsExcludedFromPromotionProviderDiscovery(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("deployment_plan_drafts.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)

	g.Expect(text).To(ContainSubstring(
		"active.health_evidence_kind = 'STANDARD_READINESS'",
	))
	g.Expect(text).To(ContainSubstring(
		"active.health_evidence_use = 'STANDARD_PROMOTION_ELIGIBLE'",
	))
}

func baselineAdoptionCanonicalTestInput() types.CreateBaselineAdoptionInput {
	component := func(key, seed string) types.BaselineAdoptionComponentInput {
		return types.BaselineAdoptionComponentInput{
			ComponentInstanceID:             uuid.New(),
			ComponentKey:                    key,
			ComponentReleaseID:              uuid.New(),
			ComponentReleaseChecksum:        baselineAdoptionDBTestChecksum(seed),
			SourceCommit:                    strings.Repeat(seed, 40),
			BuildID:                         "build-" + seed,
			ProvenanceVerificationID:        uuid.New(),
			ProvenanceEvidenceDigest:        baselineAdoptionDBTestChecksum(seed),
			ProvenancePolicyChecksum:        baselineAdoptionDBTestChecksum(seed),
			ArtifactDigest:                  baselineAdoptionDBTestChecksum(seed),
			Platform:                        "linux/amd64",
			ConfigChecksum:                  baselineAdoptionDBTestChecksum("c"),
			SchemaVersion:                   "1." + seed,
			CapabilityChecksum:              baselineAdoptionDBTestChecksum(seed),
			TopologyChecksum:                baselineAdoptionDBTestChecksum(seed),
			ObservationID:                   uuid.New(),
			ObserverID:                      uuid.New(),
			ObservationEvidenceChecksum:     baselineAdoptionDBTestChecksum(seed),
			ObservationStateChecksum:        baselineAdoptionDBTestChecksum(seed),
			ObservationRuntimeStateChecksum: baselineAdoptionDBTestChecksum(seed),
		}
	}
	return types.CreateBaselineAdoptionInput{
		OrganizationID:                 uuid.New(),
		DeploymentPlanID:               uuid.New(),
		ActorUserAccountID:             uuid.New(),
		IdempotencyKey:                 "baseline-1",
		Reason:                         "adopt healthy runtime",
		ExpectedPlanChecksum:           baselineAdoptionDBTestChecksum("a"),
		ExpectedProductReleaseChecksum: baselineAdoptionDBTestChecksum("b"),
		ExpectedTargetConfigChecksum:   baselineAdoptionDBTestChecksum("c"),
		Components: []types.BaselineAdoptionComponentInput{
			component("worker", "2"), component("api", "1"),
		},
	}
}

func baselineAdoptionDBTestChecksum(seed string) string {
	return "sha256:" + strings.Repeat(seed, 64)
}
