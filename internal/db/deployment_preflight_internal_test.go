package db

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

func TestDeploymentPreflightLoadsEveryFrozenMigrationContract(t *testing.T) {
	first := types.DeploymentPlanMigration{
		MigrationID: "ledger.042", ContractChecksum: "sha256:" + strings.Repeat("a", 64),
		ComponentKey: "ledger", DatabaseResourceKey: "postgres:ledger",
		ExpectedSourceVersion: "41", ExpectedSourceChecksum: "sha256:" + strings.Repeat("b", 64),
		ResultingVersion: "42", Phase: types.MigrationPhaseExpand,
		DependsOn: []string{"ledger.041"}, LockType: "exclusive", LockTimeoutSeconds: 30,
		OperationalImpact: "brief_write_lock", BackupRequired: true,
		BackupVerifier: "backup-verifier:v1", RetryClass: types.MigrationRetryNone,
		Reversibility:                    types.MigrationReversibilityReversible,
		PreviousApplicationCompatibility: ">=1.8.0",
		RecoveryProcedureReference:       "recovery:ledger.042:v1",
		AdapterType:                      "database.postgres.v1",
		ArtifactDigest:                   "registry.example.com/ledger@sha256:" + strings.Repeat("c", 64),
		PreconditionProbes: []types.MigrationProbe{{
			Name: "source", Reference: "probe:source", ExpectedChecksum: "sha256:" + strings.Repeat("d", 64),
		}},
		PostconditionProbes: []types.MigrationProbe{{
			Name: "target", Reference: "probe:target", ExpectedChecksum: "sha256:" + strings.Repeat("e", 64),
		}},
		EvidenceRetentionDays: 90,
	}
	second := first
	second.MigrationID = "ledger.043"

	migrations := deploymentPreflightMigrations([]types.DeploymentPlanMigration{first, second})

	if len(migrations) != 2 {
		t.Fatalf("expected every migration, got %d", len(migrations))
	}
	if !reflect.DeepEqual(migrations[0].Contract, first.MigrationContract()) {
		t.Fatalf("first migration contract was not loaded completely: %#v", migrations[0].Contract)
	}
	if !reflect.DeepEqual(migrations[1].Contract, second.MigrationContract()) {
		t.Fatalf("second migration contract was not loaded completely: %#v", migrations[1].Contract)
	}
}

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
