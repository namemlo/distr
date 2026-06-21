package agentclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v3/jwt"
	"go.uber.org/zap"
)

func TestLeaseTaskPostsAuthenticatedRequest(t *testing.T) {
	taskID := uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if got, want := r.Header.Get("Authorization"), "Bearer test-token"; got != want {
			t.Fatalf("expected authorization %q, got %q", want, got)
		}
		_ = json.NewEncoder(w).Encode(api.AgentTaskLease{
			ID:           uuid.New(),
			TaskID:       taskID,
			PlanChecksum: "sha256:abc",
			LeaseToken:   "lease-token",
			ExpiresAt:    time.Now().Add(time.Minute),
		})
	}))
	defer server.Close()
	client := testTaskLeaseClient(server.URL, "")

	lease, err := client.LeaseTask(context.Background())
	if err != nil {
		t.Fatalf("expected lease request to succeed: %v", err)
	}
	if lease == nil {
		t.Fatal("expected lease response")
	}
	if lease.TaskID != taskID {
		t.Fatalf("expected task %s, got %s", taskID, lease.TaskID)
	}
	if lease.LeaseToken != "lease-token" {
		t.Fatalf("expected lease token to round trip, got %q", lease.LeaseToken)
	}
}

func TestLeaseTaskSkipsMissingDisabledOrEmptyEndpoint(t *testing.T) {
	client := testTaskLeaseClient("", "")
	lease, err := client.LeaseTask(context.Background())
	if err != nil || lease != nil {
		t.Fatalf("expected missing endpoint to be skipped, lease=%v err=%v", lease, err)
	}

	for _, status := range []int{http.StatusForbidden, http.StatusNotFound, http.StatusNoContent} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		}))
		client = testTaskLeaseClient(server.URL, "")
		lease, err = client.LeaseTask(context.Background())
		server.Close()
		if err != nil || lease != nil {
			t.Fatalf("expected status %d to be skipped, lease=%v err=%v", status, lease, err)
		}
	}
}

func TestHeartbeatTaskLeaseFailsMissingDisabledOrEmptyEndpoint(t *testing.T) {
	taskID := uuid.New()
	client := testTaskLeaseClient("", "")
	lease, err := client.HeartbeatTaskLease(context.Background(), taskID, "lease-token")
	if err == nil || lease != nil {
		t.Fatalf("expected missing endpoint to fail closed, lease=%v err=%v", lease, err)
	}

	for _, status := range []int{http.StatusForbidden, http.StatusNotFound, http.StatusNoContent} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		}))
		client = testTaskLeaseClient("", server.URL+"/api/v1/agents/agent/tasks/{taskId}/heartbeat")
		lease, err = client.HeartbeatTaskLease(context.Background(), taskID, "lease-token")
		server.Close()
		if err == nil || lease != nil {
			t.Fatalf("expected status %d to fail closed, lease=%v err=%v", status, lease, err)
		}
	}
}

func TestHeartbeatTaskLeasePostsTokenToTaskEndpoint(t *testing.T) {
	taskID := uuid.New()
	var received api.HeartbeatAgentTaskLeaseRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/api/v1/agents/agent/tasks/"+taskID.String()+"/heartbeat"; got != want {
			t.Fatalf("expected path %q, got %q", want, got)
		}
		if got, want := r.Header.Get("Authorization"), "Bearer test-token"; got != want {
			t.Fatalf("expected authorization %q, got %q", want, got)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode heartbeat: %v", err)
		}
		_ = json.NewEncoder(w).Encode(api.AgentTaskLease{ID: uuid.New(), TaskID: taskID, LeaseToken: "lease-token"})
	}))
	defer server.Close()
	client := testTaskLeaseClient("", server.URL+"/api/v1/agents/agent/tasks/{taskId}/heartbeat")

	lease, err := client.HeartbeatTaskLease(context.Background(), taskID, "lease-token")
	if err != nil {
		t.Fatalf("expected heartbeat to succeed: %v", err)
	}
	if lease == nil || lease.TaskID != taskID {
		t.Fatalf("expected heartbeat lease for task %s, got %#v", taskID, lease)
	}
	if received.LeaseToken != "lease-token" {
		t.Fatalf("expected token %q, got %q", "lease-token", received.LeaseToken)
	}
}

func testTaskLeaseClient(leaseEndpoint, heartbeatEndpoint string) *Client {
	token := jwt.New()
	_ = token.Set(jwt.ExpirationKey, time.Now().Add(time.Hour))
	return &Client{
		clientData: clientData{
			leaseEndpoint:                 leaseEndpoint,
			taskHeartbeatEndpointTemplate: heartbeatEndpoint,
		},
		httpClient: http.DefaultClient,
		logger:     zap.NewNop(),
		token:      token,
		rawToken:   "test-token",
	}
}
