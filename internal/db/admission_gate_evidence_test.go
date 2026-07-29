package db

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/scheduling"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

type admissionGateEvidenceSourceStub struct {
	plan      admissionGateEvidencePlanRecord
	planErr   error
	preflight admissionGateEvidencePreflightRecord
	preErr    error
	preCalls  *int
}

func (s admissionGateEvidenceSourceStub) LoadAdmissionGateEvidencePlan(
	context.Context,
	admissionGateEvidenceContext,
) (admissionGateEvidencePlanRecord, error) {
	return s.plan, s.planErr
}

func (s admissionGateEvidenceSourceStub) LoadAdmissionGateEvidencePreflight(
	context.Context,
	admissionGateEvidenceContext,
) (admissionGateEvidencePreflightRecord, error) {
	if s.preCalls != nil {
		(*s.preCalls)++
	}
	return s.preflight, s.preErr
}

func TestPersistedAdmissionGateEvidenceRepositoryDerivesRequiredEvidenceFromExactPassedPreflight(
	t *testing.T,
) {
	g := NewWithT(t)
	evaluatedAt := time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC)
	evidenceContext := admissionGateEvidenceContext{
		OrganizationID:          uuid.New(),
		DeploymentPlanID:        uuid.New(),
		PlanRevision:            1,
		PlanChecksum:            admissionTestChecksum("plan"),
		EffectivePolicyChecksum: admissionTestChecksum("policy"),
		EvaluatedAt:             evaluatedAt,
	}
	runID := uuid.New()
	source := admissionGateEvidenceSourceStub{
		plan: admissionGateEvidencePlanRecord{
			OrganizationID:          evidenceContext.OrganizationID,
			DeploymentPlanID:        evidenceContext.DeploymentPlanID,
			PlanRevision:            evidenceContext.PlanRevision,
			PlanChecksum:            evidenceContext.PlanChecksum,
			EffectivePolicyChecksum: evidenceContext.EffectivePolicyChecksum,
			EffectivePolicy: types.EffectivePolicy{
				Checksum: evidenceContext.EffectivePolicyChecksum,
				RequiredEvidence: []string{
					string(types.AdmissionGateIntegrity),
					string(types.AdmissionGateBackup),
				},
			},
		},
		preflight: admissionGateEvidencePreflightRecord{
			ID:               runID,
			CreatedAt:        evaluatedAt.Add(-time.Minute),
			OrganizationID:   evidenceContext.OrganizationID,
			DeploymentPlanID: evidenceContext.DeploymentPlanID,
			PlanChecksum:     evidenceContext.PlanChecksum,
			Status:           types.DeploymentPreflightStatusPassed,
			Checks: []admissionGateEvidenceCheckRecord{
				{
					ID:                       uuid.New(),
					CreatedAt:                evaluatedAt.Add(-time.Minute),
					OrganizationID:           evidenceContext.OrganizationID,
					DeploymentPlanID:         evidenceContext.DeploymentPlanID,
					DeploymentPreflightRunID: runID,
					CheckKey:                 "plan_checksum",
					Status:                   types.DeploymentPreflightCheckStatusPassed,
					Expected: json.RawMessage(
						`{"checksum":"` + evidenceContext.PlanChecksum + `"}`,
					),
					Actual: json.RawMessage(`{"valid":true}`),
				},
				{
					ID:                       uuid.New(),
					CreatedAt:                evaluatedAt.Add(-time.Minute),
					OrganizationID:           evidenceContext.OrganizationID,
					DeploymentPlanID:         evidenceContext.DeploymentPlanID,
					DeploymentPreflightRunID: runID,
					CheckKey:                 "migration_backup:ledger.042",
					Status:                   types.DeploymentPreflightCheckStatusPassed,
					Actual: json.RawMessage(
						`{"required":true,"verified":true,"checksum":"` +
							admissionTestChecksum("backup") + `"}`,
					),
				},
			},
		},
	}

	evidence, err := (persistedAdmissionGateEvidenceRepository{source: source}).
		ResolveAdmissionGateEvidence(context.Background(), evidenceContext)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(evidence).To(HaveLen(2))
	g.Expect([]types.AdmissionGateKey{evidence[0].Key, evidence[1].Key}).To(
		ConsistOf(types.AdmissionGateIntegrity, types.AdmissionGateBackup),
	)
	for _, item := range evidence {
		g.Expect(item.Mandatory).To(BeTrue())
		g.Expect(item.Satisfied).To(BeTrue())
		g.Expect(item.Checksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
	}
}

