package externalexecutiontimestamp

import (
	"crypto/sha256"
	"encoding/binary"
	"testing"
)

func TestMigrationAdvisoryLockKey(t *testing.T) {
	digest := sha256.Sum256([]byte(
		"distr-external-execution-timestamp-migration/v1",
	))
	derived := int64(binary.BigEndian.Uint64(digest[:8]))
	if derived != MigrationAdvisoryLockKey {
		t.Fatalf("migration advisory lock key = %d, want %d",
			MigrationAdvisoryLockKey, derived)
	}
}
