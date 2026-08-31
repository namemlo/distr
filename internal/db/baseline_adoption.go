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
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const baselineAdoptionMaximumComponents = 256

var (
	baselineAdoptionChecksumPattern    = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	baselineAdoptionIdempotencyPattern = regexp.MustCompile(
		`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`,
	)
)

const baselineAdoptionOutputExpr = `
	a.id, a.created_at, a.organization_id, a.deployment_plan_id,
	a.product_release_id, a.target_config_snapshot_id,
	a.deployment_unit_id, a.environment_id, a.deployment_target_id,
	a.actor_user_account_id, a.authorization_action, a.idempotency_key,
	a.reason, a.plan_checksum, a.product_release_checksum,
	a.target_config_checksum, a.request_checksum, a.outcome_checksum,
	a.status, a.deployment_performed, a.task_count, a.lock_count,
	a.execution_count
`

const baselineAdoptionComponentOutputExpr = `
	c.id, c.created_at, c.organization_id, c.baseline_adoption_id,
	c.deployment_plan_id, c.deployment_unit_id, c.component_instance_id,
	c.component_key, c.component_release_id, c.component_release_checksum,
	c.source_commit, c.build_id, c.provenance_verification_id,
	c.provenance_evidence_digest, c.provenance_policy_checksum,
	c.artifact_digest, c.platform, c.target_config_snapshot_id,
	c.config_checksum, c.schema_version, c.capability_checksum,
	c.topology_checksum, c.observation_id, c.observer_id,
	c.observation_evidence_checksum, c.observation_evidence_reference,
	c.observation_state_checksum, c.observation_runtime_state_checksum,
	c.health_evidence_kind, c.health_evidence_use, c.health_policy_checksum,
	c.observation_captured_at,
	c.observation_fresh_until, c.active_desired_revision_id,
	c.desired_revision
`

type baselineAdoptionPlan struct {
	ID                            uuid.UUID
	OrganizationID                uuid.UUID
	ApplicationID                 uuid.UUID
	ProductReleaseID              uuid.UUID
	EnvironmentID                 uuid.UUID
	DeploymentUnitID              uuid.UUID
	TargetConfigSnapshotID        uuid.UUID
	DeploymentTargetID            uuid.UUID
	Status                        types.DeploymentPlanStatus
	PlanSchema                    string
	ProtocolVersion               string
	PlanChecksum                  string
	CanonicalPayload              []byte
	SealedAt                      *time.Time
	ProductReleaseChecksum        string
	ProductReleaseStatus          types.ReleaseBundleStatus
	ProductReleaseKind            types.ReleaseBundleKind
	TargetConfigChecksum          string
	TargetConfigPlatform          string
	TargetEnvironmentAssignmentID uuid.UUID
	DeploymentScopeID             uuid.UUID
}

type baselineAdoptionExpectedComponent struct {
	Input   types.BaselineAdoptionComponentInput
	Pin     types.ComponentReleasePin
	Binding types.ConfigComponentBinding
}

func AdoptDeploymentPlanBaseline(
	ctx context.Context,
	input types.CreateBaselineAdoptionInput,
) (*types.BaselineAdoption, error) {
	requestPayload, requestChecksum, err := canonicalizeBaselineAdoptionInput(input)
	if err != nil {
		return nil, err
	}
	if err := validateBaselineAdoptionInput(input); err != nil {
		return nil, err
	}

	var result *types.BaselineAdoption
	err = RunTxIso(ctx, pgx.Serializable, func(txCtx context.Context) error {
		existing, err := getBaselineAdoptionByIdempotencyKey(
			txCtx, input.OrganizationID, input.IdempotencyKey,
		)
		if err != nil && !errors.Is(err, apierrors.ErrNotFound) {
			return err
		}
		if existing != nil {
			if !baselineAdoptionReplayMatches(*existing, input, requestChecksum) {
				return apierrors.NewConflict(
					"idempotency key is already bound to different baseline adoption material",
				)
			}
			result = existing
			return nil
		}

		plan, canonical, err := lockBaselineAdoptionPlan(txCtx, input)
		if err != nil {
			return err
		}
		expected, err := baselineAdoptionExpectedComponents(canonical, input)
		if err != nil {
			return err
		}
		if err := ensureBaselineAdoptionHasNoExecution(txCtx, *plan); err != nil {
			return err
		}

		adoptionID := uuid.New()
		outcomeChecksum, err := baselineAdoptionOutcomeChecksum(
			adoptionID, requestChecksum, len(expected),
		)
		if err != nil {
			return err
		}
		adoption, err := insertBaselineAdoption(
			txCtx, input, *plan, adoptionID, requestPayload,
			requestChecksum, outcomeChecksum,
		)
		if err != nil {
			return err
		}

		for _, component := range expected {
			created, err := adoptBaselineComponent(
				txCtx, *adoption, *plan, component, time.Now().UTC(),
			)
			if err != nil {
				return err
			}
			adoption.Components = append(adoption.Components, *created)
		}
		if err := markBaselineAdoptionPlanExecuted(txCtx, *plan); err != nil {
			return err
		}
		if err := recordBaselineAdoptionAudit(txCtx, *adoption); err != nil {
			return err
		}
		result = adoption
		return nil
	})
	if err != nil {
		return nil, mapBaselineAdoptionWriteError(err)
	}
	return result, nil
}

func baselineAdoptionReplayMatches(
	existing types.BaselineAdoption,
	input types.CreateBaselineAdoptionInput,
	requestChecksum string,
) bool {
	return existing.OrganizationID == input.OrganizationID &&
		existing.DeploymentPlanID == input.DeploymentPlanID &&
		existing.RequestChecksum == requestChecksum
}

