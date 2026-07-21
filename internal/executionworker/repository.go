package executionworker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var frozenChecksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

const (
	adapterCancelCapability    = "distr.execution.cancel"
	adapterRetrySafeCapability = "distr.execution.retry-safe"
)

// RuntimeRepository owns the durable governance decision and frozen inputs
// used by protocol-v2 dispatch. Implementations must read persisted snapshots,
// not mutable request payloads.
type RuntimeRepository interface {
	EvaluateExecutionV2Admission(context.Context, AdmissionRequest) (AdmissionDecision, error)
	LoadFrozenAttemptInputs(context.Context, CreateAttemptRequest) (FrozenAttemptInputs, error)
}

type RepositoryAdmissionGate struct {
	flags      featureflags.Registry
	repository RuntimeRepository
}

func NewRepositoryAdmissionGate(
	flags featureflags.Registry,
	repository RuntimeRepository,
) *RepositoryAdmissionGate {
	return &RepositoryAdmissionGate{flags: flags, repository: repository}
}

func (g *RepositoryAdmissionGate) EvaluateExecutionV2Admission(
	ctx context.Context,
	request AdmissionRequest,
) (AdmissionDecision, error) {
	decision, err := g.repository.EvaluateExecutionV2Admission(ctx, request)
	if err != nil {
		return AdmissionDecision{}, err
	}
	decision.OperatorFlag = g.flags.IsEnabled(featureflags.KeyOperatorControlPlaneV2)
	decision.ExecutorFlag = g.flags.IsEnabled(featureflags.KeyExecutorProtocolV2)
	return decision, nil
}

type RepositoryFrozenAttemptInputsLoader struct {
	repository RuntimeRepository
}

func NewRepositoryFrozenAttemptInputsLoader(
	repository RuntimeRepository,
) *RepositoryFrozenAttemptInputsLoader {
	return &RepositoryFrozenAttemptInputsLoader{repository: repository}
}

func (l *RepositoryFrozenAttemptInputsLoader) LoadFrozenAttemptInputs(
	ctx context.Context,
	request CreateAttemptRequest,
) (FrozenAttemptInputs, error) {
	return l.repository.LoadFrozenAttemptInputs(ctx, request)
}

// DatabaseRuntimeRepository binds protocol-v2 admission and intent creation to
// the immutable plan/task/governance/adapter snapshots persisted by Distr.
type DatabaseRuntimeRepository struct{}

