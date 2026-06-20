package mapping

import (
	"testing"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestChannelToAPI(t *testing.T) {
	g := NewWithT(t)
	id := uuid.New()
	orgID := uuid.New()
	applicationID := uuid.New()
	lifecycleID := uuid.New()
	createdAt := time.Date(2026, 6, 20, 9, 30, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 6, 20, 10, 45, 0, 0, time.UTC)

	res := ChannelToAPI(types.Channel{
		ID:                          id,
		CreatedAt:                   createdAt,
		UpdatedAt:                   updatedAt,
		OrganizationID:              orgID,
		ApplicationID:               applicationID,
		LifecycleID:                 lifecycleID,
		Name:                        "Stable",
		Description:                 "Default production-ready channel",
		SortOrder:                   10,
		IsDefault:                   true,
		AllowedVersionRanges:        []string{">=1.0.0 <2.0.0"},
		AllowedPrereleasePatterns:   []string{"rc.*"},
		AllowedSourceBranchPatterns: []string{"release/*"},
		AllowedSourceTagPatterns:    []string{"v*"},
	})

	g.Expect(res).To(Equal(api.Channel{
		ID:                          id,
		CreatedAt:                   createdAt,
		UpdatedAt:                   updatedAt,
		ApplicationID:               applicationID,
		LifecycleID:                 lifecycleID,
		Name:                        "Stable",
		Description:                 "Default production-ready channel",
		SortOrder:                   10,
		IsDefault:                   true,
		AllowedVersionRanges:        []string{">=1.0.0 <2.0.0"},
		AllowedPrereleasePatterns:   []string{"rc.*"},
		AllowedSourceBranchPatterns: []string{"release/*"},
		AllowedSourceTagPatterns:    []string{"v*"},
	}))
}
