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
	"github.com/google/uuid"
)

const (
	webhookAllowedHostsEnv        = "DISTR_WEBHOOK_ALLOWED_HOSTS"
	webhookAllowedPrivateHostsEnv = "DISTR_WEBHOOK_ALLOWED_PRIVATE_HOSTS"
	webhookStrictReplayVerifyEnv  = "STRICT_REPLAY_VERIFY"
	webhookSelfContainedModeEnv   = "WEBHOOK_SELF_CONTAINED_MODE"
	webhookResolvedIPCacheEnv     = "DISTR_WEBHOOK_RESOLVED_IP_CACHE"
	webhookMaxRequestBodyBytes    = 64 * 1024
	webhookMaxResponseBodyBytes   = 64 * 1024
	webhookMaxResponseHeaderBytes = 16 * 1024
	webhookMaxRetryAttempts       = 5
	webhookDefaultTimeoutSeconds  = 30
	webhookConnectTimeout         = 10 * time.Second
	webhookTLSHandshakeTimeout    = 10 * time.Second
	webhookResponseHeaderTimeout  = 10 * time.Second
	webhookBuiltInOutputCount     = 7
	webhookMaxSigningSecrets      = 8
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
	SigningSecrets      []string                   `json:"signingSecrets"`
	TimeoutSeconds      int                        `json:"timeoutSeconds"`
	Retry               webhookRetryPolicy         `json:"retry"`
	ExpectedStatusCodes []int                      `json:"expectedStatusCodes"`
	IdempotencyKey      string                     `json:"idempotencyKey"`
	Outputs             []webhookOutputDeclaration `json:"outputs"`
	TenantID            uuid.UUID                  `json:"-"`
	LeaseID             uuid.UUID                  `json:"-"`
	TaskID              uuid.UUID                  `json:"-"`
	StepRunID           uuid.UUID                  `json:"-"`
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
	StatusCode         int
	Attempts           int
	SigningKeyVersion  int
	KeyRotationApplied bool
	Outputs            []api.AgentStepRunOutputRequest
	RedactionValues    []string
	AuditTrail         webhookAuditExport
}

type webhookSigningConfig struct {
	Secrets            []string
	ActiveSecret       string
	ActiveVersion      int
	KeyRotationApplied bool
}

type webhookAttemptMetric struct {
	Attempt    int
	StatusCode int
	Duration   time.Duration
	Failed     bool
}

type localTaskTimelineClient interface {
	LocalTaskTimeline(context.Context, uuid.UUID, uuid.UUID) (*api.TaskTimeline, error)
}

type selfContainedLeasedTaskClient struct {
	delegate leasedTaskClient
	lease    api.AgentTaskLease
	events   []api.StepRunEvent
}

func newSelfContainedLeasedTaskClient(delegate leasedTaskClient, lease api.AgentTaskLease) leasedTaskClient {
	return &selfContainedLeasedTaskClient{
		delegate: delegate,
		lease:    lease,
	}
}

func (c *selfContainedLeasedTaskClient) HeartbeatTaskLease(ctx context.Context, taskID uuid.UUID, leaseToken string) (*api.AgentTaskLease, error) {
	return c.delegate.HeartbeatTaskLease(ctx, taskID, leaseToken)
}

func (c *selfContainedLeasedTaskClient) RecordStepRunEvent(
	ctx context.Context,
	stepRunID uuid.UUID,
	request api.AgentStepRunEventRequest,
) (*api.StepRunEvent, error) {
	event, err := c.delegate.RecordStepRunEvent(ctx, stepRunID, request)
	if err != nil {
		return nil, err
	}
	occurredAt := time.Now().UTC()
	if request.OccurredAt != nil {
		occurredAt = request.OccurredAt.UTC()
	}
	c.events = append(c.events, api.StepRunEvent{
		ID:              uuid.New(),
		OccurredAt:      occurredAt,
		OrganizationID:  c.lease.OrganizationID,
		TaskID:          c.lease.TaskID,
		StepRunID:       stepRunID,
		TaskLeaseID:     c.lease.ID,
		AgentID:         c.lease.AgentID,
		Sequence:        request.Sequence,
		Type:            request.Type,
		Message:         request.Message,
		ProgressPercent: request.ProgressPercent,
		Details:         request.Details,
		Outputs:         webhookStepRunOutputsFromRequests(request.Outputs),
	})
	return event, nil
}

