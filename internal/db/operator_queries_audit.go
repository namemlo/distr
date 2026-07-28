package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const operatorAuditSearchSQL = `
WITH filtered AS (
	SELECT
		event.id,
		event.created_at,
		event.sequence,
		event.event_type AS action,
		primary_subject.correlation_kind AS subject_type,
		primary_subject.subject_id,
		event.actor_id AS actor_useraccount_id,
		event.outcome,
		correlations.correlation_count,
		event.payload,
		count(*) OVER() AS total_count
	FROM ControlPlaneAuditEvent event
	JOIN LATERAL (
		SELECT subject.correlation_kind, subject.subject_id
		FROM ControlPlaneAuditEventSubject subject
		WHERE subject.event_id = event.id
		  AND subject.organization_id = event.organization_id
		  AND (@subjectType::text IS NULL OR subject.correlation_kind = @subjectType)
		  AND (@subjectId::uuid IS NULL OR subject.subject_id = @subjectId)
		ORDER BY subject.correlation_kind, subject.subject_id
		LIMIT 1
	) primary_subject ON true
	JOIN LATERAL (
		SELECT count(*)::integer AS correlation_count
		FROM ControlPlaneAuditEventSubject subject_count
		WHERE subject_count.event_id = event.id
		  AND subject_count.organization_id = event.organization_id
	) correlations ON true
	WHERE event.organization_id = @organizationId
	  AND (@action::text IS NULL OR event.event_type = @action)
	  AND (@actorId::uuid IS NULL OR event.actor_id = @actorId)
	  AND (@from::timestamptz IS NULL OR event.created_at >= @from)
	  AND (@to::timestamptz IS NULL OR event.created_at < @to)
	  AND (
		@searchPattern::text IS NULL
		OR event.event_type ILIKE @searchPattern ESCAPE '\'
		OR event.outcome ILIKE @searchPattern ESCAPE '\'
	  )
	  AND (
		@organizationWide
		OR EXISTS (
			SELECT 1
			FROM ControlPlaneAuditEventSubject authorized_subject
			WHERE authorized_subject.event_id = event.id
			  AND authorized_subject.organization_id = event.organization_id
			  AND (
				(authorized_subject.correlation_kind = 'customer_organization'
				 AND authorized_subject.subject_id = ANY(@authorizedCustomerIds::uuid[]))
				OR (authorized_subject.correlation_kind = 'environment'
				 AND authorized_subject.subject_id = ANY(@authorizedEnvironmentIds::uuid[]))
				OR (authorized_subject.correlation_kind = 'deployment_unit'
				 AND authorized_subject.subject_id = ANY(@authorizedDeploymentUnitIds::uuid[]))
				OR (authorized_subject.correlation_kind = 'component'
				 AND authorized_subject.subject_id = ANY(@authorizedComponentIds::uuid[]))
				OR (authorized_subject.correlation_kind = 'campaign_draft'
				 AND authorized_subject.subject_id = ANY(@authorizedCampaignIds::uuid[]))
			  )
		)
	  )
)
SELECT *
FROM filtered event
WHERE @afterCreatedAt::timestamptz IS NULL
   OR event.created_at < @afterCreatedAt
   OR (event.created_at = @afterCreatedAt AND event.id < @afterId)
ORDER BY created_at DESC, id DESC
LIMIT @limit`