func canonicalizeBaselineAdoptionInput(
	input types.CreateBaselineAdoptionInput,
) ([]byte, string, error) {
	components := slices.Clone(input.Components)
	for index := range components {
		components[index].ComponentKey = strings.TrimSpace(components[index].ComponentKey)
		components[index].SourceCommit = strings.TrimSpace(components[index].SourceCommit)
		components[index].BuildID = strings.TrimSpace(components[index].BuildID)
		components[index].Platform = strings.TrimSpace(components[index].Platform)
		components[index].SchemaVersion = strings.TrimSpace(components[index].SchemaVersion)
	}
	slices.SortFunc(components, func(a, b types.BaselineAdoptionComponentInput) int {
		if compared := strings.Compare(a.ComponentKey, b.ComponentKey); compared != 0 {
			return compared
		}
		return strings.Compare(a.ComponentInstanceID.String(), b.ComponentInstanceID.String())
	})
	payload, err := json.Marshal(struct {
		Schema                         string                                 `json:"schema"`
		OrganizationID                 uuid.UUID                              `json:"organizationId"`
		DeploymentPlanID               uuid.UUID                              `json:"deploymentPlanId"`
		ActorUserAccountID             uuid.UUID                              `json:"actorUserAccountId"`
		IdempotencyKey                 string                                 `json:"idempotencyKey"`
		Reason                         string                                 `json:"reason"`
		ExpectedPlanChecksum           string                                 `json:"expectedPlanChecksum"`
		ExpectedProductReleaseChecksum string                                 `json:"expectedProductReleaseChecksum"`
		ExpectedTargetConfigChecksum   string                                 `json:"expectedTargetConfigChecksum"`
		Components                     []types.BaselineAdoptionComponentInput `json:"components"`
	}{
		Schema:         "distr.baseline-adoption-request/v1",
		OrganizationID: input.OrganizationID, DeploymentPlanID: input.DeploymentPlanID,
		ActorUserAccountID:   input.ActorUserAccountID,
		IdempotencyKey:       strings.TrimSpace(input.IdempotencyKey),
		Reason:               strings.TrimSpace(input.Reason),
		ExpectedPlanChecksum: strings.TrimSpace(input.ExpectedPlanChecksum),
		ExpectedProductReleaseChecksum: strings.TrimSpace(
			input.ExpectedProductReleaseChecksum,
		),
		ExpectedTargetConfigChecksum: strings.TrimSpace(input.ExpectedTargetConfigChecksum),
		Components:                   components,
	})
	if err != nil {
		return nil, "", fmt.Errorf("canonicalize baseline adoption request: %w", err)
	}
	sum := sha256.Sum256(payload)
	return payload, "sha256:" + hex.EncodeToString(sum[:]), nil
}

func validateBaselineAdoptionInput(input types.CreateBaselineAdoptionInput) error {
	if input.OrganizationID == uuid.Nil || input.DeploymentPlanID == uuid.Nil ||
		input.ActorUserAccountID == uuid.Nil {
		return apierrors.NewBadRequest("baseline adoption identity is incomplete")
	}
	if !baselineAdoptionIdempotencyPattern.MatchString(strings.TrimSpace(input.IdempotencyKey)) {
		return apierrors.NewBadRequest("baseline adoption idempotency key is invalid")
	}
	reason := strings.TrimSpace(input.Reason)
	if reason == "" || len(reason) > 2048 || strings.ContainsAny(reason, "\r\n") {
		return apierrors.NewBadRequest("baseline adoption reason is invalid")
	}
	for _, checksum := range []string{
		input.ExpectedPlanChecksum,
		input.ExpectedProductReleaseChecksum,
		input.ExpectedTargetConfigChecksum,
	} {
		if !baselineAdoptionChecksumPattern.MatchString(checksum) {
			return apierrors.NewBadRequest("baseline adoption checksum is invalid")
		}
	}
	if len(input.Components) < 1 || len(input.Components) > baselineAdoptionMaximumComponents {
		return apierrors.NewBadRequest("baseline adoption component count is invalid")
	}
	for _, component := range input.Components {
		if component.HealthEvidenceKind != types.BaselineAdoptionHealthStandardReadiness &&
			component.HealthEvidenceKind != types.BaselineAdoptionHealthLegacyLiveness {
			return apierrors.NewBadRequest("baseline adoption health evidence kind is invalid")
		}
		if component.HealthEvidenceKind == types.BaselineAdoptionHealthLegacyLiveness &&
			component.HealthEvidenceUse != types.BaselineAdoptionHealthUseBaselineRollback {
			return apierrors.NewBadRequest(
				"legacy liveness evidence is restricted to baseline or rollback use",
			)
		}
		if component.HealthEvidenceKind == types.BaselineAdoptionHealthStandardReadiness &&
			component.HealthEvidenceUse != types.BaselineAdoptionHealthUsePromotionEligible {
			return apierrors.NewBadRequest("standard readiness evidence use is invalid")
		}
		if !baselineAdoptionChecksumPattern.MatchString(component.HealthPolicyChecksum) {
			return apierrors.NewBadRequest("baseline adoption health policy checksum is invalid")
		}
	}
	return nil
}

