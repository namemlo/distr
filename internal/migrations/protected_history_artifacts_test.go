package migrations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
)

func TestMigration170DefinesImmutableAuditBoundProtectedHistoryArtifacts(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	up, err := os.ReadFile(filepath.Join("sql", "170_protected_history_artifacts.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	down, err := os.ReadFile(filepath.Join("sql", "170_protected_history_artifacts.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	upSQL := strings.ToLower(string(up))
	downSQL := strings.ToLower(string(down))

	for _, required := range []string{
		"create table protectedhistoryartifact",
		"protectedhistoryartifact_idempotency_unique",
		"protectedhistoryartifact_append_only",
		"protectedhistoryartifact_no_truncate",
		"protectedhistoryartifact_audit_event_fk",
		"controlplaneauditevent_protected_history_artifact_fk",
		"deferrable initially deferred",
		"protected_history_request_checksum",
		"protected_history_retention_checksum",
		"protected_history_audit_binding_checksum",
		"protected_history_artifact_audit_guard",
		"references organization_useraccount",
		"scope arrays must be sorted and unique",
	} {
		g.Expect(upSQL).To(ContainSubstring(required), required)
	}
	g.Expect(downSQL).To(ContainSubstring(
		"refusing migration 170 rollback while protected-history artifacts exist",
	))
	g.Expect(downSQL).NotTo(ContainSubstring("truncate protectedhistoryartifact"))
}

func TestMigration170UsesExactlyOneUpDownPair(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	files, err := filepath.Glob(filepath.Join("sql", "170_*"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(files).To(ConsistOf(
		filepath.Join("sql", "170_protected_history_artifacts.up.sql"),
		filepath.Join("sql", "170_protected_history_artifacts.down.sql"),
	))
}
