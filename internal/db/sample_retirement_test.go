package db_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestSampleRetirementRejectsUnsupportedSubjectTypeBeforeQuerying(t *testing.T) {
	_, err := db.InspectSampleRetirementSubjects(
		context.Background(),
		uuid.New(),
		[]types.SampleRetirementSubject{{
			SubjectType:       "organization",
			SubjectID:         uuid.New(),
			OwnershipMarker:   "sample:test",
			OwnershipChecksum: sampleRetirementChecksum("sample:test"),
			ExpectedChecksum:  sampleRetirementChecksum("subject"),
		}},
	)

	NewWithT(t).Expect(err).To(MatchError(ContainSubstring("unsupported sample retirement subject type")))
}

func TestSampleRetirementEvidenceRegistrationRejectsInvalidInputBeforeQuerying(
	t *testing.T,
) {
	_, ownershipErr := db.RegisterSampleRetirementOwnershipEvidence(
		context.Background(),
		types.SampleRetirementOwnershipEvidenceRegistrationInput{},
	)
	_, recoveryErr := db.RegisterSampleRetirementRecoveryEvidence(
		context.Background(),
		types.SampleRetirementRecoveryEvidenceRegistrationInput{},
	)

	g := NewWithT(t)
	g.Expect(ownershipErr).To(MatchError(ContainSubstring(
		"sample retirement ownership evidence identity is invalid",
	)))
	g.Expect(recoveryErr).To(MatchError(ContainSubstring(
		"sample retirement recovery evidence identity is invalid",
	)))
}

