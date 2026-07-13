package hubexecutor

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
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
	if len(store.events) != 2 ||
		store.events[0].Type != types.StepRunEventTypeStarted ||
		store.events[1].Type != types.StepRunEventTypeFailed {
		t.Fatalf("unexpected failure events: %#v", store.events)
	}
}

func TestWorkerWaitsForExternalCallbackBeforeSucceeding(t *testing.T) {
	executionID := uuid.New()
	var receivedExecutionID, receivedCallbackURL string
	var dispatchPersistedBeforeWebhook atomic.Bool
	var store *recordingStore
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatchPersistedBeforeWebhook.Store(store != nil && store.triggerCalls.Load() == 1)
		receivedExecutionID = r.Header.Get("X-Distr-External-Execution-ID")
		receivedCallbackURL = r.Header.Get("X-Distr-Callback-URL")
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
	lease.Steps[0].InputBindings["completionMode"] = "callback"
	delete(lease.Steps[0].InputBindings, "outputs")
	lease.Steps[0].InputBindings["component"] = "loyalty-api"
	lease.Steps[0].InputBindings["callbackTimeoutSeconds"] = 600
	store = &recordingStore{externalExecution: &types.ExternalExecution{
		ID: executionID, OrganizationID: lease.OrganizationID, StepRunID: lease.Steps[0].StepRunID,
		TaskID: lease.TaskID, DeploymentTargetID: lease.AgentID, Component: "loyalty-api",
		PlanChecksum: "sha256:" + strings.Repeat("a", 64), IdempotencyKey: "ext:callback-test",
		ExpectedStateVersion: 4, ExpectedStateChecksum: "sha256:" + strings.Repeat("b", 64),
		ExpectedVersion: "1.4.2", ExpectedImage: "repo/loyalty-api@sha256:" + strings.Repeat("c", 64),
		ExpectedPlatform:        types.DeploymentTargetPlatformLinuxAMD64,
		ExpectedConfigReference: "s3://bucket/config?versionId=v42",
		ExpectedConfigChecksum:  "sha256:" + strings.Repeat("d", 64),
		Status:                  types.ExternalExecutionStatusQueued, CallbackDeadlineAt: time.Now().Add(time.Minute),
	}}
	worker := newWorker(zaptest.NewLogger(t), store, Options{
		HeartbeatInterval: time.Hour, ExternalPollInterval: time.Millisecond,
		CallbackBaseURL: "https://distr.example.com",
		RuntimeOptions:  webhookaction.RuntimeOptions{HTTPClient: server.Client()},
	})

	err = worker.executeLease(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	if receivedExecutionID != executionID.String() {
		t.Fatalf("unexpected external execution header: %q", receivedExecutionID)
	}
	if !dispatchPersistedBeforeWebhook.Load() {
		t.Fatal("external execution dispatch was not persisted before invoking the webhook")
	}
	if receivedCallbackURL != "https://distr.example.com/api/v1/external-executions/"+executionID.String()+"/callbacks" {
		t.Fatalf("unexpected callback URL: %q", receivedCallbackURL)
	}
	if store.prepareCalls.Load() != 1 || store.triggerCalls.Load() != 1 || store.externalReads.Load() < 2 {
		t.Fatalf("external execution was not prepared, triggered, and polled: %#v", store)
	}
	if store.events[len(store.events)-1].Type != types.StepRunEventTypeSucceeded {
		t.Fatalf("worker succeeded before terminal callback: %#v", store.events)
	}
	outputs := store.events[len(store.events)-1].Outputs
	if !containsOutput(outputs, "externalExecutionId", executionID.String()) ||
		!containsOutput(outputs, "providerReference", "jenkins-42") ||
		!containsOutput(outputs, "actualVersion", "1.4.2") {
		t.Fatalf("missing external execution outputs: %#v", outputs)
	}
}

func TestWorkerResumesRunningExternalExecutionWithoutRetriggeringWebhook(t *testing.T) {
	var webhookCalls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		webhookCalls.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DISTR_WEBHOOK_ALLOWED_HOSTS", endpoint.Hostname())
	t.Setenv("DISTR_WEBHOOK_ALLOWED_PRIVATE_HOSTS", endpoint.Hostname())
	lease := hubWebhookLease(server.URL)
	lease.Steps[0].InputBindings["completionMode"] = "callback"
	delete(lease.Steps[0].InputBindings, "outputs")
	lease.Steps[0].InputBindings["component"] = "loyalty-api"
	store := &recordingStore{externalExecution: callbackExecution(lease, types.ExternalExecutionStatusRunning)}
	worker := newWorker(zaptest.NewLogger(t), store, Options{
		HeartbeatInterval: time.Hour, ExternalPollInterval: time.Millisecond,
		CallbackBaseURL: "https://distr.example.com",
		RuntimeOptions:  webhookaction.RuntimeOptions{HTTPClient: server.Client()},
	})

	err = worker.executeLease(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	if webhookCalls.Load() != 0 || store.triggerCalls.Load() != 0 {
		t.Fatalf(
			"running external execution was retriggered: webhook=%d trigger=%d",
			webhookCalls.Load(),
			store.triggerCalls.Load(),
		)
	}
	if store.externalReads.Load() < 2 ||
		store.events[len(store.events)-1].Type != types.StepRunEventTypeSucceeded {
		t.Fatalf("running execution was not resumed to completion: %#v", store.events)
	}
}

func TestWorkerDoesNotDispatchExecutionClaimedByAnotherWorker(t *testing.T) {
	var webhookCalls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		webhookCalls.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DISTR_WEBHOOK_ALLOWED_HOSTS", endpoint.Hostname())
	t.Setenv("DISTR_WEBHOOK_ALLOWED_PRIVATE_HOSTS", endpoint.Hostname())
	lease := hubWebhookLease(server.URL)
	lease.Steps[0].InputBindings["completionMode"] = "callback"
	delete(lease.Steps[0].InputBindings, "outputs")
	lease.Steps[0].InputBindings["component"] = "loyalty-api"
	store := &recordingStore{
		externalExecution: callbackExecution(lease, types.ExternalExecutionStatusQueued),
		claimConflict:     true,
	}
	worker := newWorker(zaptest.NewLogger(t), store, Options{
		HeartbeatInterval: time.Hour, ExternalPollInterval: time.Millisecond,
		CallbackBaseURL: "https://distr.example.com",
		RuntimeOptions:  webhookaction.RuntimeOptions{HTTPClient: server.Client()},
	})

	err = worker.executeLease(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	if webhookCalls.Load() != 0 {
		t.Fatalf("execution claimed by another worker was dispatched %d times", webhookCalls.Load())
	}
}

func TestWorkerHonorsSuccessfulCallbackWhenTriggerResponseIsLost(t *testing.T) {
	t.Setenv("DISTR_WEBHOOK_ALLOWED_HOSTS", "hooks.example.com")
	lease := hubWebhookLease("https://hooks.example.com/deploy")
	lease.Steps[0].InputBindings["completionMode"] = "callback"
	lease.Steps[0].InputBindings["component"] = "loyalty-api"
	delete(lease.Steps[0].InputBindings, "outputs")
	store := &recordingStore{
		externalExecution: callbackExecution(lease, types.ExternalExecutionStatusQueued),
		succeedOnFail:     true,
	}
	worker := newWorker(zaptest.NewLogger(t), store, Options{
		HeartbeatInterval: time.Hour, CallbackBaseURL: "https://distr.example.com",
		RuntimeOptions: webhookaction.RuntimeOptions{HTTPClient: &http.Client{Transport: roundTripFunc(
			func(*http.Request) (*http.Response, error) { return nil, errors.New("response lost") },
		)}},
	})

	err := worker.executeLease(context.Background(), lease)
	if err != nil {
		t.Fatalf("successful callback was overwritten by transport error: %v", err)
	}
	if store.events[len(store.events)-1].Type != types.StepRunEventTypeSucceeded {
		t.Fatalf("expected successful step after callback reconciliation: %#v", store.events)
	}
}

func TestWorkerFinalizesSucceededExternalExecutionWithoutRetriggeringWebhook(t *testing.T) {
	var webhookCalls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		webhookCalls.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DISTR_WEBHOOK_ALLOWED_HOSTS", endpoint.Hostname())
	t.Setenv("DISTR_WEBHOOK_ALLOWED_PRIVATE_HOSTS", endpoint.Hostname())
	lease := hubWebhookLease(server.URL)
	lease.Steps[0].InputBindings["completionMode"] = "callback"
	delete(lease.Steps[0].InputBindings, "outputs")
	lease.Steps[0].InputBindings["component"] = "loyalty-api"
	execution := callbackExecution(lease, types.ExternalExecutionStatusSucceeded)
	setSuccessfulObservedState(execution)
	store := &recordingStore{externalExecution: execution}
	worker := newWorker(zaptest.NewLogger(t), store, Options{
		HeartbeatInterval: time.Hour, ExternalPollInterval: time.Millisecond,
		CallbackBaseURL: "https://distr.example.com",
		RuntimeOptions:  webhookaction.RuntimeOptions{HTTPClient: server.Client()},
	})

	err = worker.executeLease(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	if webhookCalls.Load() != 0 || store.triggerCalls.Load() != 0 || store.externalReads.Load() != 0 {
		t.Fatalf("succeeded external execution was not finalized directly: webhook=%d trigger=%d reads=%d",
			webhookCalls.Load(), store.triggerCalls.Load(), store.externalReads.Load())
	}
	outputs := store.events[len(store.events)-1].Outputs
	if !containsOutput(outputs, "externalExecutionId", execution.ID.String()) ||
		!containsOutput(outputs, "actualImage", execution.ActualImage) {
		t.Fatalf("missing recovered execution outputs: %#v", outputs)
	}
}

func TestWorkerFailsFromRecoveredTerminalExternalExecutionWithoutRetriggeringWebhook(t *testing.T) {
	var webhookCalls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		webhookCalls.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DISTR_WEBHOOK_ALLOWED_HOSTS", endpoint.Hostname())
	t.Setenv("DISTR_WEBHOOK_ALLOWED_PRIVATE_HOSTS", endpoint.Hostname())
	lease := hubWebhookLease(server.URL)
	lease.Steps[0].InputBindings["completionMode"] = "callback"
	delete(lease.Steps[0].InputBindings, "outputs")
	lease.Steps[0].InputBindings["component"] = "loyalty-api"
	execution := callbackExecution(lease, types.ExternalExecutionStatusFailed)
	execution.ErrorSummary = "Jenkins deployment failed health check"
	store := &recordingStore{externalExecution: execution}
	worker := newWorker(zaptest.NewLogger(t), store, Options{
		HeartbeatInterval: time.Hour, CallbackBaseURL: "https://distr.example.com",
		RuntimeOptions: webhookaction.RuntimeOptions{HTTPClient: server.Client()},
	})

	err = worker.executeLease(context.Background(), lease)

	if err == nil || !strings.Contains(err.Error(), execution.ErrorSummary) {
		t.Fatalf("expected recovered terminal error, got %v", err)
	}
	if webhookCalls.Load() != 0 || store.triggerCalls.Load() != 0 {
		t.Fatalf(
			"failed external execution was retriggered: webhook=%d trigger=%d",
			webhookCalls.Load(),
			store.triggerCalls.Load(),
		)
	}
	if store.events[len(store.events)-1].Type != types.StepRunEventTypeFailed {
		t.Fatalf("expected failed step event: %#v", store.events)
	}
}

func TestWorkerHandlesCallbackBeforeTriggerPersistence(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	lease.Steps[0].InputBindings["completionMode"] = "callback"
	delete(lease.Steps[0].InputBindings, "outputs")
	lease.Steps[0].InputBindings["component"] = "loyalty-api"
	store := &recordingStore{
		externalExecution: callbackExecution(lease, types.ExternalExecutionStatusQueued),
		completeOnTrigger: true,
	}
	worker := newWorker(zaptest.NewLogger(t), store, Options{
		HeartbeatInterval: time.Hour, ExternalPollInterval: time.Millisecond,
		CallbackBaseURL: "https://distr.example.com",
		RuntimeOptions:  webhookaction.RuntimeOptions{HTTPClient: server.Client()},
	})

	err = worker.executeLease(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	if store.triggerCalls.Load() != 1 || store.externalReads.Load() != 1 {
		t.Fatalf(
			"callback race was not reconciled: trigger=%d reads=%d",
			store.triggerCalls.Load(),
			store.externalReads.Load(),
		)
	}
	if store.events[len(store.events)-1].Type != types.StepRunEventTypeSucceeded {
		t.Fatalf("callback race did not finish the step: %#v", store.events)
	}
}

func TestWorkerPollerShutsDownCleanly(t *testing.T) {
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
	events            []types.RecordHubStepRunEventRequest
	leaseCalls        atomic.Int32
	prepareCalls      atomic.Int32
	triggerCalls      atomic.Int32
	externalReads     atomic.Int32
	externalExecution *types.ExternalExecution
	completeOnTrigger bool
	claimConflict     bool
	succeedOnFail     bool
}

func (s *recordingStore) PrepareExternalExecution(
	_ context.Context,
	_ types.PrepareExternalExecutionRequest,
) (*types.ExternalExecution, error) {
	s.prepareCalls.Add(1)
	copy := *s.externalExecution
	return &copy, nil
}

func (s *recordingStore) MarkExternalExecutionTriggered(
	_ context.Context,
	_ types.MarkExternalExecutionTriggeredRequest,
) (*types.ExternalExecution, error) {
	s.triggerCalls.Add(1)
	if s.claimConflict {
		s.externalExecution.Status = types.ExternalExecutionStatusRunning
		return nil, apierrors.NewConflict("external execution dispatch is already claimed")
	}
	if s.completeOnTrigger {
		s.externalExecution.Status = types.ExternalExecutionStatusSucceeded
		setSuccessfulObservedState(s.externalExecution)
		return nil, apierrors.NewConflict("external execution is already terminal")
	}
	s.externalExecution.Status = types.ExternalExecutionStatusRunning
	copy := *s.externalExecution
	return &copy, nil
}

func (s *recordingStore) GetExternalExecution(context.Context, uuid.UUID, uuid.UUID) (*types.ExternalExecution, error) {
	reads := s.externalReads.Add(1)
	copy := *s.externalExecution
	if copy.Status.IsTerminal() {
		return &copy, nil
	}
	if reads == 1 {
		copy.Status = types.ExternalExecutionStatusRunning
		copy.LastCallbackSequence = 1
		copy.LastMessage = "deploying loyalty-api"
		return &copy, nil
	}
	copy.Status = types.ExternalExecutionStatusSucceeded
	copy.LastCallbackSequence = 2
	copy.ProviderReference = "jenkins-42"
	copy.ProviderURL = "https://jenkins.example/job/42"
	copy.ActualVersion = "1.4.2"
	copy.ActualImage = copy.ExpectedImage
	copy.ActualPlatform = &copy.ExpectedPlatform
	copy.ActualConfigReference = copy.ExpectedConfigReference
	copy.ActualConfigChecksum = copy.ExpectedConfigChecksum
	health := types.TargetComponentHealthHealthy
	copy.ActualHealth = &health
	return &copy, nil
}

func (s *recordingStore) TimeoutExternalExecution(
	_ context.Context,
	_ types.TimeoutExternalExecutionRequest,
) (*types.ExternalExecution, error) {
	s.externalExecution.Status = types.ExternalExecutionStatusTimedOut
	copy := *s.externalExecution
	return &copy, nil
}

func (s *recordingStore) FailExternalExecution(
	_ context.Context,
	_ types.FailExternalExecutionRequest,
) (*types.ExternalExecution, error) {
	if s.succeedOnFail {
		s.externalExecution.Status = types.ExternalExecutionStatusSucceeded
		setSuccessfulObservedState(s.externalExecution)
		copy := *s.externalExecution
		return &copy, nil
	}
	s.externalExecution.Status = types.ExternalExecutionStatusFailed
	copy := *s.externalExecution
	return &copy, nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
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

func callbackExecution(lease types.TaskLease, status types.ExternalExecutionStatus) *types.ExternalExecution {
	return &types.ExternalExecution{
		ID: uuid.New(), OrganizationID: lease.OrganizationID, StepRunID: lease.Steps[0].StepRunID,
		TaskID: lease.TaskID, DeploymentTargetID: lease.AgentID, Component: "loyalty-api",
		PlanChecksum: "sha256:" + strings.Repeat("a", 64), IdempotencyKey: "ext:callback-test",
		ExpectedStateVersion: 4, ExpectedStateChecksum: "sha256:" + strings.Repeat("b", 64),
		ExpectedVersion: "1.4.2", ExpectedImage: "repo/loyalty-api@sha256:" + strings.Repeat("c", 64),
		ExpectedPlatform:        types.DeploymentTargetPlatformLinuxAMD64,
		ExpectedConfigReference: "s3://bucket/config?versionId=v42",
		ExpectedConfigChecksum:  "sha256:" + strings.Repeat("d", 64),
		Status:                  status, CallbackDeadlineAt: time.Now().Add(time.Minute),
	}
}

func setSuccessfulObservedState(execution *types.ExternalExecution) {
	execution.ActualVersion = execution.ExpectedVersion
	execution.ActualImage = execution.ExpectedImage
	execution.ActualPlatform = &execution.ExpectedPlatform
	execution.ActualConfigReference = execution.ExpectedConfigReference
	execution.ActualConfigChecksum = execution.ExpectedConfigChecksum
	health := types.TargetComponentHealthHealthy
	execution.ActualHealth = &health
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
