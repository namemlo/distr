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
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const admissionGateEvidencePlanSQL = `
	SELECT
	  dp.organization_id,
	  dp.id,
	  1::bigint,
	  dp.canonical_checksum,
	  dp.effective_policy_checksum,
	  dp.effective_policy
	FROM DeploymentPlan dp
	WHERE dp.organization_id = @organizationID
	  AND dp.id = @deploymentPlanID
`

const admissionGateEvidencePreflightSQL = `
	SELECT
	  dpr.id,
	  dpr.created_at,
	  dpr.organization_id,
	  dpr.deployment_plan_id,
	  dpr.plan_checksum,
	  dpr.status
	FROM DeploymentPreflightRun dpr
	WHERE dpr.organization_id = @organizationID
	  AND dpr.deployment_plan_id = @deploymentPlanID
	  AND dpr.created_at <= @evaluatedAt
	ORDER BY dpr.created_at DESC, dpr.id DESC
	LIMIT 1
`

const admissionGateEvidenceChecksSQL = `
	SELECT
	  dpc.id,
	  dpc.created_at,
	  dpc.organization_id,
	  dpc.deployment_preflight_run_id,
	  dpc.deployment_plan_id,
	  dpc.check_key,
	  dpc.status,
	  dpc.expected,
	  dpc.actual
	FROM DeploymentPreflightCheck dpc
	WHERE dpc.organization_id = @organizationID
	  AND dpc.deployment_plan_id = @deploymentPlanID
	  AND dpc.deployment_preflight_run_id = @preflightRunID
	  AND dpc.created_at <= @evaluatedAt
	ORDER BY dpc.check_key, dpc.id
`

type admissionGateEvidencePlanRecord struct {
	OrganizationID          uuid.UUID
	DeploymentPlanID        uuid.UUID
	PlanRevision            int64
	PlanChecksum            string
	EffectivePolicyChecksum string
	EffectivePolicy         types.EffectivePolicy
}

type admissionGateEvidenceCheckRecord struct {
	ID                       uuid.UUID                            `db:"id"`
	CreatedAt                time.Time                            `db:"created_at"`
	OrganizationID           uuid.UUID                            `db:"organization_id"`
	DeploymentPreflightRunID uuid.UUID                            `db:"deployment_preflight_run_id"`
	DeploymentPlanID         uuid.UUID                            `db:"deployment_plan_id"`
	CheckKey                 string                               `db:"check_key"`
	Status                   types.DeploymentPreflightCheckStatus `db:"status"`
	Expected                 json.RawMessage                      `db:"expected"`
	Actual                   json.RawMessage                      `db:"actual"`
}

type admissionGateEvidencePreflightRecord struct {
	ID               uuid.UUID
	CreatedAt        time.Time
	OrganizationID   uuid.UUID
	DeploymentPlanID uuid.UUID
	PlanChecksum     string
	Status           types.DeploymentPreflightStatus
	Checks           []admissionGateEvidenceCheckRecord
}

type admissionGateEvidenceSource interface {
	LoadAdmissionGateEvidencePlan(
		context.Context,
		admissionGateEvidenceContext,
	) (admissionGateEvidencePlanRecord, error)
	LoadAdmissionGateEvidencePreflight(
		context.Context,
		admissionGateEvidenceContext,
	) (admissionGateEvidencePreflightRecord, error)
}

type databaseAdmissionGateEvidenceSource struct{}

type databaseAdmissionGateEvidencePreparer struct{}

