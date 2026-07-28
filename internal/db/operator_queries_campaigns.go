package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	operatorCampaignListQueryCount   = 1
	operatorCampaignDetailQueryCount = 1
)

const listOperatorCampaignsSQL = `
SELECT
  draft.id AS id,
  draft.created_at,
  draft.id AS draft_id,
  revision.id AS revision_id,
  run.id AS run_id,
  draft.name,
  COALESCE(
    run.state,
    CASE WHEN revision.id IS NULL THEN 'DRAFT' ELSE 'PUBLISHED' END
  ) AS status,
  COALESCE(revision.canonical_checksum, '') AS canonical_checksum,
  COALESCE(wave_counts.total, 0)::integer AS wave_count,
  COALESCE(member_counts.total, 0)::integer AS member_count,
  COALESCE(member_counts.pending, 0)::integer AS pending_count,
  COALESCE(member_counts.running, 0)::integer AS running_count,
  COALESCE(member_counts.succeeded, 0)::integer AS succeeded_count,
  COALESCE(member_counts.failed, 0)::integer AS failed_count,
  (
    COALESCE(member_counts.failed, 0)
    + COALESCE(member_counts.uncertain, 0)
    + CASE WHEN COALESCE(run.admissions_blocked, false) THEN 1 ELSE 0 END
    + CASE WHEN COALESCE(run.reconciliation_required, false) THEN 1 ELSE 0 END
  )::integer AS blocked_count
FROM DeploymentCampaignDraft AS draft
LEFT JOIN LATERAL (
  SELECT
    revision.id,
    revision.organization_id,
    revision.revision_number,
    revision.published_at,
    revision.canonical_checksum
  FROM DeploymentCampaignRevision AS revision
  WHERE revision.organization_id = draft.organization_id
    AND revision.deployment_campaign_draft_id = draft.id
  ORDER BY revision.published_at DESC, revision.id DESC
  LIMIT 1
) AS revision ON true
LEFT JOIN LATERAL (
  SELECT
    run.id,
    run.organization_id,
    run.state,
    run.updated_at,
    run.current_wave_order,
    run.admissions_blocked,
    run.reconciliation_required
  FROM DeploymentCampaignRun AS run
  WHERE run.organization_id = draft.organization_id
    AND run.campaign_revision_id = revision.id
  ORDER BY run.created_at DESC, run.id DESC
  LIMIT 1
) AS run ON true
LEFT JOIN LATERAL (
  SELECT count(*)::bigint AS total
  FROM DeploymentCampaignWave AS wave
  WHERE wave.organization_id = draft.organization_id
    AND wave.campaign_revision_id = revision.id
) AS wave_counts ON true
LEFT JOIN LATERAL (
  SELECT
    count(*)::bigint AS total,
    count(*) FILTER (WHERE member_run.status = 'PENDING')::bigint AS pending,
    count(*) FILTER (WHERE member_run.status = 'ADMITTED')::bigint AS admitted,
    count(*) FILTER (WHERE member_run.status = 'RUNNING')::bigint AS running,
    count(*) FILTER (WHERE member_run.status = 'SUCCEEDED')::bigint AS succeeded,
    count(*) FILTER (WHERE member_run.status = 'FAILED')::bigint AS failed,
    count(*) FILTER (WHERE member_run.status = 'EXCLUDED')::bigint AS excluded,
    count(*) FILTER (WHERE member_run.status = 'CANCELED')::bigint AS canceled,
    count(*) FILTER (WHERE member_run.execution_uncertain)::bigint AS uncertain,
    array_agg(member.plan_checksum ORDER BY member.wave_order, member.member_order) AS plan_checksums,
    array_agg(member.approval_checksum ORDER BY member.wave_order, member.member_order) AS approval_checksums,
    array_agg(member.admission_checksum ORDER BY member.wave_order, member.member_order) AS admission_checksums
  FROM DeploymentCampaignMember AS member
  LEFT JOIN DeploymentCampaignMemberRun AS member_run
    ON member_run.organization_id = draft.organization_id
   AND member_run.campaign_revision_id = member.campaign_revision_id
   AND member_run.campaign_member_id = member.id
   AND member_run.campaign_run_id = run.id
  WHERE member.organization_id = draft.organization_id
    AND member.campaign_revision_id = revision.id
) AS member_counts ON true
WHERE draft.organization_id = @organization_id
  AND (
    @organization_scope
    OR draft.id = ANY(@campaign_scope_ids::uuid[])
    OR EXISTS (
      SELECT 1
      FROM DeploymentCampaignRevision AS scope_revision
      JOIN DeploymentCampaignMember AS scope_member
        ON scope_member.organization_id = draft.organization_id
       AND scope_member.campaign_revision_id = scope_revision.id
      JOIN DeploymentPlan AS scope_plan
        ON scope_plan.organization_id = draft.organization_id
       AND scope_plan.id = scope_member.deployment_plan_id
      JOIN DeploymentUnit AS scope_unit
        ON scope_unit.organization_id = draft.organization_id
       AND scope_unit.id = scope_member.deployment_unit_id
      JOIN TargetEnvironmentAssignment AS scope_assignment
        ON scope_assignment.organization_id = draft.organization_id
       AND scope_assignment.id = scope_unit.target_environment_assignment_id
      JOIN DeploymentScope AS deployment_scope
        ON deployment_scope.organization_id = draft.organization_id
       AND deployment_scope.id = scope_unit.deployment_scope_id
      WHERE scope_revision.organization_id = draft.organization_id
        AND scope_revision.deployment_campaign_draft_id = draft.id
        AND (
          scope_plan.environment_id = ANY(@environment_scope_ids::uuid[])
          OR scope_unit.id = ANY(@deployment_unit_scope_ids::uuid[])
          OR deployment_scope.customer_organization_id = ANY(@customer_scope_ids::uuid[])
          OR EXISTS (
            SELECT 1
            FROM DeploymentUnitSubscriber AS subscriber
            WHERE subscriber.organization_id = draft.organization_id
              AND subscriber.deployment_unit_id = scope_unit.id
              AND subscriber.retired_at IS NULL
              AND subscriber.customer_organization_id = ANY(@customer_scope_ids::uuid[])
          )
          OR EXISTS (
            SELECT 1
            FROM DeploymentPlanTargetComponent AS scoped_component
            JOIN ComponentDefinition AS component_definition
              ON component_definition.organization_id = draft.organization_id
             AND component_definition.key = scoped_component.component
            WHERE scoped_component.organization_id = draft.organization_id
              AND scoped_component.deployment_plan_id = scope_plan.id
              AND component_definition.id = ANY(@component_scope_ids::uuid[])
          )
        )
    )
  )
  AND (
    @environment_id::uuid IS NULL
    OR EXISTS (
      SELECT 1
      FROM DeploymentCampaignMember AS filtered_member
      JOIN DeploymentPlan AS filtered_plan
        ON filtered_plan.organization_id = draft.organization_id
       AND filtered_plan.id = filtered_member.deployment_plan_id
      WHERE filtered_member.organization_id = draft.organization_id
        AND filtered_member.campaign_revision_id = revision.id
        AND filtered_plan.environment_id = @environment_id
    )
  )
  AND (
    @deployment_plan_id::uuid IS NULL
    OR EXISTS (
      SELECT 1
      FROM DeploymentCampaignMember AS filtered_member
      WHERE filtered_member.organization_id = draft.organization_id
        AND filtered_member.campaign_revision_id = revision.id
        AND filtered_member.deployment_plan_id = @deployment_plan_id
    )
  )
  AND (
    @status = ''
    OR COALESCE(run.state, CASE WHEN revision.id IS NULL THEN 'DRAFT' ELSE 'PUBLISHED' END) = @status
  )
  AND (
    @cursor_created_at::timestamptz IS NULL
    OR (draft.created_at, draft.id) <
       (@cursor_created_at::timestamptz, @cursor_id::uuid)
  )
ORDER BY draft.created_at DESC, draft.id DESC
LIMIT @limit_plus_one`

