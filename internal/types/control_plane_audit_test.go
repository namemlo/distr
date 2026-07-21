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
		ReleaseID:                 id(),
		ComponentReleaseID:        id(),
		ProductReleaseID:          id(),
		TargetConfigID:            id(),
		DeploymentPlanID:          id(),
		DeploymentPolicyID:        id(),
		DeploymentPolicyVersionID: id(),
		ApprovalID:                id(),
		MaintenanceCalendarID:     id(),
		DeploymentFreezeID:        id(),
		AdmissionDecisionID:       id(),
		EmergencyOverrideID:       id(),
		CampaignID:                id(),
		WaveID:                    id(),
		ExecutionID:               id(),
		AdapterRevisionID:         id(),
		DesiredStateID:            id(),
		ObservationID:             id(),
		DriftCaseID:               id(),
		ReconciliationID:          id(),
		DeploymentTargetID:        id(),
		EnvironmentID:             id(),
		CustomerOrganizationID:    id(),
		DeploymentUnitID:          id(),
		ComponentID:               id(),
		TaskID:                    id(),
		StepRunID:                 id(),
		AuditExportSinkID:         id(),
		AuditExportAttemptID:      id(),
	}

	correlations := input.Correlations()
	g.Expect(correlations).To(HaveLen(29))
	g.Expect(correlations).To(ContainElements(
		AuditCorrelation{Kind: AuditCorrelationComponentRelease, ID: *input.ComponentReleaseID},
		AuditCorrelation{Kind: AuditCorrelationProductRelease, ID: *input.ProductReleaseID},
		AuditCorrelation{Kind: AuditCorrelationDeploymentPolicy, ID: *input.DeploymentPolicyID},
		AuditCorrelation{Kind: AuditCorrelationDesiredState, ID: *input.DesiredStateID},
		AuditCorrelation{Kind: AuditCorrelationDriftCase, ID: *input.DriftCaseID},
	))
}
