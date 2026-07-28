package operatorqueries

import (
	"bytes"
	"crypto/hmac"
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

type signedOperatorCursor struct {
	Payload operatorCursor `json:"p"`
	MAC     string         `json:"m"`
}

// CursorCodec signs and verifies opaque operator pagination cursors. The
// signing key is deliberately private and copied at construction time so it
// cannot be mutated through the caller's byte slice.
type CursorCodec struct {
	signingKey []byte
}

func NewCursorCodec(signingKey []byte) CursorCodec {
	return CursorCodec{signingKey: bytes.Clone(signingKey)}
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

func EncodeCursor(codec CursorCodec, scope CursorScope, tuple CursorTuple) (string, error) {
	if !codec.valid() || !validCursorScope(scope) || tuple.CreatedAt.IsZero() || tuple.ID == uuid.Nil {
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
	envelope, err := json.Marshal(signedOperatorCursor{
		Payload: cursor,
		MAC:     codec.sign(payload),
	})
	if err != nil {
		return "", err
	}
	value := base64.RawURLEncoding.EncodeToString(envelope)
	if len(value) > MaximumCursorLength {
		return "", invalidCursorError()
	}
	return value, nil
}

func DecodeCursor(codec CursorCodec, value string, scope CursorScope) (*CursorTuple, error) {
	if value == "" {
		return nil, nil
	}
	if !validCursorScope(scope) {
		return nil, invalidCursorError()
	}
	cursor, err := codec.decode(value)
	if err != nil {
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
func CursorDecisionAt(codec CursorCodec, value string) (time.Time, error) {
	cursor, err := codec.decode(value)
	if err != nil {
		return time.Time{}, invalidCursorError()
	}
	if cursor.Version != operatorCursorVersion || cursor.DecisionAt.IsZero() {
		return time.Time{}, invalidCursorError()
	}
	return cursor.DecisionAt.UTC(), nil
}

func CompletePage[T any](
	codec CursorCodec,
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
	nextCursor, err := EncodeCursor(codec, scope, key(items[len(items)-1]))
	if err != nil {
		return page, err
	}
	page.NextCursor = nextCursor
	return page, nil
}

func (codec CursorCodec) valid() bool {
	return len(codec.signingKey) > 0
}

func (codec CursorCodec) sign(payload []byte) string {
	mac := hmac.New(sha256.New, codec.signingKey)
	_, _ = mac.Write([]byte("operator-pagination-cursor/v1"))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (codec CursorCodec) decode(value string) (operatorCursor, error) {
	var zero operatorCursor
	if !codec.valid() || value == "" || len(value) > MaximumCursorLength {
		return zero, invalidCursorError()
	}
	envelopeBytes, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil {
		return zero, invalidCursorError()
	}
	decoder := json.NewDecoder(bytes.NewReader(envelopeBytes))
	decoder.DisallowUnknownFields()
	var envelope signedOperatorCursor
	if err := decoder.Decode(&envelope); err != nil {
		return zero, invalidCursorError()
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return zero, invalidCursorError()
	}
	canonicalEnvelope, err := json.Marshal(envelope)
	if err != nil || base64.RawURLEncoding.EncodeToString(canonicalEnvelope) != value {
		return zero, invalidCursorError()
	}
	payload, err := json.Marshal(envelope.Payload)
	if err != nil {
		return zero, invalidCursorError()
	}
	providedMAC, err := base64.RawURLEncoding.Strict().DecodeString(envelope.MAC)
	if err != nil {
		return zero, invalidCursorError()
	}
	expectedMAC, err := base64.RawURLEncoding.Strict().DecodeString(codec.sign(payload))
	if err != nil || !hmac.Equal(providedMAC, expectedMAC) {
		return zero, invalidCursorError()
	}
	if envelope.Payload.Version != operatorCursorVersion ||
		envelope.Payload.Scope == "" ||
		envelope.Payload.DecisionAt.IsZero() ||
		!checksumPattern.MatchString(envelope.Payload.ScopeChecksum) ||
		!checksumPattern.MatchString(envelope.Payload.FilterChecksum) ||
		envelope.Payload.CreatedAt.IsZero() ||
		envelope.Payload.ID == uuid.Nil {
		return zero, invalidCursorError()
	}
	return envelope.Payload, nil
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
