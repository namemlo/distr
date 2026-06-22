package policy

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

type WebhookPolicyConfig struct {
	TenantRequestsPerSecond   int
	AgentRequestsPerSecond    int
	AgentConcurrentExecutions int
	CorridorRequestsPerSecond map[string]int
	OpenCircuitHosts          []string
	HalfOpenCircuitHosts      []string
	HalfOpenProbeLimit        int
	MaxRetryAttempts          int
	EndpointFailureLimit      int
}

type WebhookPolicyInput struct {
	TenantID    uuid.UUID
	AgentID     uuid.UUID
	Corridor    string
	Host        string
	RetryMax    int
	Replay      bool
	Priority    string
	FailureRate float64
}

type WebhookPolicyDecision struct {
	Allowed bool
	Reason  string
}

type WebhookPolicyResult struct {
	StatusCode int
	Err        error
	Replay     bool
}

type WebhookPolicyRelease func(WebhookPolicyResult)

type WebhookPolicyEvaluator interface {
	EvaluateWebhookPolicy(context.Context, WebhookPolicyInput) (WebhookPolicyDecision, WebhookPolicyRelease, error)
}

type WebhookPolicyEngine struct {
	mu sync.Mutex

	tenantLimiter   fixedWindowRateLimiter
	agentLimiter    fixedWindowRateLimiter
	corridorLimiter map[string]fixedWindowRateLimiter
	circuits        circuitBreakerRegistry

	agentConcurrent map[string]int
	hostFailures    map[string]int
	config          WebhookPolicyConfig
}

func NewWebhookPolicyEngine(config WebhookPolicyConfig) *WebhookPolicyEngine {
	now := time.Now
	corridorLimiter := map[string]fixedWindowRateLimiter{}
	for corridor, limit := range config.CorridorRequestsPerSecond {
		if normalized := normalizePolicyToken(corridor); normalized != "" {
			corridorLimiter[normalized] = newFixedWindowRateLimiter(limit, now)
		}
	}
	return &WebhookPolicyEngine{
		tenantLimiter:   newFixedWindowRateLimiter(config.TenantRequestsPerSecond, now),
		agentLimiter:    newFixedWindowRateLimiter(config.AgentRequestsPerSecond, now),
		corridorLimiter: corridorLimiter,
		circuits:        newCircuitBreakerRegistry(config.OpenCircuitHosts, config.HalfOpenCircuitHosts, config.HalfOpenProbeLimit),
		agentConcurrent: map[string]int{},
		hostFailures:    map[string]int{},
		config:          config,
	}
}

func (e *WebhookPolicyEngine) EvaluateWebhookPolicy(ctx context.Context, input WebhookPolicyInput) (WebhookPolicyDecision, WebhookPolicyRelease, error) {
	if e == nil {
		return WebhookPolicyDecision{Allowed: true}, noopWebhookPolicyRelease, nil
	}
	if err := ctx.Err(); err != nil {
		return WebhookPolicyDecision{}, noopWebhookPolicyRelease, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	host := normalizePolicyToken(input.Host)
	if allowed, reason := e.circuits.allow(host); !allowed {
		return denyWebhookPolicy(reason), noopWebhookPolicyRelease, nil
	}
	if e.config.MaxRetryAttempts > 0 && input.RetryMax > e.config.MaxRetryAttempts {
		return denyWebhookPolicy("retry storm detected"), noopWebhookPolicyRelease, nil
	}
	if e.config.EndpointFailureLimit > 0 && host != "" && e.hostFailures[host] >= e.config.EndpointFailureLimit {
		return denyWebhookPolicy("endpoint failure threshold exceeded"), noopWebhookPolicyRelease, nil
	}
	if !e.tenantLimiter.allow(input.TenantID.String()) {
		return denyWebhookPolicy("tenant rate limit exceeded"), noopWebhookPolicyRelease, nil
	}
	if !e.agentLimiter.allow(input.AgentID.String()) {
		return denyWebhookPolicy("agent rate limit exceeded"), noopWebhookPolicyRelease, nil
	}
	corridor := normalizePolicyToken(input.Corridor)
	if limiter, ok := e.corridorLimiter[corridor]; ok {
		if !limiter.allow(corridor) {
			e.corridorLimiter[corridor] = limiter
			return denyWebhookPolicy("corridor rate limit exceeded"), noopWebhookPolicyRelease, nil
		}
		e.corridorLimiter[corridor] = limiter
	}
	agentKey := input.AgentID.String()
	acquiredAgentSlot := false
	if e.config.AgentConcurrentExecutions > 0 && agentKey != "" {
		if e.agentConcurrent[agentKey] >= e.config.AgentConcurrentExecutions {
			return denyWebhookPolicy("agent concurrency limit exceeded"), noopWebhookPolicyRelease, nil
		}
		e.agentConcurrent[agentKey]++
		acquiredAgentSlot = true
	}
	var once sync.Once
	return WebhookPolicyDecision{Allowed: true}, func(result WebhookPolicyResult) {
		once.Do(func() {
			e.recordResult(input, result, acquiredAgentSlot)
		})
	}, nil
}

func (e *WebhookPolicyEngine) recordResult(input WebhookPolicyInput, result WebhookPolicyResult, releaseAgentSlot bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if releaseAgentSlot {
		agentKey := input.AgentID.String()
		if e.agentConcurrent[agentKey] > 0 {
			e.agentConcurrent[agentKey]--
		}
	}
	host := normalizePolicyToken(input.Host)
	if host == "" || result.Replay {
		return
	}
	if result.Err != nil || result.StatusCode >= 500 {
		e.hostFailures[host]++
		return
	}
	if result.StatusCode > 0 && result.StatusCode < 500 {
		delete(e.hostFailures, host)
	}
}

func denyWebhookPolicy(reason string) WebhookPolicyDecision {
	return WebhookPolicyDecision{Allowed: false, Reason: reason}
}

func noopWebhookPolicyRelease(WebhookPolicyResult) {}

func FormatWebhookPolicyDenial(reason string) error {
	if reason == "" {
		reason = "denied"
	}
	return fmt.Errorf("webhook policy denied: %s", reason)
}
