package mapping

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
)

func UserAccountToAPI(u types.UserAccountWithRole) api.UserAccountResponse {
	return api.UserAccountResponse{
		UserAccountWithRole: u,
		ImageUrl:            CreateImageURL(u.ImageID),
	}
}
