package policy

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestWebhookPolicyEngineAgentConcurrencyRelease(t *testing.T) {
	g := NewWithT(t)
	engine := NewWebhookPolicyEngine(WebhookPolicyConfig{AgentConcurrentExecutions: 1})
	input := WebhookPolicyInput{
		TenantID: uuid.New(),
		AgentID:  uuid.New(),
		Host:     "hooks.example.com",
	}

	decision, release, err := engine.EvaluateWebhookPolicy(context.Background(), input)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(decision.Allowed).To(BeTrue())

	decision, _, err = engine.EvaluateWebhookPolicy(context.Background(), input)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(decision.Allowed).To(BeFalse())
	g.Expect(decision.Reason).To(Equal("agent concurrency limit exceeded"))

	release(WebhookPolicyResult{StatusCode: 202})
	decision, release, err = engine.EvaluateWebhookPolicy(context.Background(), input)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(decision.Allowed).To(BeTrue())
	release(WebhookPolicyResult{StatusCode: 202})
}

func TestWebhookPolicyEngineEndpointFailureLimit(t *testing.T) {
	g := NewWithT(t)
	engine := NewWebhookPolicyEngine(WebhookPolicyConfig{EndpointFailureLimit: 1})
	input := WebhookPolicyInput{
		TenantID: uuid.New(),
		AgentID:  uuid.New(),
		Host:     "hooks.example.com",
	}

	decision, release, err := engine.EvaluateWebhookPolicy(context.Background(), input)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(decision.Allowed).To(BeTrue())
	release(WebhookPolicyResult{StatusCode: 503})

	decision, _, err = engine.EvaluateWebhookPolicy(context.Background(), input)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(decision.Allowed).To(BeFalse())
	g.Expect(decision.Reason).To(Equal("endpoint failure threshold exceeded"))
}

func TestWebhookPolicyEngineRetryStormAndCircuitBreaker(t *testing.T) {
	g := NewWithT(t)
	engine := NewWebhookPolicyEngine(WebhookPolicyConfig{
		MaxRetryAttempts: 1,
		OpenCircuitHosts: []string{
			"blocked.example.com",
		},
	})

	decision, _, err := engine.EvaluateWebhookPolicy(context.Background(), WebhookPolicyInput{
		TenantID: uuid.New(),
		AgentID:  uuid.New(),
		Host:     "hooks.example.com",
		RetryMax: 2,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(decision.Allowed).To(BeFalse())
	g.Expect(decision.Reason).To(Equal("retry storm detected"))

	decision, _, err = engine.EvaluateWebhookPolicy(context.Background(), WebhookPolicyInput{
		TenantID: uuid.New(),
		AgentID:  uuid.New(),
		Host:     "blocked.example.com",
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(decision.Allowed).To(BeFalse())
	g.Expect(decision.Reason).To(Equal("circuit breaker open"))
}

func TestWebhookPolicyEngineContextErrorFailsClosed(t *testing.T) {
	g := NewWithT(t)
	engine := NewWebhookPolicyEngine(WebhookPolicyConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	decision, _, err := engine.EvaluateWebhookPolicy(ctx, WebhookPolicyInput{
		TenantID: uuid.New(),
		AgentID:  uuid.New(),
		Host:     "hooks.example.com",
	})

	g.Expect(err).To(MatchError(errors.New("context canceled")))
	g.Expect(decision.Allowed).To(BeFalse())
}
