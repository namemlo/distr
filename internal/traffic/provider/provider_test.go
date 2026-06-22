package provider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	. "github.com/onsi/gomega"

	"github.com/google/uuid"
)

func TestDefaultRegistryBuildsWebhookProviderAndAdvertisesCapabilities(t *testing.T) {
	g := NewWithT(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	registry := DefaultRegistry()
	trafficProvider, err := registry.Build(ProviderConfig{
		Type: WebhookProviderType,
		Webhook: WebhookProviderConfig{
			URL:               server.URL,
			AllowInsecureHTTP: true,
		},
	})

	g.Expect(err).NotTo(HaveOccurred())
	capability := trafficProvider.Capability()
	g.Expect(capability.Type).To(Equal(WebhookProviderType))
	g.Expect(capability.Supports(OperationPrepare)).To(BeTrue())
	g.Expect(capability.Supports(OperationShift)).To(BeTrue())
	g.Expect(capability.Supports(OperationVerify)).To(BeTrue())
	g.Expect(capability.Supports(OperationRollback)).To(BeTrue())
	g.Expect(capability.Supports(OperationCleanup)).To(BeTrue())
}

func TestWebhookTrafficProviderPostsOperationPayloads(t *testing.T) {
	g := NewWithT(t)
	planTargetID := uuid.New()
	deploymentTargetID := uuid.New()
	rolloutID := uuid.New()
	var received webhookOperationPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.Expect(r.Method).To(Equal(http.MethodPost))
		g.Expect(r.Header.Get("Content-Type")).To(Equal("application/json"))
		g.Expect(r.Header.Get("Idempotency-Key")).To(Equal("rollout-window-1"))
		g.Expect(json.NewDecoder(r.Body).Decode(&received)).To(Succeed())
		g.Expect(json.NewEncoder(w).Encode(PrepareResult{
			PreparedTargets: []PreparedTarget{{
				DeploymentPlanTargetID: planTargetID,
				ProviderTargetID:       "slot-a",
				Metadata:               map[string]string{"color": "green"},
			}},
		})).To(Succeed())
	}))
	defer server.Close()
	trafficProvider, err := NewWebhookTrafficProvider(WebhookProviderConfig{
		URL:               server.URL,
		AllowInsecureHTTP: true,
	})
	g.Expect(err).NotTo(HaveOccurred())

	result, err := trafficProvider.Prepare(t.Context(), PrepareRequest{
		RolloutContext: RolloutContext{
			RolloutID:        rolloutID,
			Strategy:         "rolling",
			WindowNumber:     1,
			IdempotencyKey:   "rollout-window-1",
			ProviderMetadata: map[string]string{"provider": "webhook"},
		},
		Targets: TargetSet{Targets: []Target{{
			DeploymentPlanTargetID: planTargetID,
			DeploymentTargetID:     deploymentTargetID,
			Name:                   "customer-a",
			Weight:                 100,
		}}},
		Parameters: map[string]any{"phase": "prepare"},
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.PreparedTargets).To(HaveLen(1))
	g.Expect(result.PreparedTargets[0].ProviderTargetID).To(Equal("slot-a"))
	g.Expect(received.Operation).To(Equal(OperationPrepare))
	g.Expect(received.Context.RolloutID).To(Equal(rolloutID))
	g.Expect(received.Targets.Targets[0].DeploymentTargetID).To(Equal(deploymentTargetID))
	g.Expect(received.Parameters).To(HaveKeyWithValue("phase", "prepare"))
}

func TestWebhookTrafficProviderReturnsErrorForNonSuccessStatus(t *testing.T) {
	g := NewWithT(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "provider unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	trafficProvider, err := NewWebhookTrafficProvider(WebhookProviderConfig{
		URL:               server.URL,
		AllowInsecureHTTP: true,
	})
	g.Expect(err).NotTo(HaveOccurred())

	err = trafficProvider.Shift(t.Context(), ShiftRequest{RolloutContext: RolloutContext{IdempotencyKey: "shift-1"}})

	g.Expect(err).To(MatchError(ContainSubstring("traffic provider webhook returned status 503")))
}

func TestWebhookTrafficProviderRejectsUnsafeConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		config WebhookProviderConfig
	}{
		{name: "missing url", config: WebhookProviderConfig{}},
		{name: "http without explicit allowance", config: WebhookProviderConfig{URL: "http://example.test/hook"}},
		{name: "invalid url", config: WebhookProviderConfig{URL: "://bad-url"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			_, err := NewWebhookTrafficProvider(tt.config)

			g.Expect(err).To(HaveOccurred())
		})
	}
}

func TestRegistryRejectsDuplicateAndUnknownProviders(t *testing.T) {
	g := NewWithT(t)
	registry := NewRegistry()
	g.Expect(registry.Register("custom", func(ProviderConfig) (TrafficProvider, error) {
		return nil, nil
	})).To(Succeed())

	g.Expect(registry.Register(" custom ", func(ProviderConfig) (TrafficProvider, error) {
		return nil, nil
	})).To(MatchError(ContainSubstring("already registered")))
	_, err := registry.Build(ProviderConfig{Type: "missing"})
	g.Expect(err).To(MatchError(ContainSubstring("unknown traffic provider")))
}
