package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/planning"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const deploymentPlanDraftOutputExpr = `
	d.id,
	d.created_at,
	d.updated_at,
	d.organization_id,
	d.revision,
	d.product_release_id,
	d.deployment_unit_id,
	d.environment_assignment_id,
	d.target_config_snapshot_id,
	d.protocol_version,
	d.supersedes_deployment_plan_id,
	d.supersede_reason,
	d.preview_checksum,
	d.preview_payload
`

var targetPlanChecksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func CreateDeploymentPlanDraft(
	ctx context.Context,
	draft *types.PlanDraft,
) (*types.PlanDraft, error) {
	if err := validateDeploymentPlanDraftWrite(draft); err != nil {
		return nil, err
	}
	if draft.ID == uuid.Nil {
		draft.ID = uuid.New()
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		INSERT INTO DeploymentPlanDraft AS d (
			id,
			organization_id,
			product_release_id,
			deployment_unit_id,
			environment_assignment_id,
			target_config_snapshot_id,
			protocol_version,
			supersedes_deployment_plan_id,
			supersede_reason
		) VALUES (
			@id,
			@organizationID,
			@productReleaseID,
			@deploymentUnitID,
			@environmentAssignmentID,
			@targetConfigSnapshotID,
			@protocolVersion,
			@supersedesDeploymentPlanID,
			@supersedeReason
		)
		RETURNING `+deploymentPlanDraftOutputExpr,
		pgx.NamedArgs{
			"id":                         draft.ID,
			"organizationID":             draft.OrganizationID,
			"productReleaseID":           draft.ProductReleaseID,
			"deploymentUnitID":           draft.DeploymentUnitID,
			"environmentAssignmentID":    draft.EnvironmentAssignmentID,
			"targetConfigSnapshotID":     draft.TargetConfigSnapshotID,
			"protocolVersion":            draft.ProtocolVersion,
			"supersedesDeploymentPlanID": draft.SupersedesDeploymentPlanID,
			"supersedeReason":            strings.TrimSpace(draft.SupersedeReason),
		},
	)
	if err != nil {
		return nil, mapDeploymentPlanWriteError("create draft", err)
	}
	created, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.PlanDraft])
	if err != nil {
		return nil, mapDeploymentPlanWriteError("collect created draft", err)
	}
	return &created, nil
}

func UpdateDeploymentPlanDraft(
	ctx context.Context,
	draft *types.PlanDraft,
	expectedRevision int64,
) (*types.PlanDraft, error) {
	if err := validateDeploymentPlanDraftWrite(draft); err != nil {
		return nil, err
	}
	if draft.ID == uuid.Nil {
		return nil, apierrors.NewBadRequest("id is required")
	}
	if expectedRevision < 1 {
		return nil, apierrors.NewBadRequest("expectedRevision must be positive")
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		UPDATE DeploymentPlanDraft AS d
		SET product_release_id = @productReleaseID,
		    deployment_unit_id = @deploymentUnitID,
		    environment_assignment_id = @environmentAssignmentID,
		    target_config_snapshot_id = @targetConfigSnapshotID,
		    protocol_version = @protocolVersion,
		    supersedes_deployment_plan_id = @supersedesDeploymentPlanID,
		    supersede_reason = @supersedeReason,
		    revision = d.revision + 1,
		    preview_checksum = '',
		    preview_payload = NULL
		WHERE d.id = @id
		  AND d.organization_id = @organizationID
		  AND d.revision = @expectedRevision
		RETURNING `+deploymentPlanDraftOutputExpr,
		pgx.NamedArgs{
			"id":                         draft.ID,
			"organizationID":             draft.OrganizationID,
			"expectedRevision":           expectedRevision,
			"productReleaseID":           draft.ProductReleaseID,
			"deploymentUnitID":           draft.DeploymentUnitID,
			"environmentAssignmentID":    draft.EnvironmentAssignmentID,
			"targetConfigSnapshotID":     draft.TargetConfigSnapshotID,
			"protocolVersion":            draft.ProtocolVersion,
			"supersedesDeploymentPlanID": draft.SupersedesDeploymentPlanID,
			"supersedeReason":            strings.TrimSpace(draft.SupersedeReason),
		},
	)
	if err != nil {
		return nil, mapDeploymentPlanWriteError("update draft", err)
	}
	updated, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.PlanDraft])
	if errors.Is(err, pgx.ErrNoRows) {
		existing, getErr := GetDeploymentPlanDraft(ctx, draft.ID, draft.OrganizationID)
		if errors.Is(getErr, apierrors.ErrNotFound) {
			return nil, apierrors.ErrNotFound
		}
		if getErr != nil {
			return nil, getErr
		}
		return nil, apierrors.NewConflict(fmt.Sprintf(
			"deployment plan draft revision mismatch: expected %d, current %d",
			expectedRevision,
			existing.Revision,
		))
	}
	if err != nil {
		return nil, mapDeploymentPlanWriteError("collect updated draft", err)
	}
	return &updated, nil
}

func GetDeploymentPlanDraft(
	ctx context.Context,
	id uuid.UUID,
	organizationID uuid.UUID,
) (*types.PlanDraft, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+deploymentPlanDraftOutputExpr+`
		FROM DeploymentPlanDraft d
		WHERE d.id = @id
		  AND d.organization_id = @organizationID`,
		pgx.NamedArgs{"id": id, "organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query DeploymentPlanDraft: %w", err)
	}
	draft, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.PlanDraft])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not collect DeploymentPlanDraft: %w", err)
	}
	var planID uuid.UUID
	var status types.DeploymentPlanStatus
	err = internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT id, status
		FROM DeploymentPlan
		WHERE draft_id = @draftID
		  AND organization_id = @organizationID`,
		pgx.NamedArgs{"draftID": id, "organizationID": organizationID},
	).Scan(&planID, &status)
	if err == nil {
		draft.PublishedDeploymentPlanID = &planID
		draft.PublishedDeploymentPlanStatus = string(status)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("could not query draft publication: %w", err)
	}
	return &draft, nil
}

func ValidateDeploymentPlanDraft(
	ctx context.Context,
	id uuid.UUID,
	organizationID uuid.UUID,
) (*types.PlanDraftValidation, error) {
	draft, err := GetDeploymentPlanDraft(ctx, id, organizationID)
	if err != nil {
		return nil, err
	}
	return validateDeploymentPlanDraft(ctx, draft)
}

