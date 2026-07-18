package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/planning"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	maxPlanEvidenceComponents = 256
	maxPlanEvidenceRows       = 4096
)

func resolveDeploymentPlanEvidence(
	ctx context.Context,
	draft types.PlanDraft,
	input types.PlanResolutionInput,
	resolutions []types.RequirementResolution,
	graph types.TargetPlanGraph,
) (
	[]types.DeploymentPlanBaseline,
	[]types.DeploymentPlanChangeEntry,
	[]types.DeploymentPlanRiskEntry,
	bool,
	error,
) {
	plannedStates, err := plannedStatesFromResolution(input, resolutions, graph)
	if err != nil {
		return nil, nil, nil, false, err
	}
	if len(plannedStates) > maxPlanEvidenceComponents {
		return nil, nil, nil, false, apierrors.NewBadRequest("deployment plan component limit exceeded")
	}

	baselines := make([]types.DeploymentPlanBaseline, 0, len(plannedStates))
	changes := make([]types.DeploymentPlanChangeEntry, 0, len(plannedStates)*4)
	bootstrap := false
	for _, planned := range plannedStates {
		baseline, baselineState, err := loadVerifiedBaseline(
			ctx,
			draft,
			planned,
		)
		if err != nil {
			return nil, nil, nil, false, err
		}
		baseline.SortOrder = len(baselines)
		baseline.CanonicalChecksum, err = checksumPlanEvidence(baselineCanonical(*baseline))
		if err != nil {
			return nil, nil, nil, false, err
		}
		baselines = append(baselines, *baseline)
		bootstrap = bootstrap || baseline.Bootstrap

		notes, err := loadReleaseNotes(
			ctx,
			draft.OrganizationID,
			draft.ProductReleaseID,
			planned.ComponentKey,
			baselineState.ReleaseBundleID,
			planned.ReleaseBundleID,
		)
		if err != nil {
			return nil, nil, nil, false, err
		}
		componentChanges := planning.BuildTargetChangeSet(baselineState, planned, notes)
		changes = append(changes, componentChanges...)
		if len(changes) > maxPlanEvidenceRows {
			return nil, nil, nil, false, apierrors.NewBadRequest("deployment plan change limit exceeded")
		}
	}
	sortPlanChanges(changes)

	risks := planning.ClassifyDeploymentRisk(changes, types.PlanRiskPolicy{
		AllowForwardOnlyMigration:      false,
		RequireBootstrapApproval:       true,
		RequireAuthoritativeV2Baseline: true,
	})
	if len(risks) > maxPlanEvidenceRows {
		return nil, nil, nil, false, apierrors.NewBadRequest("deployment plan risk limit exceeded")
	}
	for index := range risks {
		risks[index].SortOrder = index
		risks[index].CanonicalChecksum, err = checksumPlanEvidence(riskCanonical(risks[index]))
		if err != nil {
			return nil, nil, nil, false, err
		}
	}
	return baselines, changes, risks, bootstrap, nil
}

func plannedStatesFromResolution(
	input types.PlanResolutionInput,
	resolutions []types.RequirementResolution,
	graph types.TargetPlanGraph,
) ([]types.PlannedState, error) {
	pins := slices.Clone(input.ReleasePins)
	slices.SortFunc(pins, func(a, b types.ComponentReleasePin) int {
		return strings.Compare(a.ComponentKey, b.ComponentKey)
	})
	if len(pins) > maxPlanEvidenceComponents {
		return nil, apierrors.NewBadRequest("deployment plan component limit exceeded")
	}
	if len(input.Config.ComponentBindings) > maxPlanEvidenceComponents ||
		len(resolutions) > maxPlanEvidenceRows {
		return nil, apierrors.NewBadRequest("deployment plan resolution input limit exceeded")
	}
	bindings := make(map[string]types.ConfigComponentBinding, len(input.Config.ComponentBindings))
	for _, binding := range input.Config.ComponentBindings {
		bindings[strings.TrimSpace(binding.ComponentKey)] = binding
	}
	result := make([]types.PlannedState, 0, len(pins))
	for _, pin := range pins {
		binding, ok := bindings[pin.ComponentKey]
		if !ok || binding.ComponentInstanceID == uuid.Nil {
			return nil, apierrors.NewBadRequest(
				"component release has no exact target component instance binding",
			)
		}
		image := ""
		for _, artifact := range pin.Artifacts {
			if artifact.Platform != input.Config.TargetPlatform {
				continue
			}
			if artifact.Type == "oci-image" || image == "" {
				image = artifact.PlatformDigest
			}
		}
		if image == "" {
			return nil, apierrors.NewBadRequest("component release has no exact target platform digest")
		}
		providerChecksum, err := providerBindingsChecksum(
			pin.ComponentKey,
			binding.ComponentInstanceID,
			resolutions,
		)
		if err != nil {
			return nil, err
		}
		schemaState, schemaChecksum, forwardOnly, err := migrationState(pin.Migrations)
		if err != nil {
			return nil, err
		}
		topologyChecksum, err := checksumPlanEvidence(struct {
			DeploymentUnitID      uuid.UUID `json:"deploymentUnitId"`
			ComponentInstanceID   uuid.UUID `json:"componentInstanceId"`
			PhysicalName          string    `json:"physicalName"`
			SubscriberSetChecksum string    `json:"subscriberSetChecksum"`
			GraphChecksum         string    `json:"graphChecksum"`
		}{
			DeploymentUnitID:      input.Unit.ID,
			ComponentInstanceID:   binding.ComponentInstanceID,
			PhysicalName:          strings.TrimSpace(binding.PhysicalName),
			SubscriberSetChecksum: input.Unit.SubscriberSetChecksum,
			GraphChecksum:         graph.Checksum,
		})
		if err != nil {
			return nil, err
		}
		configID := input.Config.ID
		result = append(result, types.PlannedState{
			ComponentInstanceID:     binding.ComponentInstanceID,
			ComponentKey:            strings.TrimSpace(pin.ComponentKey),
			ReleaseBundleID:         pin.ComponentReleaseID,
			Version:                 strings.TrimSpace(pin.Version),
			Image:                   image,
			Platform:                input.Config.TargetPlatform,
			ConfigSnapshotID:        &configID,
			ConfigChecksum:          input.Config.CanonicalChecksum,
			ProviderBindingChecksum: providerChecksum,
			SchemaState:             schemaState,
			SchemaChecksum:          schemaChecksum,
			TopologyChecksum:        topologyChecksum,
			ForwardOnly:             forwardOnly,
		})
	}
	return result, nil
}

