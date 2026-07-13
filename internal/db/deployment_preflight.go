package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/deploymentpreflight"
	"github.com/distr-sh/distr/internal/releasebundles"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const deploymentPreflightRunOutputExpr = `
	dpr.id,
	dpr.created_at,
	dpr.organization_id,
	dpr.deployment_plan_id,
	dpr.plan_checksum,
	dpr.actor_user_account_id,
	dpr.status
`

const deploymentPreflightCheckOutputExpr = `
	dpc.id,
	dpc.created_at,
	dpc.organization_id,
	dpc.deployment_preflight_run_id,
	dpc.deployment_plan_id,
	dpc.deployment_plan_target_id,
	dpc.deployment_target_id,
	dpc.task_id,
	dpc.component,
	dpc.check_key,
	dpc.status,
	dpc.expected,
	dpc.actual,
	dpc.message,
	dpc.sort_order
`

type deploymentPreflightTargetSource struct {
	ID                     uuid.UUID                      `db:"id"`
	Type                   types.DeploymentType           `db:"type"`
	Platform               types.DeploymentTargetPlatform `db:"platform"`
	CustomerOrganizationID *uuid.UUID                     `db:"customer_organization_id"`
}

func evaluateAndPersistDeploymentPreflight(
	ctx context.Context,
	plan types.DeploymentPlan,
	actorUserAccountID uuid.UUID,
) (*types.DeploymentPreflightRun, bool, error) {
	return evaluateAndPersistDeploymentPreflightScope(ctx, plan, plan, actorUserAccountID)
}

func evaluateAndPersistDeploymentPreflightForTask(
	ctx context.Context,
	plan types.DeploymentPlan,
	task types.Task,
) (*types.DeploymentPreflightRun, bool, error) {
	scoped := plan
	scoped.Targets = nil
	for _, target := range plan.Targets {
		if target.ID == task.DeploymentPlanTargetID && target.DeploymentTargetID == task.DeploymentTargetID {
			scoped.Targets = []types.DeploymentPlanTarget{target}
			break
		}
	}
	if len(scoped.Targets) != 1 {
		return nil, false, fmt.Errorf("deployment task target is not present in its frozen plan")
	}
	scoped.TargetComponents = nil
	for _, component := range plan.TargetComponents {
		if component.DeploymentPlanTargetID == task.DeploymentPlanTargetID {
			scoped.TargetComponents = append(scoped.TargetComponents, component)
		}
	}
	return evaluateAndPersistDeploymentPreflightScope(ctx, plan, scoped, uuid.Nil)
}

func evaluateAndPersistDeploymentPreflightScope(
	ctx context.Context,
	canonicalPlan types.DeploymentPlan,
	evaluationPlan types.DeploymentPlan,
	actorUserAccountID uuid.UUID,
) (*types.DeploymentPreflightRun, bool, error) {
	currentTargets, err := getDeploymentPreflightTargets(ctx, evaluationPlan)
	if err != nil {
		return nil, false, err
	}
	currentStates, err := getDeploymentPreflightStates(ctx, evaluationPlan)
	if err != nil {
		return nil, false, err
	}
	canonicalStateValid, err := deploymentPlanCanonicalStateValid(canonicalPlan)
	if err != nil {
		return nil, false, err
	}
	releaseEligible, eligibilityMessage, contractValid, contractMessage, err := getDeploymentPreflightReleaseFacts(ctx, canonicalPlan) //nolint:lll
	if err != nil {
		return nil, false, err
	}
	checks := deploymentpreflight.Evaluate(deploymentpreflight.Input{
		Plan:                      evaluationPlan,
		PlanPayloadChecksumValid:  canonicalStateValid,
		ReleaseEligible:           releaseEligible,
		ReleaseEligibilityMessage: eligibilityMessage,
		ReleaseContractValid:      contractValid,
		ReleaseContractMessage:    contractMessage,
		CurrentTargets:            currentTargets,
		CurrentStates:             currentStates,
	})
	status := types.DeploymentPreflightStatusPassed
	for _, check := range checks {
		if check.Status == types.DeploymentPreflightCheckStatusFailed {
			status = types.DeploymentPreflightStatusFailed
			break
		}
	}
	run := &types.DeploymentPreflightRun{
		ID: uuid.New(), OrganizationID: canonicalPlan.OrganizationID, DeploymentPlanID: canonicalPlan.ID,
		PlanChecksum: canonicalPlan.CanonicalChecksum, Status: status, Checks: checks,
	}
	if actorUserAccountID != uuid.Nil {
		run.ActorUserAccountID = &actorUserAccountID
	}
	if err := insertDeploymentPreflightRun(ctx, run); err != nil {
		return nil, false, err
	}
	if err := insertDeploymentPreflightChecks(ctx, *run); err != nil {
		return nil, false, err
	}
	created, err := getDeploymentPreflightRun(ctx, run.ID, canonicalPlan.OrganizationID)
	if err != nil {
		return nil, false, err
	}
	return created, status == types.DeploymentPreflightStatusPassed, nil
}

