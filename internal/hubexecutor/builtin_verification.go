package hubexecutor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var hubBuiltinChecksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type builtinVerificationRequest struct {
	OrganizationID uuid.UUID
	TaskID         uuid.UUID
	StepRunID      uuid.UUID
	StepKey        string
	ActionName     string
}

type builtinVerificationAuthority struct {
	PlanID       uuid.UUID
	TargetConfig *targetConfigVerificationAuthority
	Requirement  *requirementVerificationAuthority
}

type targetConfigVerificationAuthority struct {
	SnapshotID        uuid.UUID
	CanonicalChecksum string
	Objects           []types.TargetConfigSnapshotObjectDraft
	VerificationFacts []types.ConfigVerificationFact
}

type requirementVerificationAuthority struct {
	Resolution   types.RequirementResolution
	Observation  *types.TargetComponentObservation
	CurrentState *types.TargetComponentState
}

type targetConfigBuiltinInput struct {
	SnapshotID uuid.UUID
	Checksum   string
}

type requirementBuiltinInput struct {
	Mode            types.RequirementResolutionMode
	BindingChecksum string
	ObservationID   *uuid.UUID
}

func (w *Worker) verifyBuiltinStep(
	ctx context.Context,
	lease types.TaskLease,
	step types.TaskLeaseStep,
) ([]types.RecordStepRunOutputRequest, error) {
	if step.ActionVersion != types.AgentActionVersionV1 {
		return nil, fmt.Errorf("unsupported Hub built-in actionVersion %q", step.ActionVersion)
	}
	if w == nil || w.store == nil {
		return nil, errors.New("Hub built-in authority repository is not configured")
	}
	var (
		configInput      *targetConfigBuiltinInput
		requirementInput *requirementBuiltinInput
		err              error
	)
	switch step.ActionName {
	case "target-config.verify":
		configInput, err = decodeTargetConfigBuiltinInput(step.InputBindings)
	case "requirement.verify":
		requirementInput, err = decodeRequirementBuiltinInput(step.InputBindings)
	default:
		return nil, fmt.Errorf("unsupported Hub built-in actionName %q", step.ActionName)
	}
	if err != nil {
		return nil, err
	}
	authority, err := w.store.LoadBuiltinVerificationAuthority(ctx, builtinVerificationRequest{
		OrganizationID: lease.OrganizationID,
		TaskID:         lease.TaskID,
		StepRunID:      step.StepRunID,
		StepKey:        step.StepKey,
		ActionName:     step.ActionName,
	})
	if err != nil {
		return nil, fmt.Errorf("load authoritative Hub built-in facts: %w", err)
	}
	if authority == nil || authority.PlanID == uuid.Nil {
		return nil, errors.New("authoritative Hub built-in facts are missing")
	}
	if configInput != nil {
		if err := verifyAuthoritativeTargetConfig(*configInput, authority.TargetConfig); err != nil {
			return nil, err
		}
	}
	if requirementInput != nil {
		if err := verifyAuthoritativeRequirement(*requirementInput, authority.Requirement); err != nil {
			return nil, err
		}
	}
	return []types.RecordStepRunOutputRequest{
		{Name: "verifiedAction", Value: step.ActionName},
		{Name: "authoritativePlanId", Value: authority.PlanID.String()},
	}, nil
}

func decodeTargetConfigBuiltinInput(input map[string]any) (*targetConfigBuiltinInput, error) {
	snapshotValue, ok := input["snapshotId"].(string)
	if !ok {
		return nil, errors.New("target-config.verify snapshotId is required")
	}
	snapshotID, err := uuid.Parse(strings.TrimSpace(snapshotValue))
	if err != nil {
		return nil, errors.New("target-config.verify snapshotId is invalid")
	}
	checksum, err := builtinChecksum(input, "checksum")
	if err != nil {
		return nil, fmt.Errorf("target-config.verify: %w", err)
	}
	return &targetConfigBuiltinInput{SnapshotID: snapshotID, Checksum: checksum}, nil
}