func (databaseAdmissionGateEvidenceSource) LoadAdmissionGateEvidencePlan(
	ctx context.Context,
	evidenceContext admissionGateEvidenceContext,
) (admissionGateEvidencePlanRecord, error) {
	var record admissionGateEvidencePlanRecord
	var effectivePolicyJSON []byte
	err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		admissionGateEvidencePlanSQL,
		pgx.NamedArgs{
			"organizationID":   evidenceContext.OrganizationID,
			"deploymentPlanID": evidenceContext.DeploymentPlanID,
		},
	).Scan(
		&record.OrganizationID,
		&record.DeploymentPlanID,
		&record.PlanRevision,
		&record.PlanChecksum,
		&record.EffectivePolicyChecksum,
		&effectivePolicyJSON,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return record, apierrors.ErrNotFound
	}
	if err != nil {
		return record, fmt.Errorf("load trusted admission plan evidence: %w", err)
	}
	if len(effectivePolicyJSON) == 0 || string(effectivePolicyJSON) == "null" {
		return record, apierrors.NewConflict("trusted admission policy evidence is missing")
	}
	if err := json.Unmarshal(effectivePolicyJSON, &record.EffectivePolicy); err != nil {
		return record, fmt.Errorf("decode trusted admission policy evidence: %w", err)
	}
	return record, nil
}

func (databaseAdmissionGateEvidenceSource) LoadAdmissionGateEvidencePreflight(
	ctx context.Context,
	evidenceContext admissionGateEvidenceContext,
) (admissionGateEvidencePreflightRecord, error) {
	var record admissionGateEvidencePreflightRecord
	err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		admissionGateEvidencePreflightSQL,
		pgx.NamedArgs{
			"organizationID":   evidenceContext.OrganizationID,
			"deploymentPlanID": evidenceContext.DeploymentPlanID,
			"evaluatedAt":      evidenceContext.EvaluatedAt.UTC(),
		},
	).Scan(
		&record.ID,
		&record.CreatedAt,
		&record.OrganizationID,
		&record.DeploymentPlanID,
		&record.PlanChecksum,
		&record.Status,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return record, apierrors.ErrNotFound
	}
	if err != nil {
		return record, fmt.Errorf("load trusted admission preflight evidence: %w", err)
	}
	rows, err := internalctx.GetDb(ctx).Query(
		ctx,
		admissionGateEvidenceChecksSQL,
		pgx.NamedArgs{
			"organizationID":   evidenceContext.OrganizationID,
			"deploymentPlanID": evidenceContext.DeploymentPlanID,
			"preflightRunID":   record.ID,
			"evaluatedAt":      evidenceContext.EvaluatedAt.UTC(),
		},
	)
	if err != nil {
		return record, fmt.Errorf("query trusted admission preflight checks: %w", err)
	}
	record.Checks, err = pgx.CollectRows(
		rows,
		pgx.RowToStructByName[admissionGateEvidenceCheckRecord],
	)
	if err != nil {
		return record, fmt.Errorf("collect trusted admission preflight checks: %w", err)
	}
	return record, nil
}

type persistedAdmissionGateEvidenceRepository struct {
	source   admissionGateEvidenceSource
	preparer admissionGateEvidencePreparer
}

func (repository persistedAdmissionGateEvidenceRepository) PrepareAdmissionGateEvidence(
	ctx context.Context,
	evidenceContext admissionGateEvidenceContext,
	actorUserAccountID uuid.UUID,
) error {
	if repository.preparer == nil {
		return apierrors.NewConflict("trusted gate evidence preparer is unavailable")
	}
	return repository.preparer.PrepareAdmissionGateEvidence(
		ctx,
		evidenceContext,
		actorUserAccountID,
	)
}