func getDeploymentPreflightTargets(
	ctx context.Context,
	plan types.DeploymentPlan,
) (map[uuid.UUID]types.DeploymentTarget, error) {
	targetIDs := make([]uuid.UUID, 0, len(plan.Targets))
	for _, target := range plan.Targets {
		targetIDs = append(targetIDs, target.DeploymentTargetID)
	}
	database := internalctx.GetDb(ctx)
	rows, err := database.Query(ctx,
		`SELECT dt.id, dt.type, dt.platform, dt.customer_organization_id
		FROM DeploymentTarget dt
		WHERE dt.organization_id = @organizationId AND dt.id = ANY(@targetIds)
		FOR UPDATE OF dt`,
		pgx.NamedArgs{"organizationId": plan.OrganizationID, "targetIds": targetIDs},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query preflight targets: %w", err)
	}
	sources, err := pgx.CollectRows(rows, pgx.RowToStructByName[deploymentPreflightTargetSource])
	if err != nil {
		return nil, fmt.Errorf("could not collect preflight targets: %w", err)
	}
	result := make(map[uuid.UUID]types.DeploymentTarget, len(sources))
	for _, source := range sources {
		result[source.ID] = types.DeploymentTarget{
			ID:                     source.ID,
			Type:                   source.Type,
			Platform:               source.Platform,
			CustomerOrganizationID: source.CustomerOrganizationID,
		}
	}
	return result, nil
}

func getDeploymentPreflightStates(
	ctx context.Context,
	plan types.DeploymentPlan,
) (map[deploymentpreflight.StateKey]types.TargetComponentState, error) {
	targetIDs := make([]uuid.UUID, 0, len(plan.Targets))
	for _, target := range plan.Targets {
		targetIDs = append(targetIDs, target.DeploymentTargetID)
	}
	database := internalctx.GetDb(ctx)
	rows, err := database.Query(ctx,
		`SELECT `+targetComponentStateOutputExpr+`
		FROM TargetComponentState tcs
		WHERE tcs.organization_id = @organizationId
			AND tcs.application_id = @applicationId
			AND tcs.deployment_target_id = ANY(@targetIds)
		FOR UPDATE OF tcs`,
		pgx.NamedArgs{
			"organizationId": plan.OrganizationID,
			"applicationId":  plan.ApplicationID,
			"targetIds":      targetIDs,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query preflight component states: %w", err)
	}
	states, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.TargetComponentState])
	if err != nil {
		return nil, fmt.Errorf("could not collect preflight component states: %w", err)
	}
	result := make(map[deploymentpreflight.StateKey]types.TargetComponentState, len(states))
	for _, state := range states {
		result[deploymentpreflight.StateKey{
			DeploymentTargetID: state.DeploymentTargetID,
			ApplicationID:      state.ApplicationID,
			Component:          state.Component,
		}] = state
	}
	return result, nil
}

func deploymentPlanCanonicalStateValid(plan types.DeploymentPlan) (bool, error) {
	sum := sha256.Sum256(plan.CanonicalPayload)
	if plan.CanonicalChecksum != "sha256:"+hex.EncodeToString(sum[:]) {
		return false, nil
	}
	var stored map[string]any
	if err := json.Unmarshal(plan.CanonicalPayload, &stored); err != nil {
		return false, nil
	}
	if _, legacyPayload := stored["status"]; legacyPayload {
		delete(stored, "status")
		currentPayload, err := canonicalizeDeploymentPlan(plan)
		if err != nil {
			return false, fmt.Errorf("could not canonicalize legacy deployment plan during preflight: %w", err)
		}
		var current map[string]any
		if err := json.Unmarshal(currentPayload, &current); err != nil {
			return false, fmt.Errorf("could not decode current deployment plan during preflight: %w", err)
		}
		projected, ok := projectJSONToStoredShape(current, stored)
		return ok && reflect.DeepEqual(stored, projected), nil
	}
	currentPayload, err := canonicalizeDeploymentPlan(plan)
	if err != nil {
		return false, fmt.Errorf("could not canonicalize deployment plan during preflight: %w", err)
	}
	return bytes.Equal(plan.CanonicalPayload, currentPayload), nil
}

