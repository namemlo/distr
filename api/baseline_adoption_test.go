package api

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCreateBaselineAdoptionRequestRequiresExactEvidence(t *testing.T) {
	g := NewWithT(t)
	request := validBaselineAdoptionRequest()
	g.Expect(request.Validate()).To(Succeed())
	g.Expect(request.Components[0].ComponentKey).To(Equal("api"))

	request.Components[0].ObservationID = uuid.Nil
	g.Expect(request.Validate()).To(MatchError(ContainSubstring("observationId")))

	request = validBaselineAdoptionRequest()
	request.Components[0].SourceCommit = strings.Repeat("g", 40)
	g.Expect(request.Validate()).To(MatchError(ContainSubstring("sourceCommit")))

	request = validBaselineAdoptionRequest()
	request.Components = append(request.Components, request.Components[0])
	g.Expect(request.Validate()).To(MatchError(ContainSubstring("duplicate")))
}

func TestCreateBaselineAdoptionRequestRejectsSyntheticOrIncompleteMaterial(t *testing.T) {
	g := NewWithT(t)
	request := validBaselineAdoptionRequest()
	request.ExpectedTargetConfigChecksum = "sha256:not-a-checksum"
	g.Expect(request.Validate()).To(MatchError(ContainSubstring("expectedTargetConfigChecksum")))

	request = validBaselineAdoptionRequest()
	request.IdempotencyKey = "not url safe"
	g.Expect(request.Validate()).To(MatchError(ContainSubstring("idempotencyKey")))

	request = validBaselineAdoptionRequest()
	request.Components[0].Platform = "latest"
	g.Expect(request.Validate()).To(MatchError(ContainSubstring("platform")))

	request = validBaselineAdoptionRequest()
	request.Reason = "manual\ndatabase edit"
	g.Expect(request.Validate()).To(MatchError(ContainSubstring("reason")))
}

func validBaselineAdoptionRequest() CreateBaselineAdoptionRequest {
	return CreateBaselineAdoptionRequest{
		IdempotencyKey:                 "baseline:2026-08-31",
		Reason:                         "Adopt independently observed healthy runtime",
		ExpectedPlanChecksum:           baselineAdoptionTestChecksum("1"),
		ExpectedProductReleaseChecksum: baselineAdoptionTestChecksum("2"),
		ExpectedTargetConfigChecksum:   baselineAdoptionTestChecksum("3"),
		Components: []BaselineAdoptionComponentRequest{{
			ComponentInstanceID:             uuid.New(),
			ComponentKey:                    " api ",
			ComponentReleaseID:              uuid.New(),
			ComponentReleaseChecksum:        baselineAdoptionTestChecksum("4"),
			SourceCommit:                    strings.Repeat("a", 40),
			BuildID:                         "build-42",
			ProvenanceVerificationID:        uuid.New(),
			ProvenanceEvidenceDigest:        baselineAdoptionTestChecksum("5"),
			ProvenancePolicyChecksum:        baselineAdoptionTestChecksum("6"),
			ArtifactDigest:                  baselineAdoptionTestChecksum("7"),
			Platform:                        "linux/amd64",
			ConfigChecksum:                  baselineAdoptionTestChecksum("3"),
			SchemaVersion:                   "2026.08.31",
			CapabilityChecksum:              baselineAdoptionTestChecksum("4"),
			TopologyChecksum:                baselineAdoptionTestChecksum("8"),
			ObservationID:                   uuid.New(),
			ObserverID:                      uuid.New(),
			ObservationEvidenceChecksum:     baselineAdoptionTestChecksum("9"),
			ObservationStateChecksum:        baselineAdoptionTestChecksum("a"),
			ObservationRuntimeStateChecksum: baselineAdoptionTestChecksum("b"),
		}},
	}
}

func baselineAdoptionTestChecksum(seed string) string {
	return "sha256:" + strings.Repeat(seed, 64)
}
