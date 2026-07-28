package operatorqueries

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
)

const maximumReleaseSearchBytes = 256

type ReleaseQuery struct {
	ApplicationID *uuid.UUID
	Kind          string
	Status        string
	SearchPattern string
	Limit         int
	Cursor        *CursorTuple
	Scopes        AuditViewScopes
	CursorScope   CursorScope
}

func ListOperatorReleases(
	ctx context.Context,
	filter types.ReleaseFilter,
	pageRequest types.PageRequest,
) (types.OperatorPage[types.OperatorReleaseRow], error) {
	page := types.OperatorPage[types.OperatorReleaseRow]{Items: []types.OperatorReleaseRow{}}
	query, err := NormalizeReleaseQuery(filter, pageRequest)
	if err != nil {
		return page, err
	}
	dbQuery := db.OperatorReleaseQuery{
		OrganizationID: query.Scopes.OrganizationID, DecisionAt: query.Scopes.DecisionAt,
		OrganizationWide:       query.Scopes.OrganizationWide,
		CustomerScopeIDs:       query.Scopes.CustomerIDs,
		EnvironmentScopeIDs:    query.Scopes.EnvironmentIDs,
		DeploymentUnitScopeIDs: query.Scopes.DeploymentUnitIDs,
		ComponentScopeIDs:      query.Scopes.ComponentIDs,
		CampaignScopeIDs:       query.Scopes.CampaignIDs,
		ApplicationID:          query.ApplicationID, Kind: query.Kind, Status: query.Status,
		SearchPattern: query.SearchPattern, Limit: query.Limit + 1,
	}
	if query.Cursor != nil {
		dbQuery.Cursor = &db.OperatorReleaseCursor{
			CreatedAt: query.Cursor.CreatedAt,
			ID:        query.Cursor.ID,
		}
	}
	result, err := db.ListOperatorReleaseRows(ctx, dbQuery)
	if err != nil {
		return page, err
	}
	total := result.Total
	return CompletePage(
		result.Items,
		query.Limit,
		query.CursorScope,
		&total,
		func(row types.OperatorReleaseRow) CursorTuple {
			return CursorTuple{CreatedAt: row.CreatedAt, ID: row.ID}
		},
	)
}

func GetOperatorRelease(
	ctx context.Context,
	scopeFilter types.OperatorScopeFilter,
	releaseID uuid.UUID,
) (*types.OperatorReleaseDetail, error) {
	scopes, err := AuditViewScopesFromOperatorScopeFilter(scopeFilter)
	if err != nil {
		return nil, err
	}
	if scopes.Empty() {
		return nil, apierrors.ErrForbidden
	}
	return db.GetOperatorReleaseDetail(ctx, scopes.ToOperatorScopeFilter(), releaseID)
}

func CompareOperatorReleases(
	ctx context.Context,
	scopeFilter types.OperatorScopeFilter,
	leftID uuid.UUID,
	rightID uuid.UUID,
) (*types.OperatorReleaseCompare, error) {
	left, err := GetOperatorRelease(ctx, scopeFilter, leftID)
	if err != nil {
		return nil, err
	}
	right, err := GetOperatorRelease(ctx, scopeFilter, rightID)
	if err != nil {
		return nil, err
	}
	comparison := compareOperatorReleaseDetails(*left, *right)
	return &comparison, nil
}

