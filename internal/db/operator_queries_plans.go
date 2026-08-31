package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const operatorPlanVisibleScopeSQL = `
	AND (
	  @organizationWide
	  OR dp.environment_id = ANY(@environmentIDs::uuid[])
	  OR dp.deployment_unit_id = ANY(@deploymentUnitIDs::uuid[])
	  OR EXISTS (
	    SELECT 1
	    FROM DeploymentUnit unit
	    JOIN DeploymentScope scope
	      ON scope.id = unit.deployment_scope_id
	     AND scope.organization_id = unit.organization_id
	    LEFT JOIN DeploymentUnitSubscriber subscriber
	      ON subscriber.deployment_unit_id = unit.id
	     AND subscriber.organization_id = unit.organization_id
	     AND subscriber.retired_at IS NULL
	    WHERE unit.organization_id = dp.organization_id
	      AND unit.id = dp.deployment_unit_id
	      AND (
	        scope.customer_organization_id = ANY(@customerIDs::uuid[])
	        OR subscriber.customer_organization_id = ANY(@customerIDs::uuid[])
	      )
	  )
	  OR EXISTS (
	    SELECT 1
	    FROM ComponentInstance instance
	    WHERE instance.organization_id = dp.organization_id
	      AND instance.deployment_unit_id = dp.deployment_unit_id
	      AND instance.component_definition_id = ANY(@componentIDs::uuid[])
	      AND instance.retired_at IS NULL
	  )
	  OR EXISTS (
	    SELECT 1
	    FROM DeploymentCampaignMember member
	    JOIN DeploymentCampaignRevision revision
	      ON revision.id = member.campaign_revision_id
	     AND revision.organization_id = member.organization_id
	    WHERE member.organization_id = dp.organization_id
	      AND member.deployment_plan_id = dp.id
	      AND revision.deployment_campaign_draft_id = ANY(@campaignIDs::uuid[])
	  )
	)`

const operatorPlanListSQL = `
	SELECT
	  dp.id,
	  dp.created_at,
	  dp.status,
	  dp.plan_schema,
	  dp.protocol_version,
	  dp.release_bundle_id AS product_release_id,
	  release.release_number AS product_release_version,
	  dp.environment_id,
	  environment.name AS environment,
	  dp.deployment_unit_id,
	  COALESCE(unit.name, '') AS deployment_unit,
	  dp.target_config_snapshot_id,
	  dp.canonical_checksum,
	  dp.bootstrap,
	  (SELECT count(*) FROM DeploymentPlanTarget target
	    WHERE target.organization_id = dp.organization_id
	      AND target.deployment_plan_id = dp.id) AS target_count,
	  (SELECT count(*) FROM DeploymentPlanStep step
	    WHERE step.organization_id = dp.organization_id
	      AND step.deployment_plan_id = dp.id) AS step_count,
	  (SELECT count(*) FROM DeploymentPlanIssue issue
	    WHERE issue.organization_id = dp.organization_id
	      AND issue.deployment_plan_id = dp.id) AS issue_count,
	  (SELECT count(*) FROM DeploymentPlanIssue issue
	    WHERE issue.organization_id = dp.organization_id
	      AND issue.deployment_plan_id = dp.id
	      AND issue.severity = 'blocker') AS blocking_issue_count,
	  (SELECT count(*) FROM ApprovalRequest approval
	    WHERE approval.organization_id = dp.organization_id
	      AND approval.subject_id = dp.id
	      AND approval.subject_type = 'deployment_plan'
	      AND approval.state <> 'APPROVED') AS approval_blocker_count,
	  (SELECT count(*) FROM DeploymentPreflightCheck preflight
	    WHERE preflight.organization_id = dp.organization_id
	      AND preflight.deployment_plan_id = dp.id
	      AND preflight.status = 'FAILED') AS preflight_blocker_count
	FROM DeploymentPlan dp
	JOIN ReleaseBundle release
	  ON release.id = dp.release_bundle_id
	 AND release.organization_id = dp.organization_id
	JOIN Environment environment
	  ON environment.id = dp.environment_id
	 AND environment.organization_id = dp.organization_id
	LEFT JOIN DeploymentUnit unit
	  ON unit.id = dp.deployment_unit_id
	 AND unit.organization_id = dp.organization_id
	WHERE dp.organization_id = @organizationID
	  AND (@status = '' OR dp.status = @status)
	  AND (@environmentID::uuid IS NULL OR dp.environment_id = @environmentID)
	  AND (@deploymentUnitID::uuid IS NULL OR dp.deployment_unit_id = @deploymentUnitID)
	  AND (@productReleaseID::uuid IS NULL OR dp.release_bundle_id = @productReleaseID)
` + operatorPlanVisibleScopeSQL + `
	  AND (
	    @cursorCreatedAt::timestamptz IS NULL
	    OR (dp.created_at, dp.id) <
	      (@cursorCreatedAt::timestamptz, @cursorID::uuid)
	  )
	ORDER BY dp.created_at DESC, dp.id DESC
	LIMIT @limitPlusOne`

