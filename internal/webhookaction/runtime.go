package webhookaction

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/google/uuid"
)

const (
	webhookAllowedHostsEnv            = "DISTR_WEBHOOK_ALLOWED_HOSTS"
	webhookAllowedPrivateHostsEnv     = "DISTR_WEBHOOK_ALLOWED_PRIVATE_HOSTS"
	webhookStrictReplayVerifyEnv      = "STRICT_REPLAY_VERIFY"
	webhookSelfContainedModeEnv       = "WEBHOOK_SELF_CONTAINED_MODE"
	webhookResolvedIPCacheEnv         = "DISTR_WEBHOOK_RESOLVED_IP_CACHE"
	webhookTenantRPSEnv               = "DISTR_WEBHOOK_TENANT_RPS"
	webhookAgentRPSEnv                = "DISTR_WEBHOOK_AGENT_RPS"
	webhookAgentConcurrencyEnv        = "DISTR_WEBHOOK_AGENT_CONCURRENCY"
	webhookCorridorRPSEnv             = "DISTR_WEBHOOK_CORRIDOR_RPS"
	webhookOpenCircuitHostsEnv        = "DISTR_WEBHOOK_OPEN_CIRCUIT_HOSTS"
	webhookMaxRetryAttemptsEnv        = "DISTR_WEBHOOK_MAX_RETRY_ATTEMPTS"
	webhookEndpointFailureLimitEnv    = "DISTR_WEBHOOK_ENDPOINT_FAILURE_LIMIT"
	webhookMaxRequestBodyBytes        = 64 * 1024
	webhookMaxResponseBodyBytes       = 64 * 1024
	webhookMaxResponseHeaderBytes     = 16 * 1024
	webhookMaxRetryAttempts           = 5
	webhookDefaultTimeoutSeconds      = 30
	webhookDefaultCallbackSeconds     = 3600
	webhookConnectTimeout             = 10 * time.Second
	webhookTLSHandshakeTimeout        = 10 * time.Second
	webhookResponseHeaderTimeout      = 10 * time.Second
	webhookResponseBuiltInOutputCount = 7
	webhookCallbackBuiltInOutputCount = 10
	webhookMaxSigningSecrets          = 8
)

type CompletionMode string

const (
	CompletionModeResponse CompletionMode = "response"
	CompletionModeCallback CompletionMode = "callback"
)

var webhookUnsafeIPPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
}

type RuntimeOptions struct {
	Now               func() time.Time
	HTTPClient        *http.Client
	LookupIPAddr      func(context.Context, string) ([]net.IPAddr, error)
	DialContext       func(context.Context, string, string) (net.Conn, error)
	AttemptMetricSink chan<- AttemptMetric
}

func (o RuntimeOptions) withDefaults() RuntimeOptions {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.LookupIPAddr == nil {
		o.LookupIPAddr = net.DefaultResolver.LookupIPAddr
	}
	if o.DialContext == nil {
		o.DialContext = (&net.Dialer{}).DialContext
	}
	return o
}

type Input struct {
	URL                    string              `json:"url"`
	Method                 string              `json:"method"`
	Headers                map[string]string   `json:"headers"`
	SecretHeaders          map[string]string   `json:"secretHeaders"`
	Body                   any                 `json:"body"`
	SensitiveBody          bool                `json:"sensitiveBody"`
	SigningSecret          string              `json:"signingSecret"`
	SigningSecrets         []string            `json:"signingSecrets"`
	TimeoutSeconds         int                 `json:"timeoutSeconds"`
	Retry                  RetryPolicy         `json:"retry"`
	ExpectedStatusCodes    []int               `json:"expectedStatusCodes"`
	IdempotencyKey         string              `json:"idempotencyKey"`
	Corridor               string              `json:"corridor"`
	Priority               string              `json:"priority"`
	Outputs                []OutputDeclaration `json:"outputs"`
	CompletionMode         CompletionMode      `json:"completionMode"`
	Component              string              `json:"component"`
	CallbackTimeoutSeconds int                 `json:"callbackTimeoutSeconds"`
	TenantID               uuid.UUID           `json:"-"`
	LeaseID                uuid.UUID           `json:"-"`
	TaskID                 uuid.UUID           `json:"-"`
	StepRunID              uuid.UUID           `json:"-"`
	RuntimeHeaders         map[string]string   `json:"-"`
}

type RetryPolicy struct {
	MaxAttempts          int   `json:"maxAttempts"`
	BackoffSeconds       int   `json:"backoffSeconds"`
	RetryableStatusCodes []int `json:"retryableStatusCodes"`
}

type OutputDeclaration struct {
	Name      string `json:"name"`
	Pointer   string `json:"pointer"`
	Type      string `json:"type"`
	Required  bool   `json:"required"`
	Sensitive bool   `json:"sensitive"`
}

type webhookOutboundPolicy struct {
	allowedHosts        map[string]struct{}
	allowedPrivateHosts map[string]struct{}
}

type webhookResolvedTarget struct {
	host string
	port string
	ips  []net.IPAddr
}

type Result struct {
	StatusCode         int
	Attempts           int
	SigningKeyVersion  int
	KeyRotationApplied bool
	Outputs            []api.AgentStepRunOutputRequest
	RedactionValues    []string
	AuditTrail         AuditExport
}

type webhookSigningConfig struct {
	Secrets            []string
	ActiveSecret       string
	ActiveVersion      int
	KeyRotationApplied bool
}

type AttemptMetric struct {
	Attempt    int
	StatusCode int
	Duration   time.Duration
	Failed     bool
}

type AuditExport struct {
	Events []AuditEvent `json:"events"`
}

