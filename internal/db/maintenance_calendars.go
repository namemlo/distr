package db

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/scheduling"
	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/zonerules"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	calendarDefaultPageLimit  = 50
	calendarMaximumPageLimit  = 100
	calendarMaximumCursorSize = 2048
	calendarCursorVersion     = 1
)

const maintenanceCalendarOutputExpr = `
	c.id,
	c.created_at,
	c.updated_at,
	c.organization_id,
	c.name,
	c.description,
	c.draft_iana_zone,
	c.draft_rule_version,
	c.draft_rules,
	c.draft_revision,
	c.last_published_version_id,
	c.created_by_useraccount_id,
	c.updated_by_useraccount_id
`

const maintenanceCalendarVersionOutputExpr = `
	v.id,
	v.maintenance_calendar_id,
	v.organization_id,
	v.version_number,
	v.source_draft_revision,
	v.name,
	v.description,
	v.iana_zone,
	v.rule_version,
	v.canonical_payload,
	v.checksum,
	v.published_at,
	v.published_by_useraccount_id
`

const maintenanceWindowRuleOutputExpr = `
	r.logical_rule_id AS id,
	r.id AS version_rule_id,
	r.organization_id,
	r.calendar_version_id,
	r.name,
	r.weekdays,
	r.start_minute,
	r.end_minute,
	r.sort_order
`

const deploymentFreezeOutputExpr = `
	f.id,
	f.created_at,
	f.updated_at,
	f.organization_id,
	f.name,
	f.draft_start_at,
	f.draft_end_at,
	f.draft_iana_zone,
	f.draft_rule_version,
	f.draft_scope_kind,
	f.draft_scope_id,
	f.draft_priority,
	f.draft_reason,
	f.draft_revision,
	f.last_published_revision_id,
	f.created_by_useraccount_id,
	f.updated_by_useraccount_id
`

const deploymentFreezeRevisionOutputExpr = `
	r.id,
	r.deployment_freeze_id,
	r.organization_id,
	r.version_number,
	r.source_draft_revision,
	r.name,
	r.start_at,
	r.end_at,
	r.iana_zone,
	r.rule_version,
	r.scope_kind,
	r.scope_id,
	r.priority,
	r.reason,
	r.canonical_payload,
	r.checksum,
	r.published_at,
	r.published_by_useraccount_id
`

type calendarCursor struct {
	Version   int       `json:"v"`
	CreatedAt time.Time `json:"createdAt"`
	ID        uuid.UUID `json:"id"`
}

func CreateMaintenanceCalendar(
	ctx context.Context,
	calendar *types.MaintenanceCalendar,
) error {
	if calendar == nil {
		return apierrors.NewBadRequest("maintenance calendar is required")
	}
	if err := validateMaintenanceCalendarForWrite(*calendar); err != nil {
		return err
	}
	rulesJSON, err := marshalMaintenanceWindowRules(calendar.DraftRules)
	if err != nil {
		return err
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		INSERT INTO MaintenanceCalendar AS c (
			organization_id,
			name,
			description,
			draft_iana_zone,
			draft_rule_version,
			draft_rules,
			created_by_useraccount_id,
			updated_by_useraccount_id
		) VALUES (
			@organizationID,
			@name,
			@description,
			@ianaZone,
			@ruleVersion,
			@rules,
			@createdBy,
			@updatedBy
		)
		RETURNING `+maintenanceCalendarOutputExpr,
		pgx.NamedArgs{
			"organizationID": calendar.OrganizationID,
			"name":           strings.TrimSpace(calendar.Name),
			"description":    calendar.Description,
			"ianaZone":       strings.TrimSpace(calendar.DraftIANAZone),
			"ruleVersion":    strings.TrimSpace(calendar.DraftRuleVersion),
			"rules":          rulesJSON,
			"createdBy":      calendar.CreatedBy,
			"updatedBy":      calendar.UpdatedBy,
		},
	)
	if err != nil {
		return mapCalendarWriteError("create maintenance calendar", err)
	}
	created, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.MaintenanceCalendar],
	)
	if err != nil {
		return mapCalendarWriteError("read created maintenance calendar", err)
	}
	if err := decodeMaintenanceCalendarRules(&created); err != nil {
		return err
	}
	*calendar = created
	return nil
}

func GetMaintenanceCalendar(
	ctx context.Context,
	organizationID, calendarID uuid.UUID,
) (*types.MaintenanceCalendar, error) {
	if organizationID == uuid.Nil || calendarID == uuid.Nil {
		return nil, apierrors.ErrNotFound
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+maintenanceCalendarOutputExpr+`
		FROM MaintenanceCalendar c
		WHERE c.organization_id = @organizationID
		  AND c.id = @calendarID`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"calendarID":     calendarID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not get maintenance calendar: %w", err)
	}
	calendar, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.MaintenanceCalendar],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not collect maintenance calendar: %w", err)
	}
	if err := decodeMaintenanceCalendarRules(&calendar); err != nil {
		return nil, err
	}
	return &calendar, nil
}

func ListMaintenanceCalendars(
	ctx context.Context,
	filter types.CalendarListFilter,
) (types.Page[types.MaintenanceCalendar], error) {
	limit, cursor, err := normalizeCalendarListFilter(filter)
	if err != nil {
		return types.Page[types.MaintenanceCalendar]{}, err
	}
	rows, err := queryCalendarPage(
		ctx,
		`SELECT `+maintenanceCalendarOutputExpr+`
		 FROM MaintenanceCalendar c
		 WHERE c.organization_id = @organizationID`,
		"c",
		filter.OrganizationID,
		nil,
		cursor,
		limit,
	)
	if err != nil {
		return types.Page[types.MaintenanceCalendar]{}, fmt.Errorf(
			"could not list maintenance calendars: %w",
			err,
		)
	}
	items, err := pgx.CollectRows(
		rows,
		pgx.RowToStructByName[types.MaintenanceCalendar],
	)
	if err != nil {
		return types.Page[types.MaintenanceCalendar]{}, fmt.Errorf(
			"could not collect maintenance calendars: %w",
			err,
		)
	}
	for index := range items {
		if err := decodeMaintenanceCalendarRules(&items[index]); err != nil {
			return types.Page[types.MaintenanceCalendar]{}, err
		}
	}
	return buildCalendarPage(
		items,
		limit,
		func(value types.MaintenanceCalendar) calendarCursor {
			return calendarCursor{
				Version: calendarCursorVersion, CreatedAt: value.CreatedAt, ID: value.ID,
			}
		},
	)
}

func UpdateMaintenanceCalendar(
	ctx context.Context,
	calendar *types.MaintenanceCalendar,
) error {
	if calendar == nil || calendar.ID == uuid.Nil || calendar.DraftRevision < 1 {
		return apierrors.NewBadRequest(
			"maintenance calendar ID and expected draft revision are required",
		)
	}
	if err := validateMaintenanceCalendarForWrite(*calendar); err != nil {
		return err
	}
	rulesJSON, err := marshalMaintenanceWindowRules(calendar.DraftRules)
	if err != nil {
		return err
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		UPDATE MaintenanceCalendar AS c
		SET
			name = @name,
			description = @description,
			draft_iana_zone = @ianaZone,
			draft_rule_version = @ruleVersion,
			draft_rules = @rules,
			draft_revision = c.draft_revision + 1,
			updated_by_useraccount_id = @updatedBy,
			updated_at = now()
		WHERE c.organization_id = @organizationID
		  AND c.id = @calendarID
		  AND c.draft_revision = @expectedDraftRevision
		RETURNING `+maintenanceCalendarOutputExpr,
		pgx.NamedArgs{
			"organizationID":        calendar.OrganizationID,
			"calendarID":            calendar.ID,
			"expectedDraftRevision": calendar.DraftRevision,
			"name":                  strings.TrimSpace(calendar.Name),
			"description":           calendar.Description,
			"ianaZone":              strings.TrimSpace(calendar.DraftIANAZone),
			"ruleVersion":           strings.TrimSpace(calendar.DraftRuleVersion),
			"rules":                 rulesJSON,
			"updatedBy":             calendar.UpdatedBy,
		},
	)
	if err != nil {
		return mapCalendarWriteError("update maintenance calendar", err)
	}
	updated, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.MaintenanceCalendar],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		exists, existsErr := maintenanceCalendarExists(ctx, calendar.OrganizationID, calendar.ID)
		if existsErr != nil {
			return existsErr
		}
		if !exists {
			return apierrors.ErrNotFound
		}
		return apierrors.NewConflict("maintenance calendar draft revision changed")
	}
	if err != nil {
		return mapCalendarWriteError("read updated maintenance calendar", err)
	}
	if err := decodeMaintenanceCalendarRules(&updated); err != nil {
		return err
	}
	*calendar = updated
	return nil
}