const operatorPlanDetailIdentitySQL = `
	SELECT dp.id
	FROM DeploymentPlan dp
	WHERE dp.id = @planID
	  AND dp.organization_id = @organizationID
` + operatorPlanVisibleScopeSQL

const operatorPlanDetailRowSQL = `
	SELECT
	  dp.id, dp.created_at, dp.status, dp.plan_schema, dp.protocol_version,
	  dp.release_bundle_id AS product_release_id,
	  release.release_number AS product_release_version,
	  dp.environment_id, environment.name AS environment,
	  dp.deployment_unit_id, COALESCE(unit.name, '') AS deployment_unit,
	  dp.target_config_snapshot_id, dp.canonical_checksum,
	  (SELECT count(*) FROM DeploymentPlanTarget target
	    WHERE target.organization_id = dp.organization_id
	      AND target.deployment_plan_id = dp.id) AS target_count,
	  (SELECT count(*) FROM DeploymentPlanStep step
	    WHERE step.organization_id = dp.organization_id
	      AND step.deployment_plan_id = dp.id) AS step_count,
	  (SELECT count(*) FROM DeploymentPlanIssue issue
	    WHERE issue.organization_id = dp.organization_id
	      AND issue.deployment_plan_id = dp.id) AS issue_count,
	  (SELECT count(*) FROM DeploymentPlanIssue issue
	    WHERE issue.organization_id = dp.organization_id
	      AND issue.deployment_plan_id = dp.id
	      AND issue.severity = 'blocker') AS blocking_issue_count,
	  (SELECT count(*) FROM ApprovalRequest approval
	    WHERE approval.organization_id = dp.organization_id
	      AND approval.subject_id = dp.id
	      AND approval.subject_type = 'deployment_plan'
	      AND approval.state <> 'APPROVED') AS approval_blocker_count,
	  (SELECT count(*) FROM DeploymentPreflightCheck preflight
	    WHERE preflight.organization_id = dp.organization_id
	      AND preflight.deployment_plan_id = dp.id
	      AND preflight.status = 'FAILED') AS preflight_blocker_count,
	  dp.bootstrap,
	  release.canonical_checksum AS product_release_checksum,
	  COALESCE(config.canonical_checksum, '') AS target_config_checksum
	FROM DeploymentPlan dp
	JOIN ReleaseBundle release
	  ON release.id = dp.release_bundle_id
	 AND release.organization_id = dp.organization_id
	JOIN Environment environment
	  ON environment.id = dp.environment_id
	 AND environment.organization_id = dp.organization_id
	LEFT JOIN DeploymentUnit unit
	  ON unit.id = dp.deployment_unit_id
	 AND unit.organization_id = dp.organization_id
	LEFT JOIN TargetConfigSnapshot config
	  ON config.id = dp.target_config_snapshot_id
	 AND config.organization_id = dp.organization_id
	WHERE dp.id = @planID
	  AND dp.organization_id = @organizationID`

const operatorPlanApprovalSQL = `
	SELECT
	  request.id, request.created_at, request.state, request.subject_checksum,
	  request.effective_policy_checksum, request.subscriber_set_checksum,
	  request.expires_at, request.revision
	FROM ApprovalRequest request
	WHERE request.organization_id = @organizationID
	  AND request.subject_type = 'deployment_plan'
	  AND request.subject_id = @planID
	ORDER BY request.created_at, request.id`

const operatorPlanAdmissionSQL = `
	SELECT
	  e.id, e.created_at, e.decision, e.reason_codes, e.evaluated_at,
	  e.temporal_evidence, e.gate_evidence, e.material_checksum,
	  e.decision_checksum
	FROM AdmissionEvaluation e
	WHERE e.organization_id = @organizationID
	  AND e.deployment_plan_id = @planID
	ORDER BY e.created_at, e.id`

const operatorPlanIntentEvidenceSQL = `
	SELECT
	  intent.id,
	  intent.created_at,
	  intent.checksum,
	  attempt.execution_id,
	  attempt.step_key,
	  attempt.status
	FROM ExecutionIntent intent
	JOIN ExecutionAttempt attempt
	  ON attempt.id = intent.execution_attempt_id
	 AND attempt.organization_id = intent.organization_id
	JOIN Task task
	  ON task.id = attempt.task_id
	 AND task.organization_id = attempt.organization_id
	WHERE intent.organization_id = @organizationID
	  AND task.deployment_plan_id = @planID
	ORDER BY intent.created_at, intent.id`