func (databaseAdmissionGateEvidencePreparer) PrepareAdmissionGateEvidence(
	ctx context.Context,
	evidenceContext admissionGateEvidenceContext,
	actorUserAccountID uuid.UUID,
) error {
	plan, err := GetDeploymentPlan(
		ctx,
		evidenceContext.DeploymentPlanID,
		evidenceContext.OrganizationID,
	)
	if err != nil {
		return err
	}
	if plan.EffectivePolicy == nil {
		return apierrors.NewConflict("trusted admission policy evidence is missing")
	}
	if err := validateAdmissionGateEvidencePlan(admissionGateEvidencePlanRecord{
		OrganizationID:          plan.OrganizationID,
		DeploymentPlanID:        plan.ID,
		PlanRevision:            1,
		PlanChecksum:            plan.CanonicalChecksum,
		EffectivePolicyChecksum: plan.EffectivePolicyChecksum,
		EffectivePolicy:         *plan.EffectivePolicy,
	}, evidenceContext); err != nil {
		return err
	}
	requiredEvidence, err := normalizedRequiredAdmissionEvidence(
		plan.EffectivePolicy.RequiredEvidence,
	)
	if err != nil {
		return err
	}
	if len(requiredEvidence) == 0 {
		return nil
	}
	_, _, err = evaluateAndPersistDeploymentPreflight(ctx, *plan, actorUserAccountID)
	return err
}

type admissionEvidenceCheckMapping struct {
	exactKey  string
	keyPrefix string
}

var trustedAdmissionEvidenceCheckMappings = map[string]admissionEvidenceCheckMapping{
	string(types.AdmissionGateIntegrity):  {exactKey: "plan_checksum"},
	string(types.AdmissionGateBackup):     {keyPrefix: "migration_backup:"},
	string(types.AdmissionGateProvenance): {exactKey: "release_provenance"},
	"sbom":                                {exactKey: "release_sbom"},
}

func (repository persistedAdmissionGateEvidenceRepository) ResolveAdmissionGateEvidence(
	ctx context.Context,
	evidenceContext admissionGateEvidenceContext,
) ([]types.AdmissionGateEvidence, error) {
	if evidenceContext.OrganizationID == uuid.Nil ||
		evidenceContext.DeploymentPlanID == uuid.Nil ||
		evidenceContext.PlanRevision < 1 ||
		evidenceContext.EvaluatedAt.IsZero() ||
		!trustedAdmissionChecksumValid(evidenceContext.PlanChecksum) ||
		!trustedAdmissionChecksumValid(evidenceContext.EffectivePolicyChecksum) {
		return nil, apierrors.NewConflict("trusted admission gate evidence context is invalid")
	}
	if repository.source == nil {
		return nil, apierrors.NewConflict("trusted gate evidence source is unavailable")
	}
	plan, err := repository.source.LoadAdmissionGateEvidencePlan(ctx, evidenceContext)
	if errors.Is(err, apierrors.ErrNotFound) {
		return nil, apierrors.NewConflict("trusted admission plan evidence is missing")
	}
	if err != nil {
		return nil, err
	}
	if err := validateAdmissionGateEvidencePlan(plan, evidenceContext); err != nil {
		return nil, err
	}
	requiredEvidence, err := normalizedRequiredAdmissionEvidence(plan.EffectivePolicy.RequiredEvidence)
	if err != nil {
		return nil, err
	}
	if len(requiredEvidence) == 0 {
		return []types.AdmissionGateEvidence{}, nil
	}
	preflight, err := repository.source.LoadAdmissionGateEvidencePreflight(ctx, evidenceContext)
	if errors.Is(err, apierrors.ErrNotFound) {
		return nil, apierrors.NewConflict("required trusted admission preflight evidence is missing")
	}
	if err != nil {
		return nil, err
	}
	if err := validateAdmissionGateEvidencePreflight(preflight, evidenceContext); err != nil {
		return nil, err
	}
	result := make([]types.AdmissionGateEvidence, 0, len(requiredEvidence))
	for _, required := range requiredEvidence {
		mapping, supported := trustedAdmissionEvidenceCheckMappings[required]
		if !supported {
			return nil, apierrors.NewConflict(
				fmt.Sprintf("required trusted admission evidence %q is unsupported", required),
			)
		}
		checks := admissionChecksForRequiredEvidence(mapping, preflight.Checks)
		if len(checks) == 0 {
			return nil, apierrors.NewConflict(
				fmt.Sprintf("required trusted admission evidence %q is missing", required),
			)
		}
		for _, check := range checks {
			if check.Status != types.DeploymentPreflightCheckStatusPassed {
				return nil, apierrors.NewConflict(
					fmt.Sprintf("required trusted admission evidence %q did not pass", required),
				)
			}
			if !admissionEvidenceCheckAuthoritative(required, check, evidenceContext) {
				return nil, apierrors.NewConflict(
					fmt.Sprintf("required trusted admission evidence %q is invalid", required),
				)
			}
		}
		checksum, err := admissionGateEvidenceChecksum(evidenceContext, required, preflight, checks)
		if err != nil {
			return nil, err
		}
		result = append(result, types.AdmissionGateEvidence{
			Key:       types.AdmissionGateKey(required),
			Mandatory: true,
			Satisfied: true,
			Checksum:  checksum,
		})
	}
	return result, nil
}