func loadVerifiedBaseline(
	ctx context.Context,
	draft types.PlanDraft,
	planned types.PlannedState,
) (*types.DeploymentPlanBaseline, types.BaselineState, error) {
	type desiredState struct {
		ID       uuid.UUID
		Revision int64
		Checksum string
	}
	var desired desiredState
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT state.id, state.state_version, state.state_checksum
		FROM TargetComponentState state
		JOIN DeploymentUnit unit
		  ON unit.deployment_target_id = state.deployment_target_id
		 AND unit.organization_id = state.organization_id
		JOIN ComponentInstance instance
		  ON instance.id = @componentInstanceID
		 AND instance.deployment_unit_id = unit.id
		 AND instance.organization_id = unit.organization_id
		 AND instance.retired_at IS NULL
		JOIN ComponentDefinition definition
		  ON definition.id = instance.component_definition_id
		 AND definition.organization_id = instance.organization_id
		 AND definition.retired_at IS NULL
		JOIN ReleaseBundle product
		  ON product.id = @productReleaseID
		 AND product.organization_id = state.organization_id
		 AND product.application_id = state.application_id
		WHERE state.organization_id = @organizationID
		  AND unit.id = @deploymentUnitID
		  AND unit.retired_at IS NULL
		  AND (
		    state.component = definition.key
		    OR state.component = instance.physical_name
		  )
		FOR SHARE`,
		pgx.NamedArgs{
			"organizationID":      draft.OrganizationID,
			"deploymentUnitID":    draft.DeploymentUnitID,
			"componentInstanceID": planned.ComponentInstanceID,
			"productReleaseID":    draft.ProductReleaseID,
		},
	).Scan(&desired.ID, &desired.Revision, &desired.Checksum)
	if errors.Is(err, pgx.ErrNoRows) {
		synthetic, checksumErr := checksumPlanEvidence(struct {
			ComponentInstanceID uuid.UUID `json:"componentInstanceId"`
			ReleaseBundleID     uuid.UUID `json:"releaseBundleId"`
			Image               string    `json:"image"`
			ConfigChecksum      string    `json:"configChecksum"`
		}{
			ComponentInstanceID: planned.ComponentInstanceID,
			ReleaseBundleID:     planned.ReleaseBundleID,
			Image:               planned.Image,
			ConfigChecksum:      planned.ConfigChecksum,
		})
		if checksumErr != nil {
			return nil, types.BaselineState{}, checksumErr
		}
		baseline, selectErr := planning.SelectVerifiedBaseline(ctx, types.BaselineQuery{
			OrganizationID:          draft.OrganizationID,
			DeploymentUnitID:        draft.DeploymentUnitID,
			ComponentInstanceID:     planned.ComponentInstanceID,
			ComponentKey:            planned.ComponentKey,
			ExpectedDesiredRevision: 1,
			ExpectedDesiredChecksum: synthetic,
		})
		return baseline, baselineStateFromEvidence(*baseline), selectErr
	}
	if err != nil {
		return nil, types.BaselineState{}, fmt.Errorf("query active desired component state: %w", err)
	}

	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT observation.id,
		       observation.observed_at,
		       observation.health,
		       state.state_version,
		       state.state_checksum,
		       observation.state_version,
		       observation.state_checksum,
		       observation.release_bundle_id,
		       observation.version,
		       observation.image,
		       observation.platform,
		       observation.config_checksum,
		       execution.id,
		       plan.id,
		       COALESCE(plan.plan_schema, ''),
		       COALESCE(plan.protocol_version, ''),
		       plan.target_config_snapshot_id,
		       plan.canonical_payload
		FROM TargetComponentObservation observation
		JOIN TargetComponentState state
		  ON state.id = observation.target_component_state_id
		 AND state.organization_id = observation.organization_id
		LEFT JOIN ExternalExecution execution
		  ON execution.id = observation.external_execution_id
		 AND execution.organization_id = observation.organization_id
		 AND execution.status = 'SUCCEEDED'
		LEFT JOIN DeploymentPlan plan
		  ON plan.id = execution.deployment_plan_id
		 AND plan.organization_id = execution.organization_id
		WHERE observation.organization_id = @organizationID
		  AND observation.target_component_state_id = @stateID
		  AND observation.health = 'HEALTHY'
		  AND observation.state_version = @expectedDesiredRevision
		  AND observation.state_checksum = @expectedDesiredChecksum
		ORDER BY observation.observed_at DESC,
		         observation.state_version DESC,
		         observation.id DESC
		LIMIT 256`,
		pgx.NamedArgs{
			"organizationID":          draft.OrganizationID,
			"stateID":                 desired.ID,
			"expectedDesiredRevision": desired.Revision,
			"expectedDesiredChecksum": desired.Checksum,
		},
	)
	if err != nil {
		return nil, types.BaselineState{}, fmt.Errorf("query verified baseline observations: %w", err)
	}
	defer rows.Close()
	candidates := make([]types.BaselineCandidate, 0)
	for rows.Next() {
		var candidate types.BaselineCandidate
		var executionID, sourcePlanID, configSnapshotID *uuid.UUID
		var canonicalPayload []byte
		if err := rows.Scan(
			&candidate.ObservationID,
			&candidate.ObservedAt,
			&candidate.Health,
			&candidate.DesiredRevision,
			&candidate.DesiredChecksum,
			&candidate.ObservedRevision,
			&candidate.ObservedChecksum,
			&candidate.ReleaseBundleID,
			&candidate.Version,
			&candidate.Image,
			&candidate.Platform,
			&candidate.ConfigChecksum,
			&executionID,
			&sourcePlanID,
			&candidate.PlanSchema,
			&candidate.ProtocolVersion,
			&configSnapshotID,
			&canonicalPayload,
		); err != nil {
			return nil, types.BaselineState{}, fmt.Errorf("scan verified baseline observation: %w", err)
		}
		candidate.ExternalExecutionID = executionID
		candidate.SourceDeploymentPlanID = sourcePlanID
		candidate.ConfigSnapshotID = configSnapshotID
		if len(canonicalPayload) > 0 {
			enrichBaselineCandidate(&candidate, canonicalPayload, planned)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, types.BaselineState{}, fmt.Errorf("collect verified baseline observations: %w", err)
	}
	baseline, err := planning.SelectVerifiedBaseline(ctx, types.BaselineQuery{
		OrganizationID:          draft.OrganizationID,
		DeploymentUnitID:        draft.DeploymentUnitID,
		ComponentInstanceID:     planned.ComponentInstanceID,
		ComponentKey:            planned.ComponentKey,
		ExpectedDesiredRevision: desired.Revision,
		ExpectedDesiredChecksum: desired.Checksum,
		Candidates:              candidates,
	})
	if err != nil {
		return nil, types.BaselineState{}, err
	}
	return baseline, baselineStateFromEvidence(*baseline), nil
}

