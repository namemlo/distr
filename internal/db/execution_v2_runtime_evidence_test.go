package db

import (
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestExecutionRuntimeEvidenceValidationAndCanonicalBinding(t *testing.T) {
	g := NewWithT(t)
	input := validExecutionRuntimeEvidenceInput()
	g.Expect(validateExecutionRuntimeEvidenceInput(input)).To(Succeed())

	attempt := types.ExecutionAttempt{
		ID: input.AttemptID,
		Identity: types.ExecutionIdentity{
			ExecutionID: uuid.New(), AttemptNumber: 2, StepKey: "deploy-api",
		},
	}
	checksum, err := executionRuntimeEvidenceCanonicalChecksum(attempt, input)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(checksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
	replayChecksum, err := executionRuntimeEvidenceCanonicalChecksum(attempt, input)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(replayChecksum).To(Equal(checksum))

	tampered := input
	tampered.ResultImageDigest = "sha256:" + strings.Repeat("9", 64)
	tamperedChecksum, err := executionRuntimeEvidenceCanonicalChecksum(attempt, tampered)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(tamperedChecksum).NotTo(Equal(checksum))

	invalidHealth := input
	invalidHealth.HealthStatus = types.TargetComponentHealthUnknown
	g.Expect(validateExecutionRuntimeEvidenceInput(invalidHealth)).
		To(MatchError(ContainSubstring("result is invalid")))

	invalidCaller := input
	invalidCaller.CallerIdentity = "caller\nforged"
	g.Expect(validateExecutionRuntimeEvidenceInput(invalidCaller)).
		To(MatchError(ContainSubstring("caller or audience")))
}

func TestExecutionRuntimeEvidenceV2BindsPhysicalServiceConfigChecksums(t *testing.T) {
	g := NewWithT(t)
	input := validExecutionRuntimeEvidenceInput()
	input.SchemaVersion = types.ExecutionRuntimeEvidenceSchemaV2
	input.PreExecutionServiceConfigChecksum = checksum("9")
	input.ResultServiceConfigChecksum = checksum("a")
	g.Expect(validateExecutionRuntimeEvidenceInput(input)).To(Succeed())

	attempt := types.ExecutionAttempt{
		ID:       input.AttemptID,
		Identity: types.ExecutionIdentity{ExecutionID: uuid.New(), AttemptNumber: 1, StepKey: "deploy-api"},
	}
	original, err := executionRuntimeEvidenceCanonicalChecksum(attempt, input)
	g.Expect(err).NotTo(HaveOccurred())
	tampered := input
	tampered.ResultServiceConfigChecksum = checksum("b")
	changed, err := executionRuntimeEvidenceCanonicalChecksum(attempt, tampered)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(changed).NotTo(Equal(original))

	invalidV2 := input
	invalidV2.ResultServiceConfigChecksum = ""
	g.Expect(validateExecutionRuntimeEvidenceInput(invalidV2)).
		To(MatchError(ContainSubstring("service config checksum")))

	invalidV1 := validExecutionRuntimeEvidenceInput()
	invalidV1.PreExecutionServiceConfigChecksum = checksum("9")
	g.Expect(validateExecutionRuntimeEvidenceInput(invalidV1)).
		To(MatchError(ContainSubstring("v1 service config checksums must be empty")))

	g.Expect(runtimeEvidenceSchemaMatchesAttempt(
		types.ExecutionRuntimeEvidenceSchemaV1, types.ExecutionRuntimeContractVersionV3,
	)).To(BeTrue())
	g.Expect(runtimeEvidenceSchemaMatchesAttempt(
		types.ExecutionRuntimeEvidenceSchemaV2, types.ExecutionRuntimeContractVersionV4,
	)).To(BeTrue())
	g.Expect(runtimeEvidenceSchemaMatchesAttempt(
		types.ExecutionRuntimeEvidenceSchemaV1, types.ExecutionRuntimeContractVersionV4,
	)).To(BeFalse())
}

func validExecutionRuntimeEvidenceInput() types.ExecutionRuntimeEvidenceInput {
	return types.ExecutionRuntimeEvidenceInput{
		OrganizationID: uuid.New(), DeploymentTargetID: uuid.New(), AttemptID: uuid.New(),
		EventIdentity: uuid.New(), SchemaVersion: types.ExecutionRuntimeEvidenceSchemaV1,
		IntentChecksum: checksum("1"), ExecutorID: "executor-a",
		CallerIdentity: "urn:distr:caller:deployment-target:test",
		Audience:       "urn:distr:audience:adapter-assignment:test", FenceGeneration: 3,
		ExpectedObservedStateVersion:  7,
		ExpectedObservedStateChecksum: checksum("2"),
		PreExecutionImageDigest:       checksum("3"), PreExecutionConfigChecksum: checksum("4"),
		ResultImageDigest: checksum("5"), ResultConfigChecksum: checksum("6"),
		Platform:       types.DeploymentTargetPlatformLinuxAMD64,
		HealthStatus:   types.TargetComponentHealthHealthy,
		ResultChecksum: checksum("7"), EvidenceReference: "jenkins://job/42/runtime-proof.json",
		EvidenceChecksum: checksum("8"), CapturedAt: time.Now().UTC(),
	}
}

func checksum(value string) string {
	return "sha256:" + strings.Repeat(value, 64)
}
