package api

import (
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/externalexecution"
	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

const maxExternalExecutionReferenceLength = 2048

var (
	externalExecutionChecksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	externalExecutionImagePattern    = regexp.MustCompile(`^\S+@sha256:[0-9a-fA-F]{64}$`)
)

type ExternalExecutionObservedState struct {
	Version         string                         `json:"version"`
	Image           string                         `json:"image"`
	Platform        types.DeploymentTargetPlatform `json:"platform"`
	Contracts       []string                       `json:"contracts"`
	ConfigReference string                         `json:"configReference"`
	ConfigChecksum  string                         `json:"configChecksum"`
	Health          types.TargetComponentHealth    `json:"health"`
}

type ExternalExecutionCallbackRequest struct {
	Sequence          int64                           `json:"sequence"`
	Status            types.ExternalExecutionStatus   `json:"status"`
	ProviderReference string                          `json:"providerReference,omitempty"`
	ProviderURL       string                          `json:"providerUrl,omitempty"`
	Message           string                          `json:"message,omitempty"`
	ObservedState     *ExternalExecutionObservedState `json:"observedState,omitempty"`
}

func (r *ExternalExecutionCallbackRequest) Validate() error {
	r.ProviderReference = strings.TrimSpace(r.ProviderReference)
	r.ProviderURL = strings.TrimSpace(r.ProviderURL)
	r.Message = strings.TrimSpace(r.Message)
	if r.Sequence <= 0 {
		return validation.NewValidationFailedError("sequence must be greater than 0")
	}
	if !r.Status.IsCallbackStatus() {
		return validation.NewValidationFailedError("status must be a callback state")
	}
	if len(r.ProviderReference) > 512 || strings.ContainsAny(r.ProviderReference, "\r\n") {
		return validation.NewValidationFailedError("providerReference is invalid")
	}
	if err := externalexecution.ValidateProviderURL(r.ProviderURL); err != nil {
		return validation.NewValidationFailedError(err.Error())
	}
	if len(r.Message) > types.MaxStepRunEventMessageLength {
		return validation.NewValidationFailedError("message is too long")
	}
	if (r.Status == types.ExternalExecutionStatusFailed || r.Status == types.ExternalExecutionStatusCanceled) &&
		r.Message == "" {
		return validation.NewValidationFailedError("message is required for failed or canceled callbacks")
	}
	if r.Status == types.ExternalExecutionStatusSucceeded {
		if r.ObservedState == nil {
			return validation.NewValidationFailedError("observedState is required for succeeded callbacks")
		}
		if err := r.ObservedState.validate(); err != nil {
			return err
		}
	} else if r.ObservedState != nil {
		return validation.NewValidationFailedError("observedState is only allowed for succeeded callbacks")
	}
	return nil
}

func (s *ExternalExecutionObservedState) validate() error {
	s.Version = strings.TrimSpace(s.Version)
	s.Image = strings.TrimSpace(s.Image)
	s.ConfigReference = strings.TrimSpace(s.ConfigReference)
	s.ConfigChecksum = strings.TrimSpace(s.ConfigChecksum)
	if s.Version == "" || len(s.Version) > 128 {
		return validation.NewValidationFailedError("observedState.version is required")
	}
	if !externalExecutionImagePattern.MatchString(s.Image) {
		return validation.NewValidationFailedError("observedState.image must use an immutable sha256 digest")
	}
	if !s.Platform.IsValid() {
		return validation.NewValidationFailedError("observedState.platform is invalid")
	}
	if s.ConfigReference == "" || len(s.ConfigReference) > maxExternalExecutionReferenceLength ||
		strings.ContainsAny(s.ConfigReference, "\r\n") {
		return validation.NewValidationFailedError("observedState.configReference is required")
	}
	if !externalExecutionChecksumPattern.MatchString(s.ConfigChecksum) {
		return validation.NewValidationFailedError("observedState.configChecksum must be a sha256 checksum")
	}
	if s.Health != types.TargetComponentHealthHealthy {
		return validation.NewValidationFailedError("observedState.health must be HEALTHY for a succeeded callback")
	}
	if len(s.Contracts) > 256 {
		return validation.NewValidationFailedError("observedState.contracts contains too many entries")
	}
	seen := map[string]struct{}{}
	for i := range s.Contracts {
		s.Contracts[i] = strings.TrimSpace(s.Contracts[i])
		if s.Contracts[i] == "" || len(s.Contracts[i]) > 512 {
			return validation.NewValidationFailedError("observedState.contracts contains an invalid entry")
		}
		if _, exists := seen[s.Contracts[i]]; exists {
			return validation.NewValidationFailedError("observedState.contracts must be unique")
		}
		seen[s.Contracts[i]] = struct{}{}
	}
	slices.Sort(s.Contracts)
	return nil
}

func (r ExternalExecutionCallbackRequest) ToTypes(
	orgID, executionID uuid.UUID,
) types.RecordExternalExecutionCallbackRequest {
	request := types.RecordExternalExecutionCallbackRequest{
		OrganizationID: orgID, ExternalExecutionID: executionID, Sequence: r.Sequence, Status: r.Status,
		ProviderReference: r.ProviderReference, ProviderURL: r.ProviderURL, Message: r.Message,
	}
	if r.ObservedState != nil {
		request.ObservedState = &types.ExternalExecutionObservedState{
			Version: r.ObservedState.Version, Image: r.ObservedState.Image, Platform: r.ObservedState.Platform,
			Contracts: slices.Clone(r.ObservedState.Contracts), ConfigReference: r.ObservedState.ConfigReference,
			ConfigChecksum: r.ObservedState.ConfigChecksum, Health: r.ObservedState.Health,
		}
	}
	return request
}

type ExternalExecution struct {
	ID                     uuid.UUID                       `json:"id"`
	CreatedAt              time.Time                       `json:"createdAt"`
	UpdatedAt              time.Time                       `json:"updatedAt"`
	StartedAt              *time.Time                      `json:"startedAt,omitempty"`
	CompletedAt            *time.Time                      `json:"completedAt,omitempty"`
	CallbackDeadlineAt     time.Time                       `json:"callbackDeadlineAt"`
	StepRunID              uuid.UUID                       `json:"stepRunId"`
	TaskID                 uuid.UUID                       `json:"taskId"`
	DeploymentPlanID       uuid.UUID                       `json:"deploymentPlanId"`
	DeploymentPlanTargetID uuid.UUID                       `json:"deploymentPlanTargetId"`
	DeploymentTargetID     uuid.UUID                       `json:"deploymentTargetId"`
	ApplicationID          uuid.UUID                       `json:"applicationId"`
	ReleaseBundleID        uuid.UUID                       `json:"releaseBundleId"`
	Component              string                          `json:"component"`
	PlanChecksum           string                          `json:"planChecksum"`
	IdempotencyKey         string                          `json:"idempotencyKey"`
	ExpectedStateVersion   int64                           `json:"expectedStateVersion"`
	ExpectedStateChecksum  string                          `json:"expectedStateChecksum"`
	ExpectedState          ExternalExecutionExpectedState  `json:"expectedState"`
	Status                 types.ExternalExecutionStatus   `json:"status"`
	ProviderReference      string                          `json:"providerReference,omitempty"`
	ProviderURL            string                          `json:"providerUrl,omitempty"`
	TriggerAttempts        int                             `json:"triggerAttempts"`
	LastCallbackSequence   int64                           `json:"lastCallbackSequence"`
	LastMessage            string                          `json:"lastMessage,omitempty"`
	ErrorSummary           string                          `json:"errorSummary,omitempty"`
	ObservedState          *ExternalExecutionObservedState `json:"observedState,omitempty"`
	ObservedStateChecksum  string                          `json:"observedStateChecksum,omitempty"`
	Events                 []ExternalExecutionEvent        `json:"events"`
}

type ExternalExecutionExpectedState struct {
	Version          string                         `json:"version"`
	Image            string                         `json:"image"`
	Platform         types.DeploymentTargetPlatform `json:"platform"`
	Contracts        []string                       `json:"contracts"`
	ConfigReference  string                         `json:"configReference"`
	ConfigChecksum   string                         `json:"configChecksum"`
	ComposeReference string                         `json:"composeReference"`
	ComposeChecksum  string                         `json:"composeChecksum"`
}

type ExternalExecutionEvent struct {
	ID                uuid.UUID                     `json:"id"`
	CreatedAt         time.Time                     `json:"createdAt"`
	Sequence          int64                         `json:"sequence"`
	Status            types.ExternalExecutionStatus `json:"status"`
	ProviderReference string                        `json:"providerReference,omitempty"`
	ProviderURL       string                        `json:"providerUrl,omitempty"`
	Message           string                        `json:"message,omitempty"`
}
