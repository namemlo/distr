package agentclient

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"runtime"
	"strings"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/buildconfig"
	"github.com/distr-sh/distr/internal/types"
)

func DefaultCapabilityReport(
	runtimeName string,
	availableTooling []string,
	strategyCapabilities []string,
) api.AgentCapabilitiesRequest {
	return api.AgentCapabilitiesRequest{
		ProtocolVersion:      types.AgentCapabilityProtocolV1,
		AgentVersion:         buildconfig.Version(),
		SupportedRuntimes:    []string{runtimeName},
		SupportedActions:     []api.AgentActionCapabilityRequest{},
		OperatingSystem:      runtime.GOOS,
		Architecture:         runtime.GOARCH,
		AvailableTooling:     availableTooling,
		StrategyCapabilities: strategyCapabilities,
	}
}

func (c *Client) ReportCapabilities(ctx context.Context, capabilities api.AgentCapabilitiesRequest) error {
	if strings.TrimSpace(c.capabilitiesEndpoint) == "" {
		return nil
	}
	if err := capabilities.Validate(); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(capabilities); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.capabilitiesEndpoint, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.doAuthenticated(ctx, req, false)
	if resp != nil && (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound) {
		return nil
	}
	return err
}
