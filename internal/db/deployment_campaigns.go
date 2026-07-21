package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/campaigns"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"strings"
	"time"
)

const campaignCandidateQuery = `
	WITH selected_plan AS (
		SELECT
			plan.id,
			plan.organization_id,
			plan.deployment_unit_id,
			plan.canonical_checksum,
			plan.effective_policy_checksum,
			plan.subscriber_set_checksum,
			COALESCE(
				array_agg(DISTINCT tag ORDER BY tag)
					FILTER (WHERE tag IS NOT NULL),
				'{}'::text[]
			) AS tags
		FROM DeploymentPlan plan
		LEFT JOIN DeploymentPlanStep step
		  ON step.deployment_plan_id = plan.id
		 AND step.organization_id = plan.organization_id
		LEFT JOIN LATERAL unnest(step.target_tags) tag ON true
		WHERE plan.organization_id = $1
		  AND plan.deployment_unit_id IS NOT NULL
		  AND plan.status IN ('READY', 'EXECUTED')
		  AND (
			plan.id = ANY($2::uuid[])
			OR cardinality($3::text[]) > 0
		  )
		GROUP BY
			plan.id,
			plan.organization_id,
			plan.deployment_unit_id,
			plan.canonical_checksum,
			plan.effective_policy_checksum,
			plan.subscriber_set_checksum
		HAVING
			plan.id = ANY($2::uuid[])
			OR $3::text[] <@ COALESCE(
				array_agg(DISTINCT tag)
					FILTER (WHERE tag IS NOT NULL),
				'{}'::text[]
			)
		ORDER BY plan.id
		LIMIT 1001
	)
	SELECT
		plan.id,
		plan.organization_id,
		plan.deployment_unit_id,
		plan.canonical_checksum,
		plan.effective_policy_checksum,
		plan.tags,
		approval.id,
		approval.revision,
		approval.subject_checksum,
		COALESCE(
			approval.state = 'APPROVED'
				AND approval.expires_at > now()
				AND approval.invalidated_at IS NULL
				AND approval.subject_checksum = plan.canonical_checksum
				AND approval.effective_policy_checksum =
					plan.effective_policy_checksum
				AND approval.subscriber_set_checksum =
					plan.subscriber_set_checksum,
			false
		),
		COALESCE(admission.id, '00000000-0000-0000-0000-000000000000'::uuid),
		COALESCE(admission.decision_checksum, ''),
		COALESCE(
			ARRAY(
				SELECT (item.value ->> 'versionId')::uuid
				FROM jsonb_array_elements(
					COALESCE(
						admission.temporal_evidence -> 'calendarEvidence',
						'[]'::jsonb
					)
				) WITH ORDINALITY item(value, ordinal)
				ORDER BY item.ordinal
			),
			'{}'::uuid[]
		),
		COALESCE(
			ARRAY(
				SELECT item.value ->> 'checksum'
				FROM jsonb_array_elements(
					COALESCE(
						admission.temporal_evidence -> 'calendarEvidence',
						'[]'::jsonb
					)
				) WITH ORDINALITY item(value, ordinal)
				ORDER BY item.ordinal
			),
			'{}'::text[]
		),
		COALESCE(
			admission.decision = 'ADMIT'
				AND admission.plan_checksum = plan.canonical_checksum
				AND admission.effective_policy_checksum =
					plan.effective_policy_checksum
				AND admission.approval_request_id = approval.id
				AND admission.approval_request_revision = approval.revision,
			false
		)
	FROM selected_plan plan
	LEFT JOIN LATERAL (
		SELECT request.*
		FROM ApprovalRequest request
		WHERE request.organization_id = plan.organization_id
		  AND request.subject_type = 'deployment_plan'
		  AND request.subject_id = plan.id
		ORDER BY request.created_at DESC, request.id DESC
		LIMIT 1
	) approval ON true
	LEFT JOIN LATERAL (
		SELECT evaluation.*
		FROM AdmissionEvaluation evaluation
		WHERE evaluation.organization_id = plan.organization_id
		  AND evaluation.deployment_plan_id = plan.id
		ORDER BY evaluation.created_at DESC, evaluation.id DESC
		LIMIT 1
	) admission ON true
	ORDER BY plan.id`

const campaignPrerequisiteEvidenceQuery = `
	SELECT
		requested.plan_id,
		requested.step_key,
		requested.placement_id,
		definition.key,
		runtime_pin.artifact_digest,
		component.config_checksum,
		component.platform,
		snapshot_component.deployment_unit_id AS provider_deployment_unit_id,
		snapshot_component.component_instance_id AS provider_component_instance_id
	FROM jsonb_to_recordset($2::jsonb) AS requested(
		plan_id uuid,
		step_key text,
		placement_id uuid
	)
	JOIN DeploymentPlan plan
	  ON plan.id = requested.plan_id
	 AND plan.organization_id = $1
	JOIN DeploymentPlanStep step
	  ON step.deployment_plan_id = requested.plan_id
	 AND step.step_key = requested.step_key
	 AND step.organization_id = $1
	JOIN DeploymentPlanTargetComponent component
	  ON component.deployment_plan_id = requested.plan_id
	 AND component.id = requested.placement_id
	 AND component.organization_id = $1
	JOIN ComponentDefinition definition
	  ON definition.key = component.component
	 AND definition.organization_id = $1
	JOIN ComponentInstance instance
	  ON instance.component_definition_id = definition.id
	 AND instance.deployment_unit_id = plan.deployment_unit_id
	 AND instance.organization_id = $1
	JOIN LATERAL (
		SELECT pin.value ->> 'platformDigest' AS artifact_digest
		FROM jsonb_array_elements(
			COALESCE(
				convert_from(plan.canonical_payload, 'UTF8')::jsonb
					-> 'componentReleasePins',
				'[]'::jsonb
			)
		) pin(value)
		WHERE pin.value ->> 'componentKey' = definition.key
		  AND pin.value ->> 'platformDigest' ~ '^sha256:[0-9a-f]{64}$'
		ORDER BY pin.value ->> 'platformDigest'
		LIMIT 1
	) runtime_pin ON true
	JOIN TargetConfigSnapshotComponent snapshot_component
	  ON snapshot_component.target_config_snapshot_id =
			plan.target_config_snapshot_id
	 AND snapshot_component.deployment_unit_id = plan.deployment_unit_id
	 AND snapshot_component.component_instance_id = instance.id
	 AND snapshot_component.organization_id = $1
	WHERE requested.plan_id = ANY($3::uuid[])
	ORDER BY
		requested.plan_id,
		requested.step_key,
		requested.placement_id`

const campaignDraftOutput = `
	id,
	created_at,
	updated_at,
	organization_id,
	name,
	description,
	revision,
	membership,
	waves,
	prerequisites,
	risk_policy,
	last_published_revision_id,
	created_by_useraccount_id,
	updated_by_useraccount_id
`

