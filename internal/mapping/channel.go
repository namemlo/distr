package mapping

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
)

func ChannelToAPI(channel types.Channel) api.Channel {
	return api.Channel{
		ID:                          channel.ID,
		CreatedAt:                   channel.CreatedAt,
		UpdatedAt:                   channel.UpdatedAt,
		ApplicationID:               channel.ApplicationID,
		LifecycleID:                 channel.LifecycleID,
		Name:                        channel.Name,
		Description:                 channel.Description,
		SortOrder:                   channel.SortOrder,
		IsDefault:                   channel.IsDefault,
		AllowedVersionRanges:        channel.AllowedVersionRanges,
		AllowedPrereleasePatterns:   channel.AllowedPrereleasePatterns,
		AllowedSourceBranchPatterns: channel.AllowedSourceBranchPatterns,
		AllowedSourceTagPatterns:    channel.AllowedSourceTagPatterns,
	}
}
