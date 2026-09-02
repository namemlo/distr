package db

import (
	"regexp"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/pilotexception"
	"github.com/distr-sh/distr/internal/protectedhistory"
	"github.com/google/uuid"
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
	if !strings.Contains(lowerSQL, "to_jsonb(snapshot_value.*)") ||
		!strings.Contains(lowerSQL, "from variablesnapshotvalue snapshot_value") ||
		strings.Contains(lowerSQL, "to_jsonb(value)") {
		t.Fatal("schema-138 projection can resolve the VariableSnapshotValue row alias as its nullable value column")
	}
	if !strings.Contains(lowerSQL, "to_jsonb(plan_component.*)") ||
		!strings.Contains(lowerSQL, "from deploymentplantargetcomponent plan_component") {
		t.Fatal("schema-138 projection can resolve the DeploymentPlanTargetComponent row alias as its component column")
	}
}

func TestProtectedHistoryProjectionRegistersSchema166Through171(t *testing.T) {
	t.Parallel()
	for _, version := range []uint64{138, 165, 166, 167, 168, 169, 170, 171} {
		query, err := protectedHistoryRecordsSQLForSchema(version)
		if err != nil || strings.TrimSpace(query) == "" {
			t.Fatalf("schema %d projection is not registered: %v", version, err)
		}
	}
	query166, _ := protectedHistoryRecordsSQLForSchema(166)
	query167, _ := protectedHistoryRecordsSQLForSchema(167)
	query168, _ := protectedHistoryRecordsSQLForSchema(168)
	query169, _ := protectedHistoryRecordsSQLForSchema(169)
	query170, _ := protectedHistoryRecordsSQLForSchema(170)
	query171, _ := protectedHistoryRecordsSQLForSchema(171)
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
	for _, kind := range []string{"protectedhistoryartifact", "controlplaneauditevent"} {
		if strings.Contains(strings.ToLower(query169), "'"+kind+"'") {
			t.Fatalf("schema 169 projection references migration-170 record %s", kind)
		}
		if !strings.Contains(strings.ToLower(query170), "'"+kind+"'") {
			t.Fatalf("schema 170 projection omits retained history record %s", kind)
		}
		if !strings.Contains(strings.ToLower(query171), "'"+kind+"'") {
			t.Fatalf("schema 171 projection omits retained history record %s", kind)
		}
	}
	if _, err := protectedHistoryRecordsSQLForSchema(172); err == nil {
		t.Fatal("unknown schema 172 did not fail closed")
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

func TestProtectedHistoryWholeRowJSONConversionsAreExplicitlyQualified(t *testing.T) {
	t.Parallel()
	queries := strings.Join([]string{
		protectedHistoryLegacyRecordsSQL,
		protectedHistoryRecordsSQL,
		protectedHistorySchema167RecordsSQL,
		protectedHistorySchema168RecordsSQL,
		protectedHistorySchema169RecordsSQL,
		protectedHistorySchema170RecordsSQL,
	}, "\n")
	bareWholeRow := regexp.MustCompile(`(?i)to_jsonb\([a-z_][a-z0-9_]*\)`)
	if match := bareWholeRow.FindString(queries); match != "" {
		t.Fatalf("protected-history projection has ambiguous whole-row conversion %q", match)
	}
	for _, qualified := range []string{
		"to_jsonb(snapshot_value.*)",
		"to_jsonb(plan_component.*)",
		"to_jsonb(decision.*)",
	} {
		if !strings.Contains(strings.ToLower(queries), qualified) {
			t.Fatalf("protected-history projection omits qualified conversion %s", qualified)
		}
	}
}

func TestProtectedHistoryReplayUsesStoredPilotEvidence(t *testing.T) {
	t.Parallel()
	organizationID := uuid.New()
	actorID := uuid.New()
	existing := protectedhistory.RetainedArtifact{
		GovernanceExceptionKey:       pilotexception.Key,
		GovernanceExceptionReference: "approved-change-123",
	}
	evidence, err := protectedHistoryReplayGovernanceException(existing)
	if err != nil {
		t.Fatal(err)
	}
	request := protectedhistory.CreateRetentionRequest{
		OrganizationID: organizationID,
		Scope: protectedhistory.Scope{
			OrganizationID:      organizationID.String(),
			DeploymentTargetIDs: []string{uuid.New().String()},
		},
		IssuerUserAccountID:   actorID,
		ReviewerUserAccountID: actorID,
		GovernanceException:   evidence,
		IdempotencyKey:        "retention-123",
	}
	replayChecksum, err := protectedhistory.RetentionRequestChecksum(request)
	if err != nil {
		t.Fatalf("stored pilot evidence did not restore replay checksum material: %v", err)
	}
	request.GovernanceException = &pilotexception.Evidence{
		Key:               pilotexception.Key,
		ApprovalReference: "approved-change-123",
	}
	originalChecksum, err := protectedhistory.RetentionRequestChecksum(request)
	if err != nil {
		t.Fatal(err)
	}
	if replayChecksum != originalChecksum {
		t.Fatal("stored pilot evidence changed idempotent replay checksum")
	}
}
