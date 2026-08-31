package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/executionprotocol"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

const fixtureAuthorization = "Bearer local-fixture-credential"

func TestReferenceExecutorDerivesOperationFromSignedIntentWithoutUnsignedSpec(t *testing.T) {
	g := NewWithT(t)
	harness := newExecutorHarness(t, 4096)
	input := harness.newOperationRequest(t, "succeed", 2, 1, "resource:signed-intent-only")
	input.Binding.ConfigChecksum = checksum([]byte("hub-frozen-deployment-config"))
	input.Intent = harness.signBinding(t, input.Binding)

	body := map[string]any{
		"intent":  input.Intent,
		"binding": input.Binding,
	}
	response := harness.send(t, http.MethodPost, "/v1/operations", body, fixtureAuthorization)

	g.Expect(response.Code).To(Equal(http.StatusAccepted))
	g.Expect(decodeOperationView(t, response).Status).To(Equal(types.ExecutionAttemptStatusSucceeded))
}

func TestReferenceExecutorRejectsUnsignedOperationSpec(t *testing.T) {
	g := NewWithT(t)
	harness := newExecutorHarness(t, 4096)
	input := harness.newOperationRequest(t, "succeed", 0, 1, "resource:unknown-spec")
	body := map[string]any{
		"intent":  input.Intent,
		"binding": input.Binding,
		"spec":    map[string]any{"mode": "succeed"},
	}

	response := harness.send(t, http.MethodPost, "/v1/operations", body, fixtureAuthorization)

	g.Expect(response.Code).To(Equal(http.StatusBadRequest))
	g.Expect(response.Body.String()).To(ContainSubstring("request body is invalid"))
}

func TestReferenceExecutorVerifiesSignedIntentAndAllBindings(t *testing.T) {
	g := NewWithT(t)
	harness := newExecutorHarness(t, 4096)
	request := harness.newOperationRequest(t, "succeed", 2, 1, "resource:alpha")

	response := harness.send(t, http.MethodPost, "/v1/operations", request, fixtureAuthorization)
	g.Expect(response.Code).To(Equal(http.StatusAccepted))
	g.Expect(decodeOperationView(t, response).Status).To(Equal(types.ExecutionAttemptStatusSucceeded))

	tests := []struct {
		name   string
		mutate func(*operationRequest)
	}{
		{"tenant", func(r *operationRequest) { r.Binding.TenantID = uuid.New() }},
		{"target", func(r *operationRequest) { r.Binding.TargetID = uuid.New() }},
		{"task", func(r *operationRequest) { r.Binding.TaskID = uuid.New() }},
		{"step run", func(r *operationRequest) { r.Binding.StepRunID = uuid.New() }},
		{"execution", func(r *operationRequest) { r.Binding.ExecutionID = uuid.New() }},
		{"attempt number", func(r *operationRequest) { r.Binding.AttemptNumber++ }},
		{"step key", func(r *operationRequest) { r.Binding.StepKey = "different-step" }},
		{"plan", func(r *operationRequest) { r.Binding.PlanChecksum = checksum([]byte("different-plan")) }},
		{"artifact", func(r *operationRequest) { r.Binding.ArtifactDigest = checksum([]byte("different-artifact")) }},
		{"config", func(r *operationRequest) { r.Binding.ConfigChecksum = checksum([]byte("different-config")) }},
		{"adapter", func(r *operationRequest) { r.Binding.AdapterRevision = "reference@different" }},
		{"resource", func(r *operationRequest) { r.Binding.ResourceKey = "resource:different" }},
		{"fence", func(r *operationRequest) { r.Binding.FenceGeneration++ }},
	}
	for _, test := range tests {
		t.Run("rejects mismatched "+test.name+" binding", func(t *testing.T) {
			g := NewWithT(t)
			candidate := request
			test.mutate(&candidate)
			response := harness.send(t, http.MethodPost, "/v1/operations", candidate, fixtureAuthorization)
			g.Expect(response.Code).To(Equal(http.StatusBadRequest))
			g.Expect(response.Body.String()).NotTo(ContainSubstring(candidate.Binding.ConfigChecksum))
		})
	}

	t.Run("rejects an invalid signature", func(t *testing.T) {
		g := NewWithT(t)
		candidate := request
		candidate.Intent.Signature = "invalid-signature"
		response := harness.send(t, http.MethodPost, "/v1/operations", candidate, fixtureAuthorization)
		g.Expect(response.Code).To(Equal(http.StatusUnauthorized))
	})
}