func enrichBaselineCandidate(
	candidate *types.BaselineCandidate,
	payload []byte,
	planned types.PlannedState,
) {
	var canonical types.TargetDeploymentPlanCanonical
	if err := json.Unmarshal(payload, &canonical); err != nil {
		return
	}
	state, ok := findPlannedState(canonical, planned.ComponentInstanceID, planned.ComponentKey)
	if !ok {
		return
	}
	candidate.ProviderBindingChecksum = state.ProviderBindingChecksum
	candidate.SchemaState = state.SchemaState
	candidate.SchemaChecksum = state.SchemaChecksum
	candidate.TopologyChecksum = state.TopologyChecksum
	candidate.PlanFactsMatch =
		state.ReleaseBundleID == candidate.ReleaseBundleID &&
			imageDigestMatches(candidate.Image, state.Image) &&
			state.Platform == candidate.Platform &&
			state.ConfigChecksum == candidate.ConfigChecksum
	if state.ConfigSnapshotID != nil {
		candidate.ConfigSnapshotID = state.ConfigSnapshotID
	}
}

func imageDigestMatches(observedImage, plannedDigest string) bool {
	observedImage = strings.TrimSpace(observedImage)
	plannedDigest = strings.TrimSpace(plannedDigest)
	return observedImage == plannedDigest ||
		strings.HasSuffix(observedImage, "@"+plannedDigest)
}

func findPlannedState(
	canonical types.TargetDeploymentPlanCanonical,
	componentInstanceID uuid.UUID,
	componentKey string,
) (types.PlannedState, bool) {
	states, err := plannedStatesFromCanonical(canonical)
	if err != nil {
		return types.PlannedState{}, false
	}
	for _, state := range states {
		if state.ComponentInstanceID == componentInstanceID &&
			state.ComponentKey == componentKey {
			return state, true
		}
	}
	return types.PlannedState{}, false
}

func plannedStatesFromCanonical(
	canonical types.TargetDeploymentPlanCanonical,
) ([]types.PlannedState, error) {
	input := types.PlanResolutionInput{
		Unit: types.DeploymentUnit{
			ID:                    canonical.DeploymentUnitID,
			SubscriberSetChecksum: canonical.SubscriberSetChecksum,
		},
		Config: types.TargetConfigBinding{
			ID:                canonical.TargetConfigSnapshotID,
			CanonicalChecksum: canonical.TargetConfigSnapshotChecksum,
			TargetPlatform:    canonical.TargetPlatform,
			ComponentBindings: canonical.ComponentBindings,
		},
		ReleasePins: canonical.ComponentReleasePins,
	}
	return plannedStatesFromResolution(
		input,
		canonical.RequirementResolutions,
		canonical.Graph,
	)
}

