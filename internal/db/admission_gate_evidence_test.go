package db

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/scheduling"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

const (
	admissionComponentReleaseCommit    = "0123456789abcdef0123456789abcdef01234567"
	admissionComponentReleaseMediaType = "application/vnd.oci.image.index.v1+json"
)

type admissionGateEvidenceSourceStub struct {
	plan      admissionGateEvidencePlanRecord
	planErr   error
	preflight admissionGateEvidencePreflightRecord
	preErr    error
	preCalls  *int
}

type admissionGateEvidencePreparerStub struct {
	prepare func(context.Context, admissionGateEvidenceContext, uuid.UUID) error
}

func (s admissionGateEvidencePreparerStub) PrepareAdmissionGateEvidence(
	ctx context.Context,
	evidenceContext admissionGateEvidenceContext,
	actorUserAccountID uuid.UUID,
) error {
	return s.prepare(ctx, evidenceContext, actorUserAccountID)
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

func TestPersistedAdmissionGateEvidenceRepositoryMapsComponentReleaseEvidenceExplicitly(
	t *testing.T,
) {
	g := NewWithT(t)
	evaluatedAt := time.Date(2026, time.July, 29, 9, 0, 0, 0, time.UTC)
	evidenceContext, source := admissionGateEvidenceTestMaterial(evaluatedAt)
	source.plan.EffectivePolicy.RequiredEvidence = []string{"provenance", "sbom"}
	source.preflight.Checks = []admissionGateEvidenceCheckRecord{
		admissionReleaseEvidenceCheck(
			evidenceContext,
			source.preflight,
			"release_provenance",
			"oci://evidence.example.invalid/provenance@"+admissionTestChecksum("provenance"),
		),
		admissionReleaseEvidenceCheck(
			evidenceContext,
			source.preflight,
			"release_sbom",
			"oci://evidence.example.invalid/sbom@"+admissionTestChecksum("sbom"),
		),
	}

	evidence, err := (persistedAdmissionGateEvidenceRepository{source: source}).
		ResolveAdmissionGateEvidence(context.Background(), evidenceContext)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(evidence).To(HaveLen(2))
	g.Expect([]types.AdmissionGateKey{evidence[0].Key, evidence[1].Key}).To(
		ConsistOf(types.AdmissionGateProvenance, types.AdmissionGateKey("sbom")),
	)
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

func TestAdmissionPreparesRequiredEvidenceBeforeChoosingItsEvaluationTime(t *testing.T) {
	g := NewWithT(t)
	authorizationAt := time.Date(2026, time.July, 29, 9, 0, 0, 0, time.UTC)
	evaluationAt := authorizationAt.Add(2 * time.Second)
	evidenceContext, sourceValue := admissionGateEvidenceTestMaterial(authorizationAt)
	sourceValue.preErr = apierrors.ErrNotFound
	source := &sourceValue
	actorUserAccountID := uuid.New()
	events := make([]string, 0, 3)
	repository := persistedAdmissionGateEvidenceRepository{
		source: source,
		preparer: admissionGateEvidencePreparerStub{
			prepare: func(
				_ context.Context,
				preparedContext admissionGateEvidenceContext,
				preparedActorUserAccountID uuid.UUID,
			) error {
				events = append(events, "prepare")
				g.Expect(preparedContext).To(Equal(evidenceContext))
				g.Expect(preparedActorUserAccountID).To(Equal(actorUserAccountID))
				source.preErr = nil
				source.preflight.CreatedAt = authorizationAt.Add(time.Second)
				for index := range source.preflight.Checks {
					source.preflight.Checks[index].CreatedAt = source.preflight.CreatedAt
				}
				return nil
			},
		},
	}

	resolvedAt, err := prepareAdmissionGateEvidenceBeforeEvaluation(
		context.Background(),
		repository,
		evidenceContext,
		actorUserAccountID,
		func(context.Context) (time.Time, error) {
			events = append(events, "evaluation-time")
			return evaluationAt, nil
		},
	)
	g.Expect(err).NotTo(HaveOccurred())

	evidenceContext.EvaluatedAt = resolvedAt
	events = append(events, "resolve")
	evidence, err := repository.ResolveAdmissionGateEvidence(context.Background(), evidenceContext)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(evidence).To(HaveLen(1))
	g.Expect(resolvedAt).To(Equal(evaluationAt))
	g.Expect(events).To(Equal([]string{"prepare", "evaluation-time", "resolve"}))

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
	request.EvaluatedAt = resolvedAt

	evaluation, err := scheduling.EvaluateAdmission(context.Background(), request)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(evaluation.Decision).To(Equal(types.AdmissionDecisionAdmit))
	g.Expect(evaluation.CampaignID).NotTo(BeNil())
	g.Expect(*evaluation.CampaignID).To(Equal(request.Campaign.ID))
}

func TestDeploymentPreflightAcceptsExactValidComponentReleaseEvidenceContract(t *testing.T) {
	g := NewWithT(t)
	component := admissionComponentReleaseContract()
	planContract := &types.ReleaseContract{
		Schema:      types.ReleaseContractSchemaV2,
		ComponentV2: &component,
	}
	bundleComponent := admissionComponentReleaseContract()
	bundleContract := &types.ReleaseContract{
		Schema:      types.ReleaseContractSchemaV2,
		ComponentV2: &bundleComponent,
	}

	g.Expect(deploymentPreflightReleaseContractValid(
		planContract,
		bundleContract,
		nil,
	)).To(BeTrue())

	bundleContract.ComponentV2.Evidence.SBOM[0] =
		"oci://evidence.example.invalid/changed@sha256:" + strings.Repeat("f", 64)
	g.Expect(deploymentPreflightReleaseContractValid(
		planContract,
		bundleContract,
		nil,
	)).To(BeFalse())
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

func admissionComponentReleaseContract() types.ComponentReleaseContractV2 {
	return types.ComponentReleaseContractV2{
		Schema:       types.ReleaseContractSchemaV2,
		ComponentKey: "payments.api",
		Version:      "2.4.0",
		Source: types.ComponentReleaseSource{
			Repository:   "source/payments-api",
			RequestedRef: "refs/tags/v2.4.0",
			Commit:       admissionComponentReleaseCommit,
		},
		Build: types.ComponentReleaseBuild{
			ID:      "build-42",
			Builder: "generic-ci",
		},
		Artifacts: []types.ComponentReleaseArtifact{{
			Key:       "image",
			Type:      "oci-image",
			MediaType: admissionComponentReleaseMediaType,
			Digest:    "sha256:" + strings.Repeat("a", 64),
			Platforms: []types.ComponentReleasePlatform{{
				Platform: "linux/amd64",
				Digest:   "sha256:" + strings.Repeat("b", 64),
			}},
		}},
		Changes: types.ComponentReleaseChanges{
			Summary: "Add account settlement support",
			Commits: []string{admissionComponentReleaseCommit},
		},
		Evidence: types.ComponentReleaseEvidenceReferences{
			Provenance: []string{
				"oci://evidence.example.invalid/provenance@sha256:" + strings.Repeat("d", 64),
			},
			SBOM: []string{
				"oci://evidence.example.invalid/sbom@sha256:" + strings.Repeat("e", 64),
			},
		},
	}
}

func admissionReleaseEvidenceCheck(
	evidenceContext admissionGateEvidenceContext,
	preflight admissionGateEvidencePreflightRecord,
	checkKey, reference string,
) admissionGateEvidenceCheckRecord {
	return admissionGateEvidenceCheckRecord{
		ID:                       uuid.New(),
		CreatedAt:                preflight.CreatedAt,
		OrganizationID:           evidenceContext.OrganizationID,
		DeploymentPlanID:         evidenceContext.DeploymentPlanID,
		DeploymentPreflightRunID: preflight.ID,
		CheckKey:                 checkKey,
		Status:                   types.DeploymentPreflightCheckStatusPassed,
		Expected:                 json.RawMessage(`{"present":true,"contractValid":true}`),
		Actual: json.RawMessage(
			`{"present":true,"contractValid":true,"references":["` + reference + `"]}`,
		),
	}
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
