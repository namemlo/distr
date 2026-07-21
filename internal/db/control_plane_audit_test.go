package db

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

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
		ActorID:           uuid.New(),
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

func TestAuditExportErrorSummaryTruncatesAtUTF8Boundary(t *testing.T) {
	t.Parallel()

	summary := auditExportErrorSummary(errors.New(strings.Repeat("界", 1000)))
	if len(summary) > 2048 {
		t.Fatalf("multibyte export error summary length = %d, want <= 2048", len(summary))
	}
	if !utf8.ValidString(summary) {
		t.Fatal("multibyte export error summary contains invalid UTF-8")
	}
}

func TestAuditExportSinkCreatedEventCarriesActorAndTypedSinkCorrelation(t *testing.T) {
	t.Parallel()

	organizationID := uuid.New()
	actorID := uuid.New()
	sinkID := uuid.New()
	input := types.CreateAuditExportSinkInput{
		OrganizationID: organizationID,
		ActorID:        actorID,
		ConfigChecksum: "sha256:" + strings.Repeat("a", 64),
	}
	event := auditExportSinkCreatedEvent(input, types.AuditExportSink{ID: sinkID})

	if event.OrganizationID != organizationID || event.ActorID == nil || *event.ActorID != actorID {
		t.Fatalf("sink audit event lost actor scope: %#v", event)
	}
	if event.AuditExportSinkID == nil || *event.AuditExportSinkID != sinkID {
		t.Fatalf("sink audit event lost typed correlation: %#v", event)
	}
	if event.EventType != "audit_export_sink.created" ||
		event.Outcome != "SUCCEEDED" ||
		event.AuditExportConfigChecksum != input.ConfigChecksum {
		t.Fatalf("sink audit event lost immutable outcome: %#v", event)
	}
}

func TestControlPlaneAuditHookCanUseTransactionalOutboxAdapter(t *testing.T) {
	t.Parallel()

	input := types.ControlPlaneAuditEventInput{
		OrganizationID: uuid.New(),
		EventType:      "approval.decided",
		Outcome:        "SUCCEEDED",
		ApprovalID:     new(uuid.UUID),
	}
	*input.ApprovalID = uuid.New()
	var captured types.ControlPlaneAuditEventInput
	hook := ControlPlaneAuditAppendHookFunc(func(_ context.Context, value types.ControlPlaneAuditEventInput) error {
		captured = value
		return nil
	})
	if err := RecordControlPlaneAuditMutation(context.Background(), hook, input); err != nil {
		t.Fatalf("RecordControlPlaneAuditMutation() error = %v", err)
	}
	if captured.ApprovalID == nil || *captured.ApprovalID != *input.ApprovalID {
		t.Fatalf("outbox hook lost correlation: %#v", captured)
	}
}

func TestControlPlaneAuditedMutationRunsMutationAndHookInSameBoundary(t *testing.T) {
	t.Parallel()

	type boundaryKey struct{}
	runner := func(ctx context.Context, operation func(context.Context) error) error {
		return operation(context.WithValue(ctx, boundaryKey{}, "tx-1"))
	}
	mutationSawBoundary := false
	hookSawBoundary := false
	hook := ControlPlaneAuditAppendHookFunc(func(ctx context.Context, _ types.ControlPlaneAuditEventInput) error {
		hookSawBoundary = ctx.Value(boundaryKey{}) == "tx-1"
		return nil
	})
	err := runControlPlaneAuditedMutation(
		context.Background(),
		runner,
		hook,
		func(ctx context.Context) (types.ControlPlaneAuditEventInput, error) {
			mutationSawBoundary = ctx.Value(boundaryKey{}) == "tx-1"
			planID := uuid.New()
			return types.ControlPlaneAuditEventInput{
				OrganizationID:   uuid.New(),
				EventType:        "plan.created",
				Outcome:          "SUCCEEDED",
				DeploymentPlanID: &planID,
			}, nil
		},
	)
	if err != nil {
		t.Fatalf("runControlPlaneAuditedMutation() error = %v", err)
	}
	if !mutationSawBoundary || !hookSawBoundary {
		t.Fatalf("same boundary not preserved: mutation=%v hook=%v", mutationSawBoundary, hookSawBoundary)
	}
}

