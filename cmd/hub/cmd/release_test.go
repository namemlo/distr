package cmd

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestReleaseCreateCommandSendsPublicAPIRequest(t *testing.T) {
	g := NewWithT(t)
	body := `{"releaseNumber":"2026.06.20"}`
	var sawRequest bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawRequest = true
		g.Expect(r.Method).To(Equal(http.MethodPost))
		g.Expect(r.URL.Path).To(Equal("/api/v1/release-bundles"))
		g.Expect(r.Header.Get("Authorization")).To(Equal("AccessToken flag-token"))
		g.Expect(r.Header.Get("Idempotency-Key")).To(Equal("idem-1"))
		data, err := io.ReadAll(r.Body)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(string(data)).To(Equal(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"` + uuid.NewString() + `","status":"DRAFT"}`))
	}))
	t.Cleanup(server.Close)

	inputFile := filepath.Join(t.TempDir(), "release.json")
	g.Expect(os.WriteFile(inputFile, []byte(body), 0o600)).To(Succeed())

	stdout, stderr, err := executeReleaseCommandForTest(
		t,
		releaseCommandRuntime{
			Client: http.DefaultClient,
			Getenv: func(name string) string {
				if name == "DISTR_API_TOKEN" {
					return "env-token"
				}
				return ""
			},
		},
		"--server", server.URL,
		"--token", "flag-token",
		"--output", "json",
		"create",
		"--file", inputFile,
		"--idempotency-key", "idem-1",
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(stderr).To(BeEmpty())
	g.Expect(stdout).To(ContainSubstring(`"status":"DRAFT"`))
	g.Expect(sawRequest).To(BeTrue())
}

func TestReleaseCreateCommandReadsStdinAndUsesEnvironment(t *testing.T) {
	g := NewWithT(t)
	bundleID := uuid.NewString()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.Expect(r.Method).To(Equal(http.MethodPost))
		g.Expect(r.URL.Path).To(Equal("/api/v1/release-bundles"))
		g.Expect(r.Header.Get("Authorization")).To(Equal("AccessToken env-token"))
		data, err := io.ReadAll(r.Body)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(string(data)).To(Equal(`{"releaseNumber":"2026.06.20"}`))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"` + bundleID + `","status":"DRAFT"}`))
	}))
	t.Cleanup(server.Close)

	stdout, stderr, err := executeReleaseCommandForTest(
		t,
		releaseCommandRuntime{
			Stdin:  strings.NewReader(`{"releaseNumber":"2026.06.20"}`),
			Client: http.DefaultClient,
			Getenv: func(name string) string {
				switch name {
				case "DISTR_SERVER_URL":
					return server.URL
				case "DISTR_API_TOKEN":
					return "env-token"
				default:
					return ""
				}
			},
		},
		"create",
		"--file", "-",
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(stderr).To(BeEmpty())
	g.Expect(stdout).To(ContainSubstring("Release bundle " + bundleID + " DRAFT"))
}

func TestReleaseCreateSchemaAssertionFailsBeforeRequest(t *testing.T) {
	g := NewWithT(t)
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	t.Cleanup(server.Close)
	inputFile := filepath.Join(t.TempDir(), "release.json")
	g.Expect(os.WriteFile(
		inputFile,
		[]byte(`{"releaseContract":{"schema":"distr.release-contract/v1"}}`),
		0o600,
	)).To(Succeed())

	_, _, err := executeReleaseCommandForTest(
		t,
		releaseCommandRuntime{Client: http.DefaultClient},
		"--server", server.URL,
		"--token", "token-value",
		"create",
		"--file", inputFile,
		"--schema", "v2",
	)

	g.Expect(err).To(HaveOccurred())
	g.Expect(releaseExitCodeForTest(err)).To(Equal(releaseExitUsage))
	g.Expect(err.Error()).To(ContainSubstring(`distr.component-release/v2`))
	g.Expect(requests).To(Equal(0))
}