func baselineAdoptionOutcomeChecksum(
	adoptionID uuid.UUID,
	requestChecksum string,
	componentCount int,
) (string, error) {
	payload, err := json.Marshal(struct {
		Schema              string    `json:"schema"`
		BaselineAdoptionID  uuid.UUID `json:"baselineAdoptionId"`
		RequestChecksum     string    `json:"requestChecksum"`
		Status              string    `json:"status"`
		DeploymentPerformed bool      `json:"deploymentPerformed"`
		TaskCount           int       `json:"taskCount"`
		LockCount           int       `json:"lockCount"`
		ExecutionCount      int       `json:"executionCount"`
		ComponentCount      int       `json:"componentCount"`
	}{
		Schema: "distr.baseline-adoption-outcome/v1", BaselineAdoptionID: adoptionID,
		RequestChecksum: requestChecksum, Status: string(types.BaselineAdoptionStatusAdopted),
		DeploymentPerformed: false, TaskCount: 0, LockCount: 0, ExecutionCount: 0,
		ComponentCount: componentCount,
	})
	if err != nil {
		return "", fmt.Errorf("canonicalize baseline adoption outcome: %w", err)
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func lockBaselineAdoptionPlan(
	ctx context.Context,
	input types.CreateBaselineAdoptionInput,
) (*baselineAdoptionPlan, types.TargetDeploymentPlanCanonical, error) {
	var plan baselineAdoptionPlan
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT plan.id, plan.organization_id, plan.application_id,
		       plan.release_bundle_id, plan.environment_id,
		       plan.deployment_unit_id, plan.target_config_snapshot_id,
		       target.deployment_target_id, plan.status, plan.plan_schema,
		       plan.protocol_version, plan.canonical_checksum,
		       plan.canonical_payload, plan.sealed_at,
		       product.canonical_checksum, product.status, product.kind,
		       config.canonical_checksum, config.target_platform,
		       unit.target_environment_assignment_id, unit.deployment_scope_id
		FROM DeploymentPlan plan
		JOIN DeploymentPlanTarget target
		  ON target.deployment_plan_id = plan.id
		 AND target.organization_id = plan.organization_id
		JOIN ReleaseBundle product
		  ON product.id = plan.release_bundle_id
		 AND product.organization_id = plan.organization_id
		JOIN TargetConfigSnapshot config
		  ON config.id = plan.target_config_snapshot_id
		 AND config.organization_id = plan.organization_id
		JOIN DeploymentUnit unit
		  ON unit.id = plan.deployment_unit_id
		 AND unit.organization_id = plan.organization_id
		 AND unit.target_environment_assignment_id = config.target_environment_assignment_id
		 AND unit.deployment_target_id = target.deployment_target_id
		 AND unit.retired_at IS NULL
		WHERE plan.id = @deploymentPlanID
		  AND plan.organization_id = @organizationID
		  AND config.deployment_unit_id = unit.id
		  AND config.environment_id = plan.environment_id
		FOR UPDATE OF plan`,
		pgx.NamedArgs{
			"deploymentPlanID": input.DeploymentPlanID,
			"organizationID":   input.OrganizationID,
		},
	).Scan(
		&plan.ID, &plan.OrganizationID, &plan.ApplicationID,
		&plan.ProductReleaseID, &plan.EnvironmentID, &plan.DeploymentUnitID,
		&plan.TargetConfigSnapshotID, &plan.DeploymentTargetID, &plan.Status,
		&plan.PlanSchema, &plan.ProtocolVersion, &plan.PlanChecksum,
		&plan.CanonicalPayload, &plan.SealedAt, &plan.ProductReleaseChecksum,
		&plan.ProductReleaseStatus, &plan.ProductReleaseKind,
		&plan.TargetConfigChecksum, &plan.TargetConfigPlatform,
		&plan.TargetEnvironmentAssignmentID, &plan.DeploymentScopeID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, types.TargetDeploymentPlanCanonical{}, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, types.TargetDeploymentPlanCanonical{},
			fmt.Errorf("lock baseline adoption plan: %w", err)
	}
	if plan.Status != types.DeploymentPlanStatusReady || plan.SealedAt == nil ||
		plan.PlanSchema != types.TargetDeploymentPlanSchemaV2 ||
		plan.ProtocolVersion != types.DeploymentPlanProtocolV2 {
		return nil, types.TargetDeploymentPlanCanonical{}, apierrors.NewConflict(
			"baseline adoption requires a sealed READY native v2 plan",
		)
	}
	if plan.ProductReleaseKind != types.ReleaseBundleKindProduct ||
		plan.ProductReleaseStatus != types.ReleaseBundleStatusPublished {
		return nil, types.TargetDeploymentPlanCanonical{}, apierrors.NewConflict(
			"baseline adoption requires a published Product Release",
		)
	}
	if plan.PlanChecksum != input.ExpectedPlanChecksum ||
		plan.ProductReleaseChecksum != input.ExpectedProductReleaseChecksum ||
		plan.TargetConfigChecksum != input.ExpectedTargetConfigChecksum {
		return nil, types.TargetDeploymentPlanCanonical{}, apierrors.NewConflict(
			"baseline adoption checksum precondition is stale",
		)
	}
	var canonical types.TargetDeploymentPlanCanonical
	if err := json.Unmarshal(plan.CanonicalPayload, &canonical); err != nil {
		return nil, canonical, apierrors.NewConflict("baseline plan canonical payload is invalid")
	}
	if canonical.Schema != types.TargetDeploymentPlanSchemaV2 ||
		canonical.ProtocolVersion != types.DeploymentPlanProtocolV2 ||
		canonical.ProductReleaseID != plan.ProductReleaseID ||
		canonical.ProductReleaseChecksum != plan.ProductReleaseChecksum ||
		canonical.DeploymentUnitID != plan.DeploymentUnitID ||
		canonical.DeploymentScopeID != plan.DeploymentScopeID ||
		canonical.EnvironmentAssignmentID != plan.TargetEnvironmentAssignmentID ||
		canonical.EnvironmentID != plan.EnvironmentID ||
		canonical.DeploymentTargetID != plan.DeploymentTargetID ||
		canonical.TargetConfigSnapshotID != plan.TargetConfigSnapshotID ||
		canonical.TargetConfigSnapshotChecksum != plan.TargetConfigChecksum ||
		canonical.TargetPlatform != plan.TargetConfigPlatform ||
		!canonical.Bootstrap || canonical.PreviousStateSourcePlanID != nil {
		return nil, canonical, apierrors.NewConflict(
			"baseline plan canonical placement or bootstrap identity is invalid",
		)
	}
	for _, fact := range canonical.ConfigVerificationFacts {
		if !fact.Verified || fact.Checksum == "" || fact.ObservedChecksum != fact.Checksum {
			return nil, canonical, apierrors.NewConflict(
				"baseline target config lacks exact verified object evidence",
			)
		}
	}
	return &plan, canonical, nil
}

func baselineAdoptionExpectedComponents(
	canonical types.TargetDeploymentPlanCanonical,
	input types.CreateBaselineAdoptionInput,
) ([]baselineAdoptionExpectedComponent, error) {
	if len(canonical.ComponentReleasePins) == 0 ||
		len(canonical.ComponentReleasePins) != len(canonical.ComponentBindings) ||
		len(canonical.ComponentReleasePins) != len(input.Components) {
		return nil, apierrors.NewConflict("baseline adoption component coverage is incomplete")
	}
	requests := make(map[string]types.BaselineAdoptionComponentInput, len(input.Components))
	instances := make(map[uuid.UUID]struct{}, len(input.Components))
	for _, component := range input.Components {
		component.ComponentKey = strings.TrimSpace(component.ComponentKey)
		if _, exists := requests[component.ComponentKey]; exists {
			return nil, apierrors.NewBadRequest("baseline adoption contains duplicate component keys")
		}
		if _, exists := instances[component.ComponentInstanceID]; exists {
			return nil, apierrors.NewBadRequest("baseline adoption contains duplicate component instances")
		}
		requests[component.ComponentKey] = component
		instances[component.ComponentInstanceID] = struct{}{}
	}
	expected := make([]baselineAdoptionExpectedComponent, 0, len(canonical.ComponentReleasePins))
	for _, pin := range canonical.ComponentReleasePins {
		component, exists := requests[pin.ComponentKey]
		binding, bound := findComponentBinding(canonical.ComponentBindings, pin.ComponentKey)
		if !exists || !bound || component.ComponentInstanceID != binding.ComponentInstanceID ||
			component.ComponentReleaseID != pin.ComponentReleaseID ||
			component.ComponentReleaseChecksum != pin.ReleaseChecksum ||
			component.CapabilityChecksum != pin.ReleaseChecksum ||
			component.ArtifactDigest != pin.PlatformDigest ||
			component.Platform != canonical.TargetPlatform ||
			component.ConfigChecksum != canonical.TargetConfigSnapshotChecksum ||
			component.SchemaVersion != strings.TrimSpace(pin.Version) || !pin.ProvenanceVerified ||
			!slices.Contains(pin.Platforms, canonical.TargetPlatform) {
			return nil, apierrors.NewConflict(
				"baseline adoption component material does not match the frozen plan",
			)
		}
		topologyChecksum, err := desiredTopologyChecksum(canonical, binding)
		if err != nil {
			return nil, err
		}
		if component.TopologyChecksum != topologyChecksum ||
			!baselineAdoptionPinHasArtifact(pin, component) ||
			!baselineAdoptionPinHasProvenance(pin, component) {
			return nil, apierrors.NewConflict(
				"baseline adoption artifact or provenance does not match the frozen plan",
			)
		}
		expected = append(expected, baselineAdoptionExpectedComponent{
			Input: component, Pin: pin, Binding: binding,
		})
	}
	slices.SortFunc(expected, func(a, b baselineAdoptionExpectedComponent) int {
		return strings.Compare(a.Input.ComponentKey, b.Input.ComponentKey)
	})
	return expected, nil
}

func baselineAdoptionPinHasArtifact(
	pin types.ComponentReleasePin,
	component types.BaselineAdoptionComponentInput,
) bool {
	for _, artifact := range pin.Artifacts {
		if artifact.Platform == component.Platform &&
			artifact.PlatformDigest == component.ArtifactDigest {
			return true
		}
	}
	return false
}

func baselineAdoptionPinHasProvenance(
	pin types.ComponentReleasePin,
	component types.BaselineAdoptionComponentInput,
) bool {
	for _, fact := range pin.ProvenanceFacts {
		if fact.VerificationID == component.ProvenanceVerificationID &&
			fact.Platform == component.Platform &&
			fact.ArtifactDigest == component.ArtifactDigest &&
			fact.EvidenceDigest == component.ProvenanceEvidenceDigest &&
			fact.PolicyChecksum == component.ProvenancePolicyChecksum {
			return true
		}
	}
	return false
}

func ensureBaselineAdoptionHasNoExecution(
	ctx context.Context,
	plan baselineAdoptionPlan,
) error {
	var taskExists, externalExecutionExists, adoptionExists bool
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT EXISTS (
		         SELECT 1 FROM Task
		         WHERE deployment_plan_id = @deploymentPlanID
		           AND organization_id = @organizationID
		       ), EXISTS (
		         SELECT 1 FROM ExternalExecution
		         WHERE deployment_plan_id = @deploymentPlanID
		           AND organization_id = @organizationID
		       ), EXISTS (
		         SELECT 1 FROM BaselineAdoption
		         WHERE deployment_plan_id = @deploymentPlanID
		           AND organization_id = @organizationID
		       )`,
		pgx.NamedArgs{
			"deploymentPlanID": plan.ID,
			"organizationID":   plan.OrganizationID,
		},
	).Scan(&taskExists, &externalExecutionExists, &adoptionExists)
	if err != nil {
		return fmt.Errorf("inspect baseline adoption execution history: %w", err)
	}
	if taskExists || externalExecutionExists {
		return apierrors.NewConflict(
			"baseline adoption cannot use a plan with deployment tasks or executions",
		)
	}
	if adoptionExists {
		return apierrors.NewConflict("deployment plan already has a baseline adoption outcome")
	}
	return nil
}

func insertBaselineAdoption(
	ctx context.Context,
	input types.CreateBaselineAdoptionInput,
	plan baselineAdoptionPlan,
	adoptionID uuid.UUID,
	requestPayload []byte,
	requestChecksum,
	outcomeChecksum string,
) (*types.BaselineAdoption, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		INSERT INTO BaselineAdoption (
		  id, organization_id, deployment_plan_id, product_release_id,
		  target_config_snapshot_id, deployment_unit_id, environment_id,
		  deployment_target_id, actor_user_account_id, authorization_action,
		  idempotency_key, reason, plan_checksum, product_release_checksum,
		  target_config_checksum, request_payload, request_checksum,
		  outcome_checksum, status, deployment_performed, task_count,
		  lock_count, execution_count
		) VALUES (
		  @id, @organizationID, @deploymentPlanID, @productReleaseID,
		  @targetConfigSnapshotID, @deploymentUnitID, @environmentID,
		  @deploymentTargetID, @actorUserAccountID, @authorizationAction,
		  @idempotencyKey, @reason, @planChecksum, @productReleaseChecksum,
		  @targetConfigChecksum, @requestPayload, @requestChecksum,
		  @outcomeChecksum, @status, FALSE, 0, 0, 0
		) RETURNING `+baselineAdoptionOutputExpr,
		pgx.NamedArgs{
			"id": adoptionID, "organizationID": input.OrganizationID,
			"deploymentPlanID": plan.ID, "productReleaseID": plan.ProductReleaseID,
			"targetConfigSnapshotID": plan.TargetConfigSnapshotID,
			"deploymentUnitID":       plan.DeploymentUnitID, "environmentID": plan.EnvironmentID,
			"deploymentTargetID":  plan.DeploymentTargetID,
			"actorUserAccountID":  input.ActorUserAccountID,
			"authorizationAction": string(types.ActionPlanExecute),
			"idempotencyKey":      strings.TrimSpace(input.IdempotencyKey),
			"reason":              strings.TrimSpace(input.Reason), "planChecksum": plan.PlanChecksum,
			"productReleaseChecksum": plan.ProductReleaseChecksum,
			"targetConfigChecksum":   plan.TargetConfigChecksum,
			"requestPayload":         requestPayload, "requestChecksum": requestChecksum,
			"outcomeChecksum": outcomeChecksum,
			"status":          types.BaselineAdoptionStatusAdopted,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("insert baseline adoption: %w", err)
	}
	value, err := pgx.CollectExactlyOneRow(
		rows, pgx.RowToStructByName[types.BaselineAdoption],
	)
	if err != nil {
		return nil, fmt.Errorf("collect baseline adoption: %w", err)
	}
	return &value, nil
}

func adoptBaselineComponent(
	ctx context.Context,
	adoption types.BaselineAdoption,
	plan baselineAdoptionPlan,
	expected baselineAdoptionExpectedComponent,
	now time.Time,
) (*types.BaselineAdoptionComponent, error) {
	if err := establishEmptyBaselineDesiredHead(ctx, plan, expected.Input); err != nil {
		return nil, err
	}
	capturedAt, freshUntil, evidenceReference, err := validateRetainedBaselineEvidence(
		ctx, adoption, plan, expected, now,
	)
	if err != nil {
		return nil, err
	}
	componentID, activeID := uuid.New(), uuid.New()
	component, err := insertBaselineAdoptionComponent(
		ctx, adoption, plan, expected.Input, componentID, activeID,
		capturedAt, freshUntil, evidenceReference,
	)
	if err != nil {
		return nil, err
	}
	if err := insertBaselineActiveDesiredRevision(
		ctx, adoption, plan, expected.Input, componentID, activeID,
	); err != nil {
		return nil, err
	}
	result, err := internalctx.GetDb(ctx).Exec(ctx, `
		UPDATE ComponentDesiredStateHead
		SET active_revision_id = @activeDesiredRevisionID,
		    component_key = @componentKey,
		    updated_at = clock_timestamp()
		WHERE organization_id = @organizationID
		  AND deployment_unit_id = @deploymentUnitID
		  AND component_instance_id = @componentInstanceID
		  AND pending_revision_id IS NULL
		  AND active_revision_id IS NULL
		  AND NOT quarantined`,
		pgx.NamedArgs{
			"activeDesiredRevisionID": activeID,
			"componentKey":            expected.Input.ComponentKey,
			"organizationID":          plan.OrganizationID,
			"deploymentUnitID":        plan.DeploymentUnitID,
			"componentInstanceID":     expected.Input.ComponentInstanceID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("activate baseline desired-state head: %w", err)
	}
	if result.RowsAffected() != 1 {
		return nil, apierrors.NewConflict("baseline desired-state head changed concurrently")
	}
	return component, nil
}

func establishEmptyBaselineDesiredHead(
	ctx context.Context,
	plan baselineAdoptionPlan,
	component types.BaselineAdoptionComponentInput,
) error {
	_, err := internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO ComponentDesiredStateHead (
		  organization_id, deployment_unit_id, component_instance_id,
		  component_key, pending_revision_id, active_revision_id
		) VALUES (
		  @organizationID, @deploymentUnitID, @componentInstanceID,
		  @componentKey, NULL, NULL
		) ON CONFLICT (
		  organization_id, deployment_unit_id, component_instance_id
		) DO NOTHING`,
		pgx.NamedArgs{
			"organizationID":      plan.OrganizationID,
			"deploymentUnitID":    plan.DeploymentUnitID,
			"componentInstanceID": component.ComponentInstanceID,
			"componentKey":        component.ComponentKey,
		},
	)
	if err != nil {
		return fmt.Errorf("establish baseline desired-state head: %w", err)
	}
	var pendingID, activeID *uuid.UUID
	var quarantined, historyExists bool
	err = internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT head.pending_revision_id, head.active_revision_id,
		       head.quarantined,
		       EXISTS (
		         SELECT 1 FROM PendingDesiredRevision pending
		         WHERE pending.organization_id = head.organization_id
		           AND pending.deployment_unit_id = head.deployment_unit_id
		           AND pending.component_instance_id = head.component_instance_id
		         UNION ALL
		         SELECT 1 FROM ActiveDesiredRevision active
		         WHERE active.organization_id = head.organization_id
		           AND active.deployment_unit_id = head.deployment_unit_id
		           AND active.component_instance_id = head.component_instance_id
		       )
		FROM ComponentDesiredStateHead head
		WHERE head.organization_id = @organizationID
		  AND head.deployment_unit_id = @deploymentUnitID
		  AND head.component_instance_id = @componentInstanceID
		FOR UPDATE`,
		pgx.NamedArgs{
			"organizationID":      plan.OrganizationID,
			"deploymentUnitID":    plan.DeploymentUnitID,
			"componentInstanceID": component.ComponentInstanceID,
		},
	).Scan(&pendingID, &activeID, &quarantined, &historyExists)
	if err != nil {
		return fmt.Errorf("lock baseline desired-state head: %w", err)
	}
	if pendingID != nil || activeID != nil || quarantined || historyExists {
		return apierrors.NewConflict(
			"baseline adoption cannot replace existing native desired-state lineage",
		)
	}
	return nil
}

func validateRetainedBaselineEvidence(
	ctx context.Context,
	adoption types.BaselineAdoption,
	plan baselineAdoptionPlan,
	expected baselineAdoptionExpectedComponent,
	now time.Time,
) (time.Time, time.Time, string, error) {
	var capturedAt, freshUntil time.Time
	var evidenceReference string
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT observation.captured_at, observation.fresh_until,
		       observation.evidence_reference
		FROM ProductReleaseComponent product_component
		JOIN ReleaseBundle component_release
		  ON component_release.id = product_component.component_release_bundle_id
		 AND component_release.organization_id = product_component.organization_id
		 AND component_release.kind = 'component'
		 AND component_release.status = 'PUBLISHED'
		JOIN ComponentReleaseArtifact artifact
		  ON artifact.release_bundle_id = component_release.id
		 AND artifact.organization_id = component_release.organization_id
		 AND artifact.platform = @platform
		 AND artifact.platform_digest = @artifactDigest
		JOIN ComponentReleaseEvidenceVerification verification
		  ON verification.id = @provenanceVerificationID
		 AND verification.organization_id = artifact.organization_id
		 AND verification.release_bundle_id = artifact.release_bundle_id
		 AND verification.artifact_key = artifact.artifact_key
		 AND verification.platform = artifact.platform
		 AND verification.artifact_digest = artifact.platform_digest
		JOIN TargetConfigSnapshotComponent config_component
		  ON config_component.target_config_snapshot_id = @targetConfigSnapshotID
		 AND config_component.organization_id = product_component.organization_id
		 AND config_component.deployment_unit_id = @deploymentUnitID
		 AND config_component.component_instance_id = @componentInstanceID
		JOIN ComponentInstance instance
		  ON instance.id = config_component.component_instance_id
		 AND instance.deployment_unit_id = config_component.deployment_unit_id
		 AND instance.organization_id = config_component.organization_id
		 AND instance.retired_at IS NULL
		 AND instance.management_state IN ('managed', 'observe_only', 'legacy_cutover')
		JOIN ComponentDefinition definition
		  ON definition.id = instance.component_definition_id
		 AND definition.organization_id = instance.organization_id
		 AND definition.key = @componentKey
		 AND definition.retired_at IS NULL
		JOIN ObserverRegistration observer
		  ON observer.id = @observerID
		 AND observer.organization_id = product_component.organization_id
		 AND observer.deployment_unit_id = @deploymentUnitID
		 AND (observer.component_instance_id IS NULL
		      OR observer.component_instance_id = @componentInstanceID)
		 AND observer.enabled
		JOIN ObservedComponentState observation
		  ON observation.id = @observationID
		 AND observation.organization_id = product_component.organization_id
		 AND observation.observer_id = observer.id
		 AND observation.deployment_unit_id = @deploymentUnitID
		 AND observation.component_instance_id = @componentInstanceID
		 AND observation.component_key = @componentKey
		 AND observation.evidence_checksum = @observationEvidenceChecksum
		 AND observation.artifact_digest = @artifactDigest
		 AND observation.config_checksum = @configChecksum
		 AND observation.schema_version = @schemaVersion
		 AND observation.capability_checksum = @capabilityChecksum
		 AND observation.platform = @platform
		 AND observation.topology_checksum = @topologyChecksum
		 AND observation.state_checksum = @observationStateChecksum
		 AND observation.runtime_state_checksum = @observationRuntimeStateChecksum
		 AND observation.health = 'HEALTHY'
		 AND observation.outcome = 'COMPLETE'
		 AND observation.disposition = 'ACCEPTED'
		 AND observation.trusted
		 AND observation.is_current
		 AND observation.executor_outcome = ''
		 AND observation.fresh_until >= @now
		JOIN ComponentObservationHead observation_head
		  ON observation_head.organization_id = observation.organization_id
		 AND observation_head.observer_id = observation.observer_id
		 AND observation_head.deployment_unit_id = observation.deployment_unit_id
		 AND observation_head.component_instance_id = observation.component_instance_id
		 AND observation_head.observation_id = observation.id
		 AND observation_head.evidence_checksum = observation.evidence_checksum
		 AND observation_head.captured_at = observation.captured_at
		WHERE length(btrim(observation.evidence_reference)) > 0
		  AND product_component.product_release_bundle_id = @productReleaseID
		  AND product_component.organization_id = @organizationID
		  AND product_component.component_release_bundle_id = @componentReleaseID
		  AND product_component.component_release_checksum = @componentReleaseChecksum
		  AND product_component.component_key = @componentKey
		  AND component_release.canonical_checksum = @componentReleaseChecksum
		  AND verification.source_commit = @sourceCommit
		  AND verification.build_id = @buildID
		  AND verification.evidence_digest = @provenanceEvidenceDigest
		  AND verification.policy_checksum = @provenancePolicyChecksum
		FOR UPDATE OF observation, observation_head`,
		pgx.NamedArgs{
			"productReleaseID":         adoption.ProductReleaseID,
			"organizationID":           adoption.OrganizationID,
			"componentReleaseID":       expected.Input.ComponentReleaseID,
			"componentReleaseChecksum": expected.Input.ComponentReleaseChecksum,
			"componentInstanceID":      expected.Input.ComponentInstanceID,
			"componentKey":             expected.Input.ComponentKey,
			"provenanceVerificationID": expected.Input.ProvenanceVerificationID,
			"sourceCommit":             expected.Input.SourceCommit, "buildID": expected.Input.BuildID,
			"provenanceEvidenceDigest":        expected.Input.ProvenanceEvidenceDigest,
			"provenancePolicyChecksum":        expected.Input.ProvenancePolicyChecksum,
			"artifactDigest":                  expected.Input.ArtifactDigest,
			"platform":                        expected.Input.Platform,
			"targetConfigSnapshotID":          adoption.TargetConfigSnapshotID,
			"deploymentUnitID":                plan.DeploymentUnitID,
			"configChecksum":                  expected.Input.ConfigChecksum,
			"schemaVersion":                   expected.Input.SchemaVersion,
			"capabilityChecksum":              expected.Input.CapabilityChecksum,
			"topologyChecksum":                expected.Input.TopologyChecksum,
			"observationID":                   expected.Input.ObservationID,
			"observerID":                      expected.Input.ObserverID,
			"observationEvidenceChecksum":     expected.Input.ObservationEvidenceChecksum,
			"observationStateChecksum":        expected.Input.ObservationStateChecksum,
			"observationRuntimeStateChecksum": expected.Input.ObservationRuntimeStateChecksum,
			"now":                             now.UTC(),
		},
	).Scan(&capturedAt, &freshUntil, &evidenceReference)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, time.Time{}, "", apierrors.NewConflict(
			"baseline adoption lacks exact current release, provenance, config, or observation evidence",
		)
	}
	if err != nil {
		return time.Time{}, time.Time{}, "", fmt.Errorf("validate baseline adoption evidence: %w", err)
	}
	return capturedAt, freshUntil, evidenceReference, nil
}

