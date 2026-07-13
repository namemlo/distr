package hubexecutor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/webhookaction"
	"github.com/google/uuid"
	"go.uber.org/zap/zaptest"
)

func TestWorkerExecutesHubWebhookAndRecordsDurableEvents(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"queueId":"jenkins-42"}`))
	}))
	defer server.Close()
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DISTR_WEBHOOK_ALLOWED_HOSTS", endpoint.Hostname())
	t.Setenv("DISTR_WEBHOOK_ALLOWED_PRIVATE_HOSTS", endpoint.Hostname())
	lease := hubWebhookLease(server.URL)
	store := &recordingStore{}
	worker := newWorker(zaptest.NewLogger(t), store, Options{
		HeartbeatInterval: time.Hour,
		RuntimeOptions: webhookaction.RuntimeOptions{
			HTTPClient: server.Client(),
		},
	})

	err = worker.executeLease(context.Background(), lease)

	if err != nil {
		t.Fatal(err)
	}
	wantTypes := []types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeSucceeded,
	}
	if len(store.events) != len(wantTypes) {
		t.Fatalf("unexpected event count: got %d want %d", len(store.events), len(wantTypes))
	}
	for i, eventType := range wantTypes {
		if store.events[i].Type != eventType || store.events[i].Sequence != int64(i+1) {
			t.Fatalf("unexpected event %d: %#v", i, store.events[i])
		}
	}
	outputs := store.events[len(store.events)-1].Outputs
	if !containsOutput(outputs, "statusCode", http.StatusAccepted) {
		t.Fatalf("missing status output: %#v", outputs)
	}
	if !containsOutput(outputs, "jenkinsQueueId", "jenkins-42") {
		t.Fatalf("missing Jenkins queue output: %#v", outputs)
	}
	if !containsOutputName(outputs, "auditTrail") {
		t.Fatalf("missing audit output: %#v", outputs)
	}
}

func TestWorkerRecordsUnsupportedHubActionAsFailed(t *testing.T) {
	lease := hubWebhookLease("https://hooks.example.com/deploy")
	lease.Steps[0].ActionType = "distr.compose.deploy"
	store := &recordingStore{}
	worker := newWorker(zaptest.NewLogger(t), store, Options{HeartbeatInterval: time.Hour})

	err := worker.executeLease(context.Background(), lease)

	if err == nil {
		t.Fatal("expected unsupported action error")
	}
	if len(store.events) != 2 || store.events[0].Type != types.StepRunEventTypeStarted || store.events[1].Type != types.StepRunEventTypeFailed {
		t.Fatalf("unexpected failure events: %#v", store.events)
	}
}

func TestWorkerStartsPollerAndShutsDownCleanly(t *testing.T) {
	store := &recordingStore{}
	worker := newWorker(zaptest.NewLogger(t), store, Options{
		PollInterval:      5 * time.Millisecond,
		HeartbeatInterval: time.Hour,
		MaxConcurrency:    2,
	})
	worker.Start(context.Background())
	deadline := time.Now().Add(time.Second)
	for store.leaseCalls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if store.leaseCalls.Load() == 0 {
		t.Fatal("worker did not poll for Hub tasks")
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := worker.Shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}
}

type recordingStore struct {
	events     []types.RecordHubStepRunEventRequest
	leaseCalls atomic.Int32
}

func (s *recordingStore) Lease(context.Context) (*types.TaskLease, error) {
	s.leaseCalls.Add(1)
	return nil, nil
}

func (s *recordingStore) Heartbeat(context.Context, types.HeartbeatHubTaskLeaseRequest) (*types.TaskLease, error) {
	return nil, nil
}

func (s *recordingStore) Record(
	_ context.Context,
	request types.RecordHubStepRunEventRequest,
) (*types.StepRunEvent, error) {
	s.events = append(s.events, request)
	return &types.StepRunEvent{ID: uuid.New(), Type: request.Type, Sequence: request.Sequence}, nil
}

func hubWebhookLease(endpoint string) types.TaskLease {
	return types.TaskLease{
		ID:             uuid.New(),
		OrganizationID: uuid.New(),
		TaskID:         uuid.New(),
		AgentID:        uuid.New(),
		ExecutorType:   types.TaskExecutorTypeHub,
		LeaseToken:     "lease-token",
		Steps: []types.TaskLeaseStep{
			{
				StepRunID:      uuid.New(),
				StepKey:        "trigger-jenkins",
				Name:           "Trigger Jenkins",
				ActionType:     "distr.webhook",
				ActionVersion:  types.AgentActionVersionV1,
				IdempotencyKey: "sha256:choice-tp-loyalty",
				InputBindings: map[string]any{
					"url":            endpoint,
					"method":         "POST",
					"signingSecret":  "resolved-signing-secret",
					"idempotencyKey": "choice-tp-loyalty-2026.07.13.1",
					"expectedStatusCodes": []any{
						http.StatusAccepted,
					},
					"outputs": []any{
						map[string]any{
							"name": "jenkinsQueueId", "pointer": "/queueId", "type": "string", "required": true,
						},
					},
				},
			},
		},
	}
}

func containsOutput(outputs []types.RecordStepRunOutputRequest, name string, value any) bool {
	for _, output := range outputs {
		if output.Name == name && output.Value == value {
			return true
		}
	}
	return false
}

func containsOutputName(outputs []types.RecordStepRunOutputRequest, name string) bool {
	for _, output := range outputs {
		if output.Name == name {
			return true
		}
	}
	return false
}
