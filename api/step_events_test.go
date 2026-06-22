package api

import (
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestAgentStepRunEventRequestValidate(t *testing.T) {
	tests := []struct {
		name      string
		request   AgentStepRunEventRequest
		wantError string
	}{
		{
			name: "valid started event trims token and output names",
			request: AgentStepRunEventRequest{
				LeaseToken: " lease-token ",
				Sequence:   1,
				Type:       types.StepRunEventTypeStarted,
				Message:    " starting ",
				Logs: []AgentStepRunLogChunkRequest{
					{
						Stream:   types.StepRunLogStreamStdout,
						Severity: types.StepRunLogSeverityInfo,
						Body:     "started",
					},
				},
				Outputs: []AgentStepRunOutputRequest{
					{Name: " url ", Value: "https://example.com"},
				},
			},
		},
		{
			name:      "missing token",
			request:   AgentStepRunEventRequest{Sequence: 1, Type: types.StepRunEventTypeStarted},
			wantError: "leaseToken is required",
		},
		{
			name:      "missing sequence",
			request:   AgentStepRunEventRequest{LeaseToken: "lease-token", Type: types.StepRunEventTypeStarted},
			wantError: "sequence must be greater than 0",
		},
		{
			name: "invalid event type",
			request: AgentStepRunEventRequest{
				LeaseToken: "lease-token",
				Sequence:   1,
				Type:       types.StepRunEventType("PAUSED"),
			},
			wantError: "type is invalid",
		},
		{
			name: "invalid progress percent",
			request: AgentStepRunEventRequest{
				LeaseToken:      "lease-token",
				Sequence:        1,
				Type:            types.StepRunEventTypeProgress,
				ProgressPercent: intPtr(101),
			},
			wantError: "progressPercent must be between 0 and 100",
		},
		{
			name: "invalid log stream",
			request: AgentStepRunEventRequest{
				LeaseToken: "lease-token",
				Sequence:   1,
				Type:       types.StepRunEventTypeLog,
				Logs: []AgentStepRunLogChunkRequest{
					{Stream: types.StepRunLogStream("sideways"), Severity: types.StepRunLogSeverityInfo, Body: "body"},
				},
			},
			wantError: "logs[0].stream is invalid",
		},
		{
			name: "empty output name",
			request: AgentStepRunEventRequest{
				LeaseToken: "lease-token",
				Sequence:   1,
				Type:       types.StepRunEventTypeOutput,
				Outputs:    []AgentStepRunOutputRequest{{Name: "   ", Value: "value"}},
			},
			wantError: "outputs[0].name is required",
		},
		{
			name: "duplicate trimmed output names",
			request: AgentStepRunEventRequest{
				LeaseToken: "lease-token",
				Sequence:   1,
				Type:       types.StepRunEventTypeOutput,
				Outputs: []AgentStepRunOutputRequest{
					{Name: "url", Value: "https://example.com"},
					{Name: " url ", Value: "https://example.org"},
				},
			},
			wantError: "outputs contains duplicate name",
		},
		{
			name: "invalid output name",
			request: AgentStepRunEventRequest{
				LeaseToken: "lease-token",
				Sequence:   1,
				Type:       types.StepRunEventTypeOutput,
				Outputs:    []AgentStepRunOutputRequest{{Name: "status code", Value: 200}},
			},
			wantError: "outputs[0].name is invalid",
		},
		{
			name: "oversized log body",
			request: AgentStepRunEventRequest{
				LeaseToken: "lease-token",
				Sequence:   1,
				Type:       types.StepRunEventTypeLog,
				Logs: []AgentStepRunLogChunkRequest{
					{
						Stream:   types.StepRunLogStreamStdout,
						Severity: types.StepRunLogSeverityInfo,
						Body:     strings.Repeat("x", MaxStepRunLogChunkBodyLength+1),
					},
				},
			},
			wantError: "logs[0].body is too long",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			err := tt.request.Validate()

			if tt.wantError == "" {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(tt.request.LeaseToken).To(Equal("lease-token"))
				if len(tt.request.Outputs) > 0 {
					g.Expect(tt.request.Outputs[0].Name).To(Equal("url"))
				}
			} else {
				g.Expect(err).To(MatchError(ContainSubstring(tt.wantError)))
			}
		})
	}
}

func intPtr(value int) *int {
	return &value
}
