package db

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

const (
	authorizationDefaultPageLimit = 50
	authorizationMaximumPageLimit = 100
	authorizationCursorVersion    = 1
)

type authorizationCursor struct {
	Version        int       `json:"v"`
	OrganizationID uuid.UUID `json:"organizationId"`
	Collection     string    `json:"collection"`
	ParentID       uuid.UUID `json:"parentId,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	ID             uuid.UUID `json:"id"`
}

func normalizeAuthorizationListFilter(
	filter types.AuthorizationListFilter,
) (int, *authorizationCursor, error) {
	if filter.OrganizationID == uuid.Nil || filter.Collection == "" {
		return 0, nil, apierrors.ErrBadRequest
	}
	limit := filter.Limit
	if limit == 0 {
		limit = authorizationDefaultPageLimit
	}
	if limit < 1 || limit > authorizationMaximumPageLimit {
		return 0, nil, apierrors.ErrBadRequest
	}
	cursor, err := decodeAuthorizationCursor(
		filter.Cursor,
		filter.OrganizationID,
		filter.Collection,
		filter.ParentID,
	)
	if err != nil {
		return 0, nil, err
	}
	return limit, cursor, nil
}

func authorizationCursorArguments(
	cursor *authorizationCursor,
) (any, any) {
	if cursor == nil {
		return nil, nil
	}
	return cursor.CreatedAt, cursor.ID
}

func completeAuthorizationPage[T any](
	items []T,
	limit int,
	filter types.AuthorizationListFilter,
	key func(T) (time.Time, uuid.UUID),
) (types.Page[T], error) {
	page := types.Page[T]{Items: []T{}}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	page.Items = items
	if !hasMore || len(items) == 0 {
		return page, nil
	}
	createdAt, id := key(items[len(items)-1])
	nextCursor, err := encodeAuthorizationCursor(authorizationCursor{
		Version:        authorizationCursorVersion,
		OrganizationID: filter.OrganizationID,
		Collection:     filter.Collection,
		ParentID:       filter.ParentID,
		CreatedAt:      createdAt,
		ID:             id,
	})
	if err != nil {
		return page, err
	}
	page.NextCursor = nextCursor
	return page, nil
}

func encodeAuthorizationCursor(cursor authorizationCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("could not encode authorization cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeAuthorizationCursor(
	value string,
	organizationID uuid.UUID,
	collection string,
	parentID uuid.UUID,
) (*authorizationCursor, error) {
	if value == "" {
		return nil, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, apierrors.ErrBadRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var cursor authorizationCursor
	if err := decoder.Decode(&cursor); err != nil {
		return nil, apierrors.ErrBadRequest
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, apierrors.ErrBadRequest
	}
	if cursor.Version != authorizationCursorVersion ||
		cursor.OrganizationID != organizationID ||
		cursor.Collection != collection ||
		cursor.ParentID != parentID ||
		cursor.CreatedAt.IsZero() ||
		cursor.ID == uuid.Nil {
		return nil, apierrors.ErrBadRequest
	}
	return &cursor, nil
}
