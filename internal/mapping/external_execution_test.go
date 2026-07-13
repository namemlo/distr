package mapping

import (
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestExternalExecutionToAPIHidesOrganizationAndMapsObservedState(t *testing.T) {
	g := NewWithT(t)
	platform := types.DeploymentTargetPlatformLinuxAMD64
	health := types.TargetComponentHealthHealthy
	execution := types.ExternalExecution{
		ID: uuid.New(), CreatedAt: time.Now(), UpdatedAt: time.Now(), OrganizationID: uuid.New(),
		StepRunID: uuid.New(), TaskID: uuid.New(), DeploymentPlanID: uuid.New(),
		DeploymentPlanTargetID: uuid.New(), DeploymentTargetID: uuid.New(), ApplicationID: uuid.New(),
		ReleaseBundleID: uuid.New(), Component: "loyalty-api",
		PlanChecksum: "sha256:" + strings.Repeat("a", 64), IdempotencyKey: "ext:test",
		ExpectedVersion: "1.4.2", ExpectedImage: "repo/loyalty-api@sha256:" + strings.Repeat("b", 64),
		ExpectedPlatform: platform, ExpectedContracts: []string{"loyalty.v1"},
		ExpectedConfigReference: "s3://bucket/config?versionId=v42",
		ExpectedConfigChecksum: "sha256:" + strings.Repeat("c", 64),
		Status: types.ExternalExecutionStatusSucceeded, ActualVersion: "1.4.2",
		ActualImage: "repo/loyalty-api@sha256:" + strings.Repeat("b", 64), ActualPlatform: &platform,
		ActualContracts: []string{"loyalty.v1"}, ActualConfigReference: "s3://bucket/config?versionId=v42",
		ActualConfigChecksum: "sha256:" + strings.Repeat("c", 64), ActualHealth: &health,
	}

	mapped := ExternalExecutionToAPI(execution)

	g.Expect(mapped.ID).To(Equal(execution.ID))
	g.Expect(mapped.ExpectedState.Image).To(Equal(execution.ExpectedImage))
	g.Expect(mapped.ObservedState).NotTo(BeNil())
	g.Expect(mapped.ObservedState.Health).To(Equal(types.TargetComponentHealthHealthy))
}