func loadReleaseNotes(
	ctx context.Context,
	organizationID uuid.UUID,
	productReleaseID uuid.UUID,
	componentKey string,
	baselineReleaseID uuid.UUID,
	plannedReleaseID uuid.UUID,
) ([]types.ReleaseNote, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		WITH bounds AS (
		  SELECT product.application_id,
		         baseline.id AS baseline_release_id,
		         baseline.published_at AS baseline_at,
		         planned.id AS planned_release_id,
		         planned.published_at AS planned_at
		  FROM ReleaseBundle product
		  JOIN ReleaseBundle planned
		    ON planned.id = @plannedReleaseID
		   AND planned.organization_id = product.organization_id
		   AND planned.application_id = product.application_id
		   AND planned.kind = 'component'
		   AND planned.status = 'PUBLISHED'
		  LEFT JOIN ReleaseBundle baseline
		    ON baseline.id = @baselineReleaseID
		   AND baseline.organization_id = product.organization_id
		   AND baseline.application_id = product.application_id
		   AND baseline.kind = 'component'
		   AND baseline.status = 'PUBLISHED'
		  WHERE product.id = @productReleaseID
		    AND product.organization_id = @organizationID
		    AND product.kind = 'product'
		    AND product.status = 'PUBLISHED'
		),
		candidates AS (
		  SELECT DISTINCT bundle.id,
		         artifact.component_version,
		         bundle.published_at,
		         bundle.source_revision,
		         bundle.release_notes,
		         bounds.baseline_release_id
		  FROM bounds
		  JOIN ReleaseBundle bundle
		    ON bundle.organization_id = @organizationID
		   AND bundle.application_id = bounds.application_id
		   AND bundle.kind = 'component'
		   AND bundle.status = 'PUBLISHED'
		   AND (
		     bundle.published_at,
		     bundle.id
		   ) <= (
		     bounds.planned_at,
		     bounds.planned_release_id
		   )
		   AND (
		     bounds.baseline_at IS NULL
		     OR (
		       bundle.published_at,
		       bundle.id
		     ) >= (
		       bounds.baseline_at,
		       bounds.baseline_release_id
		     )
		   )
		  JOIN ComponentReleaseArtifact artifact
		    ON artifact.release_bundle_id = bundle.id
		   AND artifact.organization_id = bundle.organization_id
		   AND artifact.component_key = @componentKey
		  WHERE EXISTS (
		    SELECT 1
		    FROM ProductReleaseComponent lineage
		    JOIN ReleaseBundle lineage_product
		      ON lineage_product.id = lineage.product_release_bundle_id
		     AND lineage_product.organization_id = lineage.organization_id
		     AND lineage_product.application_id = bounds.application_id
		     AND lineage_product.kind = 'product'
		     AND lineage_product.status = 'PUBLISHED'
		    WHERE lineage.component_release_bundle_id = bundle.id
		      AND lineage.organization_id = bundle.organization_id
		      AND lineage.component_key = @componentKey
		  )
		),
		ranked AS (
		  SELECT candidates.*,
		         row_number() OVER (
		           ORDER BY published_at DESC, id DESC
		         ) AS recent_rank
		  FROM candidates
		)
		SELECT id, component_version, published_at, source_revision, release_notes
		FROM ranked
		WHERE recent_rank <= 129
		   OR id = baseline_release_id
		ORDER BY published_at, id
		LIMIT 130`,
		pgx.NamedArgs{
			"organizationID":    organizationID,
			"productReleaseID":  productReleaseID,
			"componentKey":      componentKey,
			"baselineReleaseID": uuidOrNil(baselineReleaseID),
			"plannedReleaseID":  plannedReleaseID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("query accumulated component release notes: %w", err)
	}
	notes, err := pgx.CollectRows(rows, pgx.RowToStructByPos[types.ReleaseNote])
	if err != nil {
		return nil, fmt.Errorf("collect accumulated component release notes: %w", err)
	}
	return notes, nil
}

func persistDeploymentPlanEvidence(
	ctx context.Context,
	plan *types.DeploymentPlan,
	actorUserAccountID uuid.UUID,
) error {
	if plan.PlanSchema != types.TargetDeploymentPlanSchemaV2 {
		return nil
	}
	for index := range plan.Baselines {
		plan.Baselines[index].ID = uuid.New()
		plan.Baselines[index].DeploymentPlanID = plan.ID
		plan.Baselines[index].OrganizationID = plan.OrganizationID
		plan.Baselines[index].ActorUserAccountID = actorUserAccountID
		plan.Baselines[index].SortOrder = index
	}
	if len(plan.Baselines) > 0 {
		_, err := internalctx.GetDb(ctx).CopyFrom(
			ctx,
			pgx.Identifier{"deploymentplanbaseline"},
			[]string{
				"id", "deployment_plan_id", "organization_id", "component_instance_id",
				"component_key", "source_deployment_plan_id", "external_execution_id",
				"observation_id", "observed_at", "desired_revision", "desired_checksum",
				"observation_checksum", "release_bundle_id", "version", "image", "platform",
				"target_config_snapshot_id", "config_checksum", "provider_binding_checksum",
				"schema_state", "schema_checksum", "topology_checksum", "projection",
				"authorizes_v2_execution", "bootstrap", "actor_user_account_id",
				"canonical_checksum", "sort_order",
			},
			pgx.CopyFromSlice(len(plan.Baselines), func(index int) ([]any, error) {
				baseline := plan.Baselines[index]
				return []any{
					baseline.ID, plan.ID, plan.OrganizationID, baseline.ComponentInstanceID,
					baseline.ComponentKey, baseline.SourceDeploymentPlanID,
					baseline.ExternalExecutionID, baseline.ObservationID, baseline.ObservedAt,
					baseline.DesiredRevision, baseline.DesiredChecksum,
					baseline.ObservationChecksum, baseline.ReleaseBundleID, baseline.Version,
					baseline.Image, baseline.Platform, baseline.ConfigSnapshotID,
					baseline.ConfigChecksum, baseline.ProviderBindingChecksum,
					baseline.SchemaState, baseline.SchemaChecksum, baseline.TopologyChecksum,
					baseline.Projection, baseline.AuthorizesV2Execution, baseline.Bootstrap,
					actorUserAccountID, baseline.CanonicalChecksum, index,
				}, nil
			}),
		)
		if err != nil {
			return mapDeploymentPlanWriteError("insert deployment plan baselines", err)
		}
	}
	for index := range plan.Changes {
		plan.Changes[index].ID = uuid.New()
		plan.Changes[index].DeploymentPlanID = plan.ID
		plan.Changes[index].OrganizationID = plan.OrganizationID
		plan.Changes[index].ActorUserAccountID = actorUserAccountID
		plan.Changes[index].SortOrder = index
	}
	if len(plan.Changes) > 0 {
		_, err := internalctx.GetDb(ctx).CopyFrom(
			ctx,
			pgx.Identifier{"deploymentplanchangeentry"},
			[]string{
				"id", "deployment_plan_id", "organization_id", "component_instance_id",
				"component_key", "kind", "before_value", "after_value", "release_notes",
				"forward_only", "actor_user_account_id", "canonical_checksum", "sort_order",
			},
			pgx.CopyFromSlice(len(plan.Changes), func(index int) ([]any, error) {
				change := plan.Changes[index]
				notes, err := json.Marshal(change.ReleaseNotes)
				if err != nil {
					return nil, err
				}
				var componentInstanceID any
				if change.ComponentInstanceID != uuid.Nil {
					componentInstanceID = change.ComponentInstanceID
				}
				return []any{
					change.ID, plan.ID, plan.OrganizationID, componentInstanceID,
					change.ComponentKey, change.Kind, change.Before, change.After, notes,
					change.ForwardOnly, actorUserAccountID, change.CanonicalChecksum, index,
				}, nil
			}),
		)
		if err != nil {
			return mapDeploymentPlanWriteError("insert deployment plan changes", err)
		}
	}
	for index := range plan.Risks {
		plan.Risks[index].ID = uuid.New()
		plan.Risks[index].DeploymentPlanID = plan.ID
		plan.Risks[index].OrganizationID = plan.OrganizationID
		plan.Risks[index].ActorUserAccountID = actorUserAccountID
		plan.Risks[index].SortOrder = index
	}
	if len(plan.Risks) > 0 {
		_, err := internalctx.GetDb(ctx).CopyFrom(
			ctx,
			pgx.Identifier{"deploymentplanriskentry"},
			[]string{
				"id", "deployment_plan_id", "organization_id", "component_key", "code",
				"level", "blocking", "message", "actor_user_account_id",
				"canonical_checksum", "sort_order",
			},
			pgx.CopyFromSlice(len(plan.Risks), func(index int) ([]any, error) {
				risk := plan.Risks[index]
				return []any{
					risk.ID, plan.ID, plan.OrganizationID, risk.ComponentKey, risk.Code,
					risk.Level, risk.Blocking, risk.Message, actorUserAccountID,
					risk.CanonicalChecksum, index,
				}, nil
			}),
		)
		if err != nil {
			return mapDeploymentPlanWriteError("insert deployment plan risks", err)
		}
	}
	return nil
}

func getDeploymentPlanBaselines(
	ctx context.Context,
	planID uuid.UUID,
	organizationID uuid.UUID,
) ([]types.DeploymentPlanBaseline, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT id, created_at, deployment_plan_id, organization_id,
		       component_instance_id, component_key, source_deployment_plan_id,
		       external_execution_id, observation_id, observed_at, desired_revision,
		       desired_checksum, observation_checksum, release_bundle_id, version,
		       image, platform, target_config_snapshot_id, config_checksum,
		       provider_binding_checksum, schema_state, schema_checksum,
		       topology_checksum, projection, authorizes_v2_execution, bootstrap,
		       actor_user_account_id, canonical_checksum, sort_order
		FROM DeploymentPlanBaseline
		WHERE deployment_plan_id = @planID
		  AND organization_id = @organizationID
		ORDER BY sort_order, component_key`,
		pgx.NamedArgs{"planID": planID, "organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("query DeploymentPlanBaseline: %w", err)
	}
	result, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.DeploymentPlanBaseline])
	if err != nil {
		return nil, fmt.Errorf("collect DeploymentPlanBaseline: %w", err)
	}
	return result, nil
}

