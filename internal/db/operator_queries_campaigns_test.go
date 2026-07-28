package db

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestOperatorCampaignQueriesUseFixedQueryCounts(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	g.Expect(operatorCampaignListQueryCount).To(Equal(1))
	g.Expect(operatorCampaignDetailQueryCount).To(Equal(1))
	g.Expect(listOperatorCampaignsSQL).NotTo(ContainSubstring("SELECT *"))
	g.Expect(getOperatorCampaignSQL).NotTo(ContainSubstring("SELECT *"))
}

func TestOperatorCampaignListScopesBeforeStableKeysetPagination(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	for _, fragment := range []string{
		"draft.organization_id = @organization_id",
		"@organization_scope",
		"@campaign_scope_ids",
		"@customer_scope_ids",
		"@environment_scope_ids",
		"@deployment_unit_scope_ids",
		"@component_scope_ids",
		"@environment_id",
		"@deployment_plan_id",
		"@status",
		"(draft.created_at, draft.id) <",
		"(@cursor_created_at::timestamptz, @cursor_id::uuid)",
		"ORDER BY draft.created_at DESC, draft.id DESC",
		"LIMIT @limit_plus_one",
	} {
		g.Expect(listOperatorCampaignsSQL).To(ContainSubstring(fragment))
	}

	scope := strings.Index(listOperatorCampaignsSQL, "@organization_scope")
	cursor := strings.Index(listOperatorCampaignsSQL, "@cursor_created_at")
	limit := strings.LastIndex(listOperatorCampaignsSQL, "LIMIT @limit_plus_one")
	g.Expect(scope).To(BeNumerically(">=", 0))
	g.Expect(cursor).To(BeNumerically(">", scope))
	g.Expect(limit).To(BeNumerically(">", cursor))
}

func TestOperatorCampaignListProjectsDraftRevisionRunCountsAndChecksums(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	for _, fragment := range []string{
		"DeploymentCampaignDraft AS draft",
		"DeploymentCampaignRevision AS revision",
		"ORDER BY revision.published_at DESC, revision.id DESC",
		"DeploymentCampaignRun AS run",
		"ORDER BY run.created_at DESC, run.id DESC",
		"revision.canonical_checksum",
		"member.plan_checksum",
		"member.approval_checksum",
		"member.admission_checksum",
		"member_counts.total",
		"member_counts.pending",
		"member_counts.running",
		"member_counts.succeeded",
		"member_counts.failed",
		"AS excluded",
		"AS canceled",
		"run.admissions_blocked",
		"run.reconciliation_required",
		"member_counts.uncertain",
	} {
		g.Expect(listOperatorCampaignsSQL).To(ContainSubstring(fragment))
	}
}

func TestOperatorCampaignDetailReturnsCompleteControlEvidence(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	for _, fragment := range []string{
		"draft.organization_id = @organization_id",
		"draft.id = @campaign_id",
		"jsonb_agg",
		"DeploymentCampaignWave AS wave",
		"DeploymentCampaignWaveRun AS wave_run",
		"DeploymentCampaignMember AS member",
		"DeploymentCampaignMemberRun AS member_run",
		"DeploymentCampaignPrerequisite AS prerequisite",
		"CampaignPrerequisiteEvaluation AS prerequisite_evaluation",
		"CampaignThresholdEvaluation AS threshold_evaluation",
		"CampaignControlRequest AS control_request",
		"expected_runtime_state_checksum",
		"actual_runtime_state_checksum",
		"request_checksum",
		"execution_uncertain",
		"active_steps_cancellable",
		"admissions_blocked",
		"reconciliation_required",
		"plan_checksum",
		"effective_policy_checksum",
		"approval_checksum",
		"calendar_checksums",
		"admission_checksum",
		"canonical_checksum",
	} {
		g.Expect(getOperatorCampaignSQL).To(ContainSubstring(fragment))
	}
}

func TestNormalizeOperatorCampaignScopesRejectsInvalidIdentityAndMixedWideScope(t *testing.T) {
	t.Parallel()

	for name, scope := range map[string]types.OperatorScopeFilter{
		"nil organization": {},
		"nil identity": {
			OrganizationID: uuid.New(),
			CampaignIDs:    []uuid.UUID{uuid.Nil},
		},
		"mixed wide and narrow": {
			OrganizationID:   uuid.New(),
			OrganizationWide: true,
			CampaignIDs:      []uuid.UUID{uuid.New()},
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := normalizeOperatorCampaignScopes(scope)
			NewWithT(t).Expect(errors.Is(err, apierrors.ErrForbidden)).To(BeTrue())
		})
	}
}