func validateDeploymentPlanDraft(
	ctx context.Context,
	draft *types.PlanDraft,
) (*types.PlanDraftValidation, error) {
	input, err := loadPlanResolutionInput(ctx, *draft)
	if err != nil {
		return nil, err
	}
	draft.ResolutionInput = input
	issues := planning.ValidatePlanDraft(ctx, *draft)
	resolutions, resolutionIssues := planning.ResolveTargetRequirements(ctx, *draft)
	if len(resolutionIssues) > 0 {
		issues = appendUniquePlanIssues(issues, resolutionIssues)
	}
	result := &types.PlanDraftValidation{
		Draft: *draft, Resolutions: resolutions, Issues: issues,
	}
	if len(issues) > 0 {
		return result, nil
	}
	graph, err := planning.BuildTargetPlanGraph(ctx, *draft, resolutions)
	if err != nil {
		result.Issues = append(result.Issues, types.ValidationIssue{
			Code: "invalid_plan_graph", Field: "graph", Message: err.Error(),
		})
		return result, nil
	}
	if err := planning.ValidateProtocolGraph(draft.ProtocolVersion, graph); err != nil {
		result.Issues = append(result.Issues, types.ValidationIssue{
			Code: "protocol_graph_mismatch", Field: "protocolVersion", Message: err.Error(),
		})
		return result, nil
	}
	canonical := buildTargetPlanCanonical(*draft, *input, resolutions, graph)
	payload, checksum, err := planning.CanonicalizeTargetDeploymentPlan(canonical)
	if err != nil {
		return nil, err
	}
	result.Graph = graph
	result.PreviewChecksum = checksum
	result.Draft.PreviewChecksum = checksum
	result.Draft.PreviewPayload = payload
	return result, nil
}

func PublishTargetDeploymentPlan(
	ctx context.Context,
	draftID uuid.UUID,
	organizationID uuid.UUID,
	expectedRevision int64,
	expectedPreviewChecksum string,
) (*types.DeploymentPlan, error) {
	if expectedRevision < 1 {
		return nil, apierrors.NewBadRequest("expectedRevision must be positive")
	}
	if !targetPlanChecksumPattern.MatchString(expectedPreviewChecksum) {
		return nil, apierrors.NewBadRequest("expectedPreviewChecksum must be lowercase sha256")
	}
	var published *types.DeploymentPlan
	err := RunTxIso(ctx, pgx.Serializable, func(txCtx context.Context) error {
		draft, err := lockDeploymentPlanDraft(txCtx, draftID, organizationID)
		if err != nil {
			return err
		}
		existing, err := getDeploymentPlanByDraftID(txCtx, draftID, organizationID)
		if err == nil {
			if existing.CanonicalChecksum != expectedPreviewChecksum {
				return apierrors.NewConflict(
					"draft is already published with a different canonical checksum",
				)
			}
			published = existing
			return nil
		}
		if !errors.Is(err, apierrors.ErrNotFound) {
			return err
		}
		if draft.Revision != expectedRevision {
			return apierrors.NewConflict(fmt.Sprintf(
				"deployment plan draft revision mismatch: expected %d, current %d",
				expectedRevision,
				draft.Revision,
			))
		}
		validation, err := validateDeploymentPlanDraft(txCtx, draft)
		if err != nil {
			return err
		}
		if len(validation.Issues) > 0 {
			return &DeploymentPlanDraftValidationError{Issues: validation.Issues}
		}
		if validation.PreviewChecksum != expectedPreviewChecksum {
			return apierrors.NewConflict(
				"deployment plan preview changed; validate the current draft and retry",
			)
		}
		plan, err := publishValidatedTargetPlan(txCtx, *draft, *validation)
		if err != nil {
			return err
		}
		published = plan
		return nil
	})
	if err != nil {
		return nil, err
	}
	return published, nil
}

type DeploymentPlanDraftValidationError struct {
	Issues []types.ValidationIssue
}

func (e *DeploymentPlanDraftValidationError) Error() string {
	return "deployment plan draft validation failed"
}

func (e *DeploymentPlanDraftValidationError) Unwrap() error {
	return apierrors.ErrBadRequest
}

func publishValidatedTargetPlan(
	ctx context.Context,
	draft types.PlanDraft,
	validation types.PlanDraftValidation,
) (*types.DeploymentPlan, error) {
	bundle, _, err := GetProductRelease(ctx, draft.ProductReleaseID, draft.OrganizationID)
	if err != nil {
		return nil, err
	}
	input := draft.ResolutionInput
	target, err := getTargetPlanDeploymentTarget(
		ctx,
		draft.OrganizationID,
		input.Assignment.DeploymentTargetID,
	)
	if err != nil {
		return nil, err
	}
	status := types.DeploymentPlanStatusReady
	issues := []types.DeploymentPlanIssue{}
	if draft.ProtocolVersion == types.DeploymentPlanProtocolV2 {
		status = types.DeploymentPlanStatusBlocked
		issues = append(issues, types.DeploymentPlanIssue{
			ID: uuid.New(), OrganizationID: draft.OrganizationID,
			Severity: types.DeploymentPlanIssueSeverityBlocker,
			Code:     "protocol_v2_execution_deferred", Field: "protocolVersion",
			Message: "protocol v2 execution remains disabled until the fenced executor protocol is installed",
		})
	}
	planTargetID := uuid.New()
	plan := &types.DeploymentPlan{
		ID:                         uuid.New(),
		OrganizationID:             draft.OrganizationID,
		ApplicationID:              bundle.ApplicationID,
		ReleaseBundleID:            bundle.ID,
		ChannelID:                  bundle.ChannelID,
		EnvironmentID:              input.Assignment.EnvironmentID,
		ProcessSnapshotID:          bundle.ProcessSnapshotID,
		VariableSnapshotID:         bundle.VariableSnapshotID,
		ReleaseContract:            bundle.ReleaseContract,
		PlanSchema:                 types.TargetDeploymentPlanSchemaV2,
		DraftID:                    &draft.ID,
		DeploymentUnitID:           &draft.DeploymentUnitID,
		TargetConfigSnapshotID:     &draft.TargetConfigSnapshotID,
		ProtocolVersion:            draft.ProtocolVersion,
		SupersedesDeploymentPlanID: draft.SupersedesDeploymentPlanID,
		SupersedeReason:            strings.TrimSpace(draft.SupersedeReason),
		Status:                     status,
		CanonicalChecksum:          validation.PreviewChecksum,
		CanonicalPayload:           validation.Draft.PreviewPayload,
		Targets: []types.DeploymentPlanTarget{{
			ID: planTargetID, OrganizationID: draft.OrganizationID,
			DeploymentTargetID: target.ID, Name: target.Name, Type: target.Type,
			Platform: target.Platform, CustomerOrganizationID: target.CustomerOrganizationID,
		}},
		Steps:                projectTargetPlanSteps(validation.Graph),
		Issues:               issues,
		ResolvedRequirements: validation.Resolutions,
		StepEdges:            validation.Graph.Edges,
	}
	if err := insertPublishedTargetPlan(ctx, plan); err != nil {
		return nil, err
	}
	if err := insertDeploymentPlanTargets(ctx, *plan); err != nil {
		return nil, err
	}
	if err := insertDeploymentPlanSteps(ctx, *plan); err != nil {
		return nil, err
	}
	if err := insertDeploymentPlanIssues(ctx, *plan); err != nil {
		return nil, err
	}
	if err := insertDeploymentPlanResolvedRequirements(ctx, *plan); err != nil {
		return nil, err
	}
	if err := insertDeploymentPlanStepEdges(ctx, *plan); err != nil {
		return nil, err
	}
	return getDeploymentPlan(ctx, plan.ID, plan.OrganizationID)
}

