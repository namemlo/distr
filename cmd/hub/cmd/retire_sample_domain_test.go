package cmd

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestSampleRetirementCLIPreviewSendsExactAllowlistAndRestoreProof(t *testing.T) {
	g := NewWithT(t)
	subjectID := uuid.NewString()
	backupChecksum := "sha256:" + strings.Repeat("a", 64)
	restoreChecksum := "sha256:" + strings.Repeat("b", 64)
	ownershipChecksum := "sha256:" + strings.Repeat("c", 64)
	expectedChecksum := "sha256:" + strings.Repeat("d", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.Expect(r.Method).To(Equal(http.MethodPost))
		g.Expect(r.URL.Path).To(Equal("/api/v1/sample-retirements/preview"))
		g.Expect(r.Header.Get("Authorization")).To(Equal("AccessToken test-token"))
		body, err := io.ReadAll(r.Body)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(string(body)).To(MatchJSON(`{
			"items": [{
				"subjectType": "deployment_target",
				"subjectId": "` + subjectID + `",
				"ownershipMarker": "tutorial-fixture",
				"ownershipChecksum": "` + ownershipChecksum + `",
				"expectedChecksum": "` + expectedChecksum + `"
			}],
			"backupReference": "backup:evidence-42",
			"backupChecksum": "` + backupChecksum + `",
			"restoreProofReference": "restore:evidence-42",
			"restoreProofChecksum": "` + restoreChecksum + `"
		}`))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"11111111-1111-4111-8111-111111111111",
			"previewChecksum":"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
		}`))
	}))
	t.Cleanup(server.Close)

	stdout, stderr, err := executeRetireSampleDomainCommandForTest(
		t,
		sampleRetirementCommandRuntime{Client: http.DefaultClient},
		"--server", server.URL,
		"--token", "test-token",
		"preview",
		"--item", strings.Join([]string{
			"deployment_target",
			subjectID,
			"tutorial-fixture",
			ownershipChecksum,
			expectedChecksum,
		}, ","),
		"--backup-reference", "backup:evidence-42",
		"--backup-checksum", backupChecksum,
		"--restore-reference", "restore:evidence-42",
		"--restore-checksum", restoreChecksum,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(stderr).To(BeEmpty())
	g.Expect(stdout).To(MatchJSON(`{
		"id":"11111111-1111-4111-8111-111111111111",
		"previewChecksum":"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	}`))
}

func TestSampleRetirementCLIApplyBindsPreviewChecksumAndApproval(t *testing.T) {
	g := NewWithT(t)
	jobID := uuid.NewString()
	previewChecksum := "sha256:" + strings.Repeat("e", 64)
	approvalChecksum := "sha256:" + strings.Repeat("a", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.Expect(r.Method).To(Equal(http.MethodPost))
		g.Expect(r.URL.Path).To(Equal("/api/v1/sample-retirements/" + jobID + "/apply"))
		body, err := io.ReadAll(r.Body)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(string(body)).To(MatchJSON(`{
			"previewChecksum": "` + previewChecksum + `",
			"approvalId": "approval-42",
			"approvalChecksum": "` + approvalChecksum + `"
		}`))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"jobId":"` + jobID + `",
			"previewChecksum":"` + previewChecksum + `",
			"state":"APPLIED",
			"appliedCount":1,
			"skippedCount":0,
			"tombstoneCount":1,
			"checkpointSequence":1,
			"noOp":false,
			"completedAt":"2026-07-28T10:00:00Z"
		}`))
	}))
	t.Cleanup(server.Close)

	stdout, stderr, err := executeRetireSampleDomainCommandForTest(
		t,
		sampleRetirementCommandRuntime{Client: http.DefaultClient},
		"--server", server.URL,
		"--token", "test-token",
		"apply", jobID,
		"--preview-checksum", previewChecksum,
		"--approval-id", "approval-42",
		"--approval-checksum", approvalChecksum,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(stderr).To(BeEmpty())
	g.Expect(stdout).To(MatchJSON(`{
		"jobId":"` + jobID + `",
		"previewChecksum":"` + previewChecksum + `",
		"state":"APPLIED",
		"appliedCount":1,
		"skippedCount":0,
		"tombstoneCount":1,
		"checkpointSequence":1,
		"noOp":false,
		"completedAt":"2026-07-28T10:00:00Z"
	}`))
}

func TestSampleRetirementCLIVerifyUsesExactJobID(t *testing.T) {
	g := NewWithT(t)
	jobID := uuid.NewString()
	previewChecksum := "sha256:" + strings.Repeat("f", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.Expect(r.Method).To(Equal(http.MethodPost))
		g.Expect(r.URL.Path).To(Equal("/api/v1/sample-retirements/" + jobID + "/verify"))
		body, err := io.ReadAll(r.Body)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(string(body)).To(MatchJSON(`{}`))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"jobId":"` + jobID + `",
			"state":"VERIFIED",
			"previewChecksum":"` + previewChecksum + `",
			"exactCounts":true,
			"tombstoneLineageValid":true,
			"auditEventsRetained":true,
			"remainingSubjectCount":0,
			"auditEventCount":2,
			"appliedCount":1,
			"tombstoneCount":1,
			"verifiedAt":"2026-07-28T10:01:00Z",
			"problems":[]
		}`))
	}))
	t.Cleanup(server.Close)

	stdout, stderr, err := executeRetireSampleDomainCommandForTest(
		t,
		sampleRetirementCommandRuntime{Client: http.DefaultClient},
		"--server", server.URL,
		"--token", "test-token",
		"verify", jobID,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(stderr).To(BeEmpty())
	g.Expect(stdout).To(MatchJSON(`{
		"jobId":"` + jobID + `",
		"state":"VERIFIED",
		"previewChecksum":"` + previewChecksum + `",
		"exactCounts":true,
		"tombstoneLineageValid":true,
		"auditEventsRetained":true,
		"remainingSubjectCount":0,
		"auditEventCount":2,
		"appliedCount":1,
		"tombstoneCount":1,
		"verifiedAt":"2026-07-28T10:01:00Z",
		"problems":[]
	}`))
}