func PublishMaintenanceCalendar(
	ctx context.Context,
	organizationID, calendarID uuid.UUID,
	expectedDraftRevision int64,
	actorID uuid.UUID,
) (*types.MaintenanceCalendarVersion, error) {
	if organizationID == uuid.Nil || calendarID == uuid.Nil ||
		expectedDraftRevision < 1 || actorID == uuid.Nil {
		return nil, apierrors.NewBadRequest(
			"organization, calendar, expected draft revision, and actor are required",
		)
	}
	var published *types.MaintenanceCalendarVersion
	err := RunTx(ctx, func(txCtx context.Context) error {
		calendar, err := lockMaintenanceCalendar(txCtx, organizationID, calendarID)
		if err != nil {
			return err
		}
		existing, err := getMaintenanceCalendarVersionByDraft(
			txCtx,
			organizationID,
			calendarID,
			expectedDraftRevision,
		)
		replay, isReplay, err := resolvePublicationReplay(
			existing,
			err,
			calendar.DraftRevision,
			expectedDraftRevision,
			"maintenance calendar",
		)
		if err != nil {
			return err
		}
		if isReplay {
			published = replay
			return nil
		}

		versionNumber, err := nextMaintenanceCalendarVersionNumber(txCtx, calendar.ID)
		if err != nil {
			return err
		}
		version := types.MaintenanceCalendarVersion{
			CalendarID:          calendar.ID,
			OrganizationID:      calendar.OrganizationID,
			VersionNumber:       versionNumber,
			SourceDraftRevision: calendar.DraftRevision,
			Name:                calendar.Name,
			Description:         calendar.Description,
			IANAZone:            calendar.DraftIANAZone,
			RuleVersion:         calendar.DraftRuleVersion,
			PublishedBy:         actorID,
			WindowRules:         append([]types.MaintenanceWindowRule(nil), calendar.DraftRules...),
		}
		version.CanonicalPayload, version.Checksum, err = scheduling.CanonicalizeCalendarVersion(version)
		if err != nil {
			return apierrors.NewBadRequest(err.Error())
		}
		sortWindowRules(version.WindowRules)
		if err := insertMaintenanceCalendarVersion(txCtx, &version); err != nil {
			return err
		}
		assignVersionScopedWindowRuleIDs(&version)
		if err := insertMaintenanceWindowRules(txCtx, version); err != nil {
			return err
		}
		if err := setMaintenanceCalendarPublishedVersion(
			txCtx,
			organizationID,
			calendarID,
			expectedDraftRevision,
			version.ID,
			actorID,
		); err != nil {
			return err
		}
		published = &version
		return recordGovernanceAuditMutation(
			txCtx,
			maintenanceCalendarPublishedAuditEvent(version),
		)
	})
	if err != nil {
		return nil, err
	}
	return published, nil
}

func GetMaintenanceCalendarVersion(
	ctx context.Context,
	organizationID, calendarID, versionID uuid.UUID,
) (*types.MaintenanceCalendarVersion, error) {
	if organizationID == uuid.Nil || calendarID == uuid.Nil || versionID == uuid.Nil {
		return nil, apierrors.ErrNotFound
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+maintenanceCalendarVersionOutputExpr+`
		FROM MaintenanceCalendarVersion v
		WHERE v.organization_id = @organizationID
		  AND v.maintenance_calendar_id = @calendarID
		  AND v.id = @versionID`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"calendarID":     calendarID,
			"versionID":      versionID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not get maintenance calendar version: %w", err)
	}
	version, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.MaintenanceCalendarVersion],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not collect maintenance calendar version: %w", err)
	}
	version.WindowRules, err = listMaintenanceWindowRules(ctx, organizationID, version.ID)
	if err != nil {
		return nil, err
	}
	return &version, nil
}

