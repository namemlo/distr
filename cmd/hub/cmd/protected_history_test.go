package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/protectedhistory"
)

func TestProtectedHistoryExportCommandWritesSealedArtifact(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	var writtenPath string
	var writtenPayload []byte
	command := newProtectedHistoryCommand(protectedHistoryRuntime{
		Stdout: &stdout,
		Export: func(_ context.Context, scope protectedhistory.Scope) (*protectedhistory.Artifact, error) {
			return protectedhistory.Build(scope, 138, []protectedhistory.RawRecord{{
				Kind: "task", ID: "55555555-5555-4555-8555-555555555555",
				Payload: json.RawMessage(`{"status":"SUCCEEDED"}`),
			}})
		},
		Write: func(path string, payload []byte) error {
			writtenPath = path
			writtenPayload = append([]byte(nil), payload...)
			return nil
		},
	})
	command.SetArgs([]string{
		"export",
		"--organization-id", "11111111-1111-4111-8111-111111111111",
		"--customer-organization-id", "22222222-2222-4222-8222-222222222222",
		"--deployment-target-id", "33333333-3333-4333-8333-333333333333",
		"--output", "baseline.json",
	})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if writtenPath != "baseline.json" {
		t.Fatalf("unexpected output path %q", writtenPath)
	}
	artifact, err := protectedhistory.Parse(writtenPayload)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.RecordCount != 1 || !strings.Contains(stdout.String(), artifact.ArtifactID) {
		t.Fatalf("unexpected command output %q", stdout.String())
	}
}

func TestProtectedHistoryCompareCommandReturnsErrorForViolation(t *testing.T) {
	t.Parallel()
	baseline, err := protectedhistory.Build(testProtectedHistoryScope(), 138, []protectedhistory.RawRecord{{
		Kind: "task", ID: "55555555-5555-4555-8555-555555555555",
		Payload: json.RawMessage(`{"status":"SUCCEEDED"}`),
	}})
	if err != nil {
		t.Fatal(err)
	}
	current, err := protectedhistory.Build(testProtectedHistoryScope(), 164, nil)
	if err != nil {
		t.Fatal(err)
	}
	baselinePayload, _ := protectedhistory.Marshal(*baseline)
	currentPayload, _ := protectedhistory.Marshal(*current)
	var stdout bytes.Buffer
	command := newProtectedHistoryCommand(protectedHistoryRuntime{
		Stdout: &stdout,
		Read: func(path string) ([]byte, error) {
			switch path {
			case "baseline.json":
				return baselinePayload, nil
			case "current.json":
				return currentPayload, nil
			default:
				return nil, errors.New("unexpected path")
			}
		},
	})
	command.SetArgs([]string{"compare", "--baseline", "baseline.json", "--current", "current.json"})
	err = command.Execute()
	if err == nil || !strings.Contains(err.Error(), "violation") {
		t.Fatalf("expected violation error, got %v", err)
	}
	if !strings.Contains(stdout.String(), `"status": "VIOLATION"`) ||
		!strings.Contains(stdout.String(), `"type": "MISSING"`) {
		t.Fatalf("comparison report was not emitted: %s", stdout.String())
	}
}

func testProtectedHistoryScope() protectedhistory.Scope {
	return protectedhistory.Scope{
		OrganizationID:          "11111111-1111-4111-8111-111111111111",
		CustomerOrganizationIDs: []string{"22222222-2222-4222-8222-222222222222"},
		DeploymentTargetIDs:     []string{"33333333-3333-4333-8333-333333333333"},
	}
}
