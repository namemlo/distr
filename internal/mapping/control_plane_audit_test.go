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
	executionAttemptID := uuid.New()
	event := ControlPlaneAuditEventToAPI(types.ControlPlaneAuditEvent{
		ComponentReleaseID:        &componentReleaseID,
		ProductReleaseID:          &productReleaseID,
		DeploymentPolicyID:        &policyID,
		DriftCaseID:               &driftCaseID,
		ExecutionAttemptID:        &executionAttemptID,
		ArtifactDigest:            "sha256:" + strings.Repeat("a", 64),
		ManifestDigest:            "sha256:" + strings.Repeat("b", 64),
		AuditExportConfigChecksum: "sha256:" + strings.Repeat("c", 64),
	})
	if event.ComponentReleaseID == nil || event.ProductReleaseID == nil ||
		event.DeploymentPolicyID == nil || event.DriftCaseID == nil ||
		event.ExecutionAttemptID == nil || *event.ExecutionAttemptID != executionAttemptID {
		t.Fatalf("mapping lost typed correlation: %#v", event)
	}
	if event.ArtifactDigest == "" || event.ManifestDigest == "" || event.AuditExportConfigChecksum == "" {
		t.Fatalf("mapping lost digest evidence: %#v", event)
	}
}

func TestControlPlaneAuditEventToAPIPreservesCampaignTypedCorrelations(t *testing.T) {
	t.Parallel()

	revisionID := uuid.New()
	runID := uuid.New()
	waveDefinitionID := uuid.New()
	waveRunID := uuid.New()
	memberID := uuid.New()
	memberRunID := uuid.New()
	controlID := uuid.New()
	event := ControlPlaneAuditEventToAPI(types.ControlPlaneAuditEvent{
		CampaignRevisionID:       &revisionID,
		CampaignRunID:            &runID,
		CampaignWaveDefinitionID: &waveDefinitionID,
		CampaignWaveRunID:        &waveRunID,
		CampaignMemberID:         &memberID,
		CampaignMemberRunID:      &memberRunID,
		CampaignControlRequestID: &controlID,
		CampaignRevisionChecksum: "sha256:" + strings.Repeat("d", 64),
		CampaignControlChecksum:  "sha256:" + strings.Repeat("e", 64),
	})

	if event.CampaignRevisionID == nil || event.CampaignRunID == nil ||
		event.CampaignWaveDefinitionID == nil || event.CampaignWaveRunID == nil ||
		event.CampaignMemberID == nil || event.CampaignMemberRunID == nil ||
		event.CampaignControlRequestID == nil {
		t.Fatalf("mapping lost typed campaign correlation: %#v", event)
	}
	if event.CampaignRevisionChecksum == "" || event.CampaignControlChecksum == "" {
		t.Fatalf("mapping lost campaign checksum evidence: %#v", event)
	}
}

func TestEvidenceBundleToAPIPreservesCanonicalVersion(t *testing.T) {
	t.Parallel()

	planID := uuid.New()
	bundle := EvidenceBundleToAPI(types.EvidenceBundle{
		Version:          types.EvidenceBundleSchemaV1,
		DeploymentPlanID: planID,
		Checksum:         "sha256:" + strings.Repeat("f", 64),
	})
	if bundle.Version != types.EvidenceBundleSchemaV1 || bundle.DeploymentPlanID != planID ||
		bundle.Checksum == "" {
		t.Fatalf("mapping lost versioned canonical evidence: %#v", bundle)
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
