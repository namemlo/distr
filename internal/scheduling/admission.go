package scheduling

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

const (
	maxAdmissionCollectionSize      = 256
	maxEmergencyAccelerationSeconds = int64(7 * 24 * 60 * 60)
)

var admissionChecksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

var protectedAdmissionGates = map[types.AdmissionGateKey]struct{}{
	types.AdmissionGateIntegrity:        {},
	types.AdmissionGateRequiredEvidence: {},
	types.AdmissionGateBackup:           {},
	types.AdmissionGateProvenance:       {},
	types.AdmissionGateObservation:      {},
	types.AdmissionGateMandatoryHealth:  {},
}

func EvaluateAdmission(
	ctx context.Context,
	request types.AdmissionRequest,
) (types.AdmissionEvaluation, error) {
	if err := ctx.Err(); err != nil {
		return types.AdmissionEvaluation{}, err
	}
	if err := validateAdmissionRequest(request); err != nil {
		return types.AdmissionEvaluation{}, err
	}
	materialChecksum, err := admissionMaterialChecksum(request)
	if err != nil {
		return types.AdmissionEvaluation{}, err
	}
	evaluation := newAdmissionEvaluation(request, materialChecksum)
	accelerations, overrideActive, err := effectiveAccelerations(request)
	if err != nil {
		return types.AdmissionEvaluation{}, err
	}

	blocked := false
	waiting := false
	accelerated := false
	for _, evidence := range normalizedGateEvidence(request.GateEvidence) {
		if evidence.Mandatory && !evidence.Satisfied {
			blocked = true
			appendAdmissionReason(&evaluation, types.AdmissionReasonMandatoryGateFailed)
		}
	}
	for _, required := range request.EffectivePolicy.RequiredEvidence {
		if !requiredEvidenceSatisfied(request.GateEvidence, required) {
			blocked = true
			appendAdmissionReason(&evaluation, types.AdmissionReasonMandatoryGateFailed)
		}
	}

	if !approvalEvidenceValid(request) {
		if overrideActive && accelerations[types.AdmissionGateApprovalWait] {
			accelerated = true
		} else {
			blocked = true
			if request.Approval.RequestID == uuid.Nil ||
				request.Approval.Evaluation.State == types.ApprovalRequestStatePending {
				appendAdmissionReason(&evaluation, types.AdmissionReasonApprovalMissing)
			} else {
				appendAdmissionReason(&evaluation, types.AdmissionReasonApprovalInvalid)
			}
		}
	}

	for _, evidence := range request.CalendarEvidence {
		if evidence.Evaluation.Allowed {
			continue
		}
		if overrideActive && accelerations[types.AdmissionGateMaintenanceWait] {
			accelerated = true
			continue
		}
		waiting = true
		appendAdmissionReason(&evaluation, types.AdmissionReasonMaintenanceWindowClosed)
	}
	for _, evidence := range request.FreezeEvidence {
		if evidence.Evaluation.Allowed && !evidence.Evaluation.Blocked {
			continue
		}
		if overrideActive && accelerations[types.AdmissionGateMaintenanceWait] {
			accelerated = true
			continue
		}
		waiting = true
		appendAdmissionReason(&evaluation, types.AdmissionReasonDeploymentFreezeActive)
	}

	switch {
	case blocked:
		evaluation.Decision = types.AdmissionDecisionBlock
	case waiting:
		evaluation.Decision = types.AdmissionDecisionWait
	default:
		evaluation.Decision = types.AdmissionDecisionAdmit
		if accelerated {
			appendAdmissionReason(&evaluation, types.AdmissionReasonEmergencyAcceleration)
		}
		if len(evaluation.ReasonCodes) == 0 {
			appendAdmissionReason(&evaluation, types.AdmissionReasonAdmitted)
		}
	}
	sort.Slice(evaluation.ReasonCodes, func(i, j int) bool {
		return evaluation.ReasonCodes[i] < evaluation.ReasonCodes[j]
	})
	evaluation.DecisionChecksum, err = admissionDecisionChecksum(evaluation)
	if err != nil {
		return types.AdmissionEvaluation{}, err
	}
	return evaluation, nil
}