func TestPersistedAdmissionGateEvidenceRepositoryFailsClosedForUntrustedBindings(t *testing.T) {
	evaluatedAt := time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC)
	baseContext, baseSource := admissionGateEvidenceTestMaterial(evaluatedAt)
	tests := []struct {
		name    string
		mutate  func(*admissionGateEvidenceContext, *admissionGateEvidenceSourceStub)
		message string
	}{
		{
			name: "missing",
			mutate: func(_ *admissionGateEvidenceContext, source *admissionGateEvidenceSourceStub) {
				source.planErr = apierrors.ErrNotFound
			},
			message: "trusted admission plan evidence is missing",
		},
		{
			name: "missing preflight",
			mutate: func(_ *admissionGateEvidenceContext, source *admissionGateEvidenceSourceStub) {
				source.preErr = apierrors.ErrNotFound
			},
			message: "required trusted admission preflight evidence is missing",
		},
		{
			name: "missing required check",
			mutate: func(_ *admissionGateEvidenceContext, source *admissionGateEvidenceSourceStub) {
				source.preflight.Checks = nil
			},
			message: `required trusted admission evidence "integrity" is missing`,
		},
		{
			name: "backup was not actually required or verified",
			mutate: func(_ *admissionGateEvidenceContext, source *admissionGateEvidenceSourceStub) {
				source.plan.EffectivePolicy.RequiredEvidence = []string{
					string(types.AdmissionGateBackup),
				}
				source.preflight.Checks[0].CheckKey = "migration_backup:ledger.042"
				source.preflight.Checks[0].Actual = json.RawMessage(
					`{"required":false,"verified":false}`,
				)
			},
			message: `required trusted admission evidence "backup" is invalid`,
		},
		{
			name: "stale",
			mutate: func(_ *admissionGateEvidenceContext, source *admissionGateEvidenceSourceStub) {
				source.preflight.CreatedAt = evaluatedAt.Add(time.Second)
			},
			message: "preflight evidence is stale",
		},
		{
			name: "cross organization",
			mutate: func(_ *admissionGateEvidenceContext, source *admissionGateEvidenceSourceStub) {
				source.plan.OrganizationID = uuid.New()
			},
			message: "does not match exact admission material",
		},
		{
			name: "plan checksum mismatch",
			mutate: func(_ *admissionGateEvidenceContext, source *admissionGateEvidenceSourceStub) {
				source.preflight.PlanChecksum = admissionTestChecksum("changed")
			},
			message: "preflight evidence is stale",
		},
		{
			name: "effective policy checksum mismatch",
			mutate: func(_ *admissionGateEvidenceContext, source *admissionGateEvidenceSourceStub) {
				source.plan.EffectivePolicyChecksum = admissionTestChecksum("changed")
			},
			message: "does not match exact admission material",
		},
		{
			name: "missing evaluated time",
			mutate: func(evidenceContext *admissionGateEvidenceContext, _ *admissionGateEvidenceSourceStub) {
				evidenceContext.EvaluatedAt = time.Time{}
			},
			message: "trusted admission gate evidence context is invalid",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evidenceContext := baseContext
			source := baseSource
			tt.mutate(&evidenceContext, &source)

			_, err := (persistedAdmissionGateEvidenceRepository{source: source}).
				ResolveAdmissionGateEvidence(context.Background(), evidenceContext)

			g := NewWithT(t)
			g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
			g.Expect(err).To(MatchError(ContainSubstring(tt.message)))
		})
	}
}

func TestProductionAdmissionUsesPersistedTrustedGateEvidenceRepository(t *testing.T) {
	g := NewWithT(t)
	repository, ok := trustedAdmissionGateEvidenceRepository.(persistedAdmissionGateEvidenceRepository)
	g.Expect(ok).To(BeTrue())
	g.Expect(repository.source).To(BeAssignableToTypeOf(databaseAdmissionGateEvidenceSource{}))
}

