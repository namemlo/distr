package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"

	"github.com/distr-sh/distr/internal/actionregistry"
	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const deploymentPlanOutputExpr = `
	dp.id,
	dp.created_at,
	dp.organization_id,
	dp.application_id,
	dp.release_bundle_id,
	dp.channel_id,
	dp.environment_id,
	dp.process_snapshot_id,
	dp.variable_snapshot_id,
	dp.status,
	dp.canonical_checksum,
	dp.canonical_payload
`

type deploymentPlanTargetSource struct {
	ID                     uuid.UUID            `db:"id"`
	Name                   string               `db:"name"`
	Type                   types.DeploymentType `db:"type"`
	CustomerOrganizationID *uuid.UUID           `db:"customer_organization_id"`
}

type canonicalDeploymentPlan struct {
	ReleaseBundleID    string                            `json:"releaseBundleId"`
	ApplicationID      string                            `json:"applicationId"`
	ChannelID          string                            `json:"channelId"`
	EnvironmentID      string                            `json:"environmentId"`
	ProcessSnapshotID  string                            `json:"processSnapshotId,omitempty"`
	VariableSnapshotID string                            `json:"variableSnapshotId,omitempty"`
	Status             string                            `json:"status"`
	Targets            []canonicalDeploymentPlanTarget   `json:"targets"`
	Steps              []canonicalDeploymentPlanStep     `json:"steps"`
	Variables          []canonicalDeploymentPlanVariable `json:"variables"`
	Issues             []canonicalDeploymentPlanIssue    `json:"issues"`
}

type canonicalDeploymentPlanTarget struct {
	DeploymentTargetID     string `json:"deploymentTargetId"`
	Name                   string `json:"name"`
	Type                   string `json:"type"`
	CustomerOrganizationID string `json:"customerOrganizationId,omitempty"`
	SortOrder              int    `json:"sortOrder"`
}

type canonicalDeploymentPlanStep struct {
	StepKey              string         `json:"stepKey"`
	Name                 string         `json:"name"`
	ActionType           string         `json:"actionType"`
	ActionName           string         `json:"actionName"`
	ExecutionLocation    string         `json:"executionLocation"`
	InputBindings        map[string]any `json:"inputBindings"`
	Condition            string         `json:"condition"`
	TargetTags           []string       `json:"targetTags"`
	FailureMode          string         `json:"failureMode"`
	TimeoutSeconds       int            `json:"timeoutSeconds"`
	RetryMaxAttempts     int            `json:"retryMaxAttempts"`
	RetryIntervalSeconds int            `json:"retryIntervalSeconds"`
	RequiredPermissions  []string       `json:"requiredPermissions"`
	SortOrder            int            `json:"sortOrder"`
	Dependencies         []string       `json:"dependencies"`
	Included             bool           `json:"included"`
	ExcludedReason       string         `json:"excludedReason,omitempty"`
}

type canonicalDeploymentPlanVariable struct {
	VariableSetID string                               `json:"variableSetId"`
	VariableID    string                               `json:"variableId"`
	Key           string                               `json:"key"`
	Type          string                               `json:"type"`
	IsRequired    bool                                 `json:"isRequired"`
	Status        string                               `json:"status"`
	Source        string                               `json:"source"`
	Value         json.RawMessage                      `json:"value,omitempty"`
	ReferenceID   string                               `json:"referenceId,omitempty"`
	ReferenceName string                               `json:"referenceName,omitempty"`
	Redacted      bool                                 `json:"redacted"`
	Trace         []types.VariableResolutionTraceEntry `json:"trace"`
}

type canonicalDeploymentPlanIssue struct {
	Severity  string `json:"severity"`
	Code      string `json:"code"`
	Field     string `json:"field"`
	Message   string `json:"message"`
	SortOrder int    `json:"sortOrder"`
}