type AuditEvent struct {
	AuditEventID       string           `json:"auditEventId"`
	ParentAuditEventID string           `json:"parentAuditEventId,omitempty"`
	EventHash          string           `json:"eventHash"`
	EventType          string           `json:"eventType"`
	TenantID           string           `json:"tenantId,omitempty"`
	LeaseID            string           `json:"leaseId,omitempty"`
	TaskID             string           `json:"taskId,omitempty"`
	StepRunID          string           `json:"stepRunId,omitempty"`
	Attempt            int              `json:"attempt,omitempty"`
	StatusCode         int              `json:"statusCode,omitempty"`
	RetryReason        string           `json:"retryReason,omitempty"`
	DNS                *AuditDNSSummary `json:"dns,omitempty"`
	SigningKeyVersion  int              `json:"signingKeyVersion,omitempty"`
	KeyRotationApplied *bool            `json:"keyRotationApplied,omitempty"`
}

type AuditDNSSummary struct {
	Host                 string `json:"host"`
	Port                 string `json:"port"`
	ResolvedAddressCount int    `json:"resolvedAddressCount"`
	PrivateHostAllowed   bool   `json:"privateHostAllowed"`
}

func DecodeInput(inputs map[string]any) (Input, error) {
	var input Input
	data, err := json.Marshal(inputs)
	if err != nil {
		return input, fmt.Errorf("encode webhook inputs: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, fmt.Errorf("decode webhook inputs: %w", err)
	}
	if input.Headers == nil {
		input.Headers = map[string]string{}
	}
	if input.SecretHeaders == nil {
		input.SecretHeaders = map[string]string{}
	}
	input.URL = strings.TrimSpace(input.URL)
	input.Method = strings.ToUpper(strings.TrimSpace(input.Method))
	input.Corridor = strings.TrimSpace(input.Corridor)
	input.Priority = strings.ToLower(strings.TrimSpace(input.Priority))
	input.CompletionMode = CompletionMode(strings.ToLower(strings.TrimSpace(string(input.CompletionMode))))
	input.Component = strings.TrimSpace(input.Component)
	if input.CompletionMode == "" {
		input.CompletionMode = CompletionModeResponse
	}
	if input.CompletionMode != CompletionModeResponse && input.CompletionMode != CompletionModeCallback {
		return input, fmt.Errorf("completionMode must be response or callback")
	}
	if input.CompletionMode == CompletionModeCallback {
		if input.Component == "" {
			return input, fmt.Errorf("component is required for callback completion mode")
		}
		if input.CallbackTimeoutSeconds == 0 {
			input.CallbackTimeoutSeconds = webhookDefaultCallbackSeconds
		}
		if input.CallbackTimeoutSeconds < 1 || input.CallbackTimeoutSeconds > 86400 {
			return input, fmt.Errorf("callbackTimeoutSeconds must be between 1 and 86400")
		}
	}
	if input.Method == "" {
		input.Method = http.MethodPost
	}
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.TimeoutSeconds == 0 {
		input.TimeoutSeconds = webhookDefaultTimeoutSeconds
	}
	if len(input.ExpectedStatusCodes) == 0 {
		input.ExpectedStatusCodes = []int{http.StatusOK, http.StatusCreated, http.StatusAccepted, http.StatusNoContent}
	}
	if input.Retry.MaxAttempts == 0 {
		input.Retry.MaxAttempts = 1
	}
	if len(input.Retry.RetryableStatusCodes) == 0 {
		input.Retry.RetryableStatusCodes = []int{
			http.StatusTooManyRequests,
			http.StatusInternalServerError,
			http.StatusBadGateway,
			http.StatusServiceUnavailable,
			http.StatusGatewayTimeout,
		}
	}
	policy, err := loadWebhookOutboundPolicy()
	if err != nil {
		return input, err
	}
	if _, err := validateWebhookTargetURL(input.URL, policy); err != nil {
		return input, err
	}
	if !isSupportedWebhookMethod(input.Method) {
		return input, fmt.Errorf("method is unsupported")
	}
	if _, err := webhookSigningConfigForInput(input); err != nil {
		return input, err
	}
	if input.TimeoutSeconds < 1 {
		return input, fmt.Errorf("timeoutSeconds must be greater than 0")
	}
	if input.Retry.MaxAttempts < 1 || input.Retry.MaxAttempts > webhookMaxRetryAttempts {
		return input, fmt.Errorf("retry.maxAttempts must be between 1 and %d", webhookMaxRetryAttempts)
	}
	if input.Retry.BackoffSeconds < 0 || input.Retry.BackoffSeconds > 60 {
		return input, fmt.Errorf("retry.backoffSeconds must be between 0 and 60")
	}
	if input.IdempotencyKey != "" && !isWebhookTokenValue(input.IdempotencyKey) {
		return input, fmt.Errorf("idempotencyKey contains unsupported characters")
	}
	if input.Corridor != "" && !isWebhookTokenValue(input.Corridor) {
		return input, fmt.Errorf("corridor contains unsupported characters")
	}
	if _, err := webhookRequestBodyBytes(input.Body); err != nil {
		return input, err
	}
	if err := validateWebhookHeaders(input.Headers, input.SecretHeaders); err != nil {
		return input, err
	}
	if err := validateWebhookStatusCodes("expectedStatusCodes", input.ExpectedStatusCodes); err != nil {
		return input, err
	}
	if err := validateWebhookStatusCodes("retry.retryableStatusCodes", input.Retry.RetryableStatusCodes); err != nil {
		return input, err
	}
	if err := validateWebhookOutputs(input.Outputs, input.CompletionMode); err != nil {
		return input, err
	}
	return input, nil
}

func webhookNewAuditTrail(input Input, first AuditEvent) (AuditExport, error) {
	return webhookAppendAuditEvent(AuditExport{}, input, first)
}

func webhookAppendAttemptAudit(
	audit AuditExport,
	input Input,
	attempt int,
	statusCode int,
	retryReason string,
) AuditExport {
	next, err := webhookAppendAuditEvent(audit, input, AuditEvent{
		EventType:   "attempt",
		Attempt:     attempt,
		StatusCode:  statusCode,
		RetryReason: retryReason,
	})
	if err != nil {
		return audit
	}
	return next
}

func webhookAppendCompletedAudit(
	audit AuditExport,
	input Input,
	statusCode int,
	attempts int,
	signingConfig webhookSigningConfig,
) AuditExport {
	keyRotationApplied := signingConfig.KeyRotationApplied
	next, err := webhookAppendAuditEvent(audit, input, AuditEvent{
		EventType:          "completed",
		Attempt:            attempts,
		StatusCode:         statusCode,
		SigningKeyVersion:  signingConfig.ActiveVersion,
		KeyRotationApplied: &keyRotationApplied,
	})
	if err != nil {
		return audit
	}
	return next
}

func webhookAppendAuditEvent(audit AuditExport, input Input, event AuditEvent) (AuditExport, error) {
	event.TenantID = webhookUUIDString(input.TenantID)
	event.LeaseID = webhookUUIDString(input.LeaseID)
	event.TaskID = webhookUUIDString(input.TaskID)
	event.StepRunID = webhookUUIDString(input.StepRunID)
	if len(audit.Events) > 0 {
		event.ParentAuditEventID = audit.Events[len(audit.Events)-1].EventHash
	}
	hash, err := AuditEventHash(event)
	if err != nil {
		return audit, err
	}
	event.EventHash = hash
	event.AuditEventID = hash
	audit.Events = append(audit.Events, event)
	return audit, nil
}

func AuditOutputRequests(audit AuditExport) []api.AgentStepRunOutputRequest {
	if len(audit.Events) == 0 {
		return nil
	}
	return []api.AgentStepRunOutputRequest{
		{Name: "auditChainRoot", Value: audit.Events[0].EventHash},
		{Name: "auditEventHash", Value: audit.Events[len(audit.Events)-1].EventHash},
		{Name: "auditTrail", Value: audit},
	}
}

func webhookVerifyAuditTrail(audit AuditExport, rootHash, finalHash string, result Result) error {
	if len(audit.Events) == 0 {
		return fmt.Errorf("stored webhook audit trail is empty")
	}
	if rootHash == "" {
		return fmt.Errorf("stored webhook auditChainRoot output is missing")
	}
	if finalHash == "" {
		return fmt.Errorf("stored webhook auditEventHash output is missing")
	}
	for i := range audit.Events {
		event := audit.Events[i]
		if i == 0 {
			if event.ParentAuditEventID != "" {
				return fmt.Errorf("stored webhook audit trail parent mismatch")
			}
		} else if event.ParentAuditEventID != audit.Events[i-1].EventHash {
			return fmt.Errorf("stored webhook audit trail parent mismatch")
		}
		expectedHash, err := AuditEventHash(event)
		if err != nil {
			return err
		}
		if event.EventHash != expectedHash || event.AuditEventID != expectedHash {
			return fmt.Errorf("stored webhook audit trail hash mismatch")
		}
	}
	if rootHash != "" && rootHash != audit.Events[0].EventHash {
		return fmt.Errorf("stored webhook audit trail root mismatch")
	}
	if finalHash != "" && finalHash != audit.Events[len(audit.Events)-1].EventHash {
		return fmt.Errorf("stored webhook audit trail final hash mismatch")
	}
	final := audit.Events[len(audit.Events)-1]
	if final.EventType != "completed" {
		return fmt.Errorf("stored webhook audit trail is incomplete")
	}
	if final.StatusCode != result.StatusCode || final.Attempt != result.Attempts {
		return fmt.Errorf("stored webhook audit trail does not match success outputs")
	}
	if final.SigningKeyVersion != 0 && result.SigningKeyVersion != 0 && final.SigningKeyVersion != result.SigningKeyVersion {
		return fmt.Errorf("stored webhook audit trail does not match signing key version")
	}
	return nil
}

func AuditEventHash(event AuditEvent) (string, error) {
	payload := struct {
		ParentAuditEventID string           `json:"parentAuditEventId,omitempty"`
		EventType          string           `json:"eventType"`
		TenantID           string           `json:"tenantId,omitempty"`
		LeaseID            string           `json:"leaseId,omitempty"`
		TaskID             string           `json:"taskId,omitempty"`
		StepRunID          string           `json:"stepRunId,omitempty"`
		Attempt            int              `json:"attempt,omitempty"`
		StatusCode         int              `json:"statusCode,omitempty"`
		RetryReason        string           `json:"retryReason,omitempty"`
		DNS                *AuditDNSSummary `json:"dns,omitempty"`
		SigningKeyVersion  int              `json:"signingKeyVersion,omitempty"`
		KeyRotationApplied *bool            `json:"keyRotationApplied,omitempty"`
	}{
		ParentAuditEventID: event.ParentAuditEventID,
		EventType:          event.EventType,
		TenantID:           event.TenantID,
		LeaseID:            event.LeaseID,
		TaskID:             event.TaskID,
		StepRunID:          event.StepRunID,
		Attempt:            event.Attempt,
		StatusCode:         event.StatusCode,
		RetryReason:        event.RetryReason,
		DNS:                event.DNS,
		SigningKeyVersion:  event.SigningKeyVersion,
		KeyRotationApplied: event.KeyRotationApplied,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("webhook audit event must be valid JSON: %w", err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func webhookUUIDString(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

func webhookStrictReplayVerifyEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(webhookStrictReplayVerifyEnv)), "true")
}

func webhookSelfContainedModeEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(webhookSelfContainedModeEnv)), "true")
}