func ListMaintenanceCalendarVersions(
	ctx context.Context,
	organizationID, calendarID uuid.UUID,
	filter types.CalendarListFilter,
) (types.Page[types.MaintenanceCalendarVersion], error) {
	if filter.OrganizationID != uuid.Nil && filter.OrganizationID != organizationID {
		return types.Page[types.MaintenanceCalendarVersion]{}, apierrors.ErrForbidden
	}
	filter.OrganizationID = organizationID
	limit, cursor, err := normalizeCalendarListFilter(filter)
	if err != nil {
		return types.Page[types.MaintenanceCalendarVersion]{}, err
	}
	if calendarID == uuid.Nil {
		return types.Page[types.MaintenanceCalendarVersion]{}, apierrors.ErrNotFound
	}
	exists, err := maintenanceCalendarExists(ctx, organizationID, calendarID)
	if err != nil {
		return types.Page[types.MaintenanceCalendarVersion]{}, fmt.Errorf(
			"could not validate maintenance calendar: %w",
			err,
		)
	}
	if !exists {
		return types.Page[types.MaintenanceCalendarVersion]{}, apierrors.ErrNotFound
	}
	rows, err := queryCalendarPage(
		ctx,
		`SELECT `+maintenanceCalendarVersionOutputExpr+`
		 FROM MaintenanceCalendarVersion v
		 WHERE v.organization_id = @organizationID
		   AND v.maintenance_calendar_id = @parentID`,
		"v",
		organizationID,
		&calendarID,
		cursor,
		limit,
	)
	if err != nil {
		return types.Page[types.MaintenanceCalendarVersion]{}, fmt.Errorf(
			"could not list maintenance calendar versions: %w",
			err,
		)
	}
	items, err := pgx.CollectRows(
		rows,
		pgx.RowToStructByName[types.MaintenanceCalendarVersion],
	)
	if err != nil {
		return types.Page[types.MaintenanceCalendarVersion]{}, fmt.Errorf(
			"could not collect maintenance calendar versions: %w",
			err,
		)
	}
	versionIDs := make([]uuid.UUID, len(items))
	for index := range items {
		versionIDs[index] = items[index].ID
	}
	rulesByVersion, err := listMaintenanceWindowRulesForVersions(
		ctx,
		organizationID,
		versionIDs,
	)
	if err != nil {
		return types.Page[types.MaintenanceCalendarVersion]{}, err
	}
	for index := range items {
		items[index].WindowRules = rulesByVersion[items[index].ID]
	}
	return buildCalendarPage(
		items,
		limit,
		func(value types.MaintenanceCalendarVersion) calendarCursor {
			return calendarCursor{
				Version: calendarCursorVersion, CreatedAt: value.PublishedAt, ID: value.ID,
			}
		},
	)
}

func CreateDeploymentFreeze(
	ctx context.Context,
	freeze *types.DeploymentFreeze,
) error {
	if freeze == nil {
		return apierrors.NewBadRequest("deployment freeze is required")
	}
	if err := validateDeploymentFreezeForWrite(*freeze); err != nil {
		return err
	}
	if err := ensureCalendarScopeBelongsToOrganization(
		ctx,
		freeze.OrganizationID,
		freeze.DraftScopeKind,
		freeze.DraftScopeID,
	); err != nil {
		return err
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		INSERT INTO DeploymentFreeze AS f (
			organization_id,
			name,
			draft_start_at,
			draft_end_at,
			draft_iana_zone,
			draft_rule_version,
			draft_scope_kind,
			draft_scope_id,
			draft_priority,
			draft_reason,
			created_by_useraccount_id,
			updated_by_useraccount_id
		) VALUES (
			@organizationID,
			@name,
			@startAt,
			@endAt,
			@ianaZone,
			@ruleVersion,
			@scopeKind,
			@scopeID,
			@priority,
			@reason,
			@createdBy,
			@updatedBy
		)
		RETURNING `+deploymentFreezeOutputExpr,
		pgx.NamedArgs{
			"organizationID": freeze.OrganizationID,
			"name":           strings.TrimSpace(freeze.Name),
			"startAt":        freeze.DraftStartAt.UTC(),
			"endAt":          freeze.DraftEndAt.UTC(),
			"ianaZone":       strings.TrimSpace(freeze.DraftIANAZone),
			"ruleVersion":    strings.TrimSpace(freeze.DraftRuleVersion),
			"scopeKind":      freeze.DraftScopeKind,
			"scopeID":        freeze.DraftScopeID,
			"priority":       freeze.DraftPriority,
			"reason":         strings.TrimSpace(freeze.DraftReason),
			"createdBy":      freeze.CreatedBy,
			"updatedBy":      freeze.UpdatedBy,
		},
	)
	if err != nil {
		return mapCalendarWriteError("create deployment freeze", err)
	}
	created, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.DeploymentFreeze],
	)
	if err != nil {
		return mapCalendarWriteError("read created deployment freeze", err)
	}
	*freeze = created
	return nil
}

func GetDeploymentFreeze(
	ctx context.Context,
	organizationID, freezeID uuid.UUID,
) (*types.DeploymentFreeze, error) {
	if organizationID == uuid.Nil || freezeID == uuid.Nil {
		return nil, apierrors.ErrNotFound
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+deploymentFreezeOutputExpr+`
		FROM DeploymentFreeze f
		WHERE f.organization_id = @organizationID
		  AND f.id = @freezeID`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"freezeID":       freezeID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not get deployment freeze: %w", err)
	}
	freeze, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.DeploymentFreeze],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not collect deployment freeze: %w", err)
	}
	return &freeze, nil
}

func ListDeploymentFreezes(
	ctx context.Context,
	filter types.CalendarListFilter,
) (types.Page[types.DeploymentFreeze], error) {
	limit, cursor, err := normalizeCalendarListFilter(filter)
	if err != nil {
		return types.Page[types.DeploymentFreeze]{}, err
	}
	rows, err := queryCalendarPage(
		ctx,
		`SELECT `+deploymentFreezeOutputExpr+`
		 FROM DeploymentFreeze f
		 WHERE f.organization_id = @organizationID`,
		"f",
		filter.OrganizationID,
		nil,
		cursor,
		limit,
	)
	if err != nil {
		return types.Page[types.DeploymentFreeze]{}, fmt.Errorf(
			"could not list deployment freezes: %w",
			err,
		)
	}
	items, err := pgx.CollectRows(
		rows,
		pgx.RowToStructByName[types.DeploymentFreeze],
	)
	if err != nil {
		return types.Page[types.DeploymentFreeze]{}, fmt.Errorf(
			"could not collect deployment freezes: %w",
			err,
		)
	}
	return buildCalendarPage(
		items,
		limit,
		func(value types.DeploymentFreeze) calendarCursor {
			return calendarCursor{
				Version: calendarCursorVersion, CreatedAt: value.CreatedAt, ID: value.ID,
			}
		},
	)
}

func UpdateDeploymentFreeze(
	ctx context.Context,
	freeze *types.DeploymentFreeze,
) error {
	if freeze == nil || freeze.ID == uuid.Nil || freeze.DraftRevision < 1 {
		return apierrors.NewBadRequest(
			"deployment freeze ID and expected draft revision are required",
		)
	}
	if err := validateDeploymentFreezeForWrite(*freeze); err != nil {
		return err
	}
	if err := ensureCalendarScopeBelongsToOrganization(
		ctx,
		freeze.OrganizationID,
		freeze.DraftScopeKind,
		freeze.DraftScopeID,
	); err != nil {
		return err
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		UPDATE DeploymentFreeze AS f
		SET
			name = @name,
			draft_start_at = @startAt,
			draft_end_at = @endAt,
			draft_iana_zone = @ianaZone,
			draft_rule_version = @ruleVersion,
			draft_scope_kind = @scopeKind,
			draft_scope_id = @scopeID,
			draft_priority = @priority,
			draft_reason = @reason,
			draft_revision = f.draft_revision + 1,
			updated_by_useraccount_id = @updatedBy,
			updated_at = now()
		WHERE f.organization_id = @organizationID
		  AND f.id = @freezeID
		  AND f.draft_revision = @expectedDraftRevision
		RETURNING `+deploymentFreezeOutputExpr,
		pgx.NamedArgs{
			"organizationID":        freeze.OrganizationID,
			"freezeID":              freeze.ID,
			"expectedDraftRevision": freeze.DraftRevision,
			"name":                  strings.TrimSpace(freeze.Name),
			"startAt":               freeze.DraftStartAt.UTC(),
			"endAt":                 freeze.DraftEndAt.UTC(),
			"ianaZone":              strings.TrimSpace(freeze.DraftIANAZone),
			"ruleVersion":           strings.TrimSpace(freeze.DraftRuleVersion),
			"scopeKind":             freeze.DraftScopeKind,
			"scopeID":               freeze.DraftScopeID,
			"priority":              freeze.DraftPriority,
			"reason":                strings.TrimSpace(freeze.DraftReason),
			"updatedBy":             freeze.UpdatedBy,
		},
	)
	if err != nil {
		return mapCalendarWriteError("update deployment freeze", err)
	}
	updated, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.DeploymentFreeze],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		exists, existsErr := deploymentFreezeExists(ctx, freeze.OrganizationID, freeze.ID)
		if existsErr != nil {
			return existsErr
		}
		if !exists {
			return apierrors.ErrNotFound
		}
		return apierrors.NewConflict("deployment freeze draft revision changed")
	}
	if err != nil {
		return mapCalendarWriteError("read updated deployment freeze", err)
	}
	*freeze = updated
	return nil
}

