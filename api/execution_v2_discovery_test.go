package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestExecutionV2LeaseResponseKeepsRuntimeChecksumIdentitiesDistinct(t *testing.T) {
	g := NewWithT(t)
	response := ExecutionV2LeaseResponse{Attempt: types.ExecutionAttempt{
		ConfigChecksum:                       "sha256:" + strings.Repeat("1", 64),
		RuntimeManifestChecksum:              "sha256:" + strings.Repeat("2", 64),
		DesiredServiceConfigChecksum:         "sha256:" + strings.Repeat("3", 64),
		ExpectedCurrentConfigChecksum:        "sha256:" + strings.Repeat("4", 64),
		ExpectedCurrentServiceConfigChecksum: "sha256:" + strings.Repeat("5", 64),
	}}

	payload, err := json.Marshal(response)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(payload)).To(ContainSubstring(
		`"configChecksum":"sha256:` + strings.Repeat("1", 64) + `"`,
	))
	g.Expect(string(payload)).To(ContainSubstring(
		`"runtimeManifestChecksum":"sha256:` + strings.Repeat("2", 64) + `"`,
	))
	g.Expect(string(payload)).To(ContainSubstring(
		`"desiredServiceConfigChecksum":"sha256:` + strings.Repeat("3", 64) + `"`,
	))
	g.Expect(string(payload)).To(ContainSubstring(
		`"expectedCurrentConfigChecksum":"sha256:` + strings.Repeat("4", 64) + `"`,
	))
	g.Expect(string(payload)).To(ContainSubstring(
		`"expectedCurrentServiceConfigChecksum":"sha256:` + strings.Repeat("5", 64) + `"`,
	))
}

func TestExecutionV2LeaseRequestValidatesFrozenExecutorIdentity(t *testing.T) {
	g := NewWithT(t)
	request := ExecutionV2LeaseRequest{
		ExecutorID:      " executor-a ",
		AdapterRevision: " adapter.compose@2 ",
		KeyID:           "sha256:" + repeatAPIHex("ab"),
		LeaseSeconds:    60,
	}

	g.Expect(request.Validate()).To(Succeed())
	g.Expect(request.ExecutorID).To(Equal("executor-a"))
	g.Expect(request.AdapterRevision).To(Equal("adapter.compose@2"))

	now := time.Date(2026, 7, 22, 1, 0, 0, 0, time.UTC)
	orgID, targetID := uuid.New(), uuid.New()
	lease := request.ToTypes(orgID, targetID, now)
	g.Expect(lease.OrganizationID).To(Equal(orgID))
	g.Expect(lease.DeploymentTargetID).To(Equal(targetID))
	g.Expect(lease.ExecutorID).To(Equal("executor-a"))
	g.Expect(lease.AdapterRevision).To(Equal("adapter.compose@2"))
	g.Expect(lease.KeyID).To(Equal(request.KeyID))
	g.Expect(lease.Now).To(Equal(now))
	g.Expect(lease.LeaseDuration).To(Equal(time.Minute))
}

func TestExecutionV2LeaseRequestRejectsInvalidIdentity(t *testing.T) {
	valid := ExecutionV2LeaseRequest{
		ExecutorID:      "executor-a",
		AdapterRevision: "adapter.compose@2",
		KeyID:           "sha256:" + repeatAPIHex("ab"),
		LeaseSeconds:    60,
	}

	tests := []struct {
		name   string
		mutate func(*ExecutionV2LeaseRequest)
		field  string
	}{
		{name: "executor", mutate: func(r *ExecutionV2LeaseRequest) { r.ExecutorID = " " }, field: "executorId"},
		{name: "adapter", mutate: func(r *ExecutionV2LeaseRequest) { r.AdapterRevision = " " }, field: "adapterRevision"},
		{name: "key", mutate: func(r *ExecutionV2LeaseRequest) { r.KeyID = "latest" }, field: "keyId"},
		{name: "lease", mutate: func(r *ExecutionV2LeaseRequest) { r.LeaseSeconds = 301 }, field: "leaseSeconds"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			request := valid
			test.mutate(&request)
			g.Expect(request.Validate()).To(MatchError(ContainSubstring(test.field)))
		})
	}
}