func Run(
	ctx context.Context,
	input Input,
	emitProgress func(string) error,
	runtimeOptions RuntimeOptions,
) (Result, error) {
	runtimeOptions = runtimeOptions.withDefaults()
	runCtx := ctx
	if input.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(input.TimeoutSeconds)*time.Second)
		defer cancel()
	}
	policy, err := loadWebhookOutboundPolicy()
	if err != nil {
		return Result{}, err
	}
	endpoint, err := validateWebhookTargetURL(input.URL, policy)
	if err != nil {
		return Result{}, err
	}
	resolvedTarget, err := resolveWebhookTarget(runCtx, endpoint, policy, runtimeOptions)
	if err != nil {
		return Result{}, err
	}
	auditTrail, err := webhookNewAuditTrail(input, AuditEvent{
		EventType: "resolvedTarget",
		DNS: &AuditDNSSummary{
			Host:                 normalizeWebhookHost(endpoint.Hostname()),
			Port:                 resolvedTarget.port,
			ResolvedAddressCount: len(resolvedTarget.ips),
			PrivateHostAllowed:   policy.isPrivateHostAllowed(endpoint.Host, endpoint.Hostname()),
		},
	})
	if err != nil {
		return Result{}, err
	}
	body, err := webhookRequestBodyBytes(input.Body)
	if err != nil {
		return Result{}, err
	}
	bodyDigest := webhookBodyDigest(body)
	timestamp := runtimeOptions.Now().UTC().Format(time.RFC3339)
	signingConfig, err := webhookSigningConfigForInput(input)
	if err != nil {
		return Result{}, err
	}
	signature := webhookSignature(
		signingConfig.ActiveSecret,
		webhookCanonicalDataWithTenant(input.Method, endpoint, timestamp, input.IdempotencyKey, bodyDigest, input.TenantID),
	)
	resultFor := func(statusCode, attempts int, outputs []api.AgentStepRunOutputRequest) Result {
		return Result{
			StatusCode:         statusCode,
			Attempts:           attempts,
			SigningKeyVersion:  signingConfig.ActiveVersion,
			KeyRotationApplied: signingConfig.KeyRotationApplied,
			Outputs:            outputs,
			RedactionValues:    []string{signature},
			AuditTrail:         auditTrail,
		}
	}
	client := newWebhookHTTPClient(policy, resolvedTarget, runtimeOptions)
	expectedStatuses := intSet(input.ExpectedStatusCodes)
	retryableStatuses := intSet(input.Retry.RetryableStatusCodes)
	maxAttempts := input.Retry.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	if maxAttempts > webhookMaxRetryAttempts {
		maxAttempts = webhookMaxRetryAttempts
	}
	var lastStatus int
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := emitProgress(fmt.Sprintf("sending webhook attempt %d of %d", attempt, maxAttempts)); err != nil {
			return resultFor(0, attempt, nil), err
		}
		attemptStarted := time.Now()
		responseBody, statusCode, err := sendWebhookAttempt(
			runCtx, client, input, endpoint, body, bodyDigest, timestamp, signature, signingConfig.ActiveVersion,
		)
		emitWebhookAttemptMetric(runtimeOptions, AttemptMetric{
			Attempt:    attempt,
			StatusCode: statusCode,
			Duration:   time.Since(attemptStarted),
			Failed:     err != nil,
		})
		lastStatus = statusCode
		if err != nil {
			if runCtx.Err() != nil {
				auditTrail = webhookAppendAttemptAudit(auditTrail, input, attempt, statusCode, "context canceled")
				return resultFor(lastStatus, attempt, nil), webhookContextError(runCtx, "webhook")
			}
			if isRetryableWebhookAttemptError(statusCode, err) && attempt < maxAttempts {
				auditTrail = webhookAppendAttemptAudit(auditTrail, input, attempt, statusCode, "retryable transport error")
				if err := sleepWebhookBackoff(runCtx, input.Retry.BackoffSeconds); err != nil {
					return resultFor(lastStatus, attempt, nil), err
				}
				continue
			}
			auditTrail = webhookAppendAttemptAudit(auditTrail, input, attempt, statusCode, "")
			return resultFor(lastStatus, attempt, nil), err
		}
		if _, ok := retryableStatuses[statusCode]; ok && attempt < maxAttempts {
			auditTrail = webhookAppendAttemptAudit(auditTrail, input, attempt, statusCode, fmt.Sprintf("retryable status %d", statusCode))
			if err := sleepWebhookBackoff(runCtx, input.Retry.BackoffSeconds); err != nil {
				return resultFor(statusCode, attempt, nil), err
			}
			continue
		}
		auditTrail = webhookAppendAttemptAudit(auditTrail, input, attempt, statusCode, "")
		if _, ok := expectedStatuses[statusCode]; ok {
			outputs, err := extractWebhookDeclaredOutputs(responseBody, input.Outputs)
			if err != nil {
				return resultFor(statusCode, attempt, nil), err
			}
			auditTrail = webhookAppendCompletedAudit(auditTrail, input, statusCode, attempt, signingConfig)
			return resultFor(statusCode, attempt, outputs), nil
		}
		return resultFor(statusCode, attempt, nil), fmt.Errorf("webhook returned unexpected status %d", statusCode)
	}
	return resultFor(lastStatus, maxAttempts, nil), fmt.Errorf("webhook did not complete")
}

