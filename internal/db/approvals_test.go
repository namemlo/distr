package db

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/pilotexception"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestSampleRetirementApprovalSubjectIsClosedAndValid(t *testing.T) {
	g := NewWithT(t)
	g.Expect(types.ApprovalSubjectSampleRetirement.IsValid()).To(BeTrue())
	g.Expect(types.ApprovalSubjectType("sample_retirement_external").IsValid()).To(BeFalse())
	g.Expect(types.ApprovalInvalidationSampleRetirementChanged.IsValid()).To(BeTrue())
	g.Expect(types.SampleRetirementSubjectEnvironment.IsValid()).To(BeTrue())
	g.Expect(types.SampleRetirementSubjectType("organization").IsValid()).To(BeFalse())
	g.Expect(types.SampleRetirementRecoveryEvidenceBackup.IsValid()).To(BeTrue())
	g.Expect(types.SampleRetirementRecoveryEvidenceKind("snapshot").IsValid()).To(BeFalse())
}

func TestSampleRetirementMigrationBindsApprovalAndRegisteredEvidence(t *testing.T) {
	g := NewWithT(t)
	up, err := os.ReadFile("../migrations/sql/162_sample_domain_retirement.up.sql")
	g.Expect(err).NotTo(HaveOccurred())
	sql := strings.ToLower(string(up))
	for _, fragment := range []string{
		"create table sampleretirementrecoveryevidence",
		"create table sampleretirementownershipevidence",
		"evidence_kind in ('backup', 'restore_proof')",
	} {
		g.Expect(sql).To(ContainSubstring(fragment))
	}
	g.Expect(sql).To(ContainSubstring(
		"subject_type in ('deployment_plan', 'sample_retirement')",
	))
	g.Expect(sql).To(ContainSubstring("create function approval_request_subject_guard()"))
	g.Expect(sql).To(ContainSubstring("job.preview_checksum = new.subject_checksum"))
	g.Expect(sql).To(ContainSubstring("ordinal integer not null check (ordinal >= 1)"))
	g.Expect(sql).To(ContainSubstring("ownership_evidence_id uuid not null"))
	g.Expect(sql).To(ContainSubstring(
		"sample_retirement_ownership_evidence_id uuid",
	))
	g.Expect(sql).To(ContainSubstring(
		"sample_retirement_recovery_evidence_id uuid",
	))
	g.Expect(sql).To(ContainSubstring("sampleretirementrecoveryevidence_append_only"))
	g.Expect(sql).To(ContainSubstring("sampleretirementownershipevidence_append_only"))

	down, err := os.ReadFile("../migrations/sql/162_sample_domain_retirement.down.sql")
	g.Expect(err).NotTo(HaveOccurred())
	downSQL := strings.ToLower(string(down))
	g.Expect(downSQL).To(ContainSubstring(
		"exists (select 1 from sampleretirementrecoveryevidence)",
	))
	g.Expect(downSQL).To(ContainSubstring(
		"exists (select 1 from sampleretirementownershipevidence)",
	))
	g.Expect(downSQL).To(ContainSubstring("add constraint approvalrequest_plan_fk"))
	g.Expect(downSQL).To(ContainSubstring(
		"drop column sample_retirement_ownership_evidence_id",
	))
}

func TestValidateSampleRetirementApprovalBinding(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	organizationID := uuid.New()
	job := types.SampleRetirementJob{
		ID:              uuid.New(),
		OrganizationID:  organizationID,
		State:           types.SampleRetirementJobPreviewed,
		PreviewChecksum: "sha256:" + strings.Repeat("a", 64),
		Version:         1,
	}
	request := types.ApprovalRequest{
		ID:                      uuid.New(),
		OrganizationID:          organizationID,
		SubjectType:             types.ApprovalSubjectSampleRetirement,
		SubjectID:               job.ID,
		SubjectRevision:         job.Version,
		SubjectChecksum:         job.PreviewChecksum,
		EffectivePolicyChecksum: "sha256:" + strings.Repeat("b", 64),
		SubscriberSetChecksum:   "sha256:" + strings.Repeat("c", 64),
		ExpiresAt:               now.Add(time.Hour),
		State:                   types.ApprovalRequestStateApproved,
		Revision:                2,
	}

	binding, err := validateSampleRetirementApprovalBinding(request, job, now)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(binding.ApprovalRequestID).To(Equal(request.ID))
	g.Expect(binding.RequestRevision).To(Equal(request.Revision))
	g.Expect(binding.ApprovalChecksum).To(Equal(approvalEvidenceChecksum(request)))

	tests := []struct {
		name   string
		mutate func(*types.ApprovalRequest, *types.SampleRetirementJob)
	}{
		{"pending", func(r *types.ApprovalRequest, _ *types.SampleRetirementJob) {
			r.State = types.ApprovalRequestStatePending
		}},
		{"expired", func(r *types.ApprovalRequest, _ *types.SampleRetirementJob) {
			r.ExpiresAt = now
		}},
		{"organization", func(r *types.ApprovalRequest, _ *types.SampleRetirementJob) {
			r.OrganizationID = uuid.New()
		}},
		{"subject type", func(r *types.ApprovalRequest, _ *types.SampleRetirementJob) {
			r.SubjectType = types.ApprovalSubjectDeploymentPlan
		}},
		{"subject id", func(r *types.ApprovalRequest, _ *types.SampleRetirementJob) {
			r.SubjectID = uuid.New()
		}},
		{"subject revision", func(r *types.ApprovalRequest, _ *types.SampleRetirementJob) {
			r.SubjectRevision++
		}},
		{"preview checksum", func(r *types.ApprovalRequest, _ *types.SampleRetirementJob) {
			r.SubjectChecksum = "sha256:" + strings.Repeat("d", 64)
		}},
		{"job is not frozen", func(_ *types.ApprovalRequest, j *types.SampleRetirementJob) {
			j.State = types.SampleRetirementJobApplying
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testRequest := request
			testJob := job
			test.mutate(&testRequest, &testJob)
			_, validationErr := validateSampleRetirementApprovalBinding(
				testRequest,
				testJob,
				now,
			)
			NewWithT(t).Expect(validationErr).To(HaveOccurred())
		})
	}
}