func TestReferenceExecutorFencesStaleOperationsAndReplaysExactDispatch(t *testing.T) {
	g := NewWithT(t)
	harness := newExecutorHarness(t, 4096)
	first := harness.newOperationRequest(t, "hold", 1, 4, "resource:shared")

	created := harness.send(t, http.MethodPost, "/v1/operations", first, fixtureAuthorization)
	g.Expect(created.Code).To(Equal(http.StatusAccepted))
	firstView := decodeOperationView(t, created)
	g.Expect(firstView.Status).To(Equal(types.ExecutionAttemptStatusRunning))

	replayed := harness.send(t, http.MethodPost, "/v1/operations", first, fixtureAuthorization)
	g.Expect(replayed.Code).To(Equal(http.StatusOK))
	replayedView := decodeOperationView(t, replayed)
	g.Expect(replayedView.CreatedAt).To(Equal(firstView.CreatedAt))
	g.Expect(replayedView.Status).To(Equal(types.ExecutionAttemptStatusRunning))

	conflict := first
	conflict.Binding.PlanChecksum = checksum([]byte("conflicting-plan"))
	conflict.Intent = harness.signBinding(t, conflict.Binding)
	conflicting := harness.send(t, http.MethodPost, "/v1/operations", conflict, fixtureAuthorization)
	g.Expect(conflicting.Code).To(Equal(http.StatusConflict))

	stale := harness.newOperationRequest(t, "succeed", 1, 3, "resource:shared")
	staleResponse := harness.send(t, http.MethodPost, "/v1/operations", stale, fixtureAuthorization)
	g.Expect(staleResponse.Code).To(Equal(http.StatusConflict))

	replacement := harness.newOperationRequest(t, "hold", 1, 5, "resource:shared")
	replacementResponse := harness.send(t, http.MethodPost, "/v1/operations", replacement, fixtureAuthorization)
	g.Expect(replacementResponse.Code).To(Equal(http.StatusAccepted))
	g.Expect(decodeOperationView(t, replacementResponse).Status).
		To(Equal(types.ExecutionAttemptStatusRunning))

	fenced := harness.send(t, http.MethodGet, "/v1/operations/"+first.Binding.AttemptID.String(), nil, fixtureAuthorization)
	g.Expect(fenced.Code).To(Equal(http.StatusOK))
	g.Expect(decodeOperationView(t, fenced).Status).To(Equal(types.ExecutionAttemptStatusFenced))
}

func TestReferenceExecutorReportsStatusAndCancelsIdempotently(t *testing.T) {
	g := NewWithT(t)
	harness := newExecutorHarness(t, 4096)
	request := harness.newOperationRequest(t, "hold", 1, 8, "resource:cancel")
	g.Expect(harness.send(t, http.MethodPost, "/v1/operations", request, fixtureAuthorization).Code).
		To(Equal(http.StatusAccepted))

	path := "/v1/operations/" + request.Binding.AttemptID.String()
	status := harness.send(t, http.MethodGet, path, nil, fixtureAuthorization)
	g.Expect(status.Code).To(Equal(http.StatusOK))
	g.Expect(decodeOperationView(t, status).Status).To(Equal(types.ExecutionAttemptStatusRunning))

	staleCancel := cancelRequest{IdempotencyKey: "cancel-stale", FenceGeneration: 7}
	g.Expect(harness.send(t, http.MethodPost, path+"/cancel", staleCancel, fixtureAuthorization).Code).
		To(Equal(http.StatusConflict))

	cancel := cancelRequest{IdempotencyKey: "cancel-once", FenceGeneration: 8}
	canceled := harness.send(t, http.MethodPost, path+"/cancel", cancel, fixtureAuthorization)
	g.Expect(canceled.Code).To(Equal(http.StatusOK))
	g.Expect(decodeOperationView(t, canceled).Status).To(Equal(types.ExecutionAttemptStatusCanceled))

	replayed := harness.send(t, http.MethodPost, path+"/cancel", cancel, fixtureAuthorization)
	g.Expect(replayed.Code).To(Equal(http.StatusOK))
	g.Expect(decodeOperationView(t, replayed).Status).To(Equal(types.ExecutionAttemptStatusCanceled))

	different := cancelRequest{IdempotencyKey: "cancel-different", FenceGeneration: 8}
	g.Expect(harness.send(t, http.MethodPost, path+"/cancel", different, fixtureAuthorization).Code).
		To(Equal(http.StatusConflict))
}