func TestReleaseValidateCommandReturnsValidationExitCode(t *testing.T) {
	g := NewWithT(t)
	bundleID := uuid.NewString()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.Expect(r.Method).To(Equal(http.MethodPost))
		g.Expect(r.URL.Path).To(Equal("/api/v1/release-bundles/" + bundleID + "/validate"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"valid": false,
			"errors": [{"field":"components.api.digest","rule":"immutable","message":"digest is required"}],
			"warnings": []
		}`))
	}))
	t.Cleanup(server.Close)

	stdout, stderr, err := executeReleaseCommandForTest(
		t,
		releaseCommandRuntime{Client: http.DefaultClient},
		"--server", server.URL,
		"--token", "token-value",
		"validate",
		bundleID,
	)

	g.Expect(err).To(HaveOccurred())
	g.Expect(releaseExitCodeForTest(err)).To(Equal(3))
	g.Expect(stderr).To(BeEmpty())
	g.Expect(stdout).To(ContainSubstring("invalid"))
	g.Expect(stdout).To(ContainSubstring("components.api.digest"))
}

func TestReleasePublishCommandReturnsValidationExitCodeForValidationFailure(t *testing.T) {
	g := NewWithT(t)
	bundleID := uuid.NewString()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.Expect(r.Method).To(Equal(http.MethodPost))
		g.Expect(r.URL.Path).To(Equal("/api/v1/release-bundles/" + bundleID + "/publish"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{
			"valid": false,
			"errors": [{"field":"sourceMetadata.branch","rule":"channel.sourceBranch","message":"source branch is not allowed"}],
			"warnings": []
		}`))
	}))
	t.Cleanup(server.Close)

	stdout, stderr, err := executeReleaseCommandForTest(
		t,
		releaseCommandRuntime{Client: http.DefaultClient},
		"--server", server.URL,
		"--token", "token-value",
		"publish",
		bundleID,
	)

	g.Expect(err).To(HaveOccurred())
	g.Expect(releaseExitCodeForTest(err)).To(Equal(3))
	g.Expect(stderr).To(BeEmpty())
	g.Expect(stdout).To(ContainSubstring("invalid"))
	g.Expect(stdout).To(ContainSubstring("sourceMetadata.branch"))
}

func TestReleasePublishCommandSendsProvenanceAndPrintsV2Digests(t *testing.T) {
	g := NewWithT(t)
	bundleID := uuid.NewString()
	manifestDigest := "sha256:" + strings.Repeat("a", 64)
	platformDigest := "sha256:" + strings.Repeat("b", 64)
	checksum := "sha256:" + strings.Repeat("c", 64)
	requestBody := `{"provenance":{"policy":{"version":"distr.provenance-policy/v1"},"evidence":[]}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.Expect(r.Method).To(Equal(http.MethodPost))
		g.Expect(r.URL.Path).To(Equal("/api/v1/release-bundles/" + bundleID + "/publish"))
		data, err := io.ReadAll(r.Body)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(string(data)).To(Equal(requestBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"` + bundleID + `",
			"status":"PUBLISHED",
			"releaseContractSchema":"distr.component-release/v2",
			"canonicalChecksum":"` + checksum + `",
			"releaseContract":{
				"schema":"distr.component-release/v2",
				"artifacts":[{
					"key":"service",
					"digest":"` + manifestDigest + `",
					"platforms":[{"platform":"linux/amd64","digest":"` + platformDigest + `"}]
				}]
			}
		}`))
	}))
	t.Cleanup(server.Close)
	provenanceFile := filepath.Join(t.TempDir(), "provenance.json")
	g.Expect(os.WriteFile(provenanceFile, []byte(requestBody), 0o600)).To(Succeed())

	stdout, stderr, err := executeReleaseCommandForTest(
		t,
		releaseCommandRuntime{Client: http.DefaultClient},
		"--server", server.URL,
		"--token", "token-value",
		"publish",
		bundleID,
		"--provenance-file", provenanceFile,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(stderr).To(BeEmpty())
	g.Expect(stdout).To(ContainSubstring("schema=distr.component-release/v2"))
	g.Expect(stdout).To(ContainSubstring("checksum=" + checksum))
	g.Expect(stdout).To(ContainSubstring("manifest=" + manifestDigest))
	g.Expect(stdout).To(ContainSubstring("platform linux/amd64 digest=" + platformDigest))
}