func (c *selfContainedLeasedTaskClient) LocalTaskTimeline(_ context.Context, taskID uuid.UUID, leaseID uuid.UUID) (*api.TaskTimeline, error) {
	timeline := &api.TaskTimeline{
		OrganizationID: c.lease.OrganizationID,
		TaskID:         taskID,
	}
	if taskID != c.lease.TaskID || (leaseID != uuid.Nil && c.lease.ID != uuid.Nil && leaseID != c.lease.ID) {
		return timeline, nil
	}
	timeline.Events = append(timeline.Events, c.events...)
	return timeline, nil
}

func webhookStepRunOutputsFromRequests(outputs []api.AgentStepRunOutputRequest) []api.StepRunOutput {
	if len(outputs) == 0 {
		return nil
	}
	values := make([]api.StepRunOutput, 0, len(outputs))
	for _, output := range outputs {
		data, err := json.Marshal(output.Value)
		if err != nil {
			data = []byte("null")
		}
		values = append(values, api.StepRunOutput{
			ID:        uuid.New(),
			Name:      output.Name,
			Value:     data,
			Sensitive: output.Sensitive,
			Redacted:  output.Sensitive,
		})
	}
	return values
}

type webhookAuditExport struct {
	Events []webhookAuditEvent `json:"events"`
}

type webhookAuditEvent struct {
	AuditEventID       string                  `json:"auditEventId"`
	ParentAuditEventID string                  `json:"parentAuditEventId,omitempty"`
	EventHash          string                  `json:"eventHash"`
	EventType          string                  `json:"eventType"`
	TenantID           string                  `json:"tenantId,omitempty"`
	LeaseID            string                  `json:"leaseId,omitempty"`
	TaskID             string                  `json:"taskId,omitempty"`
	StepRunID          string                  `json:"stepRunId,omitempty"`
	Attempt            int                     `json:"attempt,omitempty"`
	StatusCode         int                     `json:"statusCode,omitempty"`
	RetryReason        string                  `json:"retryReason,omitempty"`
	DNS                *webhookAuditDNSSummary `json:"dns,omitempty"`
	SigningKeyVersion  int                     `json:"signingKeyVersion,omitempty"`
	KeyRotationApplied *bool                   `json:"keyRotationApplied,omitempty"`
}

type webhookAuditDNSSummary struct {
	Host                 string `json:"host"`
	Port                 string `json:"port"`
	ResolvedAddressCount int    `json:"resolvedAddressCount"`
	PrivateHostAllowed   bool   `json:"privateHostAllowed"`
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
	if step.ActionType == webhookActionType && step.ActionVersion == types.AgentActionVersionV1 {
		if _, replayed, err := webhookReplayResult(ctx, lease, step.StepRunID, client); err != nil {
			return err
		} else if replayed {
			return nil
		}
	}
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
	input.TenantID = lease.OrganizationID
	input.LeaseID = lease.ID
	input.TaskID = lease.TaskID
	input.StepRunID = step.StepRunID
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
		{Name: "signingKeyVersion", Value: result.SigningKeyVersion},
		{Name: "keyRotationApplied", Value: result.KeyRotationApplied},
	}
	outputs = append(outputs, webhookAuditOutputRequests(result.AuditTrail)...)
	outputs = append(outputs, result.Outputs...)
	sequence++
	return recordStepEvent(ctx, client, step.StepRunID, lease.LeaseToken, sequence, types.StepRunEventTypeSucceeded, "Webhook succeeded", nil, outputs, secretValues...)
}