type DeploymentFreezeScopeAuthorizer func(
	context.Context,
	types.CalendarScopeRef,
) error

func PublishDeploymentFreeze(
	ctx context.Context,
	organizationID, freezeID uuid.UUID,
	expectedDraftRevision int64,
	actorID uuid.UUID,
	authorizeScope DeploymentFreezeScopeAuthorizer,
) (*types.DeploymentFreezeRevision, error) {
	if organizationID == uuid.Nil || freezeID == uuid.Nil ||
		expectedDraftRevision < 1 || actorID == uuid.Nil || authorizeScope == nil {
		return nil, apierrors.NewBadRequest(
			"organization, freeze, expected draft revision, and actor are required",
		)
	}
	var published *types.DeploymentFreezeRevision
	err := RunTx(ctx, func(txCtx context.Context) error {
		freeze, err := lockDeploymentFreeze(txCtx, organizationID, freezeID)
		if err != nil {
			return err
		}
		existing, err := getDeploymentFreezeRevisionByDraft(
			txCtx,
			organizationID,
			freezeID,
			expectedDraftRevision,
		)
		replay, isReplay, err := resolvePublicationReplay(
			existing,
			err,
			freeze.DraftRevision,
			expectedDraftRevision,
			"deployment freeze",
		)
		if err != nil {
			return err
		}
		if isReplay {
			if err := authorizeScope(txCtx, types.CalendarScopeRef{
				Kind: replay.ScopeKind,
				ID:   replay.ScopeID,
			}); err != nil {
				return err
			}
			published = replay
			return nil
		}
		if err := authorizeScope(txCtx, types.CalendarScopeRef{
			Kind: freeze.DraftScopeKind,
			ID:   freeze.DraftScopeID,
		}); err != nil {
			return err
		}
		if err := ensureCalendarScopeBelongsToOrganization(
			txCtx,
			organizationID,
			freeze.DraftScopeKind,
			freeze.DraftScopeID,
		); err != nil {
			return err
		}

		versionNumber, err := nextDeploymentFreezeRevisionNumber(txCtx, freeze.ID)
		if err != nil {
			return err
		}
		revision := types.DeploymentFreezeRevision{
			FreezeID:            freeze.ID,
			OrganizationID:      freeze.OrganizationID,
			VersionNumber:       versionNumber,
			SourceDraftRevision: freeze.DraftRevision,
			Name:                freeze.Name,
			StartAt:             freeze.DraftStartAt.UTC(),
			EndAt:               freeze.DraftEndAt.UTC(),
			IANAZone:            freeze.DraftIANAZone,
			RuleVersion:         freeze.DraftRuleVersion,
			ScopeKind:           freeze.DraftScopeKind,
			ScopeID:             freeze.DraftScopeID,
			Priority:            freeze.DraftPriority,
			Reason:              freeze.DraftReason,
			PublishedBy:         actorID,
		}
		revision.CanonicalPayload, revision.Checksum, err = scheduling.CanonicalizeFreezeRevision(revision)
		if err != nil {
			return apierrors.NewBadRequest(err.Error())
		}
		if err := insertDeploymentFreezeRevision(txCtx, &revision); err != nil {
			return err
		}
		if err := setDeploymentFreezePublishedRevision(
			txCtx,
			organizationID,
			freezeID,
			expectedDraftRevision,
			revision.ID,
			actorID,
		); err != nil {
			return err
		}
		published = &revision
		return recordGovernanceAuditMutation(
			txCtx,
			deploymentFreezePublishedAuditEvent(revision),
		)
	})
	if err != nil {
		return nil, err
	}
	return published, nil
}

func GetDeploymentFreezeRevision(
	ctx context.Context,
	organizationID, freezeID, revisionID uuid.UUID,
) (*types.DeploymentFreezeRevision, error) {
	if organizationID == uuid.Nil || freezeID == uuid.Nil || revisionID == uuid.Nil {
		return nil, apierrors.ErrNotFound
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+deploymentFreezeRevisionOutputExpr+`
		FROM DeploymentFreezeRevision r
		WHERE r.organization_id = @organizationID
		  AND r.deployment_freeze_id = @freezeID
		  AND r.id = @revisionID`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"freezeID":       freezeID,
			"revisionID":     revisionID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not get deployment freeze revision: %w", err)
	}
	revision, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.DeploymentFreezeRevision],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not collect deployment freeze revision: %w", err)
	}
	return &revision, nil
}

