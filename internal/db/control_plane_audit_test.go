package db

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

func TestAppendControlPlaneAuditEventRejectsInvalidBoundaryBeforeDatabaseAccess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input types.ControlPlaneAuditEventInput
		want  string
	}{
		{
			name:  "organization",
			input: types.ControlPlaneAuditEventInput{EventType: "plan.published", Outcome: "SUCCEEDED"},
			want:  "organization",
		},
		{
			name: "event type",
			input: types.ControlPlaneAuditEventInput{
				OrganizationID: uuid.New(),
				Outcome:        "SUCCEEDED",
			},
			want: "event type",
		},
		{
			name: "outcome",
			input: types.ControlPlaneAuditEventInput{
				OrganizationID: uuid.New(),
				EventType:      "plan.published",
			},
			want: "outcome",
		},
		{
			name: "invalid payload",
			input: types.ControlPlaneAuditEventInput{
				OrganizationID: uuid.New(),
				EventType:      "plan.published",
				Outcome:        "SUCCEEDED",
				Payload:        json.RawMessage(`{`),
			},
			want: "payload",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := AppendControlPlaneAuditEvent(context.Background(), test.input)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), test.want) {
				t.Fatalf("AppendControlPlaneAuditEvent() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestControlPlaneAuditEventRequiresCorrelationAndCanonicalChecksums(t *testing.T) {
	t.Parallel()

	input := types.ControlPlaneAuditEventInput{
		OrganizationID: uuid.New(),
		EventType:      "plan.published",
		Outcome:        "SUCCEEDED",
	}
	if err := validateControlPlaneAuditEventInput(input); err == nil ||
		!strings.Contains(err.Error(), "correlation") {
		t.Fatalf("validation error = %v, want correlation", err)
	}

	planID := uuid.New()
	input.DeploymentPlanID = &planID
	input.DeploymentPlanChecksum = "sha256:not-a-digest"
	if err := validateControlPlaneAuditEventInput(input); err == nil ||
		!strings.Contains(err.Error(), "checksum") {
		t.Fatalf("validation error = %v, want checksum", err)
	}
}

func TestCreateAuditExportSinkRejectsUnsafePersistenceBoundary(t *testing.T) {
	t.Parallel()

	valid := types.CreateAuditExportSinkInput{
		OrganizationID:    uuid.New(),
		Name:              "Security archive",
		Kind:              types.AuditExportSinkKindSIEM,
		EndpointReference: "secret://audit/siem-endpoint",
		ConfigChecksum:    "sha256:" + strings.Repeat("a", 64),
		Enabled:           true,
	}
	if err := validateCreateAuditExportSinkInput(valid); err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}

	unsafe := valid
	unsafe.EndpointReference = "secret://audit/endpoint\nAuthorization: Bearer secret"
	if err := validateCreateAuditExportSinkInput(unsafe); err == nil {
		t.Fatal("unsafe endpoint reference accepted")
	}
}

func TestAuditExportAttemptKeyIsStableAndBatchSpecific(t *testing.T) {
	t.Parallel()

	sinkID := uuid.New()
	first := auditExportAttemptKey(sinkID, 1, 4, 4, "FAILED")
	repeated := auditExportAttemptKey(sinkID, 1, 4, 4, "FAILED")
	succeeded := auditExportAttemptKey(sinkID, 1, 4, 4, "SUCCEEDED")

	if first != repeated {
		t.Fatalf("same batch produced different keys: %q != %q", first, repeated)
	}
	if first == succeeded || !strings.HasPrefix(first, "sha256:") || len(first) != 71 {
		t.Fatalf("attempt key is not status-specific SHA-256: failed=%q succeeded=%q", first, succeeded)
	}
}

func TestAuditExportErrorSummaryIsBoundedAndRedacted(t *testing.T) {
	t.Parallel()

	summary := auditExportErrorSummary(errors.New(
		"sink failed\nAuthorization: Bearer abc123 " + strings.Repeat("x", 4096),
	))
	if strings.Contains(summary, "abc123") || strings.ContainsAny(summary, "\r\n") {
		t.Fatalf("unsafe export error summary: %q", summary)
	}
	if len(summary) > 2048 {
		t.Fatalf("export error summary length = %d, want <= 2048", len(summary))
	}
}
