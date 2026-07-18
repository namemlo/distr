package actionregistry

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestDatabaseActionsAreRegisteredInBoundedOrder(t *testing.T) {
	g := NewWithT(t)
	actions := DefaultRegistry().List()

	g.Expect(actionTypes(actions)).To(ContainElements(
		"database.backup.create",
		"database.backup.verify",
		"database.migration.apply",
		"database.migration.validate",
		"database.migration.reverse",
		"database.restore.execute",
		"database.restore.verify",
	))
}

func TestDatabaseMigrationActionRequiresFrozenRetryAndLockInputs(t *testing.T) {
	g := NewWithT(t)
	registry := DefaultRegistry()

	g.Expect(registry.ValidateInput("database.migration.apply", jsonObject(t, `{
		"migrationId":"ledger.042",
		"migrationChecksum":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"databaseResourceKey":"postgres:ledger",
		"databaseLockKey":"database:postgres:ledger",
		"expectedSourceVersion":"41",
		"expectedSourceChecksum":"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		"resultingVersion":"42",
		"artifactDigest":"registry.example.com/migrations/ledger@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"idempotencyKey":"ledger.042",
		"timeoutSeconds":1800
	}`))).To(Succeed())

	err := registry.ValidateInput("database.migration.apply", jsonObject(t, `{
		"migrationId":"ledger.042",
		"migrationChecksum":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"databaseResourceKey":"postgres:ledger",
		"databaseLockKey":"database:postgres:ledger",
		"expectedSourceVersion":"41",
		"expectedSourceChecksum":"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		"resultingVersion":"42",
		"artifactDigest":"migration:latest",
		"idempotencyKey":"ledger.042",
		"timeoutSeconds":1800
	}`))
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("artifactDigest"))
}

func TestDatabaseMigrationActionsFreezeSourceSchemaChecksums(t *testing.T) {
	g := NewWithT(t)
	registry := DefaultRegistry()

	missingApplyChecksum := registry.ValidateInput("database.migration.apply", jsonObject(t, `{
		"migrationId":"ledger.042",
		"migrationChecksum":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"databaseResourceKey":"postgres:ledger",
		"databaseLockKey":"database:postgres:ledger",
		"expectedSourceVersion":"41",
		"resultingVersion":"42",
		"artifactDigest":"registry.example.com/migrations/ledger@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"idempotencyKey":"ledger.042",
		"timeoutSeconds":1800
	}`))
	g.Expect(missingApplyChecksum).To(HaveOccurred())
	g.Expect(missingApplyChecksum.Error()).To(ContainSubstring("expectedSourceChecksum"))

	g.Expect(registry.ValidateInput("database.migration.validate", jsonObject(t, `{
		"migrationId":"ledger.042",
		"migrationChecksum":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"databaseResourceKey":"postgres:ledger",
		"databaseLockKey":"database:postgres:ledger",
		"expectedSchemaVersion":"41",
		"expectedSchemaChecksum":"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		"probes":[{"name":"source","reference":"probe:ledger:source:v1","expectedChecksum":"sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}],
		"timeoutSeconds":1800
	}`))).To(Succeed())

	invalidValidationChecksum := registry.ValidateInput(
		"database.migration.validate",
		jsonObject(t, `{
			"migrationId":"ledger.042",
			"migrationChecksum":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"databaseResourceKey":"postgres:ledger",
			"databaseLockKey":"database:postgres:ledger",
			"expectedSchemaVersion":"41",
			"expectedSchemaChecksum":"unknown",
			"probes":[{"name":"source","reference":"probe:ledger:source:v1","expectedChecksum":"sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}],
			"timeoutSeconds":1800
		}`),
	)
	g.Expect(invalidValidationChecksum).To(HaveOccurred())
	g.Expect(invalidValidationChecksum.Error()).To(ContainSubstring("expectedSchemaChecksum"))
}

func TestDatabaseActionsRejectPlaintextSecretsAndUnboundedEvidence(t *testing.T) {
	g := NewWithT(t)
	registry := DefaultRegistry()

	err := registry.ValidateInput("database.backup.create", jsonObject(t, `{
		"databaseResourceKey":"postgres:ledger",
		"databaseLockKey":"database:postgres:ledger",
		"destinationReference":"backup:ledger",
		"credentials":"plaintext",
		"idempotencyKey":"backup-ledger",
		"timeoutSeconds":300
	}`))
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("credentials"))
}

func TestBackupVerifyRequiresFrozenBackupIdentityOrCreationReference(t *testing.T) {
	g := NewWithT(t)
	registry := DefaultRegistry()

	err := registry.ValidateInput("database.backup.verify", jsonObject(t, `{
		"databaseResourceKey":"postgres:ledger",
		"databaseLockKey":"database:postgres:ledger",
		"verifierReference":"backup-verifier:v1",
		"timeoutSeconds":300
	}`))
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("oneOf"))

	g.Expect(registry.ValidateInput("database.backup.verify", jsonObject(t, `{
		"databaseResourceKey":"postgres:ledger",
		"databaseLockKey":"database:postgres:ledger",
		"backupReference":"migration:ledger.042:backup:create",
		"verifierReference":"backup-verifier:v1",
		"timeoutSeconds":300
	}`))).To(Succeed())
}

func TestRestoreExecuteRequiresSeparateRecoveryApprovalInputs(t *testing.T) {
	g := NewWithT(t)
	registry := DefaultRegistry()

	g.Expect(registry.ValidateInput("database.restore.execute", jsonObject(t, `{
		"recoveryPlanId":"00000000-0000-0000-0000-000000000001",
		"separateApprovalId":"approval-123",
		"backupId":"backup-20260718-001",
		"backupChecksum":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"databaseResourceKey":"postgres:ledger",
		"databaseLockKey":"database:postgres:ledger",
		"expectedDataLossBoundary":"2026-07-18T12:00:00Z",
		"procedureVersion":"restore:v3",
		"requiredApproverGroups":["database-owners"],
		"operatorScope":"database:ledger:restore",
		"validationProbes":[{"name":"schema","reference":"probe:ledger:restore:v1","expectedChecksum":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}],
		"idempotencyKey":"restore-ledger-001",
		"timeoutSeconds":3600
	}`))).To(Succeed())

	err := registry.ValidateInput("database.restore.execute", jsonObject(t, `{
		"backupId":"backup-20260718-001",
		"backupChecksum":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"databaseResourceKey":"postgres:ledger",
		"databaseLockKey":"database:postgres:ledger",
		"idempotencyKey":"restore-ledger-001",
		"timeoutSeconds":3600
	}`))
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("separateApprovalId"))
}
