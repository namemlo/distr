package db

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

type recordingGovernanceAuditHook struct {
	events []types.ControlPlaneAuditEventInput
	err    error
}

func (hook *recordingGovernanceAuditHook) AppendControlPlaneAuditEvent(
	_ context.Context,
	event types.ControlPlaneAuditEventInput,
) error {
	hook.events = append(hook.events, event)
	return hook.err
}

func TestGovernanceAuditEventsCarryImmutableCorrelations(t *testing.T) {
	g := NewWithT(t)
	checksum := "sha256:" + strings.Repeat("a", 64)
	organizationID := uuid.New()
	actorID := uuid.New()
	planID := uuid.New()

	policy := types.DeploymentPolicyVersion{
		ID: uuid.New(), OrganizationID: organizationID, PolicyID: uuid.New(),
		CanonicalChecksum: checksum, VersionNumber: 3, PublishedByUserAccountID: &actorID,
	}
	approval := types.ApprovalRequest{
		ID: uuid.New(), OrganizationID: organizationID, SubjectID: planID,
		SubjectChecksum: checksum, EffectivePolicyChecksum: checksum,
		SubscriberSetChecksum: checksum, RequesterUserAccountID: actorID, Revision: 2,
		State: types.ApprovalRequestStatePending,
	}
	decision := types.ApprovalDecision{
		ID: uuid.New(), OrganizationID: organizationID,
		ApprovalRequestID: approval.ID, ApprovalRequirementID: uuid.New(),
		ActorUserAccountID: actorID, Decision: types.ApprovalDecisionApprove,
		RequestRevision: approval.Revision,
	}
	calendar := types.MaintenanceCalendarVersion{
		ID: uuid.New(), CalendarID: uuid.New(), OrganizationID: organizationID,
		Checksum: checksum, PublishedBy: actorID, VersionNumber: 4,
	}
	freeze := types.DeploymentFreezeRevision{
		ID: uuid.New(), FreezeID: uuid.New(), OrganizationID: organizationID,
		Checksum: checksum, PublishedBy: actorID, VersionNumber: 5,
	}
	evaluation := types.AdmissionEvaluation{
		ID: uuid.New(), OrganizationID: organizationID, DeploymentPlanID: planID,
		PlanChecksum: checksum, EffectivePolicyChecksum: checksum,
		DecisionChecksum: checksum, Decision: types.AdmissionDecisionAdmit,
		ActorUserAccountID: actorID,
	}
	override := types.EmergencyOverride{
		ID: uuid.New(), OrganizationID: organizationID, DeploymentPlanID: planID,
		PlanChecksum: checksum, EffectivePolicyChecksum: checksum, Checksum: checksum,
		ActorUserAccountID: actorID,
	}

	events := []types.ControlPlaneAuditEventInput{
		deploymentPolicyVersionPublishedAuditEvent(policy),
		deploymentPolicyBoundAuditEvent(types.PolicyBindingRequest{
			OrganizationID: organizationID, PolicyVersionID: policy.ID,
			ScopeKind: types.DeploymentPolicyBindingScopeEnvironment,
			ScopeID:   uuid.New(), Role: types.DeploymentPolicyBindingRoleOwner,
			CreatedByUserAccountID: actorID,
		}, policy),
		approvalRequestedAuditEvent(approval),
		approvalDecisionRecordedAuditEvent(approval, decision),
		approvalInvalidatedAuditEvent(approval, types.ApprovalInvalidationPlanChanged),
		maintenanceCalendarPublishedAuditEvent(calendar),
		deploymentFreezePublishedAuditEvent(freeze),
		admissionEvaluationRecordedAuditEvent(evaluation),
		emergencyOverrideCreatedAuditEvent(override),
	}

	for index, event := range events {
		g.Expect(validateControlPlaneAuditEventInput(event)).To(Succeed(), event.EventType)
		g.Expect(event.OrganizationID).To(Equal(organizationID), event.EventType)
		if index != 4 {
			g.Expect(event.ActorID).NotTo(BeNil(), event.EventType)
		}
		g.Expect(len(event.Payload)).To(BeNumerically(">", 0), event.EventType)
	}
	g.Expect(events[0].DeploymentPolicyID).NotTo(BeNil())
	g.Expect(*events[0].DeploymentPolicyID).To(Equal(policy.PolicyID))
	g.Expect(events[0].DeploymentPolicyVersionID).NotTo(BeNil())
	g.Expect(*events[0].DeploymentPolicyVersionID).To(Equal(policy.ID))
	g.Expect(events[0].PolicyChecksum).To(Equal(checksum))
	g.Expect(events[2].ApprovalID).NotTo(BeNil())
	g.Expect(*events[2].ApprovalID).To(Equal(approval.ID))
	g.Expect(events[2].DeploymentPlanID).NotTo(BeNil())
	g.Expect(*events[2].DeploymentPlanID).To(Equal(planID))
	g.Expect(events[5].MaintenanceCalendarID).NotTo(BeNil())
	g.Expect(*events[5].MaintenanceCalendarID).To(Equal(calendar.CalendarID))
	g.Expect(events[6].DeploymentFreezeID).NotTo(BeNil())
	g.Expect(*events[6].DeploymentFreezeID).To(Equal(freeze.FreezeID))
	g.Expect(events[7].AdmissionDecisionID).NotTo(BeNil())
	g.Expect(*events[7].AdmissionDecisionID).To(Equal(evaluation.ID))
	g.Expect(events[7].AdmissionChecksum).To(Equal(checksum))
	g.Expect(events[8].EmergencyOverrideID).NotTo(BeNil())
	g.Expect(*events[8].EmergencyOverrideID).To(Equal(override.ID))
}