func getDeploymentPlanChanges(
	ctx context.Context,
	planID uuid.UUID,
	organizationID uuid.UUID,
) ([]types.DeploymentPlanChangeEntry, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT id, created_at, deployment_plan_id, organization_id,
		       component_instance_id, component_key, kind, before_value, after_value,
		       release_notes, forward_only, actor_user_account_id,
		       canonical_checksum, sort_order
		FROM DeploymentPlanChangeEntry
		WHERE deployment_plan_id = @planID
		  AND organization_id = @organizationID
		ORDER BY sort_order, kind`,
		pgx.NamedArgs{"planID": planID, "organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("query DeploymentPlanChangeEntry: %w", err)
	}
	defer rows.Close()
	result := make([]types.DeploymentPlanChangeEntry, 0)
	for rows.Next() {
		var entry types.DeploymentPlanChangeEntry
		var componentInstanceID *uuid.UUID
		var notes []byte
		if err := rows.Scan(
			&entry.ID, &entry.CreatedAt, &entry.DeploymentPlanID, &entry.OrganizationID,
			&componentInstanceID, &entry.ComponentKey, &entry.Kind, &entry.Before,
			&entry.After, &notes, &entry.ForwardOnly, &entry.ActorUserAccountID,
			&entry.CanonicalChecksum, &entry.SortOrder,
		); err != nil {
			return nil, fmt.Errorf("scan DeploymentPlanChangeEntry: %w", err)
		}
		if componentInstanceID != nil {
			entry.ComponentInstanceID = *componentInstanceID
		}
		if err := json.Unmarshal(notes, &entry.ReleaseNotes); err != nil {
			return nil, fmt.Errorf("decode DeploymentPlanChangeEntry release notes: %w", err)
		}
		result = append(result, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("collect DeploymentPlanChangeEntry: %w", err)
	}
	return result, nil
}

func getDeploymentPlanRisks(
	ctx context.Context,
	planID uuid.UUID,
	organizationID uuid.UUID,
) ([]types.DeploymentPlanRiskEntry, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT id, created_at, deployment_plan_id, organization_id, component_key,
		       code, level, blocking, message, actor_user_account_id,
		       canonical_checksum, sort_order
		FROM DeploymentPlanRiskEntry
		WHERE deployment_plan_id = @planID
		  AND organization_id = @organizationID
		ORDER BY sort_order, code`,
		pgx.NamedArgs{"planID": planID, "organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("query DeploymentPlanRiskEntry: %w", err)
	}
	result, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.DeploymentPlanRiskEntry])
	if err != nil {
		return nil, fmt.Errorf("collect DeploymentPlanRiskEntry: %w", err)
	}
	return result, nil
}

