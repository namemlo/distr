package operatorqueries

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

var operatorAuditSubjectTypePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func SearchOperatorAudit(
	ctx context.Context,
	filter types.AuditFilter,
	request types.PageRequest,
	cursorCodec CursorCodec,
) (types.OperatorPage[types.OperatorAuditRow], error) {
	if !cursorCodec.valid() {
		return types.OperatorPage[types.OperatorAuditRow]{}, invalidCursorError()
	}
	filter.Action = strings.TrimSpace(filter.Action)
	filter.SubjectType = strings.TrimSpace(filter.SubjectType)
	filter.Search = strings.TrimSpace(filter.Search)
	if !validOperatorAuditFilter(filter) {
		return types.OperatorPage[types.OperatorAuditRow]{}, apierrors.ErrBadRequest
	}

	scopes, err := AuditViewScopesFromOperatorScopeFilter(filter.OperatorScopeFilter)
	if err != nil {
		return types.OperatorPage[types.OperatorAuditRow]{}, err
	}
	if scopes.Empty() {
		return types.OperatorPage[types.OperatorAuditRow]{}, apierrors.ErrForbidden
	}
	limit, err := NormalizePageRequest(request)
	if err != nil {
		return types.OperatorPage[types.OperatorAuditRow]{}, err
	}
	cursorScope, err := auditCursorScope(filter, scopes)
	if err != nil {
		return types.OperatorPage[types.OperatorAuditRow]{}, err
	}
	cursor, err := DecodeCursor(cursorCodec, request.Cursor, cursorScope)
	if err != nil {
		return types.OperatorPage[types.OperatorAuditRow]{}, err
	}

	var afterCreatedAt *time.Time
	var afterID *uuid.UUID
	if cursor != nil {
		afterCreatedAt = new(cursor.CreatedAt)
		afterID = new(cursor.ID)
	}
	items, total, err := db.SearchOperatorAuditRows(
		ctx,
		filter,
		afterCreatedAt,
		afterID,
		limit+1,
	)
	if err != nil {
		return types.OperatorPage[types.OperatorAuditRow]{}, err
	}
	return CompletePage(cursorCodec, items, limit, cursorScope, total, func(item types.OperatorAuditRow) CursorTuple {
		return CursorTuple{CreatedAt: item.CreatedAt, ID: item.ID}
	})
}

func GetOperatorAudit(
	ctx context.Context,
	filter types.OperatorScopeFilter,
	auditEventID uuid.UUID,
) (*types.OperatorAuditDetail, error) {
	scopes, err := AuditViewScopesFromOperatorScopeFilter(filter)
	if err != nil {
		return nil, err
	}
	if scopes.Empty() {
		return nil, apierrors.ErrForbidden
	}
	return db.GetOperatorAuditDetail(ctx, filter, auditEventID)
}

func ListOperatorAuditEvidence(
	ctx context.Context,
	filter types.OperatorScopeFilter,
	auditEventID uuid.UUID,
) ([]types.OperatorEvidenceRef, error) {
	detail, err := GetOperatorAudit(ctx, filter, auditEventID)
	if err != nil {
		return nil, err
	}
	return detail.Evidence, nil
}

func auditCursorScope(filter types.AuditFilter, scopes AuditViewScopes) (CursorScope, error) {
	filterChecksum, err := CanonicalFilterChecksum(struct {
		Action             string     `json:"action"`
		SubjectType        string     `json:"subjectType"`
		SubjectID          *uuid.UUID `json:"subjectId"`
		ActorUserAccountID *uuid.UUID `json:"actorUserAccountId"`
		From               *time.Time `json:"from"`
		To                 *time.Time `json:"to"`
		Search             string     `json:"search"`
	}{
		Action: filter.Action, SubjectType: filter.SubjectType, SubjectID: filter.SubjectID,
		ActorUserAccountID: filter.ActorUserAccountID,
		From:               filter.From, To: filter.To, Search: filter.Search,
	})
	if err != nil {
		return CursorScope{}, err
	}
	return CursorScope{
		OrganizationID: filter.OrganizationID,
		Collection:     types.OperatorCollectionAudit,
		DecisionAt:     scopes.DecisionAt,
		ScopeChecksum:  scopes.Checksum(),
		FilterChecksum: filterChecksum,
	}, nil
}

func validOperatorAuditFilter(filter types.AuditFilter) bool {
	if invalidOptionalOperatorUUID(filter.SubjectID) ||
		invalidOptionalOperatorUUID(filter.ActorUserAccountID) ||
		(filter.SubjectID != nil && filter.SubjectType == "") ||
		(filter.SubjectType != "" && !operatorAuditSubjectTypePattern.MatchString(filter.SubjectType)) ||
		(filter.From != nil && filter.From.IsZero()) ||
		(filter.To != nil && filter.To.IsZero()) ||
		(filter.From != nil && filter.To != nil && !filter.From.Before(*filter.To)) {
		return false
	}
	return len(filter.Action) <= 128 && len(filter.Search) <= 256
}
