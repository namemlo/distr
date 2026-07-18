package migrationplanning

import (
	"encoding/json"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestBuildRecoveryPlanReversesMigrationDependencies(t *testing.T) {
	g := NewWithT(t)
	first := migrationContractFixture()
	second := migrationContractFixture()
	second.ID = "ledger.043"
	second.Checksum = checksum("d")
	second.DependsOn = []string{first.ID}
	failed := types.FailedPlan{
		PlanID:                uuid.New(),
		Draft:                 types.PlanDraft{ID: uuid.New(), ProtocolVersion: types.DeploymentPlanProtocolV2},
		Contracts:             []types.MigrationContract{first, second},
		CompletedMigrationIDs: []string{first.ID, second.ID},
	}

	draft, err := BuildRecoveryPlan(failed, types.RecoveryRequest{
		Mode: types.RecoveryModeReverse, Reason: "migration validation failed",
	})

	g.Expect(err).NotTo(HaveOccurred())
	var recovery types.RecoveryPlan
	g.Expect(json.Unmarshal(draft.PreviewPayload, &recovery)).To(Succeed())
	g.Expect(recovery.Graph.TopologicalOrder).To(Equal([]string{
		"recovery:ledger.043:reverse",
		"recovery:ledger.042:reverse",
	}))
	g.Expect(recovery.EvidenceRetentionRequired).To(BeTrue())
	g.Expect(*draft.SupersedesDeploymentPlanID).To(Equal(failed.PlanID))
}

func TestBuildRecoveryPlanBlocksAutomaticReverseForForwardOnlyMigration(t *testing.T) {
	g := NewWithT(t)
	contract := migrationContractFixture()
	contract.Reversibility = types.MigrationReversibilityForwardOnly
	contract.RequiresForwardFix = true

	_, err := BuildRecoveryPlan(types.FailedPlan{
		PlanID: uuid.New(), Contracts: []types.MigrationContract{contract},
		CompletedMigrationIDs: []string{contract.ID},
	}, types.RecoveryRequest{Mode: types.RecoveryModeReverse, Reason: "failed"})

	g.Expect(err).To(MatchError(ContainSubstring("forward-fix")))
}

func TestBuildRecoveryPlanSupportsManualForwardFix(t *testing.T) {
	g := NewWithT(t)

	draft, err := BuildRecoveryPlan(types.FailedPlan{
		PlanID: uuid.New(), Draft: types.PlanDraft{ID: uuid.New()},
	}, types.RecoveryRequest{
		Mode: types.RecoveryModeManual, Reason: "operator follows recovery procedure",
	})

	g.Expect(err).NotTo(HaveOccurred())
	var recovery types.RecoveryPlan
	g.Expect(json.Unmarshal(draft.PreviewPayload, &recovery)).To(Succeed())
	g.Expect(recovery.Mode).To(Equal(types.RecoveryModeManual))
	g.Expect(recovery.Graph.Steps).To(BeEmpty())
}

func TestBuildRecoveryPlanRequiresSeparateRestoreApprovalAndFrozenBackup(t *testing.T) {
	g := NewWithT(t)
	failed := types.FailedPlan{PlanID: uuid.New(), Draft: types.PlanDraft{ID: uuid.New()}}

	_, err := BuildRecoveryPlan(failed, types.RecoveryRequest{
		Mode: types.RecoveryModeRestore, Reason: "emergency restore",
	})
	g.Expect(err).To(MatchError(ContainSubstring("separate approval")))

	_, err = BuildRecoveryPlan(failed, types.RecoveryRequest{
		Mode: types.RecoveryModeRestore, Reason: "emergency restore",
		SeparateApprovalID: "approval-123",
		BackupID:           "backup-20260718-001", BackupChecksum: checksum("e"),
		DatabaseResourceKey: "postgres:ledger", ExpectedDataLossBoundary: "2026-07-18T12:00:00Z",
		ProcedureVersion: "restore:v3", OperatorScope: "database:ledger:restore",
		RequiredApproverGroups: []string{"database-owners"},
	})
	g.Expect(err).To(MatchError(ContainSubstring("validation probes")))

	_, err = BuildRecoveryPlan(failed, types.RecoveryRequest{
		Mode: types.RecoveryModeRestore, Reason: "emergency restore",
		SeparateApprovalID: "approval-123",
		BackupID:           "backup-20260718-001", BackupChecksum: checksum("e"),
		DatabaseResourceKey: "postgres:ledger", ExpectedDataLossBoundary: "2026-07-18T12:00:00Z",
		ProcedureVersion: "restore:v3", OperatorScope: "database:ledger:restore",
		RequiredApproverGroups: []string{"database-owners"},
		ValidationProbes: []types.MigrationProbe{{
			Name: "schema", Reference: "", ExpectedChecksum: checksum("f"),
		}},
	})
	g.Expect(err).To(MatchError(ContainSubstring("validation probe")))

	draft, err := BuildRecoveryPlan(failed, types.RecoveryRequest{
		Mode: types.RecoveryModeRestore, Reason: "emergency restore",
		SeparateApprovalID: uuid.NewString(),
		BackupID:           "backup-20260718-001", BackupChecksum: checksum("e"),
		DatabaseResourceKey: "postgres:ledger", ExpectedDataLossBoundary: "2026-07-18T12:00:00Z",
		ProcedureVersion: "restore:v3", OperatorScope: "database:ledger:restore",
		RequiredApproverGroups: []string{"database-owners", "incident-commanders"},
		ValidationProbes: []types.MigrationProbe{{
			Name: "schema", Reference: "probe:ledger:restore:v1",
			ExpectedChecksum: checksum("f"),
		}},
	})
	g.Expect(err).NotTo(HaveOccurred())
	var recovery types.RecoveryPlan
	g.Expect(json.Unmarshal(draft.PreviewPayload, &recovery)).To(Succeed())
	g.Expect(recovery.Graph.Steps).To(HaveLen(2))
	g.Expect(recovery.Graph.Steps[0].ActionType).To(Equal("database.restore.execute"))
	g.Expect(recovery.Graph.Steps[1].ActionType).To(Equal("database.restore.verify"))
}