func CreatePreviousStatePlan(
	ctx context.Context,
	currentPlanID,
	successfulPlanID uuid.UUID,
	reason string,
) (*types.DeploymentPlan, error) {
	actorUserAccountID := internalctx.GetUserAccount(ctx).ID
	var organizationID uuid.UUID
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT plan.organization_id
		FROM DeploymentPlan plan
		JOIN Organization_UserAccount membership
		  ON membership.organization_id = plan.organization_id
		 AND membership.user_account_id = @actorUserAccountID
		WHERE plan.id = @currentPlanID`,
		pgx.NamedArgs{
			"actorUserAccountID": actorUserAccountID,
			"currentPlanID":      currentPlanID,
		},
	).Scan(&organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("resolve previous-state plan tenant: %w", err)
	}
	return CreatePreviousStatePlanForOrganization(
		ctx,
		organizationID,
		actorUserAccountID,
		currentPlanID,
		successfulPlanID,
		reason,
	)
}

func CreatePreviousStatePlanForOrganization(
	ctx context.Context,
	organizationID,
	actorUserAccountID,
	currentPlanID,
	successfulPlanID uuid.UUID,
	reason string,
	verifiers ...TargetConfigObjectVerifier,
) (*types.DeploymentPlan, error) {
	verifier := NewUnavailableTargetConfigObjectVerifier()
	if len(verifiers) == 1 && verifiers[0] != nil {
		verifier = verifiers[0]
	}
	reason = strings.TrimSpace(reason)
	if organizationID == uuid.Nil || actorUserAccountID == uuid.Nil ||
		currentPlanID == uuid.Nil || successfulPlanID == uuid.Nil {
		return nil, apierrors.NewBadRequest("organization, actor, and plan IDs are required")
	}
	if currentPlanID == successfulPlanID {
		return nil, apierrors.NewBadRequest("current and successful deployment plans must differ")
	}
	if reason == "" || len(reason) > 2048 || strings.ContainsAny(reason, "\r\n") {
		return nil, apierrors.NewBadRequest("previous-state reason is invalid")
	}

	var result *types.DeploymentPlan
	err := RunTxIso(ctx, pgx.Serializable, func(txCtx context.Context) error {
		created, createErr := createPreviousStatePlanInTx(
			txCtx,
			organizationID,
			actorUserAccountID,
			currentPlanID,
			successfulPlanID,
			reason,
			verifier,
		)
		result = created
		return createErr
	})
	if err != nil {
		existing, existingErr := getPreviousStatePlan(
			ctx,
			organizationID,
			currentPlanID,
			successfulPlanID,
		)
		if existingErr == nil {
			return existing, nil
		}
		return nil, err
	}
	return result, nil
}

func createPreviousStatePlanInTx(
	ctx context.Context,
	organizationID,
	actorUserAccountID,
	currentPlanID,
	successfulPlanID uuid.UUID,
	reason string,
	verifier TargetConfigObjectVerifier,
) (*types.DeploymentPlan, error) {
	existing, err := getPreviousStatePlan(
		ctx,
		organizationID,
		currentPlanID,
		successfulPlanID,
	)
	if plan, found, resolveErr := resolveExistingPreviousStatePlan(existing, err); found || resolveErr != nil {
		return plan, resolveErr
	}

	current, err := lockTargetDeploymentPlan(ctx, organizationID, currentPlanID)
	if err != nil {
		return nil, err
	}
	successful, err := lockTargetDeploymentPlan(ctx, organizationID, successfulPlanID)
	if err != nil {
		return nil, err
	}
	if err := validatePreviousStatePlanPair(current, successful); err != nil {
		return nil, err
	}
	if err := ensureCurrentPlanCAS(ctx, *current); err != nil {
		return nil, err
	}
	if err := ensureSuccessfulPlanObserved(
		ctx,
		organizationID,
		successfulPlanID,
		successful.CanonicalPayload,
	); err != nil {
		return nil, err
	}
	if err := ensurePreviousStateIsReversible(
		ctx,
		organizationID,
		currentPlanID,
	); err != nil {
		return nil, err
	}

	environmentAssignmentID, err := previousStateEnvironmentAssignment(
		ctx,
		organizationID,
		*successful.DeploymentUnitID,
	)
	if err != nil {
		return nil, err
	}
	currentID := current.ID
	sourceID := successful.ID
	draft, err := CreateDeploymentPlanDraft(ctx, &types.PlanDraft{
		ID:                         uuid.New(),
		OrganizationID:             organizationID,
		ProductReleaseID:           successful.ReleaseBundleID,
		DeploymentUnitID:           *successful.DeploymentUnitID,
		EnvironmentAssignmentID:    environmentAssignmentID,
		TargetConfigSnapshotID:     *successful.TargetConfigSnapshotID,
		ProtocolVersion:            types.DeploymentPlanProtocolV2,
		SupersedesDeploymentPlanID: &currentID,
		SupersedeReason:            reason,
		PreviousStateSourcePlanID:  &sourceID,
	})
	if err != nil {
		return nil, err
	}
	validation, err := validateDeploymentPlanDraft(ctx, draft, verifier)
	if err != nil {
		return nil, err
	}
	if len(validation.Issues) > 0 {
		return nil, &DeploymentPlanDraftValidationError{Issues: validation.Issues}
	}
	if err := addPreviousStateEvidence(
		draft,
		validation,
		currentPlanID,
		successfulPlanID,
	); err != nil {
		return nil, err
	}
	return publishValidatedTargetPlan(
		ctx,
		*draft,
		*validation,
		actorUserAccountID,
	)
}

func validatePreviousStatePlanPair(
	current,
	successful *types.DeploymentPlan,
) error {
	if current.PlanSchema != types.TargetDeploymentPlanSchemaV2 ||
		successful.PlanSchema != types.TargetDeploymentPlanSchemaV2 {
		return apierrors.NewBadRequest("previous-state planning requires target deployment plan v2")
	}
	if current.OrganizationID == uuid.Nil ||
		current.OrganizationID != successful.OrganizationID {
		return apierrors.NewBadRequest(
			"current and successful plans must belong to the same organization",
		)
	}
	if current.DeploymentUnitID == nil || successful.DeploymentUnitID == nil ||
		*current.DeploymentUnitID != *successful.DeploymentUnitID ||
		current.ApplicationID != successful.ApplicationID ||
		current.EnvironmentID != successful.EnvironmentID {
		return apierrors.NewBadRequest(
			"current and successful plans must belong to the same exact placement",
		)
	}
	if successful.Status != types.DeploymentPlanStatusExecuted {
		return apierrors.NewConflict("previous-state source plan is not successful")
	}
	if successful.TargetConfigSnapshotID == nil || successful.DraftID == nil {
		return apierrors.NewConflict("successful plan does not retain exact v2 source facts")
	}
	return nil
}

func previousStateEnvironmentAssignment(
	ctx context.Context,
	organizationID,
	deploymentUnitID uuid.UUID,
) (uuid.UUID, error) {
	var environmentAssignmentID uuid.UUID
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT target_environment_assignment_id
		FROM DeploymentUnit
		WHERE id = @deploymentUnitID
		  AND organization_id = @organizationID
		  AND retired_at IS NULL`,
		pgx.NamedArgs{
			"deploymentUnitID": deploymentUnitID,
			"organizationID":   organizationID,
		},
	).Scan(&environmentAssignmentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, apierrors.NewConflict("successful deployment unit is no longer active")
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("query previous-state environment assignment: %w", err)
	}
	return environmentAssignmentID, nil
}

func addPreviousStateEvidence(
	draft *types.PlanDraft,
	validation *types.PlanDraftValidation,
	currentPlanID,
	successfulPlanID uuid.UUID,
) error {
	sourceID := successfulPlanID
	draft.PreviousStateSourcePlanID = &sourceID
	validation.Draft.PreviousStateSourcePlanID = &sourceID

	validation.Changes = append(validation.Changes, types.DeploymentPlanChangeEntry{
		Kind:         types.DeploymentPlanChangePreviousState,
		Before:       currentPlanID.String(),
		After:        successfulPlanID.String(),
		ComponentKey: "",
	})
	sortPlanChanges(validation.Changes)
	validation.Risks = planning.ClassifyDeploymentRisk(
		validation.Changes,
		types.PlanRiskPolicy{
			AllowForwardOnlyMigration:      false,
			RequireBootstrapApproval:       true,
			RequireAuthoritativeV2Baseline: true,
		},
	)
	for index := range validation.Risks {
		validation.Risks[index].SortOrder = index
		checksum, err := checksumPlanEvidence(riskCanonical(validation.Risks[index]))
		if err != nil {
			return err
		}
		validation.Risks[index].CanonicalChecksum = checksum
	}
	canonical := buildTargetPlanCanonical(
		*draft,
		*draft.ResolutionInput,
		validation.Resolutions,
		validation.Graph,
		validation.Baselines,
		validation.Changes,
		validation.Risks,
		validation.Bootstrap,
	)
	payload, checksum, err := planning.CanonicalizeTargetDeploymentPlan(canonical)
	if err != nil {
		return err
	}
	validation.PreviewChecksum = checksum
	validation.Draft.PreviewChecksum = checksum
	validation.Draft.PreviewPayload = payload
	return nil
}