const operatorPlanAuditEvidenceSQL = `
	SELECT
	  id, sequence, created_at, event_type,
	  deployment_plan_checksum, approval_checksum, target_config_checksum,
	  product_release_checksum, admission_checksum, execution_checksum
	FROM ControlPlaneAuditEvent
	WHERE organization_id = @organizationID
	  AND deployment_plan_id = @planID
	ORDER BY sequence, id`

const operatorPlanAdditionalDetailQueryCount = 4

func ListOperatorPlans(
	ctx context.Context,
	filter types.OperatorPlanFilter,
	limit int,
	cursorCreatedAt *time.Time,
	cursorID *uuid.UUID,
) ([]types.OperatorPlanRow, error) {
	if limit < 1 || limit > types.OperatorMaximumPageLimit ||
		(cursorCreatedAt == nil) != (cursorID == nil) {
		return nil, apierrors.ErrBadRequest
	}
	if err := validateOperatorPlanScopeFilter(filter.OperatorScopeFilter); err != nil {
		return nil, err
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, operatorPlanListSQL, pgx.NamedArgs{
		"organizationID": filter.OrganizationID, "organizationWide": filter.OrganizationWide,
		"customerIDs": filter.CustomerIDs, "environmentIDs": filter.EnvironmentIDs,
		"deploymentUnitIDs": filter.DeploymentUnitIDs, "componentIDs": filter.ComponentIDs,
		"campaignIDs": filter.CampaignIDs, "status": filter.Status,
		"environmentID": filter.EnvironmentID, "deploymentUnitID": filter.DeploymentUnitID,
		"productReleaseID": filter.ProductReleaseID, "cursorCreatedAt": cursorCreatedAt,
		"cursorID": cursorID, "limitPlusOne": limit + 1,
	})
	if err != nil {
		return nil, fmt.Errorf("list operator plans: %w", err)
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.OperatorPlanRow])
	if err != nil {
		return nil, fmt.Errorf("collect operator plans: %w", err)
	}
	return items, nil
}

func ensureOperatorPlanVisible(
	ctx context.Context,
	scope types.OperatorScopeFilter,
	planID uuid.UUID,
) error {
	if err := validateOperatorPlanScopeFilter(scope); err != nil {
		return err
	}
	var visibleID uuid.UUID
	err := internalctx.GetDb(ctx).QueryRow(ctx, operatorPlanDetailIdentitySQL, pgx.NamedArgs{
		"organizationID": scope.OrganizationID, "planID": planID,
		"organizationWide": scope.OrganizationWide, "customerIDs": scope.CustomerIDs,
		"environmentIDs": scope.EnvironmentIDs, "deploymentUnitIDs": scope.DeploymentUnitIDs,
		"componentIDs": scope.ComponentIDs, "campaignIDs": scope.CampaignIDs,
	}).Scan(&visibleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return apierrors.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("check operator plan visibility: %w", err)
	}
	return nil
}

func validateOperatorPlanScopeFilter(scope types.OperatorScopeFilter) error {
	return validateCanonicalOperatorScopeFilter(scope)
}

type operatorPlanApprovalEvidence struct {
	ID                      uuid.UUID
	CreatedAt               time.Time
	State                   types.ApprovalRequestState
	SubjectChecksum         string
	EffectivePolicyChecksum string
	SubscriberSetChecksum   string
	ExpiresAt               time.Time
	Revision                int64
}

type operatorPlanAdmissionEvidence struct {
	ID               uuid.UUID
	CreatedAt        time.Time
	Decision         types.AdmissionDecision
	ReasonCodes      []types.AdmissionReasonCode
	EvaluatedAt      time.Time
	TemporalEvidence types.AdmissionTemporalEvidence
	GateEvidence     []types.AdmissionGateEvidence
	MaterialChecksum string
	DecisionChecksum string
}

type operatorPlanIntentEvidence struct {
	ID          uuid.UUID
	CreatedAt   time.Time
	Checksum    string
	ExecutionID uuid.UUID
	StepKey     string
	Status      string
}

type operatorPlanAuditEvidence struct {
	ID                     uuid.UUID
	Sequence               int64
	CreatedAt              time.Time
	EventType              string
	DeploymentPlanChecksum string
	ApprovalChecksum       string
	TargetConfigChecksum   string
	ProductReleaseChecksum string
	AdmissionChecksum      string
	ExecutionChecksum      string
}

