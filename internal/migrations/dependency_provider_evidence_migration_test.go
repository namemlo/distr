package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestDependencyProviderEvidenceMigrationBoundsLockWaits(t *testing.T) {
	for _, path := range []string{
		"sql/168_dependency_provider_evidence.up.sql",
		"sql/168_dependency_provider_evidence.down.sql",
	} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		normalized := strings.Join(strings.Fields(string(content)), " ")
		if !strings.HasPrefix(normalized, "SET LOCAL lock_timeout = '10s'; SET LOCAL statement_timeout = '5min';") {
			t.Fatalf("%s must bound lock and statement waits before DDL", path)
		}
	}
}