const getOperatorCampaignSQL = `
SELECT
  draft.id,
  draft.created_at,
  draft.updated_at,
  draft.name,
  draft.description,
  draft.revision AS draft_revision,
  draft.membership,
  draft.waves AS draft_waves,
  draft.prerequisites AS draft_prerequisites,
  draft.risk_policy AS draft_risk_policy,
  revision.id AS campaign_revision_id,
  revision.revision_number,
  revision.source_draft_revision,
  revision.published_at,
  revision.membership_tag_query,
  revision.risk_policy AS frozen_risk_policy,
  revision.canonical_checksum,
  run.id AS campaign_run_id,
  run.created_at AS run_created_at,
  run.updated_at AS run_updated_at,
  run.state AS run_state,
  run.version AS run_version,
  run.current_wave_order,
  run.current_member_order,
  run.admissions_blocked,
  run.pause_requested,
  run.reconciliation_required,
  run.fencing_token,
  COALESCE(waves.items, '[]'::jsonb) AS waves,
  COALESCE(members.items, '[]'::jsonb) AS members,
  COALESCE(prerequisites.items, '[]'::jsonb) AS prerequisites,
  COALESCE(thresholds.items, '[]'::jsonb) AS thresholds,
  COALESCE(controls.items, '[]'::jsonb) AS controls
FROM DeploymentCampaignDraft AS draft
LEFT JOIN LATERAL (
  SELECT revision.*
  FROM DeploymentCampaignRevision AS revision
  WHERE revision.organization_id = draft.organization_id
    AND revision.deployment_campaign_draft_id = draft.id
  ORDER BY revision.published_at DESC, revision.id DESC
  LIMIT 1
) AS revision ON true
LEFT JOIN LATERAL (
  SELECT run.*
  FROM DeploymentCampaignRun AS run
  WHERE run.organization_id = draft.organization_id
    AND run.campaign_revision_id = revision.id
  ORDER BY run.created_at DESC, run.id DESC
  LIMIT 1
) AS run ON true
LEFT JOIN LATERAL (
  SELECT jsonb_agg(
    jsonb_build_object(
      'id', wave.id,
      'order', wave.wave_order,
      'name', wave.name,
      'bakeSeconds', wave.bake_seconds,
      'maximumConcurrency', wave.maximum_concurrency,
      'runId', wave_run.id,
      'status', wave_run.status,
      'startedAt', wave_run.started_at,
      'bakeStartedAt', wave_run.bake_started_at,
      'completedAt', wave_run.completed_at
    ) ORDER BY wave.wave_order
  ) AS items
  FROM DeploymentCampaignWave AS wave
  LEFT JOIN DeploymentCampaignWaveRun AS wave_run
    ON wave_run.organization_id = draft.organization_id
   AND wave_run.campaign_revision_id = wave.campaign_revision_id
   AND wave_run.campaign_wave_id = wave.id
   AND wave_run.campaign_run_id = run.id
  WHERE wave.organization_id = draft.organization_id
    AND wave.campaign_revision_id = revision.id
) AS waves ON true
LEFT JOIN LATERAL (
  SELECT jsonb_agg(
    jsonb_build_object(
      'id', member.id,
      'planId', member.deployment_plan_id,
      'deploymentUnitId', member.deployment_unit_id,
      'waveOrder', member.wave_order,
      'memberOrder', member.member_order,
      'planChecksum', member.plan_checksum,
      'effectivePolicyChecksum', member.effective_policy_checksum,
      'approvalChecksum', member.approval_checksum,
      'calendarChecksums', member.calendar_checksums,
      'admissionChecksum', member.admission_checksum,
      'runId', member_run.id,
      'status', member_run.status,
      'admittedAt', member_run.admitted_at,
      'completedAt', member_run.completed_at,
      'executionUncertain', COALESCE(member_run.execution_uncertain, false),
      'activeStepsCancellable', COALESCE(member_run.active_steps_cancellable, false)
    ) ORDER BY member.wave_order, member.member_order, member.id
  ) AS items
  FROM DeploymentCampaignMember AS member
  LEFT JOIN DeploymentCampaignMemberRun AS member_run
    ON member_run.organization_id = draft.organization_id
   AND member_run.campaign_revision_id = member.campaign_revision_id
   AND member_run.campaign_member_id = member.id
   AND member_run.campaign_run_id = run.id
  WHERE member.organization_id = draft.organization_id
    AND member.campaign_revision_id = revision.id
) AS members ON true
LEFT JOIN LATERAL (
  SELECT jsonb_agg(
    jsonb_build_object(
      'id', prerequisite.id,
      'downstreamPlanId', prerequisite.downstream_plan_id,
      'upstreamPlanId', prerequisite.upstream_plan_id,
      'upstreamStepKey', prerequisite.upstream_step_key,
      'providerPlacementId', prerequisite.provider_placement_id,
      'providerDeploymentUnitId', prerequisite.provider_deployment_unit_id,
      'providerComponentInstanceId', prerequisite.provider_component_instance_id,
      'expectedRuntimeStateChecksum', prerequisite.expected_runtime_state_checksum,
      'evaluationId', prerequisite_evaluation.id,
      'actualObservationId', prerequisite_evaluation.actual_observation_id,
      'actualRuntimeStateChecksum', prerequisite_evaluation.actual_runtime_state_checksum,
      'matched', prerequisite_evaluation.matched,
      'reason', prerequisite_evaluation.reason,
      'evaluatedAt', prerequisite_evaluation.evaluated_at
    ) ORDER BY prerequisite.downstream_plan_id, prerequisite.upstream_plan_id,
      prerequisite.upstream_step_key, prerequisite.id
  ) AS items
  FROM DeploymentCampaignPrerequisite AS prerequisite
  LEFT JOIN DeploymentCampaignMemberRun AS prerequisite_member_run
    ON prerequisite_member_run.organization_id = draft.organization_id
   AND prerequisite_member_run.campaign_revision_id = prerequisite.campaign_revision_id
   AND prerequisite_member_run.deployment_plan_id = prerequisite.downstream_plan_id
   AND prerequisite_member_run.campaign_run_id = run.id
  LEFT JOIN LATERAL (
    SELECT prerequisite_evaluation.*
    FROM CampaignPrerequisiteEvaluation AS prerequisite_evaluation
    WHERE prerequisite_evaluation.organization_id = draft.organization_id
      AND prerequisite_evaluation.campaign_run_id = run.id
      AND prerequisite_evaluation.member_run_id = prerequisite_member_run.id
      AND prerequisite_evaluation.upstream_plan_id = prerequisite.upstream_plan_id
      AND prerequisite_evaluation.step_key = prerequisite.upstream_step_key
    ORDER BY prerequisite_evaluation.evaluated_at DESC, prerequisite_evaluation.id DESC
    LIMIT 1
  ) AS prerequisite_evaluation ON true
  WHERE prerequisite.organization_id = draft.organization_id
    AND prerequisite.campaign_revision_id = revision.id
) AS prerequisites ON true
LEFT JOIN LATERAL (
  SELECT jsonb_agg(
    jsonb_build_object(
      'id', threshold_evaluation.id,
      'evaluatedAt', threshold_evaluation.evaluated_at,
      'samples', threshold_evaluation.samples,
      'successful', threshold_evaluation.successful,
      'failed', threshold_evaluation.failed,
      'failureRate', threshold_evaluation.failure_rate,
      'maximumFailureRate', threshold_evaluation.maximum_failure_rate,
      'breached', threshold_evaluation.breached,
      'fencingToken', threshold_evaluation.fencing_token
    ) ORDER BY threshold_evaluation.evaluated_at DESC, threshold_evaluation.id DESC
  ) AS items
  FROM CampaignThresholdEvaluation AS threshold_evaluation
  WHERE threshold_evaluation.organization_id = draft.organization_id
    AND threshold_evaluation.campaign_run_id = run.id
) AS thresholds ON true
LEFT JOIN LATERAL (
  SELECT jsonb_agg(
    jsonb_build_object(
      'id', control_request.id,
      'requestId', control_request.request_id,
      'requestedAt', control_request.requested_at,
      'memberRunId', control_request.member_run_id,
      'kind', control_request.control_kind,
      'reason', control_request.reason,
      'requestChecksum', control_request.request_checksum,
      'status', control_request.status,
      'resultingRunVersion', control_request.resulting_run_version
    ) ORDER BY control_request.requested_at DESC, control_request.id DESC
  ) AS items
  FROM CampaignControlRequest AS control_request
  WHERE control_request.organization_id = draft.organization_id
    AND control_request.campaign_run_id = run.id
) AS controls ON true
WHERE draft.organization_id = @organization_id
  AND draft.id = @campaign_id
  AND (
    @organization_scope
    OR draft.id = ANY(@campaign_scope_ids::uuid[])
    OR EXISTS (
      SELECT 1
      FROM DeploymentCampaignMember AS scope_member
      JOIN DeploymentPlan AS scope_plan
        ON scope_plan.organization_id = draft.organization_id
       AND scope_plan.id = scope_member.deployment_plan_id
      JOIN DeploymentUnit AS scope_unit
        ON scope_unit.organization_id = draft.organization_id
       AND scope_unit.id = scope_member.deployment_unit_id
      JOIN TargetEnvironmentAssignment AS scope_assignment
        ON scope_assignment.organization_id = draft.organization_id
       AND scope_assignment.id = scope_unit.target_environment_assignment_id
      JOIN DeploymentScope AS deployment_scope
        ON deployment_scope.organization_id = draft.organization_id
       AND deployment_scope.id = scope_unit.deployment_scope_id
      WHERE scope_member.organization_id = draft.organization_id
        AND scope_member.campaign_revision_id = revision.id
        AND (
          scope_plan.environment_id = ANY(@environment_scope_ids::uuid[])
          OR scope_unit.id = ANY(@deployment_unit_scope_ids::uuid[])
          OR deployment_scope.customer_organization_id = ANY(@customer_scope_ids::uuid[])
          OR EXISTS (
            SELECT 1
            FROM DeploymentUnitSubscriber AS subscriber
            WHERE subscriber.organization_id = draft.organization_id
              AND subscriber.deployment_unit_id = scope_unit.id
              AND subscriber.retired_at IS NULL
              AND subscriber.customer_organization_id = ANY(@customer_scope_ids::uuid[])
          )
          OR EXISTS (
            SELECT 1
            FROM DeploymentPlanTargetComponent AS scoped_component
            JOIN ComponentDefinition AS component_definition
              ON component_definition.organization_id = draft.organization_id
             AND component_definition.key = scoped_component.component
            WHERE scoped_component.organization_id = draft.organization_id
              AND scoped_component.deployment_plan_id = scope_plan.id
              AND component_definition.id = ANY(@component_scope_ids::uuid[])
          )
        )
    )
  )`

