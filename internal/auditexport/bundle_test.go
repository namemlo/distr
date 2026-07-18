package auditexport

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

func TestBuildDeploymentEvidenceBundleIsDeterministicAndCorrelated(t *testing.T) {
	t.Parallel()

	organizationID := uuid.New()
	deploymentPlanID := uuid.New()
	events := []types.ControlPlaneAuditEvent{
		{
			ID:               uuid.MustParse("00000000-0000-0000-0000-000000000002"),
			OrganizationID:   organizationID,
			DeploymentPlanID: &deploymentPlanID,
			EventType:        "execution.completed",
			Outcome:          "SUCCEEDED",
			Sequence:         2,
			CreatedAt:        time.Date(2026, 7, 18, 2, 0, 0, 0, time.UTC),
		},
		{
			ID:               uuid.MustParse("00000000-0000-0000-0000-000000000001"),
			OrganizationID:   organizationID,
			DeploymentPlanID: &deploymentPlanID,
			EventType:        "plan.published",
			Outcome:          "SUCCEEDED",
			Sequence:         1,
			CreatedAt:        time.Date(2026, 7, 18, 1, 0, 0, 0, time.UTC),
		},
	}

	first, err := BuildDeploymentEvidenceBundle(types.EvidenceBundleQuery{
		OrganizationID:   organizationID,
		DeploymentPlanID: deploymentPlanID,
	}, events)
	if err != nil {
		t.Fatalf("BuildDeploymentEvidenceBundle() error = %v", err)
	}
	second, err := BuildDeploymentEvidenceBundle(types.EvidenceBundleQuery{
		OrganizationID:   organizationID,
		DeploymentPlanID: deploymentPlanID,
	}, []types.ControlPlaneAuditEvent{events[1], events[0]})
	if err != nil {
		t.Fatalf("BuildDeploymentEvidenceBundle() second error = %v", err)
	}

	if first.Checksum != second.Checksum {
		t.Fatalf("checksums differ for the same evidence: %q != %q", first.Checksum, second.Checksum)
	}
	if len(first.Events) != 2 || first.Events[0].Sequence != 1 || first.Events[1].Sequence != 2 {
		t.Fatalf("events are not deterministically ordered: %#v", first.Events)
	}
	if first.OrganizationID != organizationID || first.DeploymentPlanID != deploymentPlanID {
		t.Fatalf("bundle lost correlation: %#v", first)
	}
}

func TestBuildDeploymentEvidenceBundleRejectsCrossOrganizationEvent(t *testing.T) {
	t.Parallel()

	organizationID := uuid.New()
	deploymentPlanID := uuid.New()
	_, err := BuildDeploymentEvidenceBundle(types.EvidenceBundleQuery{
		OrganizationID:   organizationID,
		DeploymentPlanID: deploymentPlanID,
	}, []types.ControlPlaneAuditEvent{{
		ID:               uuid.New(),
		OrganizationID:   uuid.New(),
		DeploymentPlanID: &deploymentPlanID,
		EventType:        "plan.published",
		Sequence:         1,
		CreatedAt:        time.Now().UTC(),
	}})
	if err == nil {
		t.Fatal("BuildDeploymentEvidenceBundle() accepted cross-organization evidence")
	}
}

func TestRedactAuditPayloadRemovesSecretsAndBoundsPayload(t *testing.T) {
	t.Parallel()

	payload := json.RawMessage(`{"authorization":"Bearer abc123","password":"secret-value","message":"` +
		strings.Repeat("x", 256) + `"}`)
	redacted, changed, truncated, err := RedactAuditPayload(payload, 96)
	if err != nil {
		t.Fatalf("RedactAuditPayload() error = %v", err)
	}
	if !changed || !truncated {
		t.Fatalf("expected changed and truncated, got changed=%v truncated=%v", changed, truncated)
	}
	if len(redacted) > 96 {
		t.Fatalf("payload length = %d, want <= 96", len(redacted))
	}
	if strings.Contains(string(redacted), "abc123") || strings.Contains(string(redacted), "secret-value") {
		t.Fatalf("redacted payload contains secret material: %s", redacted)
	}
	if !json.Valid(redacted) {
		t.Fatalf("redacted payload is not valid JSON: %s", redacted)
	}
}
