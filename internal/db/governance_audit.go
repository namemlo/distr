package db

import (
	"context"
	"encoding/json"

	"github.com/distr-sh/distr/internal/types"
)

func recordGovernanceAuditMutation(
	ctx context.Context,
	event types.ControlPlaneAuditEventInput,
) error {
	return RecordControlPlaneAuditMutation(ctx, controlPlaneDomainAuditHook(ctx), event)
}

func governanceAuditPayload(value any) json.RawMessage {
	payload, _ := json.Marshal(value)
	return payload
}

func deploymentPolicyVersionPublishedAuditEvent(
	version types.DeploymentPolicyVersion,
) types.ControlPlaneAuditEventInput {
	return types.ControlPlaneAuditEventInput{
		OrganizationID:            version.OrganizationID,
		EventType:                 "policy.version.published",
		ActorID:                   version.PublishedByUserAccountID,
		Outcome:                   "SUCCEEDED",
		DeploymentPolicyID:        &version.PolicyID,
		DeploymentPolicyVersionID: &version.ID,
		PolicyChecksum:            version.CanonicalChecksum,
		Payload: governanceAuditPayload(map[string]any{
			"versionNumber": version.VersionNumber,
			"state":         version.State,
		}),
	}
}

func deploymentPolicyBoundAuditEvent(
	request types.PolicyBindingRequest,
	version types.DeploymentPolicyVersion,
) types.ControlPlaneAuditEventInput {
	event := types.ControlPlaneAuditEventInput{
		OrganizationID:            request.OrganizationID,
		EventType:                 "policy.binding.created",
		ActorID:                   &request.CreatedByUserAccountID,
		Outcome:                   "SUCCEEDED",
		DeploymentPolicyID:        &version.PolicyID,
		DeploymentPolicyVersionID: &request.PolicyVersionID,
		PolicyChecksum:            version.CanonicalChecksum,
		Payload: governanceAuditPayload(map[string]any{
			"scopeKind": request.ScopeKind,
			"scopeId":   request.ScopeID,
			"role":      request.Role,
		}),
	}
	switch request.ScopeKind {
	case types.DeploymentPolicyBindingScopeCustomer:
		event.CustomerOrganizationID = &request.ScopeID
	case types.DeploymentPolicyBindingScopeEnvironment:
		event.EnvironmentID = &request.ScopeID
	case types.DeploymentPolicyBindingScopeDeploymentUnit:
		event.DeploymentUnitID = &request.ScopeID
	case types.DeploymentPolicyBindingScopeComponent:
		event.ComponentID = &request.ScopeID
	case types.DeploymentPolicyBindingScopeCampaign:
		event.CampaignDraftID = &request.ScopeID
	}
	return event
}

func approvalRequestedAuditEvent(request types.ApprovalRequest) types.ControlPlaneAuditEventInput {
	return types.ControlPlaneAuditEventInput{
		OrganizationID:         request.OrganizationID,
		EventType:              "approval.requested",
		ActorID:                &request.RequesterUserAccountID,
		Outcome:                "PENDING",
		DeploymentPlanID:       &request.SubjectID,
		ApprovalID:             &request.ID,
		DeploymentPlanChecksum: request.SubjectChecksum,
		PolicyChecksum:         request.EffectivePolicyChecksum,
		ApprovalChecksum:       approvalEvidenceChecksum(request),
		Payload: governanceAuditPayload(map[string]any{
			"revision":              request.Revision,
			"subjectRevision":       request.SubjectRevision,
			"subscriberSetChecksum": request.SubscriberSetChecksum,
			"expiresAt":             request.ExpiresAt,
		}),
	}
}

func approvalDecisionRecordedAuditEvent(
	request types.ApprovalRequest,
	decision types.ApprovalDecision,
) types.ControlPlaneAuditEventInput {
	return types.ControlPlaneAuditEventInput{
		OrganizationID:         request.OrganizationID,
		EventType:              "approval.decided",
		ActorID:                &decision.ActorUserAccountID,
		Outcome:                string(decision.Decision),
		DeploymentPlanID:       &request.SubjectID,
		ApprovalID:             &request.ID,
		DeploymentPlanChecksum: request.SubjectChecksum,
		PolicyChecksum:         request.EffectivePolicyChecksum,
		ApprovalChecksum:       approvalEvidenceChecksum(request),
		Payload: governanceAuditPayload(map[string]any{
			"decisionId":           decision.ID,
			"requirementId":        decision.ApprovalRequirementID,
			"requestRevision":      decision.RequestRevision,
			"approvalRequestState": request.State,
		}),
	}
}