func NormalizeReleaseQuery(
	filter types.ReleaseFilter,
	page types.PageRequest,
) (ReleaseQuery, error) {
	query := ReleaseQuery{}
	scopes, err := AuditViewScopesFromOperatorScopeFilter(filter.OperatorScopeFilter)
	if err != nil {
		return query, err
	}
	if scopes.Empty() {
		return query, apierrors.ErrForbidden
	}
	if filter.ApplicationID != nil && *filter.ApplicationID == uuid.Nil {
		return query, apierrors.NewBadRequest("applicationId is invalid")
	}
	if filter.Kind != "" && !types.ReleaseBundleKind(filter.Kind).IsValid() {
		return query, apierrors.NewBadRequest("kind is invalid")
	}
	if filter.Status != "" && !validReleaseStatus(filter.Status) {
		return query, apierrors.NewBadRequest("status is invalid")
	}
	search := strings.TrimSpace(filter.Search)
	if len(search) > maximumReleaseSearchBytes {
		return query, apierrors.NewBadRequest("search is too large")
	}
	limit, err := NormalizePageRequest(page)
	if err != nil {
		return query, err
	}
	filterChecksum, err := CanonicalFilterChecksum(struct {
		ApplicationID *uuid.UUID `json:"applicationId,omitempty"`
		Kind          string     `json:"kind,omitempty"`
		Status        string     `json:"status,omitempty"`
		Search        string     `json:"search,omitempty"`
	}{filter.ApplicationID, filter.Kind, filter.Status, search})
	if err != nil {
		return query, fmt.Errorf("could not checksum release filters: %w", err)
	}
	cursorScope := CursorScope{
		OrganizationID: filter.OrganizationID,
		Collection:     types.OperatorCollectionReleases,
		DecisionAt:     filter.DecisionAt,
		ScopeChecksum:  scopes.Checksum(),
		FilterChecksum: filterChecksum,
	}
	cursor, err := DecodeCursor(page.Cursor, cursorScope)
	if err != nil {
		return query, err
	}
	query = ReleaseQuery{
		ApplicationID: filter.ApplicationID,
		Kind:          filter.Kind,
		Status:        filter.Status,
		SearchPattern: releaseSearchPattern(search),
		Limit:         limit,
		Cursor:        cursor,
		Scopes:        scopes,
		CursorScope:   cursorScope,
	}
	return query, nil
}

func validReleaseStatus(value string) bool {
	switch types.ReleaseBundleStatus(value) {
	case types.ReleaseBundleStatusDraft,
		types.ReleaseBundleStatusValidating,
		types.ReleaseBundleStatusPublished,
		types.ReleaseBundleStatusBlocked,
		types.ReleaseBundleStatusArchived:
		return true
	default:
		return false
	}
}

func releaseSearchPattern(value string) string {
	if value == "" {
		return ""
	}
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + replacer.Replace(value) + "%"
}

type releaseCompareValue struct {
	checksum string
	digest   string
}

func compareOperatorReleaseDetails(
	left types.OperatorReleaseDetail,
	right types.OperatorReleaseDetail,
) types.OperatorReleaseCompare {
	leftFacts := releaseCompareValues(left)
	rightFacts := releaseCompareValues(right)
	keys := make(map[string]struct{}, len(leftFacts)+len(rightFacts))
	for key := range leftFacts {
		keys[key] = struct{}{}
	}
	for key := range rightFacts {
		keys[key] = struct{}{}
	}
	ordered := slices.Sorted(maps.Keys(keys))
	changes := make([]types.OperatorReleaseCompareFact, 0, len(ordered))
	for _, key := range ordered {
		leftValue, leftOK := leftFacts[key]
		rightValue, rightOK := rightFacts[key]
		if leftOK && rightOK && leftValue == rightValue {
			continue
		}
		change := "modified"
		if !leftOK {
			change = "added"
		} else if !rightOK {
			change = "removed"
		}
		changes = append(changes, types.OperatorReleaseCompareFact{
			Component: key, Change: change,
			LeftChecksum: leftValue.checksum, RightChecksum: rightValue.checksum,
			LeftDigest: leftValue.digest, RightDigest: rightValue.digest,
		})
	}
	return types.OperatorReleaseCompare{Left: left.Release, Right: right.Release, Changes: changes}
}

func releaseCompareValues(detail types.OperatorReleaseDetail) map[string]releaseCompareValue {
	values := make(map[string]releaseCompareValue)
	if len(detail.ComponentPins) > 0 {
		for _, pin := range detail.ComponentPins {
			values[pin.Component] = releaseCompareValue{checksum: pin.Checksum, digest: pin.Digest}
		}
		return values
	}
	for _, artifact := range detail.Artifacts {
		values[artifact.Name] = releaseCompareValue{digest: artifact.ManifestDigest}
	}
	return values
}
