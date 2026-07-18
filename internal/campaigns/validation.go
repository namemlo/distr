package campaigns

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

var campaignChecksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func ValidateCampaignDraft(
	ctx context.Context,
	draft types.CampaignDraft,
) []types.ValidationIssue {
	issues := make([]types.ValidationIssue, 0)
	if draft.OrganizationID == uuid.Nil {
		issues = append(issues, campaignIssue(
			"campaign.organization.required",
			"organizationId",
			"organization is required",
		))
	}
	if strings.TrimSpace(draft.Name) == "" || len(draft.Name) > 200 {
		issues = append(issues, campaignIssue(
			"campaign.name.invalid",
			"name",
			"name must contain between 1 and 200 characters",
		))
	}
	members, err := ResolveCampaignMembership(ctx, draft)
	if err != nil {
		issues = append(issues, campaignIssue(
			"campaign.membership.invalid",
			"membership",
			err.Error(),
		))
	}

	candidates := make(map[uuid.UUID]types.CampaignPlanCandidate, len(draft.CandidatePlans))
	for _, candidate := range draft.CandidatePlans {
		candidates[candidate.PlanID] = candidate
	}
	units := make(map[uuid.UUID]uuid.UUID)
	for index, member := range members {
		candidate := candidates[member.PlanID]
		field := fmt.Sprintf("membership.planIds[%d]", index)
		if !candidate.Approved ||
			candidate.ApprovalRequestID == uuid.Nil ||
			candidate.ApprovalRequestRevision <= 0 ||
			candidate.ApprovalChecksum != candidate.PlanChecksum ||
			!campaignChecksumPattern.MatchString(candidate.ApprovalChecksum) {
			issues = append(issues, campaignIssue(
				"campaign.member.unapproved",
				field,
				"campaign members require a current checksum-bound approval",
			))
		}
		if !candidate.Admitted ||
			candidate.AdmissionEvaluationID == uuid.Nil ||
			!campaignChecksumPattern.MatchString(candidate.AdmissionChecksum) ||
			!campaignChecksumPattern.MatchString(
				candidate.EffectivePolicyChecksum,
			) ||
			len(candidate.CalendarVersionIDs) != len(candidate.CalendarChecksums) ||
			!campaignChecksumsValid(candidate.CalendarChecksums) {
			issues = append(issues, campaignIssue(
				"campaign.member.admission_invalid",
				field,
				"campaign members require exact policy, calendar, and admission evidence",
			))
		}
		if !campaignChecksumPattern.MatchString(candidate.PlanChecksum) ||
			candidate.PlanChecksum != candidate.CurrentPlanChecksum {
			issues = append(issues, campaignIssue(
				"campaign.member.plan_checksum_mismatch",
				field,
				"deployment plan checksum changed after selection",
			))
		}
		if candidate.DeploymentUnitID == uuid.Nil {
			issues = append(issues, campaignIssue(
				"campaign.member.deployment_unit_required",
				field,
				"deployment plan must identify a deployment unit",
			))
		} else if otherPlanID, duplicate := units[candidate.DeploymentUnitID]; duplicate {
			issues = append(issues, campaignIssue(
				"campaign.member.duplicate_deployment_unit",
				field,
				fmt.Sprintf(
					"deployment unit is already selected by plan %s",
					otherPlanID,
				),
			))
		} else {
			units[candidate.DeploymentUnitID] = candidate.PlanID
		}
	}

	validateCampaignWaves(draft.Waves, &issues)
	memberIDs := make(map[uuid.UUID]struct{}, len(members))
	for _, member := range members {
		memberIDs[member.PlanID] = struct{}{}
	}
	for index, prerequisite := range draft.Prerequisites {
		field := fmt.Sprintf("prerequisites[%d]", index)
		upstream, upstreamExists := candidates[prerequisite.UpstreamPlanID]
		_, downstreamSelected := memberIDs[prerequisite.DownstreamPlanID]
		_, upstreamSelected := memberIDs[prerequisite.UpstreamPlanID]
		if !upstreamExists || !upstreamSelected || !downstreamSelected ||
			prerequisite.DownstreamPlanID == prerequisite.UpstreamPlanID {
			issues = append(issues, campaignIssue(
				"campaign.prerequisite.member_invalid",
				field,
				"prerequisite plans must be distinct selected campaign members",
			))
			continue
		}
		if strings.TrimSpace(prerequisite.UpstreamStepKey) == "" {
			issues = append(issues, campaignIssue(
				"campaign.prerequisite.step_required",
				field+".upstreamStepKey",
				"upstream step key is required",
			))
		}
		if !containsUUID(
			upstream.SharedProviderPlacements,
			prerequisite.ProviderPlacementID,
		) {
			issues = append(issues, campaignIssue(
				"campaign.prerequisite.shared_provider_invalid",
				field+".providerPlacementId",
				"provider placement is not a frozen shared-provider placement",
			))
		}
		evidence := upstream.ExpectedStepPlacementEvidence[types.CampaignStepPlacement{
			StepKey:     prerequisite.UpstreamStepKey,
			PlacementID: prerequisite.ProviderPlacementID,
		}]
		if evidence.ProviderDeploymentUnitID == uuid.Nil ||
			evidence.ProviderComponentInstanceID == uuid.Nil {
			issues = append(issues, campaignIssue(
				"campaign.prerequisite.provider_identity_missing",
				field+".providerPlacementId",
				"provider placement has no frozen canonical component identity",
			))
		}
		if !campaignChecksumPattern.MatchString(
			prerequisite.ExpectedRuntimeStateChecksum,
		) || evidence.ExpectedRuntimeStateChecksum == "" ||
			evidence.ExpectedRuntimeStateChecksum !=
				prerequisite.ExpectedRuntimeStateChecksum {
			issues = append(issues, campaignIssue(
				"campaign.prerequisite.runtime_state_checksum_mismatch",
				field+".expectedRuntimeStateChecksum",
				"runtime-state expectation does not match the frozen upstream step",
			))
		}
	}
	if draft.RiskPolicy.MaximumConcurrency <= 0 ||
		draft.RiskPolicy.MaximumConcurrency > 1000 ||
		draft.RiskPolicy.FailureToleranceBasisPoints < 0 ||
		draft.RiskPolicy.FailureToleranceBasisPoints > 10000 ||
		draft.RiskPolicy.MinimumHealthyBasisPoints < 0 ||
		draft.RiskPolicy.MinimumHealthyBasisPoints > 10000 {
		issues = append(issues, campaignIssue(
			"campaign.risk_policy.invalid",
			"riskPolicy",
			"campaign risk and threshold policy is invalid",
		))
	}
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Field != issues[j].Field {
			return issues[i].Field < issues[j].Field
		}
		return issues[i].Code < issues[j].Code
	})
	return issues
}