func EmergencyOverrideChecksum(override types.EmergencyOverride) string {
	type checksumInput struct {
		ID                      uuid.UUID                                 `json:"id"`
		CreatedAt               string                                    `json:"createdAt"`
		OrganizationID          uuid.UUID                                 `json:"organizationId"`
		DeploymentPlanID        uuid.UUID                                 `json:"deploymentPlanId"`
		PlanRevision            int64                                     `json:"planRevision"`
		PlanChecksum            string                                    `json:"planChecksum"`
		EffectivePolicyChecksum string                                    `json:"effectivePolicyChecksum"`
		Accelerations           []types.EmergencyAcceleration             `json:"accelerations"`
		Reason                  string                                    `json:"reason"`
		ActorUserAccountID      uuid.UUID                                 `json:"actorUserAccountId"`
		ApprovalEvidence        []types.EmergencyOverrideApprovalEvidence `json:"approvalEvidence"`
		ExpiresAt               string                                    `json:"expiresAt"`
		IdempotencyKey          string                                    `json:"idempotencyKey"`
	}
	accelerations := slices.Clone(override.Accelerations)
	sort.Slice(accelerations, func(i, j int) bool {
		return accelerations[i].GateKey < accelerations[j].GateKey
	})
	approvals := slices.Clone(override.ApprovalEvidence)
	sort.Slice(approvals, func(i, j int) bool {
		return approvals[i].RequestID.String() < approvals[j].RequestID.String()
	})
	payload, err := json.Marshal(checksumInput{
		ID:                      override.ID,
		CreatedAt:               override.CreatedAt.UTC().Format(time.RFC3339Nano),
		OrganizationID:          override.OrganizationID,
		DeploymentPlanID:        override.DeploymentPlanID,
		PlanRevision:            override.PlanRevision,
		PlanChecksum:            strings.TrimSpace(override.PlanChecksum),
		EffectivePolicyChecksum: strings.TrimSpace(override.EffectivePolicyChecksum),
		Accelerations:           accelerations,
		Reason:                  strings.TrimSpace(override.Reason),
		ActorUserAccountID:      override.ActorUserAccountID,
		ApprovalEvidence:        approvals,
		ExpiresAt:               override.ExpiresAt.UTC().Format(time.RFC3339Nano),
		IdempotencyKey:          strings.TrimSpace(override.IdempotencyKey),
	})
	if err != nil {
		return ""
	}
	return checksumBytes(payload)
}

func ValidateEmergencyOverride(
	organizationID uuid.UUID,
	plan types.AdmissionPlanEvidence,
	policy types.EffectivePolicy,
	override types.EmergencyOverride,
	evaluatedAt time.Time,
) error {
	_, _, err := effectiveAccelerations(types.AdmissionRequest{
		OrganizationID:    organizationID,
		Plan:              plan,
		EffectivePolicy:   policy,
		EmergencyOverride: &override,
		EvaluatedAt:       evaluatedAt,
	})
	return err
}

func validateAdmissionRequest(request types.AdmissionRequest) error {
	if request.OrganizationID == uuid.Nil ||
		request.Plan.ID == uuid.Nil ||
		request.Plan.Revision < 1 {
		return errors.New("admission organization and immutable plan identity are required")
	}
	if request.Plan.Schema != types.AdmissionRequiredPlanSchemaV2 ||
		request.Plan.ProtocolVersion != types.AdmissionRequiredProtocolV2 {
		return errors.New("admission requires plan_schema v2 and protocol_version v2")
	}
	if !validAdmissionChecksum(request.Plan.Checksum) ||
		!validAdmissionChecksum(request.EffectivePolicy.Checksum) ||
		!validAdmissionChecksum(request.EffectivePolicy.SubscriberSetChecksum) {
		return errors.New("admission plan and effective policy checksums are invalid")
	}
	if request.EvaluatedAt.IsZero() {
		return errors.New("admission evaluation instant is required")
	}
	if len(request.EffectivePolicy.VersionIDs) == 0 ||
		len(request.EffectivePolicy.VersionIDs) > maxAdmissionCollectionSize ||
		len(request.CalendarEvidence) > maxAdmissionCollectionSize ||
		len(request.FreezeEvidence) > maxAdmissionCollectionSize ||
		len(request.GateEvidence) > maxAdmissionCollectionSize {
		return errors.New("admission evidence collection size is invalid")
	}
	for _, id := range request.EffectivePolicy.VersionIDs {
		if id == uuid.Nil {
			return errors.New("admission policy version identity is invalid")
		}
	}
	if request.Campaign != nil {
		if request.Campaign.ID == uuid.Nil ||
			request.Campaign.Revision < 1 ||
			!validAdmissionChecksum(request.Campaign.Checksum) {
			return errors.New("admission campaign evidence is invalid")
		}
	}
	if err := validateCalendarEvidence(request); err != nil {
		return err
	}
	if err := validateGateEvidence(request.GateEvidence); err != nil {
		return err
	}
	return nil
}