func TestReleasePublishCommandMapsAuthFailureAndRedactsToken(t *testing.T) {
	g := NewWithT(t)
	bundleID := uuid.NewString()
	const token = "super-secret-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.Expect(r.Method).To(Equal(http.MethodPost))
		g.Expect(r.URL.Path).To(Equal("/api/v1/release-bundles/" + bundleID + "/publish"))
		http.Error(w, "AccessToken "+token+" is invalid", http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)

	stdout, stderr, err := executeReleaseCommandForTest(
		t,
		releaseCommandRuntime{Client: http.DefaultClient},
		"--server", server.URL,
		"--token", token,
		"publish",
		bundleID,
	)

	g.Expect(err).To(HaveOccurred())
	g.Expect(releaseExitCodeForTest(err)).To(Equal(4))
	g.Expect(stdout).NotTo(ContainSubstring(token))
	g.Expect(stderr).NotTo(ContainSubstring(token))
	g.Expect(err.Error()).NotTo(ContainSubstring(token))
}

func TestReleasePublishCommandRedactsCredentialVariants(t *testing.T) {
	g := NewWithT(t)
	bundleID := uuid.NewString()
	const credential = "super-secret-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.Expect(r.Header.Get("Authorization")).To(Equal("Bearer " + credential))
		http.Error(
			w,
			"bearer "+credential+" AccessToken "+credential+" "+credential+" is invalid",
			http.StatusInternalServerError,
		)
	}))
	t.Cleanup(server.Close)

	stdout, stderr, err := executeReleaseCommandForTest(
		t,
		releaseCommandRuntime{Client: http.DefaultClient},
		"--server", server.URL,
		"--token", "Bearer "+credential,
		"publish",
		bundleID,
	)

	g.Expect(err).To(HaveOccurred())
	g.Expect(releaseExitCodeForTest(err)).To(Equal(5))
	g.Expect(stdout).NotTo(ContainSubstring(credential))
	g.Expect(stderr).NotTo(ContainSubstring(credential))
	g.Expect(err.Error()).NotTo(ContainSubstring(credential))
	g.Expect(stderr).To(ContainSubstring("[REDACTED]"))
	g.Expect(err.Error()).To(ContainSubstring("[REDACTED]"))
}

func TestReleaseCommandUsageErrorsReturnUsageExitCode(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "missing validate id", args: []string{"validate"}},
		{name: "unknown flag", args: []string{"--not-a-real-flag"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			_, _, err := executeReleaseCommandForTest(t, releaseCommandRuntime{}, tt.args...)

			g.Expect(err).To(HaveOccurred())
			g.Expect(releaseExitCodeForTest(err)).To(Equal(2))
		})
	}
}

func executeReleaseCommandForTest(
	t *testing.T,
	runtime releaseCommandRuntime,
	args ...string,
) (string, string, error) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if runtime.Stdin == nil {
		runtime.Stdin = strings.NewReader("")
	}
	if runtime.Stdout == nil {
		runtime.Stdout = &stdout
	}
	if runtime.Stderr == nil {
		runtime.Stderr = &stderr
	}
	if runtime.Getenv == nil {
		runtime.Getenv = func(string) string { return "" }
	}
	cmd := newReleaseCommand(runtime)
	cmd.SetArgs(args)
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

type exitCoder interface {
	ExitCode() int
}

func releaseExitCodeForTest(err error) int {
	var exitErr exitCoder
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}
