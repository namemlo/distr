package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
)

const operatorReleaseVisibleScopeSQL = `
  AND (
    @organizationWide
    OR EXISTS (
      SELECT 1
      FROM ComponentReleaseArtifact scoped_artifact
      JOIN ComponentDefinition scoped_component
        ON scoped_component.organization_id = scoped_artifact.organization_id
       AND scoped_component.key = scoped_artifact.component_key
      WHERE scoped_artifact.organization_id = rb.organization_id
        AND scoped_artifact.release_bundle_id = rb.id
        AND scoped_component.id = ANY(@componentScopeIDs::uuid[])
    )
    OR EXISTS (
      SELECT 1
      FROM ProductReleaseComponent scoped_pin
      JOIN ComponentDefinition scoped_component
        ON scoped_component.organization_id = scoped_pin.organization_id
       AND scoped_component.key = scoped_pin.component_key
      WHERE scoped_pin.organization_id = rb.organization_id
        AND scoped_pin.product_release_bundle_id = rb.id
        AND scoped_component.id = ANY(@componentScopeIDs::uuid[])
    )
    OR EXISTS (
      SELECT 1
      FROM DeploymentPlan scoped_plan
      WHERE scoped_plan.organization_id = rb.organization_id
        AND (
          scoped_plan.release_bundle_id = rb.id
          OR EXISTS (
            SELECT 1
            FROM ProductReleaseComponent scoped_plan_pin
            WHERE scoped_plan_pin.organization_id = scoped_plan.organization_id
              AND scoped_plan_pin.product_release_bundle_id = scoped_plan.release_bundle_id
              AND scoped_plan_pin.component_release_bundle_id = rb.id
          )
        )
        AND (
          scoped_plan.environment_id = ANY(@environmentScopeIDs::uuid[])
          OR scoped_plan.deployment_unit_id = ANY(@deploymentUnitScopeIDs::uuid[])
          OR EXISTS (
            SELECT 1
            FROM DeploymentUnit scoped_unit
            JOIN DeploymentScope scoped_deployment_scope
              ON scoped_deployment_scope.id = scoped_unit.deployment_scope_id
             AND scoped_deployment_scope.organization_id = scoped_unit.organization_id
            LEFT JOIN DeploymentUnitSubscriber scoped_subscriber
              ON scoped_subscriber.deployment_unit_id = scoped_unit.id
             AND scoped_subscriber.organization_id = scoped_unit.organization_id
             AND scoped_subscriber.retired_at IS NULL
            WHERE scoped_unit.organization_id = scoped_plan.organization_id
              AND scoped_unit.id = scoped_plan.deployment_unit_id
              AND (
                scoped_deployment_scope.customer_organization_id = ANY(@customerScopeIDs::uuid[])
                OR scoped_subscriber.customer_organization_id = ANY(@customerScopeIDs::uuid[])
              )
          )
          OR EXISTS (
            SELECT 1
            FROM ComponentInstance scoped_instance
            WHERE scoped_instance.organization_id = scoped_plan.organization_id
              AND scoped_instance.deployment_unit_id = scoped_plan.deployment_unit_id
              AND scoped_instance.component_definition_id = ANY(@componentScopeIDs::uuid[])
              AND scoped_instance.retired_at IS NULL
          )
          OR EXISTS (
            SELECT 1
            FROM DeploymentCampaignMember scoped_member
            JOIN DeploymentCampaignRevision scoped_revision
              ON scoped_revision.id = scoped_member.campaign_revision_id
             AND scoped_revision.organization_id = scoped_member.organization_id
            WHERE scoped_member.organization_id = scoped_plan.organization_id
              AND scoped_member.deployment_plan_id = scoped_plan.id
              AND scoped_revision.deployment_campaign_draft_id = ANY(@campaignScopeIDs::uuid[])
          )
        )
    )
  )`