func validateCalendarEvidence(request types.AdmissionRequest) error {
	requiredCalendars := uuidSet(request.EffectivePolicy.AdmissionRules.MaintenanceWindowVersionIDs)
	providedCalendars := map[uuid.UUID]struct{}{}
	for _, evidence := range request.CalendarEvidence {
		if evidence.VersionID == uuid.Nil ||
			!validAdmissionChecksum(evidence.Checksum) ||
			evidence.Evaluation.CalendarVersionID == nil ||
			*evidence.Evaluation.CalendarVersionID != evidence.VersionID ||
			!evidence.Evaluation.UTCInstant.Equal(request.EvaluatedAt) ||
			strings.TrimSpace(evidence.Evaluation.EvaluationIdentity) == "" {
			return errors.New("admission calendar evidence is invalid")
		}
		providedCalendars[evidence.VersionID] = struct{}{}
	}
	if !sameUUIDSet(requiredCalendars, providedCalendars) {
		return errors.New("admission calendar versions do not match the effective policy")
	}

	requiredFreezes := uuidSet(request.EffectivePolicy.AdmissionRules.FreezeRuleVersionIDs)
	providedFreezes := map[uuid.UUID]struct{}{}
	for _, evidence := range request.FreezeEvidence {
		if evidence.RevisionID == uuid.Nil ||
			!validAdmissionChecksum(evidence.Checksum) ||
			!evidence.Evaluation.UTCInstant.Equal(request.EvaluatedAt) ||
			strings.TrimSpace(evidence.Evaluation.EvaluationIdentity) == "" {
			return errors.New("admission freeze evidence is invalid")
		}
		providedFreezes[evidence.RevisionID] = struct{}{}
	}
	if !sameUUIDSet(requiredFreezes, providedFreezes) {
		return errors.New("admission freeze revisions do not match the effective policy")
	}
	return nil
}

func validateGateEvidence(evidence []types.AdmissionGateEvidence) error {
	seen := map[types.AdmissionGateKey]struct{}{}
	for _, item := range evidence {
		if strings.TrimSpace(string(item.Key)) == "" ||
			!validAdmissionChecksum(item.Checksum) {
			return errors.New("admission gate evidence is invalid")
		}
		if _, exists := seen[item.Key]; exists {
			return errors.New("admission gate evidence keys must be unique")
		}
		seen[item.Key] = struct{}{}
	}
	return nil
}