func (DatabaseRuntimeRepository) EvaluateExecutionV2Admission(
	ctx context.Context,
	request AdmissionRequest,
) (AdmissionDecision, error) {
	task, err := db.GetTask(ctx, request.TaskID, request.OrganizationID)
	if err != nil {
		return AdmissionDecision{}, err
	}
	plan, err := db.GetDeploymentPlan(ctx, request.PlanID, request.OrganizationID)
	if err != nil {
		return AdmissionDecision{}, err
	}
	if !executionScopeMatches(*task, *plan, request) {
		return AdmissionDecision{}, nil
	}

	var decision AdmissionDecision
	err = internalctx.GetDb(ctx).QueryRow(ctx, `
		WITH effective_enrollment AS (
			SELECT scope_kind, enabled, effective_until
			FROM (
				SELECT DISTINCT ON (scope_kind)
					scope_kind, enabled, effective_until
				FROM ControlPlaneEnrollment
				WHERE organization_id = @organizationID
				  AND ((scope_kind = 'organization' AND scope_id = @organizationID)
				    OR (scope_kind = 'environment' AND scope_id = @environmentID))
				  AND effective_from <= clock_timestamp()
				ORDER BY scope_kind, revision DESC
			) latest
		),
		latest_admission AS (
			SELECT admission.*
			FROM AdmissionEvaluation admission
			WHERE admission.organization_id = @organizationID
			  AND admission.deployment_plan_id = @planID
			ORDER BY admission.created_at DESC, admission.id DESC
			LIMIT 1
		),
		frozen_adapter AS (
			SELECT frozen.*
			FROM DeploymentPlanStepAdapter frozen
			WHERE frozen.organization_id = @organizationID
			  AND frozen.deployment_plan_id = @planID
			  AND frozen.step_key = @stepKey
		)
		SELECT
			COALESCE((
				SELECT count(*) = 2
				   AND bool_and(enabled)
				   AND bool_and(effective_until IS NULL OR effective_until > clock_timestamp())
				FROM effective_enrollment
			), false),
			EXISTS (
				SELECT 1
				FROM ApprovalRequest approval
				WHERE approval.organization_id = @organizationID
				  AND approval.subject_type = 'deployment_plan'
				  AND approval.subject_id = @planID
				  AND approval.subject_revision = 1
				  AND approval.subject_checksum = @planChecksum
				  AND approval.state = 'APPROVED'
				  AND approval.invalidation_reason IS NULL
				  AND approval.invalidated_at IS NULL
				  AND approval.expires_at > clock_timestamp()
			),
			EXISTS (
				SELECT 1
				FROM latest_admission admission
				JOIN ApprovalRequest approval
				  ON approval.id = admission.approval_request_id
				 AND approval.organization_id = admission.organization_id
				WHERE admission.organization_id = @organizationID
				  AND admission.deployment_plan_id = @planID
				  AND admission.plan_revision = 1
				  AND admission.plan_checksum = @planChecksum
				  AND admission.plan_schema = 'distr.target-deployment-plan/v2'
				  AND admission.protocol_version = 'v2'
				  AND admission.decision = 'ADMIT'
				  AND admission.evaluated_at <= clock_timestamp()
				  AND admission.material_checksum ~ '^sha256:[0-9a-f]{64}$'
				  AND admission.approval_request_revision = approval.revision
				  AND admission.effective_policy_checksum = approval.effective_policy_checksum
				  AND approval.subject_revision = admission.plan_revision
				  AND approval.subject_checksum = admission.plan_checksum
				  AND approval.state = 'APPROVED'
				  AND approval.invalidation_reason IS NULL
				  AND approval.invalidated_at IS NULL
				  AND approval.expires_at > clock_timestamp()
			),
			EXISTS (
				SELECT 1
				FROM frozen_adapter frozen
				JOIN AdapterAssignment assignment
				  ON assignment.id = frozen.adapter_assignment_id
				 AND assignment.organization_id = frozen.organization_id
				JOIN AdapterImplementation implementation
				  ON implementation.id = frozen.adapter_implementation_id
				 AND implementation.organization_id = frozen.organization_id
				JOIN AdapterCapability adapter_capability
				  ON adapter_capability.adapter_implementation_id = implementation.id
				 AND adapter_capability.organization_id = implementation.organization_id
				 AND adapter_capability.capability = frozen.capability
				 AND adapter_capability.version = frozen.capability_version
				JOIN AgentCapabilityReport report
				  ON report.organization_id = frozen.organization_id
				 AND report.deployment_target_id = @deploymentTargetID
				 AND report.protocol_version = 'v2'
				JOIN AgentActionCapability action_capability
				  ON action_capability.report_id = report.id
				 AND action_capability.organization_id = report.organization_id
				 AND action_capability.deployment_target_id = report.deployment_target_id
				 AND action_capability.action_type = frozen.capability
				 AND frozen.capability_version = ANY(action_capability.versions)
				JOIN TargetConfigSnapshot config
				  ON config.id = frozen.config_snapshot_id
				 AND config.organization_id = frozen.organization_id
				WHERE assignment.enabled
				  AND implementation.enabled
				  AND assignment.adapter_implementation_id = frozen.adapter_implementation_id
				  AND implementation.version = frozen.implementation_version
				  AND assignment.scope_type = frozen.scope_type
				  AND assignment.scope_reference = frozen.scope_reference
				  AND assignment.config_snapshot_id = frozen.config_snapshot_id
				  AND assignment.config_checksum = frozen.config_checksum
				  AND config.canonical_checksum = frozen.config_checksum
				  AND assignment.key_id = frozen.key_id
				  AND assignment.public_key_fingerprint = frozen.public_key_fingerprint
				  AND assignment.signing_key_reference = frozen.signing_key_reference
				  AND assignment.signing_key_version_fingerprint = frozen.signing_key_version_fingerprint
				  AND EXISTS (
					SELECT 1
					FROM DeploymentPreflightRun preflight
					JOIN DeploymentPreflightCheck check_result
					  ON check_result.deployment_preflight_run_id = preflight.id
					 AND check_result.organization_id = preflight.organization_id
					WHERE preflight.organization_id = @organizationID
					  AND preflight.deployment_plan_id = @planID
					  AND preflight.plan_checksum = @planChecksum
					  AND preflight.status = 'PASSED'
					  AND check_result.task_id = @taskID
					  AND check_result.status = 'PASSED'
					  AND NOT EXISTS (
						SELECT 1 FROM DeploymentPreflightCheck failed
						WHERE failed.deployment_preflight_run_id = preflight.id
						  AND failed.organization_id = preflight.organization_id
						  AND failed.task_id = @taskID
						  AND failed.status <> 'PASSED'
					  )
				  )
			)
	`, pgx.NamedArgs{
		"organizationID": request.OrganizationID, "environmentID": request.EnvironmentID,
		"deploymentTargetID": request.DeploymentTargetID, "planID": request.PlanID,
		"planChecksum": plan.CanonicalChecksum, "taskID": request.TaskID,
		"stepKey": strings.TrimSpace(request.StepKey),
	}).Scan(
		&decision.ScopedEnrollment,
		&decision.PlanApproved,
		&decision.PlanAdmitted,
		&decision.AdapterPreflight,
	)
	if err != nil {
		return AdmissionDecision{}, fmt.Errorf("read executor-v2 governance evidence: %w", err)
	}
	return decision, nil
}

