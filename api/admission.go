package api

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

const (
	admissionMaximumEvidenceItems = 256
	admissionMaximumOverrideHours = 24
)

var (
	admissionIdempotencyKeyPattern = regexp.MustCompile(
		`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`,
	)
	admissionChecksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type AdmitDeploymentPlanRequest struct {
	SchedulerIdempotencyKey string                           `json:"schedulerIdempotencyKey"`
	EvaluatedAt             time.Time                        `json:"evaluatedAt"`
	GateEvidence            []types.AdmissionGateEvidence    `json:"gateEvidence"`
	Campaign                *types.AdmissionCampaignEvidence `json:"campaign,omitempty"`
}

func (request *AdmitDeploymentPlanRequest) Validate() error {
	request.SchedulerIdempotencyKey = strings.TrimSpace(request.SchedulerIdempotencyKey)
	if !admissionIdempotencyKeyPattern.MatchString(request.SchedulerIdempotencyKey) {
		return validation.NewValidationFailedError(
			"schedulerIdempotencyKey must be 1-128 URL-safe characters",
		)
	}
	if request.EvaluatedAt.IsZero() {
		return validation.NewValidationFailedError("evaluatedAt is required")
	}
	request.EvaluatedAt = request.EvaluatedAt.UTC()
	if len(request.GateEvidence) > admissionMaximumEvidenceItems {
		return validation.NewValidationFailedError("gateEvidence contains too many items")
	}
	seen := make(map[types.AdmissionGateKey]struct{}, len(request.GateEvidence))
	for index, evidence := range request.GateEvidence {
		if strings.TrimSpace(string(evidence.Key)) == "" {
			return validation.NewValidationFailedError(
				fmt.Sprintf("gateEvidence[%d].key is required", index),
			)
		}
		if !admissionChecksumPattern.MatchString(strings.TrimSpace(evidence.Checksum)) {
			return validation.NewValidationFailedError(fmt.Sprintf(
				"gateEvidence[%d].checksum is invalid",
				index,
			))
		}
		if _, exists := seen[evidence.Key]; exists {
			return validation.NewValidationFailedError("gateEvidence contains duplicate gate keys")
		}
		seen[evidence.Key] = struct{}{}
	}
	if request.Campaign != nil {
		if request.Campaign.ID == uuid.Nil ||
			request.Campaign.Revision < 1 ||
			!admissionChecksumPattern.MatchString(strings.TrimSpace(request.Campaign.Checksum)) {
			return validation.NewValidationFailedError("campaign evidence is invalid")
		}
	}
	return nil
}

type CreateEmergencyOverrideRequest struct {
	Accelerations      []types.EmergencyAcceleration `json:"accelerations"`
	Reason             string                        `json:"reason"`
	ApprovalRequestIDs []uuid.UUID                   `json:"approvalRequestIds"`
	ExpiresAt          time.Time                     `json:"expiresAt"`
	IdempotencyKey     string                        `json:"idempotencyKey"`
}

func (request *CreateEmergencyOverrideRequest) Validate(now time.Time) error {
	request.Reason = strings.TrimSpace(request.Reason)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if len(request.Accelerations) == 0 ||
		len(request.Accelerations) > admissionMaximumEvidenceItems {
		return validation.NewValidationFailedError(
			"accelerations must contain between 1 and 256 items",
		)
	}
	protected := map[types.AdmissionGateKey]struct{}{
		types.AdmissionGateIntegrity:        {},
		types.AdmissionGateRequiredEvidence: {},
		types.AdmissionGateBackup:           {},
		types.AdmissionGateProvenance:       {},
		types.AdmissionGateObservation:      {},
		types.AdmissionGateMandatoryHealth:  {},
	}
	seenAccelerations := make(map[types.AdmissionGateKey]struct{}, len(request.Accelerations))
	for index, acceleration := range request.Accelerations {
		if _, denied := protected[acceleration.GateKey]; denied {
			return validation.NewValidationFailedError(fmt.Sprintf(
				"accelerations[%d] targets a protected admission gate",
				index,
			))
		}
		if acceleration.MaxAccelerationSeconds < 1 ||
			acceleration.MaxAccelerationSeconds > 7*24*60*60 {
			return validation.NewValidationFailedError(fmt.Sprintf(
				"accelerations[%d].maxAccelerationSeconds is invalid",
				index,
			))
		}
		if _, exists := seenAccelerations[acceleration.GateKey]; exists {
			return validation.NewValidationFailedError("accelerations contains duplicate gate keys")
		}
		seenAccelerations[acceleration.GateKey] = struct{}{}
	}
	if len(request.Reason) < 1 || len(request.Reason) > 4096 ||
		strings.ContainsAny(request.Reason, "\r\n") {
		return validation.NewValidationFailedError(
			"reason must contain 1-4096 single-line characters",
		)
	}
	if len(request.ApprovalRequestIDs) == 0 ||
		len(request.ApprovalRequestIDs) > admissionMaximumEvidenceItems {
		return validation.NewValidationFailedError(
			"approvalRequestIds must contain between 1 and 256 items",
		)
	}
	seenApprovals := make(map[uuid.UUID]struct{}, len(request.ApprovalRequestIDs))
	for _, requestID := range request.ApprovalRequestIDs {
		if requestID == uuid.Nil {
			return validation.NewValidationFailedError("approvalRequestIds contains an empty ID")
		}
		if _, exists := seenApprovals[requestID]; exists {
			return validation.NewValidationFailedError("approvalRequestIds contains duplicates")
		}
		seenApprovals[requestID] = struct{}{}
	}
	now = now.UTC()
	if !request.ExpiresAt.After(now) ||
		request.ExpiresAt.After(now.Add(admissionMaximumOverrideHours*time.Hour)) {
		return validation.NewValidationFailedError(
			"expiresAt must be in the next 24 hours",
		)
	}
	if !admissionIdempotencyKeyPattern.MatchString(request.IdempotencyKey) {
		return validation.NewValidationFailedError(
			"idempotencyKey must be 1-128 URL-safe characters",
		)
	}
	return nil
}

type AdmissionEvaluation struct {
	ID                        uuid.UUID                       `json:"id"`
	CreatedAt                 time.Time                       `json:"createdAt"`
	DeploymentPlanID          uuid.UUID                       `json:"deploymentPlanId"`
	PlanRevision              int64                           `json:"planRevision"`
	PlanChecksum              string                          `json:"planChecksum"`
	PlanSchema                string                          `json:"planSchema"`
	ProtocolVersion           string                          `json:"protocolVersion"`
	CampaignID                *uuid.UUID                      `json:"campaignId,omitempty"`
	CampaignRevision          *int64                          `json:"campaignRevision,omitempty"`
	CampaignChecksum          string                          `json:"campaignChecksum,omitempty"`
	EffectivePolicyChecksum   string                          `json:"effectivePolicyChecksum"`
	PolicyVersionIDs          []uuid.UUID                     `json:"policyVersionIds"`
	CalendarVersionIDs        []uuid.UUID                     `json:"calendarVersionIds"`
	FreezeRevisionIDs         []uuid.UUID                     `json:"freezeRevisionIds"`
	ApprovalRequestID         *uuid.UUID                      `json:"approvalRequestId,omitempty"`
	ApprovalRequestRevision   *int64                          `json:"approvalRequestRevision,omitempty"`
	EmergencyOverrideID       *uuid.UUID                      `json:"emergencyOverrideId,omitempty"`
	EmergencyOverrideChecksum string                          `json:"emergencyOverrideChecksum,omitempty"`
	Decision                  types.AdmissionDecision         `json:"decision"`
	ReasonCodes               []types.AdmissionReasonCode     `json:"reasonCodes"`
	EvaluatedAt               time.Time                       `json:"evaluatedAt"`
	TemporalEvidence          types.AdmissionTemporalEvidence `json:"temporalEvidence"`
	GateEvidence              []types.AdmissionGateEvidence   `json:"gateEvidence"`
	MaterialChecksum          string                          `json:"materialChecksum"`
	DecisionChecksum          string                          `json:"decisionChecksum"`
	SchedulerIdempotencyKey   string                          `json:"schedulerIdempotencyKey"`
}

type EmergencyOverride struct {
	ID                      uuid.UUID                                 `json:"id"`
	CreatedAt               time.Time                                 `json:"createdAt"`
	DeploymentPlanID        uuid.UUID                                 `json:"deploymentPlanId"`
	PlanRevision            int64                                     `json:"planRevision"`
	PlanChecksum            string                                    `json:"planChecksum"`
	EffectivePolicyChecksum string                                    `json:"effectivePolicyChecksum"`
	Accelerations           []types.EmergencyAcceleration             `json:"accelerations"`
	Reason                  string                                    `json:"reason"`
	ActorUserAccountID      uuid.UUID                                 `json:"actorUserAccountId"`
	ApprovalEvidence        []types.EmergencyOverrideApprovalEvidence `json:"approvalEvidence"`
	ExpiresAt               time.Time                                 `json:"expiresAt"`
	Checksum                string                                    `json:"checksum"`
	IdempotencyKey          string                                    `json:"idempotencyKey"`
}