func effectiveAccelerations(
	request types.AdmissionRequest,
) (map[types.AdmissionGateKey]bool, bool, error) {
	result := map[types.AdmissionGateKey]bool{}
	override := request.EmergencyOverride
	if override == nil {
		return result, false, nil
	}
	if override.ID == uuid.Nil ||
		override.OrganizationID != request.OrganizationID ||
		override.DeploymentPlanID != request.Plan.ID ||
		override.PlanRevision != request.Plan.Revision ||
		override.PlanChecksum != request.Plan.Checksum ||
		override.EffectivePolicyChecksum != request.EffectivePolicy.Checksum {
		return nil, false, errors.New("emergency override is not bound to exact admission material")
	}
	if override.CreatedAt.IsZero() ||
		override.ExpiresAt.IsZero() ||
		request.EvaluatedAt.Before(override.CreatedAt) ||
		!request.EvaluatedAt.Before(override.ExpiresAt) ||
		override.ExpiresAt.Sub(override.CreatedAt) > 24*time.Hour {
		return nil, false, errors.New("emergency override is expired or exceeds its maximum lifetime")
	}
	if override.Checksum == "" ||
		override.Checksum != EmergencyOverrideChecksum(*override) {
		return nil, false, errors.New("emergency override checksum is invalid")
	}
	if len(override.Accelerations) == 0 ||
		len(override.Accelerations) > maxAdmissionCollectionSize ||
		len(override.ApprovalEvidence) == 0 ||
		len(override.ApprovalEvidence) > maxAdmissionCollectionSize {
		return nil, false, errors.New("emergency override evidence is incomplete")
	}
	for _, approval := range override.ApprovalEvidence {
		if approval.RequestID == uuid.Nil ||
			approval.RequestRevision < 1 ||
			!validAdmissionChecksum(approval.RequestChecksum) ||
			!approval.Eligible {
			return nil, false, errors.New("emergency override approval evidence is invalid")
		}
	}

	allowed, minimumReasonLength, err := strictOverridePolicy(request.EffectivePolicy.OverrideRules)
	if err != nil {
		return nil, false, err
	}
	reason := strings.TrimSpace(override.Reason)
	if len(reason) < minimumReasonLength || len(reason) > 4096 {
		return nil, false, errors.New("emergency override reason does not satisfy policy")
	}
	for _, acceleration := range override.Accelerations {
		if _, protected := protectedAdmissionGates[acceleration.GateKey]; protected {
			return nil, false, fmt.Errorf(
				"protected admission gate %q cannot be accelerated",
				acceleration.GateKey,
			)
		}
		if !allowed[acceleration.GateKey] {
			return nil, false, fmt.Errorf(
				"admission gate %q is not shortenable by the effective policy",
				acceleration.GateKey,
			)
		}
		if acceleration.MaxAccelerationSeconds < 1 ||
			acceleration.MaxAccelerationSeconds > maxEmergencyAccelerationSeconds {
			return nil, false, errors.New("emergency acceleration duration is invalid")
		}
		if result[acceleration.GateKey] {
			return nil, false, errors.New("emergency acceleration gate keys must be unique")
		}
		result[acceleration.GateKey] = true
	}
	return result, true, nil
}

func strictOverridePolicy(
	rules []types.OverrideRules,
) (map[types.AdmissionGateKey]bool, int, error) {
	if len(rules) == 0 || len(rules) > maxAdmissionCollectionSize {
		return nil, 0, errors.New("effective policy does not permit emergency overrides")
	}
	var allowed map[types.AdmissionGateKey]bool
	minimumReasonLength := 1
	for _, rule := range rules {
		if !rule.Allowed ||
			rule.AuthorityGroupID == nil ||
			*rule.AuthorityGroupID == uuid.Nil {
			return nil, 0, errors.New("every effective policy authority must permit the emergency override")
		}
		if rule.MinimumReasonLength > minimumReasonLength {
			minimumReasonLength = rule.MinimumReasonLength
		}
		current := map[types.AdmissionGateKey]bool{}
		for _, key := range rule.ShortenableGateKeys {
			current[types.AdmissionGateKey(key)] = true
		}
		if allowed == nil {
			allowed = current
			continue
		}
		for key := range allowed {
			if !current[key] {
				delete(allowed, key)
			}
		}
	}
	return allowed, minimumReasonLength, nil
}

func approvalEvidenceValid(request types.AdmissionRequest) bool {
	approval := request.Approval
	return approval.RequestID != uuid.Nil &&
		approval.RequestRevision > 0 &&
		approval.Evaluation.RequestID == approval.RequestID &&
		approval.Evaluation.Eligible &&
		approval.Evaluation.State == types.ApprovalRequestStateApproved &&
		approval.SubjectChecksum == request.Plan.Checksum &&
		approval.EffectivePolicyChecksum == request.EffectivePolicy.Checksum &&
		approval.SubscriberSetChecksum == request.EffectivePolicy.SubscriberSetChecksum
}

