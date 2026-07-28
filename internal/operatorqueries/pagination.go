package operatorqueries

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

const (
	operatorCursorVersion = 1
	MaximumCursorLength   = 2048
)

var checksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type CursorScope struct {
	OrganizationID uuid.UUID
	Collection     types.OperatorCollection
	DecisionAt     time.Time
	ScopeChecksum  string
	FilterChecksum string
}

type CursorTuple struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

type operatorCursor struct {
	Version        int       `json:"v"`
	Scope          string    `json:"s"`
	DecisionAt     time.Time `json:"d"`
	ScopeChecksum  string    `json:"a"`
	FilterChecksum string    `json:"f"`
	CreatedAt      time.Time `json:"t"`
	ID             uuid.UUID `json:"i"`
}

func NormalizePageRequest(request types.PageRequest) (int, error) {
	limit := request.Limit
	if limit == 0 {
		limit = types.OperatorDefaultPageLimit
	}
	if limit < 1 || limit > types.OperatorMaximumPageLimit {
		return 0, apierrors.NewBadRequest("limit must be between 1 and 100")
	}
	return limit, nil
}

func CanonicalFilterChecksum(filter any) (string, error) {
	payload, err := json.Marshal(filter)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(hash[:]), nil
}

func EncodeCursor(scope CursorScope, tuple CursorTuple) (string, error) {
	if !validCursorScope(scope) || tuple.CreatedAt.IsZero() || tuple.ID == uuid.Nil {
		return "", invalidCursorError()
	}
	cursor := operatorCursor{
		Version:        operatorCursorVersion,
		Scope:          cursorScopeFingerprint(scope.OrganizationID, scope.Collection),
		DecisionAt:     scope.DecisionAt.UTC(),
		ScopeChecksum:  scope.ScopeChecksum,
		FilterChecksum: scope.FilterChecksum,
		CreatedAt:      tuple.CreatedAt.UTC(),
		ID:             tuple.ID,
	}
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func DecodeCursor(value string, scope CursorScope) (*CursorTuple, error) {
	if value == "" {
		return nil, nil
	}
	if len(value) > MaximumCursorLength || !validCursorScope(scope) {
		return nil, invalidCursorError()
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil {
		return nil, invalidCursorError()
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var cursor operatorCursor
	if err := decoder.Decode(&cursor); err != nil {
		return nil, invalidCursorError()
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, invalidCursorError()
	}
	canonical, err := json.Marshal(cursor)
	if err != nil || base64.RawURLEncoding.EncodeToString(canonical) != value {
		return nil, invalidCursorError()
	}
	if cursor.Version != operatorCursorVersion ||
		cursor.Scope != cursorScopeFingerprint(scope.OrganizationID, scope.Collection) ||
		!cursor.DecisionAt.Equal(scope.DecisionAt.UTC()) ||
		cursor.ScopeChecksum != scope.ScopeChecksum ||
		cursor.FilterChecksum != scope.FilterChecksum ||
		cursor.CreatedAt.IsZero() ||
		cursor.ID == uuid.Nil {
		return nil, invalidCursorError()
	}
	return &CursorTuple{CreatedAt: cursor.CreatedAt, ID: cursor.ID}, nil
}

// CursorDecisionAt extracts the authenticated pagination snapshot instant. The
// caller must still decode the cursor against its re-authorized CursorScope.
func CursorDecisionAt(value string) (time.Time, error) {
	if value == "" || len(value) > MaximumCursorLength {
		return time.Time{}, invalidCursorError()
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil {
		return time.Time{}, invalidCursorError()
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var cursor operatorCursor
	if err := decoder.Decode(&cursor); err != nil {
		return time.Time{}, invalidCursorError()
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || cursor.Version != operatorCursorVersion || cursor.DecisionAt.IsZero() {
		return time.Time{}, invalidCursorError()
	}
	canonical, err := json.Marshal(cursor)
	if err != nil || base64.RawURLEncoding.EncodeToString(canonical) != value {
		return time.Time{}, invalidCursorError()
	}
	return cursor.DecisionAt.UTC(), nil
}

func CompletePage[T any](
	items []T,
	limit int,
	scope CursorScope,
	total *int64,
	key func(T) CursorTuple,
) (types.OperatorPage[T], error) {
	page := types.OperatorPage[T]{Items: []T{}, Total: total}
	if limit < 1 || limit > types.OperatorMaximumPageLimit || key == nil {
		return page, apierrors.ErrBadRequest
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	page.Items = items
	if !hasMore || len(items) == 0 {
		return page, nil
	}
	nextCursor, err := EncodeCursor(scope, key(items[len(items)-1]))
	if err != nil {
		return page, err
	}
	page.NextCursor = nextCursor
	return page, nil
}

func validCursorScope(scope CursorScope) bool {
	return scope.OrganizationID != uuid.Nil &&
		scope.Collection.Valid() &&
		!scope.DecisionAt.IsZero() &&
		checksumPattern.MatchString(scope.ScopeChecksum) &&
		checksumPattern.MatchString(scope.FilterChecksum)
}

func cursorScopeFingerprint(
	organizationID uuid.UUID,
	collection types.OperatorCollection,
) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte("operator-cursor-scope/v1"))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(organizationID[:])
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(collection))
	return base64.RawURLEncoding.EncodeToString(hash.Sum(nil))
}

func invalidCursorError() error {
	return apierrors.NewBadRequest("cursor is invalid")
}