func ListDeploymentFreezeRevisions(
	ctx context.Context,
	organizationID, freezeID uuid.UUID,
	filter types.CalendarListFilter,
) (types.Page[types.DeploymentFreezeRevision], error) {
	if filter.OrganizationID != uuid.Nil && filter.OrganizationID != organizationID {
		return types.Page[types.DeploymentFreezeRevision]{}, apierrors.ErrForbidden
	}
	filter.OrganizationID = organizationID
	limit, cursor, err := normalizeCalendarListFilter(filter)
	if err != nil {
		return types.Page[types.DeploymentFreezeRevision]{}, err
	}
	if freezeID == uuid.Nil {
		return types.Page[types.DeploymentFreezeRevision]{}, apierrors.ErrNotFound
	}
	exists, err := deploymentFreezeExists(ctx, organizationID, freezeID)
	if err != nil {
		return types.Page[types.DeploymentFreezeRevision]{}, fmt.Errorf(
			"could not validate deployment freeze: %w",
			err,
		)
	}
	if !exists {
		return types.Page[types.DeploymentFreezeRevision]{}, apierrors.ErrNotFound
	}
	rows, err := queryCalendarPage(
		ctx,
		`SELECT `+deploymentFreezeRevisionOutputExpr+`
		 FROM DeploymentFreezeRevision r
		 WHERE r.organization_id = @organizationID
		   AND r.deployment_freeze_id = @parentID`,
		"r",
		organizationID,
		&freezeID,
		cursor,
		limit,
	)
	if err != nil {
		return types.Page[types.DeploymentFreezeRevision]{}, fmt.Errorf(
			"could not list deployment freeze revisions: %w",
			err,
		)
	}
	items, err := pgx.CollectRows(
		rows,
		pgx.RowToStructByName[types.DeploymentFreezeRevision],
	)
	if err != nil {
		return types.Page[types.DeploymentFreezeRevision]{}, fmt.Errorf(
			"could not collect deployment freeze revisions: %w",
			err,
		)
	}
	return buildCalendarPage(
		items,
		limit,
		func(value types.DeploymentFreezeRevision) calendarCursor {
			return calendarCursor{
				Version: calendarCursorVersion, CreatedAt: value.PublishedAt, ID: value.ID,
			}
		},
	)
}

func ListActiveDeploymentFreezeRevisions(
	ctx context.Context,
	organizationID uuid.UUID,
	scope types.CalendarScopeRef,
	instant time.Time,
) ([]types.DeploymentFreezeRevision, error) {
	if organizationID == uuid.Nil || !scope.Kind.IsValid() ||
		scope.ID == uuid.Nil || instant.IsZero() {
		return nil, apierrors.NewBadRequest(
			"organization, scope, and evaluation instant are required",
		)
	}
	if err := ensureCalendarScopeBelongsToOrganization(
		ctx,
		organizationID,
		scope.Kind,
		scope.ID,
	); err != nil {
		return nil, err
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+deploymentFreezeRevisionOutputExpr+`
		FROM DeploymentFreezeRevision r
		JOIN DeploymentFreeze f
		  ON f.id = r.deployment_freeze_id
		 AND f.organization_id = r.organization_id
		 AND f.last_published_revision_id = r.id
		WHERE r.organization_id = @organizationID
		  AND r.scope_kind = @scopeKind
		  AND r.scope_id = @scopeID
		  AND r.start_at <= @instant
		  AND r.end_at > @instant
		ORDER BY r.priority DESC, r.id`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"scopeKind":      scope.Kind,
			"scopeID":        scope.ID,
			"instant":        instant.UTC(),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not list active deployment freezes: %w", err)
	}
	revisions, err := pgx.CollectRows(
		rows,
		pgx.RowToStructByName[types.DeploymentFreezeRevision],
	)
	if err != nil {
		return nil, fmt.Errorf("could not collect active deployment freezes: %w", err)
	}
	return revisions, nil
}

func validateMaintenanceCalendarForWrite(calendar types.MaintenanceCalendar) error {
	if calendar.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	if strings.TrimSpace(calendar.Name) == "" {
		return apierrors.NewBadRequest("name is required")
	}
	if len(calendar.Name) > 200 || len(calendar.Description) > 4000 {
		return apierrors.NewBadRequest("calendar name or description is too large")
	}
	if calendar.UpdatedBy == uuid.Nil {
		return apierrors.NewBadRequest("updatedBy is required")
	}
	if strings.TrimSpace(calendar.DraftRuleVersion) == "" {
		return apierrors.NewBadRequest("rule version is required")
	}
	if _, err := zonerules.ValidateBinding(
		zonerules.Production(),
		strings.TrimSpace(calendar.DraftIANAZone),
		strings.TrimSpace(calendar.DraftRuleVersion),
	); err != nil {
		return apierrors.NewBadRequest("IANA zone is invalid")
	}
	if len(calendar.DraftRules) == 0 {
		return nil
	}
	seen := make(map[uuid.UUID]struct{}, len(calendar.DraftRules))
	for _, rule := range calendar.DraftRules {
		if rule.ID == uuid.Nil {
			return apierrors.NewBadRequest("window rule ID is required")
		}
		if _, exists := seen[rule.ID]; exists {
			return apierrors.NewBadRequest("window rule IDs must be unique")
		}
		seen[rule.ID] = struct{}{}
	}
	_, _, err := scheduling.CanonicalizeCalendarVersion(
		types.MaintenanceCalendarVersion{
			CalendarID:  uuid.New(),
			Name:        calendar.Name,
			Description: calendar.Description,
			IANAZone:    calendar.DraftIANAZone,
			RuleVersion: calendar.DraftRuleVersion,
			WindowRules: calendar.DraftRules,
		},
	)
	if err != nil {
		return apierrors.NewBadRequest(err.Error())
	}
	return nil
}

func validateDeploymentFreezeForWrite(freeze types.DeploymentFreeze) error {
	if freeze.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	if strings.TrimSpace(freeze.Name) == "" {
		return apierrors.NewBadRequest("name is required")
	}
	if freeze.UpdatedBy == uuid.Nil {
		return apierrors.NewBadRequest("updatedBy is required")
	}
	if freeze.DraftScopeKind == types.CalendarScopeOrganization &&
		freeze.DraftScopeID != freeze.OrganizationID {
		return apierrors.NewBadRequest(
			"organization scope ID must match organizationId",
		)
	}
	_, _, err := scheduling.CanonicalizeFreezeRevision(
		types.DeploymentFreezeRevision{
			FreezeID:    uuid.New(),
			Name:        freeze.Name,
			StartAt:     freeze.DraftStartAt,
			EndAt:       freeze.DraftEndAt,
			IANAZone:    freeze.DraftIANAZone,
			RuleVersion: freeze.DraftRuleVersion,
			ScopeKind:   freeze.DraftScopeKind,
			ScopeID:     freeze.DraftScopeID,
			Priority:    freeze.DraftPriority,
			Reason:      freeze.DraftReason,
		},
	)
	if err != nil {
		return apierrors.NewBadRequest(err.Error())
	}
	return nil
}

func normalizeCalendarListFilter(
	filter types.CalendarListFilter,
) (int, *calendarCursor, error) {
	if filter.OrganizationID == uuid.Nil {
		return 0, nil, apierrors.ErrBadRequest
	}
	if filter.Limit < 0 || filter.Limit > calendarMaximumPageLimit {
		return 0, nil, apierrors.ErrBadRequest
	}
	if len(filter.Cursor) > calendarMaximumCursorSize {
		return 0, nil, apierrors.ErrBadRequest
	}
	limit := filter.Limit
	if limit == 0 {
		limit = calendarDefaultPageLimit
	}
	cursor, err := decodeCalendarCursor(filter.Cursor)
	if err != nil {
		return 0, nil, err
	}
	return limit, cursor, nil
}

