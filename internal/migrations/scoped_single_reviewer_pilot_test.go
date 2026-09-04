package migrations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
)

func TestMigration171BindsPilotExceptionToAppendOnlyEvidence(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	up, err := os.ReadFile(filepath.Join("sql", "171_scoped_single_reviewer_pilot.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	down, err := os.ReadFile(filepath.Join("sql", "171_scoped_single_reviewer_pilot.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	upSQL := strings.ToLower(string(up))
	downSQL := strings.ToLower(string(down))

	for _, required := range []string{
		"scoped-single-reviewer-pilot",
		"governance_exception_key",
		"governance_exception_reference",
		"protectedhistoryartifact_review_governance_check",
		"approvaldecision_governance_exception_check",
		"issuer_useraccount_id = reviewer_useraccount_id\n      and cardinality(customer_organization_ids) = 0\n      and cardinality(deployment_target_ids) = 1\n      and governance_exception_key is not null",
		"decision = 'approve'\n      and governance_exception_key is not null\n      and governance_exception_reference is not null",
		"protected_history_artifact_audit_guard",
		"is not distinct from new.governance_exception_key",
	} {
		g.Expect(upSQL).To(ContainSubstring(required), required)
	}
	g.Expect(downSQL).To(ContainSubstring(
		"refusing migration 171 rollback while pilot governance exception evidence exists",
	))
	g.Expect(downSQL).To(ContainSubstring(
		"lock table protectedhistoryartifact, approvaldecision\n  in share row exclusive mode",
	))
	g.Expect(downSQL).To(ContainSubstring(
		"refusing migration 171 rollback while schema-171 protected-history evidence exists",
	))
}
