package planning

import (
	"context"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestSelectVerifiedBaselinePinsNewestHealthyObservationForActiveDesiredRevision(t *testing.T) {
	g := NewWithT(t)
	now := time.Now().UTC()
	componentInstanceID := uuid.New()
	desiredChecksum := testChecksum("a")
	newestObservationID := uuid.New()
	query := types.BaselineQuery{
		OrganizationID:          uuid.New(),
		DeploymentUnitID:        uuid.New(),
		ComponentInstanceID:     componentInstanceID,
		ComponentKey:            "loyalty-api",
		ExpectedDesiredRevision: 9,
		ExpectedDesiredChecksum: desiredChecksum,
		Candidates: []types.BaselineCandidate{
			{
				ObservationID: newestObservationID, ObservedAt: now.Add(2 * time.Minute),
				Health:          types.TargetComponentHealthHealthy,
				DesiredRevision: 9, DesiredChecksum: desiredChecksum,
				ObservedRevision: 9, ObservedChecksum: desiredChecksum,
				PlanSchema:      types.LegacyDeploymentPlanSchemaV1,
				ReleaseBundleID: uuid.New(), Version: "1.8.0",
				Image:          "registry.example/loyalty@" + testChecksum("b"),
				Platform:       "linux/amd64",
				ConfigChecksum: testChecksum("c"),
			},
			{
				ObservationID: uuid.New(), ObservedAt: now.Add(time.Minute),
				Health:          types.TargetComponentHealthUnhealthy,
				DesiredRevision: 9, DesiredChecksum: desiredChecksum,
				ObservedRevision: 9, ObservedChecksum: desiredChecksum,
				PlanSchema: types.TargetDeploymentPlanSchemaV2,
			},
			{
				ObservationID: uuid.New(), ObservedAt: now.Add(3 * time.Minute),
				Health:          types.TargetComponentHealthHealthy,
				DesiredRevision: 8, DesiredChecksum: testChecksum("d"),
				ObservedRevision: 8, ObservedChecksum: testChecksum("d"),
				PlanSchema: types.TargetDeploymentPlanSchemaV2,
			},
		},
	}

	baseline, err := SelectVerifiedBaseline(context.Background(), query)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(baseline).NotTo(BeNil())
	g.Expect(baseline.ObservationID).To(Equal(&newestObservationID))
	g.Expect(baseline.DesiredRevision).To(Equal(int64(9)))
	g.Expect(baseline.ObservationChecksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
	g.Expect(baseline.Projection).To(Equal(types.BaselineProjectionLegacy))
	g.Expect(baseline.AuthorizesV2Execution).To(BeFalse())
	g.Expect(baseline.Bootstrap).To(BeFalse())
}

func TestSelectVerifiedBaselineReturnsBootstrapWithoutCandidate(t *testing.T) {
	g := NewWithT(t)
	query := types.BaselineQuery{
		OrganizationID: uuid.New(), DeploymentUnitID: uuid.New(),
		ComponentInstanceID: uuid.New(), ComponentKey: "new-worker",
		ExpectedDesiredRevision: 1, ExpectedDesiredChecksum: testChecksum("e"),
	}

	baseline, err := SelectVerifiedBaseline(context.Background(), query)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(baseline.Bootstrap).To(BeTrue())
	g.Expect(baseline.Projection).To(Equal(types.BaselineProjectionBootstrap))
	g.Expect(baseline.ObservationID).To(BeNil())
	g.Expect(baseline.AuthorizesV2Execution).To(BeFalse())
}

func TestSelectVerifiedBaselineAuthorizesOnlyExactV2PlanFacts(t *testing.T) {
	g := NewWithT(t)
	sourcePlanID := uuid.New()
	executionID := uuid.New()
	desiredChecksum := testChecksum("f")
	query := types.BaselineQuery{
		OrganizationID: uuid.New(), DeploymentUnitID: uuid.New(),
		ComponentInstanceID: uuid.New(), ComponentKey: "ledger",
		ExpectedDesiredRevision: 4, ExpectedDesiredChecksum: desiredChecksum,
		Candidates: []types.BaselineCandidate{{
			SourceDeploymentPlanID: &sourcePlanID,
			ExternalExecutionID:    &executionID,
			ObservationID:          uuid.New(),
			ObservedAt:             time.Now().UTC(),
			Health:                 types.TargetComponentHealthHealthy,
			DesiredRevision:        4,
			DesiredChecksum:        desiredChecksum,
			ObservedRevision:       4,
			ObservedChecksum:       desiredChecksum,
			PlanSchema:             types.TargetDeploymentPlanSchemaV2,
			ProtocolVersion:        types.DeploymentPlanProtocolV2,
			PlanFactsMatch:         true,
			ReleaseBundleID:        uuid.New(),
			Version:                "2.0.0",
			Image:                  "registry.example/ledger@" + testChecksum("a"),
			Platform:               "linux/amd64",
			ConfigChecksum:         testChecksum("b"),
		}},
	}

	baseline, err := SelectVerifiedBaseline(context.Background(), query)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(baseline.Projection).To(Equal(types.BaselineProjectionVerifiedV2))
	g.Expect(baseline.AuthorizesV2Execution).To(BeTrue())
}

func TestSelectVerifiedBaselineObservationChecksumPinsPlatform(t *testing.T) {
	g := NewWithT(t)
	desiredChecksum := testChecksum("c")
	candidate := types.BaselineCandidate{
		ObservationID:    uuid.New(),
		ObservedAt:       time.Now().UTC(),
		Health:           types.TargetComponentHealthHealthy,
		DesiredRevision:  3,
		DesiredChecksum:  desiredChecksum,
		ObservedRevision: 3,
		ObservedChecksum: desiredChecksum,
		PlanSchema:       types.LegacyDeploymentPlanSchemaV1,
		ReleaseBundleID:  uuid.New(),
		Version:          "1.2.3",
		Image:            "registry.example/api@" + testChecksum("d"),
		Platform:         "linux/amd64",
		ConfigChecksum:   testChecksum("e"),
	}
	query := types.BaselineQuery{
		OrganizationID:          uuid.New(),
		DeploymentUnitID:        uuid.New(),
		ComponentInstanceID:     uuid.New(),
		ComponentKey:            "api",
		ExpectedDesiredRevision: 3,
		ExpectedDesiredChecksum: desiredChecksum,
		Candidates:              []types.BaselineCandidate{candidate},
	}

	amd64Baseline, err := SelectVerifiedBaseline(context.Background(), query)
	g.Expect(err).NotTo(HaveOccurred())
	query.Candidates[0].Platform = "linux/arm64"
	arm64Baseline, err := SelectVerifiedBaseline(context.Background(), query)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(amd64Baseline.ObservationChecksum).NotTo(Equal(arm64Baseline.ObservationChecksum))
}

func testChecksum(char string) string {
	result := "sha256:"
	for range 64 {
		result += char
	}
	return result
}
