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

func validExecutionRuntimeEvidenceInput() types.ExecutionRuntimeEvidenceInput {
	checksum := func(value string) string { return "sha256:" + strings.Repeat(value, 64) }
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