func CreateDeploymentCampaignDraft(
	ctx context.Context,
	draft *types.CampaignDraft,
) error {
	if draft.ID == uuid.Nil {
		draft.ID = uuid.New()
	}
	membership, waves, prerequisites, riskPolicy, err :=
		marshalCampaignDraftDocuments(*draft)
	if err != nil {
		return err
	}
	return internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO DeploymentCampaignDraft (
			id,
			organization_id,
			name,
			description,
			membership,
			waves,
			prerequisites,
			risk_policy,
			created_by_useraccount_id,
			updated_by_useraccount_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
		RETURNING created_at, updated_at, revision`,
		draft.ID,
		draft.OrganizationID,
		draft.Name,
		draft.Description,
		membership,
		waves,
		prerequisites,
		riskPolicy,
		draft.CreatedByUserAccountID,
	).Scan(&draft.CreatedAt, &draft.UpdatedAt, &draft.Revision)
}

func GetDeploymentCampaignDraft(
	ctx context.Context,
	id uuid.UUID,
	organizationID uuid.UUID,
) (*types.CampaignDraft, error) {
	draft, err := getDeploymentCampaignDraft(ctx, id, organizationID, false)
	if err != nil {
		return nil, err
	}
	if err := hydrateCampaignCandidates(ctx, draft); err != nil {
		return nil, err
	}
	return draft, nil
}

func UpdateDeploymentCampaignDraft(
	ctx context.Context,
	draft *types.CampaignDraft,
	expectedRevision int64,
) error {
	membership, waves, prerequisites, riskPolicy, err :=
		marshalCampaignDraftDocuments(*draft)
	if err != nil {
		return err
	}
	err = internalctx.GetDb(ctx).QueryRow(
		ctx,
		`UPDATE DeploymentCampaignDraft
		SET name = $3,
			description = $4,
			membership = $5,
			waves = $6,
			prerequisites = $7,
			risk_policy = $8,
			updated_by_useraccount_id = $9,
			updated_at = now(),
			revision = revision + 1
		WHERE id = $1
		  AND organization_id = $2
		  AND revision = $10
		RETURNING updated_at, revision`,
		draft.ID,
		draft.OrganizationID,
		draft.Name,
		draft.Description,
		membership,
		waves,
		prerequisites,
		riskPolicy,
		draft.UpdatedByUserAccountID,
		expectedRevision,
	).Scan(&draft.UpdatedAt, &draft.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return apierrors.NewConflict("campaign draft revision changed")
	}
	return err
}

func ValidateStoredDeploymentCampaignDraft(
	ctx context.Context,
	id uuid.UUID,
	organizationID uuid.UUID,
) ([]types.ValidationIssue, error) {
	draft, err := GetDeploymentCampaignDraft(ctx, id, organizationID)
	if err != nil {
		return nil, err
	}
	return campaigns.ValidateCampaignDraft(ctx, *draft), nil
}

func PublishCampaignRevision(
	ctx context.Context,
	campaignDraftID uuid.UUID,
	idempotencyKey string,
) (*types.CampaignRevision, error) {
	publication, ok := types.CampaignPublicationFromContext(ctx)
	if !ok || publication.OrganizationID == uuid.Nil ||
		publication.ActorUserID == uuid.Nil {
		return nil, apierrors.ErrForbidden
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" || len(idempotencyKey) > 128 {
		return nil, apierrors.NewBadRequest("idempotency key is invalid")
	}

	var result *types.CampaignRevision
	err := RunTxRR(ctx, func(txCtx context.Context) error {
		existing, err := getCampaignRevisionByPublicationKey(
			txCtx,
			campaignDraftID,
			publication.OrganizationID,
			idempotencyKey,
		)
		if err == nil {
			result = existing
			return nil
		}
		if !errors.Is(err, apierrors.ErrNotFound) {
			return err
		}

		draft, err := getDeploymentCampaignDraft(
			txCtx,
			campaignDraftID,
			publication.OrganizationID,
			true,
		)
		if err != nil {
			return err
		}
		if err := hydrateCampaignCandidates(txCtx, draft); err != nil {
			return err
		}
		issues := campaigns.ValidateCampaignDraft(txCtx, *draft)
		if len(issues) != 0 {
			return apierrors.NewBadRequest(
				"campaign validation failed: " + issues[0].Code,
			)
		}
		members, err := campaigns.ResolveCampaignMembership(txCtx, *draft)
		if err != nil {
			return err
		}
		var revisionNumber int64
		if err := internalctx.GetDb(txCtx).QueryRow(
			txCtx,
			`SELECT COALESCE(max(revision_number), 0) + 1
			FROM DeploymentCampaignRevision
			WHERE deployment_campaign_draft_id = $1
			  AND organization_id = $2`,
			draft.ID,
			draft.OrganizationID,
		).Scan(&revisionNumber); err != nil {
			return err
		}
		revision, err := campaignRevisionFromDraft(
			*draft,
			revisionNumber,
			publication.ActorUserID,
			members,
		)
		if err != nil {
			return err
		}
		if err := insertCampaignRevision(
			txCtx,
			revision,
			idempotencyKey,
		); err != nil {
			return err
		}
		if err := insertCampaignRevisionChildren(txCtx, revision); err != nil {
			return err
		}
		if _, err := internalctx.GetDb(txCtx).Exec(
			txCtx,
			`UPDATE DeploymentCampaignDraft
			SET last_published_revision_id = $3
			WHERE id = $1 AND organization_id = $2`,
			draft.ID,
			draft.OrganizationID,
			revision.ID,
		); err != nil {
			return err
		}
		result = revision
		return nil
	})
	if err == nil {
		return result, nil
	}
	return replayCampaignPublicationConflict(
		err,
		func() (*types.CampaignRevision, error) {
			return getCampaignRevisionByPublicationKey(
				ctx,
				campaignDraftID,
				publication.OrganizationID,
				idempotencyKey,
			)
		},
	)
}

func replayCampaignPublicationConflict(
	err error,
	lookup func() (*types.CampaignRevision, error),
) (*types.CampaignRevision, error) {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) ||
		(postgresError.Code != pgerrcode.UniqueViolation &&
			postgresError.Code != pgerrcode.SerializationFailure) {
		return nil, err
	}
	existing, lookupErr := lookup()
	if lookupErr == nil {
		return existing, nil
	}
	if errors.Is(lookupErr, apierrors.ErrNotFound) {
		return nil, err
	}
	return nil, fmt.Errorf("replay concurrent campaign publication: %w", lookupErr)
}

func campaignRevisionFromDraft(
	draft types.CampaignDraft,
	revisionNumber int64,
	publisherID uuid.UUID,
	members []types.CampaignMember,
) (*types.CampaignRevision, error) {
	revision := &types.CampaignRevision{
		ID:                       uuid.New(),
		OrganizationID:           draft.OrganizationID,
		CampaignDraftID:          draft.ID,
		RevisionNumber:           revisionNumber,
		SourceDraftRevision:      draft.Revision,
		Name:                     draft.Name,
		Description:              draft.Description,
		MembershipTagQuery:       draft.Membership.TagQuery,
		RiskPolicy:               draft.RiskPolicy,
		PublishedByUserAccountID: publisherID,
		Members:                  append([]types.CampaignMember(nil), members...),
		Waves:                    make([]types.CampaignWave, len(draft.Waves)),
		Prerequisites: make(
			[]types.CampaignPrerequisite,
			len(draft.Prerequisites),
		),
	}
	for index, wave := range draft.Waves {
		revision.Waves[index] = types.CampaignWave{
			Order:              wave.Order,
			Name:               wave.Name,
			BakeSeconds:        wave.BakeSeconds,
			MaximumConcurrency: wave.MaximumConcurrency,
		}
	}
	for index, prerequisite := range draft.Prerequisites {
		evidence := campaignDraftPrerequisiteEvidence(draft, prerequisite)
		revision.Prerequisites[index] = types.CampaignPrerequisite{
			DownstreamPlanID:             prerequisite.DownstreamPlanID,
			UpstreamPlanID:               prerequisite.UpstreamPlanID,
			UpstreamStepKey:              prerequisite.UpstreamStepKey,
			ProviderPlacementID:          prerequisite.ProviderPlacementID,
			ProviderDeploymentUnitID:     evidence.ProviderDeploymentUnitID,
			ProviderComponentInstanceID:  evidence.ProviderComponentInstanceID,
			ExpectedRuntimeStateChecksum: prerequisite.ExpectedRuntimeStateChecksum,
		}
	}
	payload, checksum, err := campaigns.CanonicalizeCampaignRevision(*revision)
	if err != nil {
		return nil, err
	}
	revision.CanonicalPayload = payload
	revision.CanonicalChecksum = checksum
	return revision, nil
}

func campaignDraftPrerequisiteEvidence(
	draft types.CampaignDraft,
	prerequisite types.CampaignPrerequisiteDraft,
) types.CampaignStepPlacementEvidence {
	key := types.CampaignStepPlacement{
		StepKey:     prerequisite.UpstreamStepKey,
		PlacementID: prerequisite.ProviderPlacementID,
	}
	for _, candidate := range draft.CandidatePlans {
		if candidate.PlanID == prerequisite.UpstreamPlanID {
			return candidate.ExpectedStepPlacementEvidence[key]
		}
	}
	return types.CampaignStepPlacementEvidence{}
}

func getDeploymentCampaignDraft(
	ctx context.Context,
	id uuid.UUID,
	organizationID uuid.UUID,
	forUpdate bool,
) (*types.CampaignDraft, error) {
	lock := ""
	if forUpdate {
		lock = " FOR UPDATE"
	}
	row := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`SELECT `+campaignDraftOutput+`
		FROM DeploymentCampaignDraft
		WHERE id = $1 AND organization_id = $2`+lock,
		id,
		organizationID,
	)
	var draft types.CampaignDraft
	var membership, waves, prerequisites, riskPolicy []byte
	err := row.Scan(
		&draft.ID,
		&draft.CreatedAt,
		&draft.UpdatedAt,
		&draft.OrganizationID,
		&draft.Name,
		&draft.Description,
		&draft.Revision,
		&membership,
		&waves,
		&prerequisites,
		&riskPolicy,
		&draft.LastPublishedRevisionID,
		&draft.CreatedByUserAccountID,
		&draft.UpdatedByUserAccountID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(membership, &draft.Membership); err != nil {
		return nil, fmt.Errorf("decode campaign membership: %w", err)
	}
	if err := json.Unmarshal(waves, &draft.Waves); err != nil {
		return nil, fmt.Errorf("decode campaign waves: %w", err)
	}
	if err := json.Unmarshal(prerequisites, &draft.Prerequisites); err != nil {
		return nil, fmt.Errorf("decode campaign prerequisites: %w", err)
	}
	if err := json.Unmarshal(riskPolicy, &draft.RiskPolicy); err != nil {
		return nil, fmt.Errorf("decode campaign risk policy: %w", err)
	}
	return &draft, nil
}

func marshalCampaignDraftDocuments(
	draft types.CampaignDraft,
) ([]byte, []byte, []byte, []byte, error) {
	membership, err := json.Marshal(draft.Membership)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	waves, err := json.Marshal(draft.Waves)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	prerequisites, err := json.Marshal(draft.Prerequisites)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	riskPolicy, err := json.Marshal(draft.RiskPolicy)
	return membership, waves, prerequisites, riskPolicy, err
}

func hydrateCampaignCandidates(
	ctx context.Context,
	draft *types.CampaignDraft,
) error {
	tagTerms, err := campaigns.ParseTagQuery(draft.Membership.TagQuery)
	if err != nil {
		return err
	}
	rows, err := internalctx.GetDb(ctx).Query(
		ctx,
		campaignCandidateQuery,
		draft.OrganizationID,
		draft.Membership.PlanIDs,
		tagTerms,
	)
	if err != nil {
		return err
	}
	defer rows.Close()
	candidates := make([]types.CampaignPlanCandidate, 0)
	for rows.Next() {
		var candidate types.CampaignPlanCandidate
		var approvalRequestID *uuid.UUID
		var approvalRevision *int64
		var approvalChecksum *string
		if err := rows.Scan(
			&candidate.PlanID,
			&candidate.OrganizationID,
			&candidate.DeploymentUnitID,
			&candidate.PlanChecksum,
			&candidate.EffectivePolicyChecksum,
			&candidate.Tags,
			&approvalRequestID,
			&approvalRevision,
			&approvalChecksum,
			&candidate.Approved,
			&candidate.AdmissionEvaluationID,
			&candidate.AdmissionChecksum,
			&candidate.CalendarVersionIDs,
			&candidate.CalendarChecksums,
			&candidate.Admitted,
		); err != nil {
			return err
		}
		candidate.CurrentPlanChecksum = candidate.PlanChecksum
		if approvalRequestID != nil {
			candidate.ApprovalRequestID = *approvalRequestID
			candidate.ApprovalRequestRevision = campaignInt64Value(
				approvalRevision,
			)
			candidate.ApprovalChecksum = campaignStringValue(approvalChecksum)
		}
		candidate.ExpectedStepPlacementEvidence =
			make(map[types.CampaignStepPlacement]types.CampaignStepPlacementEvidence)
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(candidates) > 1000 {
		return apierrors.NewBadRequest(
			"campaign candidate plan set exceeds 1000 entries",
		)
	}
	draft.CandidatePlans = candidates
	if len(candidates) == 0 {
		return nil
	}
	planIDs := make([]uuid.UUID, len(candidates))
	indexByPlan := make(map[uuid.UUID]int, len(candidates))
	for index := range candidates {
		planIDs[index] = candidates[index].PlanID
		indexByPlan[candidates[index].PlanID] = index
	}
	return hydrateCampaignCandidateEvidence(ctx, draft, planIDs, indexByPlan)
}

func hydrateCampaignCandidateEvidence(
	ctx context.Context,
	draft *types.CampaignDraft,
	planIDs []uuid.UUID,
	indexByPlan map[uuid.UUID]int,
) error {
	type requestedPair struct {
		PlanID      uuid.UUID `json:"plan_id"`
		StepKey     string    `json:"step_key"`
		PlacementID uuid.UUID `json:"placement_id"`
	}
	requestedPairs := make([]requestedPair, 0, len(draft.Prerequisites))
	for _, prerequisite := range draft.Prerequisites {
		if _, selected := indexByPlan[prerequisite.UpstreamPlanID]; selected {
			requestedPairs = append(requestedPairs, requestedPair{
				PlanID:      prerequisite.UpstreamPlanID,
				StepKey:     prerequisite.UpstreamStepKey,
				PlacementID: prerequisite.ProviderPlacementID,
			})
		}
	}
	if len(requestedPairs) == 0 {
		return nil
	}
	payload, err := json.Marshal(requestedPairs)
	if err != nil {
		return err
	}
	evidenceRows, err := internalctx.GetDb(ctx).Query(
		ctx,
		campaignPrerequisiteEvidenceQuery,
		draft.OrganizationID,
		payload,
		planIDs,
	)
	if err != nil {
		return err
	}
	defer evidenceRows.Close()
	for evidenceRows.Next() {
		var planID, placementID, providerUnitID, componentInstanceID uuid.UUID
		var stepKey, componentKey, artifactDigest, configChecksum, platform string
		if err := evidenceRows.Scan(
			&planID,
			&stepKey,
			&placementID,
			&componentKey,
			&artifactDigest,
			&configChecksum,
			&platform,
			&providerUnitID,
			&componentInstanceID,
		); err != nil {
			return err
		}
		runtimeChecksum, err := campaigns.RuntimeExpectationChecksum(
			types.CampaignRuntimeExpectation{
				ProviderDeploymentUnitID:    providerUnitID,
				ProviderComponentInstanceID: componentInstanceID,
				ComponentKey:                componentKey,
				ArtifactDigest:              artifactDigest,
				ConfigChecksum:              configChecksum,
				Platform:                    platform,
			},
		)
		if err != nil {
			return apierrors.NewBadRequest(
				"provider placement runtime expectation is invalid: " +
					err.Error(),
			)
		}
		index := indexByPlan[planID]
		candidate := &draft.CandidatePlans[index]
		candidate.ExpectedStepPlacementEvidence[types.CampaignStepPlacement{
			StepKey:     stepKey,
			PlacementID: placementID,
		}] = types.CampaignStepPlacementEvidence{
			ExpectedRuntimeStateChecksum: runtimeChecksum,
			ProviderDeploymentUnitID:     providerUnitID,
			ProviderComponentInstanceID:  componentInstanceID,
		}
		if !campaignContainsUUID(candidate.SharedProviderPlacements, placementID) {
			candidate.SharedProviderPlacements = append(
				candidate.SharedProviderPlacements,
				placementID,
			)
		}
	}
	return evidenceRows.Err()
}

func insertCampaignRevision(
	ctx context.Context,
	revision *types.CampaignRevision,
	idempotencyKey string,
) error {
	riskPolicy, err := json.Marshal(revision.RiskPolicy)
	if err != nil {
		return err
	}
	return internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO DeploymentCampaignRevision (
			id,
			deployment_campaign_draft_id,
			organization_id,
			revision_number,
			source_draft_revision,
			publication_key,
			name,
			description,
			membership_tag_query,
			risk_policy,
			canonical_payload,
			canonical_checksum,
			published_by_useraccount_id
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
		)
		RETURNING published_at`,
		revision.ID,
		revision.CampaignDraftID,
		revision.OrganizationID,
		revision.RevisionNumber,
		revision.SourceDraftRevision,
		idempotencyKey,
		revision.Name,
		revision.Description,
		revision.MembershipTagQuery,
		riskPolicy,
		revision.CanonicalPayload,
		revision.CanonicalChecksum,
		revision.PublishedByUserAccountID,
	).Scan(&revision.PublishedAt)
}

func insertCampaignRevisionChildren(
	ctx context.Context,
	revision *types.CampaignRevision,
) error {
	for index := range revision.Waves {
		revision.Waves[index].ID = uuid.New()
		revision.Waves[index].CampaignRevisionID = revision.ID
		revision.Waves[index].OrganizationID = revision.OrganizationID
	}
	_, err := internalctx.GetDb(ctx).CopyFrom(
		ctx,
		pgx.Identifier{"deploymentcampaignwave"},
		[]string{
			"id",
			"organization_id",
			"campaign_revision_id",
			"wave_order",
			"name",
			"bake_seconds",
			"maximum_concurrency",
		},
		pgx.CopyFromSlice(len(revision.Waves), func(index int) ([]any, error) {
			wave := revision.Waves[index]
			return []any{
				wave.ID,
				wave.OrganizationID,
				wave.CampaignRevisionID,
				wave.Order,
				wave.Name,
				wave.BakeSeconds,
				wave.MaximumConcurrency,
			}, nil
		}),
	)
	if err != nil {
		return err
	}
	for index := range revision.Members {
		revision.Members[index].ID = uuid.New()
		revision.Members[index].CampaignRevisionID = revision.ID
		revision.Members[index].OrganizationID = revision.OrganizationID
	}
	_, err = internalctx.GetDb(ctx).CopyFrom(
		ctx,
		pgx.Identifier{"deploymentcampaignmember"},
		[]string{
			"id",
			"organization_id",
			"campaign_revision_id",
			"deployment_plan_id",
			"deployment_unit_id",
			"plan_checksum",
			"effective_policy_checksum",
			"approval_request_id",
			"approval_request_revision",
			"approval_checksum",
			"calendar_version_ids",
			"calendar_checksums",
			"admission_evaluation_id",
			"admission_checksum",
			"wave_order",
			"member_order",
		},
		pgx.CopyFromSlice(len(revision.Members), func(index int) ([]any, error) {
			member := revision.Members[index]
			return []any{
				member.ID,
				member.OrganizationID,
				member.CampaignRevisionID,
				member.PlanID,
				member.DeploymentUnitID,
				member.PlanChecksum,
				member.EffectivePolicyChecksum,
				member.ApprovalRequestID,
				member.ApprovalRequestRevision,
				member.ApprovalChecksum,
				member.CalendarVersionIDs,
				member.CalendarChecksums,
				member.AdmissionEvaluationID,
				member.AdmissionChecksum,
				member.WaveOrder,
				member.MemberOrder,
			}, nil
		}),
	)
	if err != nil {
		return err
	}
	for index := range revision.Prerequisites {
		revision.Prerequisites[index].ID = uuid.New()
		revision.Prerequisites[index].CampaignRevisionID = revision.ID
		revision.Prerequisites[index].OrganizationID = revision.OrganizationID
	}
	_, err = internalctx.GetDb(ctx).CopyFrom(
		ctx,
		pgx.Identifier{"deploymentcampaignprerequisite"},
		[]string{
			"id",
			"organization_id",
			"campaign_revision_id",
			"downstream_plan_id",
			"upstream_plan_id",
			"upstream_step_key",
			"provider_placement_id",
			"provider_deployment_unit_id",
			"provider_component_instance_id",
			"expected_runtime_state_checksum",
		},
		pgx.CopyFromSlice(
			len(revision.Prerequisites),
			func(index int) ([]any, error) {
				prerequisite := revision.Prerequisites[index]
				return []any{
					prerequisite.ID,
					prerequisite.OrganizationID,
					prerequisite.CampaignRevisionID,
					prerequisite.DownstreamPlanID,
					prerequisite.UpstreamPlanID,
					prerequisite.UpstreamStepKey,
					prerequisite.ProviderPlacementID,
					prerequisite.ProviderDeploymentUnitID,
					prerequisite.ProviderComponentInstanceID,
					prerequisite.ExpectedRuntimeStateChecksum,
				}, nil
			},
		),
	)
	return err
}

