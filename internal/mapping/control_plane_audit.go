package mapping

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
)

func ControlPlaneAuditEventToAPI(event types.ControlPlaneAuditEvent) api.ControlPlaneAuditEvent {
	return api.ControlPlaneAuditEvent{
		ID:                               event.ID,
		Sequence:                         event.Sequence,
		EventType:                        event.EventType,
		ActorID:                          event.ActorID,
		Outcome:                          event.Outcome,
		ReleaseID:                        event.ReleaseID,
		ComponentReleaseID:               event.ComponentReleaseID,
		ProductReleaseID:                 event.ProductReleaseID,
		TargetConfigID:                   event.TargetConfigID,
		DeploymentPlanID:                 event.DeploymentPlanID,
		DeploymentPolicyID:               event.DeploymentPolicyID,
		DeploymentPolicyVersionID:        event.DeploymentPolicyVersionID,
		ApprovalID:                       event.ApprovalID,
		MaintenanceCalendarID:            event.MaintenanceCalendarID,
		DeploymentFreezeID:               event.DeploymentFreezeID,
		AdmissionDecisionID:              event.AdmissionDecisionID,
		EmergencyOverrideID:              event.EmergencyOverrideID,
		CampaignDraftID:                  event.CampaignDraftID,
		CampaignRevisionID:               event.CampaignRevisionID,
		CampaignRunID:                    event.CampaignRunID,
		CampaignWaveDefinitionID:         event.CampaignWaveDefinitionID,
		CampaignWaveRunID:                event.CampaignWaveRunID,
		CampaignMemberID:                 event.CampaignMemberID,
		CampaignMemberRunID:              event.CampaignMemberRunID,
		CampaignControlRequestID:         event.CampaignControlRequestID,
		CampaignExclusionID:              event.CampaignExclusionID,
		CampaignPrerequisiteEvaluationID: event.CampaignPrerequisiteEvaluationID,
		CampaignThresholdEvaluationID:    event.CampaignThresholdEvaluationID,
		ExecutionID:                      event.ExecutionID,
		ExecutionAttemptID:               event.ExecutionAttemptID,
		AdapterRevisionID:                event.AdapterRevisionID,
		DesiredStateID:                   event.DesiredStateID,
		ObservationID:                    event.ObservationID,
		DriftCaseID:                      event.DriftCaseID,
		ReconciliationID:                 event.ReconciliationID,
		DeploymentTargetID:               event.DeploymentTargetID,
		EnvironmentID:                    event.EnvironmentID,
		CustomerOrganizationID:           event.CustomerOrganizationID,
		DeploymentUnitID:                 event.DeploymentUnitID,
		ComponentID:                      event.ComponentID,
		TaskID:                           event.TaskID,
		StepRunID:                        event.StepRunID,
		AuditExportSinkID:                event.AuditExportSinkID,
		AuditExportAttemptID:             event.AuditExportAttemptID,
		ReleaseChecksum:                  event.ReleaseChecksum,
		ComponentReleaseChecksum:         event.ComponentReleaseChecksum,
		ProductReleaseChecksum:           event.ProductReleaseChecksum,
		ArtifactDigest:                   event.ArtifactDigest,
		ManifestDigest:                   event.ManifestDigest,
		TargetConfigChecksum:             event.TargetConfigChecksum,
		DeploymentPlanChecksum:           event.DeploymentPlanChecksum,
		PolicyChecksum:                   event.PolicyChecksum,
		ApprovalChecksum:                 event.ApprovalChecksum,
		CalendarChecksum:                 event.CalendarChecksum,
		AdmissionChecksum:                event.AdmissionChecksum,
		CampaignRevisionChecksum:         event.CampaignRevisionChecksum,
		CampaignControlChecksum:          event.CampaignControlChecksum,
		ExecutionChecksum:                event.ExecutionChecksum,
		DesiredStateChecksum:             event.DesiredStateChecksum,
		ObservationChecksum:              event.ObservationChecksum,
		DriftChecksum:                    event.DriftChecksum,
		ReconciliationChecksum:           event.ReconciliationChecksum,
		AuditExportConfigChecksum:        event.AuditExportConfigChecksum,
		Payload:                          event.Payload,
		PayloadRedacted:                  event.PayloadRedacted,
		PayloadTruncated:                 event.PayloadTruncated,
		CreatedAt:                        event.CreatedAt,
	}
}

func ControlPlaneAuditEventPageToAPI(
	events []types.ControlPlaneAuditEvent,
) api.ControlPlaneAuditEventPage {
	items := List(events, ControlPlaneAuditEventToAPI)
	var next int64
	if len(events) > 0 {
		next = events[len(events)-1].Sequence
	}
	return api.ControlPlaneAuditEventPage{Items: items, NextAfterSequence: next}
}

func EvidenceBundleToAPI(bundle types.EvidenceBundle) api.EvidenceBundle {
	return api.EvidenceBundle{
		Version:          bundle.Version,
		DeploymentPlanID: bundle.DeploymentPlanID,
		Events:           List(bundle.Events, ControlPlaneAuditEventToAPI),
		Checksum:         bundle.Checksum,
	}
}

func AuditExportSinkToAPI(sink types.AuditExportSink) api.AuditExportSink {
	return api.AuditExportSink{
		ID:                  sink.ID,
		Name:                sink.Name,
		Kind:                sink.Kind,
		EndpointReference:   sink.EndpointReference,
		ConfigChecksum:      sink.ConfigChecksum,
		Enabled:             sink.Enabled,
		LastSuccessAt:       sink.LastSuccessAt,
		LastFailureAt:       sink.LastFailureAt,
		ConsecutiveFailures: sink.ConsecutiveFailures,
		CreatedAt:           sink.CreatedAt,
		UpdatedAt:           sink.UpdatedAt,
	}
}

func AuditExportStatusToAPI(status types.AuditExportStatus) api.AuditExportStatus {
	return api.AuditExportStatus{
		Sink:                   AuditExportSinkToAPI(status.Sink),
		LastExportedSequence:   status.LastExportedSequence,
		LastExportedEventID:    status.LastExportedEventID,
		LatestSequence:         status.LatestSequence,
		CheckpointLag:          status.CheckpointLag,
		Alert:                  status.Alert,
		LastAttemptStatus:      status.LastAttemptStatus,
		LastAttemptError:       status.LastAttemptError,
		LastAttemptCompletedAt: status.LastAttemptCompletedAt,
	}
}