func TestReferenceExecutorBoundsPersistedAndReturnedLogs(t *testing.T) {
	g := NewWithT(t)
	harness := newExecutorHarness(t, 160)
	request := harness.newOperationRequest(t, "succeed", 1000, 1, "resource:logs")
	g.Expect(harness.send(t, http.MethodPost, "/v1/operations", request, fixtureAuthorization).Code).
		To(Equal(http.StatusAccepted))

	response := harness.send(
		t,
		http.MethodGet,
		"/v1/operations/"+request.Binding.AttemptID.String()+"/logs",
		nil,
		fixtureAuthorization,
	)
	g.Expect(response.Code).To(Equal(http.StatusOK))
	var logs operationLogs
	decodeJSON(t, response.Body.Bytes(), &logs)
	total := 0
	for _, entry := range logs.Entries {
		total += len(entry.Message)
	}
	g.Expect(total).To(BeNumerically("<=", 160))
	g.Expect(logs.Truncated).To(BeTrue())
	g.Expect(response.Body.String()).NotTo(ContainSubstring("local-fixture-credential"))
}

func TestReferenceExecutorRecoversOperationsAndResourceFencesAfterRestart(t *testing.T) {
	g := NewWithT(t)
	harness := newExecutorHarness(t, 4096)
	request := harness.newOperationRequest(t, "hold", 1, 12, "resource:restart")
	g.Expect(harness.send(t, http.MethodPost, "/v1/operations", request, fixtureAuthorization).Code).
		To(Equal(http.StatusAccepted))

	restarted, err := newReferenceExecutor(harness.config)
	g.Expect(err).NotTo(HaveOccurred())
	harness.handler = restarted

	replay := harness.send(t, http.MethodPost, "/v1/operations", request, fixtureAuthorization)
	g.Expect(replay.Code).To(Equal(http.StatusOK))
	g.Expect(decodeOperationView(t, replay).Status).To(Equal(types.ExecutionAttemptStatusRunning))

	stale := harness.newOperationRequest(t, "succeed", 1, 11, "resource:restart")
	g.Expect(harness.send(t, http.MethodPost, "/v1/operations", stale, fixtureAuthorization).Code).
		To(Equal(http.StatusConflict))

	cancel := cancelRequest{IdempotencyKey: "cancel-after-restart", FenceGeneration: 12}
	path := "/v1/operations/" + request.Binding.AttemptID.String() + "/cancel"
	canceled := harness.send(t, http.MethodPost, path, cancel, fixtureAuthorization)
	g.Expect(canceled.Code).To(Equal(http.StatusOK))
	g.Expect(decodeOperationView(t, canceled).Status).To(Equal(types.ExecutionAttemptStatusCanceled))
}

func TestReferenceExecutorBoundsHTTPAccessAndRequestBodies(t *testing.T) {
	g := NewWithT(t)
	harness := newExecutorHarness(t, 4096)
	request := harness.newOperationRequest(t, "succeed", 1, 1, "resource:http")

	g.Expect(harness.send(t, http.MethodPost, "/v1/operations", request, "").Code).
		To(Equal(http.StatusUnauthorized))
	g.Expect(harness.send(t, http.MethodPut, "/v1/operations", request, fixtureAuthorization).Code).
		To(Equal(http.StatusMethodNotAllowed))
	g.Expect(harness.send(t, http.MethodGet, "/v1/unknown", nil, fixtureAuthorization).Code).
		To(Equal(http.StatusNotFound))

	large := map[string]string{"value": string(bytes.Repeat([]byte("x"), maxRequestBytes+1))}
	tooLarge := harness.send(t, http.MethodPost, "/v1/operations", large, fixtureAuthorization)
	g.Expect(tooLarge.Code).To(Equal(http.StatusRequestEntityTooLarge))
}

