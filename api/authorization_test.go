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