func CreateDeploymentPlan(
	ctx context.Context,
	request types.CreateDeploymentPlanRequest,
) (*types.DeploymentPlan, error) {
	if err := validateDeploymentPlanRequest(request); err != nil {
		return nil, err
	}
	var created *types.DeploymentPlan
	err := RunTx(ctx, func(ctx context.Context) error {
		plan, err := resolveDeploymentPlan(ctx, request)
		if err != nil {
			return err
		}
		if err := setDeploymentPlanCanonicalFields(plan); err != nil {
			return err
		}
		if err := insertDeploymentPlan(ctx, plan); err != nil {
			return err
		}
		if err := insertDeploymentPlanTargets(ctx, *plan); err != nil {
			return err
		}
		if err := insertDeploymentPlanSteps(ctx, *plan); err != nil {
			return err
		}
		if err := insertDeploymentPlanVariables(ctx, *plan); err != nil {
			return err
		}
		if err := insertDeploymentPlanIssues(ctx, *plan); err != nil {
			return err
		}
		created, err = getDeploymentPlan(ctx, plan.ID, plan.OrganizationID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

func GetDeploymentPlansByOrganizationID(ctx context.Context, orgID uuid.UUID) ([]types.DeploymentPlan, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+deploymentPlanOutputExpr+`
		FROM DeploymentPlan dp
		WHERE dp.organization_id = @organizationId
		ORDER BY dp.created_at DESC, dp.id`,
		pgx.NamedArgs{"organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query DeploymentPlan: %w", err)
	}
	plans, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.DeploymentPlan])
	if err != nil {
		return nil, fmt.Errorf("could not collect DeploymentPlan: %w", err)
	}
	for i := range plans {
		if err := hydrateDeploymentPlan(ctx, &plans[i]); err != nil {
			return nil, err
		}
	}
	return plans, nil
}

func GetDeploymentPlan(ctx context.Context, id, orgID uuid.UUID) (*types.DeploymentPlan, error) {
	return getDeploymentPlan(ctx, id, orgID)
}

func getDeploymentPlan(ctx context.Context, id, orgID uuid.UUID) (*types.DeploymentPlan, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+deploymentPlanOutputExpr+`
		FROM DeploymentPlan dp
		WHERE dp.id = @id AND dp.organization_id = @organizationId`,
		pgx.NamedArgs{"id": id, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query DeploymentPlan: %w", err)
	}
	plan, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.DeploymentPlan])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("could not collect DeploymentPlan: %w", err)
	}
	if err := hydrateDeploymentPlan(ctx, &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

func hydrateDeploymentPlan(ctx context.Context, plan *types.DeploymentPlan) error {
	var err error
	plan.Targets, err = getDeploymentPlanTargets(ctx, plan.ID, plan.OrganizationID)
	if err != nil {
		return err
	}
	plan.Steps, err = getDeploymentPlanSteps(ctx, plan.ID, plan.OrganizationID)
	if err != nil {
		return err
	}
	plan.Variables, err = getDeploymentPlanVariables(ctx, plan.ID, plan.OrganizationID)
	if err != nil {
		return err
	}
	plan.Issues, err = getDeploymentPlanIssues(ctx, plan.ID, plan.OrganizationID)
	return err
}

func validateDeploymentPlanRequest(request types.CreateDeploymentPlanRequest) error {
	if request.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	if request.ReleaseBundleID == uuid.Nil {
		return apierrors.NewBadRequest("releaseBundleId is required")
	}
	if request.EnvironmentID == uuid.Nil {
		return apierrors.NewBadRequest("environmentId is required")
	}
	if len(request.TargetIDs) == 0 {
		return apierrors.NewBadRequest("at least one targetId is required")
	}
	seen := map[uuid.UUID]struct{}{}
	for _, targetID := range request.TargetIDs {
		if targetID == uuid.Nil {
			return apierrors.NewBadRequest("targetIds must not contain empty IDs")
		}
		if _, ok := seen[targetID]; ok {
			return apierrors.NewBadRequest("targetIds must be unique")
		}
		seen[targetID] = struct{}{}
	}
	return nil
}

func resolveDeploymentPlan(
	ctx context.Context,
	request types.CreateDeploymentPlanRequest,
) (*types.DeploymentPlan, error) {
	bundle, err := GetReleaseBundle(ctx, request.ReleaseBundleID, request.OrganizationID)
	if err != nil {
		return nil, err
	}
	if _, err := GetEnvironment(ctx, request.EnvironmentID, request.OrganizationID); err != nil {
		return nil, err
	}
	targets, err := resolveDeploymentPlanTargets(ctx, request.OrganizationID, request.TargetIDs)
	if err != nil {
		return nil, err
	}
	plan := &types.DeploymentPlan{
		ID:                 uuid.New(),
		OrganizationID:     request.OrganizationID,
		ApplicationID:      bundle.ApplicationID,
		ReleaseBundleID:    bundle.ID,
		ChannelID:          bundle.ChannelID,
		EnvironmentID:      request.EnvironmentID,
		ProcessSnapshotID:  bundle.ProcessSnapshotID,
		VariableSnapshotID: bundle.VariableSnapshotID,
		Targets:            targets,
	}
	addDeploymentPlanEligibilityBlockers(ctx, plan)
	addDeploymentPlanSnapshotData(ctx, plan)
	addDeploymentPlanIssue(
		plan,
		types.DeploymentPlanIssueSeverityWarning,
		"dry_run_not_performed",
		"dryRun",
		"read-only agent dry-run checks are not available in the deployment plan foundation",
	)
	if deploymentPlanHasBlockers(plan.Issues) {
		plan.Status = types.DeploymentPlanStatusBlocked
	} else {
		plan.Status = types.DeploymentPlanStatusReady
	}
	return plan, nil
}

func resolveDeploymentPlanTargets(
	ctx context.Context,
	orgID uuid.UUID,
	targetIDs []uuid.UUID,
) ([]types.DeploymentPlanTarget, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT
			dt.id,
			dt.name,
			dt.type,
			dt.customer_organization_id
		FROM DeploymentTarget dt
		JOIN Organization o ON o.id = dt.organization_id AND o.deleted_at IS NULL
		WHERE dt.organization_id = @organizationId
			AND dt.id = any(@targetIds)
		ORDER BY dt.name, dt.id`,
		pgx.NamedArgs{"organizationId": orgID, "targetIds": targetIDs},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query DeploymentPlan targets: %w", err)
	}
	sources, err := pgx.CollectRows(rows, pgx.RowToStructByName[deploymentPlanTargetSource])
	if err != nil {
		return nil, fmt.Errorf("could not collect DeploymentPlan targets: %w", err)
	}
	if len(sources) != len(targetIDs) {
		return nil, apierrors.ErrNotFound
	}
	targets := make([]types.DeploymentPlanTarget, 0, len(sources))
	for i, target := range sources {
		targets = append(targets, types.DeploymentPlanTarget{
			OrganizationID:         orgID,
			DeploymentTargetID:     target.ID,
			Name:                   target.Name,
			Type:                   target.Type,
			CustomerOrganizationID: target.CustomerOrganizationID,
			SortOrder:              (i + 1) * 10,
		})
	}
	return targets, nil
}

func addDeploymentPlanEligibilityBlockers(ctx context.Context, plan *types.DeploymentPlan) {
	result, err := GetReleaseBundleEligibility(ctx, plan.ReleaseBundleID, plan.EnvironmentID, plan.OrganizationID)
	if err != nil {
		addDeploymentPlanIssue(
			plan,
			types.DeploymentPlanIssueSeverityBlocker,
			"eligibility_unavailable",
			"eligibility",
			"lifecycle eligibility could not be resolved",
		)
		return
	}
	for _, reason := range result.Reasons {
		addDeploymentPlanIssue(
			plan,
			types.DeploymentPlanIssueSeverityBlocker,
			string(reason.Code),
			reason.Field,
			reason.Message,
		)
	}
}

func addDeploymentPlanSnapshotData(ctx context.Context, plan *types.DeploymentPlan) {
	if plan.ProcessSnapshotID == nil {
		addDeploymentPlanIssue(
			plan,
			types.DeploymentPlanIssueSeverityBlocker,
			"missing_process_snapshot",
			"processSnapshotId",
			"release bundle has no immutable process snapshot",
		)
	} else if snapshot, err := GetProcessSnapshot(ctx, *plan.ProcessSnapshotID, plan.OrganizationID); err != nil {
		addDeploymentPlanIssue(
			plan,
			types.DeploymentPlanIssueSeverityBlocker,
			"process_snapshot_unavailable",
			"processSnapshotId",
			"process snapshot could not be resolved",
		)
	} else {
		addDeploymentPlanSteps(plan, *snapshot)
	}
	if plan.VariableSnapshotID == nil {
		addDeploymentPlanIssue(
			plan,
			types.DeploymentPlanIssueSeverityBlocker,
			"missing_variable_snapshot",
			"variableSnapshotId",
			"release bundle has no immutable variable snapshot",
		)
	} else if snapshot, err := GetVariableSnapshot(ctx, *plan.VariableSnapshotID, plan.OrganizationID); err != nil {
		addDeploymentPlanIssue(
			plan,
			types.DeploymentPlanIssueSeverityBlocker,
			"variable_snapshot_unavailable",
			"variableSnapshotId",
			"variable snapshot could not be resolved",
		)
	} else {
		addDeploymentPlanVariables(plan, *snapshot)
	}
}

func addDeploymentPlanSteps(plan *types.DeploymentPlan, snapshot types.ProcessSnapshot) {
	registry := actionregistry.DefaultRegistry()
	steps := slices.Clone(snapshot.Revision.Steps)
	sort.SliceStable(steps, func(i, j int) bool {
		if steps[i].SortOrder != steps[j].SortOrder {
			return steps[i].SortOrder < steps[j].SortOrder
		}
		return steps[i].Key < steps[j].Key
	})
	includedCount := 0
	for _, step := range steps {
		action, ok := registry.Get(step.ActionType)
		if !ok {
			addDeploymentPlanIssue(
				plan,
				types.DeploymentPlanIssueSeverityBlocker,
				"unknown_action_type",
				"steps."+step.Key+".actionType",
				fmt.Sprintf("action type %q is not registered", step.ActionType),
			)
		} else if err := registry.ValidateInput(step.ActionType, step.InputBindings); err != nil {
			addDeploymentPlanIssue(
				plan,
				types.DeploymentPlanIssueSeverityBlocker,
				"invalid_action_input",
				"steps."+step.Key+".inputBindings",
				err.Error(),
			)
		}
		excludedReason := deploymentPlanStepExcludedReason(step, plan.ChannelID, plan.EnvironmentID)
		included := excludedReason == ""
		if included {
			includedCount++
		}
		inputBindings := step.InputBindings
		if inputBindings == nil {
			inputBindings = map[string]any{}
		}
		plan.Steps = append(plan.Steps, types.DeploymentPlanStep{
			OrganizationID:       plan.OrganizationID,
			StepKey:              step.Key,
			Name:                 step.Name,
			ActionType:           step.ActionType,
			ActionName:           action.Name,
			ExecutionLocation:    step.ExecutionLocation,
			InputBindings:        inputBindings,
			Condition:            step.Condition,
			TargetTags:           nonNilStringSlice(step.TargetTags),
			FailureMode:          step.FailureMode,
			TimeoutSeconds:       step.TimeoutSeconds,
			RetryMaxAttempts:     step.RetryMaxAttempts,
			RetryIntervalSeconds: step.RetryIntervalSeconds,
			RequiredPermissions:  nonNilStringSlice(step.RequiredPermissions),
			SortOrder:            step.SortOrder,
			Dependencies:         nonNilStringSlice(step.Dependencies),
			Included:             included,
			ExcludedReason:       excludedReason,
		})
	}
	if len(steps) > 0 && includedCount == 0 {
		addDeploymentPlanIssue(
			plan,
			types.DeploymentPlanIssueSeverityBlocker,
			"no_applicable_steps",
			"steps",
			"process snapshot has no steps applicable to the selected channel and environment",
		)
	}
}

func deploymentPlanStepExcludedReason(
	step types.DeploymentProcessStep,
	channelID uuid.UUID,
	environmentID uuid.UUID,
) string {
	if len(step.ChannelIDs) > 0 && !slices.Contains(step.ChannelIDs, channelID) {
		return "channel_scope_mismatch"
	}
	if len(step.EnvironmentIDs) > 0 && !slices.Contains(step.EnvironmentIDs, environmentID) {
		return "environment_scope_mismatch"
	}
	return ""
}

func nonNilStringSlice(values []string) []string {
	if values == nil {
		return []string{}
	}
	return slices.Clone(values)
}

func addDeploymentPlanVariables(plan *types.DeploymentPlan, snapshot types.VariableSnapshot) {
	values := slices.Clone(snapshot.Values)
	sort.SliceStable(values, func(i, j int) bool {
		if values[i].Key != values[j].Key {
			return values[i].Key < values[j].Key
		}
		if values[i].VariableSetID != values[j].VariableSetID {
			return values[i].VariableSetID.String() < values[j].VariableSetID.String()
		}
		return values[i].VariableID.String() < values[j].VariableID.String()
	})
	for _, value := range values {
		plan.Variables = append(plan.Variables, types.DeploymentPlanVariable{
			OrganizationID: plan.OrganizationID,
			VariableSetID:  value.VariableSetID,
			VariableID:     value.VariableID,
			Key:            value.Key,
			Type:           value.Type,
			IsRequired:     value.IsRequired,
			Status:         value.Status,
			Source:         value.Source,
			Value:          cloneRawMessage(value.Value),
			ReferenceID:    value.ReferenceID,
			ReferenceName:  value.ReferenceName,
			Redacted:       value.Redacted,
			Trace:          slices.Clone(value.Trace),
		})
		if value.IsRequired && value.Status == types.VariableResolutionStatusUnresolved {
			addDeploymentPlanIssue(
				plan,
				types.DeploymentPlanIssueSeverityBlocker,
				"required_variable_unresolved",
				"variables."+value.Key,
				fmt.Sprintf("required variable %q is unresolved", value.Key),
			)
		}
	}
}

func cloneRawMessage(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return nil
	}
	clone := make([]byte, len(value))
	copy(clone, value)
	return clone
}

func addDeploymentPlanIssue(
	plan *types.DeploymentPlan,
	severity types.DeploymentPlanIssueSeverity,
	code string,
	field string,
	message string,
) {
	plan.Issues = append(plan.Issues, types.DeploymentPlanIssue{
		OrganizationID: plan.OrganizationID,
		Severity:       severity,
		Code:           code,
		Field:          field,
		Message:        message,
		SortOrder:      (len(plan.Issues) + 1) * 10,
	})
}

func deploymentPlanHasBlockers(issues []types.DeploymentPlanIssue) bool {
	return slices.ContainsFunc(issues, func(issue types.DeploymentPlanIssue) bool {
		return issue.Severity == types.DeploymentPlanIssueSeverityBlocker
	})
}

func setDeploymentPlanCanonicalFields(plan *types.DeploymentPlan) error {
	payload, err := canonicalizeDeploymentPlan(*plan)
	if err != nil {
		return fmt.Errorf("could not canonicalize DeploymentPlan: %w", err)
	}
	sum := sha256.Sum256(payload)
	plan.CanonicalPayload = payload
	plan.CanonicalChecksum = "sha256:" + hex.EncodeToString(sum[:])
	return nil
}

func canonicalizeDeploymentPlan(plan types.DeploymentPlan) ([]byte, error) {
	targets := make([]canonicalDeploymentPlanTarget, 0, len(plan.Targets))
	for _, target := range plan.Targets {
		customerOrganizationID := ""
		if target.CustomerOrganizationID != nil {
			customerOrganizationID = target.CustomerOrganizationID.String()
		}
		targets = append(targets, canonicalDeploymentPlanTarget{
			DeploymentTargetID:     target.DeploymentTargetID.String(),
			Name:                   target.Name,
			Type:                   string(target.Type),
			CustomerOrganizationID: customerOrganizationID,
			SortOrder:              target.SortOrder,
		})
	}
	steps := make([]canonicalDeploymentPlanStep, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		inputBindings := step.InputBindings
		if inputBindings == nil {
			inputBindings = map[string]any{}
		}
		steps = append(steps, canonicalDeploymentPlanStep{
			StepKey:              step.StepKey,
			Name:                 step.Name,
			ActionType:           step.ActionType,
			ActionName:           step.ActionName,
			ExecutionLocation:    step.ExecutionLocation,
			InputBindings:        inputBindings,
			Condition:            step.Condition,
			TargetTags:           slices.Clone(step.TargetTags),
			FailureMode:          step.FailureMode,
			TimeoutSeconds:       step.TimeoutSeconds,
			RetryMaxAttempts:     step.RetryMaxAttempts,
			RetryIntervalSeconds: step.RetryIntervalSeconds,
			RequiredPermissions:  slices.Clone(step.RequiredPermissions),
			SortOrder:            step.SortOrder,
			Dependencies:         slices.Clone(step.Dependencies),
			Included:             step.Included,
			ExcludedReason:       step.ExcludedReason,
		})
	}
	variables := make([]canonicalDeploymentPlanVariable, 0, len(plan.Variables))
	for _, variable := range plan.Variables {
		variables = append(variables, canonicalDeploymentPlanVariable{
			VariableSetID: variable.VariableSetID.String(),
			VariableID:    variable.VariableID.String(),
			Key:           variable.Key,
			Type:          string(variable.Type),
			IsRequired:    variable.IsRequired,
			Status:        string(variable.Status),
			Source:        string(variable.Source),
			Value:         cloneRawMessage(variable.Value),
			ReferenceID:   variable.ReferenceID,
			ReferenceName: variable.ReferenceName,
			Redacted:      variable.Redacted,
			Trace:         slices.Clone(variable.Trace),
		})
	}
	issues := make([]canonicalDeploymentPlanIssue, 0, len(plan.Issues))
	for _, issue := range plan.Issues {
		issues = append(issues, canonicalDeploymentPlanIssue{
			Severity:  string(issue.Severity),
			Code:      issue.Code,
			Field:     issue.Field,
			Message:   issue.Message,
			SortOrder: issue.SortOrder,
		})
	}
	processSnapshotID := ""
	if plan.ProcessSnapshotID != nil {
		processSnapshotID = plan.ProcessSnapshotID.String()
	}
	variableSnapshotID := ""
	if plan.VariableSnapshotID != nil {
		variableSnapshotID = plan.VariableSnapshotID.String()
	}
	canonical := canonicalDeploymentPlan{
		ReleaseBundleID:    plan.ReleaseBundleID.String(),
		ApplicationID:      plan.ApplicationID.String(),
		ChannelID:          plan.ChannelID.String(),
		EnvironmentID:      plan.EnvironmentID.String(),
		ProcessSnapshotID:  processSnapshotID,
		VariableSnapshotID: variableSnapshotID,
		Status:             string(plan.Status),
		Targets:            targets,
		Steps:              steps,
		Variables:          variables,
		Issues:             issues,
	}
	return json.Marshal(canonical)
}

func insertDeploymentPlan(ctx context.Context, plan *types.DeploymentPlan) error {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`INSERT INTO DeploymentPlan AS dp (
			id,
			organization_id,
			release_bundle_id,
			application_id,
			channel_id,
			environment_id,
			process_snapshot_id,
			variable_snapshot_id,
			status,
			canonical_checksum,
			canonical_payload
		) VALUES (
			@id,
			@organizationId,
			@releaseBundleId,
			@applicationId,
			@channelId,
			@environmentId,
			@processSnapshotId,
			@variableSnapshotId,
			@status,
			@canonicalChecksum,
			@canonicalPayload
		)
		RETURNING `+deploymentPlanOutputExpr,
		pgx.NamedArgs{
			"id":                 plan.ID,
			"organizationId":     plan.OrganizationID,
			"releaseBundleId":    plan.ReleaseBundleID,
			"applicationId":      plan.ApplicationID,
			"channelId":          plan.ChannelID,
			"environmentId":      plan.EnvironmentID,
			"processSnapshotId":  plan.ProcessSnapshotID,
			"variableSnapshotId": plan.VariableSnapshotID,
			"status":             plan.Status,
			"canonicalChecksum":  plan.CanonicalChecksum,
			"canonicalPayload":   plan.CanonicalPayload,
		},
	)
	if err != nil {
		return mapDeploymentPlanWriteError("insert", err)
	}
	inserted, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.DeploymentPlan])
	if err != nil {
		return mapDeploymentPlanWriteError("scan inserted", err)
	}
	plan.CreatedAt = inserted.CreatedAt
	return nil
}

func insertDeploymentPlanTargets(ctx context.Context, plan types.DeploymentPlan) error {
	if len(plan.Targets) == 0 {
		return nil
	}
	db := internalctx.GetDb(ctx)
	_, err := db.CopyFrom(
		ctx,
		pgx.Identifier{"deploymentplantarget"},
		[]string{
			"deployment_plan_id",
			"organization_id",
			"deployment_target_id",
			"name",
			"type",
			"customer_organization_id",
			"sort_order",
		},
		pgx.CopyFromSlice(len(plan.Targets), func(i int) ([]any, error) {
			target := plan.Targets[i]
			return []any{
				plan.ID,
				plan.OrganizationID,
				target.DeploymentTargetID,
				target.Name,
				target.Type,
				target.CustomerOrganizationID,
				target.SortOrder,
			}, nil
		}),
	)
	if err != nil {
		return mapDeploymentPlanWriteError("insert targets", err)
	}
	return nil
}

func insertDeploymentPlanSteps(ctx context.Context, plan types.DeploymentPlan) error {
	if len(plan.Steps) == 0 {
		return nil
	}
	db := internalctx.GetDb(ctx)
	_, err := db.CopyFrom(
		ctx,
		pgx.Identifier{"deploymentplanstep"},
		[]string{
			"deployment_plan_id",
			"organization_id",
			"step_key",
			"name",
			"action_type",
			"action_name",
			"execution_location",
			"input_bindings",
			"condition",
			"target_tags",
			"failure_mode",
			"timeout_seconds",
			"retry_max_attempts",
			"retry_interval_seconds",
			"required_permissions",
			"sort_order",
			"dependencies",
			"included",
			"excluded_reason",
		},
		pgx.CopyFromSlice(len(plan.Steps), func(i int) ([]any, error) {
			step := plan.Steps[i]
			inputBindings, err := json.Marshal(step.InputBindings)
			if err != nil {
				return nil, err
			}
			return []any{
				plan.ID,
				plan.OrganizationID,
				step.StepKey,
				step.Name,
				step.ActionType,
				step.ActionName,
				step.ExecutionLocation,
				inputBindings,
				step.Condition,
				step.TargetTags,
				step.FailureMode,
				step.TimeoutSeconds,
				step.RetryMaxAttempts,
				step.RetryIntervalSeconds,
				step.RequiredPermissions,
				step.SortOrder,
				step.Dependencies,
				step.Included,
				step.ExcludedReason,
			}, nil
		}),
	)
	if err != nil {
		return mapDeploymentPlanWriteError("insert steps", err)
	}
	return nil
}

func insertDeploymentPlanVariables(ctx context.Context, plan types.DeploymentPlan) error {
	if len(plan.Variables) == 0 {
		return nil
	}
	db := internalctx.GetDb(ctx)
	_, err := db.CopyFrom(
		ctx,
		pgx.Identifier{"deploymentplanvariable"},
		[]string{
			"deployment_plan_id",
			"organization_id",
			"variable_set_id",
			"variable_id",
			"key",
			"type",
			"is_required",
			"status",
			"source",
			"value",
			"reference_id",
			"reference_name",
			"redacted",
			"trace",
		},
		pgx.CopyFromSlice(len(plan.Variables), func(i int) ([]any, error) {
			variable := plan.Variables[i]
			trace, err := json.Marshal(variable.Trace)
			if err != nil {
				return nil, err
			}
			return []any{
				plan.ID,
				plan.OrganizationID,
				variable.VariableSetID,
				variable.VariableID,
				variable.Key,
				variable.Type,
				variable.IsRequired,
				variable.Status,
				variable.Source,
				deploymentPlanVariableJSONValue(variable),
				variable.ReferenceID,
				variable.ReferenceName,
				variable.Redacted,
				trace,
			}, nil
		}),
	)
	if err != nil {
		return mapDeploymentPlanWriteError("insert variables", err)
	}
	return nil
}

func deploymentPlanVariableJSONValue(variable types.DeploymentPlanVariable) any {
	if variable.Redacted || len(variable.Value) == 0 {
		return nil
	}
	return variable.Value
}

func insertDeploymentPlanIssues(ctx context.Context, plan types.DeploymentPlan) error {
	if len(plan.Issues) == 0 {
		return nil
	}
	db := internalctx.GetDb(ctx)
	_, err := db.CopyFrom(
		ctx,
		pgx.Identifier{"deploymentplanissue"},
		[]string{
			"deployment_plan_id",
			"organization_id",
			"severity",
			"code",
			"field",
			"message",
			"sort_order",
		},
		pgx.CopyFromSlice(len(plan.Issues), func(i int) ([]any, error) {
			issue := plan.Issues[i]
			return []any{
				plan.ID,
				plan.OrganizationID,
				issue.Severity,
				issue.Code,
				issue.Field,
				issue.Message,
				issue.SortOrder,
			}, nil
		}),
	)
	if err != nil {
		return mapDeploymentPlanWriteError("insert issues", err)
	}
	return nil
}

func getDeploymentPlanTargets(
	ctx context.Context,
	planID uuid.UUID,
	orgID uuid.UUID,
) ([]types.DeploymentPlanTarget, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT
			id,
			deployment_plan_id,
			organization_id,
			deployment_target_id,
			name,
			type,
			customer_organization_id,
			sort_order
		FROM DeploymentPlanTarget
		WHERE deployment_plan_id = @deploymentPlanId AND organization_id = @organizationId
		ORDER BY sort_order, deployment_target_id`,
		pgx.NamedArgs{"deploymentPlanId": planID, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query DeploymentPlanTarget: %w", err)
	}
	targets, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.DeploymentPlanTarget])
	if err != nil {
		return nil, fmt.Errorf("could not collect DeploymentPlanTarget: %w", err)
	}
	return targets, nil
}

