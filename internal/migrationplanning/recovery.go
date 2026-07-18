package migrationplanning

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

func BuildRecoveryPlan(
	failed types.FailedPlan,
	request types.RecoveryRequest,
) (*types.PlanDraft, error) {
	if failed.PlanID == uuid.Nil {
		return nil, fmt.Errorf("failed plan ID is required")
	}
	if strings.TrimSpace(request.Reason) == "" {
		return nil, fmt.Errorf("recovery reason is required")
	}
	recoveryPlanID := uuid.New()
	var graph types.TargetPlanGraph
	var err error
	switch request.Mode {
	case types.RecoveryModeReverse:
		graph, err = buildReverseGraph(failed)
	case types.RecoveryModeForwardFix, types.RecoveryModeManual:
		graph, err = finalizeGraph(types.TargetPlanGraph{})
	case types.RecoveryModeRestore:
		graph, err = buildRestoreGraph(request, recoveryPlanID)
	default:
		return nil, fmt.Errorf("unsupported recovery mode %q", request.Mode)
	}
	if err != nil {
		return nil, err
	}
	recovery := types.RecoveryPlan{
		Mode: request.Mode, SourcePlanID: failed.PlanID, Graph: graph,
		EvidenceRetentionRequired: true, Reason: strings.TrimSpace(request.Reason),
	}
	payload, err := json.Marshal(recovery)
	if err != nil {
		return nil, fmt.Errorf("marshal recovery plan: %w", err)
	}
	draft := failed.Draft
	draft.ID = recoveryPlanID
	draft.Revision = 1
	draft.ProtocolVersion = types.DeploymentPlanProtocolV2
	sourcePlanID := failed.PlanID
	draft.SupersedesDeploymentPlanID = &sourcePlanID
	draft.SupersedeReason = strings.TrimSpace(request.Reason)
	draft.ExpectedPreviewChecksum = ""
	draft.PreviewPayload = payload
	draft.PreviewChecksum = checksumBytes(payload)
	draft.PublishedDeploymentPlanID = nil
	draft.PublishedDeploymentPlanStatus = ""
	return &draft, nil
}

func buildReverseGraph(failed types.FailedPlan) (types.TargetPlanGraph, error) {
	completed := make(map[string]struct{}, len(failed.CompletedMigrationIDs))
	for _, id := range failed.CompletedMigrationIDs {
		if _, duplicate := completed[id]; duplicate {
			return types.TargetPlanGraph{}, fmt.Errorf(
				"completed migration %q does not resolve uniquely",
				id,
			)
		}
		completed[id] = struct{}{}
	}
	contracts := slices.Clone(failed.Contracts)
	slices.SortFunc(contracts, func(a, b types.MigrationContract) int {
		return strings.Compare(a.ID, b.ID)
	})
	steps := make([]types.TargetPlanStep, 0, len(contracts))
	edges := make([]types.DeploymentPlanStepEdge, 0)
	byID := make(map[string][]types.MigrationContract, len(contracts))
	for _, contract := range contracts {
		byID[contract.ID] = append(byID[contract.ID], contract)
	}
	for id := range completed {
		if len(byID[id]) != 1 {
			return types.TargetPlanGraph{}, fmt.Errorf(
				"completed migration %q does not resolve uniquely",
				id,
			)
		}
	}
	for id := range completed {
		contract := byID[id][0]
		for _, dependency := range contract.DependsOn {
			_, dependencyCompleted := completed[dependency]
			if !dependencyCompleted || len(byID[dependency]) != 1 {
				return types.TargetPlanGraph{}, fmt.Errorf(
					"completed dependency %q for migration %q does not resolve uniquely",
					dependency,
					contract.ID,
				)
			}
		}
	}
	for _, contract := range contracts {
		if _, ok := completed[contract.ID]; !ok {
			continue
		}
		if contract.Reversibility == types.MigrationReversibilityForwardOnly ||
			contract.RequiresForwardFix {
			return types.TargetPlanGraph{}, fmt.Errorf(
				"migration %q is forward-only and requires a forward-fix plan",
				contract.ID,
			)
		}
		if contract.Reversibility != types.MigrationReversibilityReversible {
			return types.TargetPlanGraph{}, fmt.Errorf(
				"migration %q requires manual recovery",
				contract.ID,
			)
		}
		key := "recovery:" + contract.ID + ":reverse"
		steps = append(steps, migrationStep(
			contract, key, "Reverse "+contract.ID, "migration_recovery",
			"database.migration.reverse",
			"target:"+contract.ComponentKey,
			"database:"+contract.DatabaseResourceKey,
			map[string]any{
				"migrationId":           contract.ID,
				"migrationChecksum":     contract.Checksum,
				"databaseResourceKey":   contract.DatabaseResourceKey,
				"databaseLockKey":       "database:" + contract.DatabaseResourceKey,
				"expectedSourceVersion": contract.ResultingVersion,
				"resultingVersion":      contract.ExpectedSourceVersion,
				"procedureReference":    contract.RecoveryProcedureReference,
				"idempotencyKey":        "reverse:" + contract.IdempotencyKey,
				"timeoutSeconds":        contract.LockTimeoutSeconds,
			},
			types.MigrationRetrySafe,
			"cooperative",
			"reverse completion, resulting schema, and retained recovery evidence",
		))
	}
	for _, contract := range contracts {
		if _, ok := completed[contract.ID]; !ok {
			continue
		}
		for _, dependency := range contract.DependsOn {
			edges = append(edges, newEdge(
				"recovery:"+contract.ID+":reverse",
				"recovery:"+dependency+":reverse",
			))
		}
	}
	return finalizeGraph(types.TargetPlanGraph{Steps: steps, Edges: edges})
}