func TestNormalizeOperatorCampaignScopesPreservesCanonicalPartialVisibility(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	organizationID := uuid.New()
	campaignID := uuid.New()
	environmentID := uuid.New()
	values, err := normalizeOperatorCampaignScopes(types.OperatorScopeFilter{
		OrganizationID: organizationID, DecisionAt: time.Now().UTC(),
		CampaignIDs: []uuid.UUID{campaignID}, EnvironmentIDs: []uuid.UUID{environmentID},
		CustomerIDs: []uuid.UUID{}, DeploymentUnitIDs: []uuid.UUID{}, ComponentIDs: []uuid.UUID{},
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(values.OrganizationWide).To(BeFalse())
	g.Expect(values.CampaignIDs).To(Equal([]uuid.UUID{campaignID}))
	g.Expect(values.EnvironmentIDs).To(Equal([]uuid.UUID{environmentID}))
	g.Expect(values.CustomerIDs).To(BeEmpty())
}

func TestOperatorCampaignStatusMakesPartialRowsExplicit(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	revisionID := uuid.New()
	g.Expect(operatorCampaignStatus(nil, nil)).To(Equal("DRAFT"))
	g.Expect(operatorCampaignStatus(&revisionID, nil)).To(Equal("PUBLISHED"))
	running := "RUNNING"
	g.Expect(operatorCampaignStatus(&revisionID, &running)).To(Equal("RUNNING"))
	unknown := ""
	g.Expect(operatorCampaignStatus(&revisionID, &unknown)).To(Equal("UNKNOWN"))
}

func TestBuildOperatorCampaignDetailPreservesPartialAndUnknownState(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	revisionID := uuid.New()
	planID := uuid.New()
	unitID := uuid.New()
	detail := buildOperatorCampaignDetail(operatorCampaignDetailRecord{
		DraftID: uuid.New(), CreatedAt: time.Now().UTC(), Name: "partial campaign",
		RevisionID: &revisionID, RevisionChecksum: "sha256:" + strings.Repeat("a", 64),
		Waves: []operatorCampaignWaveProjection{{
			ID: uuid.New(), Order: 1, Name: "canary", BakeSeconds: 30, MaximumConcurrency: 1,
		}},
		Members: []operatorCampaignMemberProjection{{
			ID: uuid.New(), DeploymentPlanID: planID, DeploymentUnitID: unitID,
			WaveOrder: 1, MemberOrder: 1, PlanChecksum: "sha256:" + strings.Repeat("b", 64),
			AdmissionChecksum: "sha256:" + strings.Repeat("c", 64), ExecutionUncertain: true,
		}},
		Prerequisites: []operatorCampaignPrerequisiteProjection{{
			ID: uuid.New(), UpstreamPlanID: uuid.New(), DownstreamPlanID: planID,
			UpstreamStepKey:              "database.migrate",
			ExpectedRuntimeStateChecksum: "sha256:" + strings.Repeat("d", 64),
		}},
	})

	g.Expect(detail.Campaign.Status).To(Equal("PUBLISHED"))
	g.Expect(detail.Waves).To(HaveLen(1))
	g.Expect(detail.Waves[0].Status).To(Equal("PENDING"))
	g.Expect(detail.Members).To(HaveLen(1))
	g.Expect(detail.Members[0].Status).To(Equal("PENDING"))
	g.Expect(detail.Prerequisites).To(HaveLen(1))
	g.Expect(detail.Prerequisites[0].Status).To(Equal("UNKNOWN"))
	g.Expect(detail.Prerequisites[0].Blocking).To(BeTrue())
	g.Expect(detail.UncertaintyBlockers).To(HaveLen(1))
	g.Expect(detail.AdmissionBlockers).To(HaveLen(1))
	g.Expect(detail.Campaign.BlockedCount).To(Equal(2))
	for _, checksum := range []string{
		detail.MembershipChecksum,
		detail.PrerequisiteChecksum,
		detail.ThresholdChecksum,
		detail.ControlChecksum,
		detail.AdmissionChecksum,
	} {
		g.Expect(checksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
	}
}

func TestBuildOperatorCampaignDetailProjectsThresholdAndControlBlockers(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	runState := "PAUSED"
	detail := buildOperatorCampaignDetail(operatorCampaignDetailRecord{
		DraftID: uuid.New(), CreatedAt: time.Now().UTC(), Name: "paused campaign",
		RevisionID: new(uuid.UUID), RunID: new(uuid.UUID), RunState: &runState,
		AdmissionsBlocked: true, ReconciliationNeeded: true,
		Thresholds: []operatorCampaignThresholdProjection{{
			ID: uuid.New(), Samples: 10, Failed: 2, FailureRate: 0.2,
			MaximumFailureRate: 0.1, Breached: true, FencingToken: 7,
		}},
		Controls: []operatorCampaignControlProjection{{
			ID: uuid.New(), Kind: "PAUSE", Status: "PENDING_SAFE_POINT",
			RequestChecksum: "sha256:" + strings.Repeat("e", 64), Reason: "investigate",
		}},
	})

	g.Expect(detail.Campaign.Status).To(Equal("PAUSED"))
	g.Expect(detail.Thresholds).To(HaveLen(1))
	g.Expect(detail.Thresholds[0].Blocking).To(BeTrue())
	g.Expect(detail.Controls).To(HaveLen(1))
	g.Expect(detail.Controls[0].Blocking).To(BeTrue())
	g.Expect(detail.AdmissionBlockers).To(HaveLen(2))
	g.Expect(detail.UncertaintyBlockers).To(HaveLen(1))
}
