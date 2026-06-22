package mapping

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
)

func TaskTimelineToAPI(timeline types.TaskTimeline) api.TaskTimeline {
	return api.TaskTimeline{
		OrganizationID: timeline.OrganizationID,
		TaskID:         timeline.TaskID,
		Events:         List(timeline.Events, StepRunEventToAPI),
	}
}

func StepRunEventToAPI(event types.StepRunEvent) api.StepRunEvent {
	return api.StepRunEvent{
		ID:              event.ID,
		CreatedAt:       event.CreatedAt,
		OccurredAt:      event.OccurredAt,
		OrganizationID:  event.OrganizationID,
		TaskID:          event.TaskID,
		StepRunID:       event.StepRunID,
		TaskLeaseID:     event.TaskLeaseID,
		AgentID:         event.AgentID,
		Sequence:        event.Sequence,
		Type:            event.Type,
		Message:         event.Message,
		ProgressPercent: event.ProgressPercent,
		Details:         event.Details,
		Redacted:        event.Redacted,
		Logs:            List(event.Logs, StepRunLogChunkToAPI),
		Outputs:         List(event.Outputs, StepRunOutputToAPI),
	}
}

func StepRunLogChunkToAPI(log types.StepRunLogChunk) api.StepRunLogChunk {
	return api.StepRunLogChunk{
		ID:             log.ID,
		CreatedAt:      log.CreatedAt,
		OccurredAt:     log.OccurredAt,
		EventID:        log.EventID,
		OrganizationID: log.OrganizationID,
		TaskID:         log.TaskID,
		StepRunID:      log.StepRunID,
		TaskLeaseID:    log.TaskLeaseID,
		AgentID:        log.AgentID,
		ChunkIndex:     log.ChunkIndex,
		Stream:         log.Stream,
		Severity:       log.Severity,
		Body:           log.Body,
		Redacted:       log.Redacted,
	}
}

func StepRunOutputToAPI(output types.StepRunOutput) api.StepRunOutput {
	return api.StepRunOutput{
		ID:             output.ID,
		CreatedAt:      output.CreatedAt,
		UpdatedAt:      output.UpdatedAt,
		EventID:        output.EventID,
		OrganizationID: output.OrganizationID,
		TaskID:         output.TaskID,
		StepRunID:      output.StepRunID,
		TaskLeaseID:    output.TaskLeaseID,
		AgentID:        output.AgentID,
		Name:           output.Name,
		Value:          output.Value,
		Sensitive:      output.Sensitive,
		Redacted:       output.Redacted,
	}
}
