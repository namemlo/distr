package lifecycle

import (
	"context"
	"fmt"
	"slices"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

type EligibilityService struct{}

func NewEligibilityService() EligibilityService {
	return EligibilityService{}
}

type EligibilityRequest struct {
	ReleaseBundle types.ReleaseBundle
	Channel       types.Channel
	Lifecycle     types.Lifecycle
	EnvironmentID uuid.UUID
}

type EligibilityReasonCode string

const (
	EligibilityReasonReleaseNotPublished          EligibilityReasonCode = "release_not_published"
	EligibilityReasonReleaseBlocked               EligibilityReasonCode = "release_blocked"
	EligibilityReasonReleaseArchived              EligibilityReasonCode = "release_archived"
	EligibilityReasonEnvironmentNotInLifecycle    EligibilityReasonCode = "environment_not_in_lifecycle"
	EligibilityReasonRequiredPriorPhaseIncomplete EligibilityReasonCode = "required_prior_phase_incomplete"
	EligibilityReasonApprovalRequired             EligibilityReasonCode = "approval_required"
	EligibilityReasonChannelLifecycleMismatch     EligibilityReasonCode = "channel_lifecycle_mismatch"
)

type EligibilityReason struct {
	Code    EligibilityReasonCode
	Field   string
	Message string
}

type EligibilityResult struct {
	ReleaseBundleID uuid.UUID
	ApplicationID   uuid.UUID
	ChannelID       uuid.UUID
	LifecycleID     uuid.UUID
	EnvironmentID   uuid.UUID
	EngineReady     bool
	Eligible        bool
	TargetPhase     *EligibilityPhase
	Phases          []EligibilityPhase
	Reasons         []EligibilityReason
}

type EligibilityPhase struct {
	ID                           uuid.UUID
	Name                         string
	SortOrder                    int
	EnvironmentIDs               []uuid.UUID
	Optional                     bool
	AutomaticPromotion           bool
	MinimumSuccessfulDeployments int
	ApprovalPolicyID             *uuid.UUID
	RetentionPolicyID            *uuid.UUID
	MatchesEnvironment           bool
	RequiredBeforeTarget         bool
	BlocksEligibility            bool
}

func (EligibilityService) Explain(_ context.Context, request EligibilityRequest) EligibilityResult {
	phases := slices.Clone(request.Lifecycle.Phases)
	slices.SortStableFunc(phases, func(a, b types.LifecyclePhase) int {
		return a.SortOrder - b.SortOrder
	})

	result := EligibilityResult{
		ReleaseBundleID: request.ReleaseBundle.ID,
		ApplicationID:   request.ReleaseBundle.ApplicationID,
		ChannelID:       request.ReleaseBundle.ChannelID,
		LifecycleID:     request.Lifecycle.ID,
		EnvironmentID:   request.EnvironmentID,
		EngineReady:     true,
		Eligible:        true,
	}
	targetPhaseIndex := -1
	for _, phase := range phases {
		matchesEnvironment := slices.Contains(phase.EnvironmentIDs, request.EnvironmentID)
		result.Phases = append(result.Phases, EligibilityPhase{
			ID:                           phase.ID,
			Name:                         phase.Name,
			SortOrder:                    phase.SortOrder,
			EnvironmentIDs:               slices.Clone(phase.EnvironmentIDs),
			Optional:                     phase.Optional,
			AutomaticPromotion:           phase.AutomaticPromotion,
			MinimumSuccessfulDeployments: phase.MinimumSuccessfulDeployments,
			ApprovalPolicyID:             phase.ApprovalPolicyID,
			RetentionPolicyID:            phase.RetentionPolicyID,
			MatchesEnvironment:           matchesEnvironment,
		})
		if matchesEnvironment && targetPhaseIndex == -1 {
			targetPhaseIndex = len(result.Phases) - 1
		}
	}

	if request.Channel.LifecycleID != uuid.Nil &&
		request.Lifecycle.ID != uuid.Nil &&
		request.Channel.LifecycleID != request.Lifecycle.ID {
		result.addReason(
			EligibilityReasonChannelLifecycleMismatch,
			"channel.lifecycleId",
			"channel lifecycle does not match the evaluated lifecycle",
		)
	}

	switch request.ReleaseBundle.Status {
	case types.ReleaseBundleStatusPublished:
	case types.ReleaseBundleStatusBlocked:
		result.addReason(
			EligibilityReasonReleaseBlocked,
			"status",
			"blocked release bundles cannot advance through lifecycle phases",
		)
	case types.ReleaseBundleStatusArchived:
		result.addReason(
			EligibilityReasonReleaseArchived,
			"status",
			"archived release bundles cannot advance through lifecycle phases",
		)
	default:
		result.addReason(
			EligibilityReasonReleaseNotPublished,
			"status",
			"release bundle must be published before lifecycle eligibility can pass",
		)
	}

	if targetPhaseIndex == -1 {
		result.addReason(
			EligibilityReasonEnvironmentNotInLifecycle,
			"environmentId",
			"environment is not assigned to any phase in the channel lifecycle",
		)
		result.Eligible = len(result.Reasons) == 0
		return result
	}

	for i := range targetPhaseIndex {
		if result.Phases[i].Optional {
			continue
		}
		result.Phases[i].RequiredBeforeTarget = true
		result.Phases[i].BlocksEligibility = true
		result.addReason(
			EligibilityReasonRequiredPriorPhaseIncomplete,
			"phases."+result.Phases[i].ID.String(),
			fmt.Sprintf(
				"required lifecycle phase %q has no successful deployment evidence for this release bundle",
				result.Phases[i].Name,
			),
		)
	}

	if result.Phases[targetPhaseIndex].ApprovalPolicyID != nil {
		result.Phases[targetPhaseIndex].BlocksEligibility = true
		result.addReason(
			EligibilityReasonApprovalRequired,
			"phases."+result.Phases[targetPhaseIndex].ID.String()+".approvalPolicyId",
			fmt.Sprintf(
				"lifecycle phase %q requires approval; approval evaluation is not available yet",
				result.Phases[targetPhaseIndex].Name,
			),
		)
	}

	result.Eligible = len(result.Reasons) == 0
	targetPhase := result.Phases[targetPhaseIndex]
	result.TargetPhase = &targetPhase
	return result
}

func (r *EligibilityResult) addReason(code EligibilityReasonCode, field string, message string) {
	r.Reasons = append(r.Reasons, EligibilityReason{
		Code:    code,
		Field:   field,
		Message: message,
	})
}