func TestSampleRetirementCLIRejectsBroadSelectorsBeforeRequest(t *testing.T) {
	for _, selector := range []string{
		"--wildcard",
		"--name-pattern",
		"--older-than",
		"--approval-id",
	} {
		t.Run(selector, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				requests++
			}))
			t.Cleanup(server.Close)

			_, _, err := executeRetireSampleDomainCommandForTest(
				t,
				sampleRetirementCommandRuntime{Client: http.DefaultClient},
				"--server", server.URL,
				"--token", "test-token",
				"preview",
				selector, "*",
			)

			g := NewWithT(t)
			g.Expect(err).To(HaveOccurred())
			g.Expect(releaseExitCodeForTest(err)).To(Equal(releaseExitUsage))
			g.Expect(requests).To(Equal(0))
		})
	}
}

func TestSampleRetirementCLIPreviewRequiresExactItemAndRestoreEvidence(t *testing.T) {
	checksum := "sha256:" + strings.Repeat("a", 64)
	validItem := strings.Join([]string{
		"application",
		uuid.NewString(),
		"tutorial-fixture",
		checksum,
		checksum,
	}, ",")
	baseArgs := []string{
		"--server", "https://distr.example.invalid",
		"--token", "test-token",
		"preview",
		"--item", validItem,
		"--backup-reference", "backup:evidence-42",
		"--backup-checksum", checksum,
		"--restore-reference", "restore:evidence-42",
		"--restore-checksum", checksum,
	}
	tests := []struct {
		name       string
		removeFlag string
		replace    map[string]string
	}{
		{name: "backup reference", removeFlag: "--backup-reference"},
		{name: "backup checksum", removeFlag: "--backup-checksum"},
		{name: "restore reference", removeFlag: "--restore-reference"},
		{name: "restore checksum", removeFlag: "--restore-checksum"},
		{
			name:    "exact UUID",
			replace: map[string]string{validItem: "application,*,tutorial-fixture," + checksum + "," + checksum},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			args := make([]string, 0, len(baseArgs))
			for index := 0; index < len(baseArgs); index++ {
				if baseArgs[index] == test.removeFlag {
					index++
					continue
				}
				value := baseArgs[index]
				if replacement, ok := test.replace[value]; ok {
					value = replacement
				}
				args = append(args, value)
			}

			_, _, err := executeRetireSampleDomainCommandForTest(
				t,
				sampleRetirementCommandRuntime{},
				args...,
			)

			g := NewWithT(t)
			g.Expect(err).To(HaveOccurred())
			g.Expect(releaseExitCodeForTest(err)).To(Equal(releaseExitUsage))
		})
	}
}