func insertPublishedTargetPlan(ctx context.Context, plan *types.DeploymentPlan) error {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		INSERT INTO DeploymentPlan (
			id,
			organization_id,
			release_bundle_id,
			application_id,
			channel_id,
			environment_id,
			process_snapshot_id,
			variable_snapshot_id,
			release_contract,
			plan_schema,
			draft_id,
			deployment_unit_id,
			target_config_snapshot_id,
			protocol_version,
			supersedes_deployment_plan_id,
			supersede_reason,
			status,
			canonical_checksum,
			canonical_payload
		) VALUES (
			@id,
			@organizationID,
			@releaseBundleID,
			@applicationID,
			@channelID,
			@environmentID,
			@processSnapshotID,
			@variableSnapshotID,
			@releaseContract,
			@planSchema,
			@draftID,
			@deploymentUnitID,
			@targetConfigSnapshotID,
			@protocolVersion,
			@supersedesDeploymentPlanID,
			@supersedeReason,
			@status,
			@canonicalChecksum,
			@canonicalPayload
		)
		RETURNING `+deploymentPlanOutputExpr,
		pgx.NamedArgs{
			"id": plan.ID, "organizationID": plan.OrganizationID,
			"releaseBundleID": plan.ReleaseBundleID, "applicationID": plan.ApplicationID,
			"channelID": plan.ChannelID, "environmentID": plan.EnvironmentID,
			"processSnapshotID": plan.ProcessSnapshotID, "variableSnapshotID": plan.VariableSnapshotID,
			"releaseContract": plan.ReleaseContract, "planSchema": plan.PlanSchema,
			"draftID": plan.DraftID, "deploymentUnitID": plan.DeploymentUnitID,
			"targetConfigSnapshotID":     plan.TargetConfigSnapshotID,
			"protocolVersion":            plan.ProtocolVersion,
			"supersedesDeploymentPlanID": plan.SupersedesDeploymentPlanID,
			"supersedeReason":            plan.SupersedeReason, "status": plan.Status,
			"canonicalChecksum": plan.CanonicalChecksum, "canonicalPayload": plan.CanonicalPayload,
		},
	)
	if err != nil {
		return mapDeploymentPlanWriteError("publish target plan", err)
	}
	inserted, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.DeploymentPlan])
	if err != nil {
		return mapDeploymentPlanWriteError("collect published target plan", err)
	}
	plan.CreatedAt = inserted.CreatedAt
	return nil
}

func lockDeploymentPlanDraft(
	ctx context.Context,
	id uuid.UUID,
	organizationID uuid.UUID,
) (*types.PlanDraft, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+deploymentPlanDraftOutputExpr+`
		FROM DeploymentPlanDraft d
		WHERE d.id = @id
		  AND d.organization_id = @organizationID
		FOR UPDATE`,
		pgx.NamedArgs{"id": id, "organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not lock DeploymentPlanDraft: %w", err)
	}
	draft, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.PlanDraft])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not collect locked DeploymentPlanDraft: %w", err)
	}
	return &draft, nil
}

func getDeploymentPlanByDraftID(
	ctx context.Context,
	draftID uuid.UUID,
	organizationID uuid.UUID,
) (*types.DeploymentPlan, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+deploymentPlanOutputExpr+`
		FROM DeploymentPlan dp
		WHERE dp.draft_id = @draftID
		  AND dp.organization_id = @organizationID`,
		pgx.NamedArgs{"draftID": draftID, "organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query target plan by draft: %w", err)
	}
	plan, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.DeploymentPlan])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not collect target plan by draft: %w", err)
	}
	if err := hydrateDeploymentPlan(ctx, &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

func validateDeploymentPlanDraftWrite(draft *types.PlanDraft) error {
	if draft == nil {
		return apierrors.NewBadRequest("deployment plan draft is required")
	}
	required := []struct {
		name  string
		value uuid.UUID
	}{
		{"organizationId", draft.OrganizationID},
		{"productReleaseId", draft.ProductReleaseID},
		{"deploymentUnitId", draft.DeploymentUnitID},
		{"environmentAssignmentId", draft.EnvironmentAssignmentID},
		{"targetConfigSnapshotId", draft.TargetConfigSnapshotID},
	}
	for _, field := range required {
		if field.value == uuid.Nil {
			return apierrors.NewBadRequest(field.name + " is required")
		}
	}
	if draft.ProtocolVersion != types.DeploymentPlanProtocolV1 &&
		draft.ProtocolVersion != types.DeploymentPlanProtocolV2 {
		return apierrors.NewBadRequest("protocolVersion must be v1 or v2")
	}
	if (draft.SupersedesDeploymentPlanID == nil) !=
		(strings.TrimSpace(draft.SupersedeReason) == "") {
		return apierrors.NewBadRequest(
			"supersedesDeploymentPlanId and supersedeReason must be supplied together",
		)
	}
	if len(strings.TrimSpace(draft.SupersedeReason)) > 2048 ||
		strings.ContainsAny(draft.SupersedeReason, "\r\n") {
		return apierrors.NewBadRequest("supersedeReason is invalid")
	}
	return nil
}

func loadPlanResolutionInput(
	ctx context.Context,
	draft types.PlanDraft,
) (*types.PlanResolutionInput, error) {
	bundle, manifest, err := GetProductRelease(
		ctx,
		draft.ProductReleaseID,
		draft.OrganizationID,
	)
	if err != nil {
		return nil, err
	}
	graph, err := GetProductReleaseGraph(ctx, draft.ProductReleaseID, draft.OrganizationID)
	if err != nil {
		return nil, err
	}
	placement, err := getDeploymentRegistryPlacement(
		ctx,
		draft.OrganizationID,
		draft.DeploymentUnitID,
	)
	if err != nil {
		return nil, err
	}
	target, err := getTargetPlanDeploymentTarget(
		ctx,
		draft.OrganizationID,
		placement.Assignment.DeploymentTargetID,
	)
	if err != nil {
		return nil, err
	}
	config, err := loadTargetConfigBinding(
		ctx,
		draft.OrganizationID,
		draft.TargetConfigSnapshotID,
	)
	if err != nil {
		return nil, err
	}
	pins, err := loadTargetPlanReleasePins(
		ctx,
		draft.OrganizationID,
		*manifest,
		config.TargetPlatform,
	)
	if err != nil {
		return nil, err
	}
	requirements := targetRequirementsFromGraph(*graph)
	input := &types.PlanResolutionInput{
		EffectiveAt: placement.EffectiveAt, Assignment: placement.Assignment,
		ActiveAssignments: placement.Assignments, Unit: placement.Unit,
		ActiveUnits: placement.Units, Scope: placement.Scope,
		TargetPlatform:    target.Platform,
		ProductReleaseID:  draft.ProductReleaseID,
		ProductChecksum:   bundle.CanonicalChecksum,
		ProductPublished:  bundle.Status == types.ReleaseBundleStatusPublished,
		RequiredPlatforms: slices.Clone(manifest.RequiredPlatforms),
		ProductEdges:      slices.Clone(graph.Edges), Config: *config,
		Requirements: requirements, ReleasePins: pins,
		ComponentInstances: slices.Clone(placement.Instances),
	}
	input.Candidates = includedAndDisabledCandidates(*input, *manifest)
	observedCandidates, err := loadObservedProviderCandidates(
		ctx,
		draft.OrganizationID,
		*input,
	)
	if err != nil {
		return nil, err
	}
	input.Candidates = append(input.Candidates, observedCandidates...)
	return input, nil
}

func loadTargetConfigBinding(
	ctx context.Context,
	organizationID uuid.UUID,
	id uuid.UUID,
) (*types.TargetConfigBinding, error) {
	var binding types.TargetConfigBinding
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT id,
		       organization_id,
		       deployment_unit_id,
		       target_environment_assignment_id,
		       environment_id,
		       canonical_checksum,
		       target_platform
		FROM TargetConfigSnapshot
		WHERE id = @id
		  AND organization_id = @organizationID`,
		pgx.NamedArgs{"id": id, "organizationID": organizationID},
	).Scan(
		&binding.ID,
		&binding.OrganizationID,
		&binding.DeploymentUnitID,
		&binding.EnvironmentAssignmentID,
		&binding.EnvironmentID,
		&binding.CanonicalChecksum,
		&binding.TargetPlatform,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not query TargetConfigSnapshot plan binding: %w", err)
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT key, checksum
		FROM TargetConfigSnapshotObject
		WHERE target_config_snapshot_id = @id
		  AND organization_id = @organizationID
		ORDER BY key`,
		pgx.NamedArgs{"id": id, "organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query target config object facts: %w", err)
	}
	binding.VerificationFacts, err = pgx.CollectRows(
		rows,
		func(row pgx.CollectableRow) (types.ConfigVerificationFact, error) {
			var fact types.ConfigVerificationFact
			if scanErr := row.Scan(&fact.ObjectKey, &fact.Checksum); scanErr != nil {
				return fact, scanErr
			}
			// The immutable snapshot checksum and its database-enforced object
			// checksums are the frozen verification facts. Remote reachability
			// is rechecked by execution preflight.
			fact.ObservedChecksum = fact.Checksum
			fact.Verified = true
			return fact, nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not collect target config object facts: %w", err)
	}
	rows, err = internalctx.GetDb(ctx).Query(ctx, `
		SELECT definition.key,
		       component.component_instance_id,
		       component.physical_name
		FROM TargetConfigSnapshotComponent component
		JOIN ComponentInstance instance
		  ON instance.id = component.component_instance_id
		 AND instance.deployment_unit_id = component.deployment_unit_id
		 AND instance.organization_id = component.organization_id
		JOIN ComponentDefinition definition
		  ON definition.id = instance.component_definition_id
		 AND definition.organization_id = instance.organization_id
		WHERE component.target_config_snapshot_id = @id
		  AND component.organization_id = @organizationID
		ORDER BY definition.key, component.component_instance_id`,
		pgx.NamedArgs{"id": id, "organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query target config component bindings: %w", err)
	}
	binding.ComponentBindings, err = pgx.CollectRows(
		rows,
		pgx.RowToStructByName[types.ConfigComponentBinding],
	)
	if err != nil {
		return nil, fmt.Errorf("could not collect target config component bindings: %w", err)
	}
	rows, err = internalctx.GetDb(ctx).Query(ctx, `
		SELECT key, enabled
		FROM TargetConfigSnapshotFeatureFlag
		WHERE target_config_snapshot_id = @id
		  AND organization_id = @organizationID
		ORDER BY key`,
		pgx.NamedArgs{"id": id, "organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query target config feature flags: %w", err)
	}
	binding.FeatureFlags = map[string]bool{}
	for rows.Next() {
		var key string
		var enabled bool
		if err := rows.Scan(&key, &enabled); err != nil {
			return nil, fmt.Errorf("could not scan target config feature flag: %w", err)
		}
		binding.FeatureFlags[key] = enabled
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("could not collect target config feature flags: %w", err)
	}
	return &binding, nil
}

func loadTargetPlanReleasePins(
	ctx context.Context,
	organizationID uuid.UUID,
	manifest types.ProductReleaseManifest,
	platform string,
) ([]types.ComponentReleasePin, error) {
	pins := make([]types.ComponentReleasePin, 0, len(manifest.Components))
	for _, component := range manifest.Components {
		rows, err := internalctx.GetDb(ctx).Query(ctx, `
			SELECT artifact.artifact_key,
			       artifact.artifact_type,
			       artifact.media_type,
		       artifact.manifest_digest,
		       artifact.platform,
		       artifact.platform_digest,
		       verification.id,
		       COALESCE(verification.evidence_digest, ''),
		       COALESCE(verification.policy_checksum, ''),
		       COALESCE(verification.trust_root_id::TEXT, '')
			FROM ComponentReleaseArtifact artifact
			LEFT JOIN ComponentReleaseEvidenceVerification verification
			  ON verification.release_bundle_id = artifact.release_bundle_id
			 AND verification.organization_id = artifact.organization_id
			 AND verification.artifact_key = artifact.artifact_key
			 AND verification.platform = artifact.platform
			 AND verification.artifact_digest = artifact.platform_digest
			WHERE artifact.release_bundle_id = @releaseBundleID
			  AND artifact.organization_id = @organizationID
			  AND artifact.platform = @platform
			ORDER BY artifact.artifact_key, artifact.platform`,
			pgx.NamedArgs{
				"releaseBundleID": component.ComponentReleaseID,
				"organizationID":  organizationID,
				"platform":        platform,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("could not query component release plan artifacts: %w", err)
		}
		artifacts := make([]types.PinnedReleaseArtifact, 0)
		provenanceFacts := make([]types.ComponentProvenanceFact, 0)
		provenanceVerified := true
		for rows.Next() {
			var artifact types.PinnedReleaseArtifact
			var (
				verificationID *uuid.UUID
				evidenceDigest string
				policyChecksum string
				trustRootID    string
			)
			if err := rows.Scan(
				&artifact.Key,
				&artifact.Type,
				&artifact.MediaType,
				&artifact.ManifestDigest,
				&artifact.Platform,
				&artifact.PlatformDigest,
				&verificationID,
				&evidenceDigest,
				&policyChecksum,
				&trustRootID,
			); err != nil {
				return nil, fmt.Errorf("could not scan component release plan artifact: %w", err)
			}
			artifacts = append(artifacts, artifact)
			if verificationID == nil {
				provenanceVerified = false
				continue
			}
			provenanceFacts = append(provenanceFacts, types.ComponentProvenanceFact{
				VerificationID: *verificationID,
				ArtifactKey:    artifact.Key,
				Platform:       artifact.Platform,
				ArtifactDigest: artifact.PlatformDigest,
				EvidenceDigest: evidenceDigest,
				PolicyChecksum: policyChecksum,
				TrustRootID:    trustRootID,
			})
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("could not collect component release plan artifacts: %w", err)
		}
		if len(artifacts) == 0 {
			provenanceVerified = false
		}
		provenanceBindingChecksum := ""
		if provenanceVerified && len(provenanceFacts) == len(artifacts) {
			provenanceBindingChecksum, err = targetPlanFactChecksum(provenanceFacts)
			if err != nil {
				return nil, fmt.Errorf(
					"could not checksum component release provenance facts: %w",
					err,
				)
			}
		}
		pin := types.ComponentReleasePin{
			ComponentKey: component.ComponentKey, ComponentReleaseID: component.ComponentReleaseID,
			ReleaseChecksum: component.ComponentReleaseChecksum, Version: component.Version,
			Platforms: slices.Clone(component.Platforms), Artifacts: artifacts,
			ProvenanceVerified:        provenanceVerified,
			ProvenanceBindingChecksum: provenanceBindingChecksum,
			ProvenanceFacts:           provenanceFacts,
			Migrations:                slices.Clone(component.Migrations),
		}
		if len(artifacts) == 1 {
			pin.PlatformDigest = artifacts[0].PlatformDigest
		}
		pins = append(pins, pin)
	}
	slices.SortFunc(pins, func(a, b types.ComponentReleasePin) int {
		return strings.Compare(a.ComponentKey, b.ComponentKey)
	})
	return pins, nil
}

func targetRequirementsFromGraph(graph types.ProductReleaseGraph) []types.TargetRequirement {
	requirements := make([]types.TargetRequirement, 0)
	for _, edge := range graph.Edges {
		if edge.ResolutionStage != types.CapabilityResolutionStageTarget {
			continue
		}
		requirements = append(requirements, types.TargetRequirement{
			Key: edge.From, ConsumerKey: strings.TrimPrefix(edge.To, "component:"),
			Capability: edge.Capability, VersionRange: edge.VersionRange,
			AllowedModes: slices.Clone(edge.AllowedModes),
		})
	}
	slices.SortFunc(requirements, func(a, b types.TargetRequirement) int {
		return strings.Compare(a.Key, b.Key)
	})
	return requirements
}

func includedAndDisabledCandidates(
	input types.PlanResolutionInput,
	manifest types.ProductReleaseManifest,
) []types.RequirementProviderCandidate {
	bindingByComponent := make(map[string]types.ConfigComponentBinding, len(input.Config.ComponentBindings))
	for _, binding := range input.Config.ComponentBindings {
		bindingByComponent[binding.ComponentKey] = binding
	}
	pinByComponent := make(map[string]types.ComponentReleasePin, len(input.ReleasePins))
	for _, pin := range input.ReleasePins {
		pinByComponent[pin.ComponentKey] = pin
	}
	candidates := make([]types.RequirementProviderCandidate, 0)
	for _, requirement := range input.Requirements {
		if slices.Contains(requirement.AllowedModes, types.RequirementResolutionModeIncluded) {
			for _, component := range manifest.Components {
				for _, capability := range component.Provides {
					if capability.Name != requirement.Capability {
						continue
					}
					binding, bound := bindingByComponent[component.ComponentKey]
					pin, pinned := pinByComponent[component.ComponentKey]
					if !bound || !pinned {
						continue
					}
					releaseID := component.ComponentReleaseID
					instanceID := binding.ComponentInstanceID
					candidates = append(candidates, types.RequirementProviderCandidate{
						RequirementKey:    requirement.Key,
						Mode:              types.RequirementResolutionModeIncluded,
						ProviderReleaseID: &releaseID, ProviderVersion: capability.Version,
						ProviderPlatform: input.Config.TargetPlatform,
						DeploymentUnitID: input.Unit.ID, ComponentInstanceID: &instanceID,
						ExpectedStateVersion: 0, ObservedStateVersion: 0,
						ExpectedStateChecksum:     pin.ReleaseChecksum,
						ObservedStateChecksum:     pin.ReleaseChecksum,
						ProviderReleaseChecksum:   pin.ReleaseChecksum,
						ProvenanceBindingChecksum: pin.ProvenanceBindingChecksum,
						ProvenanceVerified:        pin.ProvenanceVerified,
						V1Compatible:              true,
					})
				}
			}
		}
		if slices.Contains(requirement.AllowedModes, types.RequirementResolutionModeFeatureDisabled) {
			if enabled, exists := input.Config.FeatureFlags[requirement.Capability]; exists && !enabled {
				candidates = append(candidates, types.RequirementProviderCandidate{
					RequirementKey:  requirement.Key,
					Mode:            types.RequirementResolutionModeFeatureDisabled,
					ProviderVersion: "disabled", ProviderPlatform: input.Config.TargetPlatform,
					ExpectedStateVersion: 0, ObservedStateVersion: 0,
					ExpectedStateChecksum: input.Config.CanonicalChecksum,
					ObservedStateChecksum: input.Config.CanonicalChecksum,
					FeatureFlagKey:        requirement.Capability, FeatureFlagEnabled: false,
					ProvenanceVerified: true, V1Compatible: true,
				})
			}
		}
	}
	return candidates
}

func loadObservedProviderCandidates(
	ctx context.Context,
	organizationID uuid.UUID,
	input types.PlanResolutionInput,
) ([]types.RequirementProviderCandidate, error) {
	candidates := make([]types.RequirementProviderCandidate, 0)
	for _, requirement := range input.Requirements {
		allowsExisting := slices.Contains(
			requirement.AllowedModes,
			types.RequirementResolutionModePinnedExisting,
		)
		allowsShared := slices.Contains(
			requirement.AllowedModes,
			types.RequirementResolutionModeSharedProvider,
		)
		allowsExternal := slices.Contains(
			requirement.AllowedModes,
			types.RequirementResolutionModeApprovedExternal,
		)
		if !allowsExisting && !allowsShared && !allowsExternal {
			continue
		}
		rows, err := internalctx.GetDb(ctx).Query(ctx, `
			SELECT release.id,
			       capability.version_or_range,
			       state.platform,
			       release.canonical_checksum,
			       unit.id,
			       instance.id,
			       unit.subscriber_set_checksum,
			       observation.id,
			       state.state_version,
			       state.state_checksum,
			       observation.state_version,
			       observation.state_checksum,
			       scope.delivery_model,
			       instance.management_state,
			       EXISTS (
			         SELECT 1
			         FROM ComponentReleaseArtifact artifact
			         WHERE artifact.release_bundle_id = release.id
			           AND artifact.organization_id = release.organization_id
			           AND artifact.platform = state.platform
			       )
			       AND NOT EXISTS (
			         SELECT 1
			         FROM ComponentReleaseArtifact artifact
			         WHERE artifact.release_bundle_id = release.id
			           AND artifact.organization_id = release.organization_id
			           AND artifact.platform = state.platform
			           AND NOT EXISTS (
			             SELECT 1
			             FROM ComponentReleaseEvidenceVerification verification
			             WHERE verification.release_bundle_id = artifact.release_bundle_id
			               AND verification.organization_id = artifact.organization_id
			               AND verification.artifact_key = artifact.artifact_key
			               AND verification.platform = artifact.platform
			               AND verification.artifact_digest = artifact.platform_digest
			           )
			       ) AS provenance_verified,
			       COALESCE((
			         SELECT 'sha256:' || encode(
			           sha256(convert_to(
			             string_agg(
			               verification.id::TEXT || '|' ||
			               verification.artifact_key || '|' ||
			               verification.platform || '|' ||
			               verification.artifact_digest || '|' ||
			               verification.evidence_digest || '|' ||
			               verification.policy_checksum || '|' ||
			               verification.trust_root_id::TEXT,
			               E'\n'
			               ORDER BY verification.artifact_key,
			                        verification.platform,
			                        verification.id
			             ),
			             'UTF8'
			           )),
			           'hex'
			         )
			         FROM ComponentReleaseEvidenceVerification verification
			         WHERE verification.release_bundle_id = release.id
			           AND verification.organization_id = release.organization_id
			           AND verification.platform = state.platform
			       ), '') AS provenance_binding_checksum
			FROM ComponentReleaseCapability capability
			JOIN ReleaseBundle release
			  ON release.id = capability.release_bundle_id
			 AND release.organization_id = capability.organization_id
			 AND release.kind = 'component'
			 AND release.status = 'PUBLISHED'
			JOIN TargetComponentState state
			  ON state.release_bundle_id = release.id
			 AND state.organization_id = release.organization_id
			 AND state.health = 'HEALTHY'
			JOIN LATERAL (
			  SELECT observed.id,
			         observed.state_version,
			         observed.state_checksum
			  FROM TargetComponentObservation observed
			  WHERE observed.target_component_state_id = state.id
			    AND observed.organization_id = state.organization_id
			    AND observed.health = 'HEALTHY'
			  ORDER BY observed.observed_at DESC,
			           observed.state_version DESC,
			           observed.id DESC
			  LIMIT 1
			) observation ON true
			JOIN DeploymentUnit unit
			  ON unit.organization_id = state.organization_id
			 AND unit.deployment_target_id = state.deployment_target_id
			 AND unit.retired_at IS NULL
			JOIN TargetEnvironmentAssignment assignment
			  ON assignment.id = unit.target_environment_assignment_id
			 AND assignment.deployment_target_id = unit.deployment_target_id
			 AND assignment.organization_id = unit.organization_id
			 AND assignment.active_from <= @effectiveAt
			 AND (
			   assignment.active_until IS NULL
			   OR assignment.active_until > @effectiveAt
			 )
			JOIN DeploymentScope scope
			  ON scope.id = unit.deployment_scope_id
			 AND scope.organization_id = unit.organization_id
			 AND scope.retired_at IS NULL
			JOIN ComponentInstance instance
			  ON instance.deployment_unit_id = unit.id
			 AND instance.organization_id = unit.organization_id
			 AND instance.retired_at IS NULL
			JOIN ComponentDefinition definition
			  ON definition.id = instance.component_definition_id
			 AND definition.organization_id = instance.organization_id
			 AND definition.retired_at IS NULL
			 AND (
			   definition.key = state.component
			   OR instance.physical_name = state.component
			 )
			WHERE capability.organization_id = @organizationID
			  AND capability.direction = 'provides'
			  AND capability.name = @capability
			  AND (
			    unit.id = @deploymentUnitID
			    OR (
			      scope.delivery_model = 'shared'
			      AND (
			        @customerOrganizationID::UUID IS NULL
			        OR EXISTS (
			          SELECT 1
			          FROM DeploymentUnitSubscriber subscriber
			          WHERE subscriber.organization_id = unit.organization_id
			            AND subscriber.deployment_unit_id = unit.id
			            AND subscriber.customer_organization_id = @customerOrganizationID
			            AND subscriber.retired_at IS NULL
			        )
			      )
			    )
			    OR scope.delivery_model = 'external'
			    OR instance.management_state = 'external'
			  )
			ORDER BY release.id, unit.id, instance.id, observation.id`,
			pgx.NamedArgs{
				"organizationID":         organizationID,
				"effectiveAt":            input.EffectiveAt,
				"capability":             requirement.Capability,
				"deploymentUnitID":       input.Unit.ID,
				"customerOrganizationID": input.Scope.CustomerOrganizationID,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("could not query observed target requirement providers: %w", err)
		}
		for rows.Next() {
			var (
				releaseID                 uuid.UUID
				version                   string
				platform                  string
				releaseChecksum           string
				unitID                    uuid.UUID
				instanceID                uuid.UUID
				subscriberChecksum        string
				observationID             uuid.UUID
				expectedVersion           int64
				expectedChecksum          string
				observedVersion           int64
				observedChecksum          string
				deliveryModel             types.DeliveryModel
				managementState           types.RegistryManagementState
				provenanceVerified        bool
				provenanceBindingChecksum string
			)
			if err := rows.Scan(
				&releaseID,
				&version,
				&platform,
				&releaseChecksum,
				&unitID,
				&instanceID,
				&subscriberChecksum,
				&observationID,
				&expectedVersion,
				&expectedChecksum,
				&observedVersion,
				&observedChecksum,
				&deliveryModel,
				&managementState,
				&provenanceVerified,
				&provenanceBindingChecksum,
			); err != nil {
				return nil, fmt.Errorf("could not scan observed target requirement provider: %w", err)
			}
			mode := types.RequirementResolutionModePinnedExisting
			componentInstanceID := &instanceID
			switch {
			case deliveryModel == types.DeliveryModelExternal ||
				managementState == types.RegistryManagementStateExternal:
				if !allowsExternal {
					continue
				}
				mode = types.RequirementResolutionModeApprovedExternal
				componentInstanceID = nil
			case unitID != input.Unit.ID:
				if deliveryModel != types.DeliveryModelShared || !allowsShared {
					continue
				}
				mode = types.RequirementResolutionModeSharedProvider
			default:
				if !allowsExisting {
					continue
				}
			}
			releaseIDCopy := releaseID
			observationIDCopy := observationID
			unitIDCopy := unitID
			candidate := types.RequirementProviderCandidate{
				RequirementKey: requirement.Key, Mode: mode,
				ProviderReleaseID: &releaseIDCopy, ObservationID: &observationIDCopy,
				ProviderVersion: version, ProviderPlatform: platform,
				ProviderReleaseChecksum:   releaseChecksum,
				ProvenanceBindingChecksum: provenanceBindingChecksum,
				DeploymentUnitID:          unitIDCopy, ComponentInstanceID: componentInstanceID,
				ExpectedStateVersion:  expectedVersion,
				ExpectedStateChecksum: expectedChecksum,
				ObservedStateVersion:  observedVersion,
				ObservedStateChecksum: observedChecksum,
				ProvenanceVerified:    provenanceVerified,
				V1Compatible:          true,
			}
			if mode == types.RequirementResolutionModeSharedProvider {
				candidate.SubscriberSetChecksum = subscriberChecksum
			}
			candidates = append(candidates, candidate)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("could not collect observed target requirement providers: %w", err)
		}
	}
	slices.SortFunc(candidates, func(a, b types.RequirementProviderCandidate) int {
		if cmp := strings.Compare(a.RequirementKey, b.RequirementKey); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(string(a.Mode), string(b.Mode)); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(a.DeploymentUnitID.String(), b.DeploymentUnitID.String()); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.ObservationID.String(), b.ObservationID.String())
	})
	return candidates, nil
}

func buildTargetPlanCanonical(
	draft types.PlanDraft,
	input types.PlanResolutionInput,
	resolutions []types.RequirementResolution,
	graph types.TargetPlanGraph,
) types.TargetDeploymentPlanCanonical {
	return types.TargetDeploymentPlanCanonical{
		Schema:                       types.TargetDeploymentPlanSchemaV2,
		ProductReleaseID:             draft.ProductReleaseID,
		ProductReleaseChecksum:       input.ProductChecksum,
		DeploymentUnitID:             draft.DeploymentUnitID,
		DeploymentScopeID:            input.Unit.DeploymentScopeID,
		SubscriberSetChecksum:        input.Unit.SubscriberSetChecksum,
		EnvironmentAssignmentID:      draft.EnvironmentAssignmentID,
		EnvironmentID:                input.Assignment.EnvironmentID,
		DeploymentTargetID:           input.Assignment.DeploymentTargetID,
		TargetConfigSnapshotID:       draft.TargetConfigSnapshotID,
		TargetConfigSnapshotChecksum: input.Config.CanonicalChecksum,
		TargetPlatform:               input.Config.TargetPlatform,
		ConfigVerificationFacts:      slices.Clone(input.Config.VerificationFacts),
		ComponentReleasePins:         slices.Clone(input.ReleasePins),
		ComponentBindings:            slices.Clone(input.Config.ComponentBindings),
		RequirementResolutions:       slices.Clone(resolutions),
		Graph:                        graph, ProtocolVersion: draft.ProtocolVersion,
		SupersedesDeploymentPlanID: draft.SupersedesDeploymentPlanID,
		SupersedeReason:            strings.TrimSpace(draft.SupersedeReason),
	}
}

func projectTargetPlanSteps(graph types.TargetPlanGraph) []types.DeploymentPlanStep {
	dependencies := make(map[string][]string)
	for _, edge := range graph.Edges {
		dependencies[edge.ToStepKey] = append(
			dependencies[edge.ToStepKey],
			edge.FromStepKey,
		)
	}
	steps := make([]types.DeploymentPlanStep, 0, len(graph.Steps))
	for _, source := range graph.Steps {
		var input map[string]any
		if len(source.InputBindings) > 0 {
			_ = json.Unmarshal(source.InputBindings, &input)
		}
		if input == nil {
			input = map[string]any{}
		}
		attempts := 1
		if source.RetryClass == "bounded" || source.RetryClass == "safe" {
			attempts = 3
		}
		deps := slices.Clone(dependencies[source.StepKey])
		slices.Sort(deps)
		steps = append(steps, types.DeploymentPlanStep{
			ID: uuid.New(), StepKey: source.StepKey, Name: source.Name,
			ActionType: source.ActionType, ActionName: source.ActionName,
			ExecutionLocation: source.ExecutionLocation, InputBindings: input,
			FailureMode: "stop", TimeoutSeconds: source.TimeoutSeconds,
			RetryMaxAttempts: attempts, RetryIntervalSeconds: 5,
			SortOrder: source.SortOrder, Dependencies: deps, Included: true,
		})
	}
	return steps
}

type targetPlanDeploymentTarget struct {
	ID                     uuid.UUID
	Name                   string
	Type                   types.DeploymentType
	Platform               types.DeploymentTargetPlatform
	CustomerOrganizationID *uuid.UUID
}

func getTargetPlanDeploymentTarget(
	ctx context.Context,
	organizationID uuid.UUID,
	id uuid.UUID,
) (*targetPlanDeploymentTarget, error) {
	var target targetPlanDeploymentTarget
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT id, name, type, platform, customer_organization_id
		FROM DeploymentTarget
		WHERE id = @id
		  AND organization_id = @organizationID`,
		pgx.NamedArgs{"id": id, "organizationID": organizationID},
	).Scan(
		&target.ID,
		&target.Name,
		&target.Type,
		&target.Platform,
		&target.CustomerOrganizationID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not query target plan DeploymentTarget: %w", err)
	}
	return &target, nil
}