type operatorCampaignWaveProjection struct {
	ID                 uuid.UUID `json:"id"`
	Order              int       `json:"order"`
	Name               string    `json:"name"`
	BakeSeconds        int       `json:"bakeSeconds"`
	MaximumConcurrency int       `json:"maximumConcurrency"`
	Status             *string   `json:"status"`
}

type operatorCampaignMemberProjection struct {
	ID                      uuid.UUID  `json:"id"`
	DeploymentPlanID        uuid.UUID  `json:"planId"`
	DeploymentUnitID        uuid.UUID  `json:"deploymentUnitId"`
	WaveOrder               int        `json:"waveOrder"`
	MemberOrder             int        `json:"memberOrder"`
	Status                  *string    `json:"status"`
	PlanChecksum            string     `json:"planChecksum"`
	EffectivePolicyChecksum string     `json:"effectivePolicyChecksum"`
	ApprovalChecksum        string     `json:"approvalChecksum"`
	CalendarChecksums       []string   `json:"calendarChecksums"`
	AdmissionChecksum       string     `json:"admissionChecksum"`
	RunID                   *uuid.UUID `json:"runId"`
	ExecutionUncertain      bool       `json:"executionUncertain"`
	ActiveStepsCancellable  bool       `json:"activeStepsCancellable"`
}

