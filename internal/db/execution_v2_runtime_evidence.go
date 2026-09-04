package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/executionprotocol"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func RecordExecutionRuntimeEvidence(
	ctx context.Context,
	input types.ExecutionRuntimeEvidenceInput,
) (*types.ExecutionRuntimeEvidence, error) {
	input.ExecutorID = strings.TrimSpace(input.ExecutorID)
	input.CallerIdentity = strings.TrimSpace(input.CallerIdentity)
	input.Audience = strings.TrimSpace(input.Audience)
	input.EvidenceReference = strings.TrimSpace(input.EvidenceReference)
	input.CapturedAt = input.CapturedAt.UTC().Truncate(time.Microsecond)
	if err := validateExecutionRuntimeEvidenceInput(input); err != nil {
		return nil, err
	}
	var result *types.ExecutionRuntimeEvidence
	err := RunTx(ctx, func(ctx context.Context) error {
		attempt, err := getExecutionAttemptForTargetUpdate(
			ctx, input.AttemptID, input.OrganizationID, input.DeploymentTargetID,
		)
		if err != nil {
			return err
		}
		existing, err := getExecutionRuntimeEvidence(
			ctx, input.AttemptID, input.OrganizationID, input.DeploymentTargetID,
		)
		if err == nil {
			if !executionprotocol.IsExactRuntimeEvidenceReplay(*existing, input) {
				return apierrors.NewConflict("conflicting duplicate execution runtime evidence")
			}
			result = existing
			return nil
		}
		if !errors.Is(err, apierrors.ErrNotFound) {
			return err
		}
		if attempt.RuntimeContractVersion != types.ExecutionRuntimeContractVersionV3 &&
			attempt.RuntimeContractVersion != types.ExecutionRuntimeContractVersionV4 {
			return apierrors.NewConflict("execution attempt has no runtime trust contract")
		}
		if !runtimeEvidenceSchemaMatchesAttempt(input.SchemaVersion, attempt.RuntimeContractVersion) {
			return apierrors.NewConflict("execution runtime evidence schema does not match the runtime contract")
		}
		if attempt.Fence.Generation != input.FenceGeneration {
			return apierrors.NewConflict("stale execution runtime-evidence fence generation")
		}
		if attempt.ClaimedBy != input.ExecutorID ||
			(attempt.Status != types.ExecutionAttemptStatusClaimed &&
				attempt.Status != types.ExecutionAttemptStatusRunning) {
			return apierrors.NewConflict("execution runtime evidence rejected by executor ownership")
		}
		database := internalctx.GetDb(ctx)
		var trustedNow time.Time
		if err := database.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&trustedNow); err != nil {
			return fmt.Errorf("read trusted database time: %w", err)
		}
		if attempt.Fence.LeaseExpiresAt.IsZero() ||
			!attempt.Fence.LeaseExpiresAt.After(trustedNow) ||
			!attempt.IntentExpiresAt.After(trustedNow) {
			return apierrors.NewConflict("execution runtime evidence rejected by lease or intent expiry")
		}
		capturedAt := input.CapturedAt.UTC()
		if capturedAt.Before(attempt.IntentIssuedAt.UTC()) ||
			!capturedAt.Before(attempt.IntentExpiresAt.UTC()) ||
			capturedAt.After(trustedNow.UTC().Add(time.Minute)) {
			return apierrors.NewConflict("execution runtime evidence capture time is outside the intent window")
		}
		var retainedIntentChecksum string
		if err := database.QueryRow(ctx, `
			SELECT checksum
			FROM ExecutionIntent
			WHERE execution_attempt_id = @attemptId
				AND organization_id = @organizationId`, pgx.NamedArgs{
			"attemptId": input.AttemptID, "organizationId": input.OrganizationID,
		}).Scan(&retainedIntentChecksum); err != nil {
			return fmt.Errorf("read retained execution intent checksum: %w", err)
		}
		if input.IntentChecksum != retainedIntentChecksum ||
			input.CallerIdentity != attempt.CallerBinding || input.Audience != attempt.Audience ||
			input.ExpectedObservedStateVersion != attempt.ExpectedObservedStateVersion ||
			input.ExpectedObservedStateChecksum != attempt.ExpectedObservedStateChecksum ||
			input.PreExecutionImageDigest != attempt.ExpectedCurrentImageDigest ||
			input.PreExecutionConfigChecksum != attempt.ExpectedCurrentConfigChecksum ||
			input.PreExecutionServiceConfigChecksum != attempt.ExpectedCurrentServiceConfigChecksum ||
			input.ResultImageDigest != attempt.ArtifactDigest ||
			input.ResultConfigChecksum != attempt.ConfigChecksum ||
			input.ResultServiceConfigChecksum != attempt.DesiredServiceConfigChecksum ||
			input.Platform != attempt.ExpectedPlatform {
			return apierrors.NewConflict("execution runtime evidence does not match the signed intent")
		}
		canonicalChecksum, err := executionRuntimeEvidenceCanonicalChecksum(*attempt, input)
		if err != nil {
			return err
		}
		result, err = insertExecutionRuntimeEvidence(ctx, *attempt, input, canonicalChecksum)
		if err != nil {
			return err
		}
		return appendExecutionV2AttemptAudit(
			ctx, *attempt, "execution.runtime_evidence_recorded", string(input.HealthStatus), nil,
			map[string]any{
				"runtimeEvidenceId": result.ID, "runtimeEvidenceChecksum": result.CanonicalChecksum,
				"executorId": input.ExecutorID, "evidenceReference": input.EvidenceReference,
			},
		)
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			existing, readErr := getExecutionRuntimeEvidence(
				ctx, input.AttemptID, input.OrganizationID, input.DeploymentTargetID,
			)
			if readErr == nil && executionprotocol.IsExactRuntimeEvidenceReplay(*existing, input) {
				return existing, nil
			}
			return nil, apierrors.NewConflict("conflicting duplicate execution runtime evidence")
		}
	}
	return result, err
}