func emitWebhookAttemptMetric(runtimeOptions RuntimeOptions, metric AttemptMetric) {
	if runtimeOptions.AttemptMetricSink == nil {
		return
	}
	select {
	case runtimeOptions.AttemptMetricSink <- metric:
	default:
	}
}

func isRetryableWebhookAttemptError(statusCode int, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && (netErr.Timeout() || netErr.Temporary()) {
		return true
	}
	if statusCode != 0 {
		return false
	}
	return false
}

func sendWebhookAttempt(
	ctx context.Context,
	client *http.Client,
	input Input,
	endpoint *url.URL,
	body []byte,
	bodyDigest string,
	timestamp string,
	signature string,
	signingKeyVersion int,
) ([]byte, int, error) {
	request, err := http.NewRequestWithContext(ctx, input.Method, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("build webhook request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	for name, value := range input.Headers {
		request.Header.Set(name, value)
	}
	for name, value := range input.SecretHeaders {
		request.Header.Set(name, value)
	}
	request.Header.Set("Idempotency-Key", input.IdempotencyKey)
	request.Header.Set("X-Distr-Timestamp", timestamp)
	request.Header.Set("X-Distr-Body-Digest", bodyDigest)
	request.Header.Set("X-Distr-Signature", signature)
	request.Header.Set("X-Distr-Key-Version", strconv.Itoa(signingKeyVersion))
	if input.TenantID != uuid.Nil {
		request.Header.Set("X-Distr-Tenant-ID", input.TenantID.String())
	}
	for name, value := range input.RuntimeHeaders {
		request.Header.Set(name, value)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, fmt.Errorf("send webhook request: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := readWebhookResponseBody(response.Body)
	if err != nil {
		return nil, response.StatusCode, err
	}
	return responseBody, response.StatusCode, nil
}

func loadWebhookOutboundPolicy() (webhookOutboundPolicy, error) {
	allowedHosts := parseWebhookHostList(os.Getenv(webhookAllowedHostsEnv))
	privateHosts := parseWebhookHostList(os.Getenv(webhookAllowedPrivateHostsEnv))
	if len(allowedHosts) == 0 && len(privateHosts) == 0 {
		return webhookOutboundPolicy{}, fmt.Errorf("%s is required", webhookAllowedHostsEnv)
	}
	for host := range privateHosts {
		allowedHosts[host] = struct{}{}
	}
	return webhookOutboundPolicy{allowedHosts: allowedHosts, allowedPrivateHosts: privateHosts}, nil
}

func parseWebhookHostList(raw string) map[string]struct{} {
	values := map[string]struct{}{}
	for _, item := range strings.Split(raw, ",") {
		host := normalizeWebhookHost(item)
		if host != "" {
			values[host] = struct{}{}
		}
	}
	return values
}

func validateWebhookTargetURL(rawURL string, policy webhookOutboundPolicy) (*url.URL, error) {
	if strings.TrimSpace(rawURL) == "" {
		return nil, fmt.Errorf("url is required")
	}
	endpoint, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("url is invalid: %w", err)
	}
	if endpoint.Scheme != "https" {
		return nil, fmt.Errorf("url must use https")
	}
	if endpoint.User != nil {
		return nil, fmt.Errorf("url must not include credentials")
	}
	if endpoint.Hostname() == "" {
		return nil, fmt.Errorf("url host is required")
	}
	if !policy.isHostAllowed(endpoint.Host, endpoint.Hostname()) {
		return nil, fmt.Errorf("webhook host is not allowlisted")
	}
	if ip := net.ParseIP(endpoint.Hostname()); ip != nil && isUnsafeWebhookIP(ip) && !policy.isPrivateHostAllowed(endpoint.Host, endpoint.Hostname()) {
		return nil, fmt.Errorf("webhook host resolves to unsafe address")
	}
	return endpoint, nil
}

func resolveWebhookTarget(
	ctx context.Context,
	endpoint *url.URL,
	policy webhookOutboundPolicy,
	runtimeOptions RuntimeOptions,
) (webhookResolvedTarget, error) {
	port := endpoint.Port()
	if port == "" {
		port = defaultWebhookPort(endpoint.Scheme)
	}
	ips, err := lookupWebhookTargetIPs(ctx, endpoint.Host, endpoint.Hostname(), policy, runtimeOptions)
	if err != nil {
		return webhookResolvedTarget{}, err
	}
	return webhookResolvedTarget{
		host: normalizeWebhookHost(endpoint.Hostname()),
		port: port,
		ips:  ips,
	}, nil
}

func defaultWebhookPort(scheme string) string {
	if scheme == "https" {
		return "443"
	}
	return ""
}

func lookupWebhookTargetIPs(
	ctx context.Context,
	hostPort, host string,
	policy webhookOutboundPolicy,
	runtimeOptions RuntimeOptions,
) ([]net.IPAddr, error) {
	privateAllowed := policy.isPrivateHostAllowed(hostPort, host)
	if ip := net.ParseIP(host); ip != nil {
		if isUnsafeWebhookIP(ip) && !privateAllowed {
			return nil, fmt.Errorf("webhook host resolves to unsafe address")
		}
		return []net.IPAddr{{IP: ip}}, nil
	}
	if webhookSelfContainedModeEnabled() {
		ips, found, err := cachedWebhookTargetIPs(hostPort, host, policy)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("webhook self-contained mode requires cached resolution for %s", normalizeWebhookHost(host))
		}
		return ips, nil
	}
	ips, err := runtimeOptions.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("webhook host did not resolve: %w", err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("webhook host did not resolve")
	}
	for _, ip := range ips {
		if isUnsafeWebhookIP(ip.IP) && !privateAllowed {
			return nil, fmt.Errorf("webhook host resolves to unsafe address")
		}
	}
	return ips, nil
}

func cachedWebhookTargetIPs(hostPort, host string, policy webhookOutboundPolicy) ([]net.IPAddr, bool, error) {
	raw := strings.TrimSpace(os.Getenv(webhookResolvedIPCacheEnv))
	if raw == "" {
		return nil, false, nil
	}
	candidates := map[string]struct{}{
		normalizeWebhookHost(hostPort): {},
		normalizeWebhookHost(host):     {},
	}
	for _, entry := range strings.FieldsFunc(raw, isWebhookResolvedIPCacheEntrySeparator) {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			return nil, false, fmt.Errorf("%s contains invalid entry", webhookResolvedIPCacheEnv)
		}
		key = normalizeWebhookHost(key)
		if _, ok := candidates[key]; !ok {
			continue
		}
		ips, err := parseCachedWebhookTargetIPs(value, policy.isPrivateHostAllowed(hostPort, host))
		if err != nil {
			return nil, false, err
		}
		return ips, true, nil
	}
	return nil, false, nil
}