func trustedAdmissionChecksumValid(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}

func validateAdmissionGateEvidencePlan(
	plan admissionGateEvidencePlanRecord,
	evidenceContext admissionGateEvidenceContext,
) error {
	if plan.OrganizationID != evidenceContext.OrganizationID ||
		plan.DeploymentPlanID != evidenceContext.DeploymentPlanID ||
		plan.PlanRevision != evidenceContext.PlanRevision ||
		plan.PlanChecksum != evidenceContext.PlanChecksum ||
		plan.EffectivePolicyChecksum != evidenceContext.EffectivePolicyChecksum ||
		plan.EffectivePolicy.Checksum != evidenceContext.EffectivePolicyChecksum {
		return apierrors.NewConflict("trusted admission plan evidence does not match exact admission material")
	}
	return nil
}

func validateAdmissionGateEvidencePreflight(
	preflight admissionGateEvidencePreflightRecord,
	evidenceContext admissionGateEvidenceContext,
) error {
	if preflight.ID == uuid.Nil ||
		preflight.OrganizationID != evidenceContext.OrganizationID ||
		preflight.DeploymentPlanID != evidenceContext.DeploymentPlanID ||
		preflight.PlanChecksum != evidenceContext.PlanChecksum ||
		preflight.CreatedAt.IsZero() ||
		preflight.CreatedAt.After(evidenceContext.EvaluatedAt) ||
		preflight.Status != types.DeploymentPreflightStatusPassed {
		return apierrors.NewConflict("trusted admission preflight evidence is stale or does not match")
	}
	for _, check := range preflight.Checks {
		if check.ID == uuid.Nil ||
			check.OrganizationID != evidenceContext.OrganizationID ||
			check.DeploymentPlanID != evidenceContext.DeploymentPlanID ||
			check.DeploymentPreflightRunID != preflight.ID ||
			check.CreatedAt.IsZero() ||
			check.CreatedAt.After(evidenceContext.EvaluatedAt) {
			return apierrors.NewConflict("trusted admission preflight check binding is invalid")
		}
	}
	return nil
}