func getCampaignRevisionByPublicationKey(
	ctx context.Context,
	draftID uuid.UUID,
	organizationID uuid.UUID,
	idempotencyKey string,
) (*types.CampaignRevision, error) {
	row := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`SELECT
			id,
			published_at,
			organization_id,
			deployment_campaign_draft_id,
			revision_number,
			source_draft_revision,
			name,
			description,
			membership_tag_query,
			risk_policy,
			canonical_payload,
			canonical_checksum,
			published_by_useraccount_id
		FROM DeploymentCampaignRevision
		WHERE deployment_campaign_draft_id = $1
		  AND organization_id = $2
		  AND publication_key = $3`,
		draftID,
		organizationID,
		idempotencyKey,
	)
	revision := &types.CampaignRevision{}
	var riskPolicy []byte
	err := row.Scan(
		&revision.ID,
		&revision.PublishedAt,
		&revision.OrganizationID,
		&revision.CampaignDraftID,
		&revision.RevisionNumber,
		&revision.SourceDraftRevision,
		&revision.Name,
		&revision.Description,
		&revision.MembershipTagQuery,
		&riskPolicy,
		&revision.CanonicalPayload,
		&revision.CanonicalChecksum,
		&revision.PublishedByUserAccountID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(riskPolicy, &revision.RiskPolicy); err != nil {
		return nil, err
	}
	if err := hydrateCampaignRevision(ctx, revision); err != nil {
		return nil, err
	}
	return revision, nil
}

func hydrateCampaignRevision(
	ctx context.Context,
	revision *types.CampaignRevision,
) error {
	waveRows, err := internalctx.GetDb(ctx).Query(
		ctx,
		`SELECT
			id,
			campaign_revision_id,
			organization_id,
			wave_order,
			name,
			bake_seconds,
			maximum_concurrency
		FROM DeploymentCampaignWave
		WHERE campaign_revision_id = $1 AND organization_id = $2
		ORDER BY wave_order, id`,
		revision.ID,
		revision.OrganizationID,
	)
	if err != nil {
		return err
	}
	revision.Waves, err = pgx.CollectRows(
		waveRows,
		pgx.RowToStructByName[types.CampaignWave],
	)
	if err != nil {
		return err
	}
	memberRows, err := internalctx.GetDb(ctx).Query(
		ctx,
		`SELECT
			id,
			campaign_revision_id,
			organization_id,
			deployment_plan_id,
			deployment_unit_id,
			plan_checksum,
			effective_policy_checksum,
			approval_request_id,
			approval_request_revision,
			approval_checksum,
			calendar_version_ids,
			calendar_checksums,
			admission_evaluation_id,
			admission_checksum,
			wave_order,
			member_order
		FROM DeploymentCampaignMember
		WHERE campaign_revision_id = $1 AND organization_id = $2
		ORDER BY wave_order, member_order, deployment_plan_id`,
		revision.ID,
		revision.OrganizationID,
	)
	if err != nil {
		return err
	}
	revision.Members, err = pgx.CollectRows(
		memberRows,
		pgx.RowToStructByName[types.CampaignMember],
	)
	if err != nil {
		return err
	}
	prerequisiteRows, err := internalctx.GetDb(ctx).Query(
		ctx,
		`SELECT
			id,
			campaign_revision_id,
			organization_id,
			downstream_plan_id,
			upstream_plan_id,
			upstream_step_key,
			provider_placement_id,
			provider_deployment_unit_id,
			provider_component_instance_id,
			expected_runtime_state_checksum
		FROM DeploymentCampaignPrerequisite
		WHERE campaign_revision_id = $1 AND organization_id = $2
		ORDER BY
			downstream_plan_id,
			upstream_plan_id,
			upstream_step_key,
			provider_placement_id`,
		revision.ID,
		revision.OrganizationID,
	)
	if err != nil {
		return err
	}
	revision.Prerequisites, err = pgx.CollectRows(
		prerequisiteRows,
		pgx.RowToStructByName[types.CampaignPrerequisite],
	)
	return err
}

func campaignStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func campaignInt64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func campaignContainsUUID(values []uuid.UUID, value uuid.UUID) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

const transitionCampaignSQL = `
UPDATE DeploymentCampaignRun
SET state = @to_state,
    version = version + 1,
    updated_at = @transitioned_at,
    admissions_blocked = @admissions_blocked,
    transition_evidence = transition_evidence || jsonb_build_array(
      jsonb_build_object(
        'from', state,
        'to', @to_state,
        'reason', @reason,
        'actorId', @actor_id,
        'at', @transitioned_at,
        'expectedVersion', @expected_version
      )
    )
WHERE id = @run_id
  AND organization_id = @organization_id
  AND state = @from_state
  AND version = @expected_version
RETURNING
  id,
  created_at,
  updated_at,
  organization_id,
  campaign_revision_id,
  state,
  version,
  current_wave_order,
  current_member_order,
  admissions_blocked,
  COALESCE(resume_state, ''),
  fencing_token,
  COALESCE(lease_holder, ''),
  lease_expires_at`

const admitCampaignMemberSQL = `
WITH updated_run AS (
  UPDATE DeploymentCampaignRun AS campaign_run
  SET version = version + 1,
      current_wave_order = @wave_order,
      current_member_order = @member_order,
      updated_at = @admitted_at
  WHERE campaign_run.id = @run_id
    AND campaign_run.state = 'RUNNING'
    AND campaign_run.admissions_blocked = FALSE
    AND campaign_run.fencing_token = @fencing_token
    AND campaign_run.lease_expires_at > clock_timestamp()
    AND EXISTS (
      SELECT 1
      FROM DeploymentCampaignMemberRun AS pending_member
      WHERE pending_member.id = @member_run_id
        AND pending_member.campaign_run_id = campaign_run.id
        AND pending_member.status = 'PENDING'
    )
  RETURNING campaign_run.id
),
updated_wave AS (
  UPDATE DeploymentCampaignWaveRun AS wave_run
  SET status = 'RUNNING',
      started_at = COALESCE(started_at, @admitted_at)
  FROM updated_run
  WHERE wave_run.id = @wave_run_id
    AND wave_run.campaign_run_id = updated_run.id
  RETURNING wave_run.id
)
UPDATE DeploymentCampaignMemberRun AS member_run
SET status = 'ADMITTED',
    admitted_at = @admitted_at,
    admitted_fencing_token = @fencing_token
FROM updated_run, updated_wave
WHERE member_run.id = @member_run_id
  AND member_run.campaign_run_id = @run_id
  AND member_run.status = 'PENDING'
  AND updated_run.id = member_run.campaign_run_id
  AND updated_wave.id = member_run.wave_run_id`

const loadCampaignScheduleSQL = `
WITH member_frontier AS (
  SELECT
    min(member_run.wave_order) FILTER (
      WHERE member_run.status IN ('PENDING', 'ADMITTED', 'RUNNING')
    ) AS open_wave_order,
    max(member_run.wave_order) AS final_wave_order,
    count(*) > 0
      AND bool_and(member_run.status IN ('SUCCEEDED', 'FAILED', 'EXCLUDED', 'CANCELED'))
      AND NOT bool_or(member_run.execution_uncertain) AS all_members_terminal
  FROM DeploymentCampaignMemberRun AS member_run
  WHERE member_run.campaign_run_id = @run_id
),
current_wave AS (
  SELECT
    COALESCE(member_frontier.open_wave_order, member_frontier.final_wave_order) AS wave_order,
    member_frontier.all_members_terminal
  FROM member_frontier
),
campaign_counts AS (
  SELECT
    count(*) FILTER (WHERE status IN ('ADMITTED', 'RUNNING'))::int AS active
  FROM DeploymentCampaignMemberRun
  WHERE campaign_run_id = @run_id
),
assessment_wave AS (
  SELECT CASE
    WHEN current_wave.all_members_terminal THEN current_wave.wave_order
    WHEN EXISTS (
      SELECT 1
      FROM DeploymentCampaignMemberRun AS member_run
      WHERE member_run.campaign_run_id = @run_id
        AND member_run.wave_order = current_wave.wave_order
        AND member_run.status IN ('SUCCEEDED', 'FAILED')
    ) THEN current_wave.wave_order
    ELSE (
      SELECT max(member_run.wave_order)
      FROM DeploymentCampaignMemberRun AS member_run
      WHERE member_run.campaign_run_id = @run_id
        AND member_run.wave_order < current_wave.wave_order
        AND member_run.status IN ('SUCCEEDED', 'FAILED')
    )
  END AS wave_order
  FROM current_wave
),
current_wave_counts AS (
  SELECT
    count(*) FILTER (WHERE member_run.status IN ('ADMITTED', 'RUNNING'))::int AS active
  FROM DeploymentCampaignMemberRun AS member_run, current_wave
  WHERE member_run.campaign_run_id = @run_id
    AND member_run.wave_order = current_wave.wave_order
),
assessment_counts AS (
  SELECT
    count(*) FILTER (WHERE member_run.status = 'SUCCEEDED')::int AS successful,
    count(*) FILTER (WHERE member_run.status = 'FAILED')::int AS failed
  FROM DeploymentCampaignMemberRun AS member_run, assessment_wave
  WHERE member_run.campaign_run_id = @run_id
    AND member_run.wave_order = assessment_wave.wave_order
),
applicable_bake AS (
  SELECT
    wave_run.completed_at + make_interval(secs => frozen_wave.bake_seconds)
      AS bake_until
  FROM DeploymentCampaignWaveRun AS wave_run
  JOIN current_wave ON TRUE
  JOIN DeploymentCampaignWave AS frozen_wave
    ON frozen_wave.id = wave_run.campaign_wave_id
   AND frozen_wave.campaign_revision_id = wave_run.campaign_revision_id
   AND frozen_wave.organization_id = wave_run.organization_id
   AND frozen_wave.wave_order = wave_run.wave_order
   AND frozen_wave.bake_seconds = wave_run.bake_duration_seconds
   AND frozen_wave.maximum_concurrency = wave_run.maximum_concurrency
  WHERE wave_run.campaign_run_id = @run_id
    AND (
      (current_wave.all_members_terminal AND wave_run.wave_order = current_wave.wave_order)
      OR (NOT current_wave.all_members_terminal AND wave_run.wave_order < current_wave.wave_order)
    )
  ORDER BY wave_run.wave_order DESC
  LIMIT 1
),
applicable_bake_wave AS (
  SELECT EXISTS (
    SELECT 1
    FROM DeploymentCampaignWaveRun AS wave_run
    JOIN current_wave ON TRUE
    WHERE wave_run.campaign_run_id = @run_id
      AND (
        (current_wave.all_members_terminal AND wave_run.wave_order = current_wave.wave_order)
        OR (NOT current_wave.all_members_terminal AND wave_run.wave_order < current_wave.wave_order)
      )
  ) AS present
)
SELECT
  campaign_run.id,
  campaign_run.created_at,
  campaign_run.updated_at,
  campaign_run.organization_id,
  campaign_run.campaign_revision_id,
  campaign_run.state,
  campaign_run.version,
  campaign_run.current_wave_order,
  campaign_run.current_member_order,
  campaign_run.admissions_blocked,
  COALESCE(campaign_run.resume_state, ''),
  campaign_run.fencing_token,
  COALESCE(campaign_run.lease_holder, ''),
  campaign_run.lease_expires_at,
  campaign_run.pause_requested,
  campaign_run.reconciliation_required,
  NOT EXISTS (
    SELECT 1 FROM DeploymentCampaignMemberRun AS active_member
    WHERE active_member.campaign_run_id = campaign_run.id
      AND active_member.status IN ('ADMITTED', 'RUNNING')
  ) AS at_safe_point,
  COALESCE(current_wave.all_members_terminal, FALSE),
  COALESCE(current_wave.wave_order, 0),
  applicable_bake.bake_until,
  COALESCE(frozen_wave.maximum_concurrency, 0),
  COALESCE(current_wave_counts.active, 0),
  COALESCE((revision.risk_policy->>'maximumConcurrency')::int, 0),
  COALESCE(campaign_counts.active, 0),
  COALESCE((revision.risk_policy->>'minimumHealthyBasisPoints')::int, 0),
  COALESCE((revision.risk_policy->>'failureToleranceBasisPoints')::int, 0),
  COALESCE(assessment_counts.successful, 0),
  COALESCE(assessment_counts.failed, 0),
  COALESCE(
    wave_run.campaign_wave_id = frozen_wave.id
    AND wave_run.campaign_revision_id = frozen_wave.campaign_revision_id
    AND wave_run.organization_id = frozen_wave.organization_id
    AND wave_run.wave_order = frozen_wave.wave_order
    AND wave_run.maximum_concurrency = frozen_wave.maximum_concurrency
    AND wave_run.bake_duration_seconds = frozen_wave.bake_seconds,
    FALSE
  ) AS frozen_wave_matches,
  applicable_bake_wave.present
FROM DeploymentCampaignRun AS campaign_run
JOIN DeploymentCampaignRevision AS revision
  ON revision.id = campaign_run.campaign_revision_id
 AND revision.organization_id = campaign_run.organization_id
LEFT JOIN current_wave ON TRUE
LEFT JOIN DeploymentCampaignWave AS frozen_wave
  ON frozen_wave.campaign_revision_id = campaign_run.campaign_revision_id
 AND frozen_wave.organization_id = campaign_run.organization_id
 AND frozen_wave.wave_order = current_wave.wave_order
LEFT JOIN DeploymentCampaignWaveRun AS wave_run
  ON wave_run.campaign_run_id = campaign_run.id
 AND wave_run.organization_id = campaign_run.organization_id
 AND wave_run.wave_order = current_wave.wave_order
LEFT JOIN current_wave_counts ON TRUE
LEFT JOIN assessment_counts ON TRUE
LEFT JOIN campaign_counts ON TRUE
LEFT JOIN applicable_bake ON TRUE
JOIN applicable_bake_wave ON TRUE
WHERE campaign_run.id = @run_id
  AND campaign_run.fencing_token = @fencing_token
  AND campaign_run.lease_expires_at > now()`