func projectJSONToStoredShape(current, stored any) (any, bool) {
	switch storedValue := stored.(type) {
	case map[string]any:
		currentValue, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		projected := make(map[string]any, len(storedValue))
		for key, storedChild := range storedValue {
			currentChild, found := currentValue[key]
			if !found {
				return nil, false
			}
			projectedChild, valid := projectJSONToStoredShape(currentChild, storedChild)
			if !valid {
				return nil, false
			}
			projected[key] = projectedChild
		}
		return projected, true
	case []any:
		currentValue, ok := current.([]any)
		if !ok || len(currentValue) != len(storedValue) {
			return nil, false
		}
		projected := make([]any, len(storedValue))
		for i := range storedValue {
			projectedChild, valid := projectJSONToStoredShape(currentValue[i], storedValue[i])
			if !valid {
				return nil, false
			}
			projected[i] = projectedChild
		}
		return projected, true
	default:
		return current, true
	}
}

func getDeploymentPreflightReleaseFacts(
	ctx context.Context,
	plan types.DeploymentPlan,
) (bool, string, bool, string, error) {
	bundle, err := getReleaseBundle(ctx, plan.ReleaseBundleID, plan.OrganizationID, true)
	if err != nil {
		return false, "", false, "", err
	}
	eligibility, eligibilityErr := GetReleaseBundleEligibility(
		ctx, plan.ReleaseBundleID, plan.EnvironmentID, plan.OrganizationID,
	)
	releaseEligible := eligibilityErr == nil && eligibility.Eligible
	eligibilityMessage := "release remains eligible for the selected environment and channel lifecycle"
	if eligibilityErr != nil {
		eligibilityMessage = "release eligibility could not be resolved at execution time"
	} else if !eligibility.Eligible {
		messages := make([]string, 0, len(eligibility.Reasons))
		for _, reason := range eligibility.Reasons {
			messages = append(messages, reason.Message)
		}
		eligibilityMessage = "release is not eligible for execution"
		if len(messages) > 0 {
			eligibilityMessage += ": " + strings.Join(messages, "; ")
		}
	}

	contractValid := true
	contractMessage := ""
	if plan.ReleaseContract != nil {
		validation := releasebundles.ValidateReleaseContract(*plan.ReleaseContract, bundle.Components)
		planContract := releasebundles.NormalizedReleaseContract(plan.ReleaseContract)
		bundleContract := releasebundles.NormalizedReleaseContract(bundle.ReleaseContract)
		contractValid = validation.Valid && bundleContract != nil && reflect.DeepEqual(planContract, bundleContract)
		contractMessage = "release contract matches the published immutable components and config"
		if !contractValid {
			contractMessage = "release contract no longer matches the published immutable components and config"
		}
	}
	return releaseEligible, eligibilityMessage, contractValid, contractMessage, nil
}

func insertDeploymentPreflightRun(ctx context.Context, run *types.DeploymentPreflightRun) error {
	database := internalctx.GetDb(ctx)
	rows, err := database.Query(ctx,
		`INSERT INTO DeploymentPreflightRun AS dpr (
			id, organization_id, deployment_plan_id, plan_checksum, actor_user_account_id, status
		) VALUES (
			@id, @organizationId, @deploymentPlanId, @planChecksum, @actorUserAccountId, @status
		)
		RETURNING `+deploymentPreflightRunOutputExpr,
		pgx.NamedArgs{
			"id": run.ID, "organizationId": run.OrganizationID, "deploymentPlanId": run.DeploymentPlanID,
			"planChecksum": run.PlanChecksum, "actorUserAccountId": run.ActorUserAccountID, "status": run.Status,
		},
	)
	if err != nil {
		return fmt.Errorf("could not insert DeploymentPreflightRun: %w", err)
	}
	created, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.DeploymentPreflightRun])
	if err != nil {
		return fmt.Errorf("could not collect DeploymentPreflightRun: %w", err)
	}
	created.Checks = run.Checks
	*run = created
	return nil
}

