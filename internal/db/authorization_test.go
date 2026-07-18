package db

import (
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
		"PrincipalGroup",
		"PrincipalGroupMember",
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

	down, err := os.ReadFile(filepath.Join(
		"..",
		"migrations",
		"sql",
		"148_scoped_authorization_enrollment.down.sql",
	))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(down)).To(ContainSubstring("refusing migration 148 rollback"))
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