const loadCampaignCandidatesSQL = `
SELECT
  campaign_run.organization_id,
  campaign_run.started_by_useraccount_id,
  revision.campaign_draft_id,
  revision.revision_number,
  revision.canonical_checksum,
  member_run.id,
  member_run.wave_run_id,
  member_run.wave_order,
  member_run.member_order,
  member_run.deployment_plan_id,
  frozen_wave.maximum_concurrency
FROM DeploymentCampaignMemberRun AS member_run
JOIN DeploymentCampaignRun AS campaign_run
  ON campaign_run.id = member_run.campaign_run_id
 AND campaign_run.organization_id = member_run.organization_id
JOIN DeploymentCampaignRevision AS revision
  ON revision.id = campaign_run.campaign_revision_id
 AND revision.organization_id = campaign_run.organization_id
JOIN DeploymentCampaignMember AS frozen_member
  ON frozen_member.campaign_revision_id = campaign_run.campaign_revision_id
 AND frozen_member.organization_id = campaign_run.organization_id
 AND frozen_member.deployment_plan_id = member_run.deployment_plan_id
 AND frozen_member.id = member_run.campaign_member_id
 AND frozen_member.deployment_unit_id = member_run.deployment_unit_id
 AND frozen_member.wave_order = member_run.wave_order
 AND frozen_member.member_order = member_run.member_order
JOIN DeploymentCampaignWave AS frozen_wave
  ON frozen_wave.campaign_revision_id = frozen_member.campaign_revision_id
 AND frozen_wave.organization_id = frozen_member.organization_id
 AND frozen_wave.wave_order = frozen_member.wave_order
JOIN DeploymentCampaignWaveRun AS wave_run
  ON wave_run.id = member_run.wave_run_id
 AND wave_run.campaign_run_id = member_run.campaign_run_id
 AND wave_run.organization_id = member_run.organization_id
 AND wave_run.campaign_wave_id = frozen_wave.id
 AND wave_run.campaign_revision_id = frozen_wave.campaign_revision_id
 AND wave_run.wave_order = frozen_wave.wave_order
 AND wave_run.maximum_concurrency = frozen_wave.maximum_concurrency
 AND wave_run.bake_duration_seconds = frozen_wave.bake_seconds
WHERE campaign_run.id = @run_id
  AND campaign_run.fencing_token = @fencing_token
  AND campaign_run.lease_expires_at > now()
  AND member_run.wave_order = @current_wave_order
  AND member_run.status = 'PENDING'
ORDER BY
  member_run.wave_order,
  member_run.member_order,
  member_run.deployment_plan_id`

const completeCampaignRunSQL = `
WITH member_frontier AS (
  SELECT
    count(*) > 0 AS has_members,
    bool_and(member.status IN ('SUCCEEDED', 'FAILED', 'EXCLUDED', 'CANCELED')) AS all_terminal,
    bool_or(member.execution_uncertain) AS any_uncertain,
    max(member.wave_order) AS final_wave_order
  FROM DeploymentCampaignMemberRun AS member
  WHERE member.campaign_run_id = @run_id
), final_member_counts AS (
  SELECT
    count(*) FILTER (WHERE member.status = 'SUCCEEDED')::int AS successful,
    count(*) FILTER (WHERE member.status = 'FAILED')::int AS failed
  FROM DeploymentCampaignMemberRun AS member
  JOIN member_frontier ON member.wave_order = member_frontier.final_wave_order
  WHERE member.campaign_run_id = @run_id
), final_wave AS (
  SELECT wave_run.*, frozen_wave.bake_seconds
  FROM DeploymentCampaignWaveRun AS wave_run
  JOIN member_frontier ON TRUE
  JOIN DeploymentCampaignRun AS final_run
    ON final_run.id = wave_run.campaign_run_id
  JOIN DeploymentCampaignWave AS frozen_wave
    ON frozen_wave.id = wave_run.campaign_wave_id
   AND frozen_wave.campaign_revision_id = final_run.campaign_revision_id
   AND frozen_wave.organization_id = final_run.organization_id
   AND frozen_wave.wave_order = wave_run.wave_order
   AND frozen_wave.maximum_concurrency = wave_run.maximum_concurrency
   AND frozen_wave.bake_seconds = wave_run.bake_duration_seconds
  WHERE wave_run.campaign_run_id = @run_id
    AND wave_run.wave_order = member_frontier.final_wave_order
)
UPDATE DeploymentCampaignRun AS campaign_run
SET state = 'COMPLETED',
    admissions_blocked = TRUE,
    version = campaign_run.version + 1,
    updated_at = @completed_at,
    transition_evidence = campaign_run.transition_evidence || jsonb_build_array(
      jsonb_build_object(
		'from', 'RUNNING',
        'to', 'COMPLETED',
        'reason', 'all campaign members reached a healthy terminal state',
        'thresholdEvaluationId', @threshold_evaluation_id,
		'fencingToken', @fencing_token,
        'at', @completed_at
      )
    )
FROM CampaignThresholdEvaluation AS threshold, member_frontier, final_member_counts,
     final_wave AS wave_run
JOIN DeploymentCampaignWave AS frozen_wave
  ON frozen_wave.id = wave_run.campaign_wave_id
 AND frozen_wave.campaign_revision_id = wave_run.campaign_revision_id
 AND frozen_wave.organization_id = wave_run.organization_id
 AND frozen_wave.wave_order = wave_run.wave_order
WHERE campaign_run.id = @run_id
  AND campaign_run.state = 'RUNNING'
  AND campaign_run.admissions_blocked = FALSE
  AND campaign_run.pause_requested = FALSE
  AND campaign_run.reconciliation_required = FALSE
  AND campaign_run.fencing_token = @fencing_token
  AND campaign_run.lease_expires_at > clock_timestamp()
  AND threshold.id = @threshold_evaluation_id
  AND threshold.campaign_run_id = campaign_run.id
  AND threshold.organization_id = campaign_run.organization_id
  AND threshold.fencing_token = @fencing_token
  AND threshold.breached = FALSE
  AND threshold.samples = final_member_counts.successful + final_member_counts.failed
  AND threshold.successful = final_member_counts.successful
  AND threshold.failed = final_member_counts.failed
  AND member_frontier.has_members
  AND member_frontier.all_terminal
  AND member_frontier.any_uncertain = FALSE
  AND wave_run.completed_at IS NOT NULL
  AND wave_run.completed_at + make_interval(secs => frozen_wave.bake_seconds) <= @completed_at`

const excludePendingCampaignMemberSQL = `
UPDATE DeploymentCampaignMemberRun
SET status = 'EXCLUDED',
    completed_at = COALESCE(completed_at, @completed_at)
WHERE id = @member_run_id
  AND campaign_run_id = @run_id
  AND organization_id = @organization_id
  AND status = 'PENDING'
RETURNING wave_run_id`

const loadCandidatePrerequisitesSQL = `
SELECT
  frozen_prerequisite.organization_id,
  frozen_prerequisite.upstream_plan_id,
  frozen_prerequisite.upstream_step_key,
  frozen_prerequisite.provider_placement_id,
  frozen_prerequisite.provider_deployment_unit_id,
  frozen_prerequisite.provider_component_instance_id,
  frozen_prerequisite.expected_runtime_state_checksum
FROM DeploymentCampaignPrerequisite AS frozen_prerequisite
JOIN DeploymentCampaignRun AS campaign_run
  ON campaign_run.campaign_revision_id = frozen_prerequisite.campaign_revision_id
 AND campaign_run.organization_id = frozen_prerequisite.organization_id
WHERE campaign_run.id = @run_id
  AND campaign_run.fencing_token = @fencing_token
  AND campaign_run.lease_expires_at > now()
  AND frozen_prerequisite.downstream_plan_id = @downstream_plan_id
ORDER BY
  frozen_prerequisite.upstream_plan_id,
  frozen_prerequisite.upstream_step_key,
  frozen_prerequisite.provider_placement_id`

const lockCampaignRunForControlSQL = `
SELECT
  id,
  created_at,
  updated_at,
  organization_id,
  campaign_revision_id,
  state,
  version,
  current_wave_order,
  current_member_order,
  admissions_blocked,
  COALESCE(resume_state, ''),
  fencing_token,
  COALESCE(lease_holder, ''),
  lease_expires_at
FROM DeploymentCampaignRun
WHERE id = @run_id AND organization_id = @organization_id
FOR UPDATE`

const insertCampaignControlSQL = `
INSERT INTO CampaignControlRequest (
  id,
  request_id,
  requested_at,
  organization_id,
  campaign_run_id,
  member_run_id,
  actor_useraccount_id,
  control_kind,
  expected_run_version,
  reason,
  request_checksum,
  status,
  resulting_run_version,
  response_snapshot
) VALUES (
  @id,
  @request_id,
  @requested_at,
  @organization_id,
  @campaign_run_id,
  @member_run_id,
  @actor_useraccount_id,
  @control_kind,
  @expected_run_version,
  @reason,
  @request_checksum,
  @status,
  @resulting_run_version,
  @response_snapshot
)
ON CONFLICT (organization_id, request_id) DO NOTHING`

const lookupCampaignControlReplaySQL = `
SELECT request_checksum, response_snapshot
FROM CampaignControlRequest
WHERE organization_id = @organization_id
  AND request_id = @request_id`

const lockCampaignControlRequestSQL = `
SELECT pg_advisory_xact_lock(hashtextextended(
  CAST(@organization_id AS text) || ':' || CAST(@request_id AS text),
  0
))`

const applyCampaignControlSQL = `
UPDATE DeploymentCampaignRun
SET state = @state,
    version = @resulting_version,
    updated_at = @updated_at,
    admissions_blocked = @admissions_blocked,
    resume_state = NULLIF(@resume_state, ''),
    pause_requested = @pause_requested,
    reconciliation_required = @reconciliation_required,
    transition_evidence = transition_evidence || jsonb_build_array(
      jsonb_build_object(
        'controlRequestId', @request_id,
        'kind', @control_kind,
        'reason', @reason,
        'at', @updated_at
      )
    )
WHERE id = @run_id
  AND organization_id = @organization_id
  AND version = @expected_version`

const applyCampaignExclusionVersionSQL = `
UPDATE DeploymentCampaignRun
SET version = version + 1,
    updated_at = @updated_at,
    transition_evidence = transition_evidence || jsonb_build_array(
      jsonb_build_object(
        'controlRequestId', @request_id,
        'kind', @control_kind,
        'memberRunId', @member_run_id,
        'reason', @reason,
        'at', @updated_at
      )
    )
WHERE id = @run_id
  AND organization_id = @organization_id
  AND version = @expected_version
  AND state NOT IN ('FAILED', 'COMPLETED', 'CANCELED')`

