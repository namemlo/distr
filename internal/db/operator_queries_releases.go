package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
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
    application.name AS application,
    COALESCE((
      SELECT jsonb_agg(jsonb_build_object(
        'id', customer.id,
        'name', customer.name
      ) ORDER BY customer.name, customer.id)
      FROM (
        SELECT DISTINCT customer_organization.id, customer_organization.name
        FROM DeploymentPlan client_plan
        JOIN DeploymentUnit client_unit
          ON client_unit.id = client_plan.deployment_unit_id
         AND client_unit.organization_id = client_plan.organization_id
        JOIN DeploymentScope client_scope
          ON client_scope.id = client_unit.deployment_scope_id
         AND client_scope.organization_id = client_unit.organization_id
        LEFT JOIN DeploymentUnitSubscriber client_subscriber
          ON client_subscriber.deployment_unit_id = client_unit.id
         AND client_subscriber.organization_id = client_unit.organization_id
         AND client_subscriber.retired_at IS NULL
        CROSS JOIN LATERAL unnest(array_remove(ARRAY[
          client_scope.customer_organization_id,
          client_subscriber.customer_organization_id
        ]::uuid[], NULL)) client_customer_id
        JOIN CustomerOrganization customer_organization
          ON customer_organization.id = client_customer_id
         AND customer_organization.organization_id = client_plan.organization_id
        WHERE client_plan.organization_id = rb.organization_id
          AND (
            client_plan.release_bundle_id = rb.id
            OR EXISTS (
              SELECT 1
              FROM ProductReleaseComponent client_pin
              WHERE client_pin.organization_id = client_plan.organization_id
                AND client_pin.product_release_bundle_id = client_plan.release_bundle_id
                AND client_pin.component_release_bundle_id = rb.id
            )
          )
      ) customer
    ), '[]'::jsonb) AS clients,
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
  JOIN Application application
    ON application.id = rb.application_id
   AND application.organization_id = rb.organization_id
  WHERE rb.organization_id = @organizationID`

const operatorReleaseListSQL = `
WITH visible_releases AS (` + operatorReleaseProjectionSQL + operatorReleaseVisibleScopeSQL + `
), filtered_releases AS (
  SELECT *
  FROM visible_releases release
  WHERE (
      @customerOrganizationID::uuid IS NULL
      OR EXISTS (
        SELECT 1
        FROM DeploymentPlan plan
        JOIN DeploymentUnit unit
          ON unit.id = plan.deployment_unit_id
         AND unit.organization_id = plan.organization_id
        JOIN DeploymentScope deployment_scope
          ON deployment_scope.id = unit.deployment_scope_id
         AND deployment_scope.organization_id = unit.organization_id
        LEFT JOIN DeploymentUnitSubscriber subscriber
          ON subscriber.deployment_unit_id = unit.id
         AND subscriber.organization_id = unit.organization_id
         AND subscriber.retired_at IS NULL
        WHERE plan.organization_id = release.organization_id
          AND (
            plan.release_bundle_id = release.id
            OR EXISTS (
              SELECT 1
              FROM ProductReleaseComponent customer_pin
              WHERE customer_pin.organization_id = plan.organization_id
                AND customer_pin.product_release_bundle_id = plan.release_bundle_id
                AND customer_pin.component_release_bundle_id = release.id
            )
          )
          AND @customerOrganizationID::uuid IN (
            deployment_scope.customer_organization_id,
            subscriber.customer_organization_id
          )
      )
    )
    AND (@applicationID::uuid IS NULL OR release.application_id = @applicationID)
    AND (
      @deploymentUnitID::uuid IS NULL
      OR EXISTS (
        SELECT 1
        FROM DeploymentPlan unit_plan
        WHERE unit_plan.organization_id = release.organization_id
          AND unit_plan.deployment_unit_id = @deploymentUnitID
          AND (
            unit_plan.release_bundle_id = release.id
            OR EXISTS (
              SELECT 1
              FROM ProductReleaseComponent unit_pin
              WHERE unit_pin.organization_id = unit_plan.organization_id
                AND unit_pin.product_release_bundle_id = unit_plan.release_bundle_id
                AND unit_pin.component_release_bundle_id = release.id
            )
          )
      )
    )
    AND (@kind = '' OR release.kind = @kind)
    AND (@status = '' OR release.status = @status)
    AND (
      @searchPattern = ''
      OR concat_ws(
        ' ', release.application, release.clients::text, release.version, release.kind, release.status,
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
        'application', release.application,
        'clients', release.clients,
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
),
selected_plan AS (
  SELECT plan.id, plan.deployment_unit_id, plan.release_bundle_id
  FROM DeploymentPlan plan
  JOIN release_row release
    ON release.organization_id = plan.organization_id
  JOIN DeploymentUnit unit
    ON unit.id = plan.deployment_unit_id
   AND unit.organization_id = plan.organization_id
  JOIN DeploymentScope scope
    ON scope.id = unit.deployment_scope_id
   AND scope.organization_id = unit.organization_id
  WHERE @deploymentUnitID::uuid IS NOT NULL
    AND plan.deployment_unit_id = @deploymentUnitID
    AND plan.plan_schema = 'distr.target-deployment-plan/v2'
    AND (
      plan.release_bundle_id = release.id
      OR EXISTS (
        SELECT 1
        FROM ProductReleaseComponent selected_pin
        WHERE selected_pin.organization_id = release.organization_id
          AND selected_pin.product_release_bundle_id = plan.release_bundle_id
          AND selected_pin.component_release_bundle_id = release.id
      )
    )
    AND (
      @organizationWide
      OR unit.id = ANY(@deploymentUnitScopeIDs::uuid[])
      OR plan.environment_id = ANY(@environmentScopeIDs::uuid[])
      OR scope.customer_organization_id = ANY(@customerScopeIDs::uuid[])
      OR EXISTS (
        SELECT 1
        FROM DeploymentUnitSubscriber subscriber
        WHERE subscriber.organization_id = unit.organization_id
          AND subscriber.deployment_unit_id = unit.id
          AND subscriber.retired_at IS NULL
          AND subscriber.customer_organization_id = ANY(@customerScopeIDs::uuid[])
      )
    )
  ORDER BY plan.created_at DESC, plan.id DESC
  LIMIT 1
)
SELECT jsonb_build_object(
  'release', jsonb_build_object(
    'id', release.id,
    'createdAt', release.created_at,
    'kind', release.kind,
    'applicationId', release.application_id,
    'application', release.application,
    'clients', release.clients,
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
      'kind', edge.capability_name,
      'consumerComponent', edge.consumer_component_key,
      'providerComponent', COALESCE(edge.provider_component_key, ''),
      'capability', edge.capability_name,
      'versionRange', edge.version_range,
      'providerVersion', edge.provider_version,
      'providerArtifacts', COALESCE((
        SELECT jsonb_agg(jsonb_build_object(
          'artifactKey', artifact.artifact_key,
          'artifactType', artifact.artifact_type,
          'manifestDigest', artifact.manifest_digest,
          'platform', artifact.platform,
          'platformDigest', artifact.platform_digest
        ) ORDER BY artifact.artifact_key, artifact.platform, artifact.id)
        FROM ProductReleaseComponent provider_pin
        JOIN ComponentReleaseArtifact artifact
          ON artifact.organization_id = provider_pin.organization_id
         AND artifact.release_bundle_id = provider_pin.component_release_bundle_id
        WHERE provider_pin.organization_id = edge.organization_id
          AND provider_pin.product_release_bundle_id = edge.product_release_bundle_id
          AND provider_pin.component_key = edge.provider_component_key
      ), '[]'::jsonb),
      'resolutionStage', edge.resolution_stage,
      'allowedModes', edge.allowed_modes,
      'ordering', edge.ordering
    ) ORDER BY edge.edge_key, edge.id)
    FROM ProductReleaseCapabilityEdge edge
    WHERE edge.organization_id = release.organization_id
      AND edge.product_release_bundle_id = release.id
  ), '[]'::jsonb),
  'sourceBuildProof', COALESCE((
    SELECT jsonb_agg(jsonb_build_object(
      'component', contract.component,
      'schema', contract.document ->> 'schema',
      'declaredRepository', contract.document -> 'source' ->> 'repository',
      'declaredRequestedRef', contract.document -> 'source' ->> 'requestedRef',
      'declaredSourceCommit', contract.document -> 'source' ->> 'commit',
      'declaredBuilderId', contract.document -> 'build' ->> 'builder',
      'declaredBuildId', contract.document -> 'build' ->> 'id',
      'verifiedSourceUri', verification.source_uri,
      'verifiedSourceCommit', verification.source_commit,
      'verifiedBuilderId', verification.builder_id,
      'verifiedBuildId', verification.build_id,
      'verifiedBuildType', verification.build_type,
      'provenanceReference', verification.evidence_reference,
      'provenanceDigest', verification.evidence_digest,
      'verificationMode', CASE
        WHEN verification.id IS NULL THEN ''
        WHEN verification.signer_issuer LIKE 'keyid:%' THEN 'keyful'
        ELSE 'keyless'
      END,
      'trustRootId', CASE
        WHEN verification.signer_issuer LIKE 'keyid:%' THEN ''
        ELSE COALESCE(verification.trust_root_id, '')
      END,
      'keyId', CASE
        WHEN verification.signer_issuer LIKE 'keyid:%' THEN verification.trust_root_id
        ELSE ''
      END,
      'keyFingerprint', CASE
        WHEN verification.signer_issuer LIKE 'keyid:%' THEN verification.signer_identity
        ELSE ''
      END,
      'sbomReference', COALESCE(sbom.reference, ''),
      'sbomDigest', COALESCE(substring(sbom.reference from '(sha256:[0-9a-f]{64})$'), ''),
      'verificationState', CASE WHEN verification.id IS NULL THEN 'UNVERIFIED' ELSE 'VERIFIED' END
    ) ORDER BY contract.component)
    FROM (
      SELECT
        direct.release_contract ->> 'componentKey' AS component,
        direct.release_contract AS document,
        direct.id AS component_release_id
      FROM ReleaseBundle direct
      WHERE direct.id = release.id
        AND direct.organization_id = release.organization_id
        AND direct.kind = 'component'
      UNION ALL
      SELECT pin.component_key, pin.contract_snapshot, pin.component_release_bundle_id
      FROM ProductReleaseComponent pin
      WHERE pin.organization_id = release.organization_id
        AND pin.product_release_bundle_id = release.id
    ) contract
    LEFT JOIN LATERAL (
      SELECT candidate.id, candidate.source_uri, candidate.source_commit,
             candidate.builder_id, candidate.build_id, candidate.build_type,
             candidate.evidence_reference, candidate.evidence_digest,
             candidate.trust_root_id, candidate.signer_issuer, candidate.signer_identity
      FROM ComponentReleaseEvidenceVerification candidate
      WHERE candidate.organization_id = release.organization_id
        AND candidate.release_bundle_id = contract.component_release_id
      ORDER BY candidate.verified_at DESC, candidate.id DESC
      LIMIT 1
    ) verification ON true
    LEFT JOIN LATERAL (
      SELECT evidence.reference
      FROM ComponentReleaseEvidence evidence
      WHERE evidence.organization_id = release.organization_id
        AND evidence.release_bundle_id = contract.component_release_id
        AND evidence.evidence_type = 'sbom'
      ORDER BY evidence.reference, evidence.id
      LIMIT 1
    ) sbom ON true
  ), '[]'::jsonb),
  'changelog', jsonb_build_array(),
  'skippedReleases', jsonb_build_array(),
  'changeContext', jsonb_build_object(
    'state', CASE WHEN @deploymentUnitID::uuid IS NULL THEN 'CONTEXT_REQUIRED' ELSE 'NOT_FOUND' END
  ),
  'changeContextSource', CASE WHEN selected_plan.id IS NULL THEN NULL ELSE jsonb_build_object(
    'deploymentPlanId', selected_plan.id,
    'deploymentUnitId', selected_plan.deployment_unit_id,
    'plannedComponents', CASE
      WHEN release.kind = 'component' THEN COALESCE((
        SELECT jsonb_agg(jsonb_build_object(
          'componentKey', planned.component_key,
          'releaseBundleId', planned.release_bundle_id
        ) ORDER BY planned.component_key)
        FROM (
          SELECT pin.component_key, pin.component_release_bundle_id AS release_bundle_id
          FROM ProductReleaseComponent pin
          WHERE pin.organization_id = release.organization_id
            AND pin.product_release_bundle_id = selected_plan.release_bundle_id
            AND pin.component_release_bundle_id = release.id
          UNION
          SELECT direct.component_key, release.id
          FROM (
            SELECT component_release.release_contract ->> 'componentKey' AS component_key
            FROM ReleaseBundle component_release
            WHERE component_release.organization_id = release.organization_id
              AND component_release.id = release.id
            UNION
            SELECT artifact.component_key
            FROM ComponentReleaseArtifact artifact
            WHERE artifact.organization_id = release.organization_id
              AND artifact.release_bundle_id = release.id
          ) direct
          WHERE selected_plan.release_bundle_id = release.id
            AND COALESCE(direct.component_key, '') <> ''
        ) planned
      ), '[]'::jsonb)
      ELSE COALESCE((
        SELECT jsonb_agg(jsonb_build_object(
          'componentKey', pin.component_key,
          'releaseBundleId', pin.component_release_bundle_id
        ) ORDER BY pin.component_key, pin.id)
        FROM ProductReleaseComponent pin
        WHERE pin.organization_id = release.organization_id
          AND pin.product_release_bundle_id = release.id
      ), '[]'::jsonb)
    END,
    'baselines', COALESCE((
      SELECT jsonb_agg(jsonb_build_object(
        'componentKey', baseline.component_key,
        'releaseBundleId', baseline.release_bundle_id,
        'observationId', baseline.observation_id,
        'observationChecksum', baseline.observation_checksum,
        'independentlyHealthy', EXISTS (
          SELECT 1
          FROM TargetComponentObservation healthy_observation
          JOIN TargetComponentState healthy_state
            ON healthy_state.id = healthy_observation.target_component_state_id
           AND healthy_state.organization_id = healthy_observation.organization_id
          WHERE healthy_observation.id = baseline.observation_id
            AND healthy_observation.organization_id = baseline.organization_id
            AND healthy_observation.component_instance_id = baseline.component_instance_id
            AND healthy_observation.health = 'HEALTHY'
            AND healthy_observation.state_version = baseline.desired_revision
            AND healthy_observation.state_checksum = baseline.desired_checksum
            AND healthy_observation.release_bundle_id = baseline.release_bundle_id
            AND healthy_state.state_version = baseline.desired_revision
            AND healthy_state.state_checksum = baseline.desired_checksum
            AND healthy_state.release_bundle_id = baseline.release_bundle_id
        )
      ) ORDER BY baseline.sort_order, baseline.component_key)
      FROM DeploymentPlanBaseline baseline
      WHERE baseline.organization_id = release.organization_id
        AND baseline.deployment_plan_id = selected_plan.id
    ), '[]'::jsonb),
    'changes', COALESCE((
      SELECT jsonb_agg(jsonb_build_object(
        'componentKey', change_entry.component_key,
        'kind', change_entry.kind,
        'before', change_entry.before_value,
        'after', change_entry.after_value,
        'releaseNotes', change_entry.release_notes,
        'forwardOnly', change_entry.forward_only,
        'sortOrder', change_entry.sort_order
      ) ORDER BY change_entry.sort_order, change_entry.id)
      FROM DeploymentPlanChangeEntry change_entry
      WHERE change_entry.organization_id = release.organization_id
        AND change_entry.deployment_plan_id = selected_plan.id
    ), '[]'::jsonb)
  ) END,
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
FROM release_row release
LEFT JOIN selected_plan ON true`

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
	DeploymentUnitID       *uuid.UUID
	CustomerOrganizationID *uuid.UUID
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
	return GetOperatorReleaseDetailWithContext(
		ctx, scope, releaseID, types.OperatorReleaseDetailContext{},
	)
}

func GetOperatorReleaseDetailWithContext(
	ctx context.Context,
	scope types.OperatorScopeFilter,
	releaseID uuid.UUID,
	detailContext types.OperatorReleaseDetailContext,
) (*types.OperatorReleaseDetail, error) {
	if releaseID == uuid.Nil || validateOperatorReleaseScope(scope) != nil {
		return nil, apierrors.ErrBadRequest
	}
	if detailContext.DeploymentUnitID != nil && *detailContext.DeploymentUnitID == uuid.Nil {
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
	args["deploymentUnitID"] = detailContext.DeploymentUnitID
	var payload string
	err := internalctx.GetDb(ctx).QueryRow(ctx, operatorReleaseDetailSQL, args).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not query operator release detail: %w", err)
	}
	var decoded operatorReleaseDetailPayload
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		return nil, fmt.Errorf("could not decode operator release detail: %w", err)
	}
	detail := decoded.OperatorReleaseDetail
	if decoded.ChangeContextSource != nil {
		changeResult := buildOperatorReleaseChangelog(*decoded.ChangeContextSource, releaseID)
		detail.ChangeContext = changeResult.Context
		detail.Changelog = changeResult.Changelog
		detail.SkippedReleases = changeResult.SkippedReleases
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
	if detail.SourceBuildProof == nil {
		detail.SourceBuildProof = []types.OperatorReleaseSourceBuildProof{}
	}
	for index := range detail.SourceBuildProof {
		if digest := immutableEvidenceDigest(detail.SourceBuildProof[index].SBOMReference); digest != "" {
			detail.SourceBuildProof[index].SBOMDigest = digest
		}
	}
	if detail.Changelog == nil {
		detail.Changelog = []types.OperatorReleaseChange{}
	}
	if detail.SkippedReleases == nil {
		detail.SkippedReleases = []types.OperatorReleaseSkippedRelease{}
	}
	if detail.Evidence == nil {
		detail.Evidence = []types.OperatorEvidenceRef{}
	}
	return &detail, nil
}

var (
	immutableColonDigestPattern = regexp.MustCompile(
		`@((?:sha256):[0-9a-f]{64})(?:$|/)`,
	)
	immutablePathDigestPattern = regexp.MustCompile(
		`/sha256/([0-9a-f]{64})(?:$|/)`,
	)
)

func immutableEvidenceDigest(reference string) string {
	if matches := immutableColonDigestPattern.FindStringSubmatch(reference); len(matches) == 2 {
		return matches[1]
	}
	if matches := immutablePathDigestPattern.FindStringSubmatch(reference); len(matches) == 2 {
		return "sha256:" + matches[1]
	}
	return ""
}

type operatorReleaseDetailPayload struct {
	types.OperatorReleaseDetail
	ChangeContextSource *operatorReleaseChangeContextSource `json:"changeContextSource"`
}

type operatorReleaseChangeContextSource struct {
	DeploymentPlanID  uuid.UUID                         `json:"deploymentPlanId"`
	DeploymentUnitID  uuid.UUID                         `json:"deploymentUnitId"`
	PlannedComponents []operatorReleasePlannedComponent `json:"plannedComponents"`
	Baselines         []operatorReleaseBaselineSource   `json:"baselines"`
	Changes           []types.DeploymentPlanChangeEntry `json:"changes"`
}

type operatorReleasePlannedComponent struct {
	ComponentKey    string    `json:"componentKey"`
	ReleaseBundleID uuid.UUID `json:"releaseBundleId"`
}

type operatorReleaseBaselineSource struct {
	ComponentKey         string     `json:"componentKey"`
	ReleaseBundleID      *uuid.UUID `json:"releaseBundleId,omitempty"`
	ObservationID        *uuid.UUID `json:"observationId,omitempty"`
	ObservationChecksum  string     `json:"observationChecksum"`
	IndependentlyHealthy bool       `json:"independentlyHealthy"`
}

type operatorReleaseChangelogResult struct {
	Context         types.OperatorReleaseChangeContext
	Changelog       []types.OperatorReleaseChange
	SkippedReleases []types.OperatorReleaseSkippedRelease
}

func buildOperatorReleaseChangelog(
	source operatorReleaseChangeContextSource,
	_ uuid.UUID,
) operatorReleaseChangelogResult {
	planID := source.DeploymentPlanID
	unitID := source.DeploymentUnitID
	result := operatorReleaseChangelogResult{
		Context: types.OperatorReleaseChangeContext{
			DeploymentPlanID: &planID,
			DeploymentUnitID: &unitID,
			State:            types.OperatorReleaseChangeContextReady,
		},
		Changelog:       []types.OperatorReleaseChange{},
		SkippedReleases: []types.OperatorReleaseSkippedRelease{},
	}
	plannedByComponent := make(map[string]uuid.UUID, len(source.PlannedComponents))
	for _, planned := range source.PlannedComponents {
		plannedByComponent[planned.ComponentKey] = planned.ReleaseBundleID
	}
	selectedBaselines := 0
	for _, baseline := range source.Baselines {
		if _, selected := plannedByComponent[baseline.ComponentKey]; !selected {
			continue
		}
		selectedBaselines++
		if !baseline.IndependentlyHealthy ||
			baseline.ObservationID == nil ||
			baseline.ObservationChecksum == "" {
			result.Context.State = types.OperatorReleaseChangeContextBaselineUnverified
			result.Context.Message = "the selected target has no independently verified healthy baseline"
			return result
		}
	}
	if selectedBaselines == 0 {
		result.Context.State = types.OperatorReleaseChangeContextBaselineUnverified
		result.Context.Message = "the selected target has no independently verified healthy baseline"
		return result
	}
	notesByComponent := make(map[string][]types.ReleaseNote)
	for _, change := range source.Changes {
		if change.ComponentKey != "" {
			if _, selected := plannedByComponent[change.ComponentKey]; !selected {
				continue
			}
		}
		if change.Kind == types.DeploymentPlanChangeLimitExceeded {
			result.Context.State = types.OperatorReleaseChangeContextDivergentHistory
			result.Context.Message = "release history diverges from the selected target baseline"
			return operatorReleaseChangelogResult{
				Context: result.Context, Changelog: []types.OperatorReleaseChange{},
				SkippedReleases: []types.OperatorReleaseSkippedRelease{},
			}
		}
		switch change.Kind {
		case types.DeploymentPlanChangeSourceNotes:
			notesByComponent[change.ComponentKey] = change.ReleaseNotes
			for _, note := range change.ReleaseNotes {
				result.Changelog = append(result.Changelog, types.OperatorReleaseChange{
					Category: "code", Component: change.ComponentKey,
					Summary: note.Summary, Reference: note.ReleaseBundleID.String(),
				})
			}
			for _, note := range change.ReleaseNotes[:max(0, len(change.ReleaseNotes)-1)] {
				result.SkippedReleases = append(result.SkippedReleases, types.OperatorReleaseSkippedRelease{
					Component: change.ComponentKey, ReleaseID: note.ReleaseBundleID,
					Version: note.Version, SourceRevision: note.SourceRevision, Summary: note.Summary,
				})
			}
		case types.DeploymentPlanChangeConfig:
			result.Changelog = append(result.Changelog, types.OperatorReleaseChange{
				Category: "config", Component: change.ComponentKey,
				Summary: "configuration changed", Reference: change.After,
			})
		case types.DeploymentPlanChangeSchema:
			result.Changelog = append(result.Changelog, types.OperatorReleaseChange{
				Category: "migration", Component: change.ComponentKey,
				Summary: "schema changed", Reference: change.After,
			})
		case types.DeploymentPlanChangeProvider:
			result.Changelog = append(result.Changelog, types.OperatorReleaseChange{
				Category: "dependency", Component: change.ComponentKey,
				Summary: "provider binding changed", Reference: change.After,
			})
		}
	}
	for _, baseline := range source.Baselines {
		plannedReleaseID, selected := plannedByComponent[baseline.ComponentKey]
		if !selected {
			continue
		}
		if baseline.ReleaseBundleID == nil {
			continue
		}
		if *baseline.ReleaseBundleID == plannedReleaseID {
			continue
		}
		notes := notesByComponent[baseline.ComponentKey]
		if len(notes) == 0 {
			return divergentOperatorReleaseChangelog(result.Context)
		}
		if notes[len(notes)-1].ReleaseBundleID != plannedReleaseID {
			return divergentOperatorReleaseChangelog(result.Context)
		}
	}
	return result
}

func divergentOperatorReleaseChangelog(
	context types.OperatorReleaseChangeContext,
) operatorReleaseChangelogResult {
	context.State = types.OperatorReleaseChangeContextDivergentHistory
	context.Message = "release history diverges from the selected target baseline"
	return operatorReleaseChangelogResult{
		Context: context, Changelog: []types.OperatorReleaseChange{},
		SkippedReleases: []types.OperatorReleaseSkippedRelease{},
	}
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
		"customerOrganizationID": query.CustomerOrganizationID,
		"applicationID":          query.ApplicationID, "kind": query.Kind, "status": query.Status,
		"deploymentUnitID": query.DeploymentUnitID,
		"searchPattern":    query.SearchPattern, "cursorCreatedAt": cursorCreatedAt,
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
		(query.ApplicationID != nil && *query.ApplicationID == uuid.Nil) ||
		(query.DeploymentUnitID != nil && *query.DeploymentUnitID == uuid.Nil) ||
		(query.CustomerOrganizationID != nil && *query.CustomerOrganizationID == uuid.Nil) {
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