const operatorReleaseProjectionSQL = `
  SELECT
    rb.id,
    rb.organization_id,
    rb.created_at,
    rb.kind,
    rb.application_id,
    NULL::bigint AS release_number,
    rb.release_number AS version,
    rb.status,
    rb.canonical_checksum AS checksum,
    rb.source_revision,
    rb.published_at,
    (SELECT count(DISTINCT artifact.artifact_key)
       FROM ComponentReleaseArtifact artifact
      WHERE artifact.organization_id = rb.organization_id
        AND artifact.release_bundle_id = rb.id) AS artifact_count,
    (SELECT count(*)
       FROM ComponentReleaseEvidence evidence
      WHERE evidence.organization_id = rb.organization_id
        AND evidence.release_bundle_id = rb.id) AS evidence_count,
    (SELECT count(*)
       FROM ProductReleaseComponent pin
      WHERE pin.organization_id = rb.organization_id
        AND pin.product_release_bundle_id = rb.id) AS component_count,
    (SELECT count(*)
       FROM ProductReleaseCapabilityEdge edge
      WHERE edge.organization_id = rb.organization_id
        AND edge.product_release_bundle_id = rb.id) AS graph_edge_count
  FROM ReleaseBundle rb
  WHERE rb.organization_id = @organizationID`

const operatorReleaseListSQL = `
WITH visible_releases AS (` + operatorReleaseProjectionSQL + operatorReleaseVisibleScopeSQL + `
), filtered_releases AS (
  SELECT *
  FROM visible_releases release
  WHERE (@applicationID::uuid IS NULL OR release.application_id = @applicationID)
    AND (@kind = '' OR release.kind = @kind)
    AND (@status = '' OR release.status = @status)
    AND (
      @searchPattern = ''
      OR concat_ws(
        ' ', release.version, release.kind, release.status,
        release.source_revision, release.checksum
      ) ILIKE @searchPattern ESCAPE '\'
    )
), paged_releases AS (
  SELECT *
  FROM filtered_releases release
  WHERE (
    @cursorCreatedAt::timestamp IS NULL
    OR (release.created_at, release.id) <
      (@cursorCreatedAt::timestamp, @cursorID::uuid)
  )
  ORDER BY release.created_at DESC, release.id DESC
  LIMIT @fetchLimit
)
SELECT jsonb_build_object(
  'total', (SELECT count(*) FROM filtered_releases),
  'items', COALESCE((
    SELECT jsonb_agg(
      jsonb_build_object(
        'id', release.id,
        'createdAt', release.created_at,
        'kind', release.kind,
        'applicationId', release.application_id,
        'releaseNumber', release.release_number,
        'version', release.version,
        'status', release.status,
        'checksum', release.checksum,
        'sourceRevision', release.source_revision,
        'publishedAt', release.published_at,
        'artifactCount', release.artifact_count,
        'evidenceCount', release.evidence_count,
        'componentCount', release.component_count,
        'graphEdgeCount', release.graph_edge_count
      ) ORDER BY release.created_at DESC, release.id DESC
    ) FROM paged_releases release
  ), '[]'::jsonb)
)::text`

