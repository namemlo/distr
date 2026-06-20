package api

import (
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

type CreateUpdateChannelRequest struct {
	ApplicationID uuid.UUID `json:"applicationId"`
	LifecycleID   uuid.UUID `json:"lifecycleId"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	SortOrder     int       `json:"sortOrder"`
	IsDefault     bool      `json:"isDefault"`
}

func (r *CreateUpdateChannelRequest) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return validation.NewValidationFailedError("name is required")
	}
	if r.ApplicationID == uuid.Nil {
		return validation.NewValidationFailedError("applicationId is required")
	}
	if r.LifecycleID == uuid.Nil {
		return validation.NewValidationFailedError("lifecycleId is required")
	}
	if r.SortOrder < 0 {
		return validation.NewValidationFailedError("sortOrder must be non-negative")
	}
	return nil
}

type Channel struct {
	ID            uuid.UUID `json:"id"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
	ApplicationID uuid.UUID `json:"applicationId"`
	LifecycleID   uuid.UUID `json:"lifecycleId"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	SortOrder     int       `json:"sortOrder"`
	IsDefault     bool      `json:"isDefault"`
}
