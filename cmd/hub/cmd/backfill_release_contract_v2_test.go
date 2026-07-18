package cmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestBackfillReleaseContractV2DefaultsToReadOnlyDryRun(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	var captured types.ReleaseBackfillRequest
	var stdout bytes.Buffer
	command := newBackfillReleaseContractV2Command(backfillReleaseContractV2Runtime{
		Stdout: &stdout,
		Run: func(_ context.Context, request types.ReleaseBackfillRequest) (*types.ReleaseBackfillReport, error) {
			captured = request
			return &types.ReleaseBackfillReport{
				DryRun: true, Scanned: 2, Eligible: 1, WouldDerive: 1, Blocked: 1,
			}, nil
		},
	})
	command.SetArgs([]string{"--organization-id", organizationID.String()})

	g.Expect(command.Execute()).To(Succeed())
	g.Expect(captured.OrganizationID).To(Equal(organizationID))
	g.Expect(captured.Apply).To(BeFalse())
	g.Expect(captured.BatchSize).To(Equal(100))
	g.Expect(captured.CheckpointID).To(Equal(uuid.Nil))
	g.Expect(stdout.String()).To(ContainSubstring("dryRun=true"))
	g.Expect(stdout.String()).To(ContainSubstring("wouldDerive=1"))
	g.Expect(stdout.String()).To(ContainSubstring("derived=0"))
	g.Expect(stdout.String()).To(ContainSubstring("blocked=1"))
}

func TestBackfillReleaseContractV2ParsesBoundedArtifactEvidenceFile(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	sourceReleaseID := uuid.New()
	digest := "sha256:" + strings.Repeat("a", 64)
	evidenceFile := filepath.Join(t.TempDir(), "artifact-evidence.json")
	documentReference := "review://backfill/choice-tp-dev/2026-07-18"
	rawEvidence := []byte(`{
		"schema":"distr.release-backfill-artifact-evidence/v1",
		"reference":"` + documentReference + `",
		"evidence":[{
			"sourceReleaseBundleId":"` + sourceReleaseID.String() + `",
			"artifactKey":"service",
			"artifactDigest":"` + digest + `",
			"mediaType":"application/vnd.oci.image.index.v1+json",
			"reference":"review://manifest/` + sourceReleaseID.String() + `",
			"evidenceDigest":"sha256:` + strings.Repeat("e", 64) + `"
		}]
	}`)
	g.Expect(os.WriteFile(evidenceFile, rawEvidence, 0o600)).To(Succeed())
	var captured types.ReleaseBackfillRequest
	command := newBackfillReleaseContractV2Command(backfillReleaseContractV2Runtime{
		Stdout: &bytes.Buffer{},
		Run: func(_ context.Context, request types.ReleaseBackfillRequest) (*types.ReleaseBackfillReport, error) {
			captured = request
			return &types.ReleaseBackfillReport{DryRun: true}, nil
		},
	})
	command.SetArgs([]string{
		"--organization-id", organizationID.String(),
		"--artifact-evidence-file", evidenceFile,
	})

	g.Expect(command.Execute()).To(Succeed())
	g.Expect(captured.ArtifactEvidence).To(HaveLen(1))
	g.Expect(captured.ArtifactEvidence[0].SourceReleaseBundleID).To(Equal(sourceReleaseID))
	g.Expect(captured.ArtifactEvidence[0].ArtifactDigest).To(Equal(digest))
	g.Expect(captured.ArtifactEvidence[0].MediaType).
		To(Equal("application/vnd.oci.image.index.v1+json"))
	g.Expect(captured.ArtifactEvidence[0].EvidenceDigest).
		To(Equal("sha256:" + strings.Repeat("e", 64)))
	g.Expect(captured.EvidenceDocumentReference).To(Equal(documentReference))
	g.Expect(captured.EvidenceDocumentChecksum).
		To(Equal(fmt.Sprintf("sha256:%x", sha256.Sum256(rawEvidence))))
}

func TestReadReleaseBackfillArtifactEvidenceRejectsAmbiguousOrOversizedInput(t *testing.T) {
	g := NewWithT(t)
	ambiguous := filepath.Join(t.TempDir(), "ambiguous.json")
	g.Expect(os.WriteFile(
		ambiguous,
		[]byte(`{"schema":"distr.release-backfill-artifact-evidence/v1","schema":"other","evidence":[]}`),
		0o600,
	)).To(Succeed())
	_, err := readReleaseBackfillArtifactEvidence(ambiguous)
	g.Expect(err).To(MatchError(ContainSubstring("unambiguous JSON document")))

	oversized := filepath.Join(t.TempDir(), "oversized.json")
	g.Expect(os.WriteFile(
		oversized,
		bytes.Repeat([]byte("x"), maxReleaseBackfillArtifactEvidenceBytes+1),
		0o600,
	)).To(Succeed())
	_, err = readReleaseBackfillArtifactEvidence(oversized)
	g.Expect(err).To(MatchError(ContainSubstring("no larger than 1 MiB")))
}