func GetOperatorPlan(
	ctx context.Context,
	scope types.OperatorScopeFilter,
	planID uuid.UUID,
) (*types.OperatorPlanDetail, error) {
	if planID == uuid.Nil {
		return nil, apierrors.ErrNotFound
	}
	if err := ensureOperatorPlanVisible(ctx, scope, planID); err != nil {
		return nil, err
	}
	row, productChecksum, targetConfigChecksum, err := loadOperatorPlanDetailRow(
		ctx, scope.OrganizationID, planID,
	)
	if err != nil {
		return nil, err
	}
	plan, err := GetDeploymentPlan(ctx, planID, scope.OrganizationID)
	if err != nil {
		return nil, err
	}
	var config *types.TargetConfigSnapshot
	if plan.TargetConfigSnapshotID != nil {
		config, err = GetTargetConfigSnapshot(
			ctx, scope.OrganizationID, *plan.TargetConfigSnapshotID,
		)
		if err != nil {
			return nil, err
		}
	}
	approvals, err := loadOperatorPlanApprovals(ctx, scope.OrganizationID, planID)
	if err != nil {
		return nil, err
	}
	admissions, err := loadOperatorPlanAdmissions(ctx, scope.OrganizationID, planID)
	if err != nil {
		return nil, err
	}
	intents, err := loadOperatorPlanIntents(ctx, scope.OrganizationID, planID)
	if err != nil {
		return nil, err
	}
	audits, err := loadOperatorPlanAudits(ctx, scope.OrganizationID, planID)
	if err != nil {
		return nil, err
	}
	detail := buildOperatorPlanDetail(
		row, *plan, config, productChecksum, targetConfigChecksum,
		approvals, admissions, intents, audits,
	)
	return &detail, nil
}

func loadOperatorPlanDetailRow(
	ctx context.Context,
	organizationID, planID uuid.UUID,
) (types.OperatorPlanRow, string, string, error) {
	var row types.OperatorPlanRow
	var productChecksum, targetConfigChecksum string
	err := internalctx.GetDb(ctx).QueryRow(ctx, operatorPlanDetailRowSQL, pgx.NamedArgs{
		"organizationID": organizationID, "planID": planID,
	}).Scan(
		&row.ID, &row.CreatedAt, &row.Status, &row.PlanSchema, &row.ProtocolVersion,
		&row.ProductReleaseID, &row.ProductReleaseVersion,
		&row.EnvironmentID, &row.Environment,
		&row.DeploymentUnitID, &row.DeploymentUnit,
		&row.TargetConfigSnapshotID, &row.CanonicalChecksum,
		&row.TargetCount, &row.StepCount, &row.IssueCount, &row.BlockingIssueCount,
		&row.ApprovalBlockerCount, &row.PreflightBlockerCount, &row.Bootstrap,
		&productChecksum, &targetConfigChecksum,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, "", "", apierrors.ErrNotFound
	}
	if err != nil {
		return row, "", "", fmt.Errorf("load operator plan row: %w", err)
	}
	return row, productChecksum, targetConfigChecksum, nil
}

func loadOperatorPlanApprovals(
	ctx context.Context,
	organizationID, planID uuid.UUID,
) ([]operatorPlanApprovalEvidence, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, operatorPlanApprovalSQL, pgx.NamedArgs{
		"organizationID": organizationID, "planID": planID,
	})
	if err != nil {
		return nil, fmt.Errorf("load operator plan approvals: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (operatorPlanApprovalEvidence, error) {
		var item operatorPlanApprovalEvidence
		err := row.Scan(
			&item.ID, &item.CreatedAt, &item.State, &item.SubjectChecksum,
			&item.EffectivePolicyChecksum, &item.SubscriberSetChecksum,
			&item.ExpiresAt, &item.Revision,
		)
		return item, err
	})
	if err != nil {
		return nil, fmt.Errorf("collect operator plan approvals: %w", err)
	}
	return items, nil
}

func loadOperatorPlanAdmissions(
	ctx context.Context,
	organizationID, planID uuid.UUID,
) ([]operatorPlanAdmissionEvidence, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, operatorPlanAdmissionSQL, pgx.NamedArgs{
		"organizationID": organizationID, "planID": planID,
	})
	if err != nil {
		return nil, fmt.Errorf("load operator plan admission evidence: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (operatorPlanAdmissionEvidence, error) {
		var item operatorPlanAdmissionEvidence
		var reasons []string
		var temporal, gates []byte
		if err := row.Scan(
			&item.ID, &item.CreatedAt, &item.Decision, &reasons, &item.EvaluatedAt,
			&temporal, &gates, &item.MaterialChecksum, &item.DecisionChecksum,
		); err != nil {
			return item, err
		}
		item.ReasonCodes = make([]types.AdmissionReasonCode, len(reasons))
		for index, reason := range reasons {
			item.ReasonCodes[index] = types.AdmissionReasonCode(reason)
		}
		if err := json.Unmarshal(temporal, &item.TemporalEvidence); err != nil {
			return item, err
		}
		if err := json.Unmarshal(gates, &item.GateEvidence); err != nil {
			return item, err
		}
		return item, nil
	})
	if err != nil {
		return nil, fmt.Errorf("collect operator plan admission evidence: %w", err)
	}
	return items, nil
}

