package db

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/planning"
	"github.com/distr-sh/distr/internal/schemaevidence"
	"github.com/distr-sh/distr/internal/types"
)

func requireCurrentDeploymentPlanSchemaEvidence(
	plan types.DeploymentPlan,
	evaluatedAt time.Time,
) error {
	if plan.PlanSchema != types.TargetDeploymentPlanSchemaV2 {
		return nil
	}
	canonical, err := decodeFrozenTargetPlanSchemaEvidence(plan.CanonicalPayload)
	if err != nil {
		return err
	}
	payload, checksum, err := planning.CanonicalizeTargetDeploymentPlan(canonical)
	if err != nil || checksum != plan.CanonicalChecksum || !bytes.Equal(payload, plan.CanonicalPayload) {
		return apierrors.NewConflict("deployment plan canonical schema evidence is invalid")
	}
	issues := schemaevidence.ValidateCanonicalPlan(
		canonical,
		plan.OrganizationID,
		evaluatedAt.UTC(),
	)
	if len(issues) > 0 {
		return apierrors.NewConflict(fmt.Sprintf(
			"deployment plan schema evidence failed: %s: %s",
			issues[0].Code,
			issues[0].Message,
		))
	}
	return nil
}

func hydrateDeploymentPlanSchemaEvidence(plan *types.DeploymentPlan) error {
	if plan.PlanSchema != types.TargetDeploymentPlanSchemaV2 {
		return nil
	}
	canonical, err := decodeFrozenTargetPlanSchemaEvidence(plan.CanonicalPayload)
	if err != nil {
		return err
	}
	plan.SchemaEvidence = canonical.SchemaEvidence
	return nil
}

func decodeFrozenTargetPlanSchemaEvidence(
	payload []byte,
) (types.TargetDeploymentPlanCanonical, error) {
	var canonical types.TargetDeploymentPlanCanonical
	if err := json.Unmarshal(payload, &canonical); err != nil ||
		canonical.Schema != types.TargetDeploymentPlanSchemaV2 {
		return canonical, apierrors.NewConflict("deployment plan canonical payload is invalid")
	}
	return canonical, nil
}
