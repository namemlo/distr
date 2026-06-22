package agentclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/distr-sh/distr/api"
	"github.com/google/uuid"
)

func (c *Client) RecordStepRunEvent(
	ctx context.Context,
	stepRunID uuid.UUID,
	request api.AgentStepRunEventRequest,
) (*api.StepRunEvent, error) {
	endpoint := stepEventEndpoint(c.stepEventEndpointTemplate, stepRunID)
	if endpoint == "" {
		return nil, fmt.Errorf("step event endpoint is required for leased tasks")
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(request); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.doAuthenticated(ctx, req, false)
	if err != nil {
		return nil, err
	}
	var event api.StepRunEvent
	if err := json.NewDecoder(resp.Body).Decode(&event); err != nil {
		return nil, err
	}
	return &event, nil
}

func stepEventEndpoint(endpointTemplate string, stepRunID uuid.UUID) string {
	endpointTemplate = strings.TrimSpace(endpointTemplate)
	if endpointTemplate == "" {
		return ""
	}
	return strings.ReplaceAll(endpointTemplate, "{stepRunId}", stepRunID.String())
}

func (c *Client) GetTaskTimeline(ctx context.Context, taskID uuid.UUID) (*api.TaskTimeline, error) {
	endpoint := taskTimelineEndpoint(c.taskTimelineEndpointTemplate, taskID)
	if endpoint == "" {
		return nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.doAuthenticated(ctx, req, false)
	if err != nil {
		return nil, err
	}
	var timeline api.TaskTimeline
	if err := json.NewDecoder(resp.Body).Decode(&timeline); err != nil {
		return nil, err
	}
	return &timeline, nil
}

func taskTimelineEndpoint(endpointTemplate string, taskID uuid.UUID) string {
	endpointTemplate = strings.TrimSpace(endpointTemplate)
	if endpointTemplate == "" {
		return ""
	}
	return strings.ReplaceAll(endpointTemplate, "{taskId}", taskID.String())
}