func normalizedRequiredAdmissionEvidence(required []string) ([]string, error) {
	result := make([]string, 0, len(required))
	seen := make(map[string]struct{}, len(required))
	for _, item := range required {
		key := strings.TrimSpace(item)
		if key == "" || key != item {
			return nil, apierrors.NewConflict("trusted admission policy required evidence is invalid")
		}
		if _, exists := seen[key]; exists {
			return nil, apierrors.NewConflict("trusted admission policy required evidence is duplicated")
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	sort.Strings(result)
	return result, nil
}

func admissionChecksForRequiredEvidence(
	mapping admissionEvidenceCheckMapping,
	checks []admissionGateEvidenceCheckRecord,
) []admissionGateEvidenceCheckRecord {
	result := make([]admissionGateEvidenceCheckRecord, 0)
	for _, check := range checks {
		matches := mapping.exactKey != "" && check.CheckKey == mapping.exactKey ||
			mapping.keyPrefix != "" && strings.HasPrefix(check.CheckKey, mapping.keyPrefix)
		if matches {
			result = append(result, check)
		}
	}
	slices.SortFunc(result, func(left, right admissionGateEvidenceCheckRecord) int {
		if byKey := strings.Compare(left.CheckKey, right.CheckKey); byKey != 0 {
			return byKey
		}
		return strings.Compare(left.ID.String(), right.ID.String())
	})
	return result
}

func admissionEvidenceCheckAuthoritative(
	required string,
	check admissionGateEvidenceCheckRecord,
	evidenceContext admissionGateEvidenceContext,
) bool {
	var expected, actual map[string]any
	if len(check.Expected) > 0 && json.Unmarshal(check.Expected, &expected) != nil {
		return false
	}
	if len(check.Actual) > 0 && json.Unmarshal(check.Actual, &actual) != nil {
		return false
	}
	switch types.AdmissionGateKey(required) {
	case types.AdmissionGateIntegrity:
		return expected["checksum"] == evidenceContext.PlanChecksum && actual["valid"] == true
	case types.AdmissionGateBackup:
		checksum, _ := actual["checksum"].(string)
		return actual["required"] == true &&
			actual["verified"] == true &&
			trustedAdmissionChecksumValid(checksum)
	case types.AdmissionGateProvenance:
		return admissionReleaseEvidenceCheckAuthoritative(expected, actual)
	default:
		return required == "sbom" &&
			admissionReleaseEvidenceCheckAuthoritative(expected, actual)
	}
}

func admissionReleaseEvidenceCheckAuthoritative(expected, actual map[string]any) bool {
	references, referencesValid := actual["references"].([]any)
	if !referencesValid || len(references) == 0 {
		return false
	}
	for _, item := range references {
		reference, valid := item.(string)
		if !valid || strings.TrimSpace(reference) == "" {
			return false
		}
	}
	return expected["present"] == true &&
		expected["contractValid"] == true &&
		actual["present"] == true &&
		actual["contractValid"] == true
}

func admissionGateEvidenceChecksum(
	evidenceContext admissionGateEvidenceContext,
	required string,
	preflight admissionGateEvidencePreflightRecord,
	checks []admissionGateEvidenceCheckRecord,
) (string, error) {
	payload, err := json.Marshal(struct {
		OrganizationID          uuid.UUID                          `json:"organizationId"`
		DeploymentPlanID        uuid.UUID                          `json:"deploymentPlanId"`
		PlanRevision            int64                              `json:"planRevision"`
		PlanChecksum            string                             `json:"planChecksum"`
		EffectivePolicyChecksum string                             `json:"effectivePolicyChecksum"`
		EvaluatedAt             string                             `json:"evaluatedAt"`
		RequiredEvidence        string                             `json:"requiredEvidence"`
		PreflightID             uuid.UUID                          `json:"preflightId"`
		PreflightCreatedAt      string                             `json:"preflightCreatedAt"`
		Checks                  []admissionGateEvidenceCheckRecord `json:"checks"`
	}{
		OrganizationID:          evidenceContext.OrganizationID,
		DeploymentPlanID:        evidenceContext.DeploymentPlanID,
		PlanRevision:            evidenceContext.PlanRevision,
		PlanChecksum:            evidenceContext.PlanChecksum,
		EffectivePolicyChecksum: evidenceContext.EffectivePolicyChecksum,
		EvaluatedAt:             evidenceContext.EvaluatedAt.UTC().Format(time.RFC3339Nano),
		RequiredEvidence:        required,
		PreflightID:             preflight.ID,
		PreflightCreatedAt:      preflight.CreatedAt.UTC().Format(time.RFC3339Nano),
		Checks:                  checks,
	})
	if err != nil {
		return "", fmt.Errorf("marshal trusted admission gate evidence: %w", err)
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
