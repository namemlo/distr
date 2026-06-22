package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

const WebhookProviderType = "webhook"

type Operation string

const (
	OperationPrepare  Operation = "prepare"
	OperationShift    Operation = "shift"
	OperationVerify   Operation = "verify"
	OperationRollback Operation = "rollback"
	OperationCleanup  Operation = "cleanup"
)

type TrafficProvider interface {
	Capability() ProviderCapability
	Prepare(context.Context, PrepareRequest) (PrepareResult, error)
	Shift(context.Context, ShiftRequest) error
	Verify(context.Context, VerifyRequest) error
	Rollback(context.Context, RollbackRequest) error
	Cleanup(context.Context, CleanupRequest) error
}

type ProviderCapability struct {
	Type       string            `json:"type"`
	Operations []Operation       `json:"operations"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

func (c ProviderCapability) Supports(operation Operation) bool {
	return slices.Contains(c.Operations, operation)
}

type RolloutContext struct {
	OrganizationID     uuid.UUID         `json:"organizationId,omitempty"`
	DeploymentPlanID   uuid.UUID         `json:"deploymentPlanId,omitempty"`
	RolloutID          uuid.UUID         `json:"rolloutId,omitempty"`
	Strategy           string            `json:"strategy,omitempty"`
	WindowNumber       int               `json:"windowNumber,omitempty"`
	IdempotencyKey     string            `json:"idempotencyKey,omitempty"`
	ProviderMetadata   map[string]string `json:"providerMetadata,omitempty"`
	RequestedAt        time.Time         `json:"requestedAt,omitempty"`
	CorrelationID      string            `json:"correlationId,omitempty"`
	ActorUserAccountID uuid.UUID         `json:"actorUserAccountId,omitempty"`
}

type TargetSet struct {
	Targets []Target `json:"targets"`
}

type Target struct {
	DeploymentPlanTargetID uuid.UUID         `json:"deploymentPlanTargetId"`
	DeploymentTargetID     uuid.UUID         `json:"deploymentTargetId"`
	Name                   string            `json:"name,omitempty"`
	Weight                 int               `json:"weight,omitempty"`
	State                  string            `json:"state,omitempty"`
	Metadata               map[string]string `json:"metadata,omitempty"`
}

type PreparedTarget struct {
	DeploymentPlanTargetID uuid.UUID         `json:"deploymentPlanTargetId"`
	ProviderTargetID       string            `json:"providerTargetId"`
	Metadata               map[string]string `json:"metadata,omitempty"`
}

type PrepareResult struct {
	PreparedTargets []PreparedTarget `json:"preparedTargets"`
}

type PrepareRequest struct {
	RolloutContext RolloutContext `json:"context"`
	Targets        TargetSet      `json:"targets"`
	Parameters     map[string]any `json:"parameters,omitempty"`
}

type ShiftRequest struct {
	RolloutContext RolloutContext `json:"context"`
	Targets        TargetSet      `json:"targets"`
	Parameters     map[string]any `json:"parameters,omitempty"`
}

type VerifyRequest struct {
	RolloutContext RolloutContext `json:"context"`
	Targets        TargetSet      `json:"targets"`
	Parameters     map[string]any `json:"parameters,omitempty"`
}

type RollbackRequest struct {
	RolloutContext RolloutContext `json:"context"`
	Targets        TargetSet      `json:"targets"`
	Parameters     map[string]any `json:"parameters,omitempty"`
}

type CleanupRequest struct {
	RolloutContext RolloutContext `json:"context"`
	Targets        TargetSet      `json:"targets"`
	Parameters     map[string]any `json:"parameters,omitempty"`
}

type ProviderConfig struct {
	Type     string
	Webhook  WebhookProviderConfig
	Metadata map[string]string
}

type ProviderFactory func(ProviderConfig) (TrafficProvider, error)

type Registry struct {
	factories map[string]ProviderFactory
}

func NewRegistry() *Registry {
	return &Registry{factories: map[string]ProviderFactory{}}
}

func DefaultRegistry() *Registry {
	registry := NewRegistry()
	if err := registry.Register(WebhookProviderType, func(config ProviderConfig) (TrafficProvider, error) {
		return NewWebhookTrafficProvider(config.Webhook)
	}); err != nil {
		panic(err)
	}
	return registry
}

func (r *Registry) Register(providerType string, factory ProviderFactory) error {
	if r == nil {
		return fmt.Errorf("traffic provider registry is nil")
	}
	if factory == nil {
		return fmt.Errorf("traffic provider factory is required")
	}
	providerType = normalizeProviderType(providerType)
	if providerType == "" {
		return fmt.Errorf("traffic provider type is required")
	}
	if _, ok := r.factories[providerType]; ok {
		return fmt.Errorf("traffic provider %q is already registered", providerType)
	}
	r.factories[providerType] = factory
	return nil
}

func (r *Registry) Build(config ProviderConfig) (TrafficProvider, error) {
	if r == nil {
		return nil, fmt.Errorf("traffic provider registry is nil")
	}
	providerType := normalizeProviderType(config.Type)
	factory, ok := r.factories[providerType]
	if !ok {
		return nil, fmt.Errorf("unknown traffic provider %q", config.Type)
	}
	return factory(config)
}

func normalizeProviderType(providerType string) string {
	return strings.ToLower(strings.TrimSpace(providerType))
}

type WebhookProviderConfig struct {
	URL               string
	Headers           map[string]string
	AllowInsecureHTTP bool
	HTTPClient        *http.Client
}

type WebhookTrafficProvider struct {
	endpoint *url.URL
	headers  map[string]string
	client   *http.Client
}

func NewWebhookTrafficProvider(config WebhookProviderConfig) (*WebhookTrafficProvider, error) {
	endpoint, err := url.Parse(strings.TrimSpace(config.URL))
	if err != nil {
		return nil, fmt.Errorf("traffic provider webhook URL is invalid: %w", err)
	}
	if endpoint.Scheme == "" || endpoint.Host == "" {
		return nil, fmt.Errorf("traffic provider webhook URL is required")
	}
	if endpoint.Scheme != "https" && !(config.AllowInsecureHTTP && endpoint.Scheme == "http") {
		return nil, fmt.Errorf("traffic provider webhook URL must use https")
	}
	client := config.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return &WebhookTrafficProvider{
		endpoint: endpoint,
		headers:  cloneStringMap(config.Headers),
		client:   client,
	}, nil
}

func (p *WebhookTrafficProvider) Capability() ProviderCapability {
	return ProviderCapability{
		Type: WebhookProviderType,
		Operations: []Operation{
			OperationPrepare,
			OperationShift,
			OperationVerify,
			OperationRollback,
			OperationCleanup,
		},
		Metadata: map[string]string{"transport": "webhook"},
	}
}

func (p *WebhookTrafficProvider) Prepare(ctx context.Context, request PrepareRequest) (PrepareResult, error) {
	var result PrepareResult
	err := p.invoke(ctx, OperationPrepare, request.RolloutContext, request.Targets, request.Parameters, &result)
	return result, err
}

func (p *WebhookTrafficProvider) Shift(ctx context.Context, request ShiftRequest) error {
	return p.invoke(ctx, OperationShift, request.RolloutContext, request.Targets, request.Parameters, nil)
}

func (p *WebhookTrafficProvider) Verify(ctx context.Context, request VerifyRequest) error {
	return p.invoke(ctx, OperationVerify, request.RolloutContext, request.Targets, request.Parameters, nil)
}

func (p *WebhookTrafficProvider) Rollback(ctx context.Context, request RollbackRequest) error {
	return p.invoke(ctx, OperationRollback, request.RolloutContext, request.Targets, request.Parameters, nil)
}

func (p *WebhookTrafficProvider) Cleanup(ctx context.Context, request CleanupRequest) error {
	return p.invoke(ctx, OperationCleanup, request.RolloutContext, request.Targets, request.Parameters, nil)
}

type webhookOperationPayload struct {
	Operation  Operation      `json:"operation"`
	Context    RolloutContext `json:"context"`
	Targets    TargetSet      `json:"targets"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

func (p *WebhookTrafficProvider) invoke(
	ctx context.Context,
	operation Operation,
	rollout RolloutContext,
	targets TargetSet,
	parameters map[string]any,
	result any,
) error {
	if p == nil {
		return fmt.Errorf("traffic provider webhook is nil")
	}
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(webhookOperationPayload{
		Operation:  operation,
		Context:    rollout,
		Targets:    targets,
		Parameters: parameters,
	}); err != nil {
		return fmt.Errorf("could not encode traffic provider webhook payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint.String(), &body)
	if err != nil {
		return fmt.Errorf("could not create traffic provider webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if rollout.IdempotencyKey != "" {
		req.Header.Set("Idempotency-Key", rollout.IdempotencyKey)
	}
	for key, value := range p.headers {
		req.Header.Set(key, value)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("traffic provider webhook request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("traffic provider webhook returned status %d", resp.StatusCode)
	}
	if result == nil {
		return nil
	}
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("could not read traffic provider webhook response: %w", err)
	}
	if len(bytes.TrimSpace(payload)) == 0 {
		return nil
	}
	if err := json.Unmarshal(payload, result); err != nil {
		return fmt.Errorf("could not decode traffic provider webhook response: %w", err)
	}
	return nil
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}