func insertDeploymentPlanResolvedRequirements(
	ctx context.Context,
	plan types.DeploymentPlan,
) error {
	if len(plan.ResolvedRequirements) == 0 {
		return nil
	}
	_, err := internalctx.GetDb(ctx).CopyFrom(
		ctx,
		pgx.Identifier{"deploymentplanresolvedrequirement"},
		[]string{
			"id", "deployment_plan_id", "organization_id", "requirement_key",
			"consumer_key", "capability", "version_range", "mode",
			"provider_release_id", "observation_id", "provider_version",
			"provider_platform", "provider_release_checksum",
			"provenance_binding_checksum", "provider_deployment_unit_id",
			"component_instance_id", "subscriber_set_checksum",
			"expected_state_version", "expected_state_checksum",
			"binding_checksum", "sort_order",
		},
		pgx.CopyFromSlice(len(plan.ResolvedRequirements), func(index int) ([]any, error) {
			resolution := plan.ResolvedRequirements[index]
			return []any{
				uuid.New(), plan.ID, plan.OrganizationID, resolution.RequirementKey,
				resolution.ConsumerKey, resolution.Capability, resolution.VersionRange,
				resolution.Mode, resolution.ProviderReleaseID, resolution.ObservationID,
				resolution.ProviderVersion, resolution.ProviderPlatform,
				resolution.ProviderReleaseChecksum, resolution.ProvenanceBindingChecksum,
				resolution.ProviderDeploymentUnitID, resolution.ComponentInstanceID,
				resolution.SubscriberSetChecksum, resolution.ExpectedStateVersion,
				resolution.ExpectedStateChecksum, resolution.BindingChecksum, resolution.SortOrder,
			}, nil
		}),
	)
	if err != nil {
		return mapDeploymentPlanWriteError("insert resolved requirements", err)
	}
	return nil
}