func TestPersistedAdmissionGateEvidenceRepositoryAllowsCampaignAdmissionWithoutUnrequiredGates(
	t *testing.T,
) {
	g := NewWithT(t)
	evaluatedAt := time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC)
	evidenceContext, source := admissionGateEvidenceTestMaterial(evaluatedAt)
	source.plan.EffectivePolicy.RequiredEvidence = []string{}
	preflightCalls := 0
	source.preCalls = &preflightCalls

	evidence, err := (persistedAdmissionGateEvidenceRepository{source: source}).
		ResolveAdmissionGateEvidence(context.Background(), evidenceContext)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(evidence).To(BeEmpty())
	g.Expect(preflightCalls).To(Equal(0))

	request := sealedAdmissionTestRequest()
	request.OrganizationID = evidenceContext.OrganizationID
	request.Plan.ID = evidenceContext.DeploymentPlanID
	request.Plan.Revision = evidenceContext.PlanRevision
	request.Plan.Checksum = evidenceContext.PlanChecksum
	request.EffectivePolicy = source.plan.EffectivePolicy
	request.EffectivePolicy.VersionIDs = []uuid.UUID{uuid.New()}
	request.EffectivePolicy.SubscriberSetChecksum = admissionTestChecksum("subscribers")
	request.Approval.SubjectChecksum = request.Plan.Checksum
	request.Approval.EffectivePolicyChecksum = request.EffectivePolicy.Checksum
	request.Approval.SubscriberSetChecksum = request.EffectivePolicy.SubscriberSetChecksum
	request.Approval.Evaluation.State = types.ApprovalRequestStateApproved
	request.Approval.Evaluation.Eligible = true
	request.Campaign = &types.AdmissionCampaignEvidence{
		ID:       uuid.New(),
		Revision: 1,
		Checksum: admissionTestChecksum("campaign"),
	}
	request.GateEvidence = evidence
	request.EvaluatedAt = evaluatedAt

	evaluation, err := scheduling.EvaluateAdmission(context.Background(), request)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(evaluation.Decision).To(Equal(types.AdmissionDecisionAdmit))
	g.Expect(evaluation.CampaignID).NotTo(BeNil())
	g.Expect(*evaluation.CampaignID).To(Equal(request.Campaign.ID))
}

func admissionGateEvidenceTestMaterial(
	evaluatedAt time.Time,
) (admissionGateEvidenceContext, admissionGateEvidenceSourceStub) {
	evidenceContext := admissionGateEvidenceContext{
		OrganizationID:          uuid.New(),
		DeploymentPlanID:        uuid.New(),
		PlanRevision:            1,
		PlanChecksum:            admissionTestChecksum("plan"),
		EffectivePolicyChecksum: admissionTestChecksum("policy"),
		EvaluatedAt:             evaluatedAt,
	}
	runID := uuid.New()
	source := admissionGateEvidenceSourceStub{
		plan: admissionGateEvidencePlanRecord{
			OrganizationID:          evidenceContext.OrganizationID,
			DeploymentPlanID:        evidenceContext.DeploymentPlanID,
			PlanRevision:            evidenceContext.PlanRevision,
			PlanChecksum:            evidenceContext.PlanChecksum,
			EffectivePolicyChecksum: evidenceContext.EffectivePolicyChecksum,
			EffectivePolicy: types.EffectivePolicy{
				Checksum: evidenceContext.EffectivePolicyChecksum,
				RequiredEvidence: []string{
					string(types.AdmissionGateIntegrity),
				},
			},
		},
		preflight: admissionGateEvidencePreflightRecord{
			ID:               runID,
			CreatedAt:        evaluatedAt.Add(-time.Minute),
			OrganizationID:   evidenceContext.OrganizationID,
			DeploymentPlanID: evidenceContext.DeploymentPlanID,
			PlanChecksum:     evidenceContext.PlanChecksum,
			Status:           types.DeploymentPreflightStatusPassed,
			Checks: []admissionGateEvidenceCheckRecord{{
				ID:                       uuid.New(),
				CreatedAt:                evaluatedAt.Add(-time.Minute),
				OrganizationID:           evidenceContext.OrganizationID,
				DeploymentPlanID:         evidenceContext.DeploymentPlanID,
				DeploymentPreflightRunID: runID,
				CheckKey:                 "plan_checksum",
				Status:                   types.DeploymentPreflightCheckStatusPassed,
				Expected: json.RawMessage(
					`{"checksum":"` + evidenceContext.PlanChecksum + `"}`,
				),
				Actual: json.RawMessage(`{"valid":true}`),
			}},
		},
	}
	return evidenceContext, source
}
