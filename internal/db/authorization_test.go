package db

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestNormalizeAuthorizationRoleDefinition(t *testing.T) {
	actorID := uuid.New()
	role := types.RoleDefinition{
		OrganizationID:  uuid.New(),
		Key:             "  Release-Managers  ",
		DisplayName:     "  Release Managers ",
		Description:     "  publishes releases  ",
		CreatedByUserID: &actorID,
		Permissions: []types.Action{
			types.ActionPlanExecute,
			types.ActionAuditView,
			types.ActionPlanExecute,
		},
	}

	err := normalizeAuthorizationRoleDefinition(&role)

	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(role.Key).To(Equal("release-managers"))
	g.Expect(role.DisplayName).To(Equal("Release Managers"))
	g.Expect(role.Description).To(Equal("publishes releases"))
	g.Expect(role.Revision).To(Equal(int64(1)))
	g.Expect(role.Permissions).To(Equal([]types.Action{
		types.ActionAuditView,
		types.ActionPlanExecute,
	}))
}

func TestNormalizeAuthorizationRoleDefinitionRejectsUnknownAction(t *testing.T) {
	role := types.RoleDefinition{
		OrganizationID: uuid.New(),
		Key:            "operators",
		DisplayName:    "Operators",
		Permissions:    []types.Action{"target.destroy"},
	}

	err := normalizeAuthorizationRoleDefinition(&role)

	g := NewWithT(t)
	g.Expect(err).To(MatchError(ContainSubstring("unsupported action")))
	g.Expect(err).To(MatchError(apierrors.ErrBadRequest))
}

func TestNormalizeAuthorizationRoleDefinitionRejectsBuiltInKeySquatting(t *testing.T) {
	for _, key := range []string{
		"legacy.read_only",
		"legacy.read_write",
		"legacy.admin",
	} {
		role := types.RoleDefinition{
			OrganizationID: uuid.New(),
			Key:            key,
			DisplayName:    "Squatter",
			Permissions:    []types.Action{types.ActionAuditView},
		}

		err := normalizeAuthorizationRoleDefinition(&role)

		NewWithT(t).Expect(err).To(MatchError(ContainSubstring("reserved")))
	}
}

func TestValidateRoleBindingRequiresSupportedScopeAndEffectiveInterval(t *testing.T) {
	now := time.Now().UTC()
	endsBeforeStart := now.Add(-time.Minute)
	binding := types.RoleBinding{
		OrganizationID:   uuid.New(),
		RoleDefinitionID: uuid.New(),
		PrincipalKind:    types.AuthorizationPrincipalUser,
		PrincipalID:      uuid.New(),
		Scope: types.ScopeRef{
			Kind: types.PermissionScopeApplication,
			ID:   uuid.New(),
		},
		EffectiveFrom:  now,
		EffectiveUntil: &endsBeforeStart,
		Reason:         "grant",
	}

	g := NewWithT(t)
	g.Expect(validateAuthorizationRoleBinding(binding)).To(
		MatchError(ContainSubstring("unsupported scope")),
	)

	binding.Scope.Kind = types.PermissionScopeEnvironment
	g.Expect(validateAuthorizationRoleBinding(binding)).To(
		MatchError(ContainSubstring("effectiveUntil")),
	)
}

func TestValidateEnrollmentAllowsOnlyOrganizationAndEnvironmentAndRequiresReason(t *testing.T) {
	enrollment := types.ControlPlaneEnrollment{
		OrganizationID: uuid.New(),
		Scope: types.ScopeRef{
			Kind: types.PermissionScopeCustomer,
			ID:   uuid.New(),
		},
		Enabled:       true,
		EffectiveFrom: time.Now().UTC(),
		ActorUserID:   uuid.New(),
		Reason:        "pilot",
	}

	g := NewWithT(t)
	g.Expect(validateControlPlaneEnrollment(enrollment)).To(
		MatchError(ContainSubstring("organization or environment")),
	)

	enrollment.Scope.Kind = types.PermissionScopeEnvironment
	enrollment.Reason = " "
	g.Expect(validateControlPlaneEnrollment(enrollment)).To(
		MatchError(ContainSubstring("reason is required")),
	)
}