func isWebhookResolvedIPCacheEntrySeparator(r rune) bool {
	switch r {
	case ',', ';', '\n', '\r':
		return true
	default:
		return false
	}
}

func parseCachedWebhookTargetIPs(value string, privateAllowed bool) ([]net.IPAddr, error) {
	var ips []net.IPAddr
	for _, item := range strings.FieldsFunc(value, isWebhookResolvedIPCacheIPSeparator) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		ip := net.ParseIP(item)
		if ip == nil {
			return nil, fmt.Errorf("%s contains invalid IP address", webhookResolvedIPCacheEnv)
		}
		if isUnsafeWebhookIP(ip) && !privateAllowed {
			return nil, fmt.Errorf("webhook host resolves to unsafe address")
		}
		ips = append(ips, net.IPAddr{IP: ip})
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("%s contains empty cached resolution", webhookResolvedIPCacheEnv)
	}
	return ips, nil
}

func isWebhookResolvedIPCacheIPSeparator(r rune) bool {
	switch r {
	case '|', ' ', '\t':
		return true
	default:
		return false
	}
}

func newWebhookHTTPClient(
	policy webhookOutboundPolicy,
	resolvedTarget webhookResolvedTarget,
	runtimeOptions RuntimeOptions,
) *http.Client {
	if runtimeOptions.HTTPClient != nil {
		clone := *runtimeOptions.HTTPClient
		clone.CheckRedirect = webhookCheckRedirect(policy, runtimeOptions)
		return &clone
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSHandshakeTimeout = webhookTLSHandshakeTimeout
	transport.ResponseHeaderTimeout = webhookResponseHeaderTimeout
	transport.MaxResponseHeaderBytes = webhookMaxResponseHeaderBytes
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if !policy.isHostAllowed(address, host) {
			return nil, fmt.Errorf("webhook host is not allowlisted")
		}
		ips := resolvedTarget.ips
		if normalizeWebhookHost(host) != resolvedTarget.host || port != resolvedTarget.port {
			ips, err = lookupWebhookTargetIPs(ctx, address, host, policy, runtimeOptions)
			if err != nil {
				return nil, err
			}
		}
		dialCtx, cancel := context.WithTimeout(ctx, webhookConnectTimeout)
		defer cancel()
		return runtimeOptions.DialContext(dialCtx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}
	return &http.Client{
		Transport:     transport,
		CheckRedirect: webhookCheckRedirect(policy, runtimeOptions),
	}
}

func webhookCheckRedirect(
	policy webhookOutboundPolicy,
	runtimeOptions RuntimeOptions,
) func(*http.Request, []*http.Request) error {
	return func(request *http.Request, _ []*http.Request) error {
		endpoint, err := validateWebhookTargetURL(request.URL.String(), policy)
		if err != nil {
			return err
		}
		if _, err := resolveWebhookTarget(request.Context(), endpoint, policy, runtimeOptions); err != nil {
			return err
		}
		return http.ErrUseLastResponse
	}
}

func (p webhookOutboundPolicy) isHostAllowed(hostPort, host string) bool {
	_, hostPortOK := p.allowedHosts[normalizeWebhookHost(hostPort)]
	_, hostOK := p.allowedHosts[normalizeWebhookHost(host)]
	return hostPortOK || hostOK
}

func (p webhookOutboundPolicy) isPrivateHostAllowed(hostPort, host string) bool {
	_, hostPortOK := p.allowedPrivateHosts[normalizeWebhookHost(hostPort)]
	_, hostOK := p.allowedPrivateHosts[normalizeWebhookHost(host)]
	return hostPortOK || hostOK
}

func normalizeWebhookHost(value string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
}

func isUnsafeWebhookIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	addr = addr.Unmap()
	return addr.IsLoopback() ||
		addr.IsPrivate() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsMulticast() ||
		addr.IsUnspecified() ||
		isWebhookUnsafePrefixIP(addr)
}