const instantiateCampaignRunSQL = `
WITH selected_revision AS (
  SELECT id, organization_id
  FROM DeploymentCampaignRevision
  WHERE id = @campaign_revision_id AND organization_id = @organization_id
), inserted_run AS (
  INSERT INTO DeploymentCampaignRun (
    id, created_at, updated_at, organization_id, campaign_revision_id,
    started_by_useraccount_id, state, version, transition_evidence
  )
  SELECT
    @run_id, @started_at, @started_at, organization_id, id,
    @actor_id, 'DRAFT', 1, '[]'::jsonb
  FROM selected_revision
  RETURNING *
), inserted_waves AS (
  INSERT INTO DeploymentCampaignWaveRun (
    id, created_at, campaign_run_id, organization_id, campaign_wave_id,
    campaign_revision_id, wave_order, maximum_concurrency, bake_duration_seconds
  )
  SELECT gen_random_uuid(), @started_at, run.id, wave.organization_id, wave.id,
    wave.campaign_revision_id, wave.wave_order, wave.maximum_concurrency, wave.bake_seconds
  FROM inserted_run run
  JOIN DeploymentCampaignWave wave
    ON wave.campaign_revision_id = run.campaign_revision_id
   AND wave.organization_id = run.organization_id
  RETURNING *
), inserted_members AS (
  INSERT INTO DeploymentCampaignMemberRun (
    id, created_at, campaign_run_id, wave_run_id, organization_id,
    campaign_member_id, campaign_revision_id, deployment_plan_id,
    deployment_unit_id, wave_order, member_order
  )
  SELECT gen_random_uuid(), @started_at, run.id, wave_run.id, member.organization_id,
    member.id, member.campaign_revision_id, member.deployment_plan_id,
    member.deployment_unit_id, member.wave_order, member.member_order
  FROM inserted_run run
  JOIN DeploymentCampaignMember member
    ON member.campaign_revision_id = run.campaign_revision_id
   AND member.organization_id = run.organization_id
  JOIN inserted_waves wave_run
    ON wave_run.campaign_run_id = run.id
   AND wave_run.wave_order = member.wave_order
  RETURNING id
)
SELECT
  run.id, run.created_at, run.updated_at, run.organization_id,
  run.campaign_revision_id, run.state, run.version,
  run.current_wave_order, run.current_member_order, run.admissions_blocked,
  '', run.fencing_token, COALESCE(run.lease_holder, ''), run.lease_expires_at,
  (SELECT count(*) FROM inserted_waves),
  (SELECT count(*) FROM inserted_members),
  (SELECT count(*) FROM DeploymentCampaignWave wave
    WHERE wave.campaign_revision_id = run.campaign_revision_id
      AND wave.organization_id = run.organization_id),
  (SELECT count(*) FROM DeploymentCampaignMember member
    WHERE member.campaign_revision_id = run.campaign_revision_id
      AND member.organization_id = run.organization_id)
FROM inserted_run run`

const recordPrerequisitesAndAdmitSQL = `
WITH inserted_evidence AS (
  INSERT INTO CampaignPrerequisiteEvaluation (
    id, evaluated_at, campaign_run_id, member_run_id, organization_id,
    upstream_plan_id, step_key, expected_runtime_state_checksum,
    actual_observation_id, actual_observation_organization_id,
    actual_runtime_state_checksum, matched, reason, fencing_token
  )
  SELECT
    evidence.id, @evaluated_at, @run_id, @member_run_id, run.organization_id,
    evidence.upstream_plan_id, evidence.step_key,
    evidence.expected_runtime_state_checksum,
    NULLIF(evidence.actual_observation_id, '00000000-0000-0000-0000-000000000000'::uuid),
    CASE WHEN evidence.actual_observation_id = '00000000-0000-0000-0000-000000000000'::uuid
      THEN NULL ELSE run.organization_id END,
    NULLIF(evidence.actual_runtime_state_checksum, ''), evidence.matched,
    evidence.reason, @fencing_token
  FROM DeploymentCampaignRun run
  CROSS JOIN jsonb_to_recordset(@evaluations::jsonb) AS evidence(
    id uuid, upstream_plan_id uuid, step_key text,
    expected_runtime_state_checksum text, actual_observation_id uuid,
    actual_runtime_state_checksum text, matched boolean, reason text
  )
  WHERE run.id = @run_id AND run.fencing_token = @fencing_token
    AND run.lease_expires_at > clock_timestamp()
  RETURNING matched
), updated_run AS (
  UPDATE DeploymentCampaignRun run
  SET state = CASE WHEN EXISTS (SELECT 1 FROM inserted_evidence WHERE NOT matched)
      THEN 'PAUSED' ELSE run.state END,
      admissions_blocked = EXISTS (SELECT 1 FROM inserted_evidence WHERE NOT matched),
      version = CASE WHEN EXISTS (SELECT 1 FROM inserted_evidence WHERE NOT matched)
        THEN version + 1 ELSE version END,
      updated_at = @evaluated_at
  WHERE run.id = @run_id AND run.fencing_token = @fencing_token
  RETURNING run.id
)
UPDATE DeploymentCampaignMemberRun member_run
SET status = 'ADMITTED', admitted_at = @evaluated_at,
    admitted_fencing_token = @fencing_token
FROM updated_run
WHERE member_run.id = @member_run_id AND member_run.campaign_run_id = @run_id
  AND member_run.wave_run_id = @wave_run_id AND member_run.status = 'PENDING'
  AND NOT EXISTS (SELECT 1 FROM inserted_evidence WHERE NOT matched)`

const recordThresholdAndMaybePauseSQL = `
WITH inserted_evaluation AS (
  INSERT INTO CampaignThresholdEvaluation (
    id, evaluated_at, campaign_run_id, organization_id, samples,
    successful, failed, failure_rate, maximum_failure_rate, breached, fencing_token
  )
  SELECT @id, @evaluated_at, @campaign_run_id, run.organization_id, @samples,
    @successful, @failed, @failure_rate, @maximum_failure_rate, @breached, @fencing_token
  FROM DeploymentCampaignRun run
  WHERE run.id = @campaign_run_id AND run.fencing_token = @fencing_token
    AND run.lease_expires_at > clock_timestamp()
  RETURNING breached
)
UPDATE DeploymentCampaignRun run
SET state = CASE WHEN evaluation.breached THEN 'PAUSED' ELSE run.state END,
    admissions_blocked = run.admissions_blocked OR evaluation.breached,
    version = CASE WHEN evaluation.breached THEN version + 1 ELSE version END,
    updated_at = @evaluated_at
FROM inserted_evaluation evaluation
WHERE run.id = @campaign_run_id AND run.fencing_token = @fencing_token`

type CampaignRepository struct{}

func (CampaignRepository) GetCampaignRun(
	ctx context.Context,
	runID uuid.UUID,
	organizationID uuid.UUID,
) (*types.CampaignRun, error) {
	return GetDeploymentCampaignRun(ctx, runID, organizationID)
}