func encodeCalendarCursor(cursor calendarCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("could not encode calendar cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeCalendarCursor(value string) (*calendarCursor, error) {
	if value == "" {
		return nil, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, apierrors.NewBadRequest("cursor is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var cursor calendarCursor
	if err := decoder.Decode(&cursor); err != nil {
		return nil, apierrors.NewBadRequest("cursor is invalid")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, apierrors.NewBadRequest("cursor is invalid")
	}
	if cursor.Version != calendarCursorVersion ||
		cursor.CreatedAt.IsZero() ||
		cursor.ID == uuid.Nil {
		return nil, apierrors.NewBadRequest("cursor is invalid")
	}
	return &cursor, nil
}

func queryCalendarPage(
	ctx context.Context,
	baseSQL, alias string,
	organizationID uuid.UUID,
	parentID *uuid.UUID,
	cursor *calendarCursor,
	limit int,
) (pgx.Rows, error) {
	var cursorCreatedAt any
	var cursorID any
	if cursor != nil {
		cursorCreatedAt = cursor.CreatedAt
		cursorID = cursor.ID
	}
	var parent any
	if parentID != nil {
		parent = *parentID
	}
	query := baseSQL + `
		   AND (
		     @cursorCreatedAt::timestamptz IS NULL
		     OR (` + alias + `.created_at, ` + alias + `.id) <
		        (@cursorCreatedAt::timestamptz, @cursorID::uuid)
		   )
		 ORDER BY ` + alias + `.created_at DESC, ` + alias + `.id DESC
		 LIMIT @limit`
	if alias == "v" || alias == "r" {
		query = strings.ReplaceAll(query, alias+".created_at", alias+".published_at")
	}
	return internalctx.GetDb(ctx).Query(ctx, query, pgx.NamedArgs{
		"organizationID":  organizationID,
		"parentID":        parent,
		"cursorCreatedAt": cursorCreatedAt,
		"cursorID":        cursorID,
		"limit":           limit + 1,
	})
}

func buildCalendarPage[T any](
	items []T,
	limit int,
	toCursor func(T) calendarCursor,
) (types.Page[T], error) {
	page := types.Page[T]{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	nextCursor, err := encodeCalendarCursor(toCursor(page.Items[len(page.Items)-1]))
	if err != nil {
		return types.Page[T]{}, err
	}
	page.NextCursor = nextCursor
	return page, nil
}

func marshalMaintenanceWindowRules(
	rules []types.MaintenanceWindowRule,
) ([]byte, error) {
	if rules == nil {
		rules = []types.MaintenanceWindowRule{}
	}
	payload, err := json.Marshal(rules)
	if err != nil {
		return nil, apierrors.NewBadRequest("window rules are invalid")
	}
	if len(payload) > 1<<20 {
		return nil, apierrors.NewBadRequest("window rules are too large")
	}
	return payload, nil
}

func decodeMaintenanceCalendarRules(calendar *types.MaintenanceCalendar) error {
	if calendar == nil {
		return errors.New("maintenance calendar is required")
	}
	if len(calendar.DraftRulesJSON) == 0 {
		calendar.DraftRules = []types.MaintenanceWindowRule{}
		return nil
	}
	if err := json.Unmarshal(calendar.DraftRulesJSON, &calendar.DraftRules); err != nil {
		return fmt.Errorf("could not decode maintenance calendar draft rules: %w", err)
	}
	if calendar.DraftRules == nil {
		calendar.DraftRules = []types.MaintenanceWindowRule{}
	}
	return nil
}

func lockMaintenanceCalendar(
	ctx context.Context,
	organizationID, calendarID uuid.UUID,
) (*types.MaintenanceCalendar, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+maintenanceCalendarOutputExpr+`
		FROM MaintenanceCalendar c
		WHERE c.organization_id = @organizationID
		  AND c.id = @calendarID
		FOR UPDATE`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"calendarID":     calendarID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not lock maintenance calendar: %w", err)
	}
	calendar, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.MaintenanceCalendar],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not collect locked maintenance calendar: %w", err)
	}
	if err := decodeMaintenanceCalendarRules(&calendar); err != nil {
		return nil, err
	}
	return &calendar, nil
}

func nextMaintenanceCalendarVersionNumber(
	ctx context.Context,
	calendarID uuid.UUID,
) (int64, error) {
	var next int64
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT coalesce(max(version_number), 0) + 1
		FROM MaintenanceCalendarVersion
		WHERE maintenance_calendar_id = @calendarID`,
		pgx.NamedArgs{"calendarID": calendarID},
	).Scan(&next)
	if err != nil {
		return 0, fmt.Errorf("could not determine next maintenance calendar version: %w", err)
	}
	return next, nil
}

func insertMaintenanceCalendarVersion(
	ctx context.Context,
	version *types.MaintenanceCalendarVersion,
) error {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		INSERT INTO MaintenanceCalendarVersion AS v (
			maintenance_calendar_id,
			organization_id,
			version_number,
			source_draft_revision,
			name,
			description,
			iana_zone,
			rule_version,
			canonical_payload,
			checksum,
			published_by_useraccount_id
		) VALUES (
			@calendarID,
			@organizationID,
			@versionNumber,
			@sourceDraftRevision,
			@name,
			@description,
			@ianaZone,
			@ruleVersion,
			@canonicalPayload,
			@checksum,
			@publishedBy
		)
		RETURNING `+maintenanceCalendarVersionOutputExpr,
		pgx.NamedArgs{
			"calendarID":          version.CalendarID,
			"organizationID":      version.OrganizationID,
			"versionNumber":       version.VersionNumber,
			"sourceDraftRevision": version.SourceDraftRevision,
			"name":                version.Name,
			"description":         version.Description,
			"ianaZone":            version.IANAZone,
			"ruleVersion":         version.RuleVersion,
			"canonicalPayload":    version.CanonicalPayload,
			"checksum":            version.Checksum,
			"publishedBy":         version.PublishedBy,
		},
	)
	if err != nil {
		return mapCalendarWriteError("publish maintenance calendar", err)
	}
	inserted, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.MaintenanceCalendarVersion],
	)
	if err != nil {
		return mapCalendarWriteError("read published maintenance calendar", err)
	}
	inserted.WindowRules = version.WindowRules
	*version = inserted
	return nil
}