func TestSampleRetirementEvidenceRegistrationIsAuditedIdempotentAndAtomic(
	t *testing.T,
) {
	database := newTask4TestDatabase(t, 162, "UTC")
	organizationID, actorID := insertSampleRetirementPrincipal(t, database)
	subjectID := uuid.New()
	ownershipInput := types.SampleRetirementOwnershipEvidenceRegistrationInput{
		OrganizationID:          organizationID,
		RecordedByUserAccountID: actorID,
		SubjectType:             types.SampleRetirementSubjectEnvironment,
		SubjectID:               subjectID,
		OwnershipMarker:         "sample:registered",
		OwnershipChecksum:       sampleRetirementChecksum("sample:registered"),
		SourceReference:         "inventory:test:registered",
		SourceChecksum:          sampleRetirementChecksum("inventory:test:registered"),
	}
	ownership, err := db.RegisterSampleRetirementOwnershipEvidence(
		database.ctx,
		ownershipInput,
	)
	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	replayedOwnership, err := db.RegisterSampleRetirementOwnershipEvidence(
		database.ctx,
		ownershipInput,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(replayedOwnership).To(Equal(ownership))

	conflictingOwnership := ownershipInput
	conflictingOwnership.SourceReference = "inventory:test:conflict"
	_, err = db.RegisterSampleRetirementOwnershipEvidence(
		database.ctx,
		conflictingOwnership,
	)
	g.Expect(err).To(MatchError(ContainSubstring("already bound differently")))

	recoveryInput := types.SampleRetirementRecoveryEvidenceRegistrationInput{
		OrganizationID:          organizationID,
		VerifiedByUserAccountID: actorID,
		EvidenceKind:            types.SampleRetirementRecoveryEvidenceBackup,
		Reference:               "backup:registered",
		Checksum:                sampleRetirementChecksum("backup:registered"),
		SourceKind:              "backup_catalog",
		SourceID:                uuid.New(),
		SourceChecksum:          sampleRetirementChecksum("backup-catalog-entry"),
		VerifiedAt:              time.Now().UTC().Add(-time.Minute),
	}
	recovery, err := db.RegisterSampleRetirementRecoveryEvidence(
		database.ctx,
		recoveryInput,
	)
	g.Expect(err).NotTo(HaveOccurred())
	replayedRecovery, err := db.RegisterSampleRetirementRecoveryEvidence(
		database.ctx,
		recoveryInput,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(replayedRecovery).To(Equal(recovery))

	conflictingRecovery := recoveryInput
	conflictingRecovery.SourceID = uuid.New()
	_, err = db.RegisterSampleRetirementRecoveryEvidence(
		database.ctx,
		conflictingRecovery,
	)
	g.Expect(err).To(MatchError(ContainSubstring("already bound differently")))

	var ownershipRows, recoveryRows, ownershipAuditRows, recoveryAuditRows int
	err = database.pool.QueryRow(context.Background(), `
		SELECT
			(SELECT count(*) FROM SampleRetirementOwnershipEvidence
			 WHERE id=$1 AND organization_id=$3),
			(SELECT count(*) FROM SampleRetirementRecoveryEvidence
			 WHERE id=$2 AND organization_id=$3),
			(SELECT count(*) FROM ControlPlaneAuditEvent
			 WHERE organization_id=$3
			   AND event_type='sample.retirement.ownership_evidence.registered'
			   AND sample_retirement_ownership_evidence_id=$1),
			(SELECT count(*) FROM ControlPlaneAuditEvent
			 WHERE organization_id=$3
			   AND event_type='sample.retirement.recovery_evidence.registered'
			   AND sample_retirement_recovery_evidence_id=$2)`,
		ownership.ID,
		recovery.ID,
		organizationID,
	).Scan(
		&ownershipRows,
		&recoveryRows,
		&ownershipAuditRows,
		&recoveryAuditRows,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect([]int{
		ownershipRows,
		recoveryRows,
		ownershipAuditRows,
		recoveryAuditRows,
	}).To(Equal([]int{1, 1, 1, 1}))

	_, err = database.pool.Exec(context.Background(), `
		CREATE FUNCTION reject_sample_retirement_recovery_audit()
		RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
		  IF NEW.event_type =
		    'sample.retirement.recovery_evidence.registered' THEN
		    RAISE EXCEPTION 'forced retirement audit failure';
		  END IF;
		  RETURN NEW;
		END;
		$$;
		CREATE TRIGGER reject_sample_retirement_recovery_audit_trigger
		BEFORE INSERT ON ControlPlaneAuditEvent
		FOR EACH ROW EXECUTE FUNCTION reject_sample_retirement_recovery_audit()`)
	g.Expect(err).NotTo(HaveOccurred())
	rolledBackReference := "restore:must-roll-back"
	_, err = db.RegisterSampleRetirementRecoveryEvidence(
		database.ctx,
		types.SampleRetirementRecoveryEvidenceRegistrationInput{
			OrganizationID:          organizationID,
			VerifiedByUserAccountID: actorID,
			EvidenceKind:            types.SampleRetirementRecoveryEvidenceRestoreProof,
			Reference:               rolledBackReference,
			Checksum:                sampleRetirementChecksum(rolledBackReference),
			SourceKind:              "restore_verifier",
			SourceID:                uuid.New(),
			SourceChecksum:          sampleRetirementChecksum("restore-verifier-entry"),
			VerifiedAt:              time.Now().UTC().Add(-time.Minute),
		},
	)
	g.Expect(err).To(MatchError(ContainSubstring("forced retirement audit failure")))
	var rolledBackRows int
	err = database.pool.QueryRow(context.Background(), `
		SELECT count(*)
		FROM SampleRetirementRecoveryEvidence
		WHERE organization_id=$1 AND reference=$2`,
		organizationID,
		rolledBackReference,
	).Scan(&rolledBackRows)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rolledBackRows).To(Equal(0))
}

func TestSampleRetirementPersistsImmutableOrganizationScopedPreview(t *testing.T) {
	database := newTask4TestDatabase(t, 162, "UTC")
	organizationID, actorID := insertSampleRetirementPrincipal(t, database)
	environmentID := uuid.New()
	_, err := database.pool.Exec(
		context.Background(),
		`INSERT INTO Environment (id, organization_id, name) VALUES ($1, $2, 'sample-environment')`,
		environmentID,
		organizationID,
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	insertSampleRetirementOwnershipEvidence(
		t, database, organizationID, actorID,
		types.SampleRetirementSubjectEnvironment, environmentID,
		"sample:test-environment",
	)
	requested := []types.SampleRetirementSubject{{
		SubjectType:       types.SampleRetirementSubjectEnvironment,
		SubjectID:         environmentID,
		OwnershipMarker:   "sample:test-environment",
		OwnershipChecksum: sampleRetirementChecksum("sample:test-environment"),
	}}
	candidates, err := db.InspectSampleRetirementSubjects(
		database.ctx,
		organizationID,
		requested,
	)
	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(candidates).To(HaveLen(1))
	g.Expect(candidates[0].OrganizationID).To(Equal(organizationID))
	g.Expect(candidates[0].BeforeCount).To(Equal(1))
	g.Expect(candidates[0].Immutable).To(BeTrue())
	g.Expect(candidates[0].CurrentChecksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))

	reports, err := db.VerifyRetirementReverseReferences(
		database.ctx,
		organizationID,
		[]types.SampleRetirementSubject{candidates[0].Subject},
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(reports).To(HaveLen(1))
	g.Expect(reports[0].Retirable).To(BeTrue())
	g.Expect(reports[0].ProtectedReferenceCount).To(Equal(0))

	preview := sampleRetirementPreviewFixture(
		organizationID,
		actorID,
		candidates[0],
		reports[0],
	)
	g.Expect(db.SaveSampleRetirementPreview(database.ctx, &preview)).To(Succeed())

	detail, err := db.GetSampleRetirementDetail(database.ctx, organizationID, preview.Job.ID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(detail.Job.PreviewChecksum).To(Equal(preview.PreviewChecksum))
	g.Expect(detail.Items).To(HaveLen(1))
	g.Expect(detail.Items[0].SubjectID).To(Equal(environmentID))
	g.Expect(detail.Items[0].ExpectedChecksum).To(Equal(candidates[0].CurrentChecksum))

	_, err = db.GetSampleRetirementDetail(database.ctx, uuid.New(), preview.Job.ID)
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())

	changed := preview
	changed.Job.PreviewChecksum = sampleRetirementChecksum("changed-preview")
	changed.PreviewChecksum = changed.Job.PreviewChecksum
	err = db.SaveSampleRetirementPreview(database.ctx, &changed)
	g.Expect(err).To(MatchError(ContainSubstring("immutable")))

	reloaded, err := db.GetSampleRetirementDetail(database.ctx, organizationID, preview.Job.ID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(reloaded.Job.PreviewChecksum).To(Equal(preview.PreviewChecksum))
}

func TestSampleRetirementReportsProtectedReverseReferences(t *testing.T) {
	database := newTask4TestDatabase(t, 162, "UTC")
	organizationID, actorID := insertSampleRetirementPrincipal(t, database)
	fixture := insertSampleRetirementReferencedEnvironment(t, database, organizationID)
	insertSampleRetirementOwnershipEvidence(
		t, database, organizationID, actorID,
		types.SampleRetirementSubjectEnvironment, fixture.environmentID,
		"sample:referenced-environment",
	)
	requested := types.SampleRetirementSubject{
		SubjectType:       types.SampleRetirementSubjectEnvironment,
		SubjectID:         fixture.environmentID,
		OwnershipMarker:   "sample:referenced-environment",
		OwnershipChecksum: sampleRetirementChecksum("sample:referenced-environment"),
	}
	candidates, err := db.InspectSampleRetirementSubjects(
		database.ctx,
		organizationID,
		[]types.SampleRetirementSubject{requested},
	)
	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())

	reports, err := db.VerifyRetirementReverseReferences(
		database.ctx,
		organizationID,
		[]types.SampleRetirementSubject{candidates[0].Subject},
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(reports).To(HaveLen(1))
	g.Expect(reports[0].Retirable).To(BeFalse())
	g.Expect(reports[0].ProtectedReferenceCount).To(BeNumerically(">=", 1))
	g.Expect(reports[0].BlockingReasons).To(ContainElement("protected reverse references exist"))
	g.Expect(reports[0].References).To(ContainElement(SatisfyAll(
		HaveField("SourceType", "deploymentplan"),
		HaveField("SourceID", fixture.planID),
		HaveField("OrganizationID", organizationID),
		HaveField("Protected", true),
	)))
}

func TestSampleRetirementApplyIsAtomicRestartableAndRetainsAudit(t *testing.T) {
	database := newTask4TestDatabase(t, 162, "UTC")
	organizationID, actorID := insertSampleRetirementPrincipal(t, database)
	environmentID := uuid.New()
	_, err := database.pool.Exec(
		context.Background(),
		`INSERT INTO Environment (id, organization_id, name) VALUES ($1, $2, 'retirable-environment')`,
		environmentID,
		organizationID,
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	insertSampleRetirementOwnershipEvidence(
		t, database, organizationID, actorID,
		types.SampleRetirementSubjectEnvironment, environmentID,
		"sample:atomic",
	)
	auditEventID := insertSampleRetirementAuditEvent(
		t,
		database,
		organizationID,
		environmentID,
	)

	subject := types.SampleRetirementSubject{
		SubjectType:       types.SampleRetirementSubjectEnvironment,
		SubjectID:         environmentID,
		OwnershipMarker:   "sample:atomic",
		OwnershipChecksum: sampleRetirementChecksum("sample:atomic"),
	}
	candidates, err := db.InspectSampleRetirementSubjects(
		database.ctx,
		organizationID,
		[]types.SampleRetirementSubject{subject},
	)
	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	reports, err := db.VerifyRetirementReverseReferences(
		database.ctx,
		organizationID,
		[]types.SampleRetirementSubject{candidates[0].Subject},
	)
	g.Expect(err).NotTo(HaveOccurred())
	preview := sampleRetirementPreviewFixture(
		organizationID,
		actorID,
		candidates[0],
		reports[0],
	)
	g.Expect(db.SaveSampleRetirementPreview(database.ctx, &preview)).To(Succeed())
	approval := insertSampleRetirementApprovalFixture(
		t,
		database,
		organizationID,
		actorID,
		preview.Job.ID,
	)
	applyRequest := types.SampleRetirementApplyRequest{
		OrganizationID:     organizationID,
		ActorUserAccountID: actorID,
		JobID:              preview.Job.ID,
		PreviewChecksum:    preview.PreviewChecksum,
		ApprovalID:         approval.ApprovalRequestID.String(),
		ApprovalChecksum:   approval.ApprovalChecksum,
	}

	checkpoint, err := db.ApplySampleRetirementItemAtomically(
		database.ctx,
		organizationID,
		preview.Job.ID,
		preview.Items[0].ID,
		applyRequest,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(checkpoint.Sequence).To(Equal(int64(1)))
	g.Expect(checkpoint.AppliedItemCount).To(Equal(1))
	g.Expect(checkpoint.TombstoneCount).To(Equal(1))

	restarted, err := db.ApplySampleRetirementItemAtomically(
		database.ctx,
		organizationID,
		preview.Job.ID,
		preview.Items[0].ID,
		applyRequest,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(restarted.ID).To(Equal(checkpoint.ID))

	result, err := db.CompleteSampleRetirementApplyAtomically(
		database.ctx,
		organizationID,
		preview.Job.ID,
		applyRequest,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.State).To(Equal(types.SampleRetirementJobApplied))
	g.Expect(result.AppliedCount).To(Equal(1))
	g.Expect(result.TombstoneCount).To(Equal(1))

	var activeCount, auditEventCount, checkpointCount, tombstoneCount int
	g.Expect(database.pool.QueryRow(
		context.Background(),
		`SELECT count(*) FROM Environment WHERE id=$1 AND organization_id=$2`,
		environmentID,
		organizationID,
	).Scan(&activeCount)).To(Succeed())
	g.Expect(database.pool.QueryRow(
		context.Background(),
		`SELECT count(*) FROM ControlPlaneAuditEvent WHERE id=$1 AND organization_id=$2`,
		auditEventID,
		organizationID,
	).Scan(&auditEventCount)).To(Succeed())
	g.Expect(database.pool.QueryRow(
		context.Background(),
		`SELECT count(*) FROM SampleRetirementCheckpoint WHERE retirement_job_id=$1`,
		preview.Job.ID,
	).Scan(&checkpointCount)).To(Succeed())
	g.Expect(database.pool.QueryRow(
		context.Background(),
		`SELECT count(*) FROM AuditSubjectTombstone WHERE retirement_job_id=$1`,
		preview.Job.ID,
	).Scan(&tombstoneCount)).To(Succeed())
	g.Expect(activeCount).To(Equal(0))
	g.Expect(auditEventCount).To(Equal(1))
	g.Expect(checkpointCount).To(Equal(1))
	g.Expect(tombstoneCount).To(Equal(1))

	verification, err := db.VerifySampleRetirementPersistence(
		database.ctx,
		organizationID,
		preview.Job.ID,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(verification.ExactCounts).To(BeTrue())
	g.Expect(verification.TombstoneLineageValid).To(BeTrue())
	g.Expect(verification.AuditEventsRetained).To(BeTrue())
	g.Expect(verification.State).To(Equal(types.SampleRetirementJobVerified))
}

type sampleRetirementReferencedEnvironment struct {
	environmentID uuid.UUID
	planID        uuid.UUID
}

func insertSampleRetirementPrincipal(
	t *testing.T,
	database *task4TestDatabase,
) (uuid.UUID, uuid.UUID) {
	t.Helper()
	organizationID := uuid.New()
	actorID := uuid.New()
	_, err := database.pool.Exec(context.Background(), `
INSERT INTO Organization (id, name) VALUES ($1, $2);
INSERT INTO UserAccount (id, email) VALUES ($3, $4);
INSERT INTO Organization_UserAccount (
  organization_id, user_account_id, user_role
) VALUES ($1, $3, 'admin')`,
		organizationID,
		"sample-retirement-"+organizationID.String(),
		actorID,
		actorID.String()+"@example.test",
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	_, err = database.pool.Exec(context.Background(), `
INSERT INTO SampleRetirementRecoveryEvidence (
  organization_id, evidence_kind, reference, checksum, source_kind,
  source_id, source_checksum, verified_at, verified_by_useraccount_id
) VALUES
  ($1, 'backup', 'backup:test', $3, 'test', $4, $3, now(), $2),
  ($1, 'restore_proof', 'restore:test', $5, 'test', $4, $5, now(), $2)`,
		organizationID,
		actorID,
		sampleRetirementChecksum("backup"),
		uuid.New(),
		sampleRetirementChecksum("restore"),
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return organizationID, actorID
}

func insertSampleRetirementApprovalFixture(
	t *testing.T,
	database *task4TestDatabase,
	organizationID, actorID, jobID uuid.UUID,
) types.SampleRetirementApprovalBinding {
	t.Helper()
	g := NewWithT(t)
	group := types.PrincipalGroup{
		OrganizationID:  organizationID,
		Key:             "sample-retirement-approvers",
		DisplayName:     "Sample retirement approvers",
		Description:     "Test approval authority",
		CreatedByUserID: &actorID,
	}
	g.Expect(db.CreateAuthorizationPrincipalGroup(database.ctx, &group)).To(Succeed())
	member := types.PrincipalGroupMember{
		OrganizationID: organizationID,
		GroupID:        group.ID,
		UserAccountID:  actorID,
		EffectiveFrom:  time.Now().UTC().Add(-time.Hour),
		AddedByUserID:  &actorID,
		Reason:         "sample retirement approval fixture",
	}
	g.Expect(db.AddAuthorizationPrincipalGroupMember(database.ctx, &member)).To(Succeed())

	policy := types.DeploymentPolicy{
		OrganizationID: organizationID,
		Key:            "sample-retirement",
		Name:           "Sample retirement",
		Description:    "Requires one approved retirement decision",
	}
	g.Expect(db.CreateDeploymentPolicy(database.ctx, &policy)).To(Succeed())
	version := types.DeploymentPolicyVersion{
		OrganizationID:         organizationID,
		PolicyID:               policy.ID,
		CreatedByUserAccountID: actorID,
		Document: types.DeploymentPolicyDocument{
			Schema: types.DeploymentPolicySchemaV1,
			ApprovalRules: []types.ApprovalRule{{
				Key:              "retirement-approval",
				PrincipalGroupID: group.ID,
				Quorum:           1,
			}},
			AdmissionRules: types.AdmissionRules{
				AllowedResolutionModes: []types.RequirementResolutionMode{
					types.RequirementResolutionModeIncluded,
				},
			},
			CampaignRules: types.CampaignRules{
				MaximumWaveSize:    1,
				MaximumConcurrency: 1,
			},
			OverrideRules: types.OverrideRules{Allowed: false},
			BootstrapRules: types.BootstrapRules{
				Mode:             types.BootstrapModeRequireApproval,
				ApprovalRuleKeys: []string{"retirement-approval"},
			},
		},
	}
	g.Expect(db.CreateDeploymentPolicyVersion(database.ctx, &version)).To(Succeed())
	published, issues, err := db.PublishDeploymentPolicyVersion(
		database.ctx,
		version.ID,
		organizationID,
		actorID,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(issues).To(BeEmpty())
	g.Expect(published).NotTo(BeNil())
	g.Expect(db.BindDeploymentPolicy(
		database.ctx,
		types.PolicyBindingRequest{
			OrganizationID:         organizationID,
			PolicyVersionID:        published.ID,
			ScopeKind:              types.DeploymentPolicyBindingScopeOrganization,
			ScopeID:                organizationID,
			Role:                   types.DeploymentPolicyBindingRoleOwner,
			CreatedByUserAccountID: actorID,
		},
	)).To(Succeed())

	authorize := func(context.Context, types.ApprovalAuthorizationContext) error {
		return nil
	}
	request, err := db.RequestSampleRetirementApproval(
		database.ctx,
		types.SampleRetirementApprovalRequestInput{
			OrganizationID:           organizationID,
			SampleRetirementJobID:    jobID,
			RequestedByUserAccountID: actorID,
			ExpiresAt:                time.Now().UTC().Add(time.Hour),
			Authorize:                authorize,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(request.Requirements).To(HaveLen(1))
	_, err = db.RecordApprovalDecision(
		database.ctx,
		types.ApprovalDecisionInput{
			OrganizationID:          organizationID,
			ApprovalRequestID:       request.ID,
			ApprovalRequirementID:   request.Requirements[0].ID,
			ActorUserAccountID:      actorID,
			Decision:                types.ApprovalDecisionApprove,
			Comment:                 "Approved for retirement integration test",
			ExpectedRequestRevision: request.Revision,
			IdempotencyKey:          "sample-retirement-approve",
			Authorize:               authorize,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	binding, err := db.ResolveSampleRetirementApproval(
		database.ctx,
		organizationID,
		jobID,
		request.ID,
	)
	g.Expect(err).NotTo(HaveOccurred())
	return binding
}

func insertSampleRetirementOwnershipEvidence(
	t *testing.T,
	database *task4TestDatabase,
	organizationID, actorID uuid.UUID,
	subjectType types.SampleRetirementSubjectType,
	subjectID uuid.UUID,
	marker string,
) uuid.UUID {
	t.Helper()
	evidenceID := uuid.New()
	_, err := database.pool.Exec(context.Background(), `
INSERT INTO SampleRetirementOwnershipEvidence (
  id, organization_id, subject_type, subject_id, ownership_marker,
  ownership_checksum, source_reference, source_checksum,
  recorded_by_useraccount_id
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		evidenceID,
		organizationID,
		subjectType,
		subjectID,
		marker,
		sampleRetirementChecksum(marker),
		"evidence:test:"+evidenceID.String(),
		sampleRetirementChecksum("source:"+evidenceID.String()),
		actorID,
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return evidenceID
}

func insertSampleRetirementReferencedEnvironment(
	t *testing.T,
	database *task4TestDatabase,
	organizationID uuid.UUID,
) sampleRetirementReferencedEnvironment {
	t.Helper()
	applicationID := uuid.New()
	lifecycleID := uuid.New()
	channelID := uuid.New()
	bundleID := uuid.New()
	environmentID := uuid.New()
	planID := uuid.New()
	_, err := database.pool.Exec(context.Background(), `
INSERT INTO Application (id, name, type, organization_id)
VALUES ($1, 'sample-app', 'docker', $2);
INSERT INTO Lifecycle (id, name, organization_id)
VALUES ($3, 'sample-lifecycle', $2);
INSERT INTO Channel (
  id, organization_id, application_id, lifecycle_id, name, is_default
) VALUES ($4, $2, $1, $3, 'sample-channel', TRUE);
INSERT INTO ReleaseBundle (
  id, organization_id, application_id, channel_id, release_number,
  status, canonical_checksum, canonical_payload
) VALUES (
  $5, $2, $1, $4, '1.0.0', 'PUBLISHED',
  'sha256:' || repeat('a', 64), convert_to('{}', 'UTF8')
);
INSERT INTO Environment (id, organization_id, name)
VALUES ($6, $2, 'referenced-environment');
INSERT INTO DeploymentPlan (
  id, organization_id, release_bundle_id, application_id, channel_id,
  environment_id, status, canonical_checksum, canonical_payload
) VALUES (
  $7, $2, $5, $1, $4, $6, 'PUBLISHED',
  'sha256:' || repeat('b', 64), convert_to('{}', 'UTF8')
)`,
		applicationID,
		organizationID,
		lifecycleID,
		channelID,
		bundleID,
		environmentID,
		planID,
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return sampleRetirementReferencedEnvironment{
		environmentID: environmentID,
		planID:        planID,
	}
}

func insertSampleRetirementAuditEvent(
	t *testing.T,
	database *task4TestDatabase,
	organizationID, subjectID uuid.UUID,
) uuid.UUID {
	t.Helper()
	eventID := uuid.New()
	_, err := database.pool.Exec(context.Background(), `
INSERT INTO ControlPlaneAuditEvent (
  id, organization_id, sequence, event_type, outcome, payload
) VALUES ($1, $2, 1, 'audit.export.configured', 'succeeded', '{}'::jsonb);
INSERT INTO ControlPlaneAuditSubject (
  correlation_kind, subject_id, organization_id, first_event_id
) VALUES ('environment', $3, $2, $1);
INSERT INTO ControlPlaneAuditEventSubject (
  event_id, organization_id, correlation_kind, subject_id
) VALUES ($1, $2, 'environment', $3)`,
		eventID,
		organizationID,
		subjectID,
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return eventID
}

func sampleRetirementPreviewFixture(
	organizationID, actorID uuid.UUID,
	candidate types.SampleRetirementCandidate,
	report types.ReferenceReport,
) types.SampleRetirementPreview {
	jobID := uuid.New()
	itemID := uuid.New()
	previewChecksum := sampleRetirementChecksum("preview:" + jobID.String())
	return types.SampleRetirementPreview{
		Job: types.SampleRetirementJob{
			ID:                       jobID,
			OrganizationID:           organizationID,
			RequestedByUserAccountID: actorID,
			State:                    types.SampleRetirementJobPreviewed,
			BackupReference:          "backup:test",
			BackupChecksum:           sampleRetirementChecksum("backup"),
			RestoreProofReference:    "restore:test",
			RestoreProofChecksum:     sampleRetirementChecksum("restore"),
			AllowlistChecksum:        sampleRetirementChecksum("allowlist:" + jobID.String()),
			PreviewChecksum:          previewChecksum,
			RequestedItemCount:       1,
			PreviewedItemCount:       1,
		},
		Items: []types.SampleRetirementItem{{
			ID:                      itemID,
			OrganizationID:          organizationID,
			RetirementJobID:         jobID,
			Ordinal:                 1,
			SubjectType:             candidate.Subject.SubjectType,
			SubjectID:               candidate.Subject.SubjectID,
			OwnershipEvidenceID:     candidate.OwnershipEvidenceID,
			OwnershipMarker:         candidate.OwnershipMarker,
			OwnershipChecksum:       candidate.OwnershipChecksum,
			ExpectedChecksum:        candidate.CurrentChecksum,
			BeforeCount:             candidate.BeforeCount,
			ReferenceReportChecksum: sampleRetirementReferenceReportChecksum(report),
			State:                   types.SampleRetirementItemPending,
		}},
		ReferenceReports: []types.ReferenceReport{report},
		PreviewChecksum:  previewChecksum,
		RequestedCount:   1,
		RetirableCount:   1,
	}
}

func sampleRetirementChecksum(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("sha256:%x", sum)
}

func sampleRetirementReferenceReportChecksum(report types.ReferenceReport) string {
	payload, err := json.Marshal(struct {
		Schema string                `json:"schema"`
		Report types.ReferenceReport `json:"report"`
	}{
		Schema: "sample-retirement-reference-report/v1",
		Report: report,
	})
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}
