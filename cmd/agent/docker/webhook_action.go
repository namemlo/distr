package main

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
	"github.com/distr-sh/distr/internal/types"
)

const (
	webhookAllowedHostsEnv        = "DISTR_WEBHOOK_ALLOWED_HOSTS"
	webhookAllowedPrivateHostsEnv = "DISTR_WEBHOOK_ALLOWED_PRIVATE_HOSTS"
	webhookMaxRequestBodyBytes    = 64 * 1024
	webhookMaxResponseBodyBytes   = 64 * 1024
	webhookMaxResponseHeaderBytes = 16 * 1024
	webhookMaxRetryAttempts       = 5
	webhookDefaultTimeoutSeconds  = 30
	webhookConnectTimeout         = 10 * time.Second
	webhookTLSHandshakeTimeout    = 10 * time.Second
	webhookResponseHeaderTimeout  = 10 * time.Second
	webhookBuiltInOutputCount     = 2
)

var webhookNow = time.Now
var webhookHTTPClientForTest *http.Client
var webhookLookupIPAddr = net.DefaultResolver.LookupIPAddr
var webhookDialContext = (&net.Dialer{}).DialContext
var webhookAttemptMetricSink chan<- webhookAttemptMetric
var webhookUnsafeIPPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
}

type webhookActionInput struct {
	URL                 string                     `json:"url"`
	Method              string                     `json:"method"`
	Headers             map[string]string          `json:"headers"`
	SecretHeaders       map[string]string          `json:"secretHeaders"`
	Body                any                        `json:"body"`
	SensitiveBody       bool                       `json:"sensitiveBody"`
	SigningSecret       string                     `json:"signingSecret"`
	TimeoutSeconds      int                        `json:"timeoutSeconds"`
	Retry               webhookRetryPolicy         `json:"retry"`
	ExpectedStatusCodes []int                      `json:"expectedStatusCodes"`
	IdempotencyKey      string                     `json:"idempotencyKey"`
	Outputs             []webhookOutputDeclaration `json:"outputs"`
}

type webhookRetryPolicy struct {
	MaxAttempts          int   `json:"maxAttempts"`
	BackoffSeconds       int   `json:"backoffSeconds"`
	RetryableStatusCodes []int `json:"retryableStatusCodes"`
}