func executionScopeMatches(
	task types.Task,
	plan types.DeploymentPlan,
	request AdmissionRequest,
) bool {
	if task.ID != request.TaskID || task.OrganizationID != request.OrganizationID ||
		task.DeploymentTargetID != request.DeploymentTargetID ||
		task.DeploymentPlanID != request.PlanID || task.EnvironmentID != request.EnvironmentID ||
		task.ProtocolVersion != types.ExecutionProtocolVersionV2 ||
		plan.ID != request.PlanID || plan.OrganizationID != request.OrganizationID ||
		plan.EnvironmentID != request.EnvironmentID || plan.ProtocolVersion != types.DeploymentPlanProtocolV2 ||
		plan.PlanSchema != types.TargetDeploymentPlanSchemaV2 {
		return false
	}
	for _, step := range task.StepRuns {
		if step.ID == request.StepRunID && step.StepKey == strings.TrimSpace(request.StepKey) {
			return true
		}
	}
	return false
}

type frozenAdapterEvidence struct {
	AssignmentID                 uuid.UUID
	ImplementationID             uuid.UUID
	ImplementationVersion        string
	Capability                   string
	CapabilityVersion            string
	ScopeType                    string
	ScopeReference               string
	ConfigSnapshotID             uuid.UUID
	ConfigChecksum               string
	KeyID                        string
	PublicKeyFingerprint         string
	SigningKeyReference          string
	SigningKeyVersionFingerprint string
	CancelCapabilityVersion      string
	RetrySafeCapabilityVersion   string
	TimeoutSeconds               int
}

