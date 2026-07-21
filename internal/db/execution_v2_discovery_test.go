package db

import (
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestExecutionV2LeaseRequestValidation(t *testing.T) {
	g := NewWithT(t)
	request := types.LeaseExecutionV2Request{
		OrganizationID: uuid.New(), DeploymentTargetID: uuid.New(),
		ExecutorID: "executor-a", AdapterRevision: "adapter.compose@2",
		KeyID: "sha256:" + repeatDBHex("ab"), Now: time.Now().UTC(),
		LeaseDuration: time.Minute,
	}

	g.Expect(validateLeaseExecutionV2Request(request)).To(Succeed())
	request.DeploymentTargetID = uuid.Nil
	g.Expect(validateLeaseExecutionV2Request(request)).To(MatchError(ContainSubstring("scope")))
}

func TestExecutionV2LeaseCandidateQueryIsCredentialAndFrozenIdentityScoped(t *testing.T) {
	g := NewWithT(t)
	query := strings.ToLower(executionV2LeaseCandidateQuery)

	for _, required := range []string{
		"ea.organization_id = @organizationid",
		"ea.deployment_target_id = @deploymenttargetid",
		"ea.status = 'pending'",
		"ea.adapter_revision = @adapterrevision",
		"ei.key_id = @keyid",
		"ea.intent_issued_at <= clock_timestamp()",
		"ea.intent_expires_at > clock_timestamp()",
		"ef.released_at is null",
		"for update of ea, ef skip locked",
	} {
		g.Expect(query).To(ContainSubstring(required))
	}
}

func TestExecutionV2ExpiredPendingReaperIsTargetScopedAndConcurrencySafe(t *testing.T) {
	g := NewWithT(t)
	query := strings.ToLower(executionV2ExpiredPendingCandidateQuery)

	for _, required := range []string{
		"ea.organization_id = @organizationid",
		"ea.deployment_target_id = @deploymenttargetid",
		"ea.status = 'pending'",
		"ea.intent_expires_at <= clock_timestamp()",
		"ef.lease_expires_at is null",
		"ef.released_at is null",
		"for update of ea, ef skip locked",
	} {
		g.Expect(query).To(ContainSubstring(required))
	}
}
