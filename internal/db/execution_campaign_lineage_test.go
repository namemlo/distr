package db

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

func TestCampaignLineageAuditIsCorrelated(t *testing.T) {
	binding := CampaignMemberTaskExecutionBinding{
		ID: uuid.New(), OrganizationID: uuid.New(), CampaignRunID: uuid.New(),
		CampaignMemberRunID: uuid.New(), DeploymentPlanID: uuid.New(),
		TaskID: uuid.New(), DeploymentTargetID: uuid.New(),
	}
	event, err := campaignLineageBoundAuditEvent(binding)
	if err != nil {
		t.Fatalf("build campaign lineage audit: %v", err)
	}
	if err := validateControlPlaneAuditEventInput(event); err != nil {
		t.Fatalf("campaign lineage audit is invalid: %v", err)
	}
	if event.DeploymentPlanID == nil || *event.DeploymentPlanID != binding.DeploymentPlanID ||
		event.CampaignRunID == nil || *event.CampaignRunID != binding.CampaignRunID ||
		event.CampaignMemberRunID == nil || *event.CampaignMemberRunID != binding.CampaignMemberRunID ||
		event.TaskID == nil || *event.TaskID != binding.TaskID ||
		event.DeploymentTargetID == nil || *event.DeploymentTargetID != binding.DeploymentTargetID {
		t.Fatalf("campaign lineage audit lost typed correlations: %#v", event)
	}
	var captured types.ControlPlaneAuditEventInput
	ctx := WithExecutionV2AuditHook(context.Background(), ControlPlaneAuditAppendHookFunc(
		func(_ context.Context, input types.ControlPlaneAuditEventInput) error {
			captured = input
			return nil
		},
	))
	if err := appendExecutionV2Audit(ctx, event); err != nil {
		t.Fatalf("append campaign lineage audit: %v", err)
	}
	if captured.EventType != "execution.campaign_lineage_bound" {
		t.Fatalf("unexpected campaign lineage audit: %#v", captured)
	}
}

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
