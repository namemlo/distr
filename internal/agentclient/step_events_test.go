package agentclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v3/jwt"
	"go.uber.org/zap"
)

func TestRecordStepRunEventPostsAuthenticatedRequest(t *testing.T) {
	stepRunID := uuid.New()
	var received api.AgentStepRunEventRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/api/v1/agents/agent/step-runs/"+stepRunID.String()+"/events"; got != want {
			t.Fatalf("expected path %q, got %q", want, got)
		}
		if got, want := r.Header.Get("Authorization"), "Bearer test-token"; got != want {
			t.Fatalf("expected authorization %q, got %q", want, got)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode step event: %v", err)
		}
		_ = json.NewEncoder(w).Encode(api.StepRunEvent{
			ID:        uuid.New(),
			StepRunID: stepRunID,
			Sequence:  1,
			Type:      types.StepRunEventTypeStarted,
		})
	}))
	defer server.Close()
	client := testStepEventClient(server.URL + "/api/v1/agents/agent/step-runs/{stepRunId}/events")

	event, err := client.RecordStepRunEvent(context.Background(), stepRunID, api.AgentStepRunEventRequest{
		LeaseToken: "lease-token",
		Sequence:   1,
		Type:       types.StepRunEventTypeStarted,
	})
	if err != nil {
		t.Fatalf("expected step event request to succeed: %v", err)
	}
	if event == nil || event.StepRunID != stepRunID {
		t.Fatalf("expected step event for %s, got %#v", stepRunID, event)
	}
	if received.LeaseToken != "lease-token" || received.Sequence != 1 {
		t.Fatalf("unexpected request payload: %#v", received)
	}
}

func TestRecordStepRunEventFailsMissingDisabledOrEmptyEndpoint(t *testing.T) {
	client := testStepEventClient("")
	event, err := client.RecordStepRunEvent(context.Background(), uuid.New(), api.AgentStepRunEventRequest{
		LeaseToken: "lease-token",
		Sequence:   1,
		Type:       types.StepRunEventTypeStarted,
	})
	if err == nil || event != nil {
		t.Fatalf("expected missing endpoint to fail closed, event=%v err=%v", event, err)
	}

	for _, status := range []int{http.StatusForbidden, http.StatusNotFound, http.StatusNoContent} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		}))
		client = testStepEventClient(server.URL + "/api/v1/agents/agent/step-runs/{stepRunId}/events")
		event, err = client.RecordStepRunEvent(context.Background(), uuid.New(), api.AgentStepRunEventRequest{
			LeaseToken: "lease-token",
			Sequence:   1,
			Type:       types.StepRunEventTypeStarted,
		})
		server.Close()
		if err == nil || event != nil {
			t.Fatalf("expected status %d to fail closed, event=%v err=%v", status, event, err)
		}
	}
}

func TestGetTaskTimelineUsesAuthenticatedTemplate(t *testing.T) {
	taskID := uuid.New()
	leaseID := uuid.New()
	stepRunID := uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/api/v1/agents/agent/tasks/"+taskID.String()+"/timeline"; got != want {
			t.Fatalf("expected path %q, got %q", want, got)
		}
		if got, want := r.URL.Query().Get("leaseId"), leaseID.String(); got != want {
			t.Fatalf("expected leaseId query %q, got %q", want, got)
		}
		if got, want := r.Header.Get("Authorization"), "Bearer test-token"; got != want {
			t.Fatalf("expected authorization %q, got %q", want, got)
		}
		_ = json.NewEncoder(w).Encode(api.TaskTimeline{
			TaskID: taskID,
			Events: []api.StepRunEvent{{
				ID:        uuid.New(),
				TaskID:    taskID,
				StepRunID: stepRunID,
				Sequence:  3,
				Type:      types.StepRunEventTypeSucceeded,
			}},
		})
	}))
	defer server.Close()
	client := testStepEventClient("")
	client.taskTimelineEndpointTemplate = server.URL + "/api/v1/agents/agent/tasks/{taskId}/timeline"

	timeline, err := client.GetTaskTimeline(context.Background(), taskID, leaseID)
	if err != nil {
		t.Fatalf("expected task timeline request to succeed: %v", err)
	}
	if timeline == nil || timeline.TaskID != taskID || len(timeline.Events) != 1 {
		t.Fatalf("unexpected timeline: %#v", timeline)
	}
	if timeline.Events[0].StepRunID != stepRunID || timeline.Events[0].Type != types.StepRunEventTypeSucceeded {
		t.Fatalf("unexpected timeline event: %#v", timeline.Events[0])
	}
}

func TestGetTaskTimelineReturnsNilWhenEndpointMissing(t *testing.T) {
	client := testStepEventClient("")

	timeline, err := client.GetTaskTimeline(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("expected missing optional endpoint to be a no-op: %v", err)
	}
	if timeline != nil {
		t.Fatalf("expected nil timeline for missing endpoint, got %#v", timeline)
	}
}

func testStepEventClient(endpointTemplate string) *Client {
	token := jwt.New()
	_ = token.Set(jwt.ExpirationKey, time.Now().Add(time.Hour))
	return &Client{
		clientData: clientData{
			stepEventEndpointTemplate: endpointTemplate,
		},
		httpClient: http.DefaultClient,
		logger:     zap.NewNop(),
		token:      token,
		rawToken:   "test-token",
	}
}
