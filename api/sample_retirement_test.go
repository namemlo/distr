package api

import (
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestSampleRetirementPreviewRequestValidation(t *testing.T) {
	g := NewWithT(t)
	valid := SampleRetirementPreviewRequest{
		BackupReference:       "s3://immutable-backups/sample-job",
		BackupChecksum:        sampleRetirementTestChecksum("a"),
		RestoreProofReference: "evidence://restore/sample-job",
		RestoreProofChecksum:  sampleRetirementTestChecksum("b"),
		Items: []SampleRetirementSubject{{
			SubjectType:       types.SampleRetirementSubjectApplication,
			SubjectID:         uuid.New(),
			OwnershipMarker:   "tutorial-fixture",
			OwnershipChecksum: sampleRetirementTestChecksum("c"),
			ExpectedChecksum:  sampleRetirementTestChecksum("d"),
		}},
	}
	g.Expect(valid.Validate()).To(Succeed())

	tests := []struct {
		name   string
		mutate func(*SampleRetirementPreviewRequest)
	}{
		{"wildcard selector", func(r *SampleRetirementPreviewRequest) { r.Selector.Wildcard = "*" }},
		{"name selector", func(r *SampleRetirementPreviewRequest) { r.Selector.NamePattern = "demo-*" }},
		{"age selector", func(r *SampleRetirementPreviewRequest) { r.Selector.OlderThan = new(time.Now()) }},
		{"empty allowlist", func(r *SampleRetirementPreviewRequest) { r.Items = nil }},
		{"nil id", func(r *SampleRetirementPreviewRequest) { r.Items[0].SubjectID = uuid.Nil }},
		{"unsupported type", func(r *SampleRetirementPreviewRequest) { r.Items[0].SubjectType = "organization" }},
		{"missing marker", func(r *SampleRetirementPreviewRequest) { r.Items[0].OwnershipMarker = "" }},
		{"marker with newline", func(r *SampleRetirementPreviewRequest) {
			r.Items[0].OwnershipMarker = "tutorial\nfixture"
		}},
		{"bad ownership checksum", func(r *SampleRetirementPreviewRequest) {
			r.Items[0].OwnershipChecksum = "sha256:*"
		}},
		{"bad expected checksum", func(r *SampleRetirementPreviewRequest) {
			r.Items[0].ExpectedChecksum = "sha256:*"
		}},
		{"duplicate id", func(r *SampleRetirementPreviewRequest) { r.Items = append(r.Items, r.Items[0]) }},
		{"missing backup reference", func(r *SampleRetirementPreviewRequest) { r.BackupReference = "" }},
		{"oversized backup reference", func(r *SampleRetirementPreviewRequest) {
			r.BackupReference = "s3://bucket/" + strings.Repeat("a", 1013)
		}},
		{"bad backup checksum", func(r *SampleRetirementPreviewRequest) { r.BackupChecksum = "sha256:*" }},
		{"missing restore proof", func(r *SampleRetirementPreviewRequest) { r.RestoreProofReference = "" }},
		{"bad restore checksum", func(r *SampleRetirementPreviewRequest) { r.RestoreProofChecksum = "sha256:*" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := valid
			request.Items = append([]SampleRetirementSubject(nil), valid.Items...)
			test.mutate(&request)
			NewWithT(t).Expect(request.Validate()).NotTo(Succeed())
		})
	}
}

func TestApplySampleRetirementRequestValidation(t *testing.T) {
	g := NewWithT(t)
	valid := ApplySampleRetirementRequest{
		PreviewChecksum:  sampleRetirementTestChecksum("a"),
		ApprovalID:       "33333333-3333-4333-8333-333333333333",
		ApprovalChecksum: sampleRetirementTestChecksum("b"),
	}
	g.Expect(valid.Validate()).To(Succeed())

	valid.PreviewChecksum = "sha256:*"
	g.Expect(valid.Validate()).NotTo(Succeed())
	valid.PreviewChecksum = sampleRetirementTestChecksum("a")
	valid.ApprovalID = ""
	g.Expect(valid.Validate()).NotTo(Succeed())
	valid.ApprovalID = "approval id with spaces"
	g.Expect(valid.Validate()).NotTo(Succeed())
	valid.ApprovalID = "approval/sample-job"
	g.Expect(valid.Validate()).NotTo(Succeed())
	valid.ApprovalID = "33333333-3333-4333-8333-333333333333"
	valid.ApprovalChecksum = "sha256:*"
	g.Expect(valid.Validate()).NotTo(Succeed())
}

func TestSampleRetirementOwnershipEvidenceRegistrationValidation(t *testing.T) {
	g := NewWithT(t)
	valid := RegisterSampleRetirementOwnershipEvidenceRequest{
		SubjectType:       types.SampleRetirementSubjectApplication,
		SubjectID:         uuid.New(),
		OwnershipMarker:   "tutorial-fixture",
		OwnershipChecksum: sampleRetirementTestChecksum("a"),
		SourceReference:   "evidence://ownership/tutorial-fixture",
		SourceChecksum:    sampleRetirementTestChecksum("b"),
	}
	g.Expect(valid.Validate()).To(Succeed())

	tests := []struct {
		name   string
		mutate func(*RegisterSampleRetirementOwnershipEvidenceRequest)
	}{
		{"unsupported type", func(r *RegisterSampleRetirementOwnershipEvidenceRequest) {
			r.SubjectType = "organization"
		}},
		{"nil subject", func(r *RegisterSampleRetirementOwnershipEvidenceRequest) {
			r.SubjectID = uuid.Nil
		}},
		{"marker newline", func(r *RegisterSampleRetirementOwnershipEvidenceRequest) {
			r.OwnershipMarker = "tutorial\nfixture"
		}},
		{"bad ownership checksum", func(r *RegisterSampleRetirementOwnershipEvidenceRequest) {
			r.OwnershipChecksum = "sha256:*"
		}},
		{"oversized source reference", func(r *RegisterSampleRetirementOwnershipEvidenceRequest) {
			r.SourceReference = "s3://bucket/" + strings.Repeat("a", 1013)
		}},
		{"bad source checksum", func(r *RegisterSampleRetirementOwnershipEvidenceRequest) {
			r.SourceChecksum = "sha256:*"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := valid
			test.mutate(&request)
			NewWithT(t).Expect(request.Validate()).NotTo(Succeed())
		})
	}
}

func TestSampleRetirementRecoveryEvidenceRegistrationValidation(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, time.July, 28, 8, 0, 0, 0, time.UTC)
	valid := RegisterSampleRetirementRecoveryEvidenceRequest{
		EvidenceKind:   types.SampleRetirementRecoveryEvidenceBackup,
		Reference:      "s3://immutable-backups/sample-job",
		Checksum:       sampleRetirementTestChecksum("a"),
		SourceKind:     "backup_manifest",
		SourceID:       uuid.New(),
		SourceChecksum: sampleRetirementTestChecksum("b"),
		VerifiedAt:     now.Add(-time.Minute),
	}
	g.Expect(valid.Validate(now)).To(Succeed())

	tests := []struct {
		name   string
		mutate func(*RegisterSampleRetirementRecoveryEvidenceRequest)
	}{
		{"unsupported kind", func(r *RegisterSampleRetirementRecoveryEvidenceRequest) {
			r.EvidenceKind = "snapshot"
		}},
		{"oversized reference", func(r *RegisterSampleRetirementRecoveryEvidenceRequest) {
			r.Reference = "s3://bucket/" + strings.Repeat("a", 1013)
		}},
		{"bad checksum", func(r *RegisterSampleRetirementRecoveryEvidenceRequest) {
			r.Checksum = "sha256:*"
		}},
		{"invalid source kind", func(r *RegisterSampleRetirementRecoveryEvidenceRequest) {
			r.SourceKind = "Backup Manifest"
		}},
		{"nil source id", func(r *RegisterSampleRetirementRecoveryEvidenceRequest) {
			r.SourceID = uuid.Nil
		}},
		{"bad source checksum", func(r *RegisterSampleRetirementRecoveryEvidenceRequest) {
			r.SourceChecksum = "sha256:*"
		}},
		{"future verification", func(r *RegisterSampleRetirementRecoveryEvidenceRequest) {
			r.VerifiedAt = now.Add(time.Second)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := valid
			test.mutate(&request)
			NewWithT(t).Expect(request.Validate(now)).NotTo(Succeed())
		})
	}
}

func sampleRetirementTestChecksum(character string) string {
	return "sha256:" + strings.Repeat(character, 64)
}
