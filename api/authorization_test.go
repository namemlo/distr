package api

import (
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCreateAuthorizationRoleRequestValidation(t *testing.T) {
	g := NewWithT(t)

	valid := CreateAuthorizationRoleRequest{
		Key:         "release-managers",
		DisplayName: "Release Managers",
		Permissions: []types.Action{
			types.ActionReleasePublish,
			types.ActionPlanExecute,
		},
	}
	g.Expect(valid.Validate()).To(Succeed())

	valid.Permissions = []types.Action{"release.delete"}
	g.Expect(valid.Validate()).To(MatchError(ContainSubstring("unsupported permission")))

	valid.Permissions = nil
	g.Expect(valid.Validate()).To(MatchError(ContainSubstring("permission")))

	for _, reserved := range []string{
		"legacy.read_only",
		"legacy.read_write",
		"legacy.admin",
	} {
		valid.Permissions = []types.Action{types.ActionAuditView}
		valid.Key = reserved
		g.Expect(valid.Validate()).To(
			MatchError(ContainSubstring("reserved")),
			reserved,
		)
	}
}

func TestCreateAuthorizationRoleBindingRequestValidation(t *testing.T) {
	now := time.Now().UTC()
	until := now.Add(time.Hour)
	request := CreateAuthorizationRoleBindingRequest{
		RoleDefinitionID: uuid.New(),
		PrincipalKind:    types.AuthorizationPrincipalGroup,
		PrincipalID:      uuid.New(),
		Scope: types.ScopeRef{
			Kind: types.PermissionScopeDeploymentUnit,
			ID:   uuid.New(),
		},
		EffectiveFrom:  now,
		EffectiveUntil: &until,
		Reason:         "on-call operators",
	}

	g := NewWithT(t)
	g.Expect(request.Validate()).To(Succeed())

	request.Scope.Kind = types.PermissionScopeApplication
	g.Expect(request.Validate()).To(MatchError(ContainSubstring("scope")))
}

func TestCreateControlPlaneEnrollmentRequestValidation(t *testing.T) {
	now := time.Now().UTC()
	request := CreateControlPlaneEnrollmentRequest{
		Scope: types.ScopeRef{
			Kind: types.PermissionScopeEnvironment,
			ID:   uuid.New(),
		},
		Enabled:       true,
		EffectiveFrom: now,
		Reason:        "staged rollout",
	}

	g := NewWithT(t)
	g.Expect(request.Validate()).To(Succeed())

	request.Scope.Kind = types.PermissionScopeCampaign
	g.Expect(request.Validate()).To(MatchError(ContainSubstring("organization or environment")))
}

func TestAuthorizationRevocationRequestRequiresInstantAndReason(t *testing.T) {
	request := RevokeAuthorizationGrantRequest{
		EffectiveFrom: time.Now().UTC(),
		Reason:        "operator removed from rotation",
	}
	g := NewWithT(t)
	g.Expect(request.Validate()).To(Succeed())

	request.EffectiveFrom = time.Time{}
	g.Expect(request.Validate()).To(MatchError(ContainSubstring("effectiveFrom")))

	request.EffectiveFrom = time.Now().UTC()
	request.Reason = " "
	g.Expect(request.Validate()).To(MatchError(ContainSubstring("reason")))
}

func TestAuthorizationListRequestDistinguishesOmittedFromExplicitZeroLimit(t *testing.T) {
	g := NewWithT(t)
	g.Expect((AuthorizationListRequest{}).Validate()).To(Succeed())

	zero := 0
	g.Expect((AuthorizationListRequest{Limit: &zero}).Validate()).To(
		MatchError(ContainSubstring("between 1 and 100")),
	)
	tooLarge := 101
	g.Expect((AuthorizationListRequest{Limit: &tooLarge}).Validate()).To(
		MatchError(ContainSubstring("between 1 and 100")),
	)
	g.Expect((AuthorizationListRequest{Cursor: "not+urlsafe"}).Validate()).To(
		MatchError(ContainSubstring("opaque")),
	)
}