func TestReferenceExecutorExposesIdentityOnlyReadinessWithoutAuthorization(t *testing.T) {
	g := NewWithT(t)
	harness := newExecutorHarness(t, 4096)

	response := harness.send(t, http.MethodGet, "/ready", nil, "")
	g.Expect(response.Code).To(Equal(http.StatusOK))
	var readiness struct {
		Status     string    `json:"status"`
		ExecutorID string    `json:"executorId"`
		TargetID   uuid.UUID `json:"targetId"`
	}
	decodeJSON(t, response.Body.Bytes(), &readiness)
	g.Expect(readiness.Status).To(Equal("ready"))
	g.Expect(readiness.ExecutorID).To(Equal(harness.config.ExecutorID))
	g.Expect(readiness.TargetID).To(Equal(harness.config.TargetID))
	g.Expect(response.Body.String()).NotTo(ContainSubstring(harness.config.SharedSecret))

	g.Expect(harness.send(t, http.MethodPost, "/ready", nil, "").Code).
		To(Equal(http.StatusMethodNotAllowed))
}

func TestReferenceExecutorHandlesConcurrentExactDispatchOnce(t *testing.T) {
	g := NewWithT(t)
	harness := newExecutorHarness(t, 4096)
	request := harness.newOperationRequest(t, "hold", 1, 2, "resource:concurrent")

	const callers = 16
	statuses := make(chan int, callers)
	var group sync.WaitGroup
	for range callers {
		group.Add(1)
		go func() {
			defer group.Done()
			response := harness.send(t, http.MethodPost, "/v1/operations", request, fixtureAuthorization)
			statuses <- response.Code
		}()
	}
	group.Wait()
	close(statuses)

	accepted := 0
	ok := 0
	for status := range statuses {
		switch status {
		case http.StatusAccepted:
			accepted++
		case http.StatusOK:
			ok++
		default:
			t.Fatalf("unexpected dispatch status %d", status)
		}
	}
	g.Expect(accepted).To(Equal(1))
	g.Expect(ok).To(Equal(callers - 1))

	path := "/v1/operations/" + request.Binding.AttemptID.String()
	view := decodeOperationView(t, harness.send(t, http.MethodGet, path, nil, fixtureAuthorization))
	g.Expect(view.Status).To(Equal(types.ExecutionAttemptStatusRunning))
}

type executorHarness struct {
	handler   http.Handler
	config    referenceExecutorConfig
	private   ed25519.PrivateKey
	now       time.Time
	behaviors sync.Map
}

func newExecutorHarness(t *testing.T, maxLogBytes int) *executorHarness {
	t.Helper()
	g := NewWithT(t)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	g.Expect(err).NotTo(HaveOccurred())
	keyID := executionprotocol.PublicKeyFingerprint(public)
	now := time.Date(2026, 7, 28, 1, 2, 3, 0, time.UTC)
	harness := &executorHarness{private: private, now: now}
	config := referenceExecutorConfig{
		ExecutorID:   "reference-executor-test",
		TargetID:     uuid.MustParse("22222222-2222-4222-8222-222222222222"),
		SharedSecret: "local-fixture-credential",
		TrustedKeys:  map[string]ed25519.PublicKey{keyID: public},
		StateFile:    filepath.Join(t.TempDir(), "executor-state.json"),
		MaxLogBytes:  maxLogBytes,
		Now:          func() time.Time { return now },
		TestBehavior: func(binding operationBinding) operationBehavior {
			if behavior, ok := harness.behaviors.Load(binding.AttemptID); ok {
				return behavior.(operationBehavior)
			}
			return operationBehavior{Status: types.ExecutionAttemptStatusSucceeded}
		},
	}
	handler, err := newReferenceExecutor(config)
	g.Expect(err).NotTo(HaveOccurred())
	harness.handler = handler
	harness.config = config
	return harness
}

func (h *executorHarness) newOperationRequest(
	t *testing.T,
	mode string,
	logEntries int,
	attemptNumber int,
	resourceKey string,
) operationRequest {
	t.Helper()
	binding := operationBinding{
		TenantID:                      uuid.MustParse("11111111-1111-4111-8111-111111111111"),
		TargetID:                      h.config.TargetID,
		AttemptID:                     uuid.New(),
		TaskID:                        uuid.New(),
		StepRunID:                     uuid.New(),
		ExecutionID:                   uuid.New(),
		AttemptNumber:                 attemptNumber,
		StepKey:                       "reference-step",
		PlanChecksum:                  checksum([]byte("plan")),
		ArtifactDigest:                checksum([]byte("artifact")),
		ConfigChecksum:                checksum([]byte("hub-frozen-deployment-config")),
		AdapterRevision:               "reference-executor@1",
		RuntimeContractVersion:        types.ExecutionRuntimeContractVersionV3,
		ExpectedObservedStateVersion:  3,
		ExpectedObservedStateChecksum: checksum([]byte("current-observed-state")),
		ExpectedCurrentImageDigest:    checksum([]byte("current-artifact")),
		ExpectedCurrentConfigChecksum: checksum([]byte("current-config")),
		ExpectedPlatform:              types.DeploymentTargetPlatformLinuxAMD64,
		CallerBinding:                 "urn:distr:caller:deployment-target:" + h.config.TargetID.String(),
		Audience:                      "urn:distr:audience:adapter-assignment:reference-executor",
		ResourceKey:                   resourceKey,
		FenceGeneration:               int64(attemptNumber),
	}
	status := types.ExecutionAttemptStatusSucceeded
	switch mode {
	case "fail":
		status = types.ExecutionAttemptStatusFailed
	case "hold":
		status = types.ExecutionAttemptStatusRunning
	}
	h.behaviors.Store(binding.AttemptID, operationBehavior{Status: status, LogEntries: logEntries})
	return operationRequest{Intent: h.signBinding(t, binding), Binding: binding}
}