const operatorAuditDetailSQL = `
SELECT ` + controlPlaneAuditEventColumns + `
FROM ControlPlaneAuditEvent event
WHERE event.organization_id = @organizationId
  AND event.id = @auditEventId
  AND (
	@organizationWide
	OR EXISTS (
		SELECT 1
		FROM ControlPlaneAuditEventSubject authorized_subject
		WHERE authorized_subject.event_id = event.id
		  AND authorized_subject.organization_id = event.organization_id
		  AND (
			(authorized_subject.correlation_kind = 'customer_organization'
			 AND authorized_subject.subject_id = ANY(@authorizedCustomerIds::uuid[]))
			OR (authorized_subject.correlation_kind = 'environment'
			 AND authorized_subject.subject_id = ANY(@authorizedEnvironmentIds::uuid[]))
			OR (authorized_subject.correlation_kind = 'deployment_unit'
			 AND authorized_subject.subject_id = ANY(@authorizedDeploymentUnitIds::uuid[]))
			OR (authorized_subject.correlation_kind = 'component'
			 AND authorized_subject.subject_id = ANY(@authorizedComponentIds::uuid[]))
			OR (authorized_subject.correlation_kind = 'campaign_draft'
			 AND authorized_subject.subject_id = ANY(@authorizedCampaignIds::uuid[]))
		  )
	)
  )`

func SearchOperatorAuditRows(
	ctx context.Context,
	filter types.AuditFilter,
	afterCreatedAt *time.Time,
	afterID *uuid.UUID,
	limit int,
) ([]types.OperatorAuditRow, *int64, error) {
	scopes, err := operatorSQLScopesFromFilter(filter.OperatorScopeFilter)
	if err != nil {
		return nil, nil, err
	}
	if limit < 1 || limit > types.OperatorMaximumPageLimit+1 ||
		(afterCreatedAt == nil) != (afterID == nil) ||
		(filter.SubjectID != nil && filter.SubjectType == "") {
		return nil, nil, apierrors.ErrBadRequest
	}

	rows, err := internalctx.GetDb(ctx).Query(ctx, operatorAuditSearchSQL, pgx.NamedArgs{
		"organizationId":              filter.OrganizationID,
		"action":                      nullableOperatorString(filter.Action),
		"subjectType":                 nullableOperatorString(filter.SubjectType),
		"subjectId":                   filter.SubjectID,
		"actorId":                     filter.ActorUserAccountID,
		"from":                        filter.From,
		"to":                          filter.To,
		"searchPattern":               operatorAuditSearchPattern(filter.Search),
		"organizationWide":            scopes.organizationWide,
		"authorizedCustomerIds":       scopes.customerIDs,
		"authorizedEnvironmentIds":    scopes.environmentIDs,
		"authorizedDeploymentUnitIds": scopes.deploymentUnitIDs,
		"authorizedComponentIds":      scopes.componentIDs,
		"authorizedCampaignIds":       scopes.campaignIDs,
		"afterCreatedAt":              afterCreatedAt,
		"afterId":                     afterID,
		"limit":                       limit,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("search operator audit rows: %w", err)
	}
	defer rows.Close()

	items := make([]types.OperatorAuditRow, 0, limit)
	var total *int64
	for rows.Next() {
		var item types.OperatorAuditRow
		var payload json.RawMessage
		var rowTotal int64
		if err := rows.Scan(
			&item.ID,
			&item.CreatedAt,
			&item.Sequence,
			&item.Action,
			&item.SubjectType,
			&item.SubjectID,
			&item.ActorUserAccountID,
			&item.Outcome,
			&item.CorrelationCount,
			&payload,
			&rowTotal,
		); err != nil {
			return nil, nil, fmt.Errorf("scan operator audit row: %w", err)
		}
		item.PayloadChecksum = operatorAuditPayloadChecksum(payload)
		if total == nil {
			total = new(rowTotal)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate operator audit rows: %w", err)
	}
	return items, total, nil
}

func GetOperatorAuditDetail(
	ctx context.Context,
	filter types.OperatorScopeFilter,
	auditEventID uuid.UUID,
) (*types.OperatorAuditDetail, error) {
	scopes, err := operatorSQLScopesFromFilter(filter)
	if err != nil {
		return nil, err
	}
	if auditEventID == uuid.Nil {
		return nil, apierrors.ErrBadRequest
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, operatorAuditDetailSQL, pgx.NamedArgs{
		"organizationId":              filter.OrganizationID,
		"auditEventId":                auditEventID,
		"organizationWide":            scopes.organizationWide,
		"authorizedCustomerIds":       scopes.customerIDs,
		"authorizedEnvironmentIds":    scopes.environmentIDs,
		"authorizedDeploymentUnitIds": scopes.deploymentUnitIDs,
		"authorizedComponentIds":      scopes.componentIDs,
		"authorizedCampaignIds":       scopes.campaignIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("query operator audit detail: %w", err)
	}
	event, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.ControlPlaneAuditEvent],
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, apierrors.ErrNotFound
		}
		return nil, fmt.Errorf("scan operator audit detail: %w", err)
	}

	correlations := event.Correlations()
	if len(correlations) == 0 {
		return nil, apierrors.ErrNotFound
	}
	detail := &types.OperatorAuditDetail{
		Event: types.OperatorAuditRow{
			ID: event.ID, CreatedAt: event.CreatedAt, Sequence: event.Sequence,
			Action: event.EventType, SubjectType: string(correlations[0].Kind),
			SubjectID: correlations[0].ID, ActorUserAccountID: event.ActorID,
			Outcome: event.Outcome, CorrelationCount: len(correlations),
			PayloadChecksum: operatorAuditPayloadChecksum(event.Payload),
		},
		Correlations: correlations,
		Payload:      slices.Clone(event.Payload),
		Evidence:     operatorAuditEvidence(event),
	}
	return detail, nil
}