func TestSampleRetirementCLIApplyRequiresEveryApprovalBindingBeforeRequest(t *testing.T) {
	jobID := uuid.NewString()
	checksum := "sha256:" + strings.Repeat("a", 64)
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "preview checksum",
			args: []string{"apply", jobID, "--approval-id", "approval-42", "--approval-checksum", checksum},
		},
		{
			name: "approval id",
			args: []string{"apply", jobID, "--preview-checksum", checksum, "--approval-checksum", checksum},
		},
		{
			name: "approval checksum",
			args: []string{"apply", jobID, "--preview-checksum", checksum, "--approval-id", "approval-42"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				requests++
			}))
			t.Cleanup(server.Close)
			args := append(
				[]string{"--server", server.URL, "--token", "test-token"},
				test.args...,
			)

			_, _, err := executeRetireSampleDomainCommandForTest(
				t,
				sampleRetirementCommandRuntime{Client: http.DefaultClient},
				args...,
			)

			g := NewWithT(t)
			g.Expect(err).To(HaveOccurred())
			g.Expect(releaseExitCodeForTest(err)).To(Equal(releaseExitUsage))
			g.Expect(requests).To(Equal(0))
		})
	}
}

func TestSampleRetirementCLIRejectsInvalidJobIDBeforeRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	t.Cleanup(server.Close)

	_, _, err := executeRetireSampleDomainCommandForTest(
		t,
		sampleRetirementCommandRuntime{Client: http.DefaultClient},
		"--server", server.URL,
		"--token", "test-token",
		"verify", "*",
	)

	g := NewWithT(t)
	g.Expect(err).To(HaveOccurred())
	g.Expect(releaseExitCodeForTest(err)).To(Equal(releaseExitUsage))
	g.Expect(requests).To(Equal(0))
}

func TestSampleRetirementCLIRedactsCredentialFromAPIError(t *testing.T) {
	const credential = "sample-retirement-secret-token"
	jobID := uuid.NewString()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(
			w,
			"Bearer "+credential+" and AccessToken "+credential+" are invalid",
			http.StatusUnauthorized,
		)
	}))
	t.Cleanup(server.Close)

	stdout, stderr, err := executeRetireSampleDomainCommandForTest(
		t,
		sampleRetirementCommandRuntime{Client: http.DefaultClient},
		"--server", server.URL,
		"--token", "Bearer "+credential,
		"verify", jobID,
	)

	g := NewWithT(t)
	g.Expect(err).To(HaveOccurred())
	g.Expect(releaseExitCodeForTest(err)).To(Equal(releaseExitAuth))
	g.Expect(stdout + stderr + err.Error()).NotTo(ContainSubstring(credential))
	g.Expect(stderr).To(ContainSubstring("[REDACTED]"))
}

func TestSampleRetirementCLIRedactsCredentialFromStructuredOutput(t *testing.T) {
	const credential = "sample-retirement-secret-token"
	jobID := uuid.NewString()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jobId":"` + jobID + `","diagnostic":"` + credential + `"}`))
	}))
	t.Cleanup(server.Close)

	stdout, stderr, err := executeRetireSampleDomainCommandForTest(
		t,
		sampleRetirementCommandRuntime{Client: http.DefaultClient},
		"--server", server.URL,
		"--token", credential,
		"verify", jobID,
	)

	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(stderr).To(BeEmpty())
	g.Expect(stdout).To(MatchJSON(`{
		"jobId":"` + jobID + `",
		"diagnostic":"[REDACTED]"
	}`))
	g.Expect(stdout).NotTo(ContainSubstring(credential))
}

