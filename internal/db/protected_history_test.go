package db

import (
	"strings"
	"testing"
)

func TestProtectedHistorySchema138ProjectionCoversAllHistoryFamiliesAndFields(t *testing.T) {
	t.Parallel()
	lowerSQL := strings.ToLower(protectedHistoryLegacyRecordsSQL)
	for _, kind := range []string{
		"application", "applicationversion", "customerorganization",
		"deploymenttarget", "deploymenttargetlogrecord",
		"deployment", "deploymentrevision", "deploymentrevisionstatus", "deploymentlogrecord",
		"releasebundle", "releasebundlecomponent", "releasebundleauditevent",
		"releasebundleidempotencykey", "processsnapshot", "variablesnapshot",
		"variablesnapshotvalue", "deploymentplan", "deploymentplanissue",
		"deploymentplanstep", "deploymentplantarget", "deploymentplantargetcomponent",
		"deploymentplanvariable", "deploymentpreflightrun", "deploymentpreflightcheck",
		"task", "tasklease", "taskresourcelock", "steprun", "steprunevent",
		"steprunlogchunk", "steprunoutput", "targetcomponentstate",
		"targetcomponentobservation", "externalexecution", "externalexecutionevent",
		"externalexecutiontimestampmanifest", "externalexecutiontimestampcellprovenance",
		"externalexecutiontimestampdeletiontombstone", "externalexecutiontimestampexpandstate",
		"externalexecutiontimestampcontractgate",
	} {
		if !strings.Contains(lowerSQL, "'"+kind+"'") {
			t.Errorf("schema-138 protected history query does not project %s", kind)
		}
	}
	if strings.Count(lowerSQL, "to_jsonb(") < 30 {
		t.Fatal("schema-138 projection does not protect complete row fields")
	}
	if strings.Contains(lowerSQL, "select *") || strings.Contains(lowerSQL, "json_agg") {
		t.Fatal("schema-138 projection uses an unbounded aggregate or select-star")
	}
	if strings.Contains(lowerSQL, "deploymenttargetstatus") {
		t.Fatal("schema-138 projection references DeploymentTargetStatus dropped by migration 95")
	}
}

func TestProtectedHistoryProjectionRegistersSchema166Through169(t *testing.T) {
	t.Parallel()
	for _, version := range []uint64{138, 165, 166, 167, 168, 169} {
		query, err := protectedHistoryRecordsSQLForSchema(version)
		if err != nil || strings.TrimSpace(query) == "" {
			t.Fatalf("schema %d projection is not registered: %v", version, err)
		}
	}
	query166, _ := protectedHistoryRecordsSQLForSchema(166)
	query167, _ := protectedHistoryRecordsSQLForSchema(167)
	query168, _ := protectedHistoryRecordsSQLForSchema(168)
	query169, _ := protectedHistoryRecordsSQLForSchema(169)
	if strings.Contains(strings.ToLower(query166), "'executionruntimeevidence'") {
		t.Fatal("schema 166 projection references migration-167 evidence")
	}
	if strings.Contains(strings.ToLower(query166), "deploymentplanresolvedrequirement") ||
		strings.Contains(strings.ToLower(query166), "baselineadoptioncomponent") {
		t.Fatal("schema 166 projection references migration-168 or migration-169 records")
	}
	if !strings.Contains(strings.ToLower(query167), "'executionruntimeevidence'") {
		t.Fatal("schema 167 projection omits runtime trust evidence")
	}
	if strings.Contains(strings.ToLower(query167), "deploymentplanresolvedrequirement") ||
		strings.Contains(strings.ToLower(query167), "baselineadoptioncomponent") {
		t.Fatal("schema 167 projection references migration-168 or migration-169 records")
	}
	if !strings.Contains(strings.ToLower(query168), "deploymentplanresolvedrequirement") {
		t.Fatal("schema 168 projection omits provider-evidence records")
	}
	if strings.Contains(strings.ToLower(query168), "baselineadoptioncomponent") {
		t.Fatal("schema 168 projection references migration-169 baseline facts")
	}
	if !strings.Contains(strings.ToLower(query169), "baselineadoptioncomponent") {
		t.Fatal("schema 169 projection omits separated baseline facts")
	}
	if _, err := protectedHistoryRecordsSQLForSchema(170); err == nil {
		t.Fatal("unknown schema 170 did not fail closed")
	}
}

func TestExpandedProjectionIncludesDatabaseBackedRetirementAuthorization(t *testing.T) {
	t.Parallel()
	lowerSQL := strings.ToLower(protectedHistoryRecordsSQL)
	for _, kind := range []string{
		"approvalrequest", "approvaldecision", "sampleretirementjob",
		"sampleretirementitem", "sampleretirementownershipevidence",
	} {
		if !strings.Contains(lowerSQL, "'"+kind+"'") {
			t.Errorf("expanded projection omits retirement authorization record %s", kind)
		}
	}
	if strings.Contains(lowerSQL, "intent.payload") {
		t.Fatal("expanded projection exposes signed execution intent payload")
	}
}