func getPreviousStatePlan(
	ctx context.Context,
	organizationID,
	currentPlanID,
	successfulPlanID uuid.UUID,
) (*types.DeploymentPlan, error) {
	var id uuid.UUID
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT id
		FROM DeploymentPlan
		WHERE organization_id = @organizationID
		  AND supersedes_deployment_plan_id = @currentPlanID
		  AND previous_state_source_plan_id = @successfulPlanID
		  AND plan_schema = 'distr.target-deployment-plan/v2'
		ORDER BY created_at, id
		LIMIT 1`,
		pgx.NamedArgs{
			"organizationID":   organizationID,
			"currentPlanID":    currentPlanID,
			"successfulPlanID": successfulPlanID,
		},
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query existing previous-state plan: %w", err)
	}
	return getDeploymentPlan(ctx, id, organizationID)
}

func lockTargetDeploymentPlan(
	ctx context.Context,
	organizationID,
	id uuid.UUID,
) (*types.DeploymentPlan, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+deploymentPlanOutputExpr+`
		FROM DeploymentPlan dp
		WHERE dp.id = @id
		  AND dp.organization_id = @organizationID
		FOR UPDATE`,
		pgx.NamedArgs{"id": id, "organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("lock target deployment plan: %w", err)
	}
	plan, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.DeploymentPlan])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("collect locked target deployment plan: %w", err)
	}
	return &plan, nil
}

func ensureCurrentPlanCAS(ctx context.Context, current types.DeploymentPlan) error {
	var newer bool
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1
		  FROM DeploymentPlan candidate
		  WHERE candidate.organization_id = @organizationID
		    AND candidate.deployment_unit_id = @deploymentUnitID
		    AND candidate.plan_schema = 'distr.target-deployment-plan/v2'
		    AND (
		      candidate.created_at > @currentCreatedAt
		      OR (
		        candidate.created_at = @currentCreatedAt
		        AND candidate.id > @currentPlanID
		      )
		    )
		)`,
		pgx.NamedArgs{
			"organizationID":   current.OrganizationID,
			"deploymentUnitID": current.DeploymentUnitID,
			"currentCreatedAt": current.CreatedAt,
			"currentPlanID":    current.ID,
		},
	).Scan(&newer)
	if err != nil {
		return fmt.Errorf("check previous-state stale CAS: %w", err)
	}
	return rejectStaleCurrentPlan(newer)
}

func ensureSuccessfulPlanObserved(
	ctx context.Context,
	organizationID,
	successfulPlanID uuid.UUID,
	canonicalPayload []byte,
) error {
	var canonical types.TargetDeploymentPlanCanonical
	if err := json.Unmarshal(canonicalPayload, &canonical); err != nil {
		return apierrors.NewConflict("successful plan canonical payload is invalid")
	}
	planned, err := plannedStatesFromCanonical(canonical)
	if err != nil || len(planned) == 0 {
		return apierrors.NewConflict("successful plan contains no component release pins")
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT DISTINCT observation.component_instance_id,
		       observation.release_bundle_id,
		       observation.image,
		       observation.platform,
		       observation.config_checksum
		FROM ExternalExecution execution
		JOIN TargetComponentObservation observation
		  ON observation.external_execution_id = execution.id
		 AND observation.organization_id = execution.organization_id
		 AND observation.health = 'HEALTHY'
		 AND observation.state_checksum = execution.observed_state_checksum
		 AND observation.config_checksum = execution.actual_config_checksum
		WHERE execution.organization_id = @organizationID
		  AND execution.deployment_plan_id = @successfulPlanID
		  AND execution.status = 'SUCCEEDED'
		LIMIT 4097`,
		pgx.NamedArgs{
			"organizationID":   organizationID,
			"successfulPlanID": successfulPlanID,
		},
	)
	if err != nil {
		return fmt.Errorf("query successful plan observations: %w", err)
	}
	defer rows.Close()
	rowCount := 0
	observed := make([]successfulComponentObservation, 0)
	for rows.Next() {
		rowCount++
		if rowCount > maxPlanEvidenceRows {
			return apierrors.NewConflict("successful plan observation limit exceeded")
		}
		var componentInstanceID *uuid.UUID
		var releaseID uuid.UUID
		var image, platform, configChecksum string
		if err := rows.Scan(
			&componentInstanceID,
			&releaseID,
			&image,
			&platform,
			&configChecksum,
		); err != nil {
			return fmt.Errorf("scan successful plan observation: %w", err)
		}
		if componentInstanceID == nil {
			continue
		}
		observed = append(observed, successfulComponentObservation{
			ComponentInstanceID: *componentInstanceID,
			ReleaseBundleID:     releaseID,
			Image:               image,
			Platform:            platform,
			ConfigChecksum:      configChecksum,
		})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("collect successful plan observations: %w", err)
	}
	if len(missingSuccessfulObservationCoverage(planned, observed)) > 0 {
		return apierrors.NewConflict(
			"successful plan lacks independent healthy observations for every component",
		)
	}
	return nil
}

type componentObservationKey struct {
	ComponentInstanceID uuid.UUID
	ReleaseBundleID     uuid.UUID
}

type successfulComponentObservation struct {
	ComponentInstanceID uuid.UUID
	ReleaseBundleID     uuid.UUID
	Image               string
	Platform            string
	ConfigChecksum      string
}

func missingSuccessfulObservationCoverage(
	planned []types.PlannedState,
	observed []successfulComponentObservation,
) []componentObservationKey {
	missing := make(map[componentObservationKey]types.PlannedState, len(planned))
	for _, state := range planned {
		key := componentObservationKey{
			ComponentInstanceID: state.ComponentInstanceID,
			ReleaseBundleID:     state.ReleaseBundleID,
		}
		missing[key] = state
	}
	for _, observation := range observed {
		key := componentObservationKey{
			ComponentInstanceID: observation.ComponentInstanceID,
			ReleaseBundleID:     observation.ReleaseBundleID,
		}
		state, ok := missing[key]
		if !ok ||
			observation.Platform != state.Platform ||
			observation.ConfigChecksum != state.ConfigChecksum ||
			!imageDigestMatches(observation.Image, state.Image) {
			continue
		}
		delete(missing, key)
	}
	result := make([]componentObservationKey, 0, len(missing))
	for key := range missing {
		result = append(result, key)
	}
	slices.SortFunc(result, func(a, b componentObservationKey) int {
		if cmp := strings.Compare(a.ComponentInstanceID.String(), b.ComponentInstanceID.String()); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.ReleaseBundleID.String(), b.ReleaseBundleID.String())
	})
	return result
}

func ensurePreviousStateIsReversible(
	ctx context.Context,
	organizationID,
	currentPlanID uuid.UUID,
) error {
	var forwardOnly bool
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1
		  FROM DeploymentPlanChangeEntry change
		  WHERE change.organization_id = @organizationID
		    AND change.deployment_plan_id = @currentPlanID
		    AND change.kind = 'schema'
		    AND change.forward_only
		)`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"currentPlanID":  currentPlanID,
		},
	).Scan(&forwardOnly)
	if err != nil {
		return fmt.Errorf("check previous-state forward-only block: %w", err)
	}
	return rejectForwardOnlyPreviousState(forwardOnly)
}