func buildRestoreGraph(
	request types.RecoveryRequest,
	recoveryPlanID uuid.UUID,
) (types.TargetPlanGraph, error) {
	if recoveryPlanID == uuid.Nil {
		return types.TargetPlanGraph{}, fmt.Errorf("database restore requires a recovery plan ID")
	}
	if strings.TrimSpace(request.SeparateApprovalID) == "" {
		return types.TargetPlanGraph{}, fmt.Errorf("database restore requires a separate approval")
	}
	if strings.TrimSpace(request.BackupID) == "" || !checksumPattern.MatchString(request.BackupChecksum) {
		return types.TargetPlanGraph{}, fmt.Errorf("database restore requires a frozen backup ID and checksum")
	}
	if !resourceKeyPattern.MatchString(request.DatabaseResourceKey) {
		return types.TargetPlanGraph{}, fmt.Errorf("database restore requires a database resource key")
	}
	if strings.TrimSpace(request.ExpectedDataLossBoundary) == "" ||
		strings.TrimSpace(request.ProcedureVersion) == "" ||
		strings.TrimSpace(request.OperatorScope) == "" ||
		len(request.RequiredApproverGroups) == 0 {
		return types.TargetPlanGraph{}, fmt.Errorf(
			"database restore requires frozen data-loss, procedure, approver, and operator inputs",
		)
	}
	if len(request.ValidationProbes) == 0 {
		return types.TargetPlanGraph{}, fmt.Errorf("database restore requires validation probes")
	}
	var probeIssues []types.ValidationIssue
	validateProbes(
		request.ValidationProbes,
		"validationProbes",
		func(code, field, message string) {
			probeIssues = append(probeIssues, types.ValidationIssue{
				Code: code, Field: field, Message: message,
			})
		},
	)
	if len(probeIssues) > 0 {
		return types.TargetPlanGraph{}, fmt.Errorf(
			"database restore validation probe is invalid: %s",
			probeIssues[0].Message,
		)
	}
	executeKey := "recovery:restore:execute"
	verifyKey := "recovery:restore:verify"
	executeInput := map[string]any{
		"recoveryPlanId":           recoveryPlanID.String(),
		"separateApprovalId":       request.SeparateApprovalID,
		"backupId":                 request.BackupID,
		"backupChecksum":           request.BackupChecksum,
		"databaseResourceKey":      request.DatabaseResourceKey,
		"databaseLockKey":          "database:" + request.DatabaseResourceKey,
		"expectedDataLossBoundary": request.ExpectedDataLossBoundary,
		"procedureVersion":         request.ProcedureVersion,
		"requiredApproverGroups":   request.RequiredApproverGroups,
		"operatorScope":            request.OperatorScope,
		"validationProbes":         request.ValidationProbes,
		"idempotencyKey":           "restore:" + request.BackupID,
		"timeoutSeconds":           86400,
	}
	verifyInput := map[string]any{
		"backupId":            request.BackupID,
		"backupChecksum":      request.BackupChecksum,
		"databaseResourceKey": request.DatabaseResourceKey,
		"databaseLockKey":     "database:" + request.DatabaseResourceKey,
		"validationProbes":    request.ValidationProbes,
		"timeoutSeconds":      3600,
	}
	steps := []types.TargetPlanStep{
		recoveryStep(executeKey, "Execute separately approved database restore",
			"database.restore.execute", request.DatabaseResourceKey, executeInput,
			"restore terminal callback and operator evidence"),
		recoveryStep(verifyKey, "Verify restored database",
			"database.restore.verify", request.DatabaseResourceKey, verifyInput,
			"isolated restore verification and schema observation"),
	}
	return finalizeGraph(types.TargetPlanGraph{
		Steps: steps, Edges: []types.DeploymentPlanStepEdge{newEdge(executeKey, verifyKey)},
	})
}

func recoveryStep(
	key, name, actionType, databaseResourceKey string,
	input map[string]any,
	observation string,
) types.TargetPlanStep {
	inputJSON, _ := json.Marshal(input)
	return types.TargetPlanStep{
		StepKey: key, Name: name, Kind: "database_recovery",
		ActionType: actionType, ActionName: actionType, ExecutionLocation: "manual_operator",
		InputBindings:   inputJSON,
		TargetLockKey:   "target:recovery:" + databaseResourceKey,
		DatabaseLockKey: "database:" + databaseResourceKey,
		TimeoutSeconds:  86400, RetryClass: string(types.MigrationRetryNone),
		CancellationBehavior:   "manual_terminal",
		ExpectedInputChecksum:  checksumBytes(inputJSON),
		ObservationRequirement: observation, V1Compatible: false,
	}
}
