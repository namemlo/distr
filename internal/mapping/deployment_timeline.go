package mapping

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
)

func DeploymentTimelineToAPI(timeline types.DeploymentTimeline) api.DeploymentTimeline {
	return api.DeploymentTimeline{
		Items: List(timeline.Items, DeploymentTimelineItemToAPI),
	}
}

func DeploymentTimelineItemToAPI(item types.DeploymentTimelineItem) api.DeploymentTimelineItem {
	return api.DeploymentTimelineItem{
		TaskID:                 item.TaskID,
		DeploymentPlanID:       item.DeploymentPlanID,
		DeploymentPlanTargetID: item.DeploymentPlanTargetID,
		DeploymentTargetID:     item.DeploymentTargetID,
		ApplicationID:          item.ApplicationID,
		ApplicationName:        item.ApplicationName,
		ReleaseBundleID:        item.ReleaseBundleID,
		ReleaseNumber:          item.ReleaseNumber,
		ChannelID:              item.ChannelID,
		ChannelName:            item.ChannelName,
		EnvironmentID:          item.EnvironmentID,
		EnvironmentName:        item.EnvironmentName,
		CustomerOrganizationID: item.CustomerOrganizationID,
		DeploymentTargetName:   item.DeploymentTargetName,
		ActorUserAccountID:     item.ActorUserAccountID,
		Status:                 item.Status,
		QueuedAt:               item.QueuedAt,
		StartedAt:              item.StartedAt,
		CompletedAt:            item.CompletedAt,
		ProcessSnapshotID:      item.ProcessSnapshotID,
		VariableSnapshotID:     item.VariableSnapshotID,
		Components:             List(item.Components, DeploymentTimelineComponentToAPI),
		LastSuccessful:         item.LastSuccessful,
		RedeployAvailable:      item.RedeployAvailable,
	}
}

func DeploymentTimelineComponentToAPI(component types.DeploymentTimelineComponent) api.DeploymentTimelineComponent {
	return api.DeploymentTimelineComponent{
		Key:     component.Key,
		Name:    component.Name,
		Type:    component.Type,
		Version: component.Version,
	}
}

func DeploymentTimelineComparisonToAPI(
	comparison types.DeploymentTimelineComparison,
) api.DeploymentTimelineComparison {
	return api.DeploymentTimelineComparison{
		Base:       DeploymentTimelineItemToAPI(comparison.Base),
		Compare:    DeploymentTimelineItemToAPI(comparison.Compare),
		Process:    DeploymentTimelineProcessChangeToAPI(comparison.Process),
		Components: List(comparison.Components, DeploymentTimelineComponentChangeToAPI),
		Steps:      List(comparison.Steps, DeploymentTimelineStepChangeToAPI),
		Variables:  List(comparison.Variables, DeploymentTimelineVariableChangeToAPI),
	}
}

func DeploymentTimelineProcessChangeToAPI(
	change types.DeploymentTimelineProcessChange,
) api.DeploymentTimelineProcessChange {
	return api.DeploymentTimelineProcessChange{
		BaseProcessSnapshotID:    change.BaseProcessSnapshotID,
		CompareProcessSnapshotID: change.CompareProcessSnapshotID,
		BaseRevisionNumber:       change.BaseRevisionNumber,
		CompareRevisionNumber:    change.CompareRevisionNumber,
		BaseCanonicalChecksum:    change.BaseCanonicalChecksum,
		CompareCanonicalChecksum: change.CompareCanonicalChecksum,
		Changed:                  change.Changed,
	}
}

func DeploymentTimelineComponentChangeToAPI(
	change types.DeploymentTimelineComponentChange,
) api.DeploymentTimelineComponentChange {
	return api.DeploymentTimelineComponentChange{
		Key:            change.Key,
		Name:           change.Name,
		Kind:           change.Kind,
		BaseVersion:    change.BaseVersion,
		CompareVersion: change.CompareVersion,
		BaseType:       change.BaseType,
		CompareType:    change.CompareType,
	}
}

func DeploymentTimelineStepChangeToAPI(change types.DeploymentTimelineStepChange) api.DeploymentTimelineStepChange {
	return api.DeploymentTimelineStepChange{
		StepKey:           change.StepKey,
		Name:              change.Name,
		Kind:              change.Kind,
		BaseActionType:    change.BaseActionType,
		CompareActionType: change.CompareActionType,
		BaseIncluded:      change.BaseIncluded,
		CompareIncluded:   change.CompareIncluded,
	}
}

func DeploymentTimelineVariableChangeToAPI(
	change types.DeploymentTimelineVariableChange,
) api.DeploymentTimelineVariableChange {
	return api.DeploymentTimelineVariableChange{
		Key:              change.Key,
		Kind:             change.Kind,
		BaseStatus:       change.BaseStatus,
		CompareStatus:    change.CompareStatus,
		BaseSource:       change.BaseSource,
		CompareSource:    change.CompareSource,
		BaseRedacted:     change.BaseRedacted,
		CompareRedacted:  change.CompareRedacted,
		BaseValue:        change.BaseValue,
		CompareValue:     change.CompareValue,
		BaseReference:    change.BaseReference,
		CompareReference: change.CompareReference,
	}
}

func DeploymentTimelineRedeployToAPI(redeploy types.DeploymentTimelineRedeploy) api.DeploymentTimelineRedeploy {
	return api.DeploymentTimelineRedeploy{
		Plan:    DeploymentPlanToAPI(redeploy.Plan),
		Warning: redeploy.Warning,
	}
}
