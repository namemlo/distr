package executionworker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

var frozenChecksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

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
// the immutable plan/task/preflight snapshots already persisted by Distr.
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
	return evaluatePersistedAdmission(*task, *plan, request), nil
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
	bundle, err := db.GetReleaseBundle(ctx, task.ReleaseBundleID, request.OrganizationID)
	if err != nil {
		return FrozenAttemptInputs{}, err
	}
	latest, err := db.GetLatestExecutionAttemptForTaskStep(
		ctx, request.OrganizationID, request.TaskID, request.StepRunID,
	)
	if errors.Is(err, apierrors.ErrNotFound) {
		latest = nil
	} else if err != nil {
		return FrozenAttemptInputs{}, err
	}
	return deriveFrozenAttemptInputs(*task, *plan, *bundle, request.StepKey, latest, request.Retry)
}

func evaluatePersistedAdmission(
	task types.Task,
	plan types.DeploymentPlan,
	request AdmissionRequest,
) AdmissionDecision {
	scopeMatches := task.ID == request.TaskID && task.OrganizationID == request.OrganizationID &&
		task.DeploymentTargetID == request.DeploymentTargetID &&
		task.DeploymentPlanID == request.PlanID && task.EnvironmentID == request.EnvironmentID
	if request.StepRunID != uuid.Nil {
		scopeMatches = scopeMatches && slices.ContainsFunc(task.StepRuns, func(step types.StepRun) bool {
			return step.ID == request.StepRunID && step.StepKey == strings.TrimSpace(request.StepKey)
		})
	}
	planFrozen := plan.ID == request.PlanID && plan.OrganizationID == request.OrganizationID &&
		plan.EnvironmentID == request.EnvironmentID && plan.Status == types.DeploymentPlanStatusExecuted &&
		plan.ProtocolVersion == string(types.ExecutionProtocolVersionV2) &&
		task.ProtocolVersion == types.ExecutionProtocolVersionV2
	stepFrozen := slices.ContainsFunc(plan.Steps, func(step types.DeploymentPlanStep) bool {
		return step.Included && step.StepKey == strings.TrimSpace(request.StepKey) &&
			strings.TrimSpace(step.ActionType) != ""
	})
	preflightPassed := false
	for _, run := range plan.PreflightRuns {
		if run.Status != types.DeploymentPreflightStatusPassed {
			continue
		}
		matchedTask := false
		allPassed := true
		for _, check := range run.Checks {
			if check.TaskID != nil && *check.TaskID == task.ID {
				matchedTask = true
				allPassed = allPassed && check.Status == types.DeploymentPreflightCheckStatusPassed
			}
		}
		if matchedTask && allPassed {
			preflightPassed = true
			break
		}
	}
	return AdmissionDecision{
		ScopedEnrollment: scopeMatches,
		PlanApproved:     planFrozen,
		PlanAdmitted:     planFrozen && task.Status == types.TaskStatusQueued,
		AdapterPreflight: planFrozen && stepFrozen && preflightPassed,
	}
}

func deriveFrozenAttemptInputs(
	task types.Task,
	plan types.DeploymentPlan,
	bundle types.ReleaseBundle,
	stepKey string,
	latest *types.ExecutionAttempt,
	retry bool,
) (FrozenAttemptInputs, error) {
	if latest != nil && !retry {
		return FrozenAttemptInputs{
			AttemptNumber: latest.Identity.AttemptNumber, PlanChecksum: latest.PlanChecksum,
			ArtifactDigest: latest.ArtifactDigest, ConfigChecksum: latest.ConfigChecksum,
			AdapterRevision: latest.AdapterRevision, ResourceKey: latest.Fence.ResourceKey,
			FenceGeneration: latest.Fence.Generation, Cancellable: latest.Cancellable,
			RetrySafe: latest.RetrySafe, IntentTTL: latest.IntentExpiresAt.Sub(latest.IntentIssuedAt),
		}, nil
	}
	if retry && latest == nil {
		return FrozenAttemptInputs{}, errors.New("execution retry requires a prior attempt")
	}
	var step *types.DeploymentPlanStep
	for i := range plan.Steps {
		if plan.Steps[i].Included && plan.Steps[i].StepKey == strings.TrimSpace(stepKey) {
			step = &plan.Steps[i]
			break
		}
	}
	if step == nil {
		return FrozenAttemptInputs{}, errors.New("frozen deployment plan step was not found")
	}
	configParts := make([]string, 0)
	for _, component := range plan.TargetComponents {
		if component.DeploymentTargetID == task.DeploymentTargetID {
			configParts = append(configParts, component.Component+"\x00"+component.ConfigChecksum)
		}
	}
	sort.Strings(configParts)
	if len(configParts) == 0 {
		return FrozenAttemptInputs{}, errors.New("frozen target config snapshot was not found")
	}
	resourceKey, err := db.CanonicalExecutionFenceResourceKey(task.Locks)
	if err != nil {
		return FrozenAttemptInputs{}, err
	}
	attemptNumber := 1
	fenceGeneration := int64(1)
	if latest != nil {
		attemptNumber = latest.Identity.AttemptNumber + 1
		fenceGeneration = latest.Fence.Generation + 1
	}
	adapterRevision := strings.TrimSpace(step.ActionType)
	if name := strings.TrimSpace(step.ActionName); name != "" {
		adapterRevision += ":" + name
	}
	if !frozenChecksumPattern.MatchString(plan.CanonicalChecksum) ||
		!frozenChecksumPattern.MatchString(bundle.CanonicalChecksum) {
		return FrozenAttemptInputs{}, errors.New("frozen plan or release checksum is invalid")
	}
	return FrozenAttemptInputs{
		AttemptNumber: attemptNumber, PlanChecksum: plan.CanonicalChecksum,
		ArtifactDigest:  bundle.CanonicalChecksum,
		ConfigChecksum:  checksumForFrozenInput(strings.Join(configParts, "\n")),
		AdapterRevision: adapterRevision, ResourceKey: resourceKey,
		FenceGeneration: fenceGeneration, Cancellable: true,
		RetrySafe: step.RetryMaxAttempts > 1, IntentTTL: 5 * time.Minute,
	}, nil
}

func checksumForFrozenInput(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}
