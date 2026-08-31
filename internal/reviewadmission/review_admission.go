package reviewadmission

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

func ReviewMaterialChecksum(planChecksum, observedStateChecksum string) string {
	payload, _ := json.Marshal(struct {
		PlanChecksum          string `json:"planChecksum"`
		ObservedStateChecksum string `json:"observedStateChecksum"`
	}{strings.TrimSpace(planChecksum), strings.TrimSpace(observedStateChecksum)})
	return checksum(payload)
}

func CanonicalChecksum(decision types.ReviewAdmissionDecisionRecord) string {
	payload, _ := json.Marshal(struct {
		ID                     uuid.UUID                     `json:"id"`
		OrganizationID         uuid.UUID                     `json:"organizationId"`
		DeploymentPlanID       uuid.UUID                     `json:"deploymentPlanId"`
		PlanRevision           int64                         `json:"planRevision"`
		PlanChecksum           string                        `json:"planChecksum"`
		ReviewMaterialChecksum string                        `json:"reviewMaterialChecksum"`
		ObservedStateChecksum  string                        `json:"observedStateChecksum"`
		Decision               types.ReviewAdmissionDecision `json:"decision"`
		Reason                 string                        `json:"reason"`
		ActorUserAccountID     uuid.UUID                     `json:"actorUserAccountId"`
		ExpiresAt              string                        `json:"expiresAt"`
		SupersedesDecisionID   *uuid.UUID                    `json:"supersedesDecisionId,omitempty"`
		RevokesDecisionID      *uuid.UUID                    `json:"revokesDecisionId,omitempty"`
		AuthorizationEvidence  string                        `json:"authorizationEvidence"`
		IdempotencyKey         string                        `json:"idempotencyKey"`
	}{
		decision.ID, decision.OrganizationID, decision.DeploymentPlanID,
		decision.PlanRevision, strings.TrimSpace(decision.PlanChecksum),
		strings.TrimSpace(decision.ReviewMaterialChecksum),
		strings.TrimSpace(decision.ObservedStateChecksum), decision.Decision,
		strings.TrimSpace(decision.Reason), decision.ActorUserAccountID,
		decision.ExpiresAt.UTC().Format(time.RFC3339Nano), decision.SupersedesDecisionID,
		decision.RevokesDecisionID, strings.TrimSpace(decision.AuthorizationEvidence),
		strings.TrimSpace(decision.IdempotencyKey),
	})
	return checksum(payload)
}

func ValidateCurrentGo(
	decision types.ReviewAdmissionDecisionRecord,
	planChecksum, observedStateChecksum string,
	now time.Time,
) error {
	if decision.Decision != types.ReviewAdmissionDecisionGo {
		return errors.New("latest review admission decision is NO_GO")
	}
	if !now.UTC().Before(decision.ExpiresAt.UTC()) {
		return errors.New("review admission GO decision is expired")
	}
	if decision.PlanChecksum != strings.TrimSpace(planChecksum) ||
		decision.ObservedStateChecksum != strings.TrimSpace(observedStateChecksum) ||
		decision.ReviewMaterialChecksum != ReviewMaterialChecksum(planChecksum, observedStateChecksum) {
		return errors.New("review admission GO decision is stale")
	}
	if decision.CanonicalChecksum != CanonicalChecksum(decision) {
		return errors.New("review admission GO decision checksum is invalid")
	}
	return nil
}

func checksum(payload []byte) string {
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}