func loadOperatorPlanIntents(
	ctx context.Context,
	organizationID, planID uuid.UUID,
) ([]operatorPlanIntentEvidence, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, operatorPlanIntentEvidenceSQL, pgx.NamedArgs{
		"organizationID": organizationID, "planID": planID,
	})
	if err != nil {
		return nil, fmt.Errorf("load operator plan intents: %w", err)
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByPos[operatorPlanIntentEvidence])
	if err != nil {
		return nil, fmt.Errorf("collect operator plan intents: %w", err)
	}
	return items, nil
}

func loadOperatorPlanAudits(
	ctx context.Context,
	organizationID, planID uuid.UUID,
) ([]operatorPlanAuditEvidence, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, operatorPlanAuditEvidenceSQL, pgx.NamedArgs{
		"organizationID": organizationID, "planID": planID,
	})
	if err != nil {
		return nil, fmt.Errorf("load operator plan audit evidence: %w", err)
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByPos[operatorPlanAuditEvidence])
	if err != nil {
		return nil, fmt.Errorf("collect operator plan audit evidence: %w", err)
	}
	return items, nil
}

func buildOperatorPlanDetail(
	row types.OperatorPlanRow,
	plan types.DeploymentPlan,
	config *types.TargetConfigSnapshot,
	productReleaseChecksum, targetConfigChecksum string,
	approvals []operatorPlanApprovalEvidence,
	admissions []operatorPlanAdmissionEvidence,
	intents []operatorPlanIntentEvidence,
	audits []operatorPlanAuditEvidence,
) types.OperatorPlanDetail {
	detail := types.OperatorPlanDetail{
		Plan: row, ProductReleaseChecksum: productReleaseChecksum,
		TargetConfigChecksum:    targetConfigChecksum,
		EffectivePolicyChecksum: plan.EffectivePolicyChecksum,
		SubscriberSetChecksum:   plan.SubscriberSetChecksum,
		Targets:                 []types.OperatorPlanFact{}, Baselines: []types.OperatorPlanFact{},
		Config: []types.OperatorPlanFact{}, Requirements: []types.OperatorPlanFact{},
		Migrations: []types.OperatorPlanFact{}, Changes: []types.OperatorPlanFact{},
		Risks: []types.OperatorPlanFact{}, Approvals: []types.OperatorPlanFact{},
		Windows: []types.OperatorPlanFact{}, Adapters: []types.OperatorPlanFact{},
		Steps: []types.OperatorPlanFact{}, Edges: []types.OperatorPlanFact{},
		Issues: []types.OperatorPlanFact{}, IntentBlockers: []types.OperatorPlanFact{},
		Evidence: []types.OperatorEvidenceRef{},
	}
	for _, target := range plan.Targets {
		id := target.ID
		detail.Targets = append(detail.Targets, types.OperatorPlanFact{
			ID: &id, Key: target.DeploymentTargetID.String(), Kind: string(target.Type),
			Status: string(target.Platform), Expected: target.Name,
			Checksum: plan.CanonicalChecksum, Order: target.SortOrder,
		})
	}
	for _, baseline := range plan.Baselines {
		id := baseline.ID
		detail.Baselines = append(detail.Baselines, types.OperatorPlanFact{
			ID: &id, Key: baseline.ComponentKey, Kind: string(baseline.Projection),
			Status: baseline.Version, Expected: baseline.DesiredChecksum,
			Actual: baseline.ObservationChecksum, Checksum: baseline.CanonicalChecksum,
			Message: baselineMessage(baseline), Blocking: !baseline.AuthorizesV2Execution,
			Order: baseline.SortOrder,
		})
	}
	appendOperatorPlanConfigFacts(&detail, config)
	for _, requirement := range plan.ResolvedRequirements {
		id := requirement.ID
		detail.Requirements = append(detail.Requirements, types.OperatorPlanFact{
			ID: &id, Key: requirement.RequirementKey, Kind: string(requirement.Mode),
			Expected: requirement.VersionRange, Actual: requirement.ProviderVersion,
			Checksum: requirement.BindingChecksum,
			Message:  requirementProviderEvidenceMessage(requirement), Order: requirement.SortOrder,
		})
	}
	for _, migration := range plan.Migrations {
		id := migration.ID
		detail.Migrations = append(detail.Migrations, types.OperatorPlanFact{
			ID: &id, Key: migration.MigrationID, Kind: string(migration.Phase),
			Expected: migration.ExpectedSourceVersion, Actual: migration.ResultingVersion,
			Checksum: migration.ContractChecksum, Message: migration.OperationalImpact,
			Order: migration.SortOrder,
		})
	}
	for _, change := range plan.Changes {
		id := change.ID
		detail.Changes = append(detail.Changes, types.OperatorPlanFact{
			ID: &id, Key: change.ComponentKey, Kind: string(change.Kind),
			Expected: change.Before, Actual: change.After, Checksum: change.CanonicalChecksum,
			Blocking: change.ForwardOnly, Order: change.SortOrder,
		})
	}
	for _, risk := range plan.Risks {
		id := risk.ID
		fact := types.OperatorPlanFact{
			ID: &id, Key: risk.Code, Kind: string(risk.Level), Checksum: risk.CanonicalChecksum,
			Message: risk.Message, Blocking: risk.Blocking, Order: risk.SortOrder,
		}
		detail.Risks = append(detail.Risks, fact)
		if fact.Blocking {
			detail.IntentBlockers = append(detail.IntentBlockers, fact)
		}
	}
	appendOperatorPlanApprovalFacts(&detail, approvals)
	appendOperatorPlanWindowFacts(&detail, admissions)
	for _, adapter := range plan.StepAdapters {
		id := adapter.ID
		detail.Adapters = append(detail.Adapters, types.OperatorPlanFact{
			ID: &id, Key: adapter.StepKey, Kind: adapter.Capability,
			Status: adapter.ImplementationVersion, Expected: adapter.ScopeReference,
			Checksum: adapter.ConfigChecksum, Order: adapter.SortOrder,
		})
	}
	for _, step := range plan.Steps {
		id := step.ID
		status := "included"
		if !step.Included {
			status = "excluded"
		}
		detail.Steps = append(detail.Steps, types.OperatorPlanFact{
			ID: &id, Key: step.StepKey, Kind: step.ActionType, Status: status,
			Checksum: step.StepInputChecksum, Message: step.ExcludedReason, Order: step.SortOrder,
		})
	}
	for index, edge := range plan.StepEdges {
		id := edge.ID
		detail.Edges = append(detail.Edges, types.OperatorPlanFact{
			ID: &id, Key: edge.Key, Kind: "dependency", Expected: edge.FromStepKey,
			Actual: edge.ToStepKey, Order: index,
		})
	}
	appendOperatorPlanIssueFacts(&detail, plan)
	appendOperatorPlanEvidence(&detail, plan, config, approvals, intents, audits)
	detail.GraphChecksum = checksumOperatorPlanValue(struct {
		Steps []types.OperatorPlanFact `json:"steps"`
		Edges []types.OperatorPlanFact `json:"edges"`
	}{detail.Steps, detail.Edges})
	detail.ChangeChecksum = checksumOperatorPlanValue(detail.Changes)
	detail.BaselineChecksum = checksumOperatorPlanValue(detail.Baselines)
	detail.ProviderResolutionChecksum = checksumOperatorPlanValue(detail.Requirements)
	detail.MigrationChecksum = checksumOperatorPlanValue(detail.Migrations)
	detail.RiskChecksum = checksumOperatorPlanValue(detail.Risks)
	detail.ApprovalChecksum = checksumOperatorPlanValue(detail.Approvals)
	detail.WindowChecksum = checksumOperatorPlanValue(detail.Windows)
	detail.AdapterChecksum = checksumOperatorPlanValue(detail.Adapters)
	detail.IntentChecksum = checksumOperatorPlanValue(intents)
	return detail
}

