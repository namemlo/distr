package campaigns

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

func ResolveCampaignMembership(
	ctx context.Context,
	draft types.CampaignDraft,
) ([]types.CampaignMember, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	selectedIDs := make(map[uuid.UUID]struct{}, len(draft.Membership.PlanIDs))
	for _, planID := range draft.Membership.PlanIDs {
		if planID == uuid.Nil {
			return nil, fmt.Errorf("selected deployment plan ID is required")
		}
		selectedIDs[planID] = struct{}{}
	}
	terms, err := parseTagQuery(draft.Membership.TagQuery)
	if err != nil {
		return nil, err
	}

	candidatesByID := make(map[uuid.UUID]types.CampaignPlanCandidate, len(draft.CandidatePlans))
	selected := make([]types.CampaignPlanCandidate, 0, len(draft.CandidatePlans))
	for _, candidate := range draft.CandidatePlans {
		if candidate.PlanID == uuid.Nil || candidate.OrganizationID != draft.OrganizationID {
			continue
		}
		candidatesByID[candidate.PlanID] = candidate
		_, explicitlySelected := selectedIDs[candidate.PlanID]
		if explicitlySelected || matchesAllTags(candidate.Tags, terms) {
			selected = append(selected, candidate)
		}
	}
	for planID := range selectedIDs {
		if _, exists := candidatesByID[planID]; !exists {
			return nil, fmt.Errorf("selected deployment plan %s was not resolved", planID)
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("campaign membership resolved no deployment plans")
	}

	waveByPlan := make(map[uuid.UUID]int)
	for _, wave := range draft.Waves {
		for _, planID := range wave.PlanIDs {
			if _, exists := waveByPlan[planID]; exists {
				return nil, fmt.Errorf("deployment plan %s is assigned to multiple waves", planID)
			}
			waveByPlan[planID] = wave.Order
		}
	}
	sort.Slice(selected, func(i, j int) bool {
		leftWave, rightWave := waveByPlan[selected[i].PlanID], waveByPlan[selected[j].PlanID]
		if leftWave != rightWave {
			return leftWave < rightWave
		}
		if selected[i].DeploymentUnitID != selected[j].DeploymentUnitID {
			return selected[i].DeploymentUnitID.String() < selected[j].DeploymentUnitID.String()
		}
		return selected[i].PlanID.String() < selected[j].PlanID.String()
	})

	members := make([]types.CampaignMember, 0, len(selected))
	memberOrderByWave := make(map[int]int)
	for _, candidate := range selected {
		waveOrder, exists := waveByPlan[candidate.PlanID]
		if !exists || waveOrder <= 0 {
			return nil, fmt.Errorf("deployment plan %s has no campaign wave", candidate.PlanID)
		}
		memberOrderByWave[waveOrder]++
		members = append(members, types.CampaignMember{
			PlanID:                  candidate.PlanID,
			DeploymentUnitID:        candidate.DeploymentUnitID,
			PlanChecksum:            candidate.PlanChecksum,
			EffectivePolicyChecksum: candidate.EffectivePolicyChecksum,
			ApprovalRequestID:       candidate.ApprovalRequestID,
			ApprovalRequestRevision: candidate.ApprovalRequestRevision,
			ApprovalChecksum:        candidate.ApprovalChecksum,
			CalendarVersionIDs: append(
				[]uuid.UUID(nil),
				candidate.CalendarVersionIDs...,
			),
			CalendarChecksums: append(
				[]string(nil),
				candidate.CalendarChecksums...,
			),
			AdmissionEvaluationID: candidate.AdmissionEvaluationID,
			AdmissionChecksum:     candidate.AdmissionChecksum,
			WaveOrder:             waveOrder,
			MemberOrder:           memberOrderByWave[waveOrder],
		})
	}
	return members, nil
}

func parseTagQuery(query string) ([]string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	rawTerms := strings.FieldsFunc(query, func(r rune) bool {
		return r == '&' || r == ','
	})
	terms := make([]string, 0, len(rawTerms))
	for _, raw := range rawTerms {
		term := strings.TrimSpace(raw)
		parts := strings.Split(term, "=")
		if len(parts) != 2 ||
			strings.TrimSpace(parts[0]) == "" ||
			strings.TrimSpace(parts[1]) == "" {
			return nil, fmt.Errorf("campaign tag query term %q is invalid", term)
		}
		terms = append(
			terms,
			strings.TrimSpace(parts[0])+"="+strings.TrimSpace(parts[1]),
		)
	}
	sort.Strings(terms)
	return terms, nil
}

func ParseTagQuery(query string) ([]string, error) {
	return parseTagQuery(query)
}

func matchesAllTags(tags, terms []string) bool {
	if len(terms) == 0 {
		return false
	}
	set := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		set[strings.TrimSpace(tag)] = struct{}{}
	}
	for _, term := range terms {
		if _, exists := set[term]; !exists {
			return false
		}
	}
	return true
}