func TestSampleRetirementCLIStructuredRedactionPreservesJSONFieldNames(t *testing.T) {
	const credential = "a"
	jobID := uuid.NewString()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"jobId":"` + jobID + `",
			"exactCounts":true,
			"diagnostic":"credential a"
		}`))
	}))
	t.Cleanup(server.Close)

	stdout, _, err := executeRetireSampleDomainCommandForTest(
		t,
		sampleRetirementCommandRuntime{Client: http.DefaultClient},
		"--server", server.URL,
		"--token", credential,
		"verify", jobID,
	)

	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(stdout).To(MatchJSON(`{
		"jobId":"` + jobID + `",
		"exactCounts":true,
		"diagnostic":"credential [REDACTED]"
	}`))
}

func TestSampleRetirementCLIUsesUsageExitCodeForUnknownFlags(t *testing.T) {
	_, _, err := executeRetireSampleDomainCommandForTest(
		t,
		sampleRetirementCommandRuntime{},
		"--not-a-real-flag",
	)

	g := NewWithT(t)
	g.Expect(err).To(HaveOccurred())
	var exitErr interface{ ExitCode() int }
	g.Expect(errors.As(err, &exitErr)).To(BeTrue())
	g.Expect(exitErr.ExitCode()).To(Equal(releaseExitUsage))
}

func TestSampleRetirementCLIRegistersOwnershipEvidence(t *testing.T) {
	g := NewWithT(t)
	subjectID := uuid.NewString()
	ownershipChecksum := "sha256:" + strings.Repeat("a", 64)
	sourceChecksum := "sha256:" + strings.Repeat("b", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.Expect(r.Method).To(Equal(http.MethodPost))
		g.Expect(r.URL.Path).To(Equal("/api/v1/sample-retirement-evidence/ownership"))
		body, err := io.ReadAll(r.Body)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(string(body)).To(MatchJSON(`{
			"subjectType":"application",
			"subjectId":"` + subjectID + `",
			"ownershipMarker":"tutorial-fixture",
			"ownershipChecksum":"` + ownershipChecksum + `",
			"sourceReference":"inventory:evidence-42",
			"sourceChecksum":"` + sourceChecksum + `"
		}`))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{
			"id":"11111111-1111-4111-8111-111111111111",
			"subjectType":"application",
			"subjectId":"` + subjectID + `",
			"ownershipChecksum":"` + ownershipChecksum + `"
		}`))
	}))
	t.Cleanup(server.Close)

	stdout, stderr, err := executeRetireSampleDomainCommandForTest(
		t,
		sampleRetirementCommandRuntime{Client: http.DefaultClient},
		"--server", server.URL,
		"--token", "test-token",
		"register-ownership-evidence",
		"--subject-type", "application",
		"--subject-id", subjectID,
		"--ownership-marker", "tutorial-fixture",
		"--ownership-checksum", ownershipChecksum,
		"--source-reference", "inventory:evidence-42",
		"--source-checksum", sourceChecksum,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(stderr).To(BeEmpty())
	g.Expect(stdout).To(MatchJSON(`{
		"id":"11111111-1111-4111-8111-111111111111",
		"subjectType":"application",
		"subjectId":"` + subjectID + `",
		"ownershipChecksum":"` + ownershipChecksum + `"
	}`))
}