func (DatabaseRuntimeRepository) LoadFrozenAttemptInputs(
	ctx context.Context,
	request CreateAttemptRequest,
) (FrozenAttemptInputs, error) {
	task, err := db.GetTask(ctx, request.TaskID, request.OrganizationID)
	if err != nil {
		return FrozenAttemptInputs{}, err
	}
	plan, err := db.GetDeploymentPlan(ctx, request.PlanID, request.OrganizationID)
	if err != nil {
		return FrozenAttemptInputs{}, err
	}
	if task.DeploymentPlanID != plan.ID || task.DeploymentTargetID != request.DeploymentTargetID ||
		task.ProtocolVersion != types.ExecutionProtocolVersionV2 ||
		plan.ProtocolVersion != types.DeploymentPlanProtocolV2 ||
		plan.PlanSchema != types.TargetDeploymentPlanSchemaV2 ||
		!frozenChecksumPattern.MatchString(plan.CanonicalChecksum) {
		return FrozenAttemptInputs{}, errors.New("execution task is not bound to a frozen v2 plan")
	}

	latest, err := db.GetLatestExecutionAttemptForTaskStep(
		ctx, request.OrganizationID, request.TaskID, request.StepRunID,
	)
	if errors.Is(err, apierrors.ErrNotFound) {
		latest = nil
	} else if err != nil {
		return FrozenAttemptInputs{}, err
	}

	canonical, step, pin, artifact, err := resolveFrozenPlanArtifact(
		plan.CanonicalPayload, request.StepKey,
	)
	if err != nil {
		return FrozenAttemptInputs{}, err
	}
	if canonical.DeploymentTargetID != task.DeploymentTargetID ||
		canonical.EnvironmentID != task.EnvironmentID ||
		canonical.ProtocolVersion != types.DeploymentPlanProtocolV2 {
		return FrozenAttemptInputs{}, errors.New("canonical plan target scope does not match execution task")
	}
	if err := verifyFrozenArtifact(ctx, request.OrganizationID, pin, artifact); err != nil {
		return FrozenAttemptInputs{}, err
	}
	adapter, err := loadFrozenAdapterEvidence(
		ctx, request.OrganizationID, request.PlanID, request.StepKey,
	)
	if err != nil {
		return FrozenAttemptInputs{}, err
	}
	resourceKey, err := db.CanonicalExecutionFenceResourceKey(task.Locks)
	if err != nil {
		return FrozenAttemptInputs{}, err
	}
	return deriveFrozenAttemptInputs(
		*plan, step, artifact.PlatformDigest, adapter, resourceKey, latest, request.Retry,
	)
}

func resolveFrozenPlanArtifact(
	payload []byte,
	stepKey string,
) (
	types.TargetDeploymentPlanCanonical,
	types.TargetPlanStep,
	types.ComponentReleasePin,
	types.PinnedReleaseArtifact,
	error,
) {
	var canonical types.TargetDeploymentPlanCanonical
	if err := json.Unmarshal(payload, &canonical); err != nil {
		return canonical, types.TargetPlanStep{}, types.ComponentReleasePin{},
			types.PinnedReleaseArtifact{}, fmt.Errorf("decode canonical deployment plan: %w", err)
	}
	wantedStep := strings.TrimSpace(stepKey)
	var step *types.TargetPlanStep
	for i := range canonical.Graph.Steps {
		if canonical.Graph.Steps[i].StepKey == wantedStep {
			step = &canonical.Graph.Steps[i]
			break
		}
	}
	if step == nil || strings.TrimSpace(step.ComponentKey) == "" || step.ComponentReleaseID == nil {
		return canonical, types.TargetPlanStep{}, types.ComponentReleasePin{},
			types.PinnedReleaseArtifact{}, errors.New("frozen plan step component release was not found")
	}
	var pin *types.ComponentReleasePin
	for i := range canonical.ComponentReleasePins {
		candidate := &canonical.ComponentReleasePins[i]
		if candidate.ComponentKey == step.ComponentKey &&
			candidate.ComponentReleaseID == *step.ComponentReleaseID {
			pin = candidate
			break
		}
	}
	if pin == nil {
		return canonical, *step, types.ComponentReleasePin{},
			types.PinnedReleaseArtifact{}, errors.New("frozen component release pin was not found")
	}
	matching := make([]types.PinnedReleaseArtifact, 0, 1)
	for _, candidate := range pin.Artifacts {
		if candidate.Platform == canonical.TargetPlatform {
			matching = append(matching, candidate)
		}
	}
	if len(matching) != 1 || !frozenChecksumPattern.MatchString(matching[0].PlatformDigest) {
		return canonical, *step, *pin, types.PinnedReleaseArtifact{},
			errors.New("frozen step must resolve exactly one target-platform artifact digest")
	}
	return canonical, *step, *pin, matching[0], nil
}