func decodeRequirementBuiltinInput(input map[string]any) (*requirementBuiltinInput, error) {
	modeValue, ok := input["mode"].(string)
	mode := types.RequirementResolutionMode(strings.TrimSpace(modeValue))
	if !ok || !mode.IsValid() {
		return nil, errors.New("requirement.verify mode is invalid")
	}
	checksum, err := builtinChecksum(input, "bindingChecksum")
	if err != nil {
		return nil, fmt.Errorf("requirement.verify: %w", err)
	}
	var observationID *uuid.UUID
	if value, exists := input["observationId"]; exists && value != nil {
		text, ok := value.(string)
		if !ok {
			return nil, errors.New("requirement.verify observationId is invalid")
		}
		parsed, err := uuid.Parse(strings.TrimSpace(text))
		if err != nil {
			return nil, errors.New("requirement.verify observationId is invalid")
		}
		observationID = &parsed
	}
	return &requirementBuiltinInput{
		Mode: mode, BindingChecksum: checksum, ObservationID: observationID,
	}, nil
}

func builtinChecksum(input map[string]any, key string) (string, error) {
	value, ok := input[key].(string)
	value = strings.TrimSpace(value)
	if !ok || !hubBuiltinChecksumPattern.MatchString(value) {
		return "", fmt.Errorf("%s must be an immutable sha256 checksum", key)
	}
	return value, nil
}

func verifyAuthoritativeTargetConfig(
	input targetConfigBuiltinInput,
	authority *targetConfigVerificationAuthority,
) error {
	if authority == nil ||
		input.SnapshotID != authority.SnapshotID ||
		input.Checksum != authority.CanonicalChecksum {
		return errors.New("authoritative target config does not match frozen step input")
	}
	if len(authority.Objects) == 0 ||
		len(authority.Objects) != len(authority.VerificationFacts) {
		return errors.New("authoritative target config verification receipt is incomplete")
	}
	objects := make(map[string]types.TargetConfigSnapshotObjectDraft, len(authority.Objects))
	for _, object := range authority.Objects {
		if _, duplicate := objects[object.Key]; duplicate || strings.TrimSpace(object.Key) == "" {
			return errors.New("authoritative target config contains ambiguous objects")
		}
		objects[object.Key] = object
	}
	seen := make(map[string]struct{}, len(authority.VerificationFacts))
	for _, fact := range authority.VerificationFacts {
		object, exists := objects[fact.ObjectKey]
		if !exists {
			return fmt.Errorf("authoritative target config object %q is missing", fact.ObjectKey)
		}
		if _, duplicate := seen[fact.ObjectKey]; duplicate {
			return fmt.Errorf("authoritative target config object %q has duplicate receipts", fact.ObjectKey)
		}
		seen[fact.ObjectKey] = struct{}{}
		if !fact.Verified || fact.VerificationCode != "verified" ||
			fact.Reference != object.Reference ||
			fact.VersionID != object.VersionID ||
			fact.MediaType != object.MediaType ||
			fact.SizeBytes != object.SizeBytes ||
			fact.Checksum != object.Checksum ||
			fact.ObservedReference != object.Reference ||
			fact.ObservedVersionID != object.VersionID ||
			fact.ObservedMediaType != object.MediaType ||
			fact.ObservedSizeBytes != object.SizeBytes ||
			fact.ObservedChecksum != object.Checksum {
			return fmt.Errorf("authoritative target config object %q verification receipt mismatched", fact.ObjectKey)
		}
	}
	return nil
}