func requiredEvidenceSatisfied(
	evidence []types.AdmissionGateEvidence,
	key string,
) bool {
	for _, item := range evidence {
		if string(item.Key) == strings.TrimSpace(key) {
			return item.Mandatory && item.Satisfied
		}
	}
	return false
}

func newAdmissionEvaluation(
	request types.AdmissionRequest,
	materialChecksum string,
) types.AdmissionEvaluation {
	evaluation := types.AdmissionEvaluation{
		OrganizationID:          request.OrganizationID,
		DeploymentPlanID:        request.Plan.ID,
		PlanRevision:            request.Plan.Revision,
		PlanChecksum:            request.Plan.Checksum,
		PlanSchema:              request.Plan.Schema,
		ProtocolVersion:         request.Plan.ProtocolVersion,
		EffectivePolicyChecksum: request.EffectivePolicy.Checksum,
		PolicyVersionIDs:        sortedUUIDs(request.EffectivePolicy.VersionIDs),
		CalendarVersionIDs:      calendarVersionIDs(request.CalendarEvidence),
		FreezeRevisionIDs:       freezeRevisionIDs(request.FreezeEvidence),
		ReasonCodes:             []types.AdmissionReasonCode{},
		EvaluatedAt:             request.EvaluatedAt.UTC(),
		TemporalEvidence: types.AdmissionTemporalEvidence{
			EvaluatedAt:      request.EvaluatedAt.UTC(),
			CalendarEvidence: slices.Clone(request.CalendarEvidence),
			FreezeEvidence:   slices.Clone(request.FreezeEvidence),
		},
		GateEvidence:     normalizedGateEvidence(request.GateEvidence),
		MaterialChecksum: materialChecksum,
	}
	if request.Campaign != nil {
		evaluation.CampaignID = new(request.Campaign.ID)
		evaluation.CampaignRevision = new(request.Campaign.Revision)
		evaluation.CampaignChecksum = request.Campaign.Checksum
	}
	if request.Approval.RequestID != uuid.Nil {
		evaluation.ApprovalRequestID = new(request.Approval.RequestID)
		evaluation.ApprovalRequestRevision = new(request.Approval.RequestRevision)
	}
	if request.EmergencyOverride != nil {
		evaluation.EmergencyOverrideID = new(request.EmergencyOverride.ID)
		evaluation.EmergencyOverrideChecksum = request.EmergencyOverride.Checksum
	}
	return evaluation
}

func admissionMaterialChecksum(request types.AdmissionRequest) (string, error) {
	type material struct {
		OrganizationID     uuid.UUID                        `json:"organizationId"`
		Plan               types.AdmissionPlanEvidence      `json:"plan"`
		Campaign           *types.AdmissionCampaignEvidence `json:"campaign,omitempty"`
		PolicyVersionIDs   []uuid.UUID                      `json:"policyVersionIds"`
		PolicyChecksum     string                           `json:"policyChecksum"`
		SubscriberChecksum string                           `json:"subscriberSetChecksum"`
		CalendarVersions   []versionChecksum                `json:"calendarVersions"`
		FreezeRevisions    []versionChecksum                `json:"freezeRevisions"`
		Approval           types.AdmissionApprovalEvidence  `json:"approval"`
		GateEvidence       []types.AdmissionGateEvidence    `json:"gateEvidence"`
		OverrideChecksum   string                           `json:"overrideChecksum,omitempty"`
	}
	value := material{
		OrganizationID:     request.OrganizationID,
		Plan:               request.Plan,
		Campaign:           request.Campaign,
		PolicyVersionIDs:   sortedUUIDs(request.EffectivePolicy.VersionIDs),
		PolicyChecksum:     request.EffectivePolicy.Checksum,
		SubscriberChecksum: request.EffectivePolicy.SubscriberSetChecksum,
		CalendarVersions:   calendarVersionChecksums(request.CalendarEvidence),
		FreezeRevisions:    freezeRevisionChecksums(request.FreezeEvidence),
		Approval:           request.Approval,
		GateEvidence:       normalizedGateEvidence(request.GateEvidence),
	}
	if request.EmergencyOverride != nil {
		value.OverrideChecksum = request.EmergencyOverride.Checksum
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal admission material: %w", err)
	}
	return checksumBytes(payload), nil
}

