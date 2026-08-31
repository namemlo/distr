package db

import (
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/protectedhistory"
)

func TestProtectedHistoryProjectionIsExplicitAndComplete(t *testing.T) {
	t.Parallel()
	lowerSQL := strings.ToLower(protectedHistoryRecordsSQL)
	for _, kind := range protectedhistory.AllowedKinds() {
		if !strings.Contains(lowerSQL, "'"+kind+"'") {
			t.Errorf("protected history query does not project %s", kind)
		}
	}
	for _, unsafeProjection := range []string{
		"to_jsonb(",
		"variable.value",
		"output.value",
		"lease.lease_token_hash",
		"plan.canonical_payload",
		"bundle.canonical_payload",
		"execution.provider_url",
		"execution.last_message",
		"event.observed_state",
	} {
		if strings.Contains(lowerSQL, unsafeProjection) {
			t.Errorf("protected history query contains unsafe projection %q", unsafeProjection)
		}
	}
}

func TestProtectedHistoryProjectionIsSchemaEvolutionAware(t *testing.T) {
	t.Parallel()
	lowerSQL := strings.ToLower(protectedHistoryRecordsSQL)
	for _, wholeRowPattern := range []string{"select *", "row_to_json", "json_agg"} {
		if strings.Contains(lowerSQL, wholeRowPattern) {
			t.Errorf("protected history query contains whole-row pattern %q", wholeRowPattern)
		}
	}
	if !strings.Contains(lowerSQL, "targetcomponentstate', state.id") {
		t.Fatal("mutable target state identity projection is absent")
	}
	stateStart := strings.Index(lowerSQL, "union all select 'targetcomponentstate'")
	observationStart := strings.Index(lowerSQL, "union all select 'targetcomponentobservation'")
	if stateStart < 0 || observationStart <= stateStart {
		t.Fatal("target state projection boundaries are absent")
	}
	stateProjection := lowerSQL[stateStart:observationStart]
	for _, mutableColumn := range []string{"state_version", "state_checksum", "release_bundle_id", "observed_at"} {
		if strings.Contains(stateProjection, mutableColumn) {
			t.Errorf("mutable target state column %q is protected as immutable history", mutableColumn)
		}
	}
}