func verifyAuthoritativeRequirement(
	input requirementBuiltinInput,
	authority *requirementVerificationAuthority,
) error {
	if authority == nil {
		return errors.New("authoritative requirement resolution is missing")
	}
	resolution := authority.Resolution
	if input.Mode != resolution.Mode || input.BindingChecksum != resolution.BindingChecksum ||
		!sameUUIDPointer(input.ObservationID, resolution.ObservationID) {
		return errors.New("authoritative requirement resolution does not match frozen step input")
	}
	if resolution.ObservationID == nil {
		if authority.Observation != nil || authority.CurrentState != nil {
			return errors.New("authoritative requirement has unexpected observation state")
		}
		return nil
	}
	if authority.Observation == nil || authority.CurrentState == nil {
		return errors.New("authoritative requirement observation state is missing")
	}
	observation := authority.Observation
	current := authority.CurrentState
	if observation.ID != *resolution.ObservationID ||
		observation.StateVersion != resolution.ExpectedStateVersion ||
		observation.StateChecksum != resolution.ExpectedStateChecksum ||
		observation.Health != types.TargetComponentHealthHealthy ||
		current.StateVersion != resolution.ExpectedStateVersion ||
		current.StateChecksum != resolution.ExpectedStateChecksum ||
		current.Health != types.TargetComponentHealthHealthy {
		return errors.New("authoritative current provider state no longer matches the frozen observation")
	}
	if resolution.ProviderReleaseID != nil &&
		(observation.ReleaseBundleID != *resolution.ProviderReleaseID ||
			current.ReleaseBundleID != *resolution.ProviderReleaseID) {
		return errors.New("authoritative provider release no longer matches the frozen requirement")
	}
	if resolution.ComponentInstanceID != nil &&
		!sameUUIDPointer(observation.ComponentInstanceID, resolution.ComponentInstanceID) {
		return errors.New("authoritative provider component no longer matches the frozen requirement")
	}
	if resolution.ProviderPlatform != "" &&
		(string(observation.Platform) != resolution.ProviderPlatform ||
			string(current.Platform) != resolution.ProviderPlatform) {
		return errors.New("authoritative provider platform no longer matches the frozen requirement")
	}
	return nil
}