func approvalInvalidatedAuditEvent(
	request types.ApprovalRequest,
	reason types.ApprovalInvalidationReason,
) types.ControlPlaneAuditEventInput {
	return types.ControlPlaneAuditEventInput{
		OrganizationID:         request.OrganizationID,
		EventType:              "approval.invalidated",
		Outcome:                "INVALIDATED",
		DeploymentPlanID:       &request.SubjectID,
		ApprovalID:             &request.ID,
		DeploymentPlanChecksum: request.SubjectChecksum,
		PolicyChecksum:         request.EffectivePolicyChecksum,
		ApprovalChecksum:       approvalEvidenceChecksum(request),
		Payload: governanceAuditPayload(map[string]any{
			"revision": request.Revision,
			"reason":   reason,
		}),
	}
}

func maintenanceCalendarPublishedAuditEvent(
	version types.MaintenanceCalendarVersion,
) types.ControlPlaneAuditEventInput {
	return types.ControlPlaneAuditEventInput{
		OrganizationID:        version.OrganizationID,
		EventType:             "maintenance_calendar.version.published",
		ActorID:               &version.PublishedBy,
		Outcome:               "SUCCEEDED",
		MaintenanceCalendarID: &version.CalendarID,
		CalendarChecksum:      version.Checksum,
		Payload: governanceAuditPayload(map[string]any{
			"versionId":     version.ID,
			"versionNumber": version.VersionNumber,
			"draftRevision": version.SourceDraftRevision,
		}),
	}
}

func deploymentFreezePublishedAuditEvent(
	revision types.DeploymentFreezeRevision,
) types.ControlPlaneAuditEventInput {
	return types.ControlPlaneAuditEventInput{
		OrganizationID:     revision.OrganizationID,
		EventType:          "deployment_freeze.revision.published",
		ActorID:            &revision.PublishedBy,
		Outcome:            "SUCCEEDED",
		DeploymentFreezeID: &revision.FreezeID,
		CalendarChecksum:   revision.Checksum,
		Payload: governanceAuditPayload(map[string]any{
			"revisionId":    revision.ID,
			"versionNumber": revision.VersionNumber,
			"draftRevision": revision.SourceDraftRevision,
			"scopeKind":     revision.ScopeKind,
			"scopeId":       revision.ScopeID,
		}),
	}
}

func admissionEvaluationRecordedAuditEvent(
	evaluation types.AdmissionEvaluation,
) types.ControlPlaneAuditEventInput {
	return types.ControlPlaneAuditEventInput{
		OrganizationID:           evaluation.OrganizationID,
		EventType:                "admission.evaluated",
		ActorID:                  &evaluation.ActorUserAccountID,
		Outcome:                  string(evaluation.Decision),
		DeploymentPlanID:         &evaluation.DeploymentPlanID,
		AdmissionDecisionID:      &evaluation.ID,
		EmergencyOverrideID:      evaluation.EmergencyOverrideID,
		CampaignRevisionID:       evaluation.CampaignID,
		DeploymentPlanChecksum:   evaluation.PlanChecksum,
		PolicyChecksum:           evaluation.EffectivePolicyChecksum,
		AdmissionChecksum:        evaluation.DecisionChecksum,
		CampaignRevisionChecksum: evaluation.CampaignChecksum,
		Payload: governanceAuditPayload(map[string]any{
			"planRevision":             evaluation.PlanRevision,
			"materialChecksum":         evaluation.MaterialChecksum,
			"reasonCodes":              evaluation.ReasonCodes,
			"campaignRevisionId":       evaluation.CampaignID,
			"campaignRevision":         evaluation.CampaignRevision,
			"campaignRevisionChecksum": evaluation.CampaignChecksum,
			"schedulerIdempotencyKey":  evaluation.SchedulerIdempotencyKey,
		}),
	}
}

func emergencyOverrideCreatedAuditEvent(
	override types.EmergencyOverride,
) types.ControlPlaneAuditEventInput {
	return types.ControlPlaneAuditEventInput{
		OrganizationID:         override.OrganizationID,
		EventType:              "emergency_override.created",
		ActorID:                &override.ActorUserAccountID,
		Outcome:                "SUCCEEDED",
		DeploymentPlanID:       &override.DeploymentPlanID,
		EmergencyOverrideID:    &override.ID,
		DeploymentPlanChecksum: override.PlanChecksum,
		PolicyChecksum:         override.EffectivePolicyChecksum,
		AdmissionChecksum:      override.Checksum,
		Payload: governanceAuditPayload(map[string]any{
			"accelerations": override.Accelerations,
			"expiresAt":     override.ExpiresAt,
		}),
	}
}