func TestScopedAuthorizationMigrationContract(t *testing.T) {
	up, err := os.ReadFile(filepath.Join(
		"..",
		"migrations",
		"sql",
		"148_scoped_authorization_enrollment.up.sql",
	))
	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	source := string(up)

	for _, table := range []string{
		"RoleDefinition",
		"RolePermission",
		"RoleBinding",
		"RoleBindingRevision",
		"PrincipalGroup",
		"PrincipalGroupMember",
		"PrincipalGroupMemberRevision",
		"ControlPlaneEnrollment",
		"AuthorizationBackfillCheckpoint",
	} {
		g.Expect(source).To(ContainSubstring("CREATE TABLE " + table))
	}
	g.Expect(source).NotTo(ContainSubstring("ALTER TABLE Organization_UserAccount"))
	g.Expect(source).NotTo(ContainSubstring("DROP TABLE Organization_UserAccount"))
	g.Expect(source).To(ContainSubstring("legacy.read_write"))
	g.Expect(source).To(ContainSubstring("authorization_prevent_immutable_mutation"))
	g.Expect(source).To(ContainSubstring("ORGANIZATION_RETENTION"))
	g.Expect(source).To(ContainSubstring("authorization_organization_membership_guard"))
	g.Expect(source).To(ContainSubstring("principal_membership_created_at"))
	g.Expect(source).To(ContainSubstring("user_membership_created_at"))
	g.Expect(source).To(ContainSubstring("state IN ('active', 'revoked')"))
	g.Expect(source).To(ContainSubstring("RoleBindingRevision_immutable"))
	g.Expect(source).To(ContainSubstring("PrincipalGroupMemberRevision_immutable"))
	g.Expect(source).To(ContainSubstring("roledefinition_reserved_key_check"))

	down, err := os.ReadFile(filepath.Join(
		"..",
		"migrations",
		"sql",
		"148_scoped_authorization_enrollment.down.sql",
	))
	g.Expect(err).NotTo(HaveOccurred())
	downSource := string(down)
	g.Expect(downSource).To(ContainSubstring("refusing migration 148 rollback"))
	guardIndex := strings.Index(downSource, "DO $$")
	g.Expect(guardIndex).To(BeNumerically(">", 0))
	for _, table := range []string{
		"RoleDefinition",
		"RolePermission",
		"RoleBinding",
		"RoleBindingRevision",
		"PrincipalGroup",
		"PrincipalGroupMember",
		"PrincipalGroupMemberRevision",
		"ControlPlaneEnrollment",
		"AuthorizationBackfillCheckpoint",
	} {
		lock := "LOCK TABLE " + table + " IN ACCESS EXCLUSIVE MODE"
		lockIndex := strings.Index(downSource, lock)
		g.Expect(lockIndex).To(
			And(BeNumerically(">=", 0), BeNumerically("<", guardIndex)),
			table,
		)
	}
}

func TestAuthorizationBackfillStatementsAreIdempotentAndCheckpointed(t *testing.T) {
	g := NewWithT(t)

	for _, statement := range []string{
		authorizationRoleDefinitionBackfillSQL,
		authorizationRolePermissionBackfillSQL,
		authorizationCheckpointBackfillSQL,
	} {
		g.Expect(strings.ToUpper(statement)).To(ContainSubstring("ON CONFLICT"))
	}
	g.Expect(authorizationCheckpointBackfillSQL).To(ContainSubstring("built_in_roles_v1"))
	g.Expect(authorizationRoleDefinitionBackfillSQL).To(ContainSubstring("legacy.read_write"))
}

func TestAuthorizationAccessQueryUsesOneDecisionInstantAndLatestRevision(t *testing.T) {
	g := NewWithT(t)
	g.Expect(authorizationAccessGrantsSQL).NotTo(ContainSubstring("now()"))
	g.Expect(authorizationAccessGrantsSQL).To(ContainSubstring("@decisionAt"))
	g.Expect(authorizationAccessGrantsSQL).To(ContainSubstring("RoleBindingRevision"))
	g.Expect(authorizationAccessGrantsSQL).To(ContainSubstring("PrincipalGroupMemberRevision"))
	g.Expect(authorizationAccessGrantsSQL).To(ContainSubstring("principal_membership_created_at"))
	g.Expect(authorizationAccessGrantsSQL).To(ContainSubstring("user_membership_created_at"))
	g.Expect(authorizationAccessGrantsSQL).To(ContainSubstring("state = 'active'"))
}