func webhookReplayResult(
	ctx context.Context,
	lease api.AgentTaskLease,
	stepRunID uuid.UUID,
	client leasedTaskClient,
) (webhookRunResult, bool, error) {
	if webhookSelfContainedModeEnabled() {
		localTimelineClient, ok := client.(localTaskTimelineClient)
		if !ok {
			return webhookRunResult{}, false, nil
		}
		timeline, err := localTimelineClient.LocalTaskTimeline(ctx, lease.TaskID, lease.ID)
		if err != nil {
			return webhookRunResult{}, false, err
		}
		return webhookReplayResultFromTimelineForLease(timeline, lease.OrganizationID, lease.AgentID, lease.TaskID, stepRunID, lease.ID)
	}
	timelineClient, ok := client.(taskTimelineClient)
	if !ok {
		return webhookRunResult{}, false, nil
	}
	timeline, err := timelineClient.GetTaskTimeline(ctx, lease.TaskID, lease.ID)
	if err != nil {
		return webhookRunResult{}, false, err
	}
	return webhookReplayResultFromTimelineForLease(timeline, lease.OrganizationID, lease.AgentID, lease.TaskID, stepRunID, lease.ID)
}

func webhookReplayResultFromTimeline(
	timeline *api.TaskTimeline,
	stepRunID uuid.UUID,
) (webhookRunResult, bool, error) {
	return webhookReplayResultFromTimelineForLease(timeline, uuid.Nil, uuid.Nil, uuid.Nil, stepRunID, uuid.Nil)
}

func webhookReplayResultFromTimelineForLease(
	timeline *api.TaskTimeline,
	organizationID uuid.UUID,
	agentID uuid.UUID,
	taskID uuid.UUID,
	stepRunID uuid.UUID,
	leaseID uuid.UUID,
) (webhookRunResult, bool, error) {
	if timeline == nil {
		return webhookRunResult{}, false, nil
	}
	if organizationID != uuid.Nil && timeline.OrganizationID != uuid.Nil && timeline.OrganizationID != organizationID {
		return webhookRunResult{}, false, fmt.Errorf("stored webhook replay organization does not match active lease")
	}
	var success *api.StepRunEvent
	var incomplete bool
	var failed bool
	for i := range timeline.Events {
		event := &timeline.Events[i]
		if event.StepRunID != stepRunID {
			continue
		}
		if organizationID != uuid.Nil && event.OrganizationID != uuid.Nil && event.OrganizationID != organizationID {
			return webhookRunResult{}, false, fmt.Errorf("stored webhook replay organization does not match active lease")
		}
		if agentID != uuid.Nil && event.AgentID != uuid.Nil && event.AgentID != agentID {
			return webhookRunResult{}, false, fmt.Errorf("stored webhook replay agent does not match active lease")
		}
		if taskID != uuid.Nil && event.TaskID != uuid.Nil && event.TaskID != taskID {
			return webhookRunResult{}, false, fmt.Errorf("stored webhook replay task does not match active lease")
		}
		if leaseID != uuid.Nil && event.TaskLeaseID != uuid.Nil && event.TaskLeaseID != leaseID {
			return webhookRunResult{}, false, fmt.Errorf("stored webhook replay lease does not match active lease")
		}
		switch event.Type {
		case types.StepRunEventTypeSucceeded:
			success = event
		case types.StepRunEventTypeFailed:
			failed = true
		case types.StepRunEventTypeStarted,
			types.StepRunEventTypeProgress,
			types.StepRunEventTypeLog,
			types.StepRunEventTypeOutput:
			incomplete = true
		}
	}
	if success == nil {
		if failed {
			return webhookRunResult{}, false, fmt.Errorf("webhook replay is already failed; refusing to re-execute external request")
		}
		if incomplete {
			return webhookRunResult{}, false, fmt.Errorf("webhook replay is incomplete; refusing to re-execute external request")
		}
		return webhookRunResult{}, false, nil
	}
	result := webhookRunResult{}
	hasStatusCode := false
	hasAttempts := false
	var auditChainRoot string
	var auditEventHash string
	hasAuditTrail := false
	for _, output := range success.Outputs {
		value, err := webhookStoredOutputValue(output.Value)
		if err != nil {
			return webhookRunResult{}, false, err
		}
		var ok bool
		switch output.Name {
		case "statusCode":
			statusCode, ok := webhookStoredIntOutput(value)
			if !ok {
				return webhookRunResult{}, false, fmt.Errorf("stored webhook statusCode output is invalid")
			}
			result.StatusCode = statusCode
			hasStatusCode = true
		case "attempts":
			attempts, ok := webhookStoredIntOutput(value)
			if !ok {
				return webhookRunResult{}, false, fmt.Errorf("stored webhook attempts output is invalid")
			}
			result.Attempts = attempts
			hasAttempts = true
		case "signingKeyVersion":
			signingKeyVersion, ok := webhookStoredIntOutput(value)
			if !ok {
				return webhookRunResult{}, false, fmt.Errorf("stored webhook signingKeyVersion output is invalid")
			}
			result.SigningKeyVersion = signingKeyVersion
		case "keyRotationApplied":
			keyRotationApplied, ok := webhookStoredBoolOutput(value)
			if !ok {
				return webhookRunResult{}, false, fmt.Errorf("stored webhook keyRotationApplied output is invalid")
			}
			result.KeyRotationApplied = keyRotationApplied
		case "auditChainRoot":
			auditChainRoot, ok = webhookStoredStringOutput(value)
			if !ok {
				return webhookRunResult{}, false, fmt.Errorf("stored webhook auditChainRoot output is invalid")
			}
		case "auditEventHash":
			auditEventHash, ok = webhookStoredStringOutput(value)
			if !ok {
				return webhookRunResult{}, false, fmt.Errorf("stored webhook auditEventHash output is invalid")
			}
		case "auditTrail":
			if err := webhookDecodeStoredOutput(output.Value, &result.AuditTrail); err != nil {
				return webhookRunResult{}, false, fmt.Errorf("stored webhook auditTrail output is invalid: %w", err)
			}
			hasAuditTrail = true
		default:
			result.Outputs = append(result.Outputs, api.AgentStepRunOutputRequest{
				Name:      output.Name,
				Value:     value,
				Sensitive: output.Sensitive,
			})
		}
	}
	if !hasStatusCode || !hasAttempts {
		return webhookRunResult{}, false, fmt.Errorf("stored webhook success is missing built-in outputs")
	}
	if hasAuditTrail {
		if err := webhookVerifyAuditTrail(result.AuditTrail, auditChainRoot, auditEventHash, result); err != nil {
			return webhookRunResult{}, false, err
		}
	} else if webhookStrictReplayVerifyEnabled() {
		return webhookRunResult{}, false, fmt.Errorf("stored webhook audit trail is missing")
	}
	return result, true, nil
}

