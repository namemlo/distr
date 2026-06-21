package api

import (
	"testing"

	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestAgentCapabilitiesRequestValidateNormalizesPayload(t *testing.T) {
	g := NewWithT(t)
	request := AgentCapabilitiesRequest{
		ProtocolVersion:      " v1 ",
		AgentVersion:         " 1.2.3 ",
		SupportedRuntimes:    []string{" docker ", "kubernetes"},
		OperatingSystem:      " linux ",
		Architecture:         " amd64 ",
		AvailableTooling:     []string{" docker ", "helm"},
		StrategyCapabilities: []string{" rolling ", "blue-green"},
		SupportedActions: []AgentActionCapabilityRequest{
			{
				ActionType: " distr.http.check ",
				Versions:   []string{" 1 "},
			},
			{
				ActionType: "distr.preflight",
				Versions:   []string{"1"},
			},
		},
	}

	err := request.Validate()

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(request.ProtocolVersion).To(Equal(types.AgentCapabilityProtocolV1))
	g.Expect(request.AgentVersion).To(Equal("1.2.3"))
	g.Expect(request.SupportedRuntimes).To(Equal([]string{"docker", "kubernetes"}))
	g.Expect(request.OperatingSystem).To(Equal("linux"))
	g.Expect(request.Architecture).To(Equal("amd64"))
	g.Expect(request.AvailableTooling).To(Equal([]string{"docker", "helm"}))
	g.Expect(request.StrategyCapabilities).To(Equal([]string{"rolling", "blue-green"}))
	g.Expect(request.SupportedActions[0].ActionType).To(Equal("distr.http.check"))
	g.Expect(request.SupportedActions[0].Versions).To(Equal([]string{"1"}))
}

func TestAgentCapabilitiesRequestValidateAllowsNoSupportedActions(t *testing.T) {
	g := NewWithT(t)
	request := AgentCapabilitiesRequest{
		ProtocolVersion:   types.AgentCapabilityProtocolV1,
		AgentVersion:      "1.2.3",
		SupportedRuntimes: []string{"docker"},
		OperatingSystem:   "linux",
		Architecture:      "amd64",
		SupportedActions:  nil,
	}

	err := request.Validate()

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(request.SupportedActions).To(BeNil())
}

func TestAgentCapabilitiesRequestValidateRejectsInvalidPayloads(t *testing.T) {
	base := func() AgentCapabilitiesRequest {
		return AgentCapabilitiesRequest{
			ProtocolVersion:   types.AgentCapabilityProtocolV1,
			AgentVersion:      "1.2.3",
			SupportedRuntimes: []string{"docker"},
			OperatingSystem:   "linux",
			Architecture:      "amd64",
			SupportedActions: []AgentActionCapabilityRequest{
				{ActionType: "distr.http.check", Versions: []string{"1"}},
			},
		}
	}
	tests := []struct {
		name    string
		mutate  func(*AgentCapabilitiesRequest)
		message string
	}{
		{
			name:    "unsupported protocol",
			mutate:  func(r *AgentCapabilitiesRequest) { r.ProtocolVersion = "v2" },
			message: "protocolVersion is unsupported",
		},
		{
			name:    "empty agent version",
			mutate:  func(r *AgentCapabilitiesRequest) { r.AgentVersion = " " },
			message: "agentVersion is required",
		},
		{
			name:    "empty runtime",
			mutate:  func(r *AgentCapabilitiesRequest) { r.SupportedRuntimes = []string{"docker", " "} },
			message: "supportedRuntimes must not contain empty values",
		},
		{
			name:    "duplicate runtime",
			mutate:  func(r *AgentCapabilitiesRequest) { r.SupportedRuntimes = []string{"docker", " docker "} },
			message: "supportedRuntimes must be unique",
		},
		{
			name:    "unknown action",
			mutate:  func(r *AgentCapabilitiesRequest) { r.SupportedActions[0].ActionType = "shell" },
			message: "supportedActions[0].actionType is unknown",
		},
		{
			name: "duplicate action",
			mutate: func(r *AgentCapabilitiesRequest) {
				r.SupportedActions = append(r.SupportedActions, AgentActionCapabilityRequest{
					ActionType: " distr.http.check ",
					Versions:   []string{"1"},
				})
			},
			message: "supportedActions actionType values must be unique",
		},
		{
			name:    "missing action versions",
			mutate:  func(r *AgentCapabilitiesRequest) { r.SupportedActions[0].Versions = nil },
			message: "supportedActions[0].versions is required",
		},
		{
			name:    "empty action version",
			mutate:  func(r *AgentCapabilitiesRequest) { r.SupportedActions[0].Versions = []string{"1", " "} },
			message: "supportedActions[0].versions must not contain empty values",
		},
		{
			name:    "duplicate action version",
			mutate:  func(r *AgentCapabilitiesRequest) { r.SupportedActions[0].Versions = []string{"1", " 1 "} },
			message: "supportedActions[0].versions must be unique",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			request := base()
			tt.mutate(&request)

			err := request.Validate()

			g.Expect(err).To(MatchError(ContainSubstring(tt.message)))
		})
	}
}