type versionChecksum struct {
	ID       uuid.UUID `json:"id"`
	Checksum string    `json:"checksum"`
}

func calendarVersionChecksums(
	evidence []types.AdmissionCalendarEvidence,
) []versionChecksum {
	result := make([]versionChecksum, 0, len(evidence))
	for _, item := range evidence {
		result = append(result, versionChecksum{ID: item.VersionID, Checksum: item.Checksum})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID.String() < result[j].ID.String() })
	return result
}

func freezeRevisionChecksums(
	evidence []types.AdmissionFreezeEvidence,
) []versionChecksum {
	result := make([]versionChecksum, 0, len(evidence))
	for _, item := range evidence {
		result = append(result, versionChecksum{ID: item.RevisionID, Checksum: item.Checksum})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID.String() < result[j].ID.String() })
	return result
}

func admissionDecisionChecksum(evaluation types.AdmissionEvaluation) (string, error) {
	payload, err := json.Marshal(struct {
		MaterialChecksum string                          `json:"materialChecksum"`
		Decision         types.AdmissionDecision         `json:"decision"`
		ReasonCodes      []types.AdmissionReasonCode     `json:"reasonCodes"`
		EvaluatedAt      string                          `json:"evaluatedAt"`
		TemporalEvidence types.AdmissionTemporalEvidence `json:"temporalEvidence"`
	}{
		MaterialChecksum: evaluation.MaterialChecksum,
		Decision:         evaluation.Decision,
		ReasonCodes:      evaluation.ReasonCodes,
		EvaluatedAt:      evaluation.EvaluatedAt.UTC().Format(time.RFC3339Nano),
		TemporalEvidence: evaluation.TemporalEvidence,
	})
	if err != nil {
		return "", fmt.Errorf("marshal admission decision: %w", err)
	}
	return checksumBytes(payload), nil
}

func normalizedGateEvidence(
	evidence []types.AdmissionGateEvidence,
) []types.AdmissionGateEvidence {
	result := slices.Clone(evidence)
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result
}

func calendarVersionIDs(evidence []types.AdmissionCalendarEvidence) []uuid.UUID {
	result := make([]uuid.UUID, 0, len(evidence))
	for _, item := range evidence {
		result = append(result, item.VersionID)
	}
	return sortedUUIDs(result)
}

func freezeRevisionIDs(evidence []types.AdmissionFreezeEvidence) []uuid.UUID {
	result := make([]uuid.UUID, 0, len(evidence))
	for _, item := range evidence {
		result = append(result, item.RevisionID)
	}
	return sortedUUIDs(result)
}

func sortedUUIDs(ids []uuid.UUID) []uuid.UUID {
	result := slices.Clone(ids)
	sort.Slice(result, func(i, j int) bool { return result[i].String() < result[j].String() })
	return result
}

func uuidSet(ids []uuid.UUID) map[uuid.UUID]struct{} {
	result := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		result[id] = struct{}{}
	}
	return result
}

func sameUUIDSet(left, right map[uuid.UUID]struct{}) bool {
	if len(left) != len(right) {
		return false
	}
	for id := range left {
		if _, ok := right[id]; !ok {
			return false
		}
	}
	return true
}

func appendAdmissionReason(
	evaluation *types.AdmissionEvaluation,
	reason types.AdmissionReasonCode,
) {
	if !slices.Contains(evaluation.ReasonCodes, reason) {
		evaluation.ReasonCodes = append(evaluation.ReasonCodes, reason)
	}
}

func validAdmissionChecksum(value string) bool {
	return admissionChecksumPattern.MatchString(strings.TrimSpace(value))
}

func checksumBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}