type operatorCampaignPrerequisiteProjection struct {
	ID                           uuid.UUID  `json:"id"`
	DownstreamPlanID             uuid.UUID  `json:"downstreamPlanId"`
	UpstreamPlanID               uuid.UUID  `json:"upstreamPlanId"`
	UpstreamStepKey              string     `json:"upstreamStepKey"`
	ExpectedRuntimeStateChecksum string     `json:"expectedRuntimeStateChecksum"`
	EvaluationID                 *uuid.UUID `json:"evaluationId"`
	ActualObservationID          *uuid.UUID `json:"actualObservationId"`
	ActualRuntimeStateChecksum   *string    `json:"actualRuntimeStateChecksum"`
	Matched                      *bool      `json:"matched"`
	Reason                       *string    `json:"reason"`
}

type operatorCampaignThresholdProjection struct {
	ID                 uuid.UUID `json:"id"`
	EvaluatedAt        time.Time `json:"evaluatedAt"`
	Samples            int       `json:"samples"`
	Successful         int       `json:"successful"`
	Failed             int       `json:"failed"`
	FailureRate        float64   `json:"failureRate"`
	MaximumFailureRate float64   `json:"maximumFailureRate"`
	Breached           bool      `json:"breached"`
	FencingToken       int64     `json:"fencingToken"`
}