const operatorReleaseDetailSQL = `
WITH release_row AS (` + operatorReleaseProjectionSQL + `
    AND rb.id = @releaseID
` + operatorReleaseVisibleScopeSQL + `
)
SELECT jsonb_build_object(
  'release', jsonb_build_object(
    'id', release.id,
    'createdAt', release.created_at,
    'kind', release.kind,
    'applicationId', release.application_id,
    'releaseNumber', release.release_number,
    'version', release.version,
    'status', release.status,
    'checksum', release.checksum,
    'sourceRevision', release.source_revision,
    'publishedAt', release.published_at,
    'artifactCount', release.artifact_count,
    'evidenceCount', release.evidence_count,
    'componentCount', release.component_count,
    'graphEdgeCount', release.graph_edge_count
  ),
  'artifacts', COALESCE((
    SELECT jsonb_agg(jsonb_build_object(
      'id', artifact.id,
      'name', artifact.name,
      'version', artifact.version,
      'manifestDigest', artifact.manifest_digest,
      'platformDigests', artifact.platform_digests
    ) ORDER BY artifact.name, artifact.id)
    FROM (
      SELECT
        (array_agg(source.id ORDER BY source.platform, source.id))[1] AS id,
        source.artifact_key AS name,
        min(source.component_version) AS version,
        min(source.manifest_digest) AS manifest_digest,
        jsonb_object_agg(source.platform, source.platform_digest ORDER BY source.platform)
          AS platform_digests
      FROM ComponentReleaseArtifact source
      WHERE source.organization_id = release.organization_id
        AND source.release_bundle_id = release.id
      GROUP BY source.artifact_key
    ) artifact
  ), '[]'::jsonb),
  'componentPins', COALESCE((
    SELECT jsonb_agg(jsonb_build_object(
      'componentReleaseId', pin.component_release_bundle_id,
      'component', pin.component_key,
      'version', pin.component_version,
      'checksum', pin.component_release_checksum,
      'digest', COALESCE((
        SELECT artifact.platform_digest
        FROM ComponentReleaseArtifact artifact
        WHERE artifact.organization_id = pin.organization_id
          AND artifact.release_bundle_id = pin.component_release_bundle_id
        ORDER BY artifact.artifact_key, artifact.platform, artifact.id
        LIMIT 1
      ), '')
    ) ORDER BY pin.component_key, pin.component_release_bundle_id)
    FROM ProductReleaseComponent pin
    WHERE pin.organization_id = release.organization_id
      AND pin.product_release_bundle_id = release.id
  ), '[]'::jsonb),
  'graphEdges', COALESCE((
    SELECT jsonb_agg(jsonb_build_object(
      'from', edge.from_node_key,
      'to', edge.to_node_key,
      'kind', edge.capability_name
    ) ORDER BY edge.edge_key, edge.id)
    FROM ProductReleaseCapabilityEdge edge
    WHERE edge.organization_id = release.organization_id
      AND edge.product_release_bundle_id = release.id
  ), '[]'::jsonb),
  'evidence', COALESCE((
    SELECT jsonb_agg(jsonb_build_object(
      'id', evidence.id,
      'kind', evidence.evidence_type,
      'label', evidence.evidence_type,
      'href', evidence.reference,
      'checksum', COALESCE((
        SELECT verification.evidence_digest
        FROM ComponentReleaseEvidenceVerification verification
        WHERE verification.organization_id = evidence.organization_id
          AND verification.release_bundle_id = evidence.release_bundle_id
          AND verification.evidence_reference = evidence.reference
        ORDER BY verification.created_at DESC, verification.id DESC
        LIMIT 1
      ), ''),
      'createdAt', COALESCE((
        SELECT verification.created_at
        FROM ComponentReleaseEvidenceVerification verification
        WHERE verification.organization_id = evidence.organization_id
          AND verification.release_bundle_id = evidence.release_bundle_id
          AND verification.evidence_reference = evidence.reference
        ORDER BY verification.created_at DESC, verification.id DESC
        LIMIT 1
      ), release.created_at)
    ) ORDER BY evidence.evidence_type, evidence.reference, evidence.id)
    FROM ComponentReleaseEvidence evidence
    WHERE evidence.organization_id = release.organization_id
      AND evidence.release_bundle_id = release.id
  ), '[]'::jsonb)
)::text
FROM release_row release`

type OperatorReleaseCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

type OperatorReleaseQuery struct {
	OrganizationID         uuid.UUID
	DecisionAt             time.Time
	OrganizationWide       bool
	CustomerScopeIDs       []uuid.UUID
	EnvironmentScopeIDs    []uuid.UUID
	DeploymentUnitScopeIDs []uuid.UUID
	ComponentScopeIDs      []uuid.UUID
	CampaignScopeIDs       []uuid.UUID
	ApplicationID          *uuid.UUID
	Kind                   string
	Status                 string
	SearchPattern          string
	Cursor                 *OperatorReleaseCursor
	Limit                  int
}

type OperatorReleaseResult struct {
	Items []types.OperatorReleaseRow
	Total int64
}

type operatorReleasePayload struct {
	Items []types.OperatorReleaseRow `json:"items"`
	Total int64                      `json:"total"`
}

func ListOperatorReleaseRows(
	ctx context.Context,
	query OperatorReleaseQuery,
) (OperatorReleaseResult, error) {
	result := OperatorReleaseResult{Items: []types.OperatorReleaseRow{}}
	if err := validateOperatorReleaseQuery(query); err != nil {
		return result, err
	}
	var cursorCreatedAt any
	var cursorID any
	if query.Cursor != nil {
		cursorCreatedAt = query.Cursor.CreatedAt
		cursorID = query.Cursor.ID
	}
	var payload string
	err := internalctx.GetDb(ctx).QueryRow(ctx, operatorReleaseListSQL, operatorReleaseNamedArgs(
		query, cursorCreatedAt, cursorID,
	)).Scan(&payload)
	if err != nil {
		return result, fmt.Errorf("could not query operator releases: %w", err)
	}
	var decoded operatorReleasePayload
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		return result, fmt.Errorf("could not decode operator releases: %w", err)
	}
	if decoded.Items == nil {
		decoded.Items = []types.OperatorReleaseRow{}
	}
	return OperatorReleaseResult{Items: decoded.Items, Total: decoded.Total}, nil
}

