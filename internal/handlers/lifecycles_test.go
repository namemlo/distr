package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/authjwt"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

func TestLifecycleFromCreateUpdateRequest(t *testing.T) {
	g := NewWithT(t)
	orgID := uuid.New()
	envID := uuid.New()

	lifecycle := lifecycleFromCreateUpdateRequest(orgID, api.CreateUpdateLifecycleRequest{
		Name:        " Standard ",
		Description: "Development to production promotion",
		SortOrder:   20,
		Phases: []api.CreateUpdateLifecyclePhaseRequest{
			{
				Name:                         " Development ",
				Description:                  "Internal validation",
				SortOrder:                    10,
				EnvironmentIDs:               []uuid.UUID{envID},
				Optional:                     true,
				AutomaticPromotion:           false,
				MinimumSuccessfulDeployments: 2,
			},
		},
	})

	g.Expect(lifecycle).To(Equal(types.Lifecycle{
		OrganizationID: orgID,
		Name:           "Standard",
		Description:    "Development to production promotion",
		SortOrder:      20,
		Phases: []types.LifecyclePhase{
			{
				Name:                         "Development",
				Description:                  "Internal validation",
				SortOrder:                    10,
				EnvironmentIDs:               []uuid.UUID{envID},
				Optional:                     true,
				AutomaticPromotion:           false,
				MinimumSuccessfulDeployments: 2,
			},
		},
	}))
}

func TestLifecycleResponses(t *testing.T) {
	g := NewWithT(t)
	id := uuid.New()

	responses := lifecycleResponses([]types.Lifecycle{{ID: id, Name: "Standard"}})

	g.Expect(responses).To(Equal([]api.Lifecycle{{ID: id, Name: "Standard", Phases: []api.LifecyclePhase{}}}))
}

func TestReplaceLifecyclePhasesHandlerRejectsInvalidPhaseLists(t *testing.T) {
	environmentID := uuid.New()

	tests := []struct {
		name string
		body string
	}{
		{
			name: "duplicate trimmed phase names",
			body: `{"phases":[` +
				`{"name":"Production","sortOrder":10,"environmentIds":["` + environmentID.String() + `"]},` +
				`{"name":" Production ","sortOrder":20,"environmentIds":["` + environmentID.String() + `"]}` +
				`]}`,
		},
		{
			name: "duplicate phase sort orders",
			body: `{"phases":[` +
				`{"name":"Staging","sortOrder":10,"environmentIds":["` + environmentID.String() + `"]},` +
				`{"name":"Production","sortOrder":10,"environmentIds":["` + environmentID.String() + `"]}` +
				`]}`,
		},
		{
			name: "duplicate environment IDs inside a phase",
			body: `{"phases":[` +
				`{"name":"Production","sortOrder":10,"environmentIds":["` +
				environmentID.String() + `","` + environmentID.String() + `"]}` +
				`]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			lifecycleID := uuid.New()
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(
				http.MethodPut,
				"/api/v1/lifecycles/"+lifecycleID.String()+"/phases",
				strings.NewReader(tt.body),
			)
			request.SetPathValue("lifecycleId", lifecycleID.String())
			ctx := internalctx.WithLogger(request.Context(), zap.NewNop())
			request = request.WithContext(auth.Authentication.NewContext(ctx, testLifecycleAuth()))

			replaceLifecyclePhasesHandler().ServeHTTP(recorder, request)

			g.Expect(recorder.Code).To(Equal(http.StatusBadRequest))
		})
	}
}

type lifecycleTestAuth struct {
	orgID uuid.UUID
	role  types.UserRole
}

func testLifecycleAuth() lifecycleTestAuth {
	return lifecycleTestAuth{
		orgID: uuid.New(),
		role:  types.UserRoleAdmin,
	}
}

func (a lifecycleTestAuth) CurrentUserID() uuid.UUID {
	return uuid.New()
}

func (a lifecycleTestAuth) CurrentUserEmail() string {
	return "admin@example.com"
}

func (a lifecycleTestAuth) CurrentUserRole() *types.UserRole {
	return &a.role
}

func (a lifecycleTestAuth) CurrentOrgID() *uuid.UUID {
	return &a.orgID
}

func (a lifecycleTestAuth) CurrentCustomerOrgID() *uuid.UUID {
	return nil
}

func (a lifecycleTestAuth) CurrentPartnerOrgID() *uuid.UUID {
	return nil
}

func (a lifecycleTestAuth) CurrentUserEmailVerified() bool {
	return true
}

func (a lifecycleTestAuth) TokenScope() authjwt.TokenScope {
	return ""
}

func (a lifecycleTestAuth) IsSuperAdmin() bool {
	return false
}

func (a lifecycleTestAuth) Token() any {
	return nil
}

func (a lifecycleTestAuth) CurrentOrg() *types.Organization {
	return &types.Organization{ID: a.orgID}
}

func (a lifecycleTestAuth) CurrentUser() *types.UserAccount {
	return &types.UserAccount{ID: uuid.New(), Email: a.CurrentUserEmail()}
}