func insertMaintenanceWindowRules(
	ctx context.Context,
	version types.MaintenanceCalendarVersion,
) error {
	if len(version.WindowRules) == 0 {
		return apierrors.NewBadRequest("at least one maintenance window rule is required")
	}
	for index := range version.WindowRules {
		version.WindowRules[index].OrganizationID = version.OrganizationID
		version.WindowRules[index].CalendarVersionID = version.ID
		if version.WindowRules[index].VersionRuleID == uuid.Nil {
			return apierrors.NewBadRequest(
				"published maintenance window rule identity is required",
			)
		}
	}
	count, err := internalctx.GetDb(ctx).CopyFrom(
		ctx,
		pgx.Identifier{"maintenancewindowrule"},
		[]string{
			"id",
			"logical_rule_id",
			"organization_id",
			"calendar_version_id",
			"name",
			"weekdays",
			"start_minute",
			"end_minute",
			"sort_order",
		},
		pgx.CopyFromSlice(len(version.WindowRules), func(index int) ([]any, error) {
			rule := version.WindowRules[index]
			return []any{
				rule.VersionRuleID,
				rule.ID,
				rule.OrganizationID,
				rule.CalendarVersionID,
				rule.Name,
				rule.Weekdays,
				rule.StartMinute,
				rule.EndMinute,
				rule.SortOrder,
			}, nil
		}),
	)
	if err != nil {
		return mapCalendarWriteError("publish maintenance window rules", err)
	}
	if count != int64(len(version.WindowRules)) {
		return errors.New("published maintenance window rule count is incomplete")
	}
	return nil
}

func setMaintenanceCalendarPublishedVersion(
	ctx context.Context,
	organizationID, calendarID uuid.UUID,
	expectedDraftRevision int64,
	versionID, actorID uuid.UUID,
) error {
	tag, err := internalctx.GetDb(ctx).Exec(ctx, `
		UPDATE MaintenanceCalendar
		SET
			last_published_version_id = @versionID,
			updated_by_useraccount_id = @actorID,
			updated_at = now()
		WHERE organization_id = @organizationID
		  AND id = @calendarID
		  AND draft_revision = @expectedDraftRevision`,
		pgx.NamedArgs{
			"organizationID":        organizationID,
			"calendarID":            calendarID,
			"expectedDraftRevision": expectedDraftRevision,
			"versionID":             versionID,
			"actorID":               actorID,
		},
	)
	if err != nil {
		return mapCalendarWriteError("link published maintenance calendar", err)
	}
	if tag.RowsAffected() != 1 {
		return apierrors.NewConflict("maintenance calendar draft revision changed")
	}
	return nil
}

func getMaintenanceCalendarVersionByDraft(
	ctx context.Context,
	organizationID, calendarID uuid.UUID,
	sourceDraftRevision int64,
) (*types.MaintenanceCalendarVersion, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+maintenanceCalendarVersionOutputExpr+`
		FROM MaintenanceCalendarVersion v
		WHERE v.organization_id = @organizationID
		  AND v.maintenance_calendar_id = @calendarID
		  AND v.source_draft_revision = @sourceDraftRevision`,
		pgx.NamedArgs{
			"organizationID":      organizationID,
			"calendarID":          calendarID,
			"sourceDraftRevision": sourceDraftRevision,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not get published maintenance calendar draft: %w", err)
	}
	version, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.MaintenanceCalendarVersion],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not collect published maintenance calendar draft: %w", err)
	}
	version.WindowRules, err = listMaintenanceWindowRules(ctx, organizationID, version.ID)
	if err != nil {
		return nil, err
	}
	return &version, nil
}

func listMaintenanceWindowRules(
	ctx context.Context,
	organizationID, versionID uuid.UUID,
) ([]types.MaintenanceWindowRule, error) {
	grouped, err := listMaintenanceWindowRulesForVersions(
		ctx,
		organizationID,
		[]uuid.UUID{versionID},
	)
	if err != nil {
		return nil, err
	}
	return grouped[versionID], nil
}

func listMaintenanceWindowRulesForVersions(
	ctx context.Context,
	organizationID uuid.UUID,
	versionIDs []uuid.UUID,
) (map[uuid.UUID][]types.MaintenanceWindowRule, error) {
	if len(versionIDs) == 0 {
		return map[uuid.UUID][]types.MaintenanceWindowRule{}, nil
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+maintenanceWindowRuleOutputExpr+`
		FROM MaintenanceWindowRule r
		WHERE r.organization_id = @organizationID
		  AND r.calendar_version_id = ANY(@versionIDs)
		ORDER BY r.calendar_version_id, r.sort_order, r.name, r.logical_rule_id, r.id`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"versionIDs":     versionIDs,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not list maintenance window rules: %w", err)
	}
	rules, err := pgx.CollectRows(
		rows,
		pgx.RowToStructByName[types.MaintenanceWindowRule],
	)
	if err != nil {
		return nil, fmt.Errorf("could not collect maintenance window rules: %w", err)
	}
	return groupMaintenanceWindowRules(versionIDs, rules), nil
}

func groupMaintenanceWindowRules(
	versionIDs []uuid.UUID,
	rules []types.MaintenanceWindowRule,
) map[uuid.UUID][]types.MaintenanceWindowRule {
	grouped := make(map[uuid.UUID][]types.MaintenanceWindowRule, len(versionIDs))
	for _, versionID := range versionIDs {
		grouped[versionID] = []types.MaintenanceWindowRule{}
	}
	for _, rule := range rules {
		grouped[rule.CalendarVersionID] = append(
			grouped[rule.CalendarVersionID],
			rule,
		)
	}
	return grouped
}

func lockDeploymentFreeze(
	ctx context.Context,
	organizationID, freezeID uuid.UUID,
) (*types.DeploymentFreeze, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+deploymentFreezeOutputExpr+`
		FROM DeploymentFreeze f
		WHERE f.organization_id = @organizationID
		  AND f.id = @freezeID
		FOR UPDATE`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"freezeID":       freezeID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not lock deployment freeze: %w", err)
	}
	freeze, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.DeploymentFreeze],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not collect locked deployment freeze: %w", err)
	}
	return &freeze, nil
}

func nextDeploymentFreezeRevisionNumber(
	ctx context.Context,
	freezeID uuid.UUID,
) (int64, error) {
	var next int64
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT coalesce(max(version_number), 0) + 1
		FROM DeploymentFreezeRevision
		WHERE deployment_freeze_id = @freezeID`,
		pgx.NamedArgs{"freezeID": freezeID},
	).Scan(&next)
	if err != nil {
		return 0, fmt.Errorf("could not determine next deployment freeze revision: %w", err)
	}
	return next, nil
}

func insertDeploymentFreezeRevision(
	ctx context.Context,
	revision *types.DeploymentFreezeRevision,
) error {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		INSERT INTO DeploymentFreezeRevision AS r (
			deployment_freeze_id,
			organization_id,
			version_number,
			source_draft_revision,
			name,
			start_at,
			end_at,
			iana_zone,
			rule_version,
			scope_kind,
			scope_id,
			priority,
			reason,
			canonical_payload,
			checksum,
			published_by_useraccount_id
		) VALUES (
			@freezeID,
			@organizationID,
			@versionNumber,
			@sourceDraftRevision,
			@name,
			@startAt,
			@endAt,
			@ianaZone,
			@ruleVersion,
			@scopeKind,
			@scopeID,
			@priority,
			@reason,
			@canonicalPayload,
			@checksum,
			@publishedBy
		)
		RETURNING `+deploymentFreezeRevisionOutputExpr,
		pgx.NamedArgs{
			"freezeID":            revision.FreezeID,
			"organizationID":      revision.OrganizationID,
			"versionNumber":       revision.VersionNumber,
			"sourceDraftRevision": revision.SourceDraftRevision,
			"name":                revision.Name,
			"startAt":             revision.StartAt,
			"endAt":               revision.EndAt,
			"ianaZone":            revision.IANAZone,
			"ruleVersion":         revision.RuleVersion,
			"scopeKind":           revision.ScopeKind,
			"scopeID":             revision.ScopeID,
			"priority":            revision.Priority,
			"reason":              revision.Reason,
			"canonicalPayload":    revision.CanonicalPayload,
			"checksum":            revision.Checksum,
			"publishedBy":         revision.PublishedBy,
		},
	)
	if err != nil {
		return mapCalendarWriteError("publish deployment freeze", err)
	}
	inserted, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.DeploymentFreezeRevision],
	)
	if err != nil {
		return mapCalendarWriteError("read published deployment freeze", err)
	}
	*revision = inserted
	return nil
}