func TestValidateAuthorizationRevocationRequiresIdentifiersInstantActorAndReason(t *testing.T) {
	valid := authorizationRevocationInput{
		OrganizationID: uuid.New(),
		SubjectID:      uuid.New(),
		EffectiveFrom:  time.Now().UTC(),
		ActorUserID:    uuid.New(),
		Reason:         "rotation ended",
	}
	g := NewWithT(t)
	g.Expect(validateAuthorizationRevocation(valid)).To(Succeed())

	valid.SubjectID = uuid.Nil
	g.Expect(validateAuthorizationRevocation(valid)).To(
		MatchError(ContainSubstring("required")),
	)
	valid.SubjectID = uuid.New()
	valid.Reason = " "
	g.Expect(validateAuthorizationRevocation(valid)).To(
		MatchError(ContainSubstring("reason")),
	)
}

func TestAuthorizationRevisionStatementsAreAppendOnlyAndMonotonic(t *testing.T) {
	g := NewWithT(t)
	for _, initial := range []string{
		authorizationRoleBindingInitialRevisionSQL,
		authorizationGroupMemberInitialRevisionSQL,
	} {
		g.Expect(initial).To(ContainSubstring("'active'"))
		g.Expect(initial).To(ContainSubstring("revision"))
	}
	for _, revocation := range []string{
		authorizationRoleBindingRevocationSQL,
		authorizationGroupMemberRevocationSQL,
	} {
		g.Expect(revocation).To(ContainSubstring("'revoked'"))
		g.Expect(revocation).To(ContainSubstring("max(revision)"))
		g.Expect(revocation).To(ContainSubstring("RETURNING created_at, revision"))
		g.Expect(strings.ToUpper(revocation)).NotTo(ContainSubstring("UPDATE "))
		g.Expect(strings.ToUpper(revocation)).NotTo(ContainSubstring("DELETE "))
	}
}

func TestAuthorizationCursorIsVersionedTenantCollectionAndParentBound(t *testing.T) {
	organizationID := uuid.New()
	parentID := uuid.New()
	cursorValue, err := encodeAuthorizationCursor(authorizationCursor{
		Version:        authorizationCursorVersion,
		OrganizationID: organizationID,
		Collection:     "group_members",
		ParentID:       parentID,
		CreatedAt:      time.Now().UTC(),
		ID:             uuid.New(),
	})
	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cursorValue).NotTo(ContainSubstring(organizationID.String()))

	cursor, err := decodeAuthorizationCursor(
		cursorValue,
		organizationID,
		"group_members",
		parentID,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cursor.OrganizationID).To(Equal(organizationID))

	_, err = decodeAuthorizationCursor(
		cursorValue,
		uuid.New(),
		"group_members",
		parentID,
	)
	g.Expect(err).To(MatchError(apierrors.ErrBadRequest))
	_, err = decodeAuthorizationCursor(
		cursorValue,
		organizationID,
		"bindings",
		parentID,
	)
	g.Expect(err).To(MatchError(apierrors.ErrBadRequest))
	_, err = decodeAuthorizationCursor(
		cursorValue,
		organizationID,
		"group_members",
		uuid.New(),
	)
	g.Expect(err).To(MatchError(apierrors.ErrBadRequest))

	cursorPayload, err := base64.RawURLEncoding.DecodeString(cursorValue)
	g.Expect(err).NotTo(HaveOccurred())
	tamperedPayload := base64.RawURLEncoding.EncodeToString(
		append(cursorPayload, byte('{')),
	)
	_, err = decodeAuthorizationCursor(
		tamperedPayload,
		organizationID,
		"group_members",
		parentID,
	)
	g.Expect(err).To(MatchError(apierrors.ErrBadRequest))
}
