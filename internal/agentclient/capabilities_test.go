package agentclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
	"github.com/lestrrat-go/jwx/v3/jwt"
	"go.uber.org/zap"
)

func TestDefaultCapabilityReportDescribesRuntimeWithoutExecutionActions(t *testing.T) {
	report := DefaultCapabilityReport("docker", []string{"docker"}, []string{"compose"})

	if err := report.Validate(); err != nil {
		t.Fatalf("expected default capability report to validate: %v", err)
	}
	if report.ProtocolVersion != types.AgentCapabilityProtocolV1 {
		t.Fatalf("expected protocol %q, got %q", types.AgentCapabilityProtocolV1, report.ProtocolVersion)
	}
	if len(report.SupportedActions) != 0 {
		t.Fatalf("expected no execution action support, got %v", agentCapabilityActionTypes(report.SupportedActions))
	}
}

func TestProtocolV2CapabilityReportOptsInWithoutChangingDefault(t *testing.T) {
	defaultReport := DefaultCapabilityReport("docker", []string{"docker"}, []string{"compose"})
	v2Report := ProtocolV2CapabilityReport(
		"docker",
		[]string{"docker"},
		[]string{"compose"},
	)

	if defaultReport.ProtocolVersion != types.AgentCapabilityProtocolV1 {
		t.Fatalf("expected default protocol %q, got %q", types.AgentCapabilityProtocolV1, defaultReport.ProtocolVersion)
	}
	if v2Report.ProtocolVersion != types.AgentCapabilityProtocolV2 {
		t.Fatalf("expected opt-in protocol %q, got %q", types.AgentCapabilityProtocolV2, v2Report.ProtocolVersion)
	}
	if err := v2Report.Validate(); err != nil {
		t.Fatalf("expected opt-in v2 report to validate: %v", err)
	}
}

func TestReportCapabilitiesPostsAuthenticatedPayload(t *testing.T) {
	var received api.AgentCapabilitiesRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if got, want := r.Header.Get("Authorization"), "Bearer test-token"; got != want {
			t.Fatalf("expected authorization %q, got %q", want, got)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client := testCapabilitiesClient(server.URL + "/api/v1/agents/target/capabilities")

	err := client.ReportCapabilities(
		context.Background(),
		DefaultCapabilityReport("kubernetes", []string{"helm"}, []string{"helm"}),
	)
	if err != nil {
		t.Fatalf("expected report to succeed: %v", err)
	}
	if got, want := received.SupportedRuntimes, []string{"kubernetes"}; !stringSlicesEqual(got, want) {
		t.Fatalf("expected runtimes %v, got %v", want, got)
	}
	if got, want := received.AvailableTooling, []string{"helm"}; !stringSlicesEqual(got, want) {
		t.Fatalf("expected tooling %v, got %v", want, got)
	}
}

func TestReportCapabilitiesSkipsMissingOrDisabledEndpoint(t *testing.T) {
	client := testCapabilitiesClient("")
	if err := client.ReportCapabilities(
		context.Background(),
		DefaultCapabilityReport("docker", []string{"docker"}, []string{"compose"}),
	); err != nil {
		t.Fatalf("expected missing endpoint to be skipped: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	client = testCapabilitiesClient(server.URL)
	if err := client.ReportCapabilities(
		context.Background(),
		DefaultCapabilityReport("docker", []string{"docker"}, []string{"compose"}),
	); err != nil {
		t.Fatalf("expected disabled endpoint to be skipped: %v", err)
	}
}

func testCapabilitiesClient(endpoint string) *Client {
	token := jwt.New()
	_ = token.Set(jwt.ExpirationKey, time.Now().Add(time.Hour))
	return &Client{
		clientData: clientData{
			capabilitiesEndpoint: endpoint,
		},
		httpClient: http.DefaultClient,
		logger:     zap.NewNop(),
		token:      token,
		rawToken:   "test-token",
	}
}

func agentCapabilityActionTypes(actions []api.AgentActionCapabilityRequest) []string {
	types := make([]string, 0, len(actions))
	for _, action := range actions {
		types = append(types, action.ActionType)
	}
	return types
}

func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