func getDeploymentPlanSteps(
	ctx context.Context,
	planID uuid.UUID,
	orgID uuid.UUID,
) ([]types.DeploymentPlanStep, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT
			id,
			deployment_plan_id,
			organization_id,
			step_key,
			name,
			action_type,
			action_name,
			execution_location,
			input_bindings,
			condition,
			target_tags,
			failure_mode,
			timeout_seconds,
			retry_max_attempts,
			retry_interval_seconds,
			required_permissions,
			sort_order,
			dependencies,
			included,
			excluded_reason
		FROM DeploymentPlanStep
		WHERE deployment_plan_id = @deploymentPlanId AND organization_id = @organizationId
		ORDER BY sort_order, step_key`,
		pgx.NamedArgs{"deploymentPlanId": planID, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query DeploymentPlanStep: %w", err)
	}
	steps, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.DeploymentPlanStep])
	if err != nil {
		return nil, fmt.Errorf("could not collect DeploymentPlanStep: %w", err)
	}
	return steps, nil
}

func getDeploymentPlanVariables(
	ctx context.Context,
	planID uuid.UUID,
	orgID uuid.UUID,
) ([]types.DeploymentPlanVariable, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT
			id,
			deployment_plan_id,
			organization_id,
			variable_set_id,
			variable_id,
			key,
			type,
			is_required,
			status,
			source,
			value,
			reference_id,
			reference_name,
			redacted,
			trace
		FROM DeploymentPlanVariable
		WHERE deployment_plan_id = @deploymentPlanId AND organization_id = @organizationId
		ORDER BY key, variable_set_id, variable_id`,
		pgx.NamedArgs{"deploymentPlanId": planID, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query DeploymentPlanVariable: %w", err)
	}
	defer rows.Close()

	variables := []types.DeploymentPlanVariable{}
	for rows.Next() {
		var variable types.DeploymentPlanVariable
		var rawValue json.RawMessage
		var trace json.RawMessage
		if err := rows.Scan(
			&variable.ID,
			&variable.DeploymentPlanID,
			&variable.OrganizationID,
			&variable.VariableSetID,
			&variable.VariableID,
			&variable.Key,
			&variable.Type,
			&variable.IsRequired,
			&variable.Status,
			&variable.Source,
			&rawValue,
			&variable.ReferenceID,
			&variable.ReferenceName,
			&variable.Redacted,
			&trace,
		); err != nil {
			return nil, fmt.Errorf("could not scan DeploymentPlanVariable: %w", err)
		}
		if len(rawValue) > 0 && string(rawValue) != "null" {
			variable.Value = rawValue
		}
		if len(trace) > 0 {
			if err := json.Unmarshal(trace, &variable.Trace); err != nil {
				return nil, fmt.Errorf("could not decode DeploymentPlanVariable trace: %w", err)
			}
		}
		variables = append(variables, variable)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("could not iterate DeploymentPlanVariable: %w", err)
	}
	return variables, nil
}