func (h *executorHarness) signBinding(
	t *testing.T,
	binding operationBinding,
) types.SignedExecutionIntent {
	t.Helper()
	g := NewWithT(t)
	keyID := executionprotocol.PublicKeyFingerprint(h.private.Public().(ed25519.PublicKey))
	signer, err := executionprotocol.NewEd25519IntentSigner(keyID, h.private)
	g.Expect(err).NotTo(HaveOccurred())
	attempt := types.ExecutionAttempt{
		ID:                 binding.AttemptID,
		OrganizationID:     binding.TenantID,
		DeploymentTargetID: binding.TargetID,
		TaskID:             binding.TaskID,
		StepRunID:          binding.StepRunID,
		Identity: types.ExecutionIdentity{
			ExecutionID:   binding.ExecutionID,
			AttemptNumber: binding.AttemptNumber,
			StepKey:       binding.StepKey,
		},
		PlanChecksum:                  binding.PlanChecksum,
		ArtifactDigest:                binding.ArtifactDigest,
		ConfigChecksum:                binding.ConfigChecksum,
		AdapterRevision:               binding.AdapterRevision,
		RuntimeContractVersion:        binding.RuntimeContractVersion,
		ExpectedObservedStateVersion:  binding.ExpectedObservedStateVersion,
		ExpectedObservedStateChecksum: binding.ExpectedObservedStateChecksum,
		ExpectedCurrentImageDigest:    binding.ExpectedCurrentImageDigest,
		ExpectedCurrentConfigChecksum: binding.ExpectedCurrentConfigChecksum,
		ExpectedPlatform:              binding.ExpectedPlatform,
		CallerBinding:                 binding.CallerBinding,
		Audience:                      binding.Audience,
		Fence: types.ExecutionFence{
			ResourceKey: binding.ResourceKey,
			Generation:  binding.FenceGeneration,
		},
		IntentIssuedAt:  h.now.Add(-time.Minute),
		IntentExpiresAt: h.now.Add(10 * time.Minute),
	}
	signed, err := executionprotocol.BuildExecutionIntent(
		executionprotocol.WithIntentSigner(context.Background(), signer),
		attempt,
	)
	g.Expect(err).NotTo(HaveOccurred())
	return signed
}

func (h *executorHarness) send(
	t *testing.T,
	method string,
	path string,
	body any,
	authorization string,
) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		reader = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, path, reader)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	h.handler.ServeHTTP(response, request)
	return response
}

func decodeOperationView(t *testing.T, response *httptest.ResponseRecorder) operationView {
	t.Helper()
	var result operationView
	decodeJSON(t, response.Body.Bytes(), &result)
	return result
}

func decodeJSON(t *testing.T, data []byte, target any) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		t.Fatalf("decode JSON: %v\nbody: %s", err, data)
	}
}

func checksum(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TestNoSensitiveFixtureValuesArePersisted(t *testing.T) {
	g := NewWithT(t)
	harness := newExecutorHarness(t, 4096)
	request := harness.newOperationRequest(t, "succeed", 2, 1, "resource:persistence")
	g.Expect(harness.send(t, http.MethodPost, "/v1/operations", request, fixtureAuthorization).Code).
		To(Equal(http.StatusAccepted))

	state, err := os.ReadFile(harness.config.StateFile)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(state)).NotTo(ContainSubstring("local-fixture-credential"))
	g.Expect(string(state)).NotTo(ContainSubstring("hub-frozen-deployment-config"))
}
