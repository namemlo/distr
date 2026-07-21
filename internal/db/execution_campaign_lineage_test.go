package db

import (
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestCampaignMemberTaskExecutionBindingRequiresCompleteLineage(t *testing.T) {
	binding := CampaignMemberTaskExecutionBinding{
		ID: uuid.New(), OrganizationID: uuid.New(), CampaignRunID: uuid.New(),
		CampaignMemberRunID: uuid.New(), DeploymentPlanID: uuid.New(),
		TaskID: uuid.New(), DeploymentTargetID: uuid.New(),
	}
	if err := validateCampaignMemberTaskExecutionBinding(binding); err != nil {
		t.Fatalf("complete binding rejected: %v", err)
	}
	binding.TaskID = uuid.Nil
	if err := validateCampaignMemberTaskExecutionBinding(binding); err == nil ||
		!strings.Contains(err.Error(), "binding is incomplete") {
		t.Fatalf("expected incomplete binding rejection, got %v", err)
	}
}

func TestCampaignLineageRepositoryIsInsertOnlyAndIdempotent(t *testing.T) {
	content, err := os.ReadFile("execution_campaign_lineage.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(content)
	for _, required := range []string{
		"INSERT INTO CampaignMemberTaskExecution",
		"ON CONFLICT (organization_id, task_id) DO NOTHING",
		"existing != binding",
		"already bound to different campaign member lineage",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("campaign lineage repository is missing %q", required)
		}
	}
	if strings.Contains(source, "UPDATE CampaignMemberTaskExecution") {
		t.Fatal("immutable campaign lineage must never be updated")
	}
}