func insertDeploymentPreflightChecks(ctx context.Context, run types.DeploymentPreflightRun) error {
	if len(run.Checks) == 0 {
		return nil
	}
	database := internalctx.GetDb(ctx)
	_, err := database.CopyFrom(
		ctx,
		pgx.Identifier{"deploymentpreflightcheck"},
		[]string{
			"id", "organization_id", "deployment_preflight_run_id", "deployment_plan_id",
			"deployment_plan_target_id", "deployment_target_id", "task_id", "component", "check_key",
			"status", "expected", "actual", "message", "sort_order",
		},
		pgx.CopyFromSlice(len(run.Checks), func(i int) ([]any, error) {
			check := run.Checks[i]
			return []any{
				uuid.New(), run.OrganizationID, run.ID, run.DeploymentPlanID,
				check.DeploymentPlanTargetID, check.DeploymentTargetID, check.TaskID, check.Component,
				check.CheckKey, check.Status, check.Expected, check.Actual, check.Message, check.SortOrder,
			}, nil
		}),
	)
	if err != nil {
		return fmt.Errorf("could not insert DeploymentPreflightCheck: %w", err)
	}
	return nil
}

func attachDeploymentPreflightTasks(
	ctx context.Context,
	runID, orgID uuid.UUID,
) error {
	database := internalctx.GetDb(ctx)
	_, err := database.Exec(ctx,
		`UPDATE DeploymentPreflightCheck AS dpc
		SET task_id = t.id
		FROM Task t
		WHERE dpc.deployment_preflight_run_id = @runId
			AND dpc.organization_id = @organizationId
			AND t.organization_id = dpc.organization_id
			AND t.deployment_plan_id = dpc.deployment_plan_id
			AND t.deployment_plan_target_id = dpc.deployment_plan_target_id`,
		pgx.NamedArgs{"runId": runID, "organizationId": orgID},
	)
	if err != nil {
		return fmt.Errorf("could not attach preflight checks to tasks: %w", err)
	}
	return nil
}

func getDeploymentPreflightRuns(
	ctx context.Context,
	planID, orgID uuid.UUID,
) ([]types.DeploymentPreflightRun, error) {
	database := internalctx.GetDb(ctx)
	rows, err := database.Query(ctx,
		`SELECT `+deploymentPreflightRunOutputExpr+`
		FROM DeploymentPreflightRun dpr
		WHERE dpr.deployment_plan_id = @deploymentPlanId AND dpr.organization_id = @organizationId
		ORDER BY dpr.created_at DESC, dpr.id DESC`,
		pgx.NamedArgs{"deploymentPlanId": planID, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query DeploymentPreflightRun: %w", err)
	}
	runs, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.DeploymentPreflightRun])
	if err != nil {
		return nil, fmt.Errorf("could not collect DeploymentPreflightRun: %w", err)
	}
	for i := range runs {
		runs[i].Checks, err = getDeploymentPreflightChecks(ctx, runs[i].ID, orgID)
		if err != nil {
			return nil, err
		}
	}
	return runs, nil
}

func getDeploymentPreflightRun(
	ctx context.Context,
	runID, orgID uuid.UUID,
) (*types.DeploymentPreflightRun, error) {
	database := internalctx.GetDb(ctx)
	rows, err := database.Query(ctx,
		`SELECT `+deploymentPreflightRunOutputExpr+`
		FROM DeploymentPreflightRun dpr
		WHERE dpr.id = @id AND dpr.organization_id = @organizationId`,
		pgx.NamedArgs{"id": runID, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query DeploymentPreflightRun: %w", err)
	}
	run, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.DeploymentPreflightRun])
	if err != nil {
		return nil, fmt.Errorf("could not collect DeploymentPreflightRun: %w", err)
	}
	run.Checks, err = getDeploymentPreflightChecks(ctx, run.ID, orgID)
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func getDeploymentPreflightChecks(
	ctx context.Context,
	runID, orgID uuid.UUID,
) ([]types.DeploymentPreflightCheck, error) {
	database := internalctx.GetDb(ctx)
	rows, err := database.Query(ctx,
		`SELECT `+deploymentPreflightCheckOutputExpr+`
		FROM DeploymentPreflightCheck dpc
		WHERE dpc.deployment_preflight_run_id = @runId AND dpc.organization_id = @organizationId
		ORDER BY dpc.sort_order, dpc.id`,
		pgx.NamedArgs{"runId": runID, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query DeploymentPreflightCheck: %w", err)
	}
	checks, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.DeploymentPreflightCheck])
	if err != nil {
		return nil, fmt.Errorf("could not collect DeploymentPreflightCheck: %w", err)
	}
	return checks, nil
}

func deploymentPreflightFailureMessage(run types.DeploymentPreflightRun) string {
	messages := make([]string, 0, 3)
	for _, check := range run.Checks {
		if check.Status == types.DeploymentPreflightCheckStatusFailed {
			messages = append(messages, check.Message)
			if len(messages) == 3 {
				break
			}
		}
	}
	if len(messages) == 0 {
		return "deployment preflight failed"
	}
	return "deployment preflight failed: " + strings.Join(messages, "; ")
}