func insertDeploymentPlanStepEdges(ctx context.Context, plan types.DeploymentPlan) error {
	if len(plan.StepEdges) == 0 {
		return nil
	}
	_, err := internalctx.GetDb(ctx).CopyFrom(
		ctx,
		pgx.Identifier{"deploymentplanstepedge"},
		[]string{
			"id", "deployment_plan_id", "organization_id", "edge_key",
			"from_step_key", "to_step_key",
		},
		pgx.CopyFromSlice(len(plan.StepEdges), func(index int) ([]any, error) {
			edge := plan.StepEdges[index]
			return []any{
				uuid.New(), plan.ID, plan.OrganizationID, edge.Key,
				edge.FromStepKey, edge.ToStepKey,
			}, nil
		}),
	)
	if err != nil {
		return mapDeploymentPlanWriteError("insert step edges", err)
	}
	return nil
}

func getDeploymentPlanResolvedRequirements(
	ctx context.Context,
	planID uuid.UUID,
	organizationID uuid.UUID,
) ([]types.RequirementResolution, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT id,
		       deployment_plan_id,
		       organization_id,
		       requirement_key,
		       consumer_key,
		       capability,
		       version_range,
		       mode,
		       provider_release_id,
		       observation_id,
		       provider_version,
		       provider_platform,
		       provider_release_checksum,
		       provenance_binding_checksum,
		       provider_deployment_unit_id,
		       component_instance_id,
		       subscriber_set_checksum,
		       expected_state_version,
		       expected_state_checksum,
		       binding_checksum,
		       sort_order
		FROM DeploymentPlanResolvedRequirement
		WHERE deployment_plan_id = @planID
		  AND organization_id = @organizationID
		ORDER BY sort_order, requirement_key`,
		pgx.NamedArgs{"planID": planID, "organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query DeploymentPlanResolvedRequirement: %w", err)
	}
	result, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.RequirementResolution])
	if err != nil {
		return nil, fmt.Errorf("could not collect DeploymentPlanResolvedRequirement: %w", err)
	}
	return result, nil
}

func getDeploymentPlanStepEdges(
	ctx context.Context,
	planID uuid.UUID,
	organizationID uuid.UUID,
) ([]types.DeploymentPlanStepEdge, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT id,
		       deployment_plan_id,
		       organization_id,
		       edge_key,
		       from_step_key,
		       to_step_key
		FROM DeploymentPlanStepEdge
		WHERE deployment_plan_id = @planID
		  AND organization_id = @organizationID
		ORDER BY edge_key`,
		pgx.NamedArgs{"planID": planID, "organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query DeploymentPlanStepEdge: %w", err)
	}
	result, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.DeploymentPlanStepEdge])
	if err != nil {
		return nil, fmt.Errorf("could not collect DeploymentPlanStepEdge: %w", err)
	}
	return result, nil
}

func appendUniquePlanIssues(
	current []types.ValidationIssue,
	additional []types.ValidationIssue,
) []types.ValidationIssue {
	seen := make(map[string]struct{}, len(current)+len(additional))
	result := make([]types.ValidationIssue, 0, len(current)+len(additional))
	for _, issue := range append(slices.Clone(current), additional...) {
		key := issue.Code + "\x00" + issue.Field + "\x00" + issue.Message
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, issue)
	}
	slices.SortFunc(result, func(a, b types.ValidationIssue) int {
		if cmp := strings.Compare(a.Field, b.Field); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.Code, b.Code)
	})
	return result
}

func targetPlanFactChecksum(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
