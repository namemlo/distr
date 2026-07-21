package db

import (
	"strings"
	"testing"
)

func TestListRunnableCampaignRunIDsQueryIsStableBoundedAndLeaseAware(t *testing.T) {
	checks := []string{
		"state = 'RUNNING'",
		"lease_expires_at IS NULL",
		"lease_expires_at <= clock_timestamp()",
		"lease_holder = @worker_id",
		"ORDER BY COALESCE(lease_expires_at, '-infinity'::timestamptz), id",
		"LIMIT @limit",
	}
	for _, check := range checks {
		if !strings.Contains(listRunnableCampaignRunIDsSQL, check) {
			t.Fatalf("query missing %q", check)
		}
	}
	if strings.Contains(listRunnableCampaignRunIDsSQL, "admissions_blocked = FALSE") {
		t.Fatal("admissions-blocked running campaigns must remain runnable for recovery and pause finalization")
	}
}

func TestValidateRunnableCampaignBatch(t *testing.T) {
	if err := validateRunnableCampaignBatch("worker-a", 25); err != nil {
		t.Fatalf("valid batch rejected: %v", err)
	}
	for _, test := range []struct {
		workerID string
		limit    int
	}{
		{workerID: "", limit: 25},
		{workerID: "worker-a", limit: 0},
		{workerID: "worker-a", limit: 101},
	} {
		if err := validateRunnableCampaignBatch(test.workerID, test.limit); err == nil {
			t.Fatalf("invalid batch accepted: %#v", test)
		}
	}
}
