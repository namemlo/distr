package db

import (
	"os"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestApprovalMigrationDefinesImmutableChecksumBoundWorkflow(t *testing.T) {
	g := NewWithT(t)
	up, err := os.ReadFile("../migrations/sql/150_approval_workflow.up.sql")
	g.Expect(err).NotTo(HaveOccurred())
	sql := strings.ToLower(string(up))

	for _, fragment := range []string{
		"create table approvalrequest (",
		"create table approvalrequirement (",
		"create table approvaldecision (",
		"subject_revision bigint not null",
		"subject_checksum text not null",
		"effective_policy_checksum text not null",
		"subscriber_set_checksum text not null",
		"requester_useraccount_id uuid not null",
		"unique (approval_request_id, actor_useraccount_id, idempotency_key)",
		"approval_decision_append_only",
		"request_revision bigint not null",
		"principal_group_id uuid not null",
		"authority_kind",
		"authority_id",
		"quorum",
	} {
		g.Expect(sql).To(ContainSubstring(fragment))
	}

	organizationRepository, err := os.ReadFile("organization.go")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(organizationRepository)).To(ContainSubstring(
		"'distr.approval_deletion_reason'",
	))
}

func TestApprovalMigrationDowngradeRefusesAuditLoss(t *testing.T) {
	g := NewWithT(t)
	down, err := os.ReadFile("../migrations/sql/150_approval_workflow.down.sql")
	g.Expect(err).NotTo(HaveOccurred())
	sql := strings.ToLower(string(down))

	g.Expect(sql).To(ContainSubstring("lock table"))
	g.Expect(sql).To(ContainSubstring("downgrade crossing 150 is forbidden"))
	g.Expect(sql).To(ContainSubstring("exists (select 1 from approvaldecision)"))
}

func TestApprovalRepositoryUsesLocksIdempotencyAndKeysetPagination(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("approvals.go")
	g.Expect(err).NotTo(HaveOccurred())
	code := strings.ToLower(string(source))

	g.Expect(code).To(ContainSubstring("for update"))
	g.Expect(code).To(ContainSubstring("idempotency_key"))
	g.Expect(code).To(ContainSubstring("expectedrequestrevision"))
	g.Expect(code).To(ContainSubstring("approval request revision changed"))
	g.Expect(code).To(ContainSubstring("(request.created_at, request.id) <"))
	g.Expect(code).To(ContainSubstring("limit + 1"))
	g.Expect(code).To(ContainSubstring("evaluatedeploymentplanapproval"))
	g.Expect(code).To(ContainSubstring(
		"(@state <> 'pending' or request.expires_at > now())",
	))
	g.Expect(code).To(ContainSubstring(
		"stateforapprovalinvalidation(reason),\n\t\t\t\treason,",
	))
	recordStart := strings.Index(code, "func recordapprovaldecision(")
	recordEnd := strings.Index(code, "func evaluateapprovaleligibility(")
	g.Expect(recordStart).To(BeNumerically(">=", 0))
	g.Expect(recordEnd).To(BeNumerically(">", recordStart))
	recordCode := code[recordStart:recordEnd]
	subjectLock := strings.Index(recordCode, "currentapprovalsubjectsnapshot")
	requestLock := strings.Index(recordCode, "getapprovalrequestforupdate")
	authorization := strings.Index(recordCode, "input.authorize(")
	g.Expect(subjectLock).To(BeNumerically(">=", 0))
	g.Expect(requestLock).To(BeNumerically(">", subjectLock))
	g.Expect(authorization).To(BeNumerically(">", requestLock))
	g.Expect(recordCode).To(ContainSubstring(
		"stateforapprovalinvalidation(invalidationreason)",
	))
	g.Expect(code).To(ContainSubstring(
		"where requirement.organization_id = @organizationid\n" +
			"\t\t  and requirement.approval_request_id = any(@requestids)",
	))
	g.Expect(code).To(ContainSubstring(
		"where decision.organization_id = @organizationid\n" +
			"\t\t  and decision.approval_request_id = any(@requestids)",
	))
}

func TestApprovalDecisionIdempotencyMatchesOnlyExactRetry(t *testing.T) {
	decision := types.ApprovalDecision{
		OrganizationID:        uuid.New(),
		ApprovalRequestID:     uuid.New(),
		ApprovalRequirementID: uuid.New(),
		ActorUserAccountID:    uuid.New(),
		Decision:              types.ApprovalDecisionApprove,
		Comment:               "Reviewed immutable evidence.",
		RequestRevision:       3,
		IdempotencyKey:        "approval-3",
	}
	input := types.ApprovalDecisionInput{
		OrganizationID:          decision.OrganizationID,
		ApprovalRequestID:       decision.ApprovalRequestID,
		ApprovalRequirementID:   decision.ApprovalRequirementID,
		ActorUserAccountID:      decision.ActorUserAccountID,
		Decision:                decision.Decision,
		Comment:                 decision.Comment,
		ExpectedRequestRevision: decision.RequestRevision,
		IdempotencyKey:          decision.IdempotencyKey,
	}

	g := NewWithT(t)
	g.Expect(approvalDecisionMatchesInput(decision, input)).To(BeTrue())

	input.Decision = types.ApprovalDecisionReject
	g.Expect(approvalDecisionMatchesInput(decision, input)).To(BeFalse())
}
