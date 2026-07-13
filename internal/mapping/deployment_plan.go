package mapping

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
)

func DeploymentPlanToAPI(plan types.DeploymentPlan) api.DeploymentPlan {
	return api.DeploymentPlan{
		ID:                 plan.ID,
		CreatedAt:          plan.CreatedAt,
		ApplicationID:      plan.ApplicationID,
		ReleaseBundleID:    plan.ReleaseBundleID,
		ChannelID:          plan.ChannelID,
		EnvironmentID:      plan.EnvironmentID,
		ProcessSnapshotID:  plan.ProcessSnapshotID,
		VariableSnapshotID: plan.VariableSnapshotID,
		ReleaseContract:    plan.ReleaseContract,
		Status:             plan.Status,
		CanonicalChecksum:  plan.CanonicalChecksum,
		Targets:            List(plan.Targets, DeploymentPlanTargetToAPI),
		TargetComponents:   List(plan.TargetComponents, DeploymentPlanTargetComponentToAPI),
		Steps:              List(plan.Steps, DeploymentPlanStepToAPI),
		Variables:          List(plan.Variables, DeploymentPlanVariableToAPI),
		Issues:             List(plan.Issues, DeploymentPlanIssueToAPI),
	}
}

func DeploymentPlanTargetToAPI(target types.DeploymentPlanTarget) api.DeploymentPlanTarget {
	return api.DeploymentPlanTarget{
		ID:                     target.ID,
		DeploymentTargetID:     target.DeploymentTargetID,
		Name:                   target.Name,
		Type:                   target.Type,
		Platform:               target.Platform,
		CustomerOrganizationID: target.CustomerOrganizationID,
		SortOrder:              target.SortOrder,
	}
}

func DeploymentPlanTargetComponentToAPI(component types.DeploymentPlanTargetComponent) api.DeploymentPlanTargetComponent {
	return api.DeploymentPlanTargetComponent{
		ID:                      component.ID,
		DeploymentPlanTargetID:  component.DeploymentPlanTargetID,
		DeploymentTargetID:      component.DeploymentTargetID,
		Component:               component.Component,
		Version:                 component.Version,
		Image:                   component.Image,
		Platform:                component.Platform,
		Contracts:               component.Contracts,
		ConfigChecksum:          component.ConfigChecksum,
		ExpectedStateVersion:    component.ExpectedStateVersion,
		ExpectedStateChecksum:   component.ExpectedStateChecksum,
		ExpectedReleaseBundleID: component.ExpectedReleaseBundleID,
		SortOrder:               component.SortOrder,
	}
}

func DeploymentPlanStepToAPI(step types.DeploymentPlanStep) api.DeploymentPlanStep {
	return api.DeploymentPlanStep{
		ID:                   step.ID,
		StepKey:              step.StepKey,
		Name:                 step.Name,
		ActionType:           step.ActionType,
		ActionName:           step.ActionName,
		ExecutionLocation:    step.ExecutionLocation,
		InputBindings:        step.InputBindings,
		Condition:            step.Condition,
		TargetTags:           step.TargetTags,
		FailureMode:          step.FailureMode,
		TimeoutSeconds:       step.TimeoutSeconds,
		RetryMaxAttempts:     step.RetryMaxAttempts,
		RetryIntervalSeconds: step.RetryIntervalSeconds,
		RequiredPermissions:  step.RequiredPermissions,
		SortOrder:            step.SortOrder,
		Dependencies:         step.Dependencies,
		Included:             step.Included,
		ExcludedReason:       step.ExcludedReason,
	}
}

func DeploymentPlanVariableToAPI(variable types.DeploymentPlanVariable) api.DeploymentPlanVariable {
	return api.DeploymentPlanVariable{
		ID:            variable.ID,
		VariableSetID: variable.VariableSetID,
		VariableID:    variable.VariableID,
		Key:           variable.Key,
		Type:          variable.Type,
		IsRequired:    variable.IsRequired,
		Status:        variable.Status,
		Source:        variable.Source,
		Value:         variable.Value,
		ReferenceID:   variable.ReferenceID,
		ReferenceName: variable.ReferenceName,
		Redacted:      variable.Redacted,
		Trace:         variable.Trace,
	}
}

func DeploymentPlanIssueToAPI(issue types.DeploymentPlanIssue) api.DeploymentPlanIssue {
	return api.DeploymentPlanIssue{
		ID:        issue.ID,
		Severity:  issue.Severity,
		Code:      issue.Code,
		Field:     issue.Field,
		Message:   issue.Message,
		SortOrder: issue.SortOrder,
	}
}