type operatorCampaignControlProjection struct {
	ID                  uuid.UUID  `json:"id"`
	RequestID           uuid.UUID  `json:"requestId"`
	RequestedAt         time.Time  `json:"requestedAt"`
	MemberRunID         *uuid.UUID `json:"memberRunId"`
	Kind                string     `json:"kind"`
	Reason              string     `json:"reason"`
	RequestChecksum     string     `json:"requestChecksum"`
	Status              string     `json:"status"`
	ResultingRunVersion int64      `json:"resultingRunVersion"`
}

type operatorCampaignDetailRecord struct {
	DraftID              uuid.UUID
	CreatedAt            time.Time
	Name                 string
	RevisionID           *uuid.UUID
	PublishedAt          *time.Time
	RevisionChecksum     string
	RunID                *uuid.UUID
	RunState             *string
	AdmissionsBlocked    bool
	ReconciliationNeeded bool
	Waves                []operatorCampaignWaveProjection
	Members              []operatorCampaignMemberProjection
	Prerequisites        []operatorCampaignPrerequisiteProjection
	Thresholds           []operatorCampaignThresholdProjection
	Controls             []operatorCampaignControlProjection
	WavesJSON            []byte
	MembersJSON          []byte
	PrerequisitesJSON    []byte
	ThresholdsJSON       []byte
	ControlsJSON         []byte
}

func ListOperatorCampaigns(
	ctx context.Context,
	filter types.CampaignFilter,
	limit int,
	cursorCreatedAt *time.Time,
	cursorID *uuid.UUID,
) ([]types.OperatorCampaignRow, error) {
	if limit < 1 || limit > types.OperatorMaximumPageLimit ||
		(cursorCreatedAt == nil) != (cursorID == nil) {
		return nil, apierrors.ErrBadRequest
	}
	scopes, err := normalizeOperatorCampaignScopes(filter.OperatorScopeFilter)
	if err != nil {
		return nil, err
	}
	if scopes.Empty() {
		return []types.OperatorCampaignRow{}, nil
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, listOperatorCampaignsSQL, pgx.NamedArgs{
		"organization_id":           filter.OrganizationID,
		"organization_scope":        scopes.OrganizationWide,
		"customer_scope_ids":        scopes.CustomerIDs,
		"environment_scope_ids":     scopes.EnvironmentIDs,
		"deployment_unit_scope_ids": scopes.DeploymentUnitIDs,
		"component_scope_ids":       scopes.ComponentIDs,
		"campaign_scope_ids":        scopes.CampaignIDs,
		"status":                    filter.Status,
		"environment_id":            filter.EnvironmentID,
		"deployment_plan_id":        filter.DeploymentPlanID,
		"cursor_created_at":         cursorCreatedAt,
		"cursor_id":                 cursorID,
		"limit_plus_one":            limit + 1,
	})
	if err != nil {
		return nil, fmt.Errorf("list operator campaigns: %w", err)
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.OperatorCampaignRow])
	if err != nil {
		return nil, fmt.Errorf("collect operator campaigns: %w", err)
	}
	return items, nil
}