func setDeploymentFreezePublishedRevision(
	ctx context.Context,
	organizationID, freezeID uuid.UUID,
	expectedDraftRevision int64,
	revisionID, actorID uuid.UUID,
) error {
	tag, err := internalctx.GetDb(ctx).Exec(ctx, `
		UPDATE DeploymentFreeze
		SET
			last_published_revision_id = @revisionID,
			updated_by_useraccount_id = @actorID,
			updated_at = now()
		WHERE organization_id = @organizationID
		  AND id = @freezeID
		  AND draft_revision = @expectedDraftRevision`,
		pgx.NamedArgs{
			"organizationID":        organizationID,
			"freezeID":              freezeID,
			"expectedDraftRevision": expectedDraftRevision,
			"revisionID":            revisionID,
			"actorID":               actorID,
		},
	)
	if err != nil {
		return mapCalendarWriteError("link published deployment freeze", err)
	}
	if tag.RowsAffected() != 1 {
		return apierrors.NewConflict("deployment freeze draft revision changed")
	}
	return nil
}

func getDeploymentFreezeRevisionByDraft(
	ctx context.Context,
	organizationID, freezeID uuid.UUID,
	sourceDraftRevision int64,
) (*types.DeploymentFreezeRevision, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+deploymentFreezeRevisionOutputExpr+`
		FROM DeploymentFreezeRevision r
		WHERE r.organization_id = @organizationID
		  AND r.deployment_freeze_id = @freezeID
		  AND r.source_draft_revision = @sourceDraftRevision`,
		pgx.NamedArgs{
			"organizationID":      organizationID,
			"freezeID":            freezeID,
			"sourceDraftRevision": sourceDraftRevision,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not get published deployment freeze draft: %w", err)
	}
	revision, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.DeploymentFreezeRevision],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not collect published deployment freeze draft: %w", err)
	}
	return &revision, nil
}

func ensureCalendarScopeBelongsToOrganization(
	ctx context.Context,
	organizationID uuid.UUID,
	kind types.CalendarScopeKind,
	scopeID uuid.UUID,
) error {
	if organizationID == uuid.Nil || !kind.IsValid() || scopeID == uuid.Nil {
		return apierrors.NewBadRequest("calendar scope is invalid")
	}
	if kind == types.CalendarScopeOrganization {
		if scopeID != organizationID {
			return apierrors.ErrNotFound
		}
		return nil
	}
	var table string
	switch kind {
	case types.CalendarScopeCustomer:
		table = "CustomerOrganization"
	case types.CalendarScopeEnvironment:
		table = "Environment"
	case types.CalendarScopeDeploymentUnit:
		table = "DeploymentUnit"
	case types.CalendarScopeComponent:
		table = "ComponentDefinition"
	case types.CalendarScopeCampaign:
		table = "DeploymentCampaignDraft"
	default:
		return apierrors.NewBadRequest("calendar scope is invalid")
	}
	var exists bool
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1
		  FROM `+table+`
		  WHERE id = @scopeID
		    AND organization_id = @organizationID
		)`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"scopeID":        scopeID,
		},
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("could not validate calendar scope: %w", err)
	}
	if !exists {
		return apierrors.ErrNotFound
	}
	return nil
}

func maintenanceCalendarExists(
	ctx context.Context,
	organizationID, calendarID uuid.UUID,
) (bool, error) {
	var exists bool
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1
		  FROM MaintenanceCalendar
		  WHERE organization_id = @organizationID
		    AND id = @calendarID
		)`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"calendarID":     calendarID,
		},
	).Scan(&exists)
	return exists, err
}

func deploymentFreezeExists(
	ctx context.Context,
	organizationID, freezeID uuid.UUID,
) (bool, error) {
	var exists bool
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1
		  FROM DeploymentFreeze
		  WHERE organization_id = @organizationID
		    AND id = @freezeID
		)`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"freezeID":       freezeID,
		},
	).Scan(&exists)
	return exists, err
}

func sortWindowRules(rules []types.MaintenanceWindowRule) {
	slices.SortFunc(rules, func(left, right types.MaintenanceWindowRule) int {
		if left.SortOrder != right.SortOrder {
			return int(left.SortOrder - right.SortOrder)
		}
		if comparison := strings.Compare(left.Name, right.Name); comparison != 0 {
			return comparison
		}
		return strings.Compare(left.ID.String(), right.ID.String())
	})
}

func resolvePublicationReplay[T any](
	existing *T,
	lookupErr error,
	currentDraftRevision, requestedDraftRevision int64,
	resourceName string,
) (*T, bool, error) {
	if lookupErr == nil && existing != nil {
		return existing, true, nil
	}
	if lookupErr != nil && !errors.Is(lookupErr, apierrors.ErrNotFound) {
		return nil, false, lookupErr
	}
	if currentDraftRevision != requestedDraftRevision {
		return nil, false, apierrors.NewConflict(
			resourceName + " draft revision changed",
		)
	}
	return nil, false, nil
}

func assignVersionScopedWindowRuleIDs(version *types.MaintenanceCalendarVersion) {
	for index := range version.WindowRules {
		rule := &version.WindowRules[index]
		rule.VersionRuleID = uuid.NewSHA1(version.ID, rule.ID[:])
		rule.OrganizationID = version.OrganizationID
		rule.CalendarVersionID = version.ID
	}
}

func mapCalendarWriteError(action string, err error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case pgerrcode.ForeignKeyViolation:
			return apierrors.ErrNotFound
		case pgerrcode.UniqueViolation:
			return apierrors.ErrAlreadyExists
		case pgerrcode.CheckViolation, pgerrcode.NotNullViolation:
			return apierrors.NewBadRequest("calendar or freeze value violates its contract")
		}
	}
	return fmt.Errorf("could not %s: %w", action, err)
}