func verifyFrozenArtifact(
	ctx context.Context,
	organizationID uuid.UUID,
	pin types.ComponentReleasePin,
	artifact types.PinnedReleaseArtifact,
) error {
	var count int
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT count(*)
		FROM ComponentReleaseArtifact
		WHERE organization_id = @organizationID
		  AND release_bundle_id = @componentReleaseID
		  AND component_key = @componentKey
		  AND component_version = @componentVersion
		  AND artifact_key = @artifactKey
		  AND platform = @platform
		  AND platform_digest = @platformDigest
	`, pgx.NamedArgs{
		"organizationID": organizationID, "componentReleaseID": pin.ComponentReleaseID,
		"componentKey": pin.ComponentKey, "componentVersion": pin.Version,
		"artifactKey": artifact.Key, "platform": artifact.Platform,
		"platformDigest": artifact.PlatformDigest,
	}).Scan(&count)
	if err != nil {
		return fmt.Errorf("verify ComponentReleaseArtifact platform_digest: %w", err)
	}
	if count != 1 {
		return errors.New("frozen component artifact does not match exactly one persisted platform digest")
	}
	return nil
}

func loadFrozenAdapterEvidence(
	ctx context.Context,
	organizationID, planID uuid.UUID,
	stepKey string,
) (frozenAdapterEvidence, error) {
	var value frozenAdapterEvidence
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT
			frozen.adapter_assignment_id,
			frozen.adapter_implementation_id,
			frozen.implementation_version,
			frozen.capability,
			frozen.capability_version,
			frozen.scope_type,
			frozen.scope_reference,
			frozen.config_snapshot_id,
			frozen.config_checksum,
			frozen.key_id,
			frozen.public_key_fingerprint,
			frozen.signing_key_reference,
			frozen.signing_key_version_fingerprint,
			COALESCE(cancel_capability.version, ''),
			COALESCE(retry_safe_capability.version, ''),
			plan_step.timeout_seconds
		FROM DeploymentPlanStepAdapter frozen
		JOIN DeploymentPlanStep plan_step
		  ON plan_step.id = frozen.deployment_plan_step_id
		 AND plan_step.deployment_plan_id = frozen.deployment_plan_id
		 AND plan_step.organization_id = frozen.organization_id
		JOIN AdapterAssignment assignment
		  ON assignment.id = frozen.adapter_assignment_id
		 AND assignment.organization_id = frozen.organization_id
		JOIN AdapterImplementation implementation
		  ON implementation.id = frozen.adapter_implementation_id
		 AND implementation.organization_id = frozen.organization_id
		JOIN AdapterCapability capability
		  ON capability.adapter_implementation_id = implementation.id
		 AND capability.organization_id = implementation.organization_id
		 AND capability.capability = frozen.capability
		 AND capability.version = frozen.capability_version
		LEFT JOIN AdapterCapability cancel_capability
		  ON cancel_capability.adapter_implementation_id = implementation.id
		 AND cancel_capability.organization_id = implementation.organization_id
		 AND cancel_capability.capability = @cancelCapability
		 AND cancel_capability.version = frozen.capability_version
		LEFT JOIN AdapterCapability retry_safe_capability
		  ON retry_safe_capability.adapter_implementation_id = implementation.id
		 AND retry_safe_capability.organization_id = implementation.organization_id
		 AND retry_safe_capability.capability = @retrySafeCapability
		 AND retry_safe_capability.version = frozen.capability_version
		JOIN TargetConfigSnapshot config
		  ON config.id = frozen.config_snapshot_id
		 AND config.organization_id = frozen.organization_id
		WHERE frozen.organization_id = @organizationID
		  AND frozen.deployment_plan_id = @planID
		  AND frozen.step_key = @stepKey
		  AND assignment.enabled
		  AND implementation.enabled
		  AND assignment.adapter_implementation_id = frozen.adapter_implementation_id
		  AND implementation.version = frozen.implementation_version
		  AND assignment.scope_type = frozen.scope_type
		  AND assignment.scope_reference = frozen.scope_reference
		  AND assignment.config_snapshot_id = frozen.config_snapshot_id
		  AND assignment.config_checksum = frozen.config_checksum
		  AND config.canonical_checksum = frozen.config_checksum
		  AND assignment.key_id = frozen.key_id
		  AND assignment.public_key_fingerprint = frozen.public_key_fingerprint
		  AND assignment.signing_key_reference = frozen.signing_key_reference
		  AND assignment.signing_key_version_fingerprint = frozen.signing_key_version_fingerprint
	`, pgx.NamedArgs{
		"organizationID": organizationID, "planID": planID,
		"stepKey": strings.TrimSpace(stepKey), "cancelCapability": adapterCancelCapability,
		"retrySafeCapability": adapterRetrySafeCapability,
	}).Scan(
		&value.AssignmentID, &value.ImplementationID, &value.ImplementationVersion,
		&value.Capability, &value.CapabilityVersion, &value.ScopeType,
		&value.ScopeReference, &value.ConfigSnapshotID, &value.ConfigChecksum,
		&value.KeyID, &value.PublicKeyFingerprint, &value.SigningKeyReference,
		&value.SigningKeyVersionFingerprint, &value.CancelCapabilityVersion,
		&value.RetrySafeCapabilityVersion, &value.TimeoutSeconds,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return value, errors.New("frozen adapter is missing or its runtime state has drifted")
	}
	if err != nil {
		return value, fmt.Errorf("read frozen adapter evidence: %w", err)
	}
	return value, nil
}