func requirementProviderEvidenceMessage(requirement types.RequirementResolution) string {
	parts := make([]string, 0, 4)
	if requirement.ObservationFreshUntil != nil {
		parts = append(parts, "observation fresh until "+requirement.ObservationFreshUntil.UTC().Format(time.RFC3339))
	}
	if requirement.ProviderApprovalRequestID != nil {
		parts = append(parts, "provider approval "+requirement.ProviderApprovalRequestID.String()+" @ "+requirement.ProviderApprovalChecksum)
	}
	if requirement.ContractProbeObservationID != nil {
		parts = append(parts, "contract probe "+requirement.ContractProbeObservationID.String()+" @ "+requirement.ContractProbeEvidenceChecksum)
	}
	return strings.Join(parts, "; ")
}

func appendOperatorPlanConfigFacts(
	detail *types.OperatorPlanDetail,
	config *types.TargetConfigSnapshot,
) {
	if config == nil {
		return
	}
	for index, object := range config.Objects {
		id := object.ID
		detail.Config = append(detail.Config, types.OperatorPlanFact{
			ID: &id, Key: object.Key, Kind: string(object.Kind), Expected: object.Reference,
			Actual: object.VersionID, Checksum: object.Checksum, Order: index,
		})
	}
	for index, component := range config.Components {
		id := component.ID
		detail.Config = append(detail.Config, types.OperatorPlanFact{
			ID: &id, Key: component.PhysicalName, Kind: "component-binding",
			Actual: component.ComponentInstanceID.String(), Checksum: config.CanonicalChecksum,
			Order: len(config.Objects) + index,
		})
	}
	for index, secret := range config.SecretReferences {
		id := secret.ID
		detail.Config = append(detail.Config, types.OperatorPlanFact{
			ID: &id, Key: secret.Key, Kind: "secret-reference", Status: secret.Provider,
			Checksum: secret.VersionFingerprint, Message: "opaque reference retained",
			Order: len(config.Objects) + len(config.Components) + index,
		})
	}
	for index, flag := range config.FeatureFlags {
		id := flag.ID
		detail.Config = append(detail.Config, types.OperatorPlanFact{
			ID: &id, Key: flag.Key, Kind: "feature-flag",
			Actual: strconv.FormatBool(flag.Enabled), Checksum: config.CanonicalChecksum,
			Order: len(config.Objects) + len(config.Components) + len(config.SecretReferences) + index,
		})
	}
}

