package externalexecution

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/distr-sh/distr/internal/types"
)

const MaxEventCount int64 = 256

func CanCallbackTransition(from, to types.ExternalExecutionStatus) bool {
	if !to.IsCallbackStatus() || from.IsTerminal() {
		return false
	}
	return from == types.ExternalExecutionStatusQueued || from == types.ExternalExecutionStatusRunning
}

func ValidateCallbackSequence(sequence int64, status types.ExternalExecutionStatus) error {
	if sequence > MaxEventCount {
		return fmt.Errorf("external execution callback history limit exceeded")
	}
	if sequence == MaxEventCount && !status.IsTerminal() {
		return fmt.Errorf("final external execution callback event must be terminal")
	}
	return nil
}

func ValidateProviderURL(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if len(value) > 2048 || strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("provider URL is invalid")
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("provider URL must be an HTTP or HTTPS URL")
	}
	if parsed.User != nil {
		return fmt.Errorf("provider URL must not contain credentials")
	}
	if parsed.RawQuery != "" {
		return fmt.Errorf("provider URL must not contain query parameters")
	}
	return nil
}

func ObservedStateChecksum(state types.ExternalExecutionObservedState) (string, error) {
	canonical := state
	canonical.Contracts = slices.Clone(state.Contracts)
	for i := range canonical.Contracts {
		canonical.Contracts[i] = strings.TrimSpace(canonical.Contracts[i])
	}
	slices.Sort(canonical.Contracts)
	payload, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("canonicalize observed state: %w", err)
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func CallbackPayloadHash(request types.RecordExternalExecutionCallbackRequest) (string, error) {
	canonical := struct {
		Sequence          int64                                 `json:"sequence"`
		Status            types.ExternalExecutionStatus         `json:"status"`
		ProviderReference string                                `json:"providerReference,omitempty"`
		ProviderURL       string                                `json:"providerUrl,omitempty"`
		Message           string                                `json:"message,omitempty"`
		ObservedState     *types.ExternalExecutionObservedState `json:"observedState,omitempty"`
	}{
		Sequence: request.Sequence, Status: request.Status, ProviderReference: request.ProviderReference,
		ProviderURL: request.ProviderURL, Message: request.Message, ObservedState: request.ObservedState,
	}
	if canonical.ObservedState != nil {
		cloned := *canonical.ObservedState
		cloned.Contracts = slices.Clone(cloned.Contracts)
		slices.Sort(cloned.Contracts)
		canonical.ObservedState = &cloned
	}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("canonicalize callback payload: %w", err)
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func ValidateObservedState(
	expected types.ExternalExecutionExpectedState,
	actual types.ExternalExecutionObservedState,
) error {
	if actual.Version != expected.Version {
		return fmt.Errorf("observed version does not match frozen execution")
	}
	if !strings.EqualFold(actual.Image, expected.Image) {
		return fmt.Errorf("observed image does not match frozen execution")
	}
	if actual.Platform != expected.Platform {
		return fmt.Errorf("observed platform does not match frozen execution")
	}
	expectedContracts := slices.Clone(expected.Contracts)
	actualContracts := slices.Clone(actual.Contracts)
	slices.Sort(expectedContracts)
	slices.Sort(actualContracts)
	if !slices.Equal(expectedContracts, actualContracts) {
		return fmt.Errorf("observed contracts do not match frozen execution")
	}
	if actual.ConfigReference != expected.ConfigReference {
		return fmt.Errorf("observed config reference does not match frozen execution")
	}
	if actual.ConfigChecksum != expected.ConfigChecksum {
		return fmt.Errorf("observed config checksum does not match frozen execution")
	}
	if actual.Health != types.TargetComponentHealthHealthy {
		return fmt.Errorf("observed health is not HEALTHY")
	}
	return nil
}
