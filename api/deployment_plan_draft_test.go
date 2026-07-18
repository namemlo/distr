package api

import (
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCreateDeploymentPlanDraftRequestValidate(t *testing.T) {
	g := NewWithT(t)
	valid := CreateDeploymentPlanDraftRequest{
		ProductReleaseID:        uuid.New(),
		DeploymentUnitID:        uuid.New(),
		EnvironmentAssignmentID: uuid.New(),
		TargetConfigSnapshotID:  uuid.New(),
		ProtocolVersion:         types.DeploymentPlanProtocolV2,
	}
	g.Expect(valid.Validate()).To(Succeed())

	invalid := valid
	invalid.ProductReleaseID = uuid.Nil
	g.Expect(invalid.Validate()).To(MatchError(ContainSubstring("productReleaseId")))

	invalid = valid
	invalid.ProtocolVersion = "v3"
	g.Expect(invalid.Validate()).To(MatchError(ContainSubstring("protocolVersion")))
}

func TestCreateDeploymentPlanDraftRequestSupersedePair(t *testing.T) {
	g := NewWithT(t)
	request := CreateDeploymentPlanDraftRequest{
		ProductReleaseID:           uuid.New(),
		DeploymentUnitID:           uuid.New(),
		EnvironmentAssignmentID:    uuid.New(),
		TargetConfigSnapshotID:     uuid.New(),
		ProtocolVersion:            types.DeploymentPlanProtocolV1,
		SupersedesDeploymentPlanID: ptrDraftUUID(uuid.New()),
	}

	g.Expect(request.Validate()).To(MatchError(ContainSubstring("supersedeReason")))
	request.SupersedeReason = strings.Repeat("x", 2049)
	g.Expect(request.Validate()).To(MatchError(ContainSubstring("supersedeReason")))
	request.SupersedeReason = "correct target binding"
	g.Expect(request.Validate()).To(Succeed())
}

func TestUpdateAndPublishDraftRequireOptimisticPreconditions(t *testing.T) {
	g := NewWithT(t)
	update := UpdateDeploymentPlanDraftRequest{}
	g.Expect(update.Validate()).To(MatchError(ContainSubstring("expectedRevision")))

	publish := PublishDeploymentPlanDraftRequest{
		ExpectedRevision:        1,
		ExpectedPreviewChecksum: "sha256:" + strings.Repeat("a", 64),
	}
	g.Expect(publish.Validate()).To(Succeed())
	publish.ExpectedPreviewChecksum = "sha256:bad"
	g.Expect(publish.Validate()).To(MatchError(ContainSubstring("expectedPreviewChecksum")))
}

func ptrDraftUUID(value uuid.UUID) *uuid.UUID {
	return &value
}