func campaignChecksumsValid(checksums []string) bool {
	for _, checksum := range checksums {
		if !campaignChecksumPattern.MatchString(checksum) {
			return false
		}
	}
	return true
}

func validateCampaignWaves(
	waves []types.CampaignWaveDraft,
	issues *[]types.ValidationIssue,
) {
	if len(waves) == 0 {
		*issues = append(*issues, campaignIssue(
			"campaign.wave.required",
			"waves",
			"at least one campaign wave is required",
		))
		return
	}
	sorted := append([]types.CampaignWaveDraft(nil), waves...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Order < sorted[j].Order
	})
	previousBake := -1
	orders := make(map[int]struct{}, len(sorted))
	for index, wave := range sorted {
		field := fmt.Sprintf("waves[%d]", index)
		if wave.Order <= 0 {
			*issues = append(*issues, campaignIssue(
				"campaign.wave.order_invalid",
				field+".order",
				"wave order must be positive",
			))
		} else if _, duplicate := orders[wave.Order]; duplicate {
			*issues = append(*issues, campaignIssue(
				"campaign.wave.order_duplicate",
				field+".order",
				"wave order must be unique",
			))
		}
		orders[wave.Order] = struct{}{}
		if wave.BakeSeconds < 0 {
			*issues = append(*issues, campaignIssue(
				"campaign.wave.bake_invalid",
				field+".bakeSeconds",
				"wave bake duration cannot be negative",
			))
		} else if previousBake >= 0 && wave.BakeSeconds < previousBake {
			*issues = append(*issues, campaignIssue(
				"campaign.wave.bake_decreased",
				field+".bakeSeconds",
				"broader wave bake duration cannot decrease",
			))
		}
		if wave.BakeSeconds >= 0 {
			previousBake = wave.BakeSeconds
		}
		if wave.MaximumConcurrency <= 0 || wave.MaximumConcurrency > 1000 {
			*issues = append(*issues, campaignIssue(
				"campaign.wave.concurrency_invalid",
				field+".maximumConcurrency",
				"wave maximum concurrency must be between 1 and 1000",
			))
		}
	}
}

func containsUUID(values []uuid.UUID, value uuid.UUID) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func campaignIssue(code, field, message string) types.ValidationIssue {
	return types.ValidationIssue{Code: code, Field: field, Message: message}
}