func (CampaignRepository) StartCampaignRun(
	ctx context.Context,
	input types.CampaignRunStartInput,
) (*types.CampaignRun, error) {
	if input.StartedAt.IsZero() {
		input.StartedAt = time.Now().UTC()
	}
	tx, err := internalctx.GetDb(ctx).Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	runID := uuid.New()
	var run types.CampaignRun
	var waveCount, memberCount, expectedWaves, expectedMembers int
	err = tx.QueryRow(ctx, instantiateCampaignRunSQL, pgx.NamedArgs{
		"run_id": runID, "started_at": input.StartedAt, "organization_id": input.OrganizationID,
		"campaign_revision_id": input.CampaignRevisionID, "actor_id": input.ActorID,
	}).Scan(
		&run.ID, &run.CreatedAt, &run.UpdatedAt, &run.OrganizationID,
		&run.CampaignRevisionID, &run.State, &run.Version,
		&run.CurrentWaveOrder, &run.CurrentMemberOrder, &run.AdmissionsBlocked,
		&run.ResumeState, &run.FencingToken, &run.LeaseHolder, &run.LeaseExpiresAt,
		&waveCount, &memberCount, &expectedWaves, &expectedMembers,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if waveCount == 0 || memberCount == 0 || waveCount != expectedWaves || memberCount != expectedMembers {
		return nil, apierrors.NewConflict("campaign runtime instantiation was incomplete")
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &run, nil
}

func (CampaignRepository) TransitionCampaignRun(
	ctx context.Context,
	transition types.CampaignTransition,
) (*types.CampaignRun, error) {
	return transitionCampaignWithGuard(ctx, transition, campaigns.IsCampaignPreRunTransition)
}

func PauseCampaign(ctx context.Context, input types.CampaignControlInput) error {
	input.Kind = types.CampaignControlKindPause
	_, err := (CampaignRepository{}).ApplyCampaignControl(ctx, input)
	return err
}

func ResumeCampaign(ctx context.Context, input types.CampaignControlInput) error {
	input.Kind = types.CampaignControlKindResume
	_, err := (CampaignRepository{}).ApplyCampaignControl(ctx, input)
	return err
}

func CancelCampaign(ctx context.Context, input types.CampaignControlInput) error {
	input.Kind = types.CampaignControlKindCancel
	_, err := (CampaignRepository{}).ApplyCampaignControl(ctx, input)
	return err
}

func ExcludeCampaignMember(
	ctx context.Context,
	input types.CampaignMemberControlInput,
) error {
	input.Kind = types.CampaignControlKindExclude
	_, err := (CampaignRepository{}).ExcludeCampaignMember(ctx, input)
	return err
}

func RetryCampaignMember(
	ctx context.Context,
	input types.CampaignMemberControlInput,
) (*types.DeploymentPlan, error) {
	repository := CampaignRepository{}
	return campaigns.NewCampaignController(repository, repository).
		RetryCampaignMember(ctx, input)
}

func (CampaignRepository) RetryCampaignMember(
	ctx context.Context,
	input types.CampaignMemberControlInput,
) (*types.DeploymentPlan, error) {
	return RetryCampaignMember(ctx, input)
}

func (CampaignRepository) CreateSupersedingPlan(
	ctx context.Context,
	input types.CampaignMemberControlInput,
) (*types.DeploymentPlan, error) {
	var sourcePlanID uuid.UUID
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
SELECT deployment_plan_id
FROM DeploymentCampaignMemberRun
WHERE id = @member_run_id
  AND campaign_run_id = @run_id
  AND organization_id = @organization_id`, pgx.NamedArgs{
		"member_run_id": input.MemberRunID, "run_id": input.RunID,
		"organization_id": input.OrganizationID,
	}).Scan(&sourcePlanID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	source, err := getDeploymentPlan(ctx, sourcePlanID, input.OrganizationID)
	if err != nil {
		return nil, err
	}
	targetIDs := make([]uuid.UUID, 0, len(source.Targets))
	for _, target := range source.Targets {
		targetIDs = append(targetIDs, target.DeploymentTargetID)
	}
	return CreateDeploymentPlan(ctx, types.CreateDeploymentPlanRequest{
		OrganizationID: input.OrganizationID, ReleaseBundleID: source.ReleaseBundleID,
		EnvironmentID: source.EnvironmentID, TargetIDs: targetIDs,
		DeploymentUnitID: source.DeploymentUnitID,
	})
}

func (CampaignRepository) PersistCampaignMemberRetry(
	ctx context.Context,
	input types.CampaignMemberControlInput,
	creator campaigns.SupersedingPlanCreator,
) (*types.DeploymentPlan, error) {
	if creator == nil {
		return nil, errors.New("campaign v1 superseding plan creator is not wired")
	}
	if input.RequestedAt.IsZero() {
		input.RequestedAt = time.Now().UTC()
	}
	input.Kind = types.CampaignControlKindRetry
	checksum := campaignRetryChecksum(input)
	tx, err := internalctx.GetDb(ctx).Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	txCtx := internalctx.WithDb(ctx, tx)
	if _, err := tx.Exec(ctx, lockCampaignControlRequestSQL, pgx.NamedArgs{
		"organization_id": input.OrganizationID,
		"request_id":      input.RequestID,
	}); err != nil {
		return nil, err
	}
	if plan, found, err := lookupCampaignRetryReplay(ctx, tx, input, checksum); err != nil {
		return nil, err
	} else if found {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return plan, nil
	}
	run, err := scanCampaignRun(tx.QueryRow(ctx, lockCampaignRunForControlSQL, pgx.NamedArgs{
		"run_id": input.RunID, "organization_id": input.OrganizationID,
	}))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	updatedRun, err := campaigns.DecideCampaignMemberMutation(*run, input)
	if err != nil {
		return nil, apierrors.NewConflict(err.Error())
	}
	var memberStatus string
	if err := tx.QueryRow(ctx, `
SELECT status
FROM DeploymentCampaignMemberRun
WHERE id = @member_run_id
  AND campaign_run_id = @run_id
  AND organization_id = @organization_id
FOR UPDATE`, pgx.NamedArgs{
		"member_run_id": input.MemberRunID, "run_id": input.RunID,
		"organization_id": input.OrganizationID,
	}).Scan(&memberStatus); errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if err := campaigns.ValidateCampaignMemberRetryStatus(memberStatus); err != nil {
		return nil, apierrors.NewConflict(err.Error())
	}
	plan, err := creator.CreateSupersedingPlan(txCtx, input)
	if err != nil {
		return nil, err
	}
	response, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	result := types.CampaignControlResult{
		RequestID: input.RequestID, Status: types.CampaignControlStatusApplied, Run: updatedRun,
	}
	controlID := uuid.New()
	tag, err := tx.Exec(ctx, insertCampaignControlSQL, campaignControlArgs(
		controlID, input.CampaignControlInput, input.MemberRunID, checksum, result, response,
	))
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() != 1 {
		return nil, apierrors.NewConflict("campaign retry idempotency serialization failed")
	}
	tag, err = tx.Exec(ctx, applyCampaignExclusionVersionSQL, pgx.NamedArgs{
		"updated_at": input.RequestedAt, "request_id": input.RequestID,
		"control_kind": input.Kind, "member_run_id": input.MemberRunID,
		"reason": input.Reason, "run_id": input.RunID,
		"organization_id": input.OrganizationID, "expected_version": input.ExpectedVersion,
	})
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() != 1 {
		return nil, apierrors.NewConflict("campaign retry lost optimistic update")
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return plan, nil
}

func (CampaignRepository) ApplyCampaignControl(
	ctx context.Context,
	input types.CampaignControlInput,
) (*types.CampaignControlResult, error) {
	if input.RequestedAt.IsZero() {
		input.RequestedAt = time.Now().UTC()
	}
	checksum := campaignControlChecksum(input, uuid.Nil)
	tx, err := internalctx.GetDb(ctx).Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	txCtx := internalctx.WithDb(ctx, tx)

	if existing, found, err := lookupCampaignControlReplay(ctx, tx, input, checksum); err != nil {
		return nil, err
	} else if found {
		existing.Duplicate = true
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return existing, nil
	}

	run, err := scanCampaignRun(tx.QueryRow(ctx, lockCampaignRunForControlSQL, pgx.NamedArgs{
		"run_id":          input.RunID,
		"organization_id": input.OrganizationID,
	}))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if existing, found, err := lookupCampaignControlReplay(ctx, tx, input, checksum); err != nil {
		return nil, err
	} else if found {
		existing.Duplicate = true
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return existing, nil
	}
	if err := tx.QueryRow(ctx, `
SELECT pause_requested, reconciliation_required
FROM DeploymentCampaignRun
WHERE id = @run_id AND organization_id = @organization_id`,
		pgx.NamedArgs{
			"run_id":          input.RunID,
			"organization_id": input.OrganizationID,
		},
	).Scan(&run.PauseRequested, &run.ReconciliationRequired); err != nil {
		return nil, err
	}

	facts, err := campaignControlFacts(txCtx, input.RunID)
	if err != nil {
		return nil, err
	}
	decision, err := campaigns.DecideCampaignControl(*run, input, facts)
	if err != nil {
		return nil, apierrors.NewConflict(err.Error())
	}
	result := types.CampaignControlResult{
		RequestID:              input.RequestID,
		Status:                 decision.Status,
		Run:                    decision.Run,
		PausePending:           decision.PausePending,
		ReconciliationRequired: decision.ReconciliationRequired,
	}
	if shouldFanoutCampaignCancel(input, decision) {
		if err := applyCampaignCancelFanout(txCtx, input); err != nil {
			return nil, err
		}
	}
	response, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	controlID := uuid.New()
	tag, err := tx.Exec(ctx, insertCampaignControlSQL, campaignControlArgs(
		controlID,
		input,
		uuid.Nil,
		checksum,
		result,
		response,
	))
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		existing, err := getDuplicateCampaignControl(ctx, tx, input, checksum)
		if err != nil {
			return nil, err
		}
		existing.Duplicate = true
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return existing, nil
	}

	tag, err = tx.Exec(ctx, applyCampaignControlSQL, pgx.NamedArgs{
		"state":                   decision.Run.State,
		"resulting_version":       decision.Run.Version,
		"updated_at":              input.RequestedAt,
		"admissions_blocked":      decision.Run.AdmissionsBlocked,
		"resume_state":            decision.Run.ResumeState,
		"pause_requested":         decision.PausePending,
		"reconciliation_required": decision.ReconciliationRequired,
		"request_id":              input.RequestID,
		"control_kind":            input.Kind,
		"reason":                  input.Reason,
		"run_id":                  input.RunID,
		"organization_id":         input.OrganizationID,
		"expected_version":        input.ExpectedVersion,
	})
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() != 1 {
		return nil, apierrors.NewConflict("campaign control lost optimistic update")
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &result, nil
}

func (CampaignRepository) ExcludeCampaignMember(
	ctx context.Context,
	input types.CampaignMemberControlInput,
) (*types.CampaignExclusion, error) {
	if input.RequestedAt.IsZero() {
		input.RequestedAt = time.Now().UTC()
	}
	checksum := campaignControlChecksum(input.CampaignControlInput, input.MemberRunID)
	tx, err := internalctx.GetDb(ctx).Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, found, err := lookupCampaignControlReplay(
		ctx,
		tx,
		input.CampaignControlInput,
		checksum,
	); err != nil {
		return nil, err
	} else if found {
		existing, err := getCampaignExclusionReplay(ctx, tx, input)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return existing, nil
	}

	run, err := scanCampaignRun(tx.QueryRow(ctx, lockCampaignRunForControlSQL, pgx.NamedArgs{
		"run_id":          input.RunID,
		"organization_id": input.OrganizationID,
	}))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if _, found, err := lookupCampaignControlReplay(
		ctx,
		tx,
		input.CampaignControlInput,
		checksum,
	); err != nil {
		return nil, err
	} else if found {
		existing, err := getCampaignExclusionReplay(ctx, tx, input)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return existing, nil
	}
	updatedRun, err := campaigns.DecideCampaignMemberMutation(*run, input)
	if err != nil {
		return nil, apierrors.NewConflict(err.Error())
	}

	var memberStatus string
	var memberWaveRunID uuid.UUID
	err = tx.QueryRow(ctx, `
SELECT status, wave_run_id
FROM DeploymentCampaignMemberRun
WHERE id = @member_run_id
  AND campaign_run_id = @run_id
  AND organization_id = @organization_id
FOR UPDATE`, pgx.NamedArgs{
		"member_run_id":   input.MemberRunID,
		"run_id":          input.RunID,
		"organization_id": input.OrganizationID,
	}).Scan(&memberStatus, &memberWaveRunID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	exclusion, err := campaigns.BuildCampaignExclusion(input, types.CampaignExclusionFacts{
		Authorized:  true,
		WasAdmitted: memberStatus != "PENDING",
	})
	if err != nil {
		return nil, err
	}
	result := types.CampaignControlResult{
		RequestID: input.RequestID,
		Status:    types.CampaignControlStatusApplied,
		Run:       updatedRun,
	}
	response, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	controlID := uuid.New()
	tag, err := tx.Exec(ctx, insertCampaignControlSQL, campaignControlArgs(
		controlID,
		input.CampaignControlInput,
		input.MemberRunID,
		checksum,
		result,
		response,
	))
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		if _, found, replayErr := lookupCampaignControlReplay(
			ctx, tx, input.CampaignControlInput, checksum,
		); replayErr != nil {
			return nil, replayErr
		} else if !found {
			return nil, apierrors.NewConflict("campaign exclusion idempotency serialization failed")
		}
		existing, err := getCampaignExclusionReplay(ctx, tx, input)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return existing, nil
	}
	exclusion.ControlRequestID = controlID
	_, err = tx.Exec(ctx, `
INSERT INTO CampaignExclusion (
  id, excluded_at, organization_id, campaign_run_id, member_run_id,
  control_request_id, excluded_by_useraccount_id, reason,
  visible_incomplete, drift_reason
) VALUES (
  @id, @excluded_at, @organization_id, @campaign_run_id, @member_run_id,
  @control_request_id, @excluded_by_useraccount_id, @reason,
  @visible_incomplete, @drift_reason
)`, pgx.NamedArgs{
		"id":                         exclusion.ID,
		"excluded_at":                exclusion.ExcludedAt,
		"organization_id":            exclusion.OrganizationID,
		"campaign_run_id":            exclusion.CampaignRunID,
		"member_run_id":              exclusion.MemberRunID,
		"control_request_id":         exclusion.ControlRequestID,
		"excluded_by_useraccount_id": exclusion.ExcludedByActorID,
		"reason":                     exclusion.Reason,
		"visible_incomplete":         exclusion.VisibleIncomplete,
		"drift_reason":               exclusion.DriftReason,
	})
	if err != nil {
		return nil, err
	}
	if memberStatus == "PENDING" {
		err = tx.QueryRow(ctx, excludePendingCampaignMemberSQL, pgx.NamedArgs{
			"member_run_id":   input.MemberRunID,
			"run_id":          input.RunID,
			"organization_id": input.OrganizationID,
			"completed_at":    input.RequestedAt,
		}).Scan(&memberWaveRunID)
		if err != nil {
			return nil, err
		}
		if err := projectCampaignWaveExecution(
			internalctx.WithDb(ctx, tx), input.OrganizationID, memberWaveRunID,
		); err != nil {
			return nil, err
		}
	}
	tag, err = tx.Exec(ctx, applyCampaignExclusionVersionSQL, pgx.NamedArgs{
		"updated_at":       input.RequestedAt,
		"request_id":       input.RequestID,
		"control_kind":     input.Kind,
		"member_run_id":    input.MemberRunID,
		"reason":           input.Reason,
		"run_id":           input.RunID,
		"organization_id":  input.OrganizationID,
		"expected_version": input.ExpectedVersion,
	})
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() != 1 {
		return nil, apierrors.NewConflict("campaign member control lost optimistic update")
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &exclusion, nil
}

func TransitionCampaign(
	ctx context.Context,
	transition types.CampaignTransition,
) (*types.CampaignRun, error) {
	return transitionCampaignWithGuard(ctx, transition, nil)
}

func transitionCampaignWithGuard(
	ctx context.Context,
	transition types.CampaignTransition,
	guard func(types.CampaignRunState, types.CampaignRunState) bool,
) (*types.CampaignRun, error) {
	var current types.CampaignRun
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
SELECT state, version
FROM DeploymentCampaignRun
WHERE id = @run_id AND organization_id = @organization_id`,
		pgx.NamedArgs{
			"run_id":          transition.RunID,
			"organization_id": transition.OrganizationID,
		},
	).Scan(&current.State, &current.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if guard != nil && !guard(current.State, transition.To) {
		return nil, apierrors.NewConflict(
			"public campaign transitions are limited to the pre-run lifecycle; use operational controls after start",
		)
	}
	if _, err := campaigns.NextCampaignRun(current, transition); err != nil {
		return nil, apierrors.NewConflict(err.Error())
	}

	at := transition.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	blocked := transition.To == types.CampaignRunStatePaused ||
		transition.To == types.CampaignRunStateFailed ||
		transition.To == types.CampaignRunStateCompleted ||
		transition.To == types.CampaignRunStateCanceled
	run, err := scanCampaignRun(internalctx.GetDb(ctx).QueryRow(ctx, transitionCampaignSQL, pgx.NamedArgs{
		"run_id":             transition.RunID,
		"organization_id":    transition.OrganizationID,
		"expected_version":   transition.ExpectedVersion,
		"from_state":         current.State,
		"to_state":           transition.To,
		"reason":             transition.Reason,
		"actor_id":           transition.ActorID,
		"transitioned_at":    at,
		"admissions_blocked": blocked,
	}))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.NewConflict("campaign run changed or does not exist")
	}
	return run, err
}

func EvaluateNextWaveAdmission(
	ctx context.Context,
	runID uuid.UUID,
	now time.Time,
) (types.WaveAdmission, error) {
	row := internalctx.GetDb(ctx).QueryRow(ctx, `
SELECT
  member_run.id,
  member_run.wave_run_id,
  member_run.wave_order,
  member_run.member_order,
  member_run.deployment_plan_id
FROM DeploymentCampaignMemberRun AS member_run
JOIN DeploymentCampaignRun AS campaign_run
  ON campaign_run.id = member_run.campaign_run_id
WHERE member_run.campaign_run_id = @run_id
  AND member_run.status = 'PENDING'
  AND campaign_run.state = 'RUNNING'
  AND campaign_run.admissions_blocked = FALSE
  AND (
    campaign_run.lease_expires_at IS NULL
    OR campaign_run.lease_expires_at > clock_timestamp()
  )
ORDER BY
  member_run.wave_order,
  member_run.member_order,
  member_run.deployment_plan_id
LIMIT 1`, pgx.NamedArgs{"run_id": runID, "evaluated_at": now})
	var candidate types.CampaignMemberCandidate
	err := row.Scan(
		&candidate.MemberRunID,
		&candidate.WaveRunID,
		&candidate.WaveOrder,
		&candidate.MemberOrder,
		&candidate.PlanID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return types.WaveAdmission{
			CampaignRunID: runID,
			Allowed:       false,
			Reason:        "no admissible campaign member",
		}, nil
	}
	if err != nil {
		return types.WaveAdmission{}, err
	}
	return types.WaveAdmission{CampaignRunID: runID, Candidate: &candidate, Allowed: true}, nil
}

func RecordCampaignPrerequisiteEvaluation(
	ctx context.Context,
	evaluation types.CampaignPrerequisiteEvaluation,
) error {
	tag, err := internalctx.GetDb(ctx).Exec(ctx, `
INSERT INTO CampaignPrerequisiteEvaluation (
  id,
  evaluated_at,
  campaign_run_id,
  member_run_id,
  organization_id,
  upstream_plan_id,
  step_key,
  expected_runtime_state_checksum,
  actual_observation_id,
  actual_observation_organization_id,
  actual_runtime_state_checksum,
  matched,
  reason,
  fencing_token
)
SELECT
  @id,
  @evaluated_at,
  @campaign_run_id,
  @member_run_id,
  campaign_run.organization_id,
  @upstream_plan_id,
  @step_key,
  @expected_runtime_state_checksum,
  NULLIF(@actual_observation_id, '00000000-0000-0000-0000-000000000000'::uuid),
  CASE
    WHEN @actual_observation_id = '00000000-0000-0000-0000-000000000000'::uuid
      THEN NULL
    ELSE campaign_run.organization_id
  END,
  NULLIF(@actual_runtime_state_checksum, ''),
  @matched,
  @reason,
  @fencing_token
FROM DeploymentCampaignRun AS campaign_run
WHERE campaign_run.id = @campaign_run_id
  AND campaign_run.fencing_token = @fencing_token
  AND campaign_run.lease_expires_at > clock_timestamp()`, prerequisiteEvaluationArgs(evaluation))
	if err != nil {
		return err
	}
	return campaigns.RequireCampaignLease(tag.RowsAffected())
}

func RecordThresholdEvaluation(
	ctx context.Context,
	evaluation types.CampaignThresholdEvaluation,
) error {
	tag, err := internalctx.GetDb(ctx).Exec(ctx, `
INSERT INTO CampaignThresholdEvaluation (
  id,
  evaluated_at,
  campaign_run_id,
  organization_id,
  samples,
  successful,
  failed,
  failure_rate,
  maximum_failure_rate,
  breached,
  fencing_token
)
SELECT
  @id,
  @evaluated_at,
  @campaign_run_id,
  campaign_run.organization_id,
  @samples,
  @successful,
  @failed,
  @failure_rate,
  @maximum_failure_rate,
  @breached,
  @fencing_token
FROM DeploymentCampaignRun AS campaign_run
WHERE campaign_run.id = @campaign_run_id
  AND campaign_run.fencing_token = @fencing_token
  AND campaign_run.lease_expires_at > clock_timestamp()`, thresholdEvaluationArgs(evaluation))
	if err != nil {
		return err
	}
	return campaigns.RequireCampaignLease(tag.RowsAffected())
}

func GetDeploymentCampaignRun(
	ctx context.Context,
	runID uuid.UUID,
	organizationID uuid.UUID,
) (*types.CampaignRun, error) {
	var run types.CampaignRun
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
SELECT
  id,
  created_at,
  updated_at,
  organization_id,
  campaign_revision_id,
  state,
  version,
  current_wave_order,
  current_member_order,
  admissions_blocked,
  COALESCE(resume_state, ''),
  fencing_token,
  COALESCE(lease_holder, ''),
  lease_expires_at,
  pause_requested,
  reconciliation_required
FROM DeploymentCampaignRun
WHERE id = @run_id AND organization_id = @organization_id`,
		pgx.NamedArgs{"run_id": runID, "organization_id": organizationID},
	).Scan(
		&run.ID, &run.CreatedAt, &run.UpdatedAt, &run.OrganizationID,
		&run.CampaignRevisionID, &run.State, &run.Version,
		&run.CurrentWaveOrder, &run.CurrentMemberOrder, &run.AdmissionsBlocked,
		&run.ResumeState, &run.FencingToken, &run.LeaseHolder, &run.LeaseExpiresAt,
		&run.PauseRequested, &run.ReconciliationRequired,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	return &run, err
}

func (CampaignRepository) AcquireCampaignLease(
	ctx context.Context,
	runID uuid.UUID,
	holder string,
	now time.Time,
	duration time.Duration,
) (types.CampaignLease, bool, error) {
	var lease types.CampaignLease
	lease.RunID = runID
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
UPDATE DeploymentCampaignRun
SET lease_holder = @holder,
    lease_expires_at = @expires_at,
    fencing_token = fencing_token + 1,
    updated_at = @now
WHERE id = @run_id
  AND state IN ('SCHEDULED', 'RUNNING', 'PAUSED')
  AND (
    lease_expires_at IS NULL
    OR lease_expires_at <= @now
    OR lease_holder = @holder
  )
RETURNING lease_holder, fencing_token, lease_expires_at`, pgx.NamedArgs{
		"run_id":     runID,
		"holder":     holder,
		"now":        now,
		"expires_at": now.Add(duration),
	}).Scan(&lease.Holder, &lease.FencingToken, &lease.LeaseExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return types.CampaignLease{}, false, nil
	}
	return lease, err == nil, err
}

