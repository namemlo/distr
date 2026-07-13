package webhookaction

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDecodeInputRejectsNonAllowlistedHost(t *testing.T) {
	t.Setenv(webhookAllowedHostsEnv, "hooks.example.com")

	_, err := DecodeInput(map[string]any{
		"url":           "https://jenkins.internal.example/job/deploy",
		"signingSecret": "secret",
	})

	if err == nil || err.Error() != "webhook host is not allowlisted" {
		t.Fatalf("expected host allowlist error, got %v", err)
	}
}

func TestRunUsesHardenedTransportAndCapturesDeclaredOutputs(t *testing.T) {
	tenantID := uuid.New()
	fixedNow := time.Date(2026, 7, 13, 9, 30, 0, 0, time.UTC)
	var received map[string]any
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Idempotency-Key") != "choice-tp-loyalty-2026.07.13.1" {
			t.Errorf("unexpected idempotency key: %q", r.Header.Get("Idempotency-Key"))
		}
		if r.Header.Get("X-Distr-Tenant-ID") != tenantID.String() {
			t.Errorf("unexpected tenant header: %q", r.Header.Get("X-Distr-Tenant-ID"))
		}
		if r.Header.Get("X-Distr-Timestamp") != fixedNow.Format(time.RFC3339) {
			t.Errorf("unexpected timestamp: %q", r.Header.Get("X-Distr-Timestamp"))
		}
		if r.Header.Get("X-Distr-Signature") == "" {
			t.Error("missing request signature")
		}
		if r.Header.Get("X-Distr-External-Execution-ID") != "external-42" {
			t.Errorf("missing runtime execution header: %q", r.Header.Get("X-Distr-External-Execution-ID"))
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"queueId":"jenkins-42","accepted":true}`))
	}))
	defer server.Close()
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(webhookAllowedHostsEnv, endpoint.Hostname())
	t.Setenv(webhookAllowedPrivateHostsEnv, endpoint.Hostname())
	input, err := DecodeInput(map[string]any{
		"url":            server.URL,
		"method":         "POST",
		"body":           map[string]any{"clientEnv": "choice-tp_dev", "service": "loyalty-api"},
		"signingSecret":  "signing-secret",
		"idempotencyKey": "choice-tp-loyalty-2026.07.13.1",
		"expectedStatusCodes": []any{
			http.StatusAccepted,
		},
		"outputs": []any{
			map[string]any{"name": "jenkinsQueueId", "pointer": "/queueId", "type": "string", "required": true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	input.TenantID = tenantID
	input.LeaseID = uuid.New()
	input.TaskID = uuid.New()
	input.StepRunID = uuid.New()
	input.RuntimeHeaders = map[string]string{"X-Distr-External-Execution-ID": "external-42"}
	var progress []string

	result, err := Run(context.Background(), input, func(message string) error {
		progress = append(progress, message)
		return nil
	}, RuntimeOptions{Now: func() time.Time { return fixedNow }, HTTPClient: server.Client()})

	if err != nil {
		t.Fatal(err)
	}
	if result.StatusCode != http.StatusAccepted || result.Attempts != 1 {
		t.Fatalf("unexpected result: status=%d attempts=%d", result.StatusCode, result.Attempts)
	}
	if len(result.Outputs) != 1 || result.Outputs[0].Name != "jenkinsQueueId" || result.Outputs[0].Value != "jenkins-42" {
		t.Fatalf("unexpected outputs: %#v", result.Outputs)
	}
	if received["clientEnv"] != "choice-tp_dev" || received["service"] != "loyalty-api" {
		t.Fatalf("unexpected body: %#v", received)
	}
	if len(progress) != 1 || len(result.AuditTrail.Events) < 3 {
		t.Fatalf("missing progress or audit evidence: progress=%v audit=%#v", progress, result.AuditTrail)
	}
}

func TestDecodeInputValidatesCallbackCompletionMode(t *testing.T) {
	t.Setenv(webhookAllowedHostsEnv, "hooks.example.com")
	input, err := DecodeInput(map[string]any{
		"url": "https://hooks.example.com/deploy", "completionMode": "callback",
		"component": "loyalty-api", "callbackTimeoutSeconds": 600, "signingSecret": "test-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if input.CompletionMode != CompletionModeCallback || input.Component != "loyalty-api" || input.CallbackTimeoutSeconds != 600 {
		t.Fatalf("unexpected callback input: %#v", input)
	}

	_, err = DecodeInput(map[string]any{
		"url": "https://hooks.example.com/deploy", "completionMode": "callback", "signingSecret": "test-secret",
	})
	if err == nil || err.Error() != "component is required for callback completion mode" {
		t.Fatalf("expected callback component error, got %v", err)
	}
}

func TestDecodeInputUsesCompletionModeOutputBudget(t *testing.T) {
	t.Setenv(webhookAllowedHostsEnv, "hooks.example.com")
	outputs := func(count int) []any {
		result := make([]any, 0, count)
		for i := 0; i < count; i++ {
			result = append(result, map[string]any{
				"name": fmt.Sprintf("remoteId%d", i), "pointer": fmt.Sprintf("/items/%d/id", i), "type": "string",
			})
		}
		return result
	}

	_, err := DecodeInput(map[string]any{
		"url": "https://hooks.example.com/deploy", "signingSecret": "test-secret",
		"completionMode": "response", "outputs": outputs(25),
	})
	if err != nil {
		t.Fatalf("response mode lost its output budget: %v", err)
	}
	_, err = DecodeInput(map[string]any{
		"url": "https://hooks.example.com/deploy", "signingSecret": "test-secret",
		"completionMode": "callback", "component": "loyalty-api", "outputs": outputs(1),
	})
	if err == nil || !strings.Contains(err.Error(), "callback completion mode") {
		t.Fatalf("expected callback output budget error, got %v", err)
	}
}