func TestAdmissionAuditPayloadUsesCampaignRevisionKeys(t *testing.T) {
	g := NewWithT(t)
	campaignRevisionID := uuid.New()
	event := admissionEvaluationRecordedAuditEvent(types.AdmissionEvaluation{
		ID: uuid.New(), OrganizationID: uuid.New(), DeploymentPlanID: uuid.New(),
		ActorUserAccountID: uuid.New(), CampaignID: &campaignRevisionID,
		CampaignChecksum: "sha256:" + strings.Repeat("c", 64),
	})

	var payload map[string]any
	g.Expect(json.Unmarshal(event.Payload, &payload)).To(Succeed())
	g.Expect(payload).To(HaveKeyWithValue("campaignRevisionId", campaignRevisionID.String()))
	g.Expect(payload).To(HaveKeyWithValue(
		"campaignRevisionChecksum",
		"sha256:"+strings.Repeat("c", 64),
	))
	g.Expect(payload).NotTo(HaveKey("campaignId"))
	g.Expect(payload).NotTo(HaveKey("campaignChecksum"))
}

func TestGovernanceAuditHookFailureIsReturned(t *testing.T) {
	g := NewWithT(t)
	hook := &recordingGovernanceAuditHook{err: errors.New("audit unavailable")}
	ctx := WithControlPlaneDomainAuditHook(context.Background(), hook)
	event := types.ControlPlaneAuditEventInput{
		OrganizationID: uuid.New(), EventType: "policy.version.published",
		Outcome: "SUCCEEDED", DeploymentPolicyVersionID: new(uuid.UUID),
	}
	*event.DeploymentPolicyVersionID = uuid.New()
	committed := false
	runner := func(ctx context.Context, operation func(context.Context) error) error {
		err := operation(ctx)
		committed = err == nil
		return err
	}

	err := runControlPlaneAuditedMutation(
		ctx,
		runner,
		controlPlaneDomainAuditHook(ctx),
		func(context.Context) (types.ControlPlaneAuditEventInput, error) {
			return event, nil
		},
	)

	g.Expect(err).To(MatchError("audit unavailable"))
	g.Expect(committed).To(BeFalse())
	g.Expect(hook.events).To(ConsistOf(event))
}

func TestGovernanceAuditHookIsContextLocal(t *testing.T) {
	g := NewWithT(t)
	first := &recordingGovernanceAuditHook{}
	second := &recordingGovernanceAuditHook{}
	event := types.ControlPlaneAuditEventInput{
		OrganizationID: uuid.New(), EventType: "policy.version.published",
		Outcome: "SUCCEEDED", DeploymentPolicyVersionID: new(uuid.UUID),
	}
	*event.DeploymentPolicyVersionID = uuid.New()

	firstCtx := WithControlPlaneDomainAuditHook(context.Background(), first)
	secondCtx := WithControlPlaneDomainAuditHook(context.Background(), second)

	g.Expect(recordGovernanceAuditMutation(firstCtx, event)).To(Succeed())
	g.Expect(recordGovernanceAuditMutation(secondCtx, event)).To(Succeed())
	g.Expect(first.events).To(ConsistOf(event))
	g.Expect(second.events).To(ConsistOf(event))
	_, direct := controlPlaneDomainAuditHook(context.Background()).(directControlPlaneAuditAppendHook)
	g.Expect(direct).To(BeTrue())
	_, nilFallback := controlPlaneDomainAuditHook(
		WithControlPlaneDomainAuditHook(context.Background(), nil),
	).(directControlPlaneAuditAppendHook)
	g.Expect(nilFallback).To(BeTrue())
}