func (CampaignRepository) LoadCampaignSchedule(
	ctx context.Context,
	runID uuid.UUID,
	fencingToken int64,
) (types.CampaignSchedule, error) {
	tx, err := internalctx.GetDb(ctx).Begin(ctx)
	if err != nil {
		return types.CampaignSchedule{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(
		ctx,
		"SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY",
	); err != nil {
		return types.CampaignSchedule{}, err
	}

	var schedule types.CampaignSchedule
	var failureToleranceBasisPoints int
	var frozenWaveMatches bool
	var applicableBakeWavePresent bool
	err = tx.QueryRow(ctx, loadCampaignScheduleSQL, pgx.NamedArgs{
		"run_id":        runID,
		"fencing_token": fencingToken,
	}).Scan(
		&schedule.Run.ID,
		&schedule.Run.CreatedAt,
		&schedule.Run.UpdatedAt,
		&schedule.Run.OrganizationID,
		&schedule.Run.CampaignRevisionID,
		&schedule.Run.State,
		&schedule.Run.Version,
		&schedule.Run.CurrentWaveOrder,
		&schedule.Run.CurrentMemberOrder,
		&schedule.Run.AdmissionsBlocked,
		&schedule.Run.ResumeState,
		&schedule.Run.FencingToken,
		&schedule.Run.LeaseHolder,
		&schedule.Run.LeaseExpiresAt,
		&schedule.Run.PauseRequested,
		&schedule.Run.ReconciliationRequired,
		&schedule.AtSafePoint,
		&schedule.AllMembersTerminal,
		&schedule.CurrentWaveOrder,
		&schedule.BakeUntil,
		&schedule.WaveMaximumConcurrency,
		&schedule.WaveActive,
		&schedule.CampaignMaximumConcurrency,
		&schedule.CampaignActive,
		&schedule.MinimumHealthyBasisPoints,
		&failureToleranceBasisPoints,
		&schedule.ThresholdSnapshot.Successful,
		&schedule.ThresholdSnapshot.Failed,
		&frozenWaveMatches,
		&applicableBakeWavePresent,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return types.CampaignSchedule{}, campaigns.ErrCampaignLeaseLost
	}
	if err != nil {
		return types.CampaignSchedule{}, err
	}
	if schedule.CurrentWaveOrder > 0 && !frozenWaveMatches {
		return types.CampaignSchedule{}, apierrors.NewConflict(
			"campaign wave runtime no longer matches frozen wave",
		)
	}
	if applicableBakeWavePresent && schedule.BakeUntil == nil {
		return types.CampaignSchedule{}, apierrors.NewConflict(
			"campaign applicable bake wave no longer matches frozen wave",
		)
	}
	if schedule.CurrentWaveOrder > 0 {
		schedule.ThresholdPolicy = types.CampaignThresholdPolicy{
			MinimumSamples:     1,
			MaximumFailureRate: float64(failureToleranceBasisPoints) / 10000,
		}
	}

	rows, err := tx.Query(ctx, loadCampaignCandidatesSQL, pgx.NamedArgs{
		"run_id":             runID,
		"fencing_token":      fencingToken,
		"current_wave_order": schedule.CurrentWaveOrder,
	})
	if err != nil {
		return types.CampaignSchedule{}, err
	}
	for rows.Next() {
		var candidate types.CampaignMemberCandidate
		var frozenMaximumConcurrency int
		if err := rows.Scan(
			&candidate.OrganizationID,
			&candidate.ActorUserAccountID,
			&candidate.CampaignEvidence.ID,
			&candidate.CampaignEvidence.Revision,
			&candidate.CampaignEvidence.Checksum,
			&candidate.MemberRunID,
			&candidate.WaveRunID,
			&candidate.WaveOrder,
			&candidate.MemberOrder,
			&candidate.PlanID,
			&frozenMaximumConcurrency,
		); err != nil {
			rows.Close()
			return types.CampaignSchedule{}, err
		}
		if schedule.WaveMaximumConcurrency != frozenMaximumConcurrency {
			rows.Close()
			return types.CampaignSchedule{}, apierrors.NewConflict(
				"campaign wave runtime no longer matches frozen wave concurrency",
			)
		}
		schedule.Candidates = append(schedule.Candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return types.CampaignSchedule{}, err
	}
	rows.Close()

	for i := range schedule.Candidates {
		prerequisiteRows, err := tx.Query(
			ctx,
			loadCandidatePrerequisitesSQL,
			pgx.NamedArgs{
				"run_id":             runID,
				"fencing_token":      fencingToken,
				"downstream_plan_id": schedule.Candidates[i].PlanID,
			},
		)
		if err != nil {
			return types.CampaignSchedule{}, err
		}
		for prerequisiteRows.Next() {
			var requirement types.CampaignObservationRequirement
			if err := prerequisiteRows.Scan(
				&requirement.OrganizationID,
				&requirement.UpstreamPlanID,
				&requirement.StepKey,
				&requirement.ProviderPlacementID,
				&requirement.ProviderDeploymentUnitID,
				&requirement.ProviderComponentInstanceID,
				&requirement.ExpectedRuntimeStateChecksum,
			); err != nil {
				prerequisiteRows.Close()
				return types.CampaignSchedule{}, err
			}
			schedule.Candidates[i].Prerequisites = append(
				schedule.Candidates[i].Prerequisites,
				requirement,
			)
		}
		if err := prerequisiteRows.Err(); err != nil {
			prerequisiteRows.Close()
			return types.CampaignSchedule{}, err
		}
		prerequisiteRows.Close()
	}
	if err := tx.Commit(ctx); err != nil {
		return types.CampaignSchedule{}, err
	}
	return schedule, nil
}

func (CampaignRepository) FinalizePendingCampaignPause(
	ctx context.Context,
	runID uuid.UUID,
	fencingToken int64,
) (bool, error) {
	tag, err := internalctx.GetDb(ctx).Exec(ctx, `
UPDATE DeploymentCampaignRun AS campaign_run
SET state = 'PAUSED',
    pause_requested = FALSE,
    admissions_blocked = TRUE,
    version = version + 1,
    updated_at = now(),
    transition_evidence = transition_evidence || jsonb_build_array(
      jsonb_build_object('to', 'PAUSED', 'reason', 'pause safe point reached', 'at', now())
    )
WHERE campaign_run.id = @run_id
  AND campaign_run.fencing_token = @fencing_token
  AND campaign_run.lease_expires_at > clock_timestamp()
  AND campaign_run.pause_requested = TRUE
  AND campaign_run.state = 'RUNNING'
  AND NOT EXISTS (
    SELECT 1
    FROM DeploymentCampaignMemberRun AS member_run
    WHERE member_run.campaign_run_id = campaign_run.id
      AND member_run.status IN ('ADMITTED', 'RUNNING')
  )`, pgx.NamedArgs{"run_id": runID, "fencing_token": fencingToken})
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (CampaignRepository) CompleteCampaignRun(
	ctx context.Context,
	runID uuid.UUID,
	fencingToken int64,
	thresholdEvaluationID uuid.UUID,
	completedAt time.Time,
) (bool, error) {
	if thresholdEvaluationID == uuid.Nil {
		return false, apierrors.NewConflict("campaign completion requires threshold evidence")
	}
	tag, err := internalctx.GetDb(ctx).Exec(ctx, completeCampaignRunSQL, pgx.NamedArgs{
		"run_id":                  runID,
		"fencing_token":           fencingToken,
		"threshold_evaluation_id": thresholdEvaluationID,
		"completed_at":            completedAt,
	})
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (CampaignRepository) RecordCampaignPrerequisiteEvaluation(
	ctx context.Context,
	evaluation types.CampaignPrerequisiteEvaluation,
	fencingToken int64,
) error {
	evaluation.FencingToken = fencingToken
	return RecordCampaignPrerequisiteEvaluation(ctx, evaluation)
}

func (CampaignRepository) RecordPrerequisitesAndAdmit(
	ctx context.Context,
	candidate types.CampaignMemberCandidate,
	admission types.CampaignMemberAdmission,
	resolver campaigns.CampaignObservationResolver,
	verifier campaigns.CampaignObservationVerifier,
	authorizer types.AdmissionAuthorizer,
	fencingToken int64,
) ([]types.Task, bool, bool, error) {
	tx, err := internalctx.GetDb(ctx).Begin(ctx)
	if err != nil {
		return nil, false, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	txCtx := internalctx.WithDb(ctx, tx)
	paused := false
	pauseReason := "campaign prerequisite mismatch"
	for _, requirement := range candidate.Prerequisites {
		observationID := requirement.ObservationID
		runtimeChecksum := requirement.RuntimeStateChecksum
		var verifyErr error
		if observationID == uuid.Nil || runtimeChecksum == "" {
			observationID, runtimeChecksum, verifyErr = resolver.ResolveCampaignObservation(
				txCtx, requirement.OrganizationID, requirement.ProviderComponentInstanceID,
				requirement.ExpectedRuntimeStateChecksum,
			)
		}
		if verifyErr == nil {
			verifyErr = verifier.VerifyCampaignObservation(
				txCtx, requirement.OrganizationID, observationID, runtimeChecksum,
			)
		}
		matched := verifyErr == nil && observationID != uuid.Nil &&
			runtimeChecksum == requirement.ExpectedRuntimeStateChecksum
		evaluation := types.CampaignPrerequisiteEvaluation{
			ID: uuid.New(), CampaignRunID: admission.RunID, MemberRunID: candidate.MemberRunID,
			UpstreamPlanID: requirement.UpstreamPlanID, StepKey: requirement.StepKey,
			ExpectedRuntimeStateChecksum: requirement.ExpectedRuntimeStateChecksum,
			ActualObservationID:          observationID, ActualRuntimeStateChecksum: runtimeChecksum,
			Matched: matched, EvaluatedAt: admission.AdmittedAt, FencingToken: fencingToken,
		}
		if verifyErr != nil {
			evaluation.ActualObservationID = uuid.Nil
			evaluation.ActualRuntimeStateChecksum = ""
			evaluation.Reason = verifyErr.Error()
			if errors.Is(verifyErr, campaigns.ErrCampaignObservationVerifierUnavailable) ||
				errors.Is(verifyErr, campaigns.ErrCampaignObservationResolverUnavailable) {
				pauseReason = "trusted observation unavailable"
			}
		} else if !matched {
			evaluation.Reason = "prerequisite runtime state does not match frozen expectation"
		}
		if err := RecordCampaignPrerequisiteEvaluation(txCtx, evaluation); err != nil {
			return nil, false, false, err
		}
		paused = paused || !matched
	}
	if paused {
		if err := (CampaignRepository{}).PauseCampaignAdmission(
			txCtx, admission.RunID, pauseReason, fencingToken,
		); err != nil {
			return nil, false, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, false, false, err
		}
		return nil, false, true, nil
	}
	admitted, err := (CampaignRepository{}).AdmitCampaignMember(txCtx, admission, fencingToken)
	if err != nil {
		return nil, false, false, err
	}
	if !admitted {
		// The deferred rollback also removes the prerequisite evidence. Evidence
		// is retained only for the admission decision it atomically governed.
		return nil, false, false, nil
	}
	tasks, err := materializeAdmittedCampaignTasks(txCtx, candidate, admission, authorizer)
	if err != nil {
		return nil, false, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, false, err
	}
	return tasks, admitted, false, nil
}

func (CampaignRepository) RecordCampaignThresholdEvaluation(
	ctx context.Context,
	evaluation types.CampaignThresholdEvaluation,
	fencingToken int64,
) error {
	evaluation.FencingToken = fencingToken
	return RecordThresholdEvaluation(ctx, evaluation)
}

func (CampaignRepository) RecordThresholdAndMaybePause(
	ctx context.Context,
	evaluation types.CampaignThresholdEvaluation,
	fencingToken int64,
) (bool, error) {
	tx, err := internalctx.GetDb(ctx).Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	txCtx := internalctx.WithDb(ctx, tx)
	evaluation.FencingToken = fencingToken
	if err := RecordThresholdEvaluation(txCtx, evaluation); err != nil {
		return false, err
	}
	if evaluation.Breached {
		if err := (CampaignRepository{}).PauseCampaignAdmission(
			txCtx, evaluation.CampaignRunID, "campaign threshold breached", fencingToken,
		); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return evaluation.Breached, nil
}

func (CampaignRepository) AdmitCampaignMember(
	ctx context.Context,
	admission types.CampaignMemberAdmission,
	fencingToken int64,
) (bool, error) {
	tag, err := internalctx.GetDb(ctx).Exec(ctx, admitCampaignMemberSQL, pgx.NamedArgs{
		"member_run_id": admission.MemberRunID,
		"wave_run_id":   admission.WaveRunID,
		"run_id":        admission.RunID,
		"wave_order":    admission.WaveOrder,
		"member_order":  admission.MemberOrder,
		"admitted_at":   admission.AdmittedAt,
		"fencing_token": fencingToken,
	})
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		var currentToken int64
		err := internalctx.GetDb(ctx).QueryRow(ctx, `
SELECT fencing_token FROM DeploymentCampaignRun WHERE id = @run_id`,
			pgx.NamedArgs{"run_id": admission.RunID},
		).Scan(&currentToken)
		if err == nil && currentToken != fencingToken {
			return false, fmt.Errorf("campaign scheduler lease lost")
		}
		return false, err
	}
	return true, nil
}

func (CampaignRepository) PauseCampaignAdmission(
	ctx context.Context,
	runID uuid.UUID,
	reason string,
	fencingToken int64,
) error {
	tag, err := internalctx.GetDb(ctx).Exec(ctx, `
UPDATE DeploymentCampaignRun
SET state = 'PAUSED',
    resume_state = CASE
      WHEN state IN ('SCHEDULED', 'RUNNING') THEN state
      ELSE resume_state
    END,
    admissions_blocked = TRUE,
    version = version + 1,
    updated_at = now(),
    transition_evidence = transition_evidence || jsonb_build_array(
      jsonb_build_object('to', 'PAUSED', 'reason', @reason, 'at', now())
    )
WHERE id = @run_id
  AND fencing_token = @fencing_token
  AND lease_expires_at > clock_timestamp()
  AND state IN ('SCHEDULED', 'RUNNING', 'PAUSED')`,
		pgx.NamedArgs{"run_id": runID, "reason": reason, "fencing_token": fencingToken},
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("campaign scheduler lease lost")
	}
	return nil
}

func scanCampaignRun(row pgx.Row) (*types.CampaignRun, error) {
	var run types.CampaignRun
	err := row.Scan(
		&run.ID,
		&run.CreatedAt,
		&run.UpdatedAt,
		&run.OrganizationID,
		&run.CampaignRevisionID,
		&run.State,
		&run.Version,
		&run.CurrentWaveOrder,
		&run.CurrentMemberOrder,
		&run.AdmissionsBlocked,
		&run.ResumeState,
		&run.FencingToken,
		&run.LeaseHolder,
		&run.LeaseExpiresAt,
	)
	return &run, err
}

func prerequisiteEvaluationArgs(
	evaluation types.CampaignPrerequisiteEvaluation,
) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                              evaluation.ID,
		"evaluated_at":                    evaluation.EvaluatedAt,
		"campaign_run_id":                 evaluation.CampaignRunID,
		"member_run_id":                   evaluation.MemberRunID,
		"upstream_plan_id":                evaluation.UpstreamPlanID,
		"step_key":                        evaluation.StepKey,
		"expected_runtime_state_checksum": evaluation.ExpectedRuntimeStateChecksum,
		"actual_observation_id":           evaluation.ActualObservationID,
		"actual_runtime_state_checksum":   evaluation.ActualRuntimeStateChecksum,
		"matched":                         evaluation.Matched,
		"reason":                          evaluation.Reason,
		"fencing_token":                   evaluation.FencingToken,
	}
}

func thresholdEvaluationArgs(evaluation types.CampaignThresholdEvaluation) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                   evaluation.ID,
		"evaluated_at":         evaluation.EvaluatedAt,
		"campaign_run_id":      evaluation.CampaignRunID,
		"samples":              evaluation.Samples,
		"successful":           evaluation.Successful,
		"failed":               evaluation.Failed,
		"failure_rate":         evaluation.FailureRate,
		"maximum_failure_rate": evaluation.MaximumFailureRate,
		"breached":             evaluation.Breached,
		"fencing_token":        evaluation.FencingToken,
	}
}

func campaignControlFacts(
	ctx context.Context,
	runID uuid.UUID,
) (types.CampaignControlFacts, error) {
	var active int
	var uncertain bool
	var cancellable bool
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
SELECT
  count(*) FILTER (WHERE status IN ('ADMITTED', 'RUNNING')),
  COALESCE(bool_or(execution_uncertain) FILTER (
    WHERE status IN ('ADMITTED', 'RUNNING')
  ), FALSE),
  COALESCE(bool_and(active_steps_cancellable) FILTER (
    WHERE status IN ('ADMITTED', 'RUNNING')
  ), TRUE)
FROM DeploymentCampaignMemberRun
WHERE campaign_run_id = @run_id`,
		pgx.NamedArgs{"run_id": runID},
	).Scan(&active, &uncertain, &cancellable)
	return types.CampaignControlFacts{
		AtSafePoint:               active == 0,
		HasUncertainSteps:         uncertain,
		AllActiveStepsCancellable: cancellable,
	}, err
}

func campaignControlChecksum(
	input types.CampaignControlInput,
	memberRunID uuid.UUID,
) string {
	value := fmt.Sprintf(
		"%s\x00%s\x00%s\x00%d\x00%s\x00%s",
		input.RunID,
		memberRunID,
		input.Kind,
		input.ExpectedVersion,
		input.Reason,
		input.ActorID,
	)
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func campaignRetryChecksum(input types.CampaignMemberControlInput) string {
	value := campaignControlChecksum(input.CampaignControlInput, input.MemberRunID) +
		"\x00" + input.ProtocolVersion
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func campaignControlArgs(
	controlID uuid.UUID,
	input types.CampaignControlInput,
	memberRunID uuid.UUID,
	checksum string,
	result types.CampaignControlResult,
	response []byte,
) pgx.NamedArgs {
	var member any
	if memberRunID != uuid.Nil {
		member = memberRunID
	}
	return pgx.NamedArgs{
		"id":                    controlID,
		"request_id":            input.RequestID,
		"requested_at":          input.RequestedAt,
		"organization_id":       input.OrganizationID,
		"campaign_run_id":       input.RunID,
		"member_run_id":         member,
		"actor_useraccount_id":  input.ActorID,
		"control_kind":          input.Kind,
		"expected_run_version":  input.ExpectedVersion,
		"reason":                input.Reason,
		"request_checksum":      checksum,
		"status":                result.Status,
		"resulting_run_version": result.Run.Version,
		"response_snapshot":     response,
	}
}

func getDuplicateCampaignControl(
	ctx context.Context,
	tx pgx.Tx,
	input types.CampaignControlInput,
	checksum string,
) (*types.CampaignControlResult, error) {
	result, found, err := lookupCampaignControlReplay(ctx, tx, input, checksum)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, pgx.ErrNoRows
	}
	return result, nil
}

func lookupCampaignControlReplay(
	ctx context.Context,
	tx pgx.Tx,
	input types.CampaignControlInput,
	checksum string,
) (*types.CampaignControlResult, bool, error) {
	var existingChecksum string
	var response []byte
	err := tx.QueryRow(ctx, lookupCampaignControlReplaySQL,
		pgx.NamedArgs{
			"organization_id": input.OrganizationID,
			"request_id":      input.RequestID,
		},
	).Scan(&existingChecksum, &response)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if existingChecksum != checksum {
		return nil, false, apierrors.NewConflict(
			"campaign control request ID reused with different input",
		)
	}
	var result types.CampaignControlResult
	if err := json.Unmarshal(response, &result); err != nil {
		return nil, false, err
	}
	return &result, true, nil
}

func lookupCampaignRetryReplay(
	ctx context.Context,
	tx pgx.Tx,
	input types.CampaignMemberControlInput,
	checksum string,
) (*types.DeploymentPlan, bool, error) {
	var existingChecksum string
	var response []byte
	err := tx.QueryRow(ctx, lookupCampaignControlReplaySQL, pgx.NamedArgs{
		"organization_id": input.OrganizationID,
		"request_id":      input.RequestID,
	}).Scan(&existingChecksum, &response)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if existingChecksum != checksum {
		return nil, false, apierrors.NewConflict(
			"campaign control request ID reused with different input",
		)
	}
	var plan types.DeploymentPlan
	if err := json.Unmarshal(response, &plan); err != nil {
		return nil, false, err
	}
	return &plan, true, nil
}

func getCampaignExclusionReplay(
	ctx context.Context,
	tx pgx.Tx,
	input types.CampaignMemberControlInput,
) (*types.CampaignExclusion, error) {
	var existing types.CampaignExclusion
	err := tx.QueryRow(ctx, `
SELECT
  exclusion.id,
  exclusion.organization_id,
  exclusion.campaign_run_id,
  exclusion.member_run_id,
  exclusion.control_request_id,
  exclusion.reason,
  exclusion.visible_incomplete,
  exclusion.drift_reason,
  exclusion.excluded_at,
  exclusion.excluded_by_useraccount_id
FROM CampaignExclusion AS exclusion
JOIN CampaignControlRequest AS control
  ON control.id = exclusion.control_request_id
 AND control.organization_id = exclusion.organization_id
 AND control.campaign_run_id = exclusion.campaign_run_id
WHERE control.organization_id = @organization_id
  AND control.request_id = @request_id`,
		pgx.NamedArgs{
			"organization_id": input.OrganizationID,
			"request_id":      input.RequestID,
		},
	).Scan(
		&existing.ID,
		&existing.OrganizationID,
		&existing.CampaignRunID,
		&existing.MemberRunID,
		&existing.ControlRequestID,
		&existing.Reason,
		&existing.VisibleIncomplete,
		&existing.DriftReason,
		&existing.ExcludedAt,
		&existing.ExcludedByActorID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.NewConflict(
			"campaign control replay has no matching exclusion",
		)
	}
	return &existing, err
}