func insertBaselineAdoptionComponent(
	ctx context.Context,
	adoption types.BaselineAdoption,
	plan baselineAdoptionPlan,
	input types.BaselineAdoptionComponentInput,
	componentID,
	activeID uuid.UUID,
	capturedAt,
	freshUntil time.Time,
	evidenceReference string,
) (*types.BaselineAdoptionComponent, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		INSERT INTO BaselineAdoptionComponent (
		  id, organization_id, baseline_adoption_id, deployment_plan_id,
		  deployment_unit_id, component_instance_id, component_key,
		  component_release_id, component_release_checksum, source_commit,
		  build_id, provenance_verification_id, provenance_evidence_digest,
		  provenance_policy_checksum, artifact_digest, platform,
		  target_config_snapshot_id, config_checksum, schema_version,
		  capability_checksum, topology_checksum, observation_id, observer_id,
		  observation_evidence_checksum, observation_evidence_reference,
		  observation_state_checksum, observation_runtime_state_checksum,
		  health_evidence_kind, health_evidence_use, health_policy_checksum,
		  observation_captured_at,
		  observation_fresh_until, active_desired_revision_id, desired_revision
		) VALUES (
		  @id, @organizationID, @baselineAdoptionID, @deploymentPlanID,
		  @deploymentUnitID, @componentInstanceID, @componentKey,
		  @componentReleaseID, @componentReleaseChecksum, @sourceCommit,
		  @buildID, @provenanceVerificationID, @provenanceEvidenceDigest,
		  @provenancePolicyChecksum, @artifactDigest, @platform,
		  @targetConfigSnapshotID, @configChecksum, @schemaVersion,
		  @capabilityChecksum, @topologyChecksum, @observationID, @observerID,
		  @observationEvidenceChecksum, @observationEvidenceReference,
		  @observationStateChecksum, @observationRuntimeStateChecksum,
		  @healthEvidenceKind, @healthEvidenceUse, @healthPolicyChecksum,
		  @observationCapturedAt,
		  @observationFreshUntil, @activeDesiredRevisionID, 1
		) RETURNING `+baselineAdoptionComponentOutputExpr,
		pgx.NamedArgs{
			"id": componentID, "organizationID": adoption.OrganizationID,
			"baselineAdoptionID": adoption.ID, "deploymentPlanID": plan.ID,
			"deploymentUnitID":    plan.DeploymentUnitID,
			"componentInstanceID": input.ComponentInstanceID,
			"componentKey":        input.ComponentKey, "componentReleaseID": input.ComponentReleaseID,
			"componentReleaseChecksum": input.ComponentReleaseChecksum,
			"sourceCommit":             input.SourceCommit, "buildID": input.BuildID,
			"provenanceVerificationID": input.ProvenanceVerificationID,
			"provenanceEvidenceDigest": input.ProvenanceEvidenceDigest,
			"provenancePolicyChecksum": input.ProvenancePolicyChecksum,
			"artifactDigest":           input.ArtifactDigest, "platform": input.Platform,
			"targetConfigSnapshotID": adoption.TargetConfigSnapshotID,
			"configChecksum":         input.ConfigChecksum, "schemaVersion": input.SchemaVersion,
			"capabilityChecksum": input.CapabilityChecksum,
			"topologyChecksum":   input.TopologyChecksum,
			"observationID":      input.ObservationID, "observerID": input.ObserverID,
			"observationEvidenceChecksum":     input.ObservationEvidenceChecksum,
			"observationEvidenceReference":    evidenceReference,
			"observationStateChecksum":        input.ObservationStateChecksum,
			"observationRuntimeStateChecksum": input.ObservationRuntimeStateChecksum,
			"healthEvidenceKind":              input.HealthEvidenceKind,
			"healthEvidenceUse":               input.HealthEvidenceUse,
			"healthPolicyChecksum":            input.HealthPolicyChecksum,
			"observationCapturedAt":           capturedAt, "observationFreshUntil": freshUntil,
			"activeDesiredRevisionID": activeID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("insert baseline adoption component: %w", err)
	}
	value, err := pgx.CollectExactlyOneRow(
		rows, pgx.RowToStructByName[types.BaselineAdoptionComponent],
	)
	if err != nil {
		return nil, fmt.Errorf("collect baseline adoption component: %w", err)
	}
	return &value, nil
}

func insertBaselineActiveDesiredRevision(
	ctx context.Context,
	adoption types.BaselineAdoption,
	plan baselineAdoptionPlan,
	input types.BaselineAdoptionComponentInput,
	componentID,
	activeID uuid.UUID,
) error {
	_, err := internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO ActiveDesiredRevision (
		  id, organization_id, pending_revision_id, deployment_plan_id,
		  execution_id, deployment_unit_id, component_instance_id,
		  component_key, revision, artifact_digest, config_checksum,
		  schema_version, capability_checksum, platform, topology_checksum,
		  verified_observation_id, source_kind, baseline_adoption_component_id,
		  health_evidence_kind, health_evidence_use
		) VALUES (
		  @id, @organizationID, NULL, @deploymentPlanID,
		  NULL, @deploymentUnitID, @componentInstanceID,
		  @componentKey, 1, @artifactDigest, @configChecksum,
		  @schemaVersion, @capabilityChecksum, @platform, @topologyChecksum,
		  @verifiedObservationID, 'BASELINE_ADOPTION', @baselineAdoptionComponentID,
		  @healthEvidenceKind, @healthEvidenceUse
		)`,
		pgx.NamedArgs{
			"id": activeID, "organizationID": adoption.OrganizationID,
			"deploymentPlanID": plan.ID, "deploymentUnitID": plan.DeploymentUnitID,
			"componentInstanceID": input.ComponentInstanceID,
			"componentKey":        input.ComponentKey, "artifactDigest": input.ArtifactDigest,
			"configChecksum": input.ConfigChecksum, "schemaVersion": input.SchemaVersion,
			"capabilityChecksum": input.CapabilityChecksum, "platform": input.Platform,
			"topologyChecksum":            input.TopologyChecksum,
			"verifiedObservationID":       input.ObservationID,
			"baselineAdoptionComponentID": componentID,
			"healthEvidenceKind":          input.HealthEvidenceKind,
			"healthEvidenceUse":           input.HealthEvidenceUse,
		},
	)
	if err != nil {
		return fmt.Errorf("insert baseline active desired revision: %w", err)
	}
	return nil
}