func isWebhookUnsafePrefixIP(addr netip.Addr) bool {
	for _, prefix := range webhookUnsafeIPPrefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func webhookRequestBodyBytes(body any) ([]byte, error) {
	if body == nil {
		body = map[string]any{}
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("body must be valid JSON: %w", err)
	}
	if len(data) > webhookMaxRequestBodyBytes {
		return nil, fmt.Errorf("body is too large")
	}
	return data, nil
}

func readWebhookResponseBody(reader io.Reader) ([]byte, error) {
	limited := io.LimitReader(reader, webhookMaxResponseBodyBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read webhook response body: %w", err)
	}
	if len(data) > webhookMaxResponseBodyBytes {
		return nil, fmt.Errorf("webhook response body is too large")
	}
	return data, nil
}

func webhookBodyDigest(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func webhookCanonicalData(method string, endpoint *url.URL, timestamp, idempotencyKey, bodyDigest string) string {
	return webhookCanonicalDataWithTenant(method, endpoint, timestamp, idempotencyKey, bodyDigest, uuid.Nil)
}

func webhookCanonicalDataWithTenant(method string, endpoint *url.URL, timestamp, idempotencyKey, bodyDigest string, tenantID uuid.UUID) string {
	path := endpoint.EscapedPath()
	if path == "" {
		path = "/"
	}
	if query := endpoint.Query().Encode(); query != "" {
		path += "?" + query
	}
	parts := []string{
		strings.ToUpper(strings.TrimSpace(method)),
		path,
		timestamp,
		idempotencyKey,
		bodyDigest,
	}
	if tenantID != uuid.Nil {
		parts = append(parts, "tenant:"+tenantID.String())
	}
	return strings.Join(parts, "\n")
}

func webhookSignature(secret string, canonical string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func webhookSigningConfigForInput(input Input) (webhookSigningConfig, error) {
	if len(input.SigningSecrets) > 0 {
		if strings.TrimSpace(input.SigningSecret) != "" {
			return webhookSigningConfig{}, fmt.Errorf("signingSecret and signingSecrets cannot both be set")
		}
		return webhookSigningConfigFromSecrets(input.SigningSecrets)
	}
	if strings.TrimSpace(input.SigningSecret) == "" {
		return webhookSigningConfig{}, fmt.Errorf("signingSecret or signingSecrets is required")
	}
	return webhookSigningConfig{
		Secrets:            []string{input.SigningSecret},
		ActiveSecret:       input.SigningSecret,
		ActiveVersion:      1,
		KeyRotationApplied: false,
	}, nil
}

func webhookSigningConfigFromSecrets(secrets []string) (webhookSigningConfig, error) {
	if len(secrets) == 0 {
		return webhookSigningConfig{}, fmt.Errorf("signingSecrets is required")
	}
	if len(secrets) > webhookMaxSigningSecrets {
		return webhookSigningConfig{}, fmt.Errorf("signingSecrets contains too many entries")
	}
	seen := map[string]struct{}{}
	for _, secret := range secrets {
		if strings.TrimSpace(secret) == "" {
			return webhookSigningConfig{}, fmt.Errorf("signingSecrets contains empty secret")
		}
		if _, ok := seen[secret]; ok {
			return webhookSigningConfig{}, fmt.Errorf("signingSecrets contains duplicate secret")
		}
		seen[secret] = struct{}{}
	}
	activeVersion := len(secrets)
	return webhookSigningConfig{
		Secrets:            append([]string(nil), secrets...),
		ActiveSecret:       secrets[activeVersion-1],
		ActiveVersion:      activeVersion,
		KeyRotationApplied: activeVersion > 1,
	}, nil
}

func webhookVerifySignature(secrets []string, keyVersion string, canonicalData string, signature string) (int, bool) {
	signingConfig, err := webhookSigningConfigFromSecrets(secrets)
	if err != nil {
		return 0, false
	}
	keyVersion = strings.TrimSpace(keyVersion)
	if keyVersion != "" {
		version, err := strconv.Atoi(keyVersion)
		if err != nil || version < 1 || version > len(signingConfig.Secrets) {
			return 0, false
		}
		return version, webhookSignatureMatches(signingConfig.Secrets[version-1], canonicalData, signature)
	}
	if len(signingConfig.Secrets) > 1 {
		return 0, false
	}
	if webhookSignatureMatches(signingConfig.ActiveSecret, canonicalData, signature) {
		return signingConfig.ActiveVersion, true
	}
	for i := len(signingConfig.Secrets) - 2; i >= 0; i-- {
		if webhookSignatureMatches(signingConfig.Secrets[i], canonicalData, signature) {
			return i + 1, true
		}
	}
	return 0, false
}

func webhookSignatureMatches(secret string, canonicalData string, signature string) bool {
	expected := webhookSignature(secret, canonicalData)
	return hmac.Equal([]byte(expected), []byte(signature))
}

func extractWebhookDeclaredOutputs(
	responseBody []byte,
	declarations []OutputDeclaration,
) ([]api.AgentStepRunOutputRequest, error) {
	if len(declarations) == 0 {
		return nil, nil
	}
	if len(bytes.TrimSpace(responseBody)) == 0 {
		responseBody = []byte("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(responseBody))
	decoder.UseNumber()
	var payload any
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode webhook response JSON: %w", err)
	}
	outputs := make([]api.AgentStepRunOutputRequest, 0, len(declarations))
	for _, declaration := range declarations {
		value, found, err := webhookJSONPointer(payload, declaration.Pointer)
		if err != nil {
			return nil, fmt.Errorf("extract output %q: %w", declaration.Name, err)
		}
		if !found {
			if declaration.Required {
				return nil, fmt.Errorf("required output %q is missing", declaration.Name)
			}
			continue
		}
		if !webhookOutputTypeMatches(value, declaration.Type) {
			return nil, fmt.Errorf("output %q does not match type %q", declaration.Name, declaration.Type)
		}
		output := api.AgentStepRunOutputRequest{Name: declaration.Name, Sensitive: declaration.Sensitive}
		if !declaration.Sensitive {
			output.Value = value
		}
		outputs = append(outputs, output)
	}
	return outputs, nil
}

func webhookJSONPointer(payload any, pointer string) (any, bool, error) {
	if pointer == "" || !strings.HasPrefix(pointer, "/") {
		return nil, false, fmt.Errorf("pointer must start with /")
	}
	current := payload
	for _, rawToken := range strings.Split(pointer[1:], "/") {
		token := strings.ReplaceAll(strings.ReplaceAll(rawToken, "~1", "/"), "~0", "~")
		switch typed := current.(type) {
		case map[string]any:
			value, ok := typed[token]
			if !ok {
				return nil, false, nil
			}
			current = value
		case []any:
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(typed) {
				return nil, false, nil
			}
			current = typed[index]
		default:
			return nil, false, nil
		}
	}
	return current, true, nil
}

func webhookOutputTypeMatches(value any, expected string) bool {
	switch expected {
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		switch value.(type) {
		case json.Number, float64, int, int64:
			return true
		default:
			return false
		}
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	default:
		return false
	}
}

func validateWebhookHeaders(publicHeaders, secretHeaders map[string]string) error {
	seen := map[string]string{}
	for name := range publicHeaders {
		if !isWebhookHeaderName(name) {
			return fmt.Errorf("headers contains invalid header name %q", name)
		}
		if isWebhookSensitiveHeaderName(name) {
			return fmt.Errorf("headers cannot include %s; use secretHeaders", name)
		}
		if isWebhookReservedHeaderName(name) {
			return fmt.Errorf("headers cannot include reserved header %s", name)
		}
		seen[strings.ToLower(name)] = "headers"
	}
	for name, value := range secretHeaders {
		if !isWebhookHeaderName(name) {
			return fmt.Errorf("secretHeaders contains invalid header name %q", name)
		}
		if isWebhookReservedHeaderName(name) {
			return fmt.Errorf("secretHeaders cannot include reserved header %s", name)
		}
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("secretHeaders value must be a secret reference")
		}
		normalized := strings.ToLower(name)
		if previous, ok := seen[normalized]; ok {
			return fmt.Errorf("secretHeaders conflicts with %s for header %s", previous, name)
		}
		seen[normalized] = "secretHeaders"
	}
	return nil
}

func validateWebhookOutputs(outputs []OutputDeclaration, completionMode CompletionMode) error {
	if completionMode == CompletionModeCallback && len(outputs) > 0 {
		return fmt.Errorf("outputs are not supported in callback completion mode; report durable values in the callback")
	}
	builtInOutputCount := webhookResponseBuiltInOutputCount
	if completionMode == CompletionModeCallback {
		builtInOutputCount = webhookCallbackBuiltInOutputCount
	}
	if len(outputs) > api.MaxStepRunEventOutputItemCount-builtInOutputCount {
		return fmt.Errorf("outputs contains too many entries")
	}
	seen := map[string]struct{}{}
	for _, output := range outputs {
		output.Name = strings.TrimSpace(output.Name)
		output.Pointer = strings.TrimSpace(output.Pointer)
		output.Type = strings.TrimSpace(output.Type)
		if output.Name == "" {
			return fmt.Errorf("outputs name is required")
		}
		if isReservedWebhookOutputName(output.Name, completionMode == CompletionModeCallback) {
			return fmt.Errorf("outputs name %s is reserved", output.Name)
		}
		if _, ok := seen[output.Name]; ok {
			return fmt.Errorf("outputs contains duplicate name")
		}
		seen[output.Name] = struct{}{}
		if !strings.HasPrefix(output.Pointer, "/") {
			return fmt.Errorf("outputs pointer must start with /")
		}
		if !isSupportedWebhookOutputType(output.Type) {
			return fmt.Errorf("outputs type is unsupported")
		}
	}
	return nil
}

func isReservedWebhookOutputName(name string, callback bool) bool {
	switch name {
	case "statusCode", "attempts", "signingKeyVersion", "keyRotationApplied", "auditChainRoot", "auditEventHash", "auditTrail":
		return true
	}
	if !callback {
		return false
	}
	switch name {
	case "externalExecutionId", "providerReference", "providerUrl", "actualVersion", "actualImage", "actualPlatform",
		"actualConfigReference", "actualConfigChecksum", "actualHealth", "observedStateChecksum":
		return true
	default:
		return false
	}
}

func validateWebhookStatusCodes(label string, values []int) error {
	seen := map[int]struct{}{}
	for _, value := range values {
		if value < 100 || value > 599 {
			return fmt.Errorf("%s contains invalid status code", label)
		}
		if _, ok := seen[value]; ok {
			return fmt.Errorf("%s contains duplicate status code", label)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func isSupportedWebhookMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func isSupportedWebhookOutputType(value string) bool {
	switch value {
	case "string", "number", "boolean", "object", "array":
		return true
	default:
		return false
	}
}

func isWebhookHeaderName(name string) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	for _, r := range name {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		switch r {
		case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
			continue
		default:
			return false
		}
	}
	return true
}

func isWebhookSensitiveHeaderName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "authorization", "proxy-authorization", "x-api-key", "cookie":
		return true
	default:
		return false
	}
}

func isWebhookReservedHeaderName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "idempotency-key", "x-distr-timestamp", "x-distr-body-digest", "x-distr-signature", "x-distr-key-version", "x-distr-tenant-id":
		return true
	default:
		return false
	}
}

func isWebhookTokenValue(value string) bool {
	for _, r := range value {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		switch r {
		case '_', '.', ':', '-':
			continue
		default:
			return false
		}
	}
	return true
}

func intSet(values []int) map[int]struct{} {
	set := make(map[int]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func SecretValues(input Input) []string {
	values := make([]string, 0, len(input.SecretHeaders)+1+len(input.SigningSecrets))
	for _, secret := range input.SigningSecrets {
		if secret != "" {
			values = appendWebhookSecretValue(values, secret)
		}
	}
	if len(input.SigningSecrets) == 0 && input.SigningSecret != "" {
		values = appendWebhookSecretValue(values, input.SigningSecret)
	}
	for _, value := range input.SecretHeaders {
		if value != "" {
			values = appendWebhookSecretValue(values, value)
		}
	}
	return values
}

func appendWebhookSecretValue(values []string, value string) []string {
	values = append(values, value)
	trimmed := strings.TrimSpace(value)
	if trimmed != "" && trimmed != value {
		values = append(values, trimmed)
	}
	return values
}

func sleepWebhookBackoff(ctx context.Context, seconds int) error {
	if seconds <= 0 {
		return nil
	}
	timer := time.NewTimer(time.Duration(seconds) * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return webhookContextError(ctx, "webhook")
	case <-timer.C:
		return nil
	}
}

func webhookContextError(ctx context.Context, label string) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%s timed out", label)
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return fmt.Errorf("%s canceled", label)
	}
	return ctx.Err()
}