func sameUUIDPointer(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func (s databaseStore) LoadBuiltinVerificationAuthority(
	ctx context.Context,
	request builtinVerificationRequest,
) (*builtinVerificationAuthority, error) {
	if request.OrganizationID == uuid.Nil || request.TaskID == uuid.Nil ||
		request.StepRunID == uuid.Nil || strings.TrimSpace(request.StepKey) == "" ||
		strings.TrimSpace(request.ActionName) == "" {
		return nil, errors.New("Hub built-in verification identity is invalid")
	}
	var planID uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT task.deployment_plan_id
		FROM Task task
		JOIN StepRun run
		  ON run.task_id = task.id
		 AND run.organization_id = task.organization_id
		JOIN DeploymentPlanStep step
		  ON step.id = run.deployment_plan_step_id
		 AND step.deployment_plan_id = task.deployment_plan_id
		 AND step.organization_id = task.organization_id
		WHERE task.id = @taskID
		  AND task.organization_id = @organizationID
		  AND run.id = @stepRunID
		  AND run.step_key = @stepKey
		  AND step.action_type = 'builtin'
		  AND step.action_name = @actionName
		  AND lower(trim(step.execution_location)) = 'hub'`,
		pgx.NamedArgs{
			"taskID": request.TaskID, "organizationID": request.OrganizationID,
			"stepRunID": request.StepRunID, "stepKey": strings.TrimSpace(request.StepKey),
			"actionName": strings.TrimSpace(request.ActionName),
		},
	).Scan(&planID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.NewConflict("Hub built-in step is not bound to the task's frozen plan")
	}
	if err != nil {
		return nil, fmt.Errorf("load Hub built-in plan binding: %w", err)
	}
	plan, err := db.GetDeploymentPlan(s.context(ctx), planID, request.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("load Hub built-in deployment plan: %w", err)
	}
	var canonical types.TargetDeploymentPlanCanonical
	if err := json.Unmarshal(plan.CanonicalPayload, &canonical); err != nil {
		return nil, fmt.Errorf("decode Hub built-in canonical plan: %w", err)
	}
	if plan.PlanSchema != types.TargetDeploymentPlanSchemaV2 ||
		canonical.Schema != types.TargetDeploymentPlanSchemaV2 ||
		canonical.ProtocolVersion != types.DeploymentPlanProtocolV2 {
		return nil, errors.New("Hub built-in requires a frozen protocol-v2 target plan")
	}
	authority := &builtinVerificationAuthority{PlanID: plan.ID}
	switch request.ActionName {
	case "target-config.verify":
		config, err := loadTargetConfigVerificationAuthority(
			s.context(ctx),
			*plan,
			canonical,
			request.StepKey,
		)
		if err != nil {
			return nil, err
		}
		authority.TargetConfig = config
	case "requirement.verify":
		requirement, err := s.loadRequirementVerificationAuthority(
			ctx,
			*plan,
			canonical,
			request.StepKey,
		)
		if err != nil {
			return nil, err
		}
		authority.Requirement = requirement
	default:
		return nil, fmt.Errorf("unsupported Hub built-in actionName %q", request.ActionName)
	}
	return authority, nil
}

func loadTargetConfigVerificationAuthority(
	ctx context.Context,
	plan types.DeploymentPlan,
	canonical types.TargetDeploymentPlanCanonical,
	stepKey string,
) (*targetConfigVerificationAuthority, error) {
	if stepKey != "config:verify" || plan.TargetConfigSnapshotID == nil ||
		*plan.TargetConfigSnapshotID != canonical.TargetConfigSnapshotID {
		return nil, errors.New("frozen plan target config identity is inconsistent")
	}
	snapshot, err := db.GetTargetConfigSnapshot(
		ctx,
		plan.OrganizationID,
		canonical.TargetConfigSnapshotID,
	)
	if err != nil {
		return nil, fmt.Errorf("load authoritative target config snapshot: %w", err)
	}
	if snapshot.CanonicalChecksum != canonical.TargetConfigSnapshotChecksum {
		return nil, errors.New("authoritative target config snapshot checksum drifted")
	}
	objects := make([]types.TargetConfigSnapshotObjectDraft, 0, len(snapshot.Objects))
	for _, object := range snapshot.Objects {
		objects = append(objects, types.TargetConfigSnapshotObjectDraft{
			Key: object.Key, Kind: object.Kind, Reference: object.Reference,
			VersionID: object.VersionID, MediaType: object.MediaType,
			SizeBytes: object.SizeBytes, Checksum: object.Checksum,
		})
	}
	return &targetConfigVerificationAuthority{
		SnapshotID:        canonical.TargetConfigSnapshotID,
		CanonicalChecksum: canonical.TargetConfigSnapshotChecksum,
		Objects:           objects,
		VerificationFacts: canonical.ConfigVerificationFacts,
	}, nil
}

func (s databaseStore) loadRequirementVerificationAuthority(
	ctx context.Context,
	plan types.DeploymentPlan,
	canonical types.TargetDeploymentPlanCanonical,
	stepKey string,
) (*requirementVerificationAuthority, error) {
	stepInput, err := frozenRequirementStepInput(canonical.Graph.Steps, stepKey)
	if err != nil {
		return nil, err
	}
	var persisted *types.RequirementResolution
	for index := range plan.ResolvedRequirements {
		candidate := &plan.ResolvedRequirements[index]
		if candidate.BindingChecksum != stepInput.BindingChecksum {
			continue
		}
		if persisted != nil {
			return nil, errors.New("frozen requirement binding checksum is ambiguous")
		}
		persisted = candidate
	}
	if persisted == nil {
		return nil, errors.New("frozen requirement resolution is missing")
	}
	var canonicalResolution *types.RequirementResolution
	for index := range canonical.RequirementResolutions {
		candidate := &canonical.RequirementResolutions[index]
		if candidate.RequirementKey == persisted.RequirementKey {
			canonicalResolution = candidate
			break
		}
	}
	if canonicalResolution == nil || !sameFrozenRequirement(*persisted, *canonicalResolution) ||
		persisted.Mode != stepInput.Mode ||
		!sameUUIDPointer(persisted.ObservationID, stepInput.ObservationID) {
		return nil, errors.New("persisted requirement resolution does not match the canonical plan")
	}
	authority := &requirementVerificationAuthority{Resolution: *persisted}
	if persisted.ObservationID == nil {
		return authority, nil
	}
	observation, current, err := s.loadRequirementObservation(
		ctx,
		plan.OrganizationID,
		*persisted.ObservationID,
	)
	if err != nil {
		return nil, err
	}
	authority.Observation = observation
	authority.CurrentState = current
	return authority, nil
}

func frozenRequirementStepInput(
	steps []types.TargetPlanStep,
	stepKey string,
) (*requirementBuiltinInput, error) {
	for _, step := range steps {
		if step.StepKey != stepKey {
			continue
		}
		if step.ActionType != "builtin" || step.ActionName != "requirement.verify" ||
			step.ExecutionLocation != "hub" {
			return nil, errors.New("canonical requirement verification step is invalid")
		}
		var input map[string]any
		if err := json.Unmarshal(step.InputBindings, &input); err != nil {
			return nil, fmt.Errorf("decode canonical requirement verification input: %w", err)
		}
		return decodeRequirementBuiltinInput(input)
	}
	return nil, errors.New("canonical requirement verification step is missing")
}

func sameFrozenRequirement(left, right types.RequirementResolution) bool {
	return left.RequirementKey == right.RequirementKey &&
		left.ConsumerKey == right.ConsumerKey &&
		left.Capability == right.Capability &&
		left.VersionRange == right.VersionRange &&
		left.Mode == right.Mode &&
		sameUUIDPointer(left.ProviderReleaseID, right.ProviderReleaseID) &&
		sameUUIDPointer(left.ObservationID, right.ObservationID) &&
		left.ProviderVersion == right.ProviderVersion &&
		left.ProviderPlatform == right.ProviderPlatform &&
		left.ProviderReleaseChecksum == right.ProviderReleaseChecksum &&
		left.ProvenanceBindingChecksum == right.ProvenanceBindingChecksum &&
		sameUUIDPointer(left.ProviderDeploymentUnitID, right.ProviderDeploymentUnitID) &&
		sameUUIDPointer(left.ComponentInstanceID, right.ComponentInstanceID) &&
		left.SubscriberSetChecksum == right.SubscriberSetChecksum &&
		left.ExpectedStateVersion == right.ExpectedStateVersion &&
		left.ExpectedStateChecksum == right.ExpectedStateChecksum &&
		left.BindingChecksum == right.BindingChecksum &&
		left.SortOrder == right.SortOrder
}

func (s databaseStore) loadRequirementObservation(
	ctx context.Context,
	organizationID, observationID uuid.UUID,
) (*types.TargetComponentObservation, *types.TargetComponentState, error) {
	var (
		observation types.TargetComponentObservation
		current     types.TargetComponentState
	)
	err := s.pool.QueryRow(ctx, `
		SELECT
			observation.id,
			observation.component_instance_id,
			observation.state_version,
			observation.state_checksum,
			observation.release_bundle_id,
			observation.platform,
			observation.health,
			current.state_version,
			current.state_checksum,
			current.release_bundle_id,
			current.platform,
			current.health
		FROM TargetComponentObservation observation
		JOIN TargetComponentState current
		  ON current.id = observation.target_component_state_id
		 AND current.organization_id = observation.organization_id
		WHERE observation.id = @observationID
		  AND observation.organization_id = @organizationID`,
		pgx.NamedArgs{
			"observationID":  observationID,
			"organizationID": organizationID,
		},
	).Scan(
		&observation.ID,
		&observation.ComponentInstanceID,
		&observation.StateVersion,
		&observation.StateChecksum,
		&observation.ReleaseBundleID,
		&observation.Platform,
		&observation.Health,
		&current.StateVersion,
		&current.StateChecksum,
		&current.ReleaseBundleID,
		&current.Platform,
		&current.Health,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, apierrors.NewConflict(
			"frozen requirement observation is no longer authoritative",
		)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("load authoritative requirement observation: %w", err)
	}
	return &observation, &current, nil
}