func TestSampleRetirementFourEyesPolicyRequiresRequesterSeparationOnEveryRule(
	t *testing.T,
) {
	policy := types.EffectivePolicy{
		ApprovalRules: []types.ApprovalRule{
			{
				Key: "operations",
				SeparationConstraints: []types.SeparationConstraint{
					types.SeparationConstraintRequesterCannotApprove,
				},
			},
			{
				Key: "security",
				SeparationConstraints: []types.SeparationConstraint{
					types.SeparationConstraintDistinctApprovers,
				},
			},
		},
	}

	g := NewWithT(t)
	g.Expect(validateSampleRetirementFourEyesPolicy(policy)).To(
		MatchError(ContainSubstring(
			"every sample retirement approval rule must prevent requester approval",
		)),
	)

	policy.ApprovalRules[1].SeparationConstraints = append(
		policy.ApprovalRules[1].SeparationConstraints,
		types.SeparationConstraintRequesterCannotApprove,
	)
	g.Expect(validateSampleRetirementFourEyesPolicy(policy)).To(Succeed())
}

func TestSampleRetirementRequesterCannotRecordApprovingDecision(t *testing.T) {
	requesterID := uuid.New()
	request := types.ApprovalRequest{
		SubjectType:            types.ApprovalSubjectSampleRetirement,
		RequesterUserAccountID: requesterID,
	}
	input := types.ApprovalDecisionInput{
		ActorUserAccountID: requesterID,
		Decision:           types.ApprovalDecisionApprove,
	}

	g := NewWithT(t)
	g.Expect(validateSampleRetirementDecisionActor(request, input)).To(
		MatchError(ContainSubstring(
			"sample retirement requester cannot approve",
		)),
	)

	input.Decision = types.ApprovalDecisionReject
	g.Expect(validateSampleRetirementDecisionActor(request, input)).To(Succeed())

	input.Decision = types.ApprovalDecisionApprove
	input.ActorUserAccountID = uuid.New()
	g.Expect(validateSampleRetirementDecisionActor(request, input)).To(Succeed())

	request.SubjectType = types.ApprovalSubjectDeploymentPlan
	input.ActorUserAccountID = requesterID
	g.Expect(validateSampleRetirementDecisionActor(request, input)).To(Succeed())
}

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
	requirement := strings.Index(recordCode, "getapprovalrequirement(")
	currentGroupAuthority := strings.Index(recordCode, "approvalactorinrequiredgroup(")
	invalidation := strings.Index(recordCode, "detectapprovalinvalidation(")
	idempotentReplay := strings.Index(recordCode, "getidempotentapprovaldecision(")
	g.Expect(subjectLock).To(BeNumerically(">=", 0))
	g.Expect(requestLock).To(BeNumerically(">", subjectLock))
	g.Expect(authorization).To(BeNumerically(">", requestLock))
	g.Expect(requirement).To(BeNumerically(">", authorization))
	g.Expect(currentGroupAuthority).To(BeNumerically(">", requirement))
	g.Expect(invalidation).To(BeNumerically(">", currentGroupAuthority))
	g.Expect(idempotentReplay).To(BeNumerically(">", invalidation))
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
		OrganizationID:               uuid.New(),
		ApprovalRequestID:            uuid.New(),
		ApprovalRequirementID:        uuid.New(),
		ActorUserAccountID:           uuid.New(),
		Decision:                     types.ApprovalDecisionApprove,
		Comment:                      "Reviewed immutable evidence.",
		RequestRevision:              3,
		IdempotencyKey:               "approval-3",
		GovernanceExceptionKey:       "scoped-single-reviewer-pilot",
		GovernanceExceptionReference: "approved-change-123",
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

	input.Decision = decision.Decision
	input.ExpectedRequestRevision++
	g.Expect(approvalDecisionMatchesInput(decision, input)).To(BeFalse())

	input.ExpectedRequestRevision = decision.RequestRevision
	changedPilot, err := pilotexception.Parse(
		true,
		uuid.NewString(),
		uuid.NewString(),
		uuid.NewString(),
		"approved-change-456",
	)
	g.Expect(err).NotTo(HaveOccurred())
	input.SingleReviewerPilot = changedPilot
	g.Expect(approvalDecisionMatchesInput(decision, input)).To(BeTrue())

	input.SingleReviewerPilot = pilotexception.Config{}
	g.Expect(approvalDecisionMatchesInput(decision, input)).To(BeTrue())
}

func TestAdmissionApprovalRevalidatesCurrentRequirementAuthority(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("approvals.go")
	g.Expect(err).NotTo(HaveOccurred())
	code := string(source)
	for _, fragment := range []string{
		"currentAuthorizedApprovalDecisions",
		"PrincipalGroupMemberRevision",
		"current_revision.state = 'active'",
		"membership.created_at = member.user_membership_created_at",
		"governance.EvaluateApprovalForAdmission",
		"requireCurrentDeploymentPlanApprovalForExecution",
	} {
		g.Expect(code).To(ContainSubstring(fragment))
	}
}
