package campaigns

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/distr-sh/distr/internal/types"
)

func CanonicalizeCampaignRevision(
	revision types.CampaignRevision,
) ([]byte, string, error) {
	waves := append([]types.CampaignWave(nil), revision.Waves...)
	sort.Slice(waves, func(i, j int) bool {
		if waves[i].Order != waves[j].Order {
			return waves[i].Order < waves[j].Order
		}
		return waves[i].Name < waves[j].Name
	})
	members := append([]types.CampaignMember(nil), revision.Members...)
	sort.Slice(members, func(i, j int) bool {
		if members[i].WaveOrder != members[j].WaveOrder {
			return members[i].WaveOrder < members[j].WaveOrder
		}
		if members[i].MemberOrder != members[j].MemberOrder {
			return members[i].MemberOrder < members[j].MemberOrder
		}
		return members[i].PlanID.String() < members[j].PlanID.String()
	})
	prerequisites := append(
		[]types.CampaignPrerequisite(nil),
		revision.Prerequisites...,
	)
	sort.Slice(prerequisites, func(i, j int) bool {
		if prerequisites[i].DownstreamPlanID != prerequisites[j].DownstreamPlanID {
			return prerequisites[i].DownstreamPlanID.String() <
				prerequisites[j].DownstreamPlanID.String()
		}
		if prerequisites[i].UpstreamPlanID != prerequisites[j].UpstreamPlanID {
			return prerequisites[i].UpstreamPlanID.String() <
				prerequisites[j].UpstreamPlanID.String()
		}
		if prerequisites[i].UpstreamStepKey != prerequisites[j].UpstreamStepKey {
			return prerequisites[i].UpstreamStepKey < prerequisites[j].UpstreamStepKey
		}
		return prerequisites[i].ProviderPlacementID.String() <
			prerequisites[j].ProviderPlacementID.String()
	})

	type canonicalWave struct {
		Order              int    `json:"order"`
		Name               string `json:"name"`
		BakeSeconds        int    `json:"bakeSeconds"`
		MaximumConcurrency int    `json:"maximumConcurrency"`
	}
	type canonicalMember struct {
		PlanID                  string   `json:"planId"`
		DeploymentUnitID        string   `json:"deploymentUnitId"`
		PlanChecksum            string   `json:"planChecksum"`
		EffectivePolicyChecksum string   `json:"effectivePolicyChecksum"`
		ApprovalRequestID       string   `json:"approvalRequestId"`
		ApprovalRequestRevision int64    `json:"approvalRequestRevision"`
		ApprovalChecksum        string   `json:"approvalChecksum"`
		CalendarVersionIDs      []string `json:"calendarVersionIds"`
		CalendarChecksums       []string `json:"calendarChecksums"`
		AdmissionEvaluationID   string   `json:"admissionEvaluationId"`
		AdmissionChecksum       string   `json:"admissionChecksum"`
		WaveOrder               int      `json:"waveOrder"`
		MemberOrder             int      `json:"memberOrder"`
	}
	type canonicalPrerequisite struct {
		DownstreamPlanID              string `json:"downstreamPlanId"`
		UpstreamPlanID                string `json:"upstreamPlanId"`
		UpstreamStepKey               string `json:"upstreamStepKey"`
		ProviderPlacementID           string `json:"providerPlacementId"`
		ProviderDeploymentUnitID      string `json:"providerDeploymentUnitId"`
		ProviderComponentInstanceID   string `json:"providerComponentInstanceId"`
		ExpectedObservedStateChecksum string `json:"expectedObservedStateChecksum"`
	}
	document := struct {
		Schema              string                   `json:"schema"`
		OrganizationID      string                   `json:"organizationId"`
		CampaignDraftID     string                   `json:"campaignDraftId"`
		RevisionNumber      int64                    `json:"revisionNumber"`
		SourceDraftRevision int64                    `json:"sourceDraftRevision"`
		Name                string                   `json:"name"`
		Description         string                   `json:"description"`
		MembershipTagQuery  string                   `json:"membershipTagQuery,omitempty"`
		RiskPolicy          types.CampaignRiskPolicy `json:"riskPolicy"`
		Waves               []canonicalWave          `json:"waves"`
		Members             []canonicalMember        `json:"members"`
		Prerequisites       []canonicalPrerequisite  `json:"prerequisites"`
	}{
		Schema:              types.CampaignRevisionSchemaV1,
		OrganizationID:      revision.OrganizationID.String(),
		CampaignDraftID:     revision.CampaignDraftID.String(),
		RevisionNumber:      revision.RevisionNumber,
		SourceDraftRevision: revision.SourceDraftRevision,
		Name:                revision.Name,
		Description:         revision.Description,
		MembershipTagQuery:  revision.MembershipTagQuery,
		RiskPolicy:          revision.RiskPolicy,
		Waves:               make([]canonicalWave, 0, len(waves)),
		Members:             make([]canonicalMember, 0, len(members)),
		Prerequisites:       make([]canonicalPrerequisite, 0, len(prerequisites)),
	}
	for _, wave := range waves {
		document.Waves = append(document.Waves, canonicalWave{
			Order:              wave.Order,
			Name:               wave.Name,
			BakeSeconds:        wave.BakeSeconds,
			MaximumConcurrency: wave.MaximumConcurrency,
		})
	}
	for _, member := range members {
		calendarVersionIDs := make([]string, len(member.CalendarVersionIDs))
		for index, versionID := range member.CalendarVersionIDs {
			calendarVersionIDs[index] = versionID.String()
		}
		document.Members = append(document.Members, canonicalMember{
			PlanID:                  member.PlanID.String(),
			DeploymentUnitID:        member.DeploymentUnitID.String(),
			PlanChecksum:            member.PlanChecksum,
			EffectivePolicyChecksum: member.EffectivePolicyChecksum,
			ApprovalRequestID:       member.ApprovalRequestID.String(),
			ApprovalRequestRevision: member.ApprovalRequestRevision,
			ApprovalChecksum:        member.ApprovalChecksum,
			CalendarVersionIDs:      calendarVersionIDs,
			CalendarChecksums: append(
				[]string(nil),
				member.CalendarChecksums...,
			),
			AdmissionEvaluationID: member.AdmissionEvaluationID.String(),
			AdmissionChecksum:     member.AdmissionChecksum,
			WaveOrder:             member.WaveOrder,
			MemberOrder:           member.MemberOrder,
		})
	}
	for _, prerequisite := range prerequisites {
		document.Prerequisites = append(
			document.Prerequisites,
			canonicalPrerequisite{
				DownstreamPlanID:    prerequisite.DownstreamPlanID.String(),
				UpstreamPlanID:      prerequisite.UpstreamPlanID.String(),
				UpstreamStepKey:     prerequisite.UpstreamStepKey,
				ProviderPlacementID: prerequisite.ProviderPlacementID.String(),
				ProviderDeploymentUnitID: prerequisite.
					ProviderDeploymentUnitID.String(),
				ProviderComponentInstanceID: prerequisite.
					ProviderComponentInstanceID.String(),
				ExpectedObservedStateChecksum: prerequisite.
					ExpectedObservedStateChecksum,
			},
		)
	}

	payload, err := json.Marshal(document)
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(payload)
	return payload, "sha256:" + hex.EncodeToString(digest[:]), nil
}