func GetOperatorCampaign(
	ctx context.Context,
	campaignID uuid.UUID,
	filter types.CampaignFilter,
) (*types.OperatorCampaignDetail, error) {
	if campaignID == uuid.Nil {
		return nil, apierrors.ErrNotFound
	}
	scopes, err := normalizeOperatorCampaignScopes(filter.OperatorScopeFilter)
	if err != nil {
		return nil, err
	}
	if scopes.Empty() {
		return nil, apierrors.ErrNotFound
	}

	record, err := scanOperatorCampaignDetail(internalctx.GetDb(ctx).QueryRow(
		ctx,
		getOperatorCampaignSQL,
		pgx.NamedArgs{
			"organization_id":           filter.OrganizationID,
			"campaign_id":               campaignID,
			"organization_scope":        scopes.OrganizationWide,
			"customer_scope_ids":        scopes.CustomerIDs,
			"environment_scope_ids":     scopes.EnvironmentIDs,
			"deployment_unit_scope_ids": scopes.DeploymentUnitIDs,
			"component_scope_ids":       scopes.ComponentIDs,
			"campaign_scope_ids":        scopes.CampaignIDs,
		},
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get operator campaign: %w", err)
	}
	return buildOperatorCampaignDetail(record), nil
}

func normalizeOperatorCampaignScopes(
	filter types.OperatorScopeFilter,
) (operatorCampaignScopeValues, error) {
	if err := validateCanonicalOperatorScopeFilter(filter); err != nil {
		return operatorCampaignScopeValues{}, err
	}
	values := operatorCampaignScopeValues{
		OrganizationID: filter.OrganizationID, OrganizationWide: filter.OrganizationWide,
		CustomerIDs: filter.CustomerIDs, EnvironmentIDs: filter.EnvironmentIDs,
		DeploymentUnitIDs: filter.DeploymentUnitIDs, ComponentIDs: filter.ComponentIDs,
		CampaignIDs: filter.CampaignIDs,
	}
	return values, nil
}

type operatorCampaignScopeValues struct {
	OrganizationID    uuid.UUID
	OrganizationWide  bool
	CustomerIDs       []uuid.UUID
	EnvironmentIDs    []uuid.UUID
	DeploymentUnitIDs []uuid.UUID
	ComponentIDs      []uuid.UUID
	CampaignIDs       []uuid.UUID
}

func (values operatorCampaignScopeValues) Empty() bool {
	return !values.OrganizationWide && values.EmptyNarrow()
}

func (values operatorCampaignScopeValues) EmptyNarrow() bool {
	return len(values.CustomerIDs) == 0 && len(values.EnvironmentIDs) == 0 &&
		len(values.DeploymentUnitIDs) == 0 && len(values.ComponentIDs) == 0 &&
		len(values.CampaignIDs) == 0
}

func operatorCampaignStatus(revisionID *uuid.UUID, runState *string) string {
	if revisionID == nil {
		return "DRAFT"
	}
	if runState == nil {
		return "PUBLISHED"
	}
	if *runState == "" {
		return "UNKNOWN"
	}
	return *runState
}

func scanOperatorCampaignDetail(row pgx.Row) (operatorCampaignDetailRecord, error) {
	var record operatorCampaignDetailRecord
	var updatedAt time.Time
	var description string
	var draftRevision int64
	var membershipJSON, draftWavesJSON, draftPrerequisitesJSON, draftRiskPolicyJSON []byte
	var revisionNumber, sourceDraftRevision *int64
	var membershipTagQuery *string
	var frozenRiskPolicyJSON []byte
	var runCreatedAt, runUpdatedAt *time.Time
	var runVersion, fencingToken *int64
	var currentWaveOrder, currentMemberOrder *int
	var pauseRequested *bool
	var admissionsBlocked, reconciliationRequired *bool
	err := row.Scan(
		&record.DraftID,
		&record.CreatedAt,
		&updatedAt,
		&record.Name,
		&description,
		&draftRevision,
		&membershipJSON,
		&draftWavesJSON,
		&draftPrerequisitesJSON,
		&draftRiskPolicyJSON,
		&record.RevisionID,
		&revisionNumber,
		&sourceDraftRevision,
		&record.PublishedAt,
		&membershipTagQuery,
		&frozenRiskPolicyJSON,
		&record.RevisionChecksum,
		&record.RunID,
		&runCreatedAt,
		&runUpdatedAt,
		&record.RunState,
		&runVersion,
		&currentWaveOrder,
		&currentMemberOrder,
		&admissionsBlocked,
		&pauseRequested,
		&reconciliationRequired,
		&fencingToken,
		&record.WavesJSON,
		&record.MembersJSON,
		&record.PrerequisitesJSON,
		&record.ThresholdsJSON,
		&record.ControlsJSON,
	)
	if err != nil {
		return record, err
	}
	record.AdmissionsBlocked = admissionsBlocked != nil && *admissionsBlocked
	record.ReconciliationNeeded = reconciliationRequired != nil && *reconciliationRequired
	for raw, destination := range map[string]any{
		string(record.WavesJSON):         &record.Waves,
		string(record.MembersJSON):       &record.Members,
		string(record.PrerequisitesJSON): &record.Prerequisites,
		string(record.ThresholdsJSON):    &record.Thresholds,
		string(record.ControlsJSON):      &record.Controls,
	} {
		if err := json.Unmarshal([]byte(raw), destination); err != nil {
			return record, fmt.Errorf("decode operator campaign detail: %w", err)
		}
	}
	return record, nil
}

func buildOperatorCampaignDetail(record operatorCampaignDetailRecord) *types.OperatorCampaignDetail {
	detail := &types.OperatorCampaignDetail{
		Campaign: types.OperatorCampaignRow{
			ID: record.DraftID, CreatedAt: record.CreatedAt, DraftID: record.DraftID,
			RevisionID: record.RevisionID, RunID: record.RunID, Name: record.Name,
			Status:            operatorCampaignStatus(record.RevisionID, record.RunState),
			CanonicalChecksum: record.RevisionChecksum,
		},
		RevisionChecksum:     record.RevisionChecksum,
		MembershipChecksum:   checksumOperatorProjection(record.MembersJSON),
		PrerequisiteChecksum: checksumOperatorProjection(record.PrerequisitesJSON),
		ThresholdChecksum:    checksumOperatorProjection(record.ThresholdsJSON),
		ControlChecksum:      checksumOperatorProjection(record.ControlsJSON),
		AdmissionChecksum:    checksumCampaignAdmissions(record.Members),
		Waves:                []types.OperatorCampaignWave{},
		Members:              []types.OperatorCampaignMember{},
		Prerequisites:        []types.OperatorPlanFact{},
		Thresholds:           []types.OperatorPlanFact{},
		Controls:             []types.OperatorPlanFact{},
		UncertaintyBlockers:  []types.OperatorPlanFact{},
		AdmissionBlockers:    []types.OperatorPlanFact{},
		Evidence:             []types.OperatorEvidenceRef{},
	}
	waveIndexes := make(map[int]int, len(record.Waves))
	for _, wave := range record.Waves {
		status := "PENDING"
		if wave.Status != nil && *wave.Status != "" {
			status = *wave.Status
		}
		waveIndexes[wave.Order] = len(detail.Waves)
		detail.Waves = append(detail.Waves, types.OperatorCampaignWave{
			ID: wave.ID, Order: wave.Order, Name: wave.Name, Status: status,
			BakeSeconds: wave.BakeSeconds, MaximumConcurrency: wave.MaximumConcurrency,
		})
	}
	for _, member := range record.Members {
		status := "PENDING"
		if member.Status != nil && *member.Status != "" {
			status = *member.Status
		}
		detail.Members = append(detail.Members, types.OperatorCampaignMember{
			ID: member.ID, DeploymentPlanID: member.DeploymentPlanID,
			DeploymentUnitID: member.DeploymentUnitID, WaveOrder: member.WaveOrder,
			MemberOrder: member.MemberOrder, Status: status, PlanChecksum: member.PlanChecksum,
		})
		detail.Campaign.MemberCount++
		switch status {
		case "PENDING", "ADMITTED":
			detail.Campaign.PendingCount++
		case "RUNNING":
			detail.Campaign.RunningCount++
		case "SUCCEEDED":
			detail.Campaign.SucceededCount++
		case "FAILED":
			detail.Campaign.FailedCount++
		}
		if index, ok := waveIndexes[member.WaveOrder]; ok {
			detail.Waves[index].MemberCount++
			switch status {
			case "SUCCEEDED":
				detail.Waves[index].SucceededCount++
			case "FAILED":
				detail.Waves[index].FailedCount++
			}
		}
		if member.ExecutionUncertain {
			detail.UncertaintyBlockers = append(detail.UncertaintyBlockers, types.OperatorPlanFact{
				ID: &member.ID, Key: member.DeploymentPlanID.String(), Kind: "member",
				Status: "UNKNOWN", Checksum: member.PlanChecksum,
				Message: "execution outcome is uncertain", Blocking: true, Order: member.MemberOrder,
			})
		}
	}
	detail.Campaign.WaveCount = len(detail.Waves)
	appendCampaignPrerequisites(detail, record.Prerequisites)
	appendCampaignThresholds(detail, record.Thresholds)
	appendCampaignControls(detail, record.Controls)
	if record.AdmissionsBlocked {
		detail.AdmissionBlockers = append(detail.AdmissionBlockers, types.OperatorPlanFact{
			Key: "campaign-admissions", Kind: "admission", Status: "BLOCKED",
			Message: "campaign admissions are blocked", Blocking: true,
		})
	}
	if record.ReconciliationNeeded {
		detail.UncertaintyBlockers = append(detail.UncertaintyBlockers, types.OperatorPlanFact{
			Key: "campaign-reconciliation", Kind: "reconciliation", Status: "REQUIRED",
			Message: "campaign reconciliation is required", Blocking: true,
		})
	}
	detail.Campaign.BlockedCount = len(detail.AdmissionBlockers) + len(detail.UncertaintyBlockers)
	appendCampaignEvidence(detail, record)
	return detail
}

func appendCampaignPrerequisites(
	detail *types.OperatorCampaignDetail,
	items []operatorCampaignPrerequisiteProjection,
) {
	for index, item := range items {
		status := "UNKNOWN"
		blocking := true
		actual := ""
		message := "prerequisite has not been evaluated"
		if item.Matched != nil {
			if *item.Matched {
				status, blocking = "SATISFIED", false
			} else {
				status = "BLOCKED"
			}
			if item.Reason != nil {
				message = *item.Reason
			}
		}
		if item.ActualRuntimeStateChecksum != nil {
			actual = *item.ActualRuntimeStateChecksum
		}
		fact := types.OperatorPlanFact{
			ID: &item.ID, Key: item.UpstreamStepKey, Kind: "prerequisite", Status: status,
			Expected: item.ExpectedRuntimeStateChecksum, Actual: actual,
			Checksum: item.ExpectedRuntimeStateChecksum, Message: message,
			Blocking: blocking, Order: index,
		}
		detail.Prerequisites = append(detail.Prerequisites, fact)
		if blocking {
			detail.AdmissionBlockers = append(detail.AdmissionBlockers, fact)
		}
	}
}

func appendCampaignThresholds(
	detail *types.OperatorCampaignDetail,
	items []operatorCampaignThresholdProjection,
) {
	for index, item := range items {
		status := "SATISFIED"
		if item.Breached {
			status = "BREACHED"
		}
		fact := types.OperatorPlanFact{
			ID: &item.ID, Key: fmt.Sprintf("threshold-%d", item.FencingToken),
			Kind: "threshold", Status: status,
			Expected: fmt.Sprintf("failureRate<=%.6f", item.MaximumFailureRate),
			Actual:   fmt.Sprintf("failureRate=%.6f samples=%d", item.FailureRate, item.Samples),
			Blocking: item.Breached, Order: index,
		}
		detail.Thresholds = append(detail.Thresholds, fact)
		if item.Breached {
			detail.AdmissionBlockers = append(detail.AdmissionBlockers, fact)
		}
	}
}

func appendCampaignControls(
	detail *types.OperatorCampaignDetail,
	items []operatorCampaignControlProjection,
) {
	for index, item := range items {
		detail.Controls = append(detail.Controls, types.OperatorPlanFact{
			ID: &item.ID, Key: item.Kind, Kind: "control", Status: item.Status,
			Checksum: item.RequestChecksum, Message: item.Reason,
			Blocking: item.Status == "PENDING_SAFE_POINT" || item.Status == "PENDING_RECONCILIATION",
			Order:    index,
		})
	}
}

func appendCampaignEvidence(
	detail *types.OperatorCampaignDetail,
	record operatorCampaignDetailRecord,
) {
	if record.RevisionID != nil && record.PublishedAt != nil {
		detail.Evidence = append(detail.Evidence, types.OperatorEvidenceRef{
			ID: *record.RevisionID, Kind: "campaign_revision", Label: "Campaign revision",
			Href:     "/api/v1/control-plane/campaigns/" + record.DraftID.String(),
			Checksum: record.RevisionChecksum, CreatedAt: *record.PublishedAt,
		})
	}
	for _, member := range record.Members {
		detail.Evidence = append(detail.Evidence, types.OperatorEvidenceRef{
			ID: member.DeploymentPlanID, Kind: "deployment_plan", Label: "Deployment plan",
			Href:     "/api/v1/control-plane/plans/" + member.DeploymentPlanID.String(),
			Checksum: member.PlanChecksum, CreatedAt: record.CreatedAt,
		})
	}
}

func checksumCampaignAdmissions(items []operatorCampaignMemberProjection) string {
	checksums := make([]string, 0, len(items))
	for _, item := range items {
		checksums = append(checksums, item.AdmissionChecksum)
	}
	payload, _ := json.Marshal(checksums)
	return checksumOperatorProjection(payload)
}

func checksumOperatorProjection(payload []byte) string {
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}