func appendOperatorPlanApprovalFacts(
	detail *types.OperatorPlanDetail,
	approvals []operatorPlanApprovalEvidence,
) {
	for index, approval := range approvals {
		id := approval.ID
		blocking := approval.State != types.ApprovalRequestStateApproved
		fact := types.OperatorPlanFact{
			ID: &id, Key: approval.ID.String(), Kind: "approval-request",
			Status: string(approval.State), Expected: approval.EffectivePolicyChecksum,
			Actual: approval.SubscriberSetChecksum, Checksum: approval.SubjectChecksum,
			Message:  "revision " + strconv.FormatInt(approval.Revision, 10),
			Blocking: blocking, Order: index,
		}
		detail.Approvals = append(detail.Approvals, fact)
		if blocking {
			detail.IntentBlockers = append(detail.IntentBlockers, fact)
		}
	}
}

func appendOperatorPlanWindowFacts(
	detail *types.OperatorPlanDetail,
	admissions []operatorPlanAdmissionEvidence,
) {
	order := 0
	for _, admission := range admissions {
		for _, calendar := range admission.TemporalEvidence.CalendarEvidence {
			id := calendar.VersionID
			blocking := admission.Decision != types.AdmissionDecisionAdmit
			fact := types.OperatorPlanFact{
				ID: &id, Key: id.String(), Kind: "maintenance-window",
				Status: string(calendar.Evaluation.ReasonCode), Checksum: calendar.Checksum,
				Message:  "remaining wait seconds " + strconv.FormatInt(calendar.RemainingWaitSeconds, 10),
				Blocking: blocking, Order: order,
			}
			order++
			detail.Windows = append(detail.Windows, fact)
			if blocking {
				detail.IntentBlockers = append(detail.IntentBlockers, fact)
			}
		}
		for _, freeze := range admission.TemporalEvidence.FreezeEvidence {
			id := freeze.RevisionID
			blocking := admission.Decision != types.AdmissionDecisionAdmit
			fact := types.OperatorPlanFact{
				ID: &id, Key: id.String(), Kind: "deployment-freeze",
				Status: string(freeze.Evaluation.ReasonCode), Checksum: freeze.Checksum,
				Message:  "remaining wait seconds " + strconv.FormatInt(freeze.RemainingWaitSeconds, 10),
				Blocking: blocking, Order: order,
			}
			order++
			detail.Windows = append(detail.Windows, fact)
			if blocking {
				detail.IntentBlockers = append(detail.IntentBlockers, fact)
			}
		}
		for _, gate := range admission.GateEvidence {
			if gate.Satisfied {
				continue
			}
			fact := types.OperatorPlanFact{
				Key: string(gate.Key), Kind: "admission-gate", Status: string(admission.Decision),
				Checksum: gate.Checksum, Blocking: gate.Mandatory, Order: order,
			}
			order++
			detail.Windows = append(detail.Windows, fact)
			if fact.Blocking {
				detail.IntentBlockers = append(detail.IntentBlockers, fact)
			}
		}
	}
}

