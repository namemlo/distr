package db

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

func TestDeploymentPlanCanonicalStateValidComparesLegacyPayload(t *testing.T) {
	plan := types.DeploymentPlan{
		ReleaseBundleID: uuid.New(),
		ApplicationID:   uuid.New(),
		ChannelID:       uuid.New(),
		EnvironmentID:   uuid.New(),
		Status:          types.DeploymentPlanStatusReady,
	}
	payload, err := canonicalizeDeploymentPlan(plan)
	if err != nil {
		t.Fatalf("canonicalize deployment plan: %v", err)
	}
	var legacy map[string]any
	if err := json.Unmarshal(payload, &legacy); err != nil {
		t.Fatalf("decode canonical deployment plan: %v", err)
	}
	legacy["status"] = string(types.DeploymentPlanStatusReady)
	delete(legacy, "targetComponents")
	legacyPayload, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("encode legacy deployment plan: %v", err)
	}
	sum := sha256.Sum256(legacyPayload)
	plan.CanonicalPayload = legacyPayload
	plan.CanonicalChecksum = "sha256:" + hex.EncodeToString(sum[:])
	plan.Status = types.DeploymentPlanStatusExecuted

	valid, err := deploymentPlanCanonicalStateValid(plan)
	if err != nil {
		t.Fatalf("validate legacy deployment plan: %v", err)
	}
	if !valid {
		t.Fatal("expected unchanged legacy immutable state to remain valid")
	}

	plan.EnvironmentID = uuid.New()
	valid, err = deploymentPlanCanonicalStateValid(plan)
	if err != nil {
		t.Fatalf("validate drifted legacy deployment plan: %v", err)
	}
	if valid {
		t.Fatal("expected legacy immutable state drift to fail validation")
	}
}
