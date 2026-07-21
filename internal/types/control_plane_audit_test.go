package types

import (
	"testing"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestControlPlaneAuditInputExposesCompleteTypedCorrelation(t *testing.T) {
	g := NewWithT(t)
	id := func() *uuid.UUID {
		value := uuid.New()
		return &value
	}
	input := ControlPlaneAuditEventInput{
		ReleaseID:                        id(),
		ComponentReleaseID:               id(),
		ProductReleaseID:                 id(),
		TargetConfigID:                   id(),
		DeploymentPlanID:                 id(),
		DeploymentPolicyID:               id(),
		DeploymentPolicyVersionID:        id(),
		ApprovalID:                       id(),
		MaintenanceCalendarID:            id(),
		DeploymentFreezeID:               id(),
		AdmissionDecisionID:              id(),
		EmergencyOverrideID:              id(),
		CampaignDraftID:                  id(),
		CampaignRevisionID:               id(),
		CampaignRunID:                    id(),
		CampaignWaveDefinitionID:         id(),
		CampaignWaveRunID:                id(),
		CampaignMemberID:                 id(),
		CampaignMemberRunID:              id(),
		CampaignControlRequestID:         id(),
		CampaignExclusionID:              id(),
		CampaignPrerequisiteEvaluationID: id(),
		CampaignThresholdEvaluationID:    id(),
		ExecutionID:                      id(),
		ExecutionAttemptID:               id(),
		AdapterRevisionID:                id(),
		DesiredStateID:                   id(),
		ObservationID:                    id(),
		DriftCaseID:                      id(),
		ReconciliationID:                 id(),
		DeploymentTargetID:               id(),
		EnvironmentID:                    id(),
		CustomerOrganizationID:           id(),
		DeploymentUnitID:                 id(),
		ComponentID:                      id(),
		TaskID:                           id(),
		StepRunID:                        id(),
		AuditExportSinkID:                id(),
		AuditExportAttemptID:             id(),
	}

	correlations := input.Correlations()
	g.Expect(correlations).To(HaveLen(39))
	g.Expect(correlations).To(ContainElements(
		AuditCorrelation{Kind: AuditCorrelationComponentRelease, ID: *input.ComponentReleaseID},
		AuditCorrelation{Kind: AuditCorrelationProductRelease, ID: *input.ProductReleaseID},
		AuditCorrelation{Kind: AuditCorrelationDeploymentPolicy, ID: *input.DeploymentPolicyID},
		AuditCorrelation{Kind: AuditCorrelationDesiredState, ID: *input.DesiredStateID},
		AuditCorrelation{Kind: AuditCorrelationDriftCase, ID: *input.DriftCaseID},
		AuditCorrelation{Kind: AuditCorrelationCampaignDraft, ID: *input.CampaignDraftID},
		AuditCorrelation{Kind: AuditCorrelationCampaignRevision, ID: *input.CampaignRevisionID},
		AuditCorrelation{Kind: AuditCorrelationCampaignRun, ID: *input.CampaignRunID},
		AuditCorrelation{Kind: AuditCorrelationCampaignWaveDefinition, ID: *input.CampaignWaveDefinitionID},
		AuditCorrelation{Kind: AuditCorrelationCampaignWaveRun, ID: *input.CampaignWaveRunID},
		AuditCorrelation{Kind: AuditCorrelationCampaignMember, ID: *input.CampaignMemberID},
		AuditCorrelation{Kind: AuditCorrelationCampaignMemberRun, ID: *input.CampaignMemberRunID},
		AuditCorrelation{Kind: AuditCorrelationCampaignControlRequest, ID: *input.CampaignControlRequestID},
		AuditCorrelation{Kind: AuditCorrelationCampaignExclusion, ID: *input.CampaignExclusionID},
		AuditCorrelation{Kind: AuditCorrelationCampaignPrerequisiteEvaluation, ID: *input.CampaignPrerequisiteEvaluationID},
		AuditCorrelation{Kind: AuditCorrelationCampaignThresholdEvaluation, ID: *input.CampaignThresholdEvaluationID},
		AuditCorrelation{Kind: AuditCorrelationExecutionAttempt, ID: *input.ExecutionAttemptID},
	))
}

func TestEvidenceBundleUsesVersionedCanonicalContract(t *testing.T) {
	g := NewWithT(t)
	g.Expect(EvidenceBundleSchemaV1).To(Equal("distr.control-plane-evidence/v1"))
}