func TestSampleRetirementCLIRegistersRecoveryEvidence(t *testing.T) {
	g := NewWithT(t)
	sourceID := uuid.NewString()
	evidenceChecksum := "sha256:" + strings.Repeat("c", 64)
	sourceChecksum := "sha256:" + strings.Repeat("d", 64)
	verifiedAt := "2026-07-28T01:30:00Z"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.Expect(r.Method).To(Equal(http.MethodPost))
		g.Expect(r.URL.Path).To(Equal("/api/v1/sample-retirement-evidence/recovery"))
		body, err := io.ReadAll(r.Body)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(string(body)).To(MatchJSON(`{
			"evidenceKind":"restore_proof",
			"reference":"restore:evidence-42",
			"checksum":"` + evidenceChecksum + `",
			"sourceKind":"database_backup",
			"sourceId":"` + sourceID + `",
			"sourceChecksum":"` + sourceChecksum + `",
			"verifiedAt":"` + verifiedAt + `"
		}`))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{
			"id":"22222222-2222-4222-8222-222222222222",
			"evidenceKind":"restore_proof",
			"sourceId":"` + sourceID + `",
			"checksum":"` + evidenceChecksum + `"
		}`))
	}))
	t.Cleanup(server.Close)

	stdout, stderr, err := executeRetireSampleDomainCommandForTest(
		t,
		sampleRetirementCommandRuntime{Client: http.DefaultClient},
		"--server", server.URL,
		"--token", "test-token",
		"register-recovery-evidence",
		"--evidence-kind", "restore_proof",
		"--reference", "restore:evidence-42",
		"--checksum", evidenceChecksum,
		"--source-kind", "database_backup",
		"--source-id", sourceID,
		"--source-checksum", sourceChecksum,
		"--verified-at", verifiedAt,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(stderr).To(BeEmpty())
	g.Expect(stdout).To(MatchJSON(`{
		"id":"22222222-2222-4222-8222-222222222222",
		"evidenceKind":"restore_proof",
		"sourceId":"` + sourceID + `",
		"checksum":"` + evidenceChecksum + `"
	}`))
}

func TestSampleRetirementCLIRejectsInexactRecoverySourceBeforeRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	t.Cleanup(server.Close)
	checksum := "sha256:" + strings.Repeat("a", 64)

	_, _, err := executeRetireSampleDomainCommandForTest(
		t,
		sampleRetirementCommandRuntime{Client: http.DefaultClient},
		"--server", server.URL,
		"--token", "test-token",
		"register-recovery-evidence",
		"--evidence-kind", "backup",
		"--reference", "backup:evidence-42",
		"--checksum", checksum,
		"--source-kind", "Database Backup",
		"--source-id", uuid.NewString(),
		"--source-checksum", checksum,
		"--verified-at", "2026-07-28T01:30:00Z",
	)

	g := NewWithT(t)
	g.Expect(err).To(HaveOccurred())
	g.Expect(releaseExitCodeForTest(err)).To(Equal(releaseExitUsage))
	g.Expect(requests).To(Equal(0))
}

func TestSampleRetirementCLIRejectsFutureRecoveryVerificationBeforeRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	t.Cleanup(server.Close)
	checksum := "sha256:" + strings.Repeat("a", 64)

	_, _, err := executeRetireSampleDomainCommandForTest(
		t,
		sampleRetirementCommandRuntime{Client: http.DefaultClient},
		"--server", server.URL,
		"--token", "test-token",
		"register-recovery-evidence",
		"--evidence-kind", "backup",
		"--reference", "backup:evidence-42",
		"--checksum", checksum,
		"--source-kind", "database_backup",
		"--source-id", uuid.NewString(),
		"--source-checksum", checksum,
		"--verified-at", "2999-01-01T00:00:00Z",
	)

	g := NewWithT(t)
	g.Expect(err).To(HaveOccurred())
	g.Expect(releaseExitCodeForTest(err)).To(Equal(releaseExitUsage))
	g.Expect(requests).To(Equal(0))
}

func TestSampleRetirementCLIEvidenceRegistrationRequiresExactBindings(t *testing.T) {
	checksum := "sha256:" + strings.Repeat("a", 64)
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "ownership subject UUID",
			args: []string{
				"register-ownership-evidence",
				"--subject-type", "application",
				"--subject-id", "*",
				"--ownership-marker", "tutorial-fixture",
				"--ownership-checksum", checksum,
				"--source-reference", "inventory:evidence-42",
				"--source-checksum", checksum,
			},
		},
		{
			name: "ownership checksum",
			args: []string{
				"register-ownership-evidence",
				"--subject-type", "application",
				"--subject-id", uuid.NewString(),
				"--ownership-marker", "tutorial-fixture",
				"--source-reference", "inventory:evidence-42",
				"--source-checksum", checksum,
			},
		},
		{
			name: "ownership source checksum",
			args: []string{
				"register-ownership-evidence",
				"--subject-type", "application",
				"--subject-id", uuid.NewString(),
				"--ownership-marker", "tutorial-fixture",
				"--ownership-checksum", checksum,
				"--source-reference", "inventory:evidence-42",
			},
		},
		{
			name: "recovery source UUID",
			args: []string{
				"register-recovery-evidence",
				"--evidence-kind", "backup",
				"--reference", "backup:evidence-42",
				"--checksum", checksum,
				"--source-kind", "database_backup",
				"--source-id", "*",
				"--source-checksum", checksum,
				"--verified-at", "2026-07-28T01:30:00Z",
			},
		},
		{
			name: "recovery checksum",
			args: []string{
				"register-recovery-evidence",
				"--evidence-kind", "backup",
				"--reference", "backup:evidence-42",
				"--source-kind", "database_backup",
				"--source-id", uuid.NewString(),
				"--source-checksum", checksum,
				"--verified-at", "2026-07-28T01:30:00Z",
			},
		},
		{
			name: "recovery source checksum",
			args: []string{
				"register-recovery-evidence",
				"--evidence-kind", "backup",
				"--reference", "backup:evidence-42",
				"--checksum", checksum,
				"--source-kind", "database_backup",
				"--source-id", uuid.NewString(),
				"--verified-at", "2026-07-28T01:30:00Z",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				requests++
			}))
			t.Cleanup(server.Close)
			args := append(
				[]string{"--server", server.URL, "--token", "test-token"},
				test.args...,
			)

			_, _, err := executeRetireSampleDomainCommandForTest(
				t,
				sampleRetirementCommandRuntime{Client: http.DefaultClient},
				args...,
			)

			g := NewWithT(t)
			g.Expect(err).To(HaveOccurred())
			g.Expect(releaseExitCodeForTest(err)).To(Equal(releaseExitUsage))
			g.Expect(requests).To(Equal(0))
		})
	}
}

func executeRetireSampleDomainCommandForTest(
	t *testing.T,
	runtime sampleRetirementCommandRuntime,
	args ...string,
) (string, string, error) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if runtime.Stdout == nil {
		runtime.Stdout = &stdout
	}
	if runtime.Stderr == nil {
		runtime.Stderr = &stderr
	}
	if runtime.Getenv == nil {
		runtime.Getenv = func(string) string { return "" }
	}
	command := newRetireSampleDomainCommand(runtime)
	command.SetArgs(args)
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	err := command.Execute()
	return stdout.String(), stderr.String(), err
}