func operatorAuditEvidence(event types.ControlPlaneAuditEvent) []types.OperatorEvidenceRef {
	values := []struct {
		kind     types.AuditCorrelationKind
		id       *uuid.UUID
		checksum string
		label    string
	}{
		{types.AuditCorrelationRelease, event.ReleaseID, event.ReleaseChecksum, "Release"},
		{types.AuditCorrelationComponentRelease, event.ComponentReleaseID, event.ComponentReleaseChecksum, "Component release"},
		{types.AuditCorrelationProductRelease, event.ProductReleaseID, event.ProductReleaseChecksum, "Product release"},
		{types.AuditCorrelationTargetConfig, event.TargetConfigID, event.TargetConfigChecksum, "Target configuration"},
		{types.AuditCorrelationDeploymentPlan, event.DeploymentPlanID, event.DeploymentPlanChecksum, "Deployment plan"},
		{types.AuditCorrelationApproval, event.ApprovalID, event.ApprovalChecksum, "Approval"},
		{types.AuditCorrelationCampaignRevision, event.CampaignRevisionID, event.CampaignRevisionChecksum, "Campaign revision"},
		{types.AuditCorrelationExecution, event.ExecutionID, event.ExecutionChecksum, "Execution"},
		{types.AuditCorrelationDesiredState, event.DesiredStateID, event.DesiredStateChecksum, "Desired state"},
		{types.AuditCorrelationObservation, event.ObservationID, event.ObservationChecksum, "Observation"},
		{types.AuditCorrelationDriftCase, event.DriftCaseID, event.DriftChecksum, "Drift case"},
		{types.AuditCorrelationReconciliation, event.ReconciliationID, event.ReconciliationChecksum, "Reconciliation"},
	}
	evidence := make([]types.OperatorEvidenceRef, 0, len(values))
	for _, value := range values {
		if value.id == nil || value.checksum == "" {
			continue
		}
		evidence = append(evidence, types.OperatorEvidenceRef{
			ID: *value.id, Kind: string(value.kind), Label: value.label,
			Href: "/api/v1/control-plane/audit?subjectType=" + string(value.kind) +
				"&subjectId=" + value.id.String(),
			Checksum: value.checksum, CreatedAt: event.CreatedAt,
		})
	}
	return evidence
}

func operatorAuditSearchPattern(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + replacer.Replace(value) + "%"
}

func operatorAuditPayloadChecksum(payload json.RawMessage) string {
	if len(payload) == 0 {
		return ""
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}