func appendOperatorPlanIssueFacts(
	detail *types.OperatorPlanDetail,
	plan types.DeploymentPlan,
) {
	for _, issue := range plan.Issues {
		id := issue.ID
		fact := types.OperatorPlanFact{
			ID: &id, Key: issue.Code, Kind: "plan-issue", Status: string(issue.Severity),
			Expected: issue.Field, Message: issue.Message,
			Blocking: issue.Severity == types.DeploymentPlanIssueSeverityBlocker,
			Order:    issue.SortOrder,
		}
		detail.Issues = append(detail.Issues, fact)
		if fact.Blocking {
			detail.IntentBlockers = append(detail.IntentBlockers, fact)
		}
	}
	for _, run := range plan.PreflightRuns {
		for _, check := range run.Checks {
			id := check.ID
			fact := types.OperatorPlanFact{
				ID: &id, Key: check.CheckKey, Kind: "preflight", Status: string(check.Status),
				Expected: stringifyOperatorPlanValue(check.Expected),
				Actual:   stringifyOperatorPlanValue(check.Actual), Message: check.Message,
				Checksum: run.PlanChecksum,
				Blocking: check.Status == types.DeploymentPreflightCheckStatusFailed,
				Order:    check.SortOrder,
			}
			detail.Issues = append(detail.Issues, fact)
			if fact.Blocking {
				detail.IntentBlockers = append(detail.IntentBlockers, fact)
			}
		}
	}
}

func appendOperatorPlanEvidence(
	detail *types.OperatorPlanDetail,
	plan types.DeploymentPlan,
	config *types.TargetConfigSnapshot,
	approvals []operatorPlanApprovalEvidence,
	intents []operatorPlanIntentEvidence,
	audits []operatorPlanAuditEvidence,
) {
	detail.Evidence = append(detail.Evidence, types.OperatorEvidenceRef{
		ID: plan.ID, Kind: "deployment-plan", Label: "Immutable deployment plan",
		Href:     "/api/v1/control-plane/plans/" + plan.ID.String(),
		Checksum: plan.CanonicalChecksum, CreatedAt: plan.CreatedAt,
	})
	detail.Evidence = append(detail.Evidence, types.OperatorEvidenceRef{
		ID: plan.ReleaseBundleID, Kind: "product-release", Label: "Product release",
		Href:     "/api/v1/control-plane/releases/" + plan.ReleaseBundleID.String(),
		Checksum: detail.ProductReleaseChecksum, CreatedAt: plan.CreatedAt,
	})
	if config != nil {
		detail.Evidence = append(detail.Evidence, types.OperatorEvidenceRef{
			ID: config.ID, Kind: "target-config", Label: "Target config snapshot",
			Href:     "/api/v1/target-config-snapshots/" + config.ID.String(),
			Checksum: config.CanonicalChecksum, CreatedAt: config.CreatedAt,
		})
		for _, object := range config.Objects {
			detail.Evidence = append(detail.Evidence, types.OperatorEvidenceRef{
				ID: object.ID, Kind: "config-object", Label: object.Key,
				Href: object.Reference, Checksum: object.Checksum, CreatedAt: config.CreatedAt,
			})
		}
	}
	for _, approval := range approvals {
		detail.Evidence = append(detail.Evidence, types.OperatorEvidenceRef{
			ID: approval.ID, Kind: "approval", Label: "Approval " + string(approval.State),
			Href:     "/api/v1/approval-requests/" + approval.ID.String(),
			Checksum: approval.SubjectChecksum, CreatedAt: approval.CreatedAt,
		})
	}
	for _, intent := range intents {
		detail.Evidence = append(detail.Evidence, types.OperatorEvidenceRef{
			ID: intent.ID, Kind: "execution-intent", Label: intent.StepKey,
			Href:     "/api/v1/control-plane/executions/" + intent.ExecutionID.String(),
			Checksum: intent.Checksum, CreatedAt: intent.CreatedAt,
		})
	}
	for _, audit := range audits {
		detail.Evidence = append(detail.Evidence, types.OperatorEvidenceRef{
			ID: audit.ID, Kind: "audit", Label: audit.EventType,
			Href: "/api/v1/control-plane/audit/" + audit.ID.String(),
			Checksum: firstOperatorPlanChecksum(
				audit.DeploymentPlanChecksum, audit.ApprovalChecksum,
				audit.TargetConfigChecksum, audit.ProductReleaseChecksum,
				audit.AdmissionChecksum, audit.ExecutionChecksum,
			),
			CreatedAt: audit.CreatedAt,
		})
	}
}

func checksumOperatorPlanValue(value any) string {
	payload, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func stringifyOperatorPlanValue(value any) string {
	payload, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(payload)
}

func baselineMessage(baseline types.DeploymentPlanBaseline) string {
	if baseline.ObservedAt == nil {
		return "no verified observation"
	}
	return "verified observation at " + baseline.ObservedAt.UTC().Format(time.RFC3339Nano)
}

func firstOperatorPlanChecksum(values ...string) string {
	for _, value := range values {
		if strings.HasPrefix(value, "sha256:") {
			return value
		}
	}
	return ""
}