type webhookOutputDeclaration struct {
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

type webhookRunResult struct {
	StatusCode      int
	Attempts        int
	Outputs         []api.AgentStepRunOutputRequest
	RedactionValues []string
}

type webhookAttemptMetric struct {
	Attempt    int
	StatusCode int
	Duration   time.Duration
	Failed     bool
}

func decodeWebhookActionInput(inputs map[string]any) (webhookActionInput, error) {
	var input webhookActionInput
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
	if strings.TrimSpace(input.SigningSecret) == "" {
		return input, fmt.Errorf("signingSecret is required")
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
	if err := validateWebhookOutputs(input.Outputs); err != nil {
		return input, err
	}
	return input, nil
}

func executeWebhookStep(
	ctx context.Context,
	lease api.AgentTaskLease,
	step api.AgentTaskLeaseStep,
	client leasedTaskClient,
) error {
	sequence := int64(1)
	if err := recordStepEvent(ctx, client, step.StepRunID, lease.LeaseToken, sequence, types.StepRunEventTypeStarted, "starting webhook", nil, nil); err != nil {
		return err
	}
	var secretValues []string
	recordFailure := func(err error) error {
		sequence++
		redactedErr := redactErrorWithSecretValues(err, secretValues)
		if recordErr := recordStepEvent(ctx, client, step.StepRunID, lease.LeaseToken, sequence, types.StepRunEventTypeFailed, redactedErr.Error(), nil, nil, secretValues...); recordErr != nil {
			return redactErrorWithSecretValues(recordErr, secretValues)
		}
		return redactedErr
	}
	if step.ActionType != webhookActionType {
		return recordFailure(fmt.Errorf("unsupported actionType %q", step.ActionType))
	}
	if step.ActionVersion != types.AgentActionVersionV1 {
		return recordFailure(fmt.Errorf("unsupported actionVersion %q", step.ActionVersion))
	}
	input, err := decodeWebhookActionInput(step.Inputs)
	if err != nil {
		return recordFailure(err)
	}
	if input.IdempotencyKey == "" {
		input.IdempotencyKey = strings.TrimSpace(step.IdempotencyKey)
	}
	if input.IdempotencyKey == "" {
		return recordFailure(fmt.Errorf("idempotencyKey is required"))
	}
	secretValues = webhookSecretValues(input)
	runCtx, runCancel := context.WithTimeout(ctx, time.Duration(input.TimeoutSeconds)*time.Second)
	defer runCancel()
	heartbeatErrCh, stopHeartbeat := startTaskLeaseHeartbeat(runCtx, lease, client, runCancel)
	emitProgress := func(message string) error {
		sequence++
		return recordStepEvent(runCtx, client, step.StepRunID, lease.LeaseToken, sequence, types.StepRunEventTypeProgress, message, nil, nil, secretValues...)
	}
	result, err := runWebhookAction(runCtx, input, emitProgress)
	secretValues = append(secretValues, result.RedactionValues...)
	stopHeartbeat()
	if heartbeatErr := taskLeaseHeartbeatError(heartbeatErrCh); heartbeatErr != nil {
		return recordFailure(heartbeatErr)
	}
	if err != nil {
		return recordFailure(err)
	}
	outputs := []api.AgentStepRunOutputRequest{
		{Name: "statusCode", Value: result.StatusCode},
		{Name: "attempts", Value: result.Attempts},
	}
	outputs = append(outputs, result.Outputs...)
	sequence++
	return recordStepEvent(ctx, client, step.StepRunID, lease.LeaseToken, sequence, types.StepRunEventTypeSucceeded, "Webhook succeeded", nil, outputs, secretValues...)
}

func runWebhookAction(
	ctx context.Context,
	input webhookActionInput,
	emitProgress func(string) error,
) (webhookRunResult, error) {
	runCtx := ctx
	if input.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(input.TimeoutSeconds)*time.Second)
		defer cancel()
	}
	policy, err := loadWebhookOutboundPolicy()
	if err != nil {
		return webhookRunResult{}, err
	}
	endpoint, err := validateWebhookTargetURL(input.URL, policy)
	if err != nil {
		return webhookRunResult{}, err
	}
	resolvedTarget, err := resolveWebhookTarget(runCtx, endpoint, policy)
	if err != nil {
		return webhookRunResult{}, err
	}
	body, err := webhookRequestBodyBytes(input.Body)
	if err != nil {
		return webhookRunResult{}, err
	}
	bodyDigest := webhookBodyDigest(body)
	timestamp := webhookNow().UTC().Format(time.RFC3339)
	signature := webhookSignature(
		input.SigningSecret,
		webhookCanonicalData(input.Method, endpoint, timestamp, input.IdempotencyKey, bodyDigest),
	)
	resultFor := func(statusCode, attempts int, outputs []api.AgentStepRunOutputRequest) webhookRunResult {
		return webhookRunResult{
			StatusCode:      statusCode,
			Attempts:        attempts,
			Outputs:         outputs,
			RedactionValues: []string{signature},
		}
	}
	client := newWebhookHTTPClient(policy, resolvedTarget)
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
			runCtx, client, input, endpoint, body, bodyDigest, timestamp, signature,
		)
		emitWebhookAttemptMetric(webhookAttemptMetric{
			Attempt:    attempt,
			StatusCode: statusCode,
			Duration:   time.Since(attemptStarted),
			Failed:     err != nil,
		})
		lastStatus = statusCode
		if err != nil {
			if runCtx.Err() != nil {
				return resultFor(lastStatus, attempt, nil), webhookContextError(runCtx, "webhook")
			}
			if isRetryableWebhookAttemptError(statusCode, err) && attempt < maxAttempts {
				if err := sleepWebhookBackoff(runCtx, input.Retry.BackoffSeconds); err != nil {
					return resultFor(lastStatus, attempt, nil), err
				}
				continue
			}
			return resultFor(lastStatus, attempt, nil), err
		}
		if _, ok := retryableStatuses[statusCode]; ok && attempt < maxAttempts {
			if err := sleepWebhookBackoff(runCtx, input.Retry.BackoffSeconds); err != nil {
				return resultFor(statusCode, attempt, nil), err
			}
			continue
		}
		if _, ok := expectedStatuses[statusCode]; ok {
			outputs, err := extractWebhookDeclaredOutputs(responseBody, input.Outputs)
			if err != nil {
				return resultFor(statusCode, attempt, nil), err
			}
			return resultFor(statusCode, attempt, outputs), nil
		}
		return resultFor(statusCode, attempt, nil), fmt.Errorf("webhook returned unexpected status %d", statusCode)
	}
	return resultFor(lastStatus, maxAttempts, nil), fmt.Errorf("webhook did not complete")
}