func deriveFrozenAttemptInputs(
	plan types.DeploymentPlan,
	step types.TargetPlanStep,
	artifactDigest string,
	adapter frozenAdapterEvidence,
	resourceKey string,
	latest *types.ExecutionAttempt,
	retry bool,
) (FrozenAttemptInputs, error) {
	if retry && latest == nil {
		return FrozenAttemptInputs{}, errors.New("execution retry requires a prior attempt")
	}
	if adapter.TimeoutSeconds != step.TimeoutSeconds {
		return FrozenAttemptInputs{}, errors.New("canonical and persisted frozen step timeouts do not match")
	}
	intentTTL := time.Duration(adapter.TimeoutSeconds) * time.Second
	if intentTTL <= 0 || intentTTL > executionprotocolMaxIntentTTL {
		return FrozenAttemptInputs{}, errors.New("frozen step timeout must be between one second and fifteen minutes")
	}
	adapterRevision, err := frozenAdapterRevision(adapter)
	if err != nil {
		return FrozenAttemptInputs{}, err
	}
	cancellable := strings.EqualFold(step.CancellationBehavior, "cooperative") &&
		adapter.CancelCapabilityVersion == adapter.CapabilityVersion
	retrySafe := (strings.EqualFold(step.RetryClass, "safe") ||
		strings.EqualFold(step.RetryClass, "bounded")) &&
		adapter.RetrySafeCapabilityVersion == adapter.CapabilityVersion
	if latest != nil && !retry {
		if latest.PlanChecksum != plan.CanonicalChecksum ||
			latest.ArtifactDigest != artifactDigest ||
			latest.ConfigChecksum != adapter.ConfigChecksum ||
			latest.AdapterRevision != adapterRevision ||
			latest.Fence.ResourceKey != resourceKey ||
			latest.IntentExpiresAt.Sub(latest.IntentIssuedAt) != intentTTL ||
			latest.Cancellable != cancellable ||
			latest.RetrySafe != retrySafe {
			return FrozenAttemptInputs{}, errors.New("existing execution attempt does not match frozen plan evidence")
		}
		return FrozenAttemptInputs{
			AttemptNumber: latest.Identity.AttemptNumber, PlanChecksum: latest.PlanChecksum,
			ArtifactDigest: latest.ArtifactDigest, ConfigChecksum: latest.ConfigChecksum,
			AdapterRevision: latest.AdapterRevision, ResourceKey: latest.Fence.ResourceKey,
			FenceGeneration: latest.Fence.Generation, Cancellable: latest.Cancellable,
			RetrySafe: latest.RetrySafe, IntentTTL: latest.IntentExpiresAt.Sub(latest.IntentIssuedAt),
			PublicKeyFingerprint:         adapter.PublicKeyFingerprint,
			SigningKeyReference:          adapter.SigningKeyReference,
			SigningKeyVersionFingerprint: adapter.SigningKeyVersionFingerprint,
		}, nil
	}
	if retry && (!latest.RetrySafe || !retrySafe) {
		return FrozenAttemptInputs{}, errors.New("execution retry requires a retry-safe prior attempt")
	}
	attemptNumber := 1
	fenceGeneration := int64(1)
	if latest != nil {
		attemptNumber = latest.Identity.AttemptNumber + 1
		fenceGeneration = latest.Fence.Generation + 1
	}
	return FrozenAttemptInputs{
		AttemptNumber: attemptNumber, PlanChecksum: plan.CanonicalChecksum,
		ArtifactDigest: artifactDigest, ConfigChecksum: adapter.ConfigChecksum,
		AdapterRevision: adapterRevision, ResourceKey: resourceKey,
		FenceGeneration: fenceGeneration,
		Cancellable:     cancellable,
		RetrySafe:       retrySafe,
		IntentTTL:       intentTTL, PublicKeyFingerprint: adapter.PublicKeyFingerprint,
		SigningKeyReference:          adapter.SigningKeyReference,
		SigningKeyVersionFingerprint: adapter.SigningKeyVersionFingerprint,
	}, nil
}

