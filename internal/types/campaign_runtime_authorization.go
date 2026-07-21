package types

import "github.com/google/uuid"

// CampaignRuntimeAuthorizationTarget is the immutable campaign and environment
// scope resolved for a runtime mutation.
type CampaignRuntimeAuthorizationTarget struct {
	CampaignDraftID uuid.UUID
	EnvironmentIDs  []uuid.UUID
}
