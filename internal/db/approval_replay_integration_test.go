package db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestApprovalDecisionReplayRequiresCurrentAuthority(t *testing.T) {
	database := newTask4TestDatabase(t, 171, "UTC")
	organizationID, requesterID := insertApprovalReplayPrincipal(t, database)
	jobID := insertApprovalReplaySampleRetirementJob(
		t,
		database,
		organizationID,
		requesterID,
	)
	binding := insertSampleRetirementApprovalFixture(
		t,
		database,
		organizationID,
		requesterID,
		jobID,
	)
	request, err := db.GetApprovalRequest(
		database.ctx,
		binding.ApprovalRequestID,
		organizationID,
	)
	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(request.Requirements).To(HaveLen(1))
	g.Expect(request.Decisions).To(HaveLen(1))
	requirement := request.Requirements[0]
	stored := request.Decisions[0]

	authorizedCalls := 0
	input := types.ApprovalDecisionInput{
		OrganizationID:          organizationID,
		ApprovalRequestID:       request.ID,
		ApprovalRequirementID:   requirement.ID,
		ActorUserAccountID:      stored.ActorUserAccountID,
		Decision:                stored.Decision,
		Comment:                 stored.Comment,
		ExpectedRequestRevision: stored.RequestRevision,
		IdempotencyKey:          stored.IdempotencyKey,
		Authorize: func(
			_ context.Context,
			authorization types.ApprovalAuthorizationContext,
		) error {
			authorizedCalls++
			g.Expect(authorization.OrganizationID).To(Equal(organizationID))
			g.Expect(authorization.SampleRetirementJobID).To(Equal(jobID))
			g.Expect(authorization.ApprovalRequestID).To(Equal(request.ID))
			g.Expect(authorization.ApprovalRequirementID).To(Equal(requirement.ID))
			g.Expect(authorization.ActorUserAccountID).To(Equal(stored.ActorUserAccountID))
			return nil
		},
	}
	replayed, err := db.RecordApprovalDecision(database.ctx, input)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(replayed.ID).To(Equal(stored.ID))
	g.Expect(authorizedCalls).To(Equal(1))

	input.Authorize = func(
		context.Context,
		types.ApprovalAuthorizationContext,
	) error {
		return apierrors.ErrForbidden
	}
	replayed, err = db.RecordApprovalDecision(database.ctx, input)
	g.Expect(errors.Is(err, apierrors.ErrForbidden)).To(BeTrue())
	g.Expect(replayed).To(BeNil())

	wrongRevision := input
	wrongRevision.ExpectedRequestRevision++
	wrongRevision.Authorize = func(
		context.Context,
		types.ApprovalAuthorizationContext,
	) error {
		return nil
	}
	replayed, err = db.RecordApprovalDecision(database.ctx, wrongRevision)
	g.Expect(err).To(MatchError(ContainSubstring(
		"idempotency key is already bound to a different approval decision",
	)))
	g.Expect(replayed).To(BeNil())

	crossTenant := input
	crossTenant.OrganizationID = uuid.New()
	crossTenantAuthorizerCalled := false
	crossTenant.Authorize = func(
		context.Context,
		types.ApprovalAuthorizationContext,
	) error {
		crossTenantAuthorizerCalled = true
		return nil
	}
	replayed, err = db.RecordApprovalDecision(database.ctx, crossTenant)
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
	g.Expect(replayed).To(BeNil())
	g.Expect(crossTenantAuthorizerCalled).To(BeFalse())

	g.Expect(db.InvalidateApproval(
		database.ctx,
		request.ID,
		types.ApprovalInvalidationSuperseded,
	)).To(Succeed())
	input.Authorize = func(
		context.Context,
		types.ApprovalAuthorizationContext,
	) error {
		return nil
	}
	replayed, err = db.RecordApprovalDecision(database.ctx, input)
	g.Expect(err).To(MatchError(ContainSubstring(
		"approval request is invalid: superseded",
	)))
	g.Expect(replayed).To(BeNil())

	var memberID uuid.UUID
	g.Expect(database.pool.QueryRow(context.Background(), `
		SELECT id
		FROM PrincipalGroupMember
		WHERE organization_id = $1
		  AND group_id = $2
		  AND user_account_id = $3`,
		organizationID,
		requirement.PrincipalGroupID,
		stored.ActorUserAccountID,
	).Scan(&memberID)).To(Succeed())
	g.Expect(db.RevokeAuthorizationPrincipalGroupMember(
		database.ctx,
		requirement.PrincipalGroupID,
		&types.PrincipalGroupMemberRevision{
			OrganizationID:         organizationID,
			PrincipalGroupMemberID: memberID,
			EffectiveFrom:          time.Now().UTC().Add(-time.Second),
			ActorUserID:            requesterID,
			Reason:                 "revoke approval replay authority",
		},
	)).To(Succeed())

	input.Authorize = func(
		context.Context,
		types.ApprovalAuthorizationContext,
	) error {
		return nil
	}
	replayed, err = db.RecordApprovalDecision(database.ctx, input)
	g.Expect(errors.Is(err, apierrors.ErrForbidden)).To(BeTrue())
	g.Expect(replayed).To(BeNil())
}