func emitWebhookAttemptMetric(metric webhookAttemptMetric) {
	if webhookAttemptMetricSink == nil {
		return
	}
	select {
	case webhookAttemptMetricSink <- metric:
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
	input webhookActionInput,
	endpoint *url.URL,
	body []byte,
	bodyDigest string,
	timestamp string,
	signature string,
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

func resolveWebhookTarget(ctx context.Context, endpoint *url.URL, policy webhookOutboundPolicy) (webhookResolvedTarget, error) {
	port := endpoint.Port()
	if port == "" {
		port = defaultWebhookPort(endpoint.Scheme)
	}
	ips, err := lookupWebhookTargetIPs(ctx, endpoint.Host, endpoint.Hostname(), policy)
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

func lookupWebhookTargetIPs(ctx context.Context, hostPort, host string, policy webhookOutboundPolicy) ([]net.IPAddr, error) {
	privateAllowed := policy.isPrivateHostAllowed(hostPort, host)
	if ip := net.ParseIP(host); ip != nil {
		if isUnsafeWebhookIP(ip) && !privateAllowed {
			return nil, fmt.Errorf("webhook host resolves to unsafe address")
		}
		return []net.IPAddr{{IP: ip}}, nil
	}
	ips, err := webhookLookupIPAddr(ctx, host)
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

func newWebhookHTTPClient(policy webhookOutboundPolicy, resolvedTarget webhookResolvedTarget) *http.Client {
	if webhookHTTPClientForTest != nil {
		clone := *webhookHTTPClientForTest
		clone.CheckRedirect = webhookCheckRedirect(policy)
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
			ips, err = lookupWebhookTargetIPs(ctx, address, host, policy)
			if err != nil {
				return nil, err
			}
		}
		dialCtx, cancel := context.WithTimeout(ctx, webhookConnectTimeout)
		defer cancel()
		return webhookDialContext(dialCtx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}
	return &http.Client{
		Transport:     transport,
		CheckRedirect: webhookCheckRedirect(policy),
	}
}

func webhookCheckRedirect(policy webhookOutboundPolicy) func(*http.Request, []*http.Request) error {
	return func(request *http.Request, _ []*http.Request) error {
		endpoint, err := validateWebhookTargetURL(request.URL.String(), policy)
		if err != nil {
			return err
		}
		if _, err := resolveWebhookTarget(request.Context(), endpoint, policy); err != nil {
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
	path := endpoint.EscapedPath()
	if path == "" {
		path = "/"
	}
	if query := endpoint.Query().Encode(); query != "" {
		path += "?" + query
	}
	return strings.Join([]string{
		strings.ToUpper(strings.TrimSpace(method)),
		path,
		timestamp,
		idempotencyKey,
		bodyDigest,
	}, "\n")
}

func webhookSignature(secret string, canonical string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func extractWebhookDeclaredOutputs(
	responseBody []byte,
	declarations []webhookOutputDeclaration,
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

func validateWebhookOutputs(outputs []webhookOutputDeclaration) error {
	if len(outputs) > api.MaxStepRunEventOutputItemCount-webhookBuiltInOutputCount {
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
		if isReservedWebhookOutputName(output.Name) {
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

func isReservedWebhookOutputName(name string) bool {
	switch name {
	case "statusCode", "attempts":
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
	case "idempotency-key", "x-distr-timestamp", "x-distr-body-digest", "x-distr-signature":
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

func webhookSecretValues(input webhookActionInput) []string {
	values := make([]string, 0, len(input.SecretHeaders)+1)
	if input.SigningSecret != "" {
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
