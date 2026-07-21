package handlers

import (
	"context"
	"errors"
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCampaignRevisionDraftEditWithoutAuthorityIsDenied(t *testing.T) {
	g := NewWithT(t)
	denied := campaignActionAuthorizerFunc(func(
		context.Context,
		types.CampaignAuthorizationContext,
	) error {
		return apierrors.ErrForbidden
	})

	err := authorizeCampaignDraftMutation(
		t.Context(),
		denied,
		uuid.New(),
		uuid.New(),
		uuid.New(),
	)

	g.Expect(errors.Is(err, apierrors.ErrForbidden)).To(BeTrue())
}

func TestCampaignRevisionProductionAuthorizerDeniesMissingAuthentication(t *testing.T) {
	g := NewWithT(t)

	err := newCampaignActionAuthorizer().AuthorizeCampaignAction(
		t.Context(),
		types.CampaignAuthorizationContext{
			OrganizationID:  uuid.New(),
			ActorUserID:     uuid.New(),
			CampaignDraftID: uuid.New(),
		},
	)

	g.Expect(errors.Is(err, apierrors.ErrForbidden)).To(BeTrue())
}
