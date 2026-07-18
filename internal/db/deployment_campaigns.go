package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/campaigns"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