func TestMigration160DefinesTenantSafeCorrelationAndImmutableRetryHistory(t *testing.T) {
	t.Parallel()

	up, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "160_control_plane_audit_export.up.sql"))
	if err != nil {
		t.Fatalf("read migration up: %v", err)
	}
	down, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "160_control_plane_audit_export.down.sql"))
	if err != nil {
		t.Fatalf("read migration down: %v", err)
	}
	upSQL := string(up)
	for _, fragment := range []string{
		"CREATE TABLE ControlPlaneAuditSubject",
		"PRIMARY KEY (correlation_kind, subject_id)",
		"CREATE TABLE ControlPlaneAuditEventSubject",
		"FOREIGN KEY (event_id, organization_id)",
		"FOREIGN KEY (correlation_kind, subject_id, organization_id)",
		"status IN ('RUNNING', 'SUCCEEDED', 'FAILED')",
		"component_release_id UUID",
		"product_release_id UUID",
		"deployment_policy_id UUID",
		"maintenance_calendar_id UUID",
		"admission_decision_id UUID",
		"emergency_override_id UUID",
		"desired_state_id UUID",
		"drift_case_id UUID",
		"artifact_digest TEXT",
		"manifest_digest TEXT",
	} {
		if !strings.Contains(upSQL, fragment) {
			t.Fatalf("migration 160 missing %q", fragment)
		}
	}
	if !strings.Contains(string(down), "DROP TABLE IF EXISTS ControlPlaneAuditEventSubject") ||
		!strings.Contains(string(down), "DROP TABLE IF EXISTS ControlPlaneAuditSubject") {
		t.Fatal("migration 160 down does not remove correlation graph after refusal guard")
	}
}

func TestMigration160RollbackLocksAndRefusesAllOwnedEvidence(t *testing.T) {
	t.Parallel()

	down, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "160_control_plane_audit_export.down.sql"))
	if err != nil {
		t.Fatalf("read migration down: %v", err)
	}
	downSQL := string(down)
	lockIndex := strings.Index(downSQL, "LOCK TABLE")
	checkIndex := strings.Index(downSQL, "IF EXISTS")
	if lockIndex < 0 || checkIndex < 0 || lockIndex > checkIndex {
		t.Fatal("migration 160 down must lock owned tables before retained-data checks")
	}
	if !strings.Contains(downSQL[:checkIndex], "IN ACCESS EXCLUSIVE MODE") {
		t.Fatal("migration 160 down must hold an ACCESS EXCLUSIVE lock through refusal checks and drops")
	}
	for _, table := range []string{
		"ControlPlaneAuditEvent",
		"ControlPlaneAuditSubject",
		"ControlPlaneAuditEventSubject",
		"AuditExportSink",
		"AuditExportCheckpoint",
		"AuditExportAttempt",
	} {
		if !strings.Contains(downSQL[:checkIndex], table) {
			t.Fatalf("migration 160 down does not lock %s before retained-data checks", table)
		}
		if !strings.Contains(downSQL[checkIndex:], "SELECT 1 FROM "+table) {
			t.Fatalf("migration 160 down does not refuse retained data in %s", table)
		}
	}
}

func TestAuditExportAttemptLeaseRecoversCrashAfterStart(t *testing.T) {
	t.Parallel()

	up, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "160_control_plane_audit_export.up.sql"))
	if err != nil {
		t.Fatalf("read migration up: %v", err)
	}
	if !strings.Contains(string(up), "lease_expires_at TIMESTAMPTZ NOT NULL") {
		t.Fatal("migration 160 does not persist the audit export attempt lease")
	}

	source, err := os.ReadFile("control_plane_audit.go")
	if err != nil {
		t.Fatalf("read control-plane audit repository: %v", err)
	}
	sourceText := string(source)
	for _, fragment := range []string{
		"lease_expires_at <= now()",
		"SET status = 'FAILED'",
		"audit export attempt lease expired",
		"lease_expires_at",
		"make_interval(secs => @leaseSeconds)",
		"AND a.lease_expires_at > now()",
	} {
		if !strings.Contains(sourceText, fragment) {
			t.Fatalf("audit export stale-attempt recovery missing %q", fragment)
		}
	}
}

func TestControlPlaneAuditSelectColumnsCoverPersistedEventShape(t *testing.T) {
	t.Parallel()

	eventType := reflect.TypeOf(types.ControlPlaneAuditEvent{})
	for i := 0; i < eventType.NumField(); i++ {
		column := eventType.Field(i).Tag.Get("db")
		if column == "" || column == "-" {
			continue
		}
		if !strings.Contains(controlPlaneAuditEventColumns, column) {
			t.Fatalf("controlPlaneAuditEventColumns missing %q for %s", column, eventType.Field(i).Name)
		}
	}
}