func markBaselineAdoptionPlanExecuted(
	ctx context.Context,
	plan baselineAdoptionPlan,
) error {
	result, err := internalctx.GetDb(ctx).Exec(ctx, `
		UPDATE DeploymentPlan
		SET status = 'EXECUTED'
		WHERE id = @deploymentPlanID
		  AND organization_id = @organizationID
		  AND status = 'READY'`,
		pgx.NamedArgs{
			"deploymentPlanID": plan.ID,
			"organizationID":   plan.OrganizationID,
		},
	)
	if err != nil {
		return fmt.Errorf("record baseline adoption plan outcome: %w", err)
	}
	if result.RowsAffected() != 1 {
		return apierrors.NewConflict("baseline adoption plan status changed concurrently")
	}
	return nil
}

func recordBaselineAdoptionAudit(
	ctx context.Context,
	adoption types.BaselineAdoption,
) error {
	payload, err := json.Marshal(map[string]any{
		"baselineAdoptionId":  adoption.ID,
		"requestChecksum":     adoption.RequestChecksum,
		"outcomeChecksum":     adoption.OutcomeChecksum,
		"outcome":             types.BaselineAdoptionStatusAdopted,
		"deploymentPerformed": false,
		"taskCount":           0,
		"lockCount":           0,
		"executionCount":      0,
		"componentCount":      len(adoption.Components),
	})
	if err != nil {
		return fmt.Errorf("canonicalize baseline adoption audit payload: %w", err)
	}
	return RecordControlPlaneAuditMutation(
		ctx,
		controlPlaneDomainAuditHook(ctx),
		types.ControlPlaneAuditEventInput{
			OrganizationID:         adoption.OrganizationID,
			EventType:              "baseline_adoption.adopted",
			ActorID:                &adoption.ActorUserAccountID,
			Outcome:                "ADOPTED",
			ProductReleaseID:       &adoption.ProductReleaseID,
			TargetConfigID:         &adoption.TargetConfigSnapshotID,
			DeploymentPlanID:       &adoption.DeploymentPlanID,
			DeploymentTargetID:     &adoption.DeploymentTargetID,
			EnvironmentID:          &adoption.EnvironmentID,
			DeploymentUnitID:       &adoption.DeploymentUnitID,
			ProductReleaseChecksum: adoption.ProductReleaseChecksum,
			TargetConfigChecksum:   adoption.TargetConfigChecksum,
			DeploymentPlanChecksum: adoption.PlanChecksum,
			DesiredStateChecksum:   adoption.OutcomeChecksum,
			Payload:                payload,
		},
	)
}