func getDeploymentPlanIssues(
	ctx context.Context,
	planID uuid.UUID,
	orgID uuid.UUID,
) ([]types.DeploymentPlanIssue, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT
			id,
			deployment_plan_id,
			organization_id,
			severity,
			code,
			field,
			message,
			sort_order
		FROM DeploymentPlanIssue
		WHERE deployment_plan_id = @deploymentPlanId AND organization_id = @organizationId
		ORDER BY sort_order, severity, code`,
		pgx.NamedArgs{"deploymentPlanId": planID, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query DeploymentPlanIssue: %w", err)
	}
	issues, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.DeploymentPlanIssue])
	if err != nil {
		return nil, fmt.Errorf("could not collect DeploymentPlanIssue: %w", err)
	}
	return issues, nil
}

func mapDeploymentPlanWriteError(action string, err error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case pgerrcode.ForeignKeyViolation:
			return fmt.Errorf("could not %s DeploymentPlan: %w", action, apierrors.ErrNotFound)
		case pgerrcode.UniqueViolation:
			return fmt.Errorf("could not %s DeploymentPlan: %w", action, apierrors.ErrConflict)
		case pgerrcode.CheckViolation:
			return fmt.Errorf("could not %s DeploymentPlan: %w", action, apierrors.ErrBadRequest)
		}
	}
	return fmt.Errorf("could not %s DeploymentPlan: %w", action, err)
}