func rejectStaleCurrentPlan(newer bool) error {
	if newer {
		return apierrors.NewConflict("current deployment plan is stale")
	}
	return nil
}

func rejectForwardOnlyPreviousState(forwardOnly bool) error {
	if forwardOnly {
		return apierrors.NewConflict(
			"current plan contains a forward-only schema transition; use a forward fix",
		)
	}
	return nil
}

func resolveExistingPreviousStatePlan(
	existing *types.DeploymentPlan,
	err error,
) (*types.DeploymentPlan, bool, error) {
	if err == nil {
		return existing, true, nil
	}
	if errors.Is(err, apierrors.ErrNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func providerBindingsChecksum(
	componentKey string,
	componentInstanceID uuid.UUID,
	resolutions []types.RequirementResolution,
) (string, error) {
	selected := make([]types.RequirementResolution, 0)
	for _, resolution := range resolutions {
		if resolution.ConsumerKey == componentKey ||
			(resolution.ComponentInstanceID != nil &&
				*resolution.ComponentInstanceID == componentInstanceID) {
			selected = append(selected, resolution)
		}
	}
	slices.SortFunc(selected, func(a, b types.RequirementResolution) int {
		return strings.Compare(a.RequirementKey, b.RequirementKey)
	})
	return checksumPlanEvidence(selected)
}

func migrationState(
	migrations []types.MigrationDeclaration,
) (string, string, bool, error) {
	normalized := slices.Clone(migrations)
	slices.SortFunc(normalized, func(a, b types.MigrationDeclaration) int {
		if a.Order != b.Order {
			return a.Order - b.Order
		}
		return strings.Compare(a.Key, b.Key)
	})
	forwardOnly := false
	keys := make([]string, 0, len(normalized))
	for _, migration := range normalized {
		keys = append(keys, migration.Key+"@"+migration.Compatibility)
		if migration.Compatibility == "breaking" ||
			migration.FailurePolicy == "forward-fix" {
			forwardOnly = true
		}
	}
	checksum, err := checksumPlanEvidence(normalized)
	return strings.Join(keys, ","), checksum, forwardOnly, err
}

func baselineStateFromEvidence(baseline types.DeploymentPlanBaseline) types.BaselineState {
	var releaseID uuid.UUID
	if baseline.ReleaseBundleID != nil {
		releaseID = *baseline.ReleaseBundleID
	}
	return types.BaselineState{
		ComponentInstanceID:     baseline.ComponentInstanceID,
		ComponentKey:            baseline.ComponentKey,
		ReleaseBundleID:         releaseID,
		Version:                 baseline.Version,
		Image:                   baseline.Image,
		Platform:                baseline.Platform,
		ConfigSnapshotID:        baseline.ConfigSnapshotID,
		ConfigChecksum:          baseline.ConfigChecksum,
		ProviderBindingChecksum: baseline.ProviderBindingChecksum,
		SchemaState:             baseline.SchemaState,
		SchemaChecksum:          baseline.SchemaChecksum,
		TopologyChecksum:        baseline.TopologyChecksum,
		Projection:              baseline.Projection,
		Bootstrap:               baseline.Bootstrap,
	}
}

func sortPlanChanges(changes []types.DeploymentPlanChangeEntry) {
	slices.SortStableFunc(changes, func(a, b types.DeploymentPlanChangeEntry) int {
		if cmp := strings.Compare(a.ComponentKey, b.ComponentKey); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(string(a.Kind), string(b.Kind)); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.ComponentInstanceID.String(), b.ComponentInstanceID.String())
	})
	for index := range changes {
		changes[index].SortOrder = index
		checksum, err := checksumPlanEvidence(changeCanonical(changes[index]))
		if err == nil {
			changes[index].CanonicalChecksum = checksum
		}
	}
}

func checksumPlanEvidence(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal deployment plan evidence: %w", err)
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func baselineCanonical(baseline types.DeploymentPlanBaseline) any {
	baseline.ID = uuid.Nil
	baseline.CreatedAt = baseline.CreatedAt.UTC()
	baseline.DeploymentPlanID = uuid.Nil
	baseline.OrganizationID = uuid.Nil
	baseline.ActorUserAccountID = uuid.Nil
	baseline.CanonicalChecksum = ""
	return baseline
}

func changeCanonical(change types.DeploymentPlanChangeEntry) any {
	change.ID = uuid.Nil
	change.CreatedAt = change.CreatedAt.UTC()
	change.DeploymentPlanID = uuid.Nil
	change.OrganizationID = uuid.Nil
	change.ActorUserAccountID = uuid.Nil
	change.CanonicalChecksum = ""
	return change
}

func riskCanonical(risk types.DeploymentPlanRiskEntry) any {
	risk.ID = uuid.Nil
	risk.CreatedAt = risk.CreatedAt.UTC()
	risk.DeploymentPlanID = uuid.Nil
	risk.OrganizationID = uuid.Nil
	risk.ActorUserAccountID = uuid.Nil
	risk.CanonicalChecksum = ""
	return risk
}
