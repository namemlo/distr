package db

import (
	"context"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

type controlPlaneDomainAuditHookContextKey struct{}

// WithControlPlaneDomainAuditHook injects an outbox or test adapter for the
// request's control-plane domain mutation boundaries. Production callers
// default to the direct append hook bound to the owning transaction.
func WithControlPlaneDomainAuditHook(
	ctx context.Context,
	hook ControlPlaneAuditAppendHook,
) context.Context {
	return context.WithValue(ctx, controlPlaneDomainAuditHookContextKey{}, hook)
}

func controlPlaneDomainAuditHook(ctx context.Context) ControlPlaneAuditAppendHook {
	if hook, ok := ctx.Value(controlPlaneDomainAuditHookContextKey{}).(ControlPlaneAuditAppendHook); ok && hook != nil {
		return hook
	}
	return DirectControlPlaneAuditAppendHook()
}

func releaseControlPlaneAuditInput(
	bundle types.ReleaseBundle,
	eventType string,
	actorID *uuid.UUID,
	outcome string,
) types.ControlPlaneAuditEventInput {
	input := types.ControlPlaneAuditEventInput{
		OrganizationID:  bundle.OrganizationID,
		EventType:       eventType,
		ActorID:         actorID,
		Outcome:         outcome,
		ReleaseID:       &bundle.ID,
		ReleaseChecksum: bundle.CanonicalChecksum,
	}
	switch bundle.Kind {
	case types.ReleaseBundleKindComponent:
		input.ComponentReleaseID = &bundle.ID
		input.ComponentReleaseChecksum = bundle.CanonicalChecksum
	case types.ReleaseBundleKindProduct:
		input.ProductReleaseID = &bundle.ID
		input.ProductReleaseChecksum = bundle.CanonicalChecksum
	}
	return input
}

func recordReleaseControlPlaneAuditMutation(
	ctx context.Context,
	input types.ControlPlaneAuditEventInput,
) error {
	return RecordControlPlaneAuditMutation(
		ctx,
		controlPlaneDomainAuditHook(ctx),
		input,
	)
}

func releaseControlPlaneEventType(bundle types.ReleaseBundle, action string) string {
	switch bundle.Kind {
	case types.ReleaseBundleKindComponent:
		return "component_release." + action
	case types.ReleaseBundleKindProduct:
		return "product_release." + action
	default:
		return "release." + action
	}
}

func productReleaseComponentAuditInput(
	bundle types.ReleaseBundle,
	component types.ProductReleaseComponent,
	eventType string,
) types.ControlPlaneAuditEventInput {
	input := releaseControlPlaneAuditInput(bundle, eventType, nil, "SUCCEEDED")
	input.ComponentReleaseID = &component.ComponentReleaseID
	input.ComponentReleaseChecksum = component.ComponentReleaseChecksum
	return input
}

func targetConfigControlPlaneAuditInput(
	snapshot types.TargetConfigSnapshot,
	eventType string,
) types.ControlPlaneAuditEventInput {
	return types.ControlPlaneAuditEventInput{
		OrganizationID:       snapshot.OrganizationID,
		EventType:            eventType,
		ActorID:              &snapshot.CreatedByUserAccountID,
		Outcome:              "SUCCEEDED",
		TargetConfigID:       &snapshot.ID,
		EnvironmentID:        &snapshot.EnvironmentID,
		DeploymentUnitID:     &snapshot.DeploymentUnitID,
		TargetConfigChecksum: snapshot.CanonicalChecksum,
	}
}

func recordTargetConfigControlPlaneAuditMutation(
	ctx context.Context,
	input types.ControlPlaneAuditEventInput,
) error {
	return RecordControlPlaneAuditMutation(
		ctx,
		controlPlaneDomainAuditHook(ctx),
		input,
	)
}

func deploymentPlanControlPlaneAuditInput(
	plan types.DeploymentPlan,
	eventType string,
	actorID *uuid.UUID,
) types.ControlPlaneAuditEventInput {
	input := types.ControlPlaneAuditEventInput{
		OrganizationID:         plan.OrganizationID,
		EventType:              eventType,
		ActorID:                actorID,
		Outcome:                "SUCCEEDED",
		DeploymentPlanID:       &plan.ID,
		EnvironmentID:          &plan.EnvironmentID,
		DeploymentPlanChecksum: plan.CanonicalChecksum,
	}
	if plan.ReleaseContract != nil && plan.ReleaseContract.ProductV1 != nil {
		input.ProductReleaseID = &plan.ReleaseBundleID
	} else {
		input.ReleaseID = &plan.ReleaseBundleID
	}
	if plan.TargetConfigSnapshotID != nil {
		input.TargetConfigID = plan.TargetConfigSnapshotID
	}
	if plan.DeploymentUnitID != nil {
		input.DeploymentUnitID = plan.DeploymentUnitID
	}
	return input
}

func deploymentPlanValidationAuditInput(
	validation types.PlanDraftValidation,
	outcome string,
) types.ControlPlaneAuditEventInput {
	draft := validation.Draft
	input := types.ControlPlaneAuditEventInput{
		OrganizationID:         draft.OrganizationID,
		EventType:              "plan.validated",
		ActorID:                &draft.UpdatedByUserAccountID,
		Outcome:                outcome,
		ProductReleaseID:       &draft.ProductReleaseID,
		TargetConfigID:         &draft.TargetConfigSnapshotID,
		DeploymentUnitID:       &draft.DeploymentUnitID,
		DeploymentPlanChecksum: validation.PreviewChecksum,
	}
	if draft.ResolutionInput != nil {
		input.ProductReleaseChecksum = draft.ResolutionInput.ProductChecksum
		input.TargetConfigChecksum = draft.ResolutionInput.Config.CanonicalChecksum
		input.EnvironmentID = &draft.ResolutionInput.Assignment.EnvironmentID
	}
	return input
}

func deploymentPlanDraftAuditInput(
	draft types.PlanDraft,
	eventType string,
) types.ControlPlaneAuditEventInput {
	return types.ControlPlaneAuditEventInput{
		OrganizationID:   draft.OrganizationID,
		EventType:        eventType,
		ActorID:          &draft.UpdatedByUserAccountID,
		Outcome:          "SUCCEEDED",
		ProductReleaseID: &draft.ProductReleaseID,
		TargetConfigID:   &draft.TargetConfigSnapshotID,
		DeploymentUnitID: &draft.DeploymentUnitID,
	}
}

func recordDeploymentPlanControlPlaneAuditMutation(
	ctx context.Context,
	input types.ControlPlaneAuditEventInput,
) error {
	return RecordControlPlaneAuditMutation(
		ctx,
		controlPlaneDomainAuditHook(ctx),
		input,
	)
}