func getBaselineAdoptionByIdempotencyKey(
	ctx context.Context,
	organizationID uuid.UUID,
	idempotencyKey string,
) (*types.BaselineAdoption, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+baselineAdoptionOutputExpr+`
		FROM BaselineAdoption a
		WHERE a.organization_id = @organizationID
		  AND a.idempotency_key = @idempotencyKey
		FOR SHARE`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"idempotencyKey": strings.TrimSpace(idempotencyKey),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("query baseline adoption idempotency key: %w", err)
	}
	value, err := pgx.CollectExactlyOneRow(
		rows, pgx.RowToStructByName[types.BaselineAdoption],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("collect baseline adoption idempotency key: %w", err)
	}
	components, err := getBaselineAdoptionComponents(ctx, value.ID, value.OrganizationID)
	if err != nil {
		return nil, err
	}
	value.Components = components
	return &value, nil
}

func getBaselineAdoptionComponents(
	ctx context.Context,
	adoptionID,
	organizationID uuid.UUID,
) ([]types.BaselineAdoptionComponent, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+baselineAdoptionComponentOutputExpr+`
		FROM BaselineAdoptionComponent c
		WHERE c.baseline_adoption_id = @baselineAdoptionID
		  AND c.organization_id = @organizationID
		ORDER BY c.component_key, c.component_instance_id`,
		pgx.NamedArgs{
			"baselineAdoptionID": adoptionID,
			"organizationID":     organizationID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("query baseline adoption components: %w", err)
	}
	values, err := pgx.CollectRows(
		rows, pgx.RowToStructByName[types.BaselineAdoptionComponent],
	)
	if err != nil {
		return nil, fmt.Errorf("collect baseline adoption components: %w", err)
	}
	return values, nil
}

func mapBaselineAdoptionWriteError(err error) error {
	if errors.Is(err, apierrors.ErrBadRequest) ||
		errors.Is(err, apierrors.ErrConflict) ||
		errors.Is(err, apierrors.ErrNotFound) {
		return err
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case pgerrcode.UniqueViolation, pgerrcode.SerializationFailure:
			return fmt.Errorf("baseline adoption conflict: %w", apierrors.ErrConflict)
		case pgerrcode.ForeignKeyViolation:
			return fmt.Errorf("baseline adoption evidence is missing: %w", apierrors.ErrNotFound)
		case pgerrcode.CheckViolation:
			return fmt.Errorf("baseline adoption evidence is invalid: %w", apierrors.ErrConflict)
		}
	}
	return err
}
