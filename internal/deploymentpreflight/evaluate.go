package deploymentpreflight

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"

	"github.com/Masterminds/semver/v3"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

var (
	sha256ChecksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	backupIDPattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,255}$`)
)

type StateKey struct {
	DeploymentTargetID uuid.UUID
	ApplicationID      uuid.UUID
	Component          string
}

type Input struct {
	Plan                      types.DeploymentPlan
	PlanPayloadChecksumValid  bool
	ReleaseEligible           bool
	ReleaseEligibilityMessage string
	ReleaseContractValid      bool
	ReleaseContractMessage    string
	CurrentTargets            map[uuid.UUID]types.DeploymentTarget
	CurrentStates             map[StateKey]types.TargetComponentState
	Migrations                []types.MigrationPreflight
}

//nolint:gocyclo // The evaluator deliberately emits all independent preflight checks in one deterministic pass.
func Evaluate(input Input) []types.DeploymentPreflightCheck {
	checks := make([]types.DeploymentPreflightCheck, 0, 1+len(input.Plan.TargetComponents)*2)
	add := func(check types.DeploymentPreflightCheck) {
		check.SortOrder = (len(checks) + 1) * 10
		if check.Expected == nil {
			check.Expected = map[string]any{}
		}
		if check.Actual == nil {
			check.Actual = map[string]any{}
		}
		checks = append(checks, check)
	}
	targetsByPlanTargetID := make(map[uuid.UUID]types.DeploymentPlanTarget, len(input.Plan.Targets))
	for _, target := range input.Plan.Targets {
		targetsByPlanTargetID[target.ID] = target
		checksumStatus := types.DeploymentPreflightCheckStatusPassed
		checksumMessage := "plan canonical payload matches its checksum"
		if !input.PlanPayloadChecksumValid {
			checksumStatus = types.DeploymentPreflightCheckStatusFailed
			checksumMessage = "plan canonical payload no longer matches its checksum"
		}
		planTargetID := target.ID
		deploymentTargetID := target.DeploymentTargetID
		add(types.DeploymentPreflightCheck{
			DeploymentPlanTargetID: &planTargetID,
			DeploymentTargetID:     &deploymentTargetID,
			CheckKey:               "plan_checksum",
			Status:                 checksumStatus,
			Expected:               map[string]any{"checksum": input.Plan.CanonicalChecksum},
			Actual:                 map[string]any{"valid": input.PlanPayloadChecksumValid},
			Message:                checksumMessage,
		})

		liveTarget, targetFound := input.CurrentTargets[target.DeploymentTargetID]
		bindingMatches := targetFound && liveTarget.Type == target.Type &&
			uuidPointersEqual(liveTarget.CustomerOrganizationID, target.CustomerOrganizationID)
		bindingMessage := "target type and customer binding match the frozen plan"
		if !bindingMatches {
			bindingMessage = "target type or customer binding changed after the plan was created"
		}
		add(targetCheck(target, "target_binding", statusFor(bindingMatches),
			map[string]any{
				"type": target.Type, "customerOrganizationId": uuidPointerString(target.CustomerOrganizationID),
			},
			map[string]any{
				"found": targetFound, "type": liveTarget.Type,
				"customerOrganizationId": uuidPointerString(liveTarget.CustomerOrganizationID),
			}, bindingMessage))

		eligibilityMessage := input.ReleaseEligibilityMessage
		if eligibilityMessage == "" {
			if input.ReleaseEligible {
				eligibilityMessage = "release remains eligible for the selected environment and channel lifecycle"
			} else {
				eligibilityMessage = "release is not eligible for the selected environment and channel lifecycle"
			}
		}
		add(targetCheck(target, "release_eligibility", statusFor(input.ReleaseEligible),
			map[string]any{"eligible": true}, map[string]any{"eligible": input.ReleaseEligible}, eligibilityMessage))

		if input.Plan.ReleaseContract != nil {
			contractMessage := input.ReleaseContractMessage
			if contractMessage == "" {
				if input.ReleaseContractValid {
					contractMessage = "release contract matches the published immutable components and config"
				} else {
					contractMessage = "release contract does not match the published immutable components and config"
				}
			}
			add(targetCheck(target, "release_contract", statusFor(input.ReleaseContractValid),
				map[string]any{"schema": types.ReleaseContractSchemaV1, "valid": true},
				map[string]any{
					"schema": input.Plan.ReleaseContract.Schema, "valid": input.ReleaseContractValid,
				}, contractMessage))
			add(targetCheck(target, "release_operations", types.DeploymentPreflightCheckStatusPassed,
				map[string]any{"recorded": true},
				map[string]any{
					"migrationRequired":    input.Plan.ReleaseContract.Operations.MigrationRequired,
					"configChangeRequired": input.Plan.ReleaseContract.Operations.ConfigChangeRequired,
				}, "migration and configuration-change flags are frozen in the plan"))
		}
	}
	desiredByTargetAndComponent := make(map[StateKey]types.DeploymentPlanTargetComponent, len(input.Plan.TargetComponents))
	for _, component := range input.Plan.TargetComponents {
		target := targetsByPlanTargetID[component.DeploymentPlanTargetID]
		key := StateKey{
			DeploymentTargetID: component.DeploymentTargetID,
			ApplicationID:      input.Plan.ApplicationID,
			Component:          component.Component,
		}
		desiredByTargetAndComponent[key] = component
		liveTarget, targetFound := input.CurrentTargets[component.DeploymentTargetID]
		platformMatches := targetFound &&
			liveTarget.Platform == target.Platform &&
			liveTarget.Platform == component.Platform
		platformStatus := statusFor(platformMatches)
		platformMessage := fmt.Sprintf("target platform matches %s", component.Platform)
		if !platformMatches {
			platformMessage = fmt.Sprintf("target platform changed or does not support %s", component.Platform)
		}
		add(componentCheck(target, component, "target_platform:"+component.Component, platformStatus,
			map[string]any{"platform": component.Platform},
			map[string]any{"found": targetFound, "platform": liveTarget.Platform}, platformMessage))

		current, stateFound := input.CurrentStates[key]
		stateMatches := false
		if component.ExpectedStateVersion == 0 {
			stateMatches = !stateFound
		} else {
			stateMatches = stateFound &&
				current.StateVersion == component.ExpectedStateVersion &&
				current.StateChecksum == component.ExpectedStateChecksum
		}
		stateMessage := "target component state matches the frozen plan snapshot"
		if !stateMatches {
			stateMessage = "target component state changed after the plan was created"
		}
		add(componentCheck(target, component, "target_state:"+component.Component, statusFor(stateMatches),
			map[string]any{
				"stateVersion":  component.ExpectedStateVersion,
				"stateChecksum": component.ExpectedStateChecksum,
			},
			map[string]any{
				"found": stateFound, "stateVersion": current.StateVersion, "stateChecksum": current.StateChecksum,
			}, stateMessage))
	}

	if len(input.Plan.Migrations) > 0 || len(input.Migrations) > 0 {
		coveragePassed, expectedIDs, actualIDs := migrationEvidenceCoverage(
			input.Plan.Migrations,
			input.Migrations,
		)
		add(types.DeploymentPreflightCheck{
			CheckKey: "migration_evidence_coverage",
			Status:   statusFor(coveragePassed),
			Expected: map[string]any{"migrationIds": expectedIDs},
			Actual:   map[string]any{"migrationIds": actualIDs},
			Message:  "migration preflight evidence exactly covers the frozen migration contracts",
		})
	}
	for _, migration := range input.Migrations {
		contract := migration.Contract
		backupPassed := !contract.BackupRequired ||
			(migration.Backup != nil && migration.Backup.Verified &&
				backupIDPattern.MatchString(migration.Backup.ID) &&
				sha256ChecksumPattern.MatchString(migration.Backup.Checksum))
		backupActual := map[string]any{"required": contract.BackupRequired, "verified": false}
		if migration.Backup != nil {
			backupActual["backupId"] = migration.Backup.ID
			backupActual["checksum"] = migration.Backup.Checksum
			backupActual["verified"] = migration.Backup.Verified
		}
		add(migrationCheck(contract, "migration_backup:"+contract.ID, statusFor(backupPassed),
			map[string]any{"required": contract.BackupRequired, "verified": contract.BackupRequired},
			backupActual, "required backup identity and verification evidence are available"))

		schemaPassed :=
			migration.CurrentSchema.DatabaseResourceKey == contract.DatabaseResourceKey &&
				migration.CurrentSchema.Version == contract.ExpectedSourceVersion &&
				sha256ChecksumPattern.MatchString(migration.CurrentSchema.Checksum) &&
				migration.CurrentSchema.Checksum == contract.ExpectedSourceChecksum
		add(migrationCheck(contract, "migration_schema:"+contract.ID, statusFor(schemaPassed),
			map[string]any{
				"databaseResourceKey": contract.DatabaseResourceKey,
				"version":             contract.ExpectedSourceVersion,
				"checksum":            contract.ExpectedSourceChecksum,
			},
			map[string]any{
				"databaseResourceKey": migration.CurrentSchema.DatabaseResourceKey,
				"version":             migration.CurrentSchema.Version,
				"checksum":            migration.CurrentSchema.Checksum,
			}, "current schema identity, version, and checksum match the migration contract"))

		add(migrationCheck(contract, "migration_target_lock:"+contract.ID,
			statusFor(migration.TargetLockAvailable),
			map[string]any{"available": true}, map[string]any{"available": migration.TargetLockAvailable},
			"target mutation lock is available"))
		add(migrationCheck(contract, "migration_database_lock:"+contract.ID,
			statusFor(migration.DatabaseLockAvailable),
			map[string]any{"resourceKey": contract.DatabaseResourceKey, "available": true},
			map[string]any{"resourceKey": contract.DatabaseResourceKey, "available": migration.DatabaseLockAvailable},
			"database resource lock is available"))
		add(migrationCheck(contract, "migration_adapter:"+contract.ID,
			statusFor(migration.AdapterAvailable),
			map[string]any{"adapterType": contract.AdapterType, "available": true},
			map[string]any{"adapterType": contract.AdapterType, "available": migration.AdapterAvailable},
			"required database adapter is available"))
		add(migrationCheck(contract, "migration_probes:"+contract.ID,
			statusFor(migration.PreconditionProbesPassed),
			map[string]any{"passed": true, "count": len(contract.PreconditionProbes)},
			map[string]any{"passed": migration.PreconditionProbesPassed},
			"migration precondition probes match their frozen expectations"))
	}

	if input.Plan.ReleaseContract == nil {
		return checks
	}
	for _, target := range input.Plan.Targets {
		for _, requirement := range input.Plan.ReleaseContract.Compatibility.Requires {
			key := StateKey{
				DeploymentTargetID: target.DeploymentTargetID,
				ApplicationID:      input.Plan.ApplicationID,
				Component:          requirement.Component,
			}
			version := ""
			contracts := []string(nil)
			source := "observed"
			found := false
			if desired, ok := desiredByTargetAndComponent[key]; ok {
				version, contracts, source, found = desired.Version, desired.Contracts, "candidate", true
			} else if current, ok := input.CurrentStates[key]; ok {
				version, contracts, found = current.Version, current.Contracts, true
			}
			if requirement.MinimumVersion != "" {
				matches := found && versionAtLeast(version, requirement.MinimumVersion)
				message := fmt.Sprintf("%s version satisfies >= %s", requirement.Component, requirement.MinimumVersion)
				if !matches {
					message = fmt.Sprintf("%s version does not satisfy >= %s", requirement.Component, requirement.MinimumVersion)
				}
				add(requirementCheck(target, requirement.Component, "dependency_version:"+requirement.Component,
					statusFor(matches), map[string]any{"minimumVersion": requirement.MinimumVersion},
					map[string]any{"found": found, "version": version, "source": source}, message))
			}
			if requirement.Contract != "" {
				matches := found && slices.Contains(contracts, requirement.Contract)
				message := fmt.Sprintf("%s provides contract %s", requirement.Component, requirement.Contract)
				if !matches {
					message = fmt.Sprintf("%s does not provide contract %s", requirement.Component, requirement.Contract)
				}
				add(requirementCheck(target, requirement.Component, "dependency_contract:"+requirement.Component,
					statusFor(matches), map[string]any{"contract": requirement.Contract},
					map[string]any{"found": found, "contracts": contracts, "source": source}, message))
			}
		}
	}
	return checks
}

func migrationEvidenceCoverage(
	planned []types.DeploymentPlanMigration,
	supplied []types.MigrationPreflight,
) (bool, []string, []string) {
	expected := make(map[string][]types.MigrationContract, len(planned))
	expectedIDs := make([]string, 0, len(planned))
	for _, migration := range planned {
		contract := migration.MigrationContract()
		expected[contract.ID] = append(expected[contract.ID], contract)
		expectedIDs = append(expectedIDs, contract.ID)
	}
	actual := make(map[string][]types.MigrationContract, len(supplied))
	actualIDs := make([]string, 0, len(supplied))
	for _, migration := range supplied {
		actual[migration.Contract.ID] = append(actual[migration.Contract.ID], migration.Contract)
		actualIDs = append(actualIDs, migration.Contract.ID)
	}
	slices.Sort(expectedIDs)
	slices.Sort(actualIDs)
	if len(expectedIDs) != len(actualIDs) {
		return false, expectedIDs, actualIDs
	}
	for id, contracts := range expected {
		if len(contracts) != 1 ||
			len(actual[id]) != 1 ||
			!reflect.DeepEqual(contracts[0], actual[id][0]) {
			return false, expectedIDs, actualIDs
		}
	}
	return true, expectedIDs, actualIDs
}

func migrationCheck(
	contract types.MigrationContract,
	key string,
	status types.DeploymentPreflightCheckStatus,
	expected, actual map[string]any,
	message string,
) types.DeploymentPreflightCheck {
	return types.DeploymentPreflightCheck{
		Component: contract.ComponentKey,
		CheckKey:  key,
		Status:    status,
		Expected:  expected,
		Actual:    actual,
		Message:   message,
	}
}

func componentCheck(
	target types.DeploymentPlanTarget,
	component types.DeploymentPlanTargetComponent,
	key string,
	status types.DeploymentPreflightCheckStatus,
	expected, actual map[string]any,
	message string,
) types.DeploymentPreflightCheck {
	planTargetID := target.ID
	deploymentTargetID := target.DeploymentTargetID
	return types.DeploymentPreflightCheck{
		DeploymentPlanTargetID: &planTargetID,
		DeploymentTargetID:     &deploymentTargetID,
		Component:              component.Component,
		CheckKey:               key,
		Status:                 status,
		Expected:               expected,
		Actual:                 actual,
		Message:                message,
	}
}

func requirementCheck(
	target types.DeploymentPlanTarget,
	component, key string,
	status types.DeploymentPreflightCheckStatus,
	expected, actual map[string]any,
	message string,
) types.DeploymentPreflightCheck {
	planTargetID := target.ID
	deploymentTargetID := target.DeploymentTargetID
	return types.DeploymentPreflightCheck{
		DeploymentPlanTargetID: &planTargetID,
		DeploymentTargetID:     &deploymentTargetID,
		Component:              component,
		CheckKey:               key,
		Status:                 status,
		Expected:               expected,
		Actual:                 actual,
		Message:                message,
	}
}

func targetCheck(
	target types.DeploymentPlanTarget,
	key string,
	status types.DeploymentPreflightCheckStatus,
	expected, actual map[string]any,
	message string,
) types.DeploymentPreflightCheck {
	planTargetID := target.ID
	deploymentTargetID := target.DeploymentTargetID
	return types.DeploymentPreflightCheck{
		DeploymentPlanTargetID: &planTargetID,
		DeploymentTargetID:     &deploymentTargetID,
		CheckKey:               key,
		Status:                 status,
		Expected:               expected,
		Actual:                 actual,
		Message:                message,
	}
}

func uuidPointersEqual(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func uuidPointerString(value *uuid.UUID) string {
	if value == nil {
		return ""
	}
	return value.String()
}

func statusFor(passed bool) types.DeploymentPreflightCheckStatus {
	if passed {
		return types.DeploymentPreflightCheckStatusPassed
	}
	return types.DeploymentPreflightCheckStatusFailed
}

func versionAtLeast(actual, minimum string) bool {
	actualVersion, err := semver.StrictNewVersion(actual)
	if err != nil {
		return false
	}
	minimumVersion, err := semver.StrictNewVersion(minimum)
	return err == nil && !actualVersion.LessThan(minimumVersion)
}
