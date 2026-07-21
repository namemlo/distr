package mapping

import (
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

func TestControlPlaneAuditEventToAPIPreservesCorrelation(t *testing.T) {
	t.Parallel()

	planID := uuid.New()
	event := ControlPlaneAuditEventToAPI(types.ControlPlaneAuditEvent{
		ID:                     uuid.New(),
		OrganizationID:         uuid.New(),
		Sequence:               42,
		EventType:              "plan.published",
		DeploymentPlanID:       &planID,
		DeploymentPlanChecksum: "sha256:test",
		PayloadRedacted:        true,
	})
	if event.Sequence != 42 || event.DeploymentPlanID == nil || *event.DeploymentPlanID != planID {
		t.Fatalf("mapping lost correlation: %#v", event)
	}
	if !event.PayloadRedacted || event.DeploymentPlanChecksum != "sha256:test" {
		t.Fatalf("mapping lost evidence flags: %#v", event)
	}
}

func TestControlPlaneAuditEventToAPIPreservesExpandedTypedCorrelation(t *testing.T) {
	t.Parallel()

	componentReleaseID := uuid.New()
	productReleaseID := uuid.New()
	policyID := uuid.New()
	driftCaseID := uuid.New()
	event := ControlPlaneAuditEventToAPI(types.ControlPlaneAuditEvent{
		ComponentReleaseID:        &componentReleaseID,
		ProductReleaseID:          &productReleaseID,
		DeploymentPolicyID:        &policyID,
		DriftCaseID:               &driftCaseID,
		ArtifactDigest:            "sha256:" + strings.Repeat("a", 64),
		ManifestDigest:            "sha256:" + strings.Repeat("b", 64),
		AuditExportConfigChecksum: "sha256:" + strings.Repeat("c", 64),
	})
	if event.ComponentReleaseID == nil || event.ProductReleaseID == nil ||
		event.DeploymentPolicyID == nil || event.DriftCaseID == nil {
		t.Fatalf("mapping lost typed correlation: %#v", event)
	}
	if event.ArtifactDigest == "" || event.ManifestDigest == "" || event.AuditExportConfigChecksum == "" {
		t.Fatalf("mapping lost digest evidence: %#v", event)
	}
}

func TestAuditExportStatusToAPIPreservesLagAndFailureEvidence(t *testing.T) {
	t.Parallel()

	completedAt := time.Date(2026, 7, 18, 3, 0, 0, 0, time.UTC)
	status := AuditExportStatusToAPI(types.AuditExportStatus{
		Sink: types.AuditExportSink{
			ID:                  uuid.New(),
			Name:                "Archive",
			Kind:                types.AuditExportSinkKindObjectStore,
			EndpointReference:   "secret://audit/archive",
			ConsecutiveFailures: 2,
		},
		LastExportedSequence:   10,
		LatestSequence:         14,
		CheckpointLag:          4,
		Alert:                  true,
		LastAttemptStatus:      "FAILED",
		LastAttemptError:       "sink unavailable",
		LastAttemptCompletedAt: &completedAt,
	})

	if status.Sink.ID == uuid.Nil || status.CheckpointLag != 4 || !status.Alert {
		t.Fatalf("mapping lost sink status: %#v", status)
	}
	if status.LastAttemptStatus != "FAILED" || status.LastAttemptCompletedAt == nil {
		t.Fatalf("mapping lost failure evidence: %#v", status)
	}
}