func executionRuntimeEvidenceCanonicalChecksum(
	attempt types.ExecutionAttempt,
	input types.ExecutionRuntimeEvidenceInput,
) (string, error) {
	payload, err := json.Marshal(struct {
		Schema                            string                         `json:"schema"`
		OrganizationID                    uuid.UUID                      `json:"organizationId"`
		DeploymentTargetID                uuid.UUID                      `json:"deploymentTargetId"`
		AttemptID                         uuid.UUID                      `json:"attemptId"`
		Identity                          types.ExecutionIdentity        `json:"identity"`
		EventIdentity                     uuid.UUID                      `json:"eventIdentity"`
		IntentChecksum                    string                         `json:"intentChecksum"`
		ExecutorID                        string                         `json:"executorId"`
		CallerIdentity                    string                         `json:"callerIdentity"`
		Audience                          string                         `json:"audience"`
		FenceGeneration                   int64                          `json:"fenceGeneration"`
		ExpectedObservedStateVersion      int64                          `json:"expectedObservedStateVersion"`
		ExpectedObservedStateChecksum     string                         `json:"expectedObservedStateChecksum"`
		PreExecutionImageDigest           string                         `json:"preExecutionImageDigest"`
		PreExecutionConfigChecksum        string                         `json:"preExecutionConfigChecksum"`
		PreExecutionServiceConfigChecksum string                         `json:"preExecutionServiceConfigChecksum,omitempty"`
		ResultImageDigest                 string                         `json:"resultImageDigest"`
		ResultConfigChecksum              string                         `json:"resultConfigChecksum"`
		ResultServiceConfigChecksum       string                         `json:"resultServiceConfigChecksum,omitempty"`
		Platform                          types.DeploymentTargetPlatform `json:"platform"`
		HealthStatus                      types.TargetComponentHealth    `json:"healthStatus"`
		ResultChecksum                    string                         `json:"resultChecksum"`
		EvidenceReference                 string                         `json:"evidenceReference"`
		EvidenceChecksum                  string                         `json:"evidenceChecksum"`
		CapturedAt                        string                         `json:"capturedAt"`
	}{
		Schema:         input.SchemaVersion,
		OrganizationID: input.OrganizationID, DeploymentTargetID: input.DeploymentTargetID,
		AttemptID: input.AttemptID, Identity: attempt.Identity, EventIdentity: input.EventIdentity,
		IntentChecksum: input.IntentChecksum, ExecutorID: input.ExecutorID,
		CallerIdentity: input.CallerIdentity, Audience: input.Audience,
		FenceGeneration:                   input.FenceGeneration,
		ExpectedObservedStateVersion:      input.ExpectedObservedStateVersion,
		ExpectedObservedStateChecksum:     input.ExpectedObservedStateChecksum,
		PreExecutionImageDigest:           input.PreExecutionImageDigest,
		PreExecutionConfigChecksum:        input.PreExecutionConfigChecksum,
		PreExecutionServiceConfigChecksum: input.PreExecutionServiceConfigChecksum,
		ResultImageDigest:                 input.ResultImageDigest, ResultConfigChecksum: input.ResultConfigChecksum,
		ResultServiceConfigChecksum: input.ResultServiceConfigChecksum,
		Platform:                    input.Platform, HealthStatus: input.HealthStatus,
		ResultChecksum: input.ResultChecksum, EvidenceReference: input.EvidenceReference,
		EvidenceChecksum: input.EvidenceChecksum,
		CapturedAt:       input.CapturedAt.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return "", fmt.Errorf("marshal execution runtime evidence: %w", err)
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func validateExecutionRuntimeEvidenceInput(input types.ExecutionRuntimeEvidenceInput) error {
	if input.OrganizationID == uuid.Nil || input.DeploymentTargetID == uuid.Nil ||
		input.AttemptID == uuid.Nil || input.EventIdentity == uuid.Nil ||
		strings.TrimSpace(input.ExecutorID) == "" || len(input.ExecutorID) > 128 ||
		strings.ContainsAny(input.ExecutorID, "\r\n") || input.FenceGeneration <= 0 ||
		input.ExpectedObservedStateVersion <= 0 || input.CapturedAt.IsZero() {
		return apierrors.NewBadRequest("execution runtime evidence identity is invalid")
	}
	if input.SchemaVersion != types.ExecutionRuntimeEvidenceSchemaV1 &&
		input.SchemaVersion != types.ExecutionRuntimeEvidenceSchemaV2 {
		return apierrors.NewBadRequest("execution runtime evidence schema is invalid")
	}
	caller, audience := strings.TrimSpace(input.CallerIdentity), strings.TrimSpace(input.Audience)
	if caller == "" || len(caller) > 512 || strings.ContainsAny(caller, "\r\n") ||
		audience == "" || len(audience) > 512 || strings.ContainsAny(audience, "\r\n") {
		return apierrors.NewBadRequest("execution runtime evidence caller or audience is invalid")
	}
	for _, checksum := range []string{
		input.IntentChecksum, input.ExpectedObservedStateChecksum,
		input.PreExecutionImageDigest, input.PreExecutionConfigChecksum,
		input.ResultImageDigest, input.ResultConfigChecksum,
		input.ResultChecksum, input.EvidenceChecksum,
	} {
		if !intentChecksumPatternDB.MatchString(checksum) {
			return apierrors.NewBadRequest("execution runtime evidence checksum is invalid")
		}
	}
	if input.SchemaVersion == types.ExecutionRuntimeEvidenceSchemaV1 {
		if input.PreExecutionServiceConfigChecksum != "" || input.ResultServiceConfigChecksum != "" {
			return apierrors.NewBadRequest("execution runtime evidence v1 service config checksums must be empty")
		}
	} else if !intentChecksumPatternDB.MatchString(input.PreExecutionServiceConfigChecksum) ||
		!intentChecksumPatternDB.MatchString(input.ResultServiceConfigChecksum) {
		return apierrors.NewBadRequest("execution runtime evidence service config checksum is invalid")
	}
	if !input.Platform.IsValid() ||
		(input.HealthStatus != types.TargetComponentHealthHealthy &&
			input.HealthStatus != types.TargetComponentHealthUnhealthy) {
		return apierrors.NewBadRequest("execution runtime evidence result is invalid")
	}
	if reference := strings.TrimSpace(input.EvidenceReference); reference == "" ||
		len(reference) > 2048 || strings.ContainsAny(reference, "\r\n") {
		return apierrors.NewBadRequest("execution runtime evidence reference is invalid")
	}
	return nil
}

func insertExecutionRuntimeEvidence(
	ctx context.Context,
	attempt types.ExecutionAttempt,
	input types.ExecutionRuntimeEvidenceInput,
	canonicalChecksum string,
) (*types.ExecutionRuntimeEvidence, error) {
	var preExecutionServiceConfigChecksum, resultServiceConfigChecksum any
	if input.SchemaVersion == types.ExecutionRuntimeEvidenceSchemaV2 {
		preExecutionServiceConfigChecksum = input.PreExecutionServiceConfigChecksum
		resultServiceConfigChecksum = input.ResultServiceConfigChecksum
	}
	row := internalctx.GetDb(ctx).QueryRow(ctx, `
		INSERT INTO ExecutionRuntimeEvidence (
			organization_id, deployment_target_id, execution_attempt_id,
			execution_id, attempt_number, step_key, event_identity, schema_version,
			intent_checksum, executor_id, caller_identity, audience, fence_generation,
			expected_observed_state_revision, expected_observed_state_checksum,
			pre_execution_image_digest, pre_execution_config_checksum,
			pre_execution_service_config_checksum,
			result_image_digest, result_config_checksum, result_service_config_checksum,
			platform, health_status,
			result_checksum, evidence_reference, evidence_checksum,
			canonical_checksum, captured_at
		) VALUES (
			@organizationId, @deploymentTargetId, @attemptId,
			@executionId, @attemptNumber, @stepKey, @eventIdentity, @schemaVersion,
			@intentChecksum, @executorId, @callerIdentity, @audience, @fenceGeneration,
			@expectedObservedStateVersion, @expectedObservedStateChecksum,
			@preExecutionImageDigest, @preExecutionConfigChecksum,
			@preExecutionServiceConfigChecksum,
			@resultImageDigest, @resultConfigChecksum, @resultServiceConfigChecksum,
			@platform, @healthStatus,
			@resultChecksum, @evidenceReference, @evidenceChecksum,
			@canonicalChecksum, @capturedAt
		)
		RETURNING id, created_at, organization_id, deployment_target_id,
			execution_attempt_id, execution_id, attempt_number, step_key,
			event_identity, schema_version, intent_checksum, executor_id,
			caller_identity, audience, fence_generation,
			expected_observed_state_revision, expected_observed_state_checksum,
			pre_execution_image_digest, pre_execution_config_checksum,
			pre_execution_service_config_checksum,
			result_image_digest, result_config_checksum, result_service_config_checksum,
			platform, health_status,
			result_checksum, evidence_reference, evidence_checksum,
			canonical_checksum, captured_at`, pgx.NamedArgs{
		"organizationId": input.OrganizationID, "deploymentTargetId": input.DeploymentTargetID,
		"attemptId": input.AttemptID, "executionId": attempt.Identity.ExecutionID,
		"attemptNumber": attempt.Identity.AttemptNumber, "stepKey": attempt.Identity.StepKey,
		"eventIdentity": input.EventIdentity, "schemaVersion": input.SchemaVersion,
		"intentChecksum": input.IntentChecksum, "executorId": input.ExecutorID,
		"callerIdentity": input.CallerIdentity, "audience": input.Audience,
		"fenceGeneration":                   input.FenceGeneration,
		"expectedObservedStateVersion":      input.ExpectedObservedStateVersion,
		"expectedObservedStateChecksum":     input.ExpectedObservedStateChecksum,
		"preExecutionImageDigest":           input.PreExecutionImageDigest,
		"preExecutionConfigChecksum":        input.PreExecutionConfigChecksum,
		"preExecutionServiceConfigChecksum": preExecutionServiceConfigChecksum,
		"resultImageDigest":                 input.ResultImageDigest,
		"resultConfigChecksum":              input.ResultConfigChecksum,
		"resultServiceConfigChecksum":       resultServiceConfigChecksum,
		"platform":                          input.Platform, "healthStatus": input.HealthStatus,
		"resultChecksum": input.ResultChecksum, "evidenceReference": input.EvidenceReference,
		"evidenceChecksum": input.EvidenceChecksum, "canonicalChecksum": canonicalChecksum,
		"capturedAt": input.CapturedAt.UTC(),
	})
	evidence, err := scanExecutionRuntimeEvidence(row)
	if err != nil {
		return nil, fmt.Errorf("insert ExecutionRuntimeEvidence: %w", err)
	}
	return evidence, nil
}

func scanExecutionRuntimeEvidence(row rowScanner) (*types.ExecutionRuntimeEvidence, error) {
	var evidence types.ExecutionRuntimeEvidence
	var executionID uuid.UUID
	var attemptNumber int
	var stepKey string
	var preExecutionServiceConfigChecksum, resultServiceConfigChecksum *string
	if err := row.Scan(
		&evidence.ID, &evidence.CreatedAt, &evidence.OrganizationID,
		&evidence.DeploymentTargetID, &evidence.AttemptID, &executionID,
		&attemptNumber, &stepKey, &evidence.EventIdentity, &evidence.SchemaVersion,
		&evidence.IntentChecksum, &evidence.ExecutorID, &evidence.CallerIdentity,
		&evidence.Audience, &evidence.FenceGeneration,
		&evidence.ExpectedObservedStateVersion, &evidence.ExpectedObservedStateChecksum,
		&evidence.PreExecutionImageDigest, &evidence.PreExecutionConfigChecksum,
		&preExecutionServiceConfigChecksum,
		&evidence.ResultImageDigest, &evidence.ResultConfigChecksum,
		&resultServiceConfigChecksum,
		&evidence.Platform, &evidence.HealthStatus, &evidence.ResultChecksum,
		&evidence.EvidenceReference, &evidence.EvidenceChecksum,
		&evidence.CanonicalChecksum, &evidence.CapturedAt,
	); err != nil {
		return nil, err
	}
	evidence.Identity = types.ExecutionIdentity{
		ExecutionID: executionID, AttemptNumber: attemptNumber, StepKey: stepKey,
	}
	if preExecutionServiceConfigChecksum != nil {
		evidence.PreExecutionServiceConfigChecksum = *preExecutionServiceConfigChecksum
	}
	if resultServiceConfigChecksum != nil {
		evidence.ResultServiceConfigChecksum = *resultServiceConfigChecksum
	}
	return &evidence, nil
}

func getExecutionRuntimeEvidence(
	ctx context.Context,
	attemptID, organizationID, deploymentTargetID uuid.UUID,
) (*types.ExecutionRuntimeEvidence, error) {
	evidence, err := scanExecutionRuntimeEvidence(internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT id, created_at, organization_id, deployment_target_id,
			execution_attempt_id, execution_id, attempt_number, step_key,
			event_identity, schema_version, intent_checksum, executor_id,
			caller_identity, audience, fence_generation,
			expected_observed_state_revision, expected_observed_state_checksum,
			pre_execution_image_digest, pre_execution_config_checksum,
			pre_execution_service_config_checksum,
			result_image_digest, result_config_checksum, result_service_config_checksum,
			platform, health_status,
			result_checksum, evidence_reference, evidence_checksum,
			canonical_checksum, captured_at
		FROM ExecutionRuntimeEvidence
		WHERE execution_attempt_id = @attemptId
			AND organization_id = @organizationId
			AND deployment_target_id = @deploymentTargetId`, pgx.NamedArgs{
		"attemptId": attemptID, "organizationId": organizationID,
		"deploymentTargetId": deploymentTargetID,
	}))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get ExecutionRuntimeEvidence: %w", err)
	}
	return evidence, nil
}

func validateSuccessfulCompletionRuntimeEvidence(
	ctx context.Context,
	attempt types.ExecutionAttempt,
	input types.CompletionInput,
) error {
	if input.Status != types.ExecutionAttemptStatusSucceeded {
		return nil
	}
	evidence, err := getExecutionRuntimeEvidence(
		ctx, attempt.ID, attempt.OrganizationID, attempt.DeploymentTargetID,
	)
	if errors.Is(err, apierrors.ErrNotFound) {
		return apierrors.NewConflict("successful execution completion has no runtime evidence")
	}
	if err != nil {
		return err
	}
	if evidence.ID != input.RuntimeEvidenceID ||
		evidence.CanonicalChecksum != input.RuntimeEvidenceChecksum ||
		evidence.ExecutorID != input.ExecutorID ||
		evidence.FenceGeneration != input.FenceGeneration ||
		evidence.CallerIdentity != attempt.CallerBinding ||
		evidence.Audience != attempt.Audience ||
		evidence.ExpectedObservedStateVersion != attempt.ExpectedObservedStateVersion ||
		evidence.ExpectedObservedStateChecksum != attempt.ExpectedObservedStateChecksum ||
		evidence.PreExecutionImageDigest != attempt.ExpectedCurrentImageDigest ||
		evidence.PreExecutionConfigChecksum != attempt.ExpectedCurrentConfigChecksum ||
		evidence.PreExecutionServiceConfigChecksum != attempt.ExpectedCurrentServiceConfigChecksum ||
		evidence.ResultImageDigest != attempt.ArtifactDigest ||
		evidence.ResultConfigChecksum != attempt.ConfigChecksum ||
		evidence.ResultServiceConfigChecksum != attempt.DesiredServiceConfigChecksum ||
		evidence.Platform != attempt.ExpectedPlatform ||
		evidence.HealthStatus != types.TargetComponentHealthHealthy {
		return apierrors.NewConflict("successful execution completion runtime evidence is invalid")
	}
	return nil
}

func runtimeEvidenceSchemaMatchesAttempt(
	schema string,
	version types.ExecutionRuntimeContractVersion,
) bool {
	return (schema == types.ExecutionRuntimeEvidenceSchemaV1 &&
		version == types.ExecutionRuntimeContractVersionV3) ||
		(schema == types.ExecutionRuntimeEvidenceSchemaV2 &&
			version == types.ExecutionRuntimeContractVersionV4)
}