func insertApprovalReplayPrincipal(
	t *testing.T,
	database *task4TestDatabase,
) (uuid.UUID, uuid.UUID) {
	t.Helper()
	organizationID := uuid.New()
	requesterID := uuid.New()
	g := NewWithT(t)
	_, err := database.pool.Exec(
		context.Background(),
		`INSERT INTO Organization (id, name) VALUES ($1, $2)`,
		organizationID,
		"approval-replay-"+organizationID.String(),
	)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = database.pool.Exec(
		context.Background(),
		`INSERT INTO UserAccount (id, email) VALUES ($1, $2)`,
		requesterID,
		requesterID.String()+"@example.test",
	)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = database.pool.Exec(context.Background(), `
		INSERT INTO Organization_UserAccount (
		  organization_id, user_account_id, user_role
		) VALUES ($1, $2, 'admin')`,
		organizationID,
		requesterID,
	)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = database.pool.Exec(context.Background(), `
		INSERT INTO SampleRetirementRecoveryEvidence (
		  organization_id, evidence_kind, reference, checksum, source_kind,
		  source_id, source_checksum, verified_at, verified_by_useraccount_id
		) VALUES
		  ($1, 'backup', 'backup:test', $3, 'test', $4, $3, now(), $2),
		  ($1, 'restore_proof', 'restore:test', $5, 'test', $4, $5, now(), $2)`,
		organizationID,
		requesterID,
		sampleRetirementChecksum("backup"),
		uuid.New(),
		sampleRetirementChecksum("restore"),
	)
	g.Expect(err).NotTo(HaveOccurred())
	return organizationID, requesterID
}

func insertApprovalReplaySampleRetirementJob(
	t *testing.T,
	database *task4TestDatabase,
	organizationID uuid.UUID,
	requesterID uuid.UUID,
) uuid.UUID {
	t.Helper()
	jobID := uuid.New()
	var backupEvidenceID, restoreEvidenceID uuid.UUID
	g := NewWithT(t)
	g.Expect(database.pool.QueryRow(context.Background(), `
		SELECT
		  (SELECT id FROM SampleRetirementRecoveryEvidence
		   WHERE organization_id = $1 AND evidence_kind = 'backup' LIMIT 1),
		  (SELECT id FROM SampleRetirementRecoveryEvidence
		   WHERE organization_id = $1 AND evidence_kind = 'restore_proof' LIMIT 1)`,
		organizationID,
	).Scan(&backupEvidenceID, &restoreEvidenceID)).To(Succeed())
	_, err := database.pool.Exec(context.Background(), `
		INSERT INTO SampleRetirementJob (
		  id,
		  organization_id,
		  requested_by_useraccount_id,
		  state,
		  backup_evidence_id,
		  backup_reference,
		  backup_checksum,
		  restore_proof_evidence_id,
		  restore_proof_reference,
		  restore_proof_checksum,
		  allowlist_checksum,
		  preview_checksum,
		  requested_item_count,
		  previewed_item_count
		) VALUES (
		  $1, $2, $3, 'PREVIEWED', $4, 'backup:test', $5,
		  $6, 'restore:test', $7, $8, $9, 1, 1
		)`,
		jobID,
		organizationID,
		requesterID,
		backupEvidenceID,
		sampleRetirementChecksum("backup"),
		restoreEvidenceID,
		sampleRetirementChecksum("restore"),
		sampleRetirementChecksum("approval-replay-allowlist"),
		sampleRetirementChecksum("approval-replay-preview"),
	)
	g.Expect(err).NotTo(HaveOccurred())
	return jobID
}
