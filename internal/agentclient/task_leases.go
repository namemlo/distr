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

func (c *Client) LeaseTask(ctx context.Context) (*api.AgentTaskLease, error) {
	if strings.TrimSpace(c.leaseEndpoint) == "" {
		return nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.leaseEndpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.doAuthenticated(ctx, req, false)
	if resp != nil {
		switch resp.StatusCode {
		case http.StatusForbidden, http.StatusNotFound, http.StatusNoContent:
			return nil, nil
		}
	}
	if err != nil {
		return nil, err
	}
	var lease api.AgentTaskLease
	if err := json.NewDecoder(resp.Body).Decode(&lease); err != nil {
		return nil, err
	}
	return &lease, nil
}

func (c *Client) HeartbeatTaskLease(
	ctx context.Context,
	taskID uuid.UUID,
	leaseToken string,
) (*api.AgentTaskLease, error) {
	endpoint := taskHeartbeatEndpoint(c.taskHeartbeatEndpointTemplate, taskID)
	if endpoint == "" {
		return nil, fmt.Errorf("task heartbeat endpoint is required for leased tasks")
	}
	request := api.HeartbeatAgentTaskLeaseRequest{LeaseToken: leaseToken}
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
	var lease api.AgentTaskLease
	if err := json.NewDecoder(resp.Body).Decode(&lease); err != nil {
		return nil, err
	}
	return &lease, nil
}

func taskHeartbeatEndpoint(endpointTemplate string, taskID uuid.UUID) string {
	endpointTemplate = strings.TrimSpace(endpointTemplate)
	if endpointTemplate == "" {
		return ""
	}
	return strings.ReplaceAll(endpointTemplate, "{taskId}", taskID.String())
}