const executionprotocolMaxIntentTTL = time.Minute * 15

func frozenAdapterRevision(value frozenAdapterEvidence) (string, error) {
	payload, err := json.Marshal(struct {
		AssignmentID                 uuid.UUID `json:"assignmentId"`
		ImplementationID             uuid.UUID `json:"implementationId"`
		ImplementationVersion        string    `json:"implementationVersion"`
		Capability                   string    `json:"capability"`
		CapabilityVersion            string    `json:"capabilityVersion"`
		ScopeType                    string    `json:"scopeType"`
		ScopeReference               string    `json:"scopeReference"`
		ConfigSnapshotID             uuid.UUID `json:"configSnapshotId"`
		ConfigChecksum               string    `json:"configChecksum"`
		KeyID                        string    `json:"keyId"`
		PublicKeyFingerprint         string    `json:"publicKeyFingerprint"`
		SigningKeyReference          string    `json:"signingKeyReference"`
		SigningKeyVersionFingerprint string    `json:"signingKeyVersionFingerprint"`
		CancelCapabilityVersion      string    `json:"cancelCapabilityVersion"`
		RetrySafeCapabilityVersion   string    `json:"retrySafeCapabilityVersion"`
	}{
		AssignmentID: value.AssignmentID, ImplementationID: value.ImplementationID,
		ImplementationVersion: value.ImplementationVersion,
		Capability:            value.Capability, CapabilityVersion: value.CapabilityVersion,
		ScopeType: value.ScopeType, ScopeReference: value.ScopeReference,
		ConfigSnapshotID: value.ConfigSnapshotID, ConfigChecksum: value.ConfigChecksum,
		KeyID: value.KeyID, PublicKeyFingerprint: value.PublicKeyFingerprint,
		SigningKeyReference:          value.SigningKeyReference,
		SigningKeyVersionFingerprint: value.SigningKeyVersionFingerprint,
		CancelCapabilityVersion:      value.CancelCapabilityVersion,
		RetrySafeCapabilityVersion:   value.RetrySafeCapabilityVersion,
	})
	if err != nil {
		return "", fmt.Errorf("encode frozen adapter revision: %w", err)
	}
	return checksumForFrozenInput(string(payload)), nil
}

func checksumForFrozenInput(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}
