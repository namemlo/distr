package webhookaction

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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
