package planning

import (
	"fmt"
	"slices"
	"strings"

	"github.com/distr-sh/distr/internal/types"
)

func ClassifyDeploymentRisk(
	changes []types.DeploymentPlanChangeEntry,
	policy types.EffectivePolicy,
) []types.DeploymentPlanRiskEntry {
	orderedChanges := slices.Clone(changes)
	slices.SortStableFunc(orderedChanges, func(a, b types.DeploymentPlanChangeEntry) int {
		if cmp := strings.Compare(a.ComponentKey, b.ComponentKey); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(string(a.Kind), string(b.Kind)); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.ComponentInstanceID.String(), b.ComponentInstanceID.String())
	})
	risks := make([]types.DeploymentPlanRiskEntry, 0, len(changes))
	add := func(
		change types.DeploymentPlanChangeEntry,
		code string,
		level types.DeploymentPlanRiskLevel,
		blocking bool,
		message string,
	) {
		risk := types.DeploymentPlanRiskEntry{
			ComponentKey: change.ComponentKey,
			Code:         code,
			Level:        level,
			Blocking:     blocking,
			Message:      boundedText(message, 2048),
			SortOrder:    len(risks),
		}
		checksum, err := canonicalChecksum(struct {
			ComponentKey string                        `json:"componentKey"`
			Code         string                        `json:"code"`
			Level        types.DeploymentPlanRiskLevel `json:"level"`
			Blocking     bool                          `json:"blocking"`
			Message      string                        `json:"message"`
			SortOrder    int                           `json:"sortOrder"`
		}{
			ComponentKey: risk.ComponentKey, Code: code, Level: level,
			Blocking: blocking, Message: risk.Message, SortOrder: risk.SortOrder,
		})
		if err == nil {
			risk.CanonicalChecksum = checksum
		}
		risks = append(risks, risk)
	}

	for _, change := range orderedChanges {
		switch {
		case change.Kind == types.DeploymentPlanChangeSchema && change.ForwardOnly:
			add(
				change,
				"forward_only_migration",
				types.DeploymentPlanRiskCritical,
				!policy.AllowForwardOnlyMigration,
				"schema transition is forward-only and cannot use automatic previous-state execution",
			)
		case change.Kind == types.DeploymentPlanChangeBaselineAuthority &&
			policy.RequireAuthoritativeV2Baseline:
			add(
				change,
				"baseline_not_v2_authoritative",
				types.DeploymentPlanRiskHigh,
				true,
				"legacy observation is visible for comparison but cannot authorize protocol v2 execution",
			)
		case change.Kind == types.DeploymentPlanChangeBootstrap &&
			policy.RequireBootstrapApproval:
			add(
				change,
				"bootstrap_approval_required",
				types.DeploymentPlanRiskHigh,
				true,
				"component has no verified healthy baseline and requires bootstrap approval",
			)
		case change.Kind == types.DeploymentPlanChangeProvider:
			add(
				change,
				"provider_binding_change",
				types.DeploymentPlanRiskHigh,
				false,
				"capability provider binding changes for this component",
			)
		case change.Kind == types.DeploymentPlanChangeTopology:
			add(
				change,
				"topology_change",
				types.DeploymentPlanRiskHigh,
				false,
				"deployment topology or subscriber blast radius changes",
			)
		case change.Kind == types.DeploymentPlanChangeLimitExceeded:
			add(
				change,
				"planning_limit_exceeded",
				types.DeploymentPlanRiskCritical,
				true,
				fmt.Sprintf("bounded planning input exceeded: %s", change.Before),
			)
		}
	}
	return risks
}
