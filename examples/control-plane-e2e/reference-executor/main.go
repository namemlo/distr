package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/distr-sh/distr/internal/executionprotocol"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

const (
	maxRequestBytes = 64 << 10
	maxStateBytes   = 4 << 20
	maxAllowedLogs  = 64 << 10
	stateVersion    = 1
)

var (
	checksumPattern       = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._~:/+-]{1,128}$`)
)

type referenceExecutorConfig struct {
	ExecutorID   string
	TargetID     uuid.UUID
	SharedSecret string
	TrustedKeys  map[string]ed25519.PublicKey
	StateFile    string
	MaxLogBytes  int
	Now          func() time.Time
	TestBehavior func(operationBinding) operationBehavior
}

type operationRequest struct {
	Intent  types.SignedExecutionIntent `json:"intent"`
	Binding operationBinding            `json:"binding"`
}

type operationBinding struct {
	TenantID        uuid.UUID `json:"tenantId"`
	TargetID        uuid.UUID `json:"targetId"`
	AttemptID       uuid.UUID `json:"attemptId"`
	TaskID          uuid.UUID `json:"taskId"`
	StepRunID       uuid.UUID `json:"stepRunId"`
	ExecutionID     uuid.UUID `json:"executionId"`
	AttemptNumber   int       `json:"attemptNumber"`
	StepKey         string    `json:"stepKey"`
	PlanChecksum    string    `json:"planChecksum"`
	ArtifactDigest  string    `json:"artifactDigest"`
	ConfigChecksum  string    `json:"configChecksum"`
	AdapterRevision string    `json:"adapterRevision"`
	ResourceKey     string    `json:"resourceKey"`
	FenceGeneration int64     `json:"fenceGeneration"`
}

type operationBehavior struct {
	Status     types.ExecutionAttemptStatus
	LogEntries int
}

type cancelRequest struct {
	IdempotencyKey  string `json:"idempotencyKey"`
	FenceGeneration int64  `json:"fenceGeneration"`
}

type operationView struct {
	ID            uuid.UUID                    `json:"id"`
	ExecutorID    string                       `json:"executorId"`
	Status        types.ExecutionAttemptStatus `json:"status"`
	Binding       operationBinding             `json:"binding"`
	CreatedAt     time.Time                    `json:"createdAt"`
	UpdatedAt     time.Time                    `json:"updatedAt"`
	LogsTruncated bool                         `json:"logsTruncated"`
}

type logEntry struct {
	Sequence int       `json:"sequence"`
	At       time.Time `json:"at"`
	Message  string    `json:"message"`
}

type operationLogs struct {
	Entries   []logEntry `json:"entries"`
	Truncated bool       `json:"truncated"`
}

type readinessView struct {
	Status     string    `json:"status"`
	ExecutorID string    `json:"executorId"`
	TargetID   uuid.UUID `json:"targetId"`
}

type signedIntentPayload struct {
	Schema             string    `json:"schema"`
	OrganizationID     uuid.UUID `json:"organizationId"`
	DeploymentTargetID uuid.UUID `json:"deploymentTargetId"`
	AttemptID          uuid.UUID `json:"attemptId"`
	TaskID             uuid.UUID `json:"taskId"`
	StepRunID          uuid.UUID `json:"stepRunId"`
	ExecutionID        uuid.UUID `json:"executionId"`
	AttemptNumber      int       `json:"attemptNumber"`
	StepKey            string    `json:"stepKey"`
	PlanChecksum       string    `json:"planChecksum"`
	ArtifactDigest     string    `json:"artifactDigest"`
	ConfigChecksum     string    `json:"configChecksum"`
	AdapterRevision    string    `json:"adapterRevision"`
	ResourceKey        string    `json:"resourceKey"`
	FenceGeneration    int64     `json:"fenceGeneration"`
	IssuedAt           time.Time `json:"issuedAt"`
	ExpiresAt          time.Time `json:"expiresAt"`
}

type persistedOperation struct {
	ID               uuid.UUID                    `json:"id"`
	Binding          operationBinding             `json:"binding"`
	RequestChecksum  string                       `json:"requestChecksum"`
	Status           types.ExecutionAttemptStatus `json:"status"`
	CreatedAt        time.Time                    `json:"createdAt"`
	UpdatedAt        time.Time                    `json:"updatedAt"`
	CancelKey        string                       `json:"cancelKey,omitempty"`
	CancelGeneration int64                        `json:"cancelGeneration,omitempty"`
	Logs             []logEntry                   `json:"logs,omitempty"`
	LogBytes         int                          `json:"logBytes"`
	LogsTruncated    bool                         `json:"logsTruncated"`
}

type persistedState struct {
	Version             int                            `json:"version"`
	Operations          map[string]*persistedOperation `json:"operations"`
	ResourceGenerations map[string]int64               `json:"resourceGenerations"`
}

type referenceExecutor struct {
	mu     sync.Mutex
	config referenceExecutorConfig
	state  persistedState
}

func newReferenceExecutor(config referenceExecutorConfig) (http.Handler, error) {
	config.ExecutorID = strings.TrimSpace(config.ExecutorID)
	config.StateFile = strings.TrimSpace(config.StateFile)
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.TestBehavior == nil {
		config.TestBehavior = func(operationBinding) operationBehavior {
			return operationBehavior{Status: types.ExecutionAttemptStatusSucceeded}
		}
	}
	if config.ExecutorID == "" || len(config.ExecutorID) > 128 ||
		strings.ContainsAny(config.ExecutorID, "\r\n") {
		return nil, errors.New("executor ID is invalid")
	}
	if config.TargetID == uuid.Nil {
		return nil, errors.New("target ID is required")
	}
	if config.SharedSecret == "" {
		return nil, errors.New("executor shared credential is required")
	}
	if config.StateFile == "" {
		return nil, errors.New("state file is required")
	}
	if config.MaxLogBytes <= 0 || config.MaxLogBytes > maxAllowedLogs {
		return nil, fmt.Errorf("max log bytes must be between 1 and %d", maxAllowedLogs)
	}
	if err := executionprotocol.ValidateTrustPolicy(types.TrustPolicy{Keys: config.TrustedKeys}); err != nil {
		return nil, fmt.Errorf("trusted intent keys are invalid: %w", err)
	}
	state, err := loadState(config.StateFile, config.MaxLogBytes)
	if err != nil {
		return nil, err
	}
	return &referenceExecutor{config: config, state: state}, nil
}

func (e *referenceExecutor) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.URL.Path == "/ready" {
		if request.Method != http.MethodGet {
			methodNotAllowed(response, http.MethodGet)
			return
		}
		if request.URL.RawQuery != "" {
			writeError(response, http.StatusBadRequest, "query parameters are not supported")
			return
		}
		writeJSON(response, http.StatusOK, readinessView{
			Status:     "ready",
			ExecutorID: e.config.ExecutorID,
			TargetID:   e.config.TargetID,
		})
		return
	}
	if !e.authorized(request) {
		writeError(response, http.StatusUnauthorized, "authorization failed")
		return
	}
	if request.URL.RawQuery != "" {
		writeError(response, http.StatusBadRequest, "query parameters are not supported")
		return
	}
	if request.URL.Path == "/v1/operations" {
		if request.Method != http.MethodPost {
			methodNotAllowed(response, http.MethodPost)
			return
		}
		e.createOperation(response, request)
		return
	}
	const prefix = "/v1/operations/"
	if !strings.HasPrefix(request.URL.Path, prefix) {
		writeError(response, http.StatusNotFound, "route not found")
		return
	}
	parts := strings.Split(strings.TrimPrefix(request.URL.Path, prefix), "/")
	if len(parts) < 1 || len(parts) > 2 || parts[0] == "" {
		writeError(response, http.StatusNotFound, "route not found")
		return
	}
	operationID, err := uuid.Parse(parts[0])
	if err != nil {
		writeError(response, http.StatusNotFound, "operation not found")
		return
	}
	if len(parts) == 1 {
		if request.Method != http.MethodGet {
			methodNotAllowed(response, http.MethodGet)
			return
		}
		e.getOperation(response, operationID)
		return
	}
	switch parts[1] {
	case "cancel":
		if request.Method != http.MethodPost {
			methodNotAllowed(response, http.MethodPost)
			return
		}
		e.cancelOperation(response, request, operationID)
	case "logs":
		if request.Method != http.MethodGet {
			methodNotAllowed(response, http.MethodGet)
			return
		}
		e.getLogs(response, operationID)
	default:
		writeError(response, http.StatusNotFound, "route not found")
	}
}

func (e *referenceExecutor) authorized(request *http.Request) bool {
	actual := []byte(request.Header.Get("Authorization"))
	expected := []byte("Bearer " + e.config.SharedSecret)
	return len(actual) == len(expected) && subtle.ConstantTimeCompare(actual, expected) == 1
}

func (e *referenceExecutor) createOperation(response http.ResponseWriter, request *http.Request) {
	var input operationRequest
	if !decodeRequest(response, request, &input) {
		return
	}
	status, err := e.validateOperation(input)
	if err != nil {
		writeError(response, status, err.Error())
		return
	}
	behavior := e.config.TestBehavior(input.Binding)
	if behavior.LogEntries < 0 || behavior.LogEntries > 10000 {
		writeError(response, http.StatusInternalServerError, "executor behavior is invalid")
		return
	}
	switch behavior.Status {
	case types.ExecutionAttemptStatusSucceeded,
		types.ExecutionAttemptStatusFailed,
		types.ExecutionAttemptStatusRunning:
	default:
		writeError(response, http.StatusInternalServerError, "executor behavior is invalid")
		return
	}
	requestBytes, err := json.Marshal(input)
	if err != nil {
		writeError(response, http.StatusBadRequest, "operation request is invalid")
		return
	}
	requestChecksum := sha256Checksum(requestBytes)

	e.mu.Lock()
	defer e.mu.Unlock()

	id := input.Binding.AttemptID.String()
	if existing, ok := e.state.Operations[id]; ok {
		if existing.RequestChecksum != requestChecksum {
			writeError(response, http.StatusConflict, "attempt identity is already bound")
			return
		}
		writeJSON(response, http.StatusOK, e.view(existing))
		return
	}
	currentGeneration := e.state.ResourceGenerations[input.Binding.ResourceKey]
	if input.Binding.FenceGeneration <= currentGeneration {
		writeError(response, http.StatusConflict, "fence generation is stale")
		return
	}

	previous := cloneState(e.state)
	now := e.config.Now().UTC()
	for _, existing := range e.state.Operations {
		if existing.Binding.ResourceKey == input.Binding.ResourceKey &&
			existing.Binding.FenceGeneration < input.Binding.FenceGeneration &&
			!existing.Status.IsTerminal() {
			existing.Status = types.ExecutionAttemptStatusFenced
			existing.UpdatedAt = now
			e.appendLog(existing, now, "operation fenced by a newer generation")
		}
	}
	record := &persistedOperation{
		ID:              input.Binding.AttemptID,
		Binding:         input.Binding,
		RequestChecksum: requestChecksum,
		Status:          types.ExecutionAttemptStatusRunning,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	e.appendLog(record, now, "operation accepted")
	for index := 1; index <= behavior.LogEntries; index++ {
		e.appendLog(record, now, fmt.Sprintf("deterministic progress %04d", index))
	}
	switch behavior.Status {
	case types.ExecutionAttemptStatusSucceeded:
		record.Status = types.ExecutionAttemptStatusSucceeded
		e.appendLog(record, now, "operation succeeded")
	case types.ExecutionAttemptStatusFailed:
		record.Status = types.ExecutionAttemptStatusFailed
		e.appendLog(record, now, "operation failed")
	case types.ExecutionAttemptStatusRunning:
		e.appendLog(record, now, "operation waiting at a safe boundary")
	}
	e.state.Operations[id] = record
	e.state.ResourceGenerations[input.Binding.ResourceKey] = input.Binding.FenceGeneration
	if err := saveState(e.config.StateFile, e.state); err != nil {
		e.state = previous
		writeError(response, http.StatusInternalServerError, "operation state could not be persisted")
		return
	}
	writeJSON(response, http.StatusAccepted, e.view(record))
}

func (e *referenceExecutor) validateOperation(
	input operationRequest,
) (int, error) {
	if err := input.Binding.validate(); err != nil {
		return http.StatusBadRequest, err
	}
	if input.Binding.TargetID != e.config.TargetID {
		return http.StatusBadRequest, errors.New("target binding mismatch")
	}
	var payload signedIntentPayload
	if err := decodeStrictJSON(input.Intent.Payload, &payload); err != nil {
		return http.StatusBadRequest, errors.New("signed execution intent payload is invalid")
	}
	if !payload.matches(input.Binding) {
		return http.StatusBadRequest, errors.New("signed execution intent binding mismatch")
	}
	policy := types.TrustPolicy{
		Keys:                   e.config.TrustedKeys,
		Now:                    e.config.Now,
		ExpectedArtifactDigest: input.Binding.ArtifactDigest,
		ExpectedConfigChecksum: input.Binding.ConfigChecksum,
	}
	if err := executionprotocol.VerifyExecutionIntent(input.Intent, policy); err != nil {
		return http.StatusUnauthorized, errors.New("signed execution intent is not authorized")
	}
	return http.StatusOK, nil
}

func (binding operationBinding) validate() error {
	if binding.TenantID == uuid.Nil || binding.TargetID == uuid.Nil ||
		binding.AttemptID == uuid.Nil || binding.TaskID == uuid.Nil ||
		binding.StepRunID == uuid.Nil || binding.ExecutionID == uuid.Nil ||
		binding.AttemptNumber <= 0 {
		return errors.New("operation identity is invalid")
	}
	if strings.TrimSpace(binding.StepKey) == "" || len(binding.StepKey) > 128 ||
		strings.ContainsAny(binding.StepKey, "\r\n") {
		return errors.New("step binding is invalid")
	}
	if !checksumPattern.MatchString(binding.PlanChecksum) ||
		!checksumPattern.MatchString(binding.ArtifactDigest) ||
		!checksumPattern.MatchString(binding.ConfigChecksum) {
		return errors.New("immutable checksum binding is invalid")
	}
	if strings.TrimSpace(binding.AdapterRevision) == "" ||
		len(binding.AdapterRevision) > 256 ||
		strings.ContainsAny(binding.AdapterRevision, "\r\n") {
		return errors.New("adapter binding is invalid")
	}
	if strings.TrimSpace(binding.ResourceKey) == "" ||
		len(binding.ResourceKey) > 512 ||
		strings.ContainsAny(binding.ResourceKey, "\r\n") ||
		binding.FenceGeneration <= 0 {
		return errors.New("resource fence binding is invalid")
	}
	return nil
}

func (payload signedIntentPayload) matches(binding operationBinding) bool {
	return payload.Schema == "distr.execution-intent/v2" &&
		payload.OrganizationID == binding.TenantID &&
		payload.DeploymentTargetID == binding.TargetID &&
		payload.AttemptID == binding.AttemptID &&
		payload.TaskID == binding.TaskID &&
		payload.StepRunID == binding.StepRunID &&
		payload.ExecutionID == binding.ExecutionID &&
		payload.AttemptNumber == binding.AttemptNumber &&
		payload.StepKey == strings.TrimSpace(binding.StepKey) &&
		payload.PlanChecksum == binding.PlanChecksum &&
		payload.ArtifactDigest == binding.ArtifactDigest &&
		payload.ConfigChecksum == binding.ConfigChecksum &&
		payload.AdapterRevision == strings.TrimSpace(binding.AdapterRevision) &&
		payload.ResourceKey == strings.TrimSpace(binding.ResourceKey) &&
		payload.FenceGeneration == binding.FenceGeneration
}

func (e *referenceExecutor) getOperation(response http.ResponseWriter, id uuid.UUID) {
	e.mu.Lock()
	defer e.mu.Unlock()
	record, ok := e.state.Operations[id.String()]
	if !ok {
		writeError(response, http.StatusNotFound, "operation not found")
		return
	}
	writeJSON(response, http.StatusOK, e.view(record))
}

func (e *referenceExecutor) cancelOperation(
	response http.ResponseWriter,
	request *http.Request,
	id uuid.UUID,
) {
	var input cancelRequest
	if !decodeRequest(response, request, &input) {
		return
	}
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if !idempotencyKeyPattern.MatchString(input.IdempotencyKey) || input.FenceGeneration <= 0 {
		writeError(response, http.StatusBadRequest, "cancel request is invalid")
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	record, ok := e.state.Operations[id.String()]
	if !ok {
		writeError(response, http.StatusNotFound, "operation not found")
		return
	}
	if record.CancelKey != "" {
		if record.CancelKey == input.IdempotencyKey &&
			record.CancelGeneration == input.FenceGeneration {
			writeJSON(response, http.StatusOK, e.view(record))
			return
		}
		writeError(response, http.StatusConflict, "cancel identity is already bound")
		return
	}
	if input.FenceGeneration != record.Binding.FenceGeneration ||
		e.state.ResourceGenerations[record.Binding.ResourceKey] != input.FenceGeneration {
		writeError(response, http.StatusConflict, "cancel fence generation is stale")
		return
	}
	if record.Status != types.ExecutionAttemptStatusRunning {
		writeError(response, http.StatusConflict, "operation is not cancellable")
		return
	}

	previous := cloneState(e.state)
	now := e.config.Now().UTC()
	record.CancelKey = input.IdempotencyKey
	record.CancelGeneration = input.FenceGeneration
	record.Status = types.ExecutionAttemptStatusCanceled
	record.UpdatedAt = now
	e.appendLog(record, now, "operation canceled at a safe boundary")
	if err := saveState(e.config.StateFile, e.state); err != nil {
		e.state = previous
		writeError(response, http.StatusInternalServerError, "operation state could not be persisted")
		return
	}
	writeJSON(response, http.StatusOK, e.view(e.state.Operations[id.String()]))
}

func (e *referenceExecutor) getLogs(response http.ResponseWriter, id uuid.UUID) {
	e.mu.Lock()
	defer e.mu.Unlock()
	record, ok := e.state.Operations[id.String()]
	if !ok {
		writeError(response, http.StatusNotFound, "operation not found")
		return
	}
	entries := append([]logEntry(nil), record.Logs...)
	writeJSON(response, http.StatusOK, operationLogs{
		Entries: entries, Truncated: record.LogsTruncated,
	})
}

func (e *referenceExecutor) appendLog(record *persistedOperation, at time.Time, message string) {
	if record.LogsTruncated {
		return
	}
	if record.LogBytes+len(message) > e.config.MaxLogBytes {
		record.LogsTruncated = true
		return
	}
	record.Logs = append(record.Logs, logEntry{
		Sequence: len(record.Logs) + 1,
		At:       at.UTC(),
		Message:  message,
	})
	record.LogBytes += len(message)
}

func (e *referenceExecutor) view(record *persistedOperation) operationView {
	return operationView{
		ID:            record.ID,
		ExecutorID:    e.config.ExecutorID,
		Status:        record.Status,
		Binding:       record.Binding,
		CreatedAt:     record.CreatedAt,
		UpdatedAt:     record.UpdatedAt,
		LogsTruncated: record.LogsTruncated,
	}
}

func loadState(path string, maxLogBytes int) (persistedState, error) {
	state := persistedState{
		Version:             stateVersion,
		Operations:          map[string]*persistedOperation{},
		ResourceGenerations: map[string]int64{},
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return persistedState{}, fmt.Errorf("open executor state: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxStateBytes+1))
	if err != nil {
		return persistedState{}, fmt.Errorf("read executor state: %w", err)
	}
	if len(data) > maxStateBytes {
		return persistedState{}, errors.New("executor state exceeds the size limit")
	}
	if err := decodeStrictJSON(data, &state); err != nil {
		return persistedState{}, fmt.Errorf("decode executor state: %w", err)
	}
	if state.Version != stateVersion || state.Operations == nil ||
		state.ResourceGenerations == nil {
		return persistedState{}, errors.New("executor state version is unsupported")
	}
	for key, record := range state.Operations {
		if record == nil || record.ID.String() != key ||
			record.Binding.AttemptID != record.ID ||
			record.Binding.validate() != nil ||
			!checksumPattern.MatchString(record.RequestChecksum) ||
			record.LogBytes < 0 || record.LogBytes > maxLogBytes {
			return persistedState{}, errors.New("executor state contains an invalid operation")
		}
		actualLogBytes := 0
		for index, entry := range record.Logs {
			if entry.Sequence != index+1 || entry.At.IsZero() ||
				strings.ContainsAny(entry.Message, "\r\n") {
				return persistedState{}, errors.New("executor state contains an invalid log entry")
			}
			actualLogBytes += len(entry.Message)
		}
		if actualLogBytes != record.LogBytes || actualLogBytes > maxLogBytes {
			return persistedState{}, errors.New("executor state log accounting is invalid")
		}
		if state.ResourceGenerations[record.Binding.ResourceKey] <
			record.Binding.FenceGeneration {
			return persistedState{}, errors.New("executor state contains a stale resource fence")
		}
	}
	return state, nil
}

func saveState(path string, state persistedState) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create executor state directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".reference-executor-*.tmp")
	if err != nil {
		return fmt.Errorf("create executor state temporary file: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("protect executor state temporary file: %w", err)
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(state); err != nil {
		temporary.Close()
		return fmt.Errorf("encode executor state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync executor state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close executor state: %w", err)
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("replace executor state: %w", err)
	}
	return nil
}

func cloneState(state persistedState) persistedState {
	data, err := json.Marshal(state)
	if err != nil {
		panic("reference executor state is not serializable")
	}
	var cloned persistedState
	if err := json.Unmarshal(data, &cloned); err != nil {
		panic("reference executor state is not deserializable")
	}
	return cloned
}

func decodeRequest(response http.ResponseWriter, request *http.Request, target any) bool {
	if mediaType := strings.ToLower(strings.TrimSpace(strings.Split(
		request.Header.Get("Content-Type"), ";",
	)[0])); mediaType != "application/json" {
		writeError(response, http.StatusUnsupportedMediaType, "content type must be application/json")
		return false
	}
	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(response, http.StatusRequestEntityTooLarge, "request body exceeds the size limit")
		} else {
			writeError(response, http.StatusBadRequest, "request body is invalid")
		}
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(response, http.StatusBadRequest, "request body must contain one JSON value")
		return false
	}
	return true
}

func decodeStrictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("multiple JSON values are not allowed")
	}
	return nil
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	if err := json.NewEncoder(response).Encode(value); err != nil {
		return
	}
}

func writeError(response http.ResponseWriter, status int, message string) {
	writeJSON(response, status, map[string]string{"error": message})
}

func methodNotAllowed(response http.ResponseWriter, methods ...string) {
	response.Header().Set("Allow", strings.Join(methods, ", "))
	writeError(response, http.StatusMethodNotAllowed, "method not allowed")
}

func sha256Checksum(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func environmentConfig() (referenceExecutorConfig, string, error) {
	port := 8080
	if raw := strings.TrimSpace(os.Getenv("PORT")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 65535 {
			return referenceExecutorConfig{}, "", errors.New("PORT must be between 1 and 65535")
		}
		port = value
	}
	targetID, err := uuid.Parse(strings.TrimSpace(os.Getenv("TARGET_ID")))
	if err != nil {
		return referenceExecutorConfig{}, "", errors.New("TARGET_ID must be a UUID")
	}
	maxLogBytes := 8192
	if raw := strings.TrimSpace(os.Getenv("MAX_LOG_BYTES")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return referenceExecutorConfig{}, "", errors.New("MAX_LOG_BYTES must be an integer")
		}
		maxLogBytes = value
	}
	keys, err := parseTrustedKeys(os.Getenv("EXECUTOR_PUBLIC_KEYS_JSON"))
	if err != nil {
		return referenceExecutorConfig{}, "", err
	}
	config := referenceExecutorConfig{
		ExecutorID:   defaultString(os.Getenv("EXECUTOR_ID"), "reference-executor"),
		TargetID:     targetID,
		SharedSecret: os.Getenv("EXECUTOR_SHARED_SECRET"),
		TrustedKeys:  keys,
		StateFile:    defaultString(os.Getenv("STATE_FILE"), "./reference-executor-state.json"),
		MaxLogBytes:  maxLogBytes,
		Now:          time.Now,
	}
	return config, fmt.Sprintf("0.0.0.0:%d", port), nil
}

func parseTrustedKeys(value string) (map[string]ed25519.PublicKey, error) {
	encoded := map[string]string{}
	if err := decodeStrictJSON([]byte(value), &encoded); err != nil {
		return nil, errors.New("EXECUTOR_PUBLIC_KEYS_JSON must be a JSON object")
	}
	keys := make(map[string]ed25519.PublicKey, len(encoded))
	for keyID, publicValue := range encoded {
		publicKey, err := base64.StdEncoding.DecodeString(publicValue)
		if err != nil {
			publicKey, err = base64.RawStdEncoding.DecodeString(publicValue)
		}
		if err != nil || len(publicKey) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("EXECUTOR_PUBLIC_KEYS_JSON key %q is not Ed25519", keyID)
		}
		keys[keyID] = ed25519.PublicKey(publicKey)
	}
	return keys, nil
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func main() {
	config, address, err := environmentConfig()
	if err != nil {
		log.Fatal(err)
	}
	handler, err := newReferenceExecutor(config)
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	log.Printf("reference executor listening on %s", address)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