func GetOperatorReleaseDetail(
	ctx context.Context,
	scope types.OperatorScopeFilter,
	releaseID uuid.UUID,
) (*types.OperatorReleaseDetail, error) {
	if releaseID == uuid.Nil || validateOperatorReleaseScope(scope) != nil {
		return nil, apierrors.ErrBadRequest
	}
	query := OperatorReleaseQuery{
		OrganizationID: scope.OrganizationID, DecisionAt: scope.DecisionAt,
		OrganizationWide: scope.OrganizationWide,
		CustomerScopeIDs: scope.CustomerIDs, EnvironmentScopeIDs: scope.EnvironmentIDs,
		DeploymentUnitScopeIDs: scope.DeploymentUnitIDs, ComponentScopeIDs: scope.ComponentIDs,
		CampaignScopeIDs: scope.CampaignIDs, Limit: 1,
	}
	args := operatorReleaseNamedArgs(query, nil, nil)
	args["releaseID"] = releaseID
	var payload string
	err := internalctx.GetDb(ctx).QueryRow(ctx, operatorReleaseDetailSQL, args).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not query operator release detail: %w", err)
	}
	var detail types.OperatorReleaseDetail
	if err := json.Unmarshal([]byte(payload), &detail); err != nil {
		return nil, fmt.Errorf("could not decode operator release detail: %w", err)
	}
	if detail.Artifacts == nil {
		detail.Artifacts = []types.OperatorReleaseArtifact{}
	}
	if detail.ComponentPins == nil {
		detail.ComponentPins = []types.OperatorReleaseComponentPin{}
	}
	if detail.GraphEdges == nil {
		detail.GraphEdges = []types.OperatorReleaseGraphEdge{}
	}
	if detail.Evidence == nil {
		detail.Evidence = []types.OperatorEvidenceRef{}
	}
	return &detail, nil
}

func operatorReleaseNamedArgs(
	query OperatorReleaseQuery,
	cursorCreatedAt any,
	cursorID any,
) pgx.NamedArgs {
	return pgx.NamedArgs{
		"organizationID": query.OrganizationID, "organizationWide": query.OrganizationWide,
		"customerScopeIDs":       operatorReleaseNonNilUUIDs(query.CustomerScopeIDs),
		"environmentScopeIDs":    operatorReleaseNonNilUUIDs(query.EnvironmentScopeIDs),
		"deploymentUnitScopeIDs": operatorReleaseNonNilUUIDs(query.DeploymentUnitScopeIDs),
		"componentScopeIDs":      operatorReleaseNonNilUUIDs(query.ComponentScopeIDs),
		"campaignScopeIDs":       operatorReleaseNonNilUUIDs(query.CampaignScopeIDs),
		"applicationID":          query.ApplicationID, "kind": query.Kind, "status": query.Status,
		"searchPattern": query.SearchPattern, "cursorCreatedAt": cursorCreatedAt,
		"cursorID": cursorID, "fetchLimit": query.Limit,
	}
}

func validateOperatorReleaseQuery(query OperatorReleaseQuery) error {
	if err := validateOperatorReleaseScope(types.OperatorScopeFilter{
		OrganizationID: query.OrganizationID, DecisionAt: query.DecisionAt,
		OrganizationWide: query.OrganizationWide,
		CustomerIDs:      query.CustomerScopeIDs, EnvironmentIDs: query.EnvironmentScopeIDs,
		DeploymentUnitIDs: query.DeploymentUnitScopeIDs, ComponentIDs: query.ComponentScopeIDs,
		CampaignIDs: query.CampaignScopeIDs,
	}); err != nil {
		return err
	}
	if query.Limit < 1 || query.Limit > types.OperatorMaximumPageLimit+1 ||
		(query.ApplicationID != nil && *query.ApplicationID == uuid.Nil) {
		return apierrors.ErrBadRequest
	}
	if query.Cursor != nil && (query.Cursor.CreatedAt.IsZero() || query.Cursor.ID == uuid.Nil) {
		return apierrors.ErrBadRequest
	}
	return nil
}

func validateOperatorReleaseScope(scope types.OperatorScopeFilter) error {
	return validateCanonicalOperatorScopeFilter(scope)
}

func operatorReleaseNonNilUUIDs(values []uuid.UUID) []uuid.UUID {
	if values == nil {
		return []uuid.UUID{}
	}
	return values
}
