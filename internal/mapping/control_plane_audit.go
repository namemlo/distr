package mapping

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
)

func ControlPlaneAuditEventToAPI(event types.ControlPlaneAuditEvent) api.ControlPlaneAuditEvent {
	return api.ControlPlaneAuditEvent{
		ID:                     event.ID,
		Sequence:               event.Sequence,
		EventType:              event.EventType,
		ActorID:                event.ActorID,
		Outcome:                event.Outcome,
		ReleaseID:              event.ReleaseID,
		TargetConfigID:         event.TargetConfigID,
		DeploymentPlanID:       event.DeploymentPlanID,
		ApprovalID:             event.ApprovalID,
		CampaignID:             event.CampaignID,
		WaveID:                 event.WaveID,
		ExecutionID:            event.ExecutionID,
		AdapterRevisionID:      event.AdapterRevisionID,
		ObservationID:          event.ObservationID,
		ReconciliationID:       event.ReconciliationID,
		ReleaseChecksum:        event.ReleaseChecksum,
		TargetConfigChecksum:   event.TargetConfigChecksum,
		DeploymentPlanChecksum: event.DeploymentPlanChecksum,
		ApprovalChecksum:       event.ApprovalChecksum,
		CampaignChecksum:       event.CampaignChecksum,
		ExecutionChecksum:      event.ExecutionChecksum,
		ObservationChecksum:    event.ObservationChecksum,
		Payload:                event.Payload,
		PayloadRedacted:        event.PayloadRedacted,
		PayloadTruncated:       event.PayloadTruncated,
		CreatedAt:              event.CreatedAt,
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