func TestBackfillReleaseContractV2ApplyRequiresStableCheckpointAndParsesCursor(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	checkpointID := uuid.New()
	cursorID := uuid.New()
	cursorTime := time.Date(2026, 7, 18, 10, 11, 12, 123, time.UTC)
	var captured types.ReleaseBackfillRequest
	command := newBackfillReleaseContractV2Command(backfillReleaseContractV2Runtime{
		Stdout: &bytes.Buffer{},
		Run: func(_ context.Context, request types.ReleaseBackfillRequest) (*types.ReleaseBackfillReport, error) {
			captured = request
			return &types.ReleaseBackfillReport{
				CheckpointID: request.CheckpointID, DryRun: false, LastCursor: request.Cursor,
			}, nil
		},
	})
	command.SetArgs([]string{
		"--organization-id", organizationID.String(),
		"--apply",
		"--checkpoint-id", checkpointID.String(),
		"--cursor-created-at", cursorTime.Format(time.RFC3339Nano),
		"--cursor-release-bundle-id", cursorID.String(),
	})

	g.Expect(command.Execute()).To(Succeed())
	g.Expect(captured.Apply).To(BeTrue())
	g.Expect(captured.CheckpointID).To(Equal(checkpointID))
	g.Expect(captured.Cursor).NotTo(BeNil())
	g.Expect(captured.Cursor.CreatedAt).To(Equal(cursorTime))
	g.Expect(captured.Cursor.ReleaseBundleID).To(Equal(cursorID))
}

func TestBackfillReleaseContractV2RejectsUnsafeApplyAndPartialCursor(t *testing.T) {
	organizationID := uuid.NewString()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "apply without checkpoint",
			args: []string{"--organization-id", organizationID, "--apply"},
			want: "checkpoint-id is required",
		},
		{
			name: "partial cursor",
			args: []string{"--organization-id", organizationID, "--cursor-created-at", time.Now().Format(time.RFC3339Nano)},
			want: "must be provided together",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			command := newBackfillReleaseContractV2Command(backfillReleaseContractV2Runtime{
				Stdout: &bytes.Buffer{},
				Run: func(context.Context, types.ReleaseBackfillRequest) (*types.ReleaseBackfillReport, error) {
					t.Fatal("runtime must not run for invalid options")
					return nil, nil
				},
			})
			command.SetArgs(tt.args)
			err := command.Execute()
			g.Expect(err).To(HaveOccurred())
			g.Expect(strings.ToLower(err.Error())).To(ContainSubstring(strings.ToLower(tt.want)))
		})
	}
}

func TestBackfillReleaseContractV2PrintsSafeResumeCursorOnFailure(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	checkpointID := uuid.New()
	cursor := &types.ReleaseBackfillCursor{
		CreatedAt:       time.Date(2026, 7, 18, 11, 12, 13, 0, time.UTC),
		ReleaseBundleID: uuid.New(),
	}
	var stdout bytes.Buffer
	command := newBackfillReleaseContractV2Command(backfillReleaseContractV2Runtime{
		Stdout: &stdout,
		Run: func(context.Context, types.ReleaseBackfillRequest) (*types.ReleaseBackfillReport, error) {
			return &types.ReleaseBackfillReport{
				CheckpointID:     checkpointID,
				DryRun:           false,
				Scanned:          2,
				Derived:          1,
				AwaitingEvidence: 1,
				Failed:           1,
				LastCursor:       cursor,
				NextCursor:       cursor,
			}, errors.New("apply failed")
		},
	})
	command.SetArgs([]string{
		"--organization-id", organizationID.String(),
		"--checkpoint-id", checkpointID.String(),
		"--apply",
	})

	err := command.Execute()

	g.Expect(err).To(MatchError("apply failed"))
	g.Expect(stdout.String()).To(ContainSubstring("failed=1"))
	g.Expect(stdout.String()).To(ContainSubstring("awaitingEvidence=1"))
	g.Expect(stdout.String()).To(ContainSubstring("nextCursor=" + formatReleaseBackfillCursor(cursor)))
	g.Expect(stdout.String()).To(ContainSubstring(formatReleaseBackfillCursor(cursor)))
}