func webhookStoredOutputValue(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode stored webhook output: %w", err)
	}
	return value, nil
}

func webhookStoredIntOutput(value any) (int, bool) {
	switch typed := value.(type) {
	case json.Number:
		number, err := typed.Int64()
		if err != nil {
			return 0, false
		}
		return int(number), true
	case float64:
		number := int(typed)
		return number, typed == float64(number)
	case int:
		return typed, true
	default:
		return 0, false
	}
}

func webhookStoredBoolOutput(value any) (bool, bool) {
	typed, ok := value.(bool)
	return typed, ok
}

func webhookStoredStringOutput(value any) (string, bool) {
	typed, ok := value.(string)
	return typed, ok
}

func webhookDecodeStoredOutput(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		return fmt.Errorf("empty output")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return decoder.Decode(target)
}

func webhookNewAuditTrail(input webhookActionInput, first webhookAuditEvent) (webhookAuditExport, error) {
	return webhookAppendAuditEvent(webhookAuditExport{}, input, first)
}

func webhookAppendAttemptAudit(
	audit webhookAuditExport,
	input webhookActionInput,
	attempt int,
	statusCode int,
	retryReason string,
) webhookAuditExport {
	next, err := webhookAppendAuditEvent(audit, input, webhookAuditEvent{
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
	audit webhookAuditExport,
	input webhookActionInput,
	statusCode int,
	attempts int,
	signingConfig webhookSigningConfig,
) webhookAuditExport {
	keyRotationApplied := signingConfig.KeyRotationApplied
	next, err := webhookAppendAuditEvent(audit, input, webhookAuditEvent{
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

func webhookAppendAuditEvent(audit webhookAuditExport, input webhookActionInput, event webhookAuditEvent) (webhookAuditExport, error) {
	event.TenantID = webhookUUIDString(input.TenantID)
	event.LeaseID = webhookUUIDString(input.LeaseID)
	event.TaskID = webhookUUIDString(input.TaskID)
	event.StepRunID = webhookUUIDString(input.StepRunID)
	if len(audit.Events) > 0 {
		event.ParentAuditEventID = audit.Events[len(audit.Events)-1].EventHash
	}
	hash, err := webhookAuditEventHash(event)
	if err != nil {
		return audit, err
	}
	event.EventHash = hash
	event.AuditEventID = hash
	audit.Events = append(audit.Events, event)
	return audit, nil
}

func webhookAuditOutputRequests(audit webhookAuditExport) []api.AgentStepRunOutputRequest {
	if len(audit.Events) == 0 {
		return nil
	}
	return []api.AgentStepRunOutputRequest{
		{Name: "auditChainRoot", Value: audit.Events[0].EventHash},
		{Name: "auditEventHash", Value: audit.Events[len(audit.Events)-1].EventHash},
		{Name: "auditTrail", Value: audit},
	}
}

func webhookVerifyAuditTrail(audit webhookAuditExport, rootHash, finalHash string, result webhookRunResult) error {
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
		expectedHash, err := webhookAuditEventHash(event)
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

func webhookAuditEventHash(event webhookAuditEvent) (string, error) {
	payload := struct {
		ParentAuditEventID string                  `json:"parentAuditEventId,omitempty"`
		EventType          string                  `json:"eventType"`
		TenantID           string                  `json:"tenantId,omitempty"`
		LeaseID            string                  `json:"leaseId,omitempty"`
		TaskID             string                  `json:"taskId,omitempty"`
		StepRunID          string                  `json:"stepRunId,omitempty"`
		Attempt            int                     `json:"attempt,omitempty"`
		StatusCode         int                     `json:"statusCode,omitempty"`
		RetryReason        string                  `json:"retryReason,omitempty"`
		DNS                *webhookAuditDNSSummary `json:"dns,omitempty"`
		SigningKeyVersion  int                     `json:"signingKeyVersion,omitempty"`
		KeyRotationApplied *bool                   `json:"keyRotationApplied,omitempty"`
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
	auditTrail, err := webhookNewAuditTrail(input, webhookAuditEvent{
		EventType: "resolvedTarget",
		DNS: &webhookAuditDNSSummary{
			Host:                 normalizeWebhookHost(endpoint.Hostname()),
			Port:                 resolvedTarget.port,
			ResolvedAddressCount: len(resolvedTarget.ips),
			PrivateHostAllowed:   policy.isPrivateHostAllowed(endpoint.Host, endpoint.Hostname()),
		},
	})
	if err != nil {
		return webhookRunResult{}, err
	}
	body, err := webhookRequestBodyBytes(input.Body)
	if err != nil {
		return webhookRunResult{}, err
	}
	bodyDigest := webhookBodyDigest(body)
	timestamp := webhookNow().UTC().Format(time.RFC3339)
	signingConfig, err := webhookSigningConfigForInput(input)
	if err != nil {
		return webhookRunResult{}, err
	}
	signature := webhookSignature(
		signingConfig.ActiveSecret,
		webhookCanonicalDataWithTenant(input.Method, endpoint, timestamp, input.IdempotencyKey, bodyDigest, input.TenantID),
	)
	resultFor := func(statusCode, attempts int, outputs []api.AgentStepRunOutputRequest) webhookRunResult {
		return webhookRunResult{
			StatusCode:         statusCode,
			Attempts:           attempts,
			SigningKeyVersion:  signingConfig.ActiveVersion,
			KeyRotationApplied: signingConfig.KeyRotationApplied,
			Outputs:            outputs,
			RedactionValues:    []string{signature},
			AuditTrail:         auditTrail,
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
			runCtx, client, input, endpoint, body, bodyDigest, timestamp, signature, signingConfig.ActiveVersion,
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

func webhookSigningConfigForInput(input webhookActionInput) (webhookSigningConfig, error) {
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
	case "statusCode", "attempts", "signingKeyVersion", "keyRotationApplied", "auditChainRoot", "auditEventHash", "auditTrail":
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

func webhookSecretValues(input webhookActionInput) []string {
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
